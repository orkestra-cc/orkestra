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
	// First answer public, second private: a fresh connection must re-check.
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
	resp, err := client.Get(srv.URL)
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
	resp, err := client.Get(srv.URL)
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
