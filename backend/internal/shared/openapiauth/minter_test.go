package openapiauth

import (
	"context"
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

func TestMinter_MintAndCache(t *testing.T) {
	var calls atomic.Int32
	var gotAuth, gotPath, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"token":   "eyJtest.jwt.signature",
			"ttl":     3600,
		})
	}))
	defer srv.Close()

	m := NewMinter(Config{
		AccountEmail: "ops@example.com",
		APIKey:       "keysecret",
		OAuthBaseURL: srv.URL,
		Scopes:       []string{"GET:company.openapi.com/IT-start/*"},
		Tag:          "company",
	}, nil, nil, nil)

	tok, err := m.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	if tok != "eyJtest.jwt.signature" {
		t.Fatalf("unexpected token: %q", tok)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 mint call, got %d", got)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("expected Basic auth, got %q", gotAuth)
	}
	if gotPath != "/token" {
		t.Fatalf("expected POST /token, got %q", gotPath)
	}
	if !strings.Contains(gotBody, "GET:company.openapi.com/IT-start/*") {
		t.Fatalf("expected scope in body, got %q", gotBody)
	}

	// Second call must hit the in-process cache.
	tok2, err := m.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if tok2 != tok {
		t.Fatalf("cache miss: got %q want %q", tok2, tok)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected still 1 mint call after cache hit, got %d", got)
	}

	// Invalidate forces a fresh mint on next call.
	m.Invalidate(context.Background())
	if _, err := m.Token(context.Background()); err != nil {
		t.Fatalf("Token after invalidate: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 mint calls after invalidate, got %d", got)
	}
}

func TestMinter_DataEnvelopeShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"token": "envelope.jwt",
				"ttl":   600,
			},
		})
	}))
	defer srv.Close()

	m := NewMinter(Config{
		AccountEmail: "a@b.it",
		APIKey:       "k",
		OAuthBaseURL: srv.URL,
		Scopes:       []string{"GET:x"},
		Tag:          "billing",
	}, nil, nil, nil)

	tok, err := m.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "envelope.jwt" {
		t.Fatalf("expected envelope token, got %q", tok)
	}
}

func TestMinter_UpstreamAuthRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"message":"Wrong Auth Data Provided","error":120}`))
	}))
	defer srv.Close()

	m := NewMinter(Config{
		AccountEmail: "a@b.it",
		APIKey:       "wrong",
		OAuthBaseURL: srv.URL,
		Scopes:       []string{"GET:x"},
		Tag:          "company",
	}, nil, nil, nil)

	_, err := m.Token(context.Background())
	if !errors.Is(err, ErrUpstreamAuth) {
		t.Fatalf("expected ErrUpstreamAuth, got %v", err)
	}
}

func TestMinter_MissingCredentials(t *testing.T) {
	m := NewMinter(Config{Tag: "company"}, nil, nil, nil)
	_, err := m.Token(context.Background())
	if !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials, got %v", err)
	}
}

// fakeCache is an in-memory cache satisfying the openapiauth.Cache interface.
type fakeCache struct {
	store map[string]string
	hits  int
	sets  int
}

func newFakeCache() *fakeCache { return &fakeCache{store: make(map[string]string)} }

func (f *fakeCache) Get(_ context.Context, key string) (string, error) {
	v, ok := f.store[key]
	if !ok {
		return "", nil
	}
	f.hits++
	return v, nil
}

func (f *fakeCache) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	f.sets++
	f.store[key] = value.(string)
	return nil
}

func TestMinter_RedisCacheRoundtrip(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "shared.jwt", "ttl": 3600})
	}))
	defer srv.Close()

	cache := newFakeCache()
	cfg := Config{AccountEmail: "a@b.it", APIKey: "k", OAuthBaseURL: srv.URL, Scopes: []string{"GET:x"}, Tag: "company"}

	m1 := NewMinter(cfg, cache, nil, nil)
	tok, err := m1.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "shared.jwt" {
		t.Fatalf("token: %q", tok)
	}
	if cache.sets != 1 {
		t.Fatalf("expected 1 cache set, got %d", cache.sets)
	}

	// A second minter instance (simulating a different replica) should
	// fetch from Redis without minting again.
	m2 := NewMinter(cfg, cache, nil, nil)
	tok2, err := m2.Token(context.Background())
	if err != nil {
		t.Fatalf("Token m2: %v", err)
	}
	if tok2 != "shared.jwt" {
		t.Fatalf("m2 token: %q", tok2)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected only 1 mint across replicas, got %d", got)
	}
	if cache.hits != 1 {
		t.Fatalf("expected 1 cache hit on m2, got %d", cache.hits)
	}
}
