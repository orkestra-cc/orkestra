package installation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/providers/teitest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var baseCfg = teitest.Config{ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Dimensions: 1024, MaxInputLength: 8192, MaxClientBatchSize: 32}

func embedEP(url string) Endpoint {
	return Endpoint{
		Name: "embed", URL: url, Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal,
		Model: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Normalize: true, Truncate: true,
	}
}

func rerankEP(url string) Endpoint {
	return Endpoint{
		Name: "rerank", URL: url, Kind: iface.InferenceKindRerank, Boundary: iface.InferenceBoundaryLocal,
		Model: "BAAI/bge-reranker-v2-m3", Revision: "def456", Truncate: true,
	}
}

func newSet(t *testing.T, clk *clock, eps ...Endpoint) *Set {
	t.Helper()
	return NewSet(eps, providers.NewInstallationHTTPClient(providers.TransportConfig{}), Options{Now: clk.Now})
}

func TestSet_VerifiesAndMeasuresDimensions(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	v, err := set.Verify(context.Background(), "embed")
	if err != nil {
		t.Fatal(err)
	}
	p := v.Profile
	if p.ID != embedID || p.Dimensions != 1024 || p.MaxInputTokens != 8192 || p.Endpoint != "embed" || p.Boundary != iface.InferenceBoundaryLocal {
		t.Fatalf("profile = %+v", p)
	}
	if v.BatchSize != 32 {
		t.Fatalf("batch = %d", v.BatchSize)
	}
	calls := srv.EmbedCalls()
	if len(calls) != 1 || len(calls[0].Inputs) != 1 || calls[0].Inputs[0] != ProbeText || !calls[0].Normalize || !calls[0].Truncate {
		t.Fatalf("embed calls = %+v", calls)
	}
}

func TestSet_CachesUntilVerifyInterval(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	clk := newClock()
	set := newSet(t, clk, embedEP(srv.URL))
	ctx := context.Background()
	for range 2 {
		if _, err := set.Verify(ctx, "embed"); err != nil {
			t.Fatal(err)
		}
	}
	if n := srv.InfoCalls(); n != 1 {
		t.Fatalf("info calls = %d", n)
	}
	clk.Advance(61 * time.Second)
	if _, err := set.Verify(ctx, "embed"); err != nil {
		t.Fatal(err)
	}
	if n := srv.InfoCalls(); n != 2 {
		t.Fatalf("info calls = %d", n)
	}
}

func TestSet_MismatchCases(t *testing.T) {
	with := func(mut func(*teitest.Config)) teitest.Config {
		c := baseCfg
		mut(&c)
		return c
	}
	cases := map[string]struct {
		cfg   teitest.Config
		field string
	}{
		"model":       {with(func(c *teitest.Config) { c.ModelID = "BAAI/other" }), "model_id"},
		"revision":    {with(func(c *teitest.Config) { c.Revision = "zzz" }), "model_sha"},
		"no revision": {with(func(c *teitest.Config) { c.Revision = "" }), "model_sha"},
		"pooling":     {with(func(c *teitest.Config) { c.Pooling = "mean" }), "pooling"},
		"kind":        {with(func(c *teitest.Config) { c.Kind = "rerank" }), "model_type"},
		"dimensions":  {with(func(c *teitest.Config) { c.Dimensions = 768 }), "dimensions"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := teitest.New(t, baseCfg)
			clk := newClock()
			set := newSet(t, clk, embedEP(srv.URL))
			if _, err := set.Verify(context.Background(), "embed"); err != nil {
				t.Fatal(err)
			}
			srv.SetConfig(tc.cfg)
			clk.Advance(61 * time.Second)
			_, err := set.Verify(context.Background(), "embed")
			if !errors.Is(err, iface.ErrLLMProfileMismatch) || !strings.HasSuffix(err.Error(), ": "+tc.field) {
				t.Fatalf("err = %v, want mismatch on %s", err, tc.field)
			}
		})
	}
}

func TestSet_RerankRevisionMismatch(t *testing.T) {
	cfg := teitest.Config{ModelID: "BAAI/bge-reranker-v2-m3", Revision: "def456", Kind: "rerank"}
	srv := teitest.New(t, cfg)
	clk := newClock()
	set := newSet(t, clk, rerankEP(srv.URL))
	if _, err := set.Verify(context.Background(), "rerank"); err != nil {
		t.Fatal(err)
	}
	cfg.Revision = "zzz"
	srv.SetConfig(cfg)
	clk.Advance(61 * time.Second)
	_, err := set.Verify(context.Background(), "rerank")
	if !errors.Is(err, iface.ErrLLMProfileMismatch) || !strings.HasSuffix(err.Error(), ": model_sha") {
		t.Fatalf("err = %v", err)
	}
}

func TestSet_EndpointWithoutAdapterIsNotConfigured(t *testing.T) {
	ep := embedEP("")
	set := newSet(t, newClock(), ep)
	if _, err := set.Verify(context.Background(), "embed"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestSet_DimensionChangeSurvivesInvalidate(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	if _, err := set.Verify(context.Background(), "embed"); err != nil {
		t.Fatal(err)
	}
	srv.SetConfig(teitest.Config{ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Dimensions: 768})
	set.Invalidate("embed")
	if _, err := set.Verify(context.Background(), "embed"); !errors.Is(err, iface.ErrLLMProfileMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestSet_UnreachableIsCachedForTheBackoff(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	clk := newClock()
	set := newSet(t, clk, embedEP(srv.URL))
	ctx := context.Background()
	srv.Fail("/info", 0, 503)
	if _, err := set.Verify(ctx, "embed"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	clk.Advance(time.Second)
	if _, err := set.Verify(ctx, "embed"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if n := srv.InfoCalls(); n != 1 {
		t.Fatalf("info calls = %d", n)
	}
	srv.Fail("/info", 0, 0)
	clk.Advance(6 * time.Second)
	if _, err := set.Verify(ctx, "embed"); err != nil {
		t.Fatal(err)
	}
	if n := srv.InfoCalls(); n != 2 {
		t.Fatalf("info calls = %d", n)
	}
}

func TestSet_ConcurrentVerifyRunsOnce(t *testing.T) {
	cfg := baseCfg
	cfg.InfoDelay = 100 * time.Millisecond
	srv := teitest.New(t, cfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := set.Verify(context.Background(), "embed")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := srv.InfoCalls(); n != 1 {
		t.Fatalf("info calls = %d", n)
	}
}

func TestSet_CallerCancelDoesNotPoisonTheCache(t *testing.T) {
	cfg := baseCfg
	cfg.InfoDelay = 200 * time.Millisecond
	srv := teitest.New(t, cfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := set.Verify(ctx, "embed"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	time.Sleep(600 * time.Millisecond) // the detached flight finishes and fills the cache
	if _, err := set.Verify(context.Background(), "embed"); err != nil {
		t.Fatal(err)
	}
	if n := srv.InfoCalls(); n != 1 {
		t.Fatalf("info calls = %d", n)
	}
}

func TestSet_UnknownAndChatAreNotConfigured(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	chat := Endpoint{Name: "chat", URL: srv.URL, Kind: iface.InferenceKindChat, Boundary: iface.InferenceBoundaryLocal, Model: "m", Revision: "r"}
	set := newSet(t, newClock(), embedEP(srv.URL), chat)
	for _, name := range []string{"nope", "chat"} {
		if _, err := set.Verify(context.Background(), name); !errors.Is(err, iface.ErrLLMNotConfigured) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if n := srv.InfoCalls(); n != 0 {
		t.Fatalf("info calls = %d", n)
	}
}

func TestSet_RerankVerifiesWithoutProbe(t *testing.T) {
	srv := teitest.New(t, teitest.Config{ModelID: "BAAI/bge-reranker-v2-m3", Revision: "def456", Kind: "rerank"})
	set := newSet(t, newClock(), rerankEP(srv.URL))
	v, err := set.Verify(context.Background(), "rerank")
	if err != nil {
		t.Fatal(err)
	}
	if v.Profile.Dimensions != 0 || v.Profile.ID != rerankID || len(srv.EmbedCalls()) != 0 {
		t.Fatalf("v = %+v, embeds = %d", v.Profile, len(srv.EmbedCalls()))
	}
}

func TestSet_InvalidateForcesReverify(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	ctx := context.Background()
	if _, err := set.Verify(ctx, "embed"); err != nil {
		t.Fatal(err)
	}
	set.Invalidate("embed")
	if _, err := set.Verify(ctx, "embed"); err != nil {
		t.Fatal(err)
	}
	if n := srv.InfoCalls(); n != 2 {
		t.Fatalf("info calls = %d", n)
	}
}

func TestSet_VerifyReturnsACopy(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	a, _ := set.Verify(context.Background(), "embed")
	a.Profile.Dimensions = 1
	b, _ := set.Verify(context.Background(), "embed")
	if b.Profile.Dimensions != 1024 {
		t.Fatalf("cache was mutated: %d", b.Profile.Dimensions)
	}
}

func TestSet_LookupHelpers(t *testing.T) {
	srv := teitest.New(t, baseCfg)
	set := newSet(t, newClock(), embedEP(srv.URL))
	if ep, ok := set.ByProfile(embedID); !ok || ep.Name != "embed" {
		t.Fatalf("ByProfile = %v %v", ep, ok)
	}
	if _, ok := set.ByProfile("ip_" + "0000000000000000000000000000000000000000000000000000000000000000"); ok {
		t.Fatal("unknown profile found")
	}
	if set.HasKind(iface.InferenceKindRerank) || !set.HasKind(iface.InferenceKindEmbed) {
		t.Fatal("HasKind")
	}
	if set.Len() != 1 || set.Adapter("embed") == nil || set.Adapter("nope") != nil {
		t.Fatal("Len/Adapter")
	}
	if _, ok := set.Endpoint("embed"); !ok {
		t.Fatal("Endpoint")
	}
}
