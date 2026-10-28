package providers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// setResolverForTest swaps the client's resolver.
func setResolverForTest(t *testing.T, c *http.Client, r resolverFunc) {
	t.Helper()
	pt, ok := c.Transport.(*pinnedTransport)
	if !ok {
		t.Fatalf("transport is %T, want *pinnedTransport", c.Transport)
	}
	pt.resolve = r
}

func TestTransport_RefusesPrivateDestinationInProductionLike(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	_, err := client.Get(srv.URL) // 127.0.0.1
	if !errors.Is(err, ErrDestinationNotPublic) {
		t.Fatalf("err = %v, want ErrDestinationNotPublic", err)
	}
}

func TestTransport_ResolverRebindIsRefused(t *testing.T) {
	// A hostname whose A record is private must be refused at dial time even
	// though the URL string looks public.
	client := NewHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	setResolverForTest(t, client, func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
	})
	_, err := client.Get("https://api.example.test/v1/models")
	if !errors.Is(err, ErrDestinationNotPublic) {
		t.Fatalf("err = %v, want ErrDestinationNotPublic", err)
	}
}

func TestTransport_MixedAnswerWithOnePrivateAddressIsRefused(t *testing.T) {
	client := NewHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	setResolverForTest(t, client, func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("169.254.169.254")}}, nil
	})
	_, err := client.Get("https://api.example.test/")
	if !errors.Is(err, ErrDestinationNotPublic) {
		t.Fatalf("err = %v, want ErrDestinationNotPublic", err)
	}
}

func TestTransport_RebindBetweenCallsIsCaughtAtDial(t *testing.T) {
	// Every new connection re-resolves and re-checks: an answer that turns
	// non-public is refused each time, never cached from an earlier dial.
	client := NewHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	calls := 0
	setResolverForTest(t, client, func(_ context.Context, _ string) ([]net.IPAddr, error) {
		calls++
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil // never public
	})
	for i := 0; i < 2; i++ {
		if _, err := client.Get("https://api.example.test/"); !errors.Is(err, ErrDestinationNotPublic) {
			t.Fatalf("call %d err = %v, want ErrDestinationNotPublic", i, err)
		}
	}
	if calls != 2 {
		t.Fatalf("resolver calls = %d, want 2 (resolved per dial)", calls)
	}
}

func TestTransport_DialsTheValidatedIPAndKeepsHostHeader(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second})
	setResolverForTest(t, client, func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "llm.example.test" {
			t.Errorf("resolved %q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	resp, err := client.Get("http://llm.example.test:" + u.Port() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotHost != "llm.example.test:"+u.Port() {
		t.Fatalf("Host header = %q", gotHost)
	}
}

func TestTransport_IgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("proxy env must be ignored: %v", err)
	}
	resp.Body.Close()
}

func TestTransport_RefusesRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second})
	_, err := client.Get(srv.URL)
	if !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("err = %v, want ErrRedirectRefused", err)
	}
}

func TestReadAllBounded(t *testing.T) {
	if _, err := ReadAllBounded(strings.NewReader(strings.Repeat("x", 2048)), 1024); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
	b, err := ReadAllBounded(strings.NewReader("ok"), 1024)
	if err != nil || string(b) != "ok" {
		t.Fatalf("b=%q err=%v", b, err)
	}
}

func TestTransport_CapsNonStreamingResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.(http.Flusher).Flush() // force chunked: no Content-Length shortcut
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second, MaxResponseBytes: 1024})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body) // a plain reader, like an SDK decoder: the transport enforces the cap
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestTransport_RejectsOversizedContentLengthUpFront(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2048))) // small write: net/http sets Content-Length
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second, MaxResponseBytes: 1024})
	if _, err := client.Get(srv.URL); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestTransport_BodyUnderTheCapPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1024)))
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second, MaxResponseBytes: 1024})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || len(b) != 1024 {
		t.Fatalf("len=%d err=%v", len(b), err)
	}
}

func streamGet(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(WithStreaming(context.Background()), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client.Do(req)
}

func TestTransport_UpstreamContentTypeCannotLiftTheSizeCap(t *testing.T) {
	for _, ct := range []string{"text/event-stream", "application/x-ndjson", "text/event-stream; charset=utf-8"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
		}))
		client := NewHTTPClient(TransportConfig{ProductionLike: false, MaxResponseBytes: 1024})
		resp, err := client.Get(srv.URL) // a non-stream request: the response header must not matter
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		srv.Close()
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("content-type %q: err = %v, want ErrResponseTooLarge", ct, err)
		}
	}
}

func TestTransport_NonStreamIsNotCutByTheStreamIdleTimeout(t *testing.T) {
	// A slow, reasoning-like model: no headers for longer than IdleTimeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, IdleTimeout: 100 * time.Millisecond, HeaderTimeout: 5 * time.Second})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("slow non-stream call was cut: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "done" {
		t.Fatalf("body = %q", b)
	}
}

func TestTransport_HeaderTimeoutBoundsANonStreamCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, HeaderTimeout: 150 * time.Millisecond})
	start := time.Now()
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("expected a header timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestTransport_StreamIdleTimeoutAbortsAStalledStream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: hi\n\n"))
		w.(http.Flusher).Flush()
		select { // then go silent
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(release)
	client := NewHTTPClient(TransportConfig{ProductionLike: false, IdleTimeout: 150 * time.Millisecond})
	resp, err := streamGet(t, client, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, ErrStreamIdle) {
		t.Fatalf("err = %v, want ErrStreamIdle", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("idle abort took %v", time.Since(start))
	}
}

func TestTransport_StreamThatNeverSendsHeadersIsAbortedByIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, IdleTimeout: 150 * time.Millisecond, HeaderTimeout: time.Minute})
	start := time.Now()
	_, err := streamGet(t, client, srv.URL)
	if !errors.Is(err, ErrStreamIdle) {
		t.Fatalf("err = %v, want ErrStreamIdle", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestTransport_StreamHeaderWaitHonoursCallerCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, IdleTimeout: time.Minute})
	ctx, cancel := context.WithCancel(WithStreaming(context.Background()))
	time.AfterFunc(100*time.Millisecond, cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrStreamIdle) {
		t.Fatalf("err = %v, want context.Canceled (not an idle timeout)", err)
	}
}

func TestTransport_ActiveStreamIsNotCutAndHasNoTotalCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 8; i++ {
			_, _ = w.Write([]byte("data: " + strings.Repeat("y", 400) + "\n\n"))
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()
	// total (>3 KiB) exceeds MaxResponseBytes and the elapsed time exceeds
	// IdleTimeout, but no single gap does: a healthy stream must complete.
	client := NewHTTPClient(TransportConfig{ProductionLike: false, IdleTimeout: 200 * time.Millisecond, MaxResponseBytes: 1024})
	resp, err := streamGet(t, client, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(b) < 3000 {
		t.Fatalf("read %d bytes", len(b))
	}
}

// Final review I1: the response cap is raised per request (embeddings),
// never for the client as a whole.
func TestTransport_ResponseCapMarkerRaisesTheCapForThatRequestOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush() // chunked: exercise the body cap
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second, MaxResponseBytes: 1024})
	get := func(ctx context.Context) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		return err
	}
	if err := get(WithResponseCap(context.Background(), 4096)); err != nil {
		t.Fatalf("raised cap: %v", err)
	}
	if err := get(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("unmarked request err = %v, want ErrResponseTooLarge", err)
	}
	if err := get(WithResponseCap(context.Background(), 0)); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("zero marker err = %v, want the client's cap", err)
	}
}

// Final review I3: the stream idle timeout is set per request (the router
// derives it from request_timeout); the client's IdleTimeout is only the
// default for unmarked streams.
func TestTransport_StreamIdleMarkerOverridesTheClientIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/late-headers" {
			time.Sleep(400 * time.Millisecond) // silent before the headers
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: thinking\n\n"))
		w.(http.Flusher).Flush()
		time.Sleep(400 * time.Millisecond) // a silent reasoning phase
		_, _ = w.Write([]byte("data: answer\n\n"))
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, IdleTimeout: 100 * time.Millisecond})
	read := func(ctx context.Context, path string) error {
		req, _ := http.NewRequestWithContext(WithStreaming(ctx), http.MethodGet, srv.URL+path, nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		return err
	}
	for _, path := range []string{"/", "/late-headers"} {
		if err := read(WithStreamIdleTimeout(context.Background(), 2*time.Second), path); err != nil {
			t.Fatalf("%s with a 2s idle marker: %v", path, err)
		}
		if err := read(context.Background(), path); !errors.Is(err, ErrStreamIdle) {
			t.Fatalf("%s unmarked err = %v, want ErrStreamIdle (client default 100ms)", path, err)
		}
	}
	if d := StreamIdleTimeout(WithStreamIdleTimeout(context.Background(), -time.Second)); d != 0 {
		t.Fatalf("negative marker = %v", d)
	}
}

func TestInstallationHTTPClient_ReachesPrivateHostWhileTheOrgClientCannot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	org := NewHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	if _, err := org.Get(srv.URL); !errors.Is(err, ErrDestinationNotPublic) {
		t.Fatalf("org client err = %v, want ErrDestinationNotPublic", err)
	}
	inst := NewInstallationHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	resp, err := inst.Get(srv.URL)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("installation client = %v, %v", resp, err)
	}
	resp.Body.Close()
}

func TestInstallationHTTPClient_KeepsTheOtherProtections(t *testing.T) {
	redirect := httptest.NewServer(http.RedirectHandler("http://example.test/", http.StatusFound))
	defer redirect.Close()
	if _, err := NewInstallationHTTPClient(TransportConfig{}).Get(redirect.URL); !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("err = %v, want ErrRedirectRefused", err)
	}
}
