package qdrant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

type recorded struct {
	Method string
	Path   string // path including the query string
	Header http.Header
	Body   map[string]any
}

type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

func (r *recorder) add(req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, recorded{Method: req.Method, Path: req.URL.RequestURI(), Header: req.Header.Clone(), Body: body})
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.reqs...)
}

// fakeServer records every request and answers through h.
func fakeServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func okJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func mustNew(t *testing.T, cfg Config) *Driver {
	t.Helper()
	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func TestNew_ValidatesURLWithoutDialing(t *testing.T) {
	for _, in := range []string{"", "ftp://x", "http://"} {
		_, err := New(Config{URL: in})
		if err == nil {
			t.Errorf("New(%q) = nil error", in)
			continue
		}
		if in != "" && strings.Contains(err.Error(), in) {
			t.Errorf("New(%q) error echoes the input: %v", in, err)
		}
	}
	srv, rec := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { okJSON(w, `{}`) })
	if _, err := New(Config{URL: srv.URL + "/"}); err != nil {
		t.Fatalf("New(valid) = %v", err)
	}
	if n := len(rec.all()); n != 0 {
		t.Fatalf("New dialed: server saw %d requests", n)
	}
}

func TestDo_SendsAPIKey(t *testing.T) {
	srv, rec := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			okJSON(w, `{"version":"1.20.0"}`)
			return
		}
		okJSON(w, `{"result":{}}`)
	})
	d := mustNew(t, Config{URL: srv.URL, APIKey: "k-SECRET"})
	if err := d.Health(t.Context()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	reqs := rec.all()
	if len(reqs) == 0 {
		t.Fatal("no requests")
	}
	for _, r := range reqs {
		if got := r.Header.Get("api-key"); got != "k-SECRET" {
			t.Errorf("%s %s api-key = %q", r.Method, r.Path, got)
		}
	}
}

func TestDo_ClassifiesStatus(t *testing.T) {
	cases := []struct {
		status      int
		unavailable bool
		notFound    bool
	}{
		{500, true, false}, {502, true, false}, {503, true, false},
		{429, true, false}, {401, true, false}, {403, true, false},
		{400, false, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(c.status) })
			d := mustNew(t, Config{URL: srv.URL})
			err := d.Health(t.Context())
			if err == nil {
				t.Fatal("want error")
			}
			if got := errors.Is(err, engine.ErrUnavailable); got != c.unavailable {
				t.Errorf("Is(ErrUnavailable) = %v, want %v (%v)", got, c.unavailable, err)
			}
			if errors.Is(err, engine.ErrNotFound) {
				t.Errorf("Is(ErrNotFound) for %d", c.status)
			}
		})
	}
	t.Run("404 maps to ErrNotFound", func(t *testing.T) {
		srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
		err := mustNew(t, Config{URL: srv.URL}).Health(t.Context())
		if !errors.Is(err, engine.ErrNotFound) {
			t.Fatalf("Health 404 = %v, want ErrNotFound", err)
		}
	})
	t.Run("404 on drop is nil", func(t *testing.T) {
		srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
		if err := mustNew(t, Config{URL: srv.URL}).DropCollection(t.Context(), "c"); err != nil {
			t.Fatalf("DropCollection 404 = %v", err)
		}
	})
}

func TestDo_ErrorsNeverLeak(t *testing.T) {
	srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "BODY-SECRET")
	})
	d := mustNew(t, Config{URL: srv.URL, APIKey: "k-SECRET"})
	err := d.Health(t.Context())
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{srv.URL, strings.TrimPrefix(srv.URL, "http://"), "k-SECRET", "BODY-SECRET"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("error leaks %q: %v", s, err)
		}
	}
}

func TestDo_TimeoutIsUnavailable(t *testing.T) {
	srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(500 * time.Millisecond) })
	d := mustNew(t, Config{URL: srv.URL, Timeout: 50 * time.Millisecond})
	err := d.Health(t.Context())
	if !errors.Is(err, engine.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Health = %v, want ErrUnavailable and DeadlineExceeded", err)
	}
}

func TestDo_CanceledIsNotUnavailable(t *testing.T) {
	srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { okJSON(w, `{}`) })
	d := mustNew(t, Config{URL: srv.URL})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := d.Health(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, engine.ErrUnavailable) {
		t.Fatalf("Health = %v, want Canceled and not ErrUnavailable", err)
	}
}

func TestDo_OversizeBodyIsUnavailable(t *testing.T) {
	srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	})
	d := mustNew(t, Config{URL: srv.URL, MaxResponseBytes: 1024})
	if err := d.Health(t.Context()); !errors.Is(err, engine.ErrUnavailable) {
		t.Fatalf("Health = %v, want ErrUnavailable", err)
	}
}

func TestDo_UndecodableBodyIsUnavailable(t *testing.T) {
	srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "not json") })
	d := mustNew(t, Config{URL: srv.URL})
	if err := d.Health(t.Context()); !errors.Is(err, engine.ErrUnavailable) {
		t.Fatalf("Health = %v, want ErrUnavailable", err)
	}
}

func TestDo_UnreachableIsUnavailable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	d := mustNew(t, Config{URL: "http://" + addr})
	err = d.Health(t.Context())
	if !errors.Is(err, engine.ErrUnavailable) {
		t.Fatalf("Health = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), addr) {
		t.Errorf("error leaks the address: %v", err)
	}
}

func TestHealth_Version(t *testing.T) {
	serve := func(version string, collectionsStatus int) *httptest.Server {
		srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				okJSON(w, fmt.Sprintf(`{"title":"qdrant","version":%q}`, version))
				return
			}
			if collectionsStatus != 200 {
				w.WriteHeader(collectionsStatus)
				return
			}
			okJSON(w, `{"result":{"collections":[]}}`)
		})
		return srv
	}
	t.Run("old version", func(t *testing.T) {
		err := mustNew(t, Config{URL: serve("1.19.2", 200).URL}).Health(t.Context())
		if !errors.Is(err, engine.ErrUnavailable) {
			t.Fatalf("Health(1.19.2) = %v, want ErrUnavailable", err)
		}
	})
	t.Run("old version is not echoed", func(t *testing.T) {
		err := mustNew(t, Config{URL: serve("1.19.2-server-text", 200).URL}).Health(t.Context())
		if !errors.Is(err, engine.ErrUnavailable) {
			t.Fatalf("Health = %v, want ErrUnavailable", err)
		}
		if strings.Contains(err.Error(), "server-text") || !strings.Contains(err.Error(), "1.19") {
			t.Errorf("Health error = %q, want the parsed 1.19 and no server-supplied text", err)
		}
	})
	for _, v := range []string{"1.20.0", "1.20.1-dev", "2.0.0"} {
		t.Run(v, func(t *testing.T) {
			if err := mustNew(t, Config{URL: serve(v, 200).URL}).Health(t.Context()); err != nil {
				t.Fatalf("Health(%s) = %v", v, err)
			}
		})
	}
	t.Run("collections unauthorized", func(t *testing.T) {
		err := mustNew(t, Config{URL: serve("1.20.0", 401).URL}).Health(t.Context())
		if !errors.Is(err, engine.ErrUnavailable) {
			t.Fatalf("Health = %v, want ErrUnavailable", err)
		}
	})
	t.Run("unparsable version", func(t *testing.T) {
		err := mustNew(t, Config{URL: serve("banana", 200).URL}).Health(t.Context())
		if !errors.Is(err, engine.ErrUnavailable) {
			t.Fatalf("Health = %v, want ErrUnavailable", err)
		}
	})
}

// A non-2xx answer's body is drained (bounded) before close, so the
// keep-alive connection is reused rather than torn down on every refusal.
func TestDo_NonSuccessBodyIsDrainedForReuse(t *testing.T) {
	var mu sync.Mutex
	conns := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"status":{"error":"`+strings.Repeat("x", 8192)+`"}}`)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			mu.Lock()
			conns++
			mu.Unlock()
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	d := mustNew(t, Config{URL: srv.URL})
	for i := range 3 {
		if err := d.Health(t.Context()); err == nil {
			t.Fatalf("request %d: want an error on 400", i)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if conns != 1 {
		t.Errorf("opened %d connections for 3 refused requests, want 1 (body not drained)", conns)
	}
}
