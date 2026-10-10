package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/orkestra/backend/internal/core/llm/netguard"
)

var (
	ErrDestinationNotPublic = errors.New("llm providers: destination resolves to a non-public address")
	ErrResponseTooLarge     = errors.New("llm providers: response exceeds the size limit")
	ErrRedirectRefused      = errors.New("llm providers: redirects are not followed")
	ErrStreamIdle           = errors.New("llm providers: stream stalled (idle timeout)")
)

const (
	defaultMaxResponseBytes = 8 << 20
	defaultIdleTimeout      = 30 * time.Second
)

// TransportConfig tunes the one http.Client adapters may use.
type TransportConfig struct {
	ProductionLike bool
	// Timeout is http.Client.Timeout: it bounds the whole exchange INCLUDING
	// reading the body. Streaming callers must leave it 0 and rely on the
	// request context plus IdleTimeout.
	Timeout time.Duration
	// MaxResponseBytes caps every non-streaming response body (enforced in
	// the transport, so SDK decoders that read the body directly are bound
	// too); 0 = 8 MiB. Streams are not capped in total: their per-event size
	// is bounded by the decoder and their liveness by IdleTimeout.
	MaxResponseBytes int64
	// IdleTimeout is both the response-header timeout and, for event-stream
	// / NDJSON bodies, the maximum silence between two reads; 0 = 30s. A
	// non-streaming call whose provider only answers after generating the
	// whole completion needs this raised to its expected latency.
	IdleTimeout time.Duration
}

type resolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

type pinnedTransport struct {
	base      *http.Transport
	resolve   resolverFunc
	prod      bool
	maxBytes  int64
	idleAfter time.Duration
}

// NewHTTPClient builds the only http.Client the adapters may use. Every new
// connection resolves the host itself and dials a validated IP, so a DNS
// answer that changed since the operator saved the URL (rebinding) is caught
// at dial time. The dialled address is the one that was validated (no second
// lookup), Host header and TLS SNI stay the original hostname, proxies from
// the environment are ignored (a proxy would bypass the IP check) and
// redirects are refused outright. Response bodies are size-capped (or idle-
// bounded for streams) inside the RoundTripper.
func NewHTTPClient(cfg TransportConfig) *http.Client {
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}
	pt := &pinnedTransport{prod: cfg.ProductionLike, resolve: net.DefaultResolver.LookupIPAddr, maxBytes: maxBytes, idleAfter: idle}
	base := &http.Transport{
		Proxy:                 nil, // never honour HTTP_PROXY: the proxy would bypass the IP check
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: idle,
		ExpectContinueTimeout: time.Second,
	}
	base.DialContext = pt.dial
	pt.base = base
	return &http.Client{
		Transport: pt,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrRedirectRefused
		},
	}
}

func (p *pinnedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := p.base.RoundTrip(req)
	if err != nil || resp.Body == nil || resp.Body == http.NoBody {
		return resp, err
	}
	if isStreamingContentType(resp.Header.Get("Content-Type")) {
		resp.Body = newIdleBody(resp.Body, p.idleAfter)
		return resp, nil
	}
	if resp.ContentLength > p.maxBytes {
		_ = resp.Body.Close()
		return nil, ErrResponseTooLarge
	}
	resp.Body = &cappedBody{rc: resp.Body, max: p.maxBytes}
	return resp, nil
}

func isStreamingContentType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(ct))
	}
	switch mt {
	case "text/event-stream", "application/x-ndjson", "application/ndjson", "application/jsonl", "application/x-jsonlines", "application/stream+json":
		return true
	}
	return false
}

func (p *pinnedTransport) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := p.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("llm providers: no address for %s", host)
	}
	if p.prod {
		// All-or-nothing: one private answer among public ones is how a
		// rebinding attacker hides the interesting address.
		for _, ip := range ips {
			if !netguard.IsPublicIP(ip) {
				return nil, fmt.Errorf("%w: %s", ErrDestinationNotPublic, host)
			}
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// cappedBody fails with ErrResponseTooLarge as soon as more than max bytes
// have been delivered.
type cappedBody struct {
	rc   io.ReadCloser
	max  int64
	read int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.read > b.max {
		return 0, ErrResponseTooLarge
	}
	if int64(len(p)) > b.max-b.read+1 {
		p = p[:b.max-b.read+1] // read at most one byte past the cap to detect overflow
	}
	n, err := b.rc.Read(p)
	b.read += int64(n)
	if b.read > b.max {
		return n - int(b.read-b.max), ErrResponseTooLarge
	}
	return n, err
}

func (b *cappedBody) Close() error { return b.rc.Close() }

// idleBody closes the underlying body when a single Read stays blocked for
// longer than d, turning a stalled stream into ErrStreamIdle. The timer runs
// only while a Read is in flight, so a slow consumer is never blamed on the
// provider.
type idleBody struct {
	rc    io.ReadCloser
	d     time.Duration
	t     *time.Timer
	fired atomic.Bool
}

func newIdleBody(rc io.ReadCloser, d time.Duration) *idleBody {
	b := &idleBody{rc: rc, d: d}
	b.t = time.AfterFunc(d, func() {
		b.fired.Store(true)
		_ = rc.Close()
	})
	b.t.Stop()
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	if b.fired.Load() {
		return 0, ErrStreamIdle
	}
	b.t.Reset(b.d)
	n, err := b.rc.Read(p)
	b.t.Stop()
	if err != nil && b.fired.Load() {
		err = ErrStreamIdle
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.t.Stop()
	return b.rc.Close()
}

// ReadAllBounded reads at most limit bytes and fails when more arrive.
func ReadAllBounded(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrResponseTooLarge
	}
	return b, nil
}
