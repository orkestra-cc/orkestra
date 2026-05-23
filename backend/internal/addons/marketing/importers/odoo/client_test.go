package odoo

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{
		BaseURL:        srv.URL,
		Database:       "test_db",
		APIKey:         "test_key",
		HTTPClient:     srv.Client(),
		RetryAttempts:  3,
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func TestNewClient_Validates(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing base url", Config{Database: "db", APIKey: "k"}},
		{"missing database", Config{BaseURL: "http://x", APIKey: "k"}},
		{"missing api key", Config{BaseURL: "http://x", Database: "db"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewClient(tc.cfg); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSearchRead_AuthHeaders(t *testing.T) {
	var (
		gotAPIKey  string
		gotDBName  string
		gotContent string
	)
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		gotDBName = r.Header.Get("X-DB-Name")
		gotContent = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(SearchReadResult{Records: []map[string]any{}, TotalCount: 0})
	}))

	if _, err := c.SearchRead("res.partner", SearchReadOpts{Limit: 50}); err != nil {
		t.Fatalf("SearchRead: %v", err)
	}
	if gotAPIKey != "test_key" {
		t.Errorf("X-API-Key = %q", gotAPIKey)
	}
	if gotDBName != "test_db" {
		t.Errorf("X-DB-Name = %q", gotDBName)
	}
	if gotContent != "application/json" {
		t.Errorf("Content-Type = %q", gotContent)
	}
}

func TestSearchRead_PaginationOffsetLimit(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		var opts SearchReadOpts
		_ = json.Unmarshal(body, &opts)
		if opts.Offset != 100 {
			t.Errorf("expected offset=100, got %d", opts.Offset)
		}
		if opts.Limit != 50 {
			t.Errorf("expected limit=50, got %d", opts.Limit)
		}
		_ = json.NewEncoder(w).Encode(SearchReadResult{Records: []map[string]any{}, TotalCount: 0})
	}))
	if _, err := c.SearchRead("res.partner", SearchReadOpts{Offset: 100, Limit: 50}); err != nil {
		t.Fatalf("SearchRead: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestSearchRead_AuthError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	_, err := c.SearchRead("res.partner", SearchReadOpts{})
	if !errors.Is(err, ErrOdooAuth) {
		t.Fatalf("expected ErrOdooAuth, got %v", err)
	}
}

func TestSearchRead_NotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown model", http.StatusNotFound)
	}))
	_, err := c.SearchRead("nonexistent.model", SearchReadOpts{})
	if !errors.Is(err, ErrOdooNotFound) {
		t.Fatalf("expected ErrOdooNotFound, got %v", err)
	}
}

func TestSearchRead_RetryOn5xx_RecoversIfTransient(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			http.Error(w, "transient", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(SearchReadResult{Records: []map[string]any{{"id": float64(1)}}, TotalCount: 1})
	}))
	res, err := c.SearchRead("res.partner", SearchReadOpts{})
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if len(res.Records) != 1 || res.TotalCount != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (1 fail + 1 success)", calls)
	}
}

func TestSearchRead_RetryExhaustion(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "still broken", http.StatusInternalServerError)
	}))
	_, err := c.SearchRead("res.partner", SearchReadOpts{})
	if !errors.Is(err, ErrOdooServer) {
		t.Fatalf("expected ErrOdooServer, got %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 attempts", calls)
	}
}

func TestSearchRead_TrimTrailingSlashInBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(SearchReadResult{})
	}))
	defer srv.Close()
	c, err := NewClient(Config{
		BaseURL:    srv.URL + "/",
		Database:   "db",
		APIKey:     "k",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.SearchRead("res.partner", SearchReadOpts{}); err != nil {
		t.Fatalf("SearchRead: %v", err)
	}
	if !strings.HasPrefix(gotPath, "/api/v2/res.partner/search_read") {
		t.Fatalf("path = %q (expected /api/v2/res.partner/search_read)", gotPath)
	}
}
