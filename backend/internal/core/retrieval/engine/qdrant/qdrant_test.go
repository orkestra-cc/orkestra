package qdrant

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/enginetest"
)

// liveConfig gates the live tests on QDRANT_TEST_URL: without it they skip
// and prove nothing about the driver.
func liveConfig(t *testing.T) Config {
	t.Helper()
	u := os.Getenv("QDRANT_TEST_URL")
	if u == "" {
		t.Skip("set QDRANT_TEST_URL (and QDRANT_TEST_API_KEY) to run the Qdrant conformance suite")
	}
	return Config{URL: u, APIKey: os.Getenv("QDRANT_TEST_API_KEY")}
}

func newLive(t *testing.T, cfg Config) *Driver {
	t.Helper()
	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// waitReady polls Health for up to 30s: the CI service container may still
// be starting when the tests begin.
func waitReady(t *testing.T, d *Driver) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		err := d.Health(ctx)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Qdrant not ready after 30s; last Health error: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestConformance(t *testing.T) {
	cfg := liveConfig(t)
	waitReady(t, newLive(t, cfg))
	enginetest.Run(t, func(t *testing.T) engine.Engine { return newLive(t, cfg) })
}

func TestLive_WrongAPIKeyIsUnavailable(t *testing.T) {
	cfg := liveConfig(t)
	if cfg.APIKey == "" {
		t.Skip("set QDRANT_TEST_API_KEY: a server without an API key accepts any key")
	}
	waitReady(t, newLive(t, cfg))

	wrong := cfg
	wrong.APIKey = "wrong-" + cfg.APIKey
	err := newLive(t, wrong).Health(t.Context())
	if !errors.Is(err, engine.ErrUnavailable) {
		t.Fatalf("Health with a wrong API key = %v, want engine.ErrUnavailable", err)
	}
	secrets := map[string]string{"the URL": cfg.URL, "the API key": cfg.APIKey}
	if u, perr := url.Parse(cfg.URL); perr == nil && u.Host != "" {
		secrets["the host"] = u.Host
	}
	for what, s := range secrets {
		// The message is not printed: it would carry what it leaks.
		if strings.Contains(err.Error(), s) {
			t.Errorf("Health error message contains %s", what)
		}
	}
}
