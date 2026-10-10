package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	defaultHeaderTimeout    = 5 * time.Minute
	defaultIdleTimeout      = 30 * time.Second
	// MaxEmbedResponseBytes is the non-stream cap of an embedding call:
	// the contract admits 256 inputs, and 256 vectors of a 3072-dim model
	// in float JSON are well past the 8 MiB default.
	MaxEmbedResponseBytes = 32 << 20
)

// responseCapKey and streamIdleKey carry per-request overrides of the
// client's non-stream body cap and stream idle timeout.
type (
	responseCapKey struct{}
	streamIdleKey  struct{}
)

// WithResponseCap sets the non-stream body cap of every request made with
// the returned context to n bytes (n <= 0 leaves the client's cap). Like
// WithStreaming it is request-side on purpose: an adapter raises the cap
// only for the call shape that needs it (embeddings, MaxEmbedResponseBytes),
// never because of anything the response says.
func WithResponseCap(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, responseCapKey{}, n)
}

func responseCap(ctx context.Context, def int64) int64 {
	if n, _ := ctx.Value(responseCapKey{}).(int64); n > 0 {
		return n
	}
	return def
}

// WithStreamIdleTimeout sets the stream idle timeout (the wait for headers
// and every silence between two reads) of the streaming requests made
// with the returned context; d <= 0 leaves the client's IdleTimeout. The
// router derives it per call from the hot-reloadable request_timeout, so
// a model that thinks silently is cut by the attempt deadline, not by a
// fixed 30 s.
func WithStreamIdleTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, streamIdleKey{}, d)
}

// StreamIdleTimeout returns the idle timeout set by WithStreamIdleTimeout
// (0 when none).
func StreamIdleTimeout(ctx context.Context) time.Duration {
	d, _ := ctx.Value(streamIdleKey{}).(time.Duration)
	return max(d, 0)
}

// streamingKey marks a request as an intentional stream.
type streamingKey struct{}

// WithStreaming marks every request made with the returned context as a
// stream: the response body is NOT size-capped but must keep producing data
// (TransportConfig.IdleTimeout applies to the wait for headers and between
// reads). Adapters call it on, and only on, the streaming call they are about
// to make. The choice is request-side on purpose: it must never be inferred
// from the response (Content-Type is chosen by the upstream, and a hostile
// endpoint could otherwise lift the size cap with one header).
func WithStreaming(ctx context.Context) context.Context {
	return context.WithValue(ctx, streamingKey{}, true)
}

func isStreaming(ctx context.Context) bool {
	v, _ := ctx.Value(streamingKey{}).(bool)
	return v
}

// TransportConfig tunes the one http.Client adapters may use. One client
// serves both call shapes; the shape is chosen per request.
//
//   - Non-streaming (default, no marker): the body is capped at
//     MaxResponseBytes (or the request's WithResponseCap) whatever the
//     response Content-Type says, and the wait
//     for headers is bounded by HeaderTimeout (generous: a reasoning model
//     sends nothing until it is done). Stall detection is the caller's
//     context deadline (or Timeout).
//   - Streaming (request context from WithStreaming): no total cap (the
//     decoder bounds each event), but IdleTimeout (or the request's
//     WithStreamIdleTimeout) bounds the wait for headers and every silence
//     between two reads.
//
// Timeout is http.Client.Timeout and also covers the body read, so it would
// cut healthy streams: a client that serves streams must leave it 0 and rely
// on the request context.
type TransportConfig struct {
	ProductionLike   bool
	Timeout          time.Duration // whole-exchange timeout incl. body; 0 = none (required for streaming clients)
	MaxResponseBytes int64         // non-stream body cap; 0 = 8 MiB
	HeaderTimeout    time.Duration // non-stream wait for response headers; 0 = 5 min
	IdleTimeout      time.Duration // stream: headers + inter-read silence; 0 = 30s
}

type resolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

type pinnedTransport struct {
	base      *http.Transport
	resolve   resolverFunc
	prod      bool
	maxBytes  int64
	idleAfter time.Duration // stream idle timeout
}

// NewHTTPClient builds the only http.Client the adapters may use. Every new
// connection resolves the host itself and dials a validated IP, so a DNS
// answer that changed since the operator saved the URL (rebinding) is caught
// at dial time. The dialled address is the one that was validated (no second
// lookup), Host header and TLS SNI stay the original hostname, proxies from
// the environment are ignored (a proxy would bypass the IP check) and
// redirects are refused outright. Response bodies are size-capped (or, for
// requests marked WithStreaming, idle-bounded) inside the RoundTripper.
func NewHTTPClient(cfg TransportConfig) *http.Client {
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	header := cfg.HeaderTimeout
	if header <= 0 {
		header = defaultHeaderTimeout
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
		ResponseHeaderTimeout: header,
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
	if isStreaming(req.Context()) {
		return p.roundTripStream(req)
	}
	maxBytes := responseCap(req.Context(), p.maxBytes)
	resp, err := p.base.RoundTrip(req)
	if err != nil || resp.Body == nil || resp.Body == http.NoBody {
		return resp, err
	}
	if resp.ContentLength > maxBytes {
		_ = resp.Body.Close()
		return nil, ErrResponseTooLarge
	}
	resp.Body = &cappedBody{rc: resp.Body, max: maxBytes}
	return resp, nil
}

// roundTripStream bounds the wait for headers with the idle timeout (by
// cancelling a derived context, released again when the body is closed) and
// wraps the body in the per-read idle guard.
func (p *pinnedTransport) roundTripStream(req *http.Request) (*http.Response, error) {
	idle := p.idleAfter
	if d := StreamIdleTimeout(req.Context()); d > 0 {
		idle = d
	}
	ctx, cancel := context.WithCancel(req.Context())
	var stalled atomic.Bool
	t := time.AfterFunc(idle, func() {
		stalled.Store(true)
		cancel()
	})
	resp, err := p.base.RoundTrip(req.WithContext(ctx))
	t.Stop()
	if err != nil {
		cancel()
		if stalled.Load() && req.Context().Err() == nil {
			return nil, ErrStreamIdle
		}
		return nil, err
	}
	if resp.Body == nil || resp.Body == http.NoBody {
		cancel()
		return resp, nil
	}
	resp.Body = newIdleBody(resp.Body, idle, cancel)
	return resp, nil
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
	rc      io.ReadCloser
	d       time.Duration
	t       *time.Timer
	fired   atomic.Bool
	release context.CancelFunc
}

func newIdleBody(rc io.ReadCloser, d time.Duration, release context.CancelFunc) *idleBody {
	b := &idleBody{rc: rc, d: d, release: release}
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
	err := b.rc.Close()
	b.release()
	return err
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
