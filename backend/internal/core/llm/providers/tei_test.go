package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/providers/teitest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func teiClient() *http.Client {
	return NewInstallationHTTPClient(TransportConfig{Timeout: 5 * time.Second})
}

func newTEIT(t *testing.T, cfg teitest.Config, key string) (*TEI, *teitest.Server) {
	t.Helper()
	srv := teitest.New(t, cfg)
	tei, err := NewTEI(srv.URL, key, teiClient())
	if err != nil {
		t.Fatal(err)
	}
	return tei, srv
}

func TestTEI_InfoParsesIdentity(t *testing.T) {
	tei, srv := newTEIT(t, teitest.Config{ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", MaxInputLength: 8192, MaxClientBatchSize: 32}, "")
	info, err := tei.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.ModelID != "BAAI/bge-m3" || info.ModelSHA == nil || *info.ModelSHA != "abc123" ||
		info.ModelType.Embedding == nil || info.ModelType.Embedding.Pooling != "cls" ||
		len(info.ModelType.Reranker) != 0 || info.MaxInputLength != 8192 || info.MaxClientBatchSize != 32 {
		t.Fatalf("info = %+v", info)
	}
	srv.SetConfig(teitest.Config{ModelID: "m", Revision: ""})
	if info, err = tei.Info(context.Background()); err != nil || info.ModelSHA != nil {
		t.Fatalf("null revision: %+v, %v", info, err)
	}
	srv.SetConfig(teitest.Config{ModelID: "r", Kind: "rerank"})
	if info, err = tei.Info(context.Background()); err != nil || info.ModelType.Embedding != nil || len(info.ModelType.Reranker) == 0 {
		t.Fatalf("rerank: %+v, %v", info, err)
	}
}

func TestTEI_EmbedSplitsAtMaxClientBatchSize(t *testing.T) {
	tei, srv := newTEIT(t, teitest.Config{ModelID: "m"}, "")
	inputs := make([]string, 70)
	for i := range inputs {
		inputs[i] = strings.Repeat("x", i+1)
	}
	out, err := tei.Embed(context.Background(), inputs, TEIEmbedOptions{Normalize: true, Truncate: true, BatchSize: 32})
	if err != nil {
		t.Fatal(err)
	}
	calls := srv.EmbedCalls()
	if len(calls) != 3 || len(calls[0].Inputs) != 32 || len(calls[1].Inputs) != 32 || len(calls[2].Inputs) != 6 {
		t.Fatalf("calls = %d", len(calls))
	}
	for _, c := range calls {
		if !c.Normalize || !c.Truncate {
			t.Fatalf("flags lost: %+v", c)
		}
	}
	if len(out.Vectors) != 70 {
		t.Fatalf("vectors = %d", len(out.Vectors))
	}
	for i, v := range out.Vectors {
		if v[0] != float32(i+1) {
			t.Fatalf("vector %d = %v", i, v)
		}
	}
	if out.Usage.InputTokens != 2485 {
		t.Fatalf("tokens = %d, want 2485", out.Usage.InputTokens)
	}
}

func TestTEI_EmbedWithoutComputeTokensEstimates(t *testing.T) {
	tei, _ := newTEIT(t, teitest.Config{ModelID: "m", OmitComputeTokens: true}, "")
	out, err := tei.Embed(context.Background(), []string{"abcd", "ef"}, TEIEmbedOptions{})
	if err != nil || out.Usage.InputTokens != 2 {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}

func TestTEI_RerankMergesBatchesWithGlobalIndices(t *testing.T) {
	tei, srv := newTEIT(t, teitest.Config{ModelID: "r", Kind: "rerank"}, "")
	texts := make([]string, 40)
	longest := 0
	for i := range texts {
		texts[i] = strings.Repeat("y", (i*7)%40+1)
		if len(texts[i]) > len(texts[longest]) {
			longest = i
		}
	}
	scores, usage, err := tei.Rerank(context.Background(), "q", texts, TEIRerankOptions{BatchSize: 16})
	if err != nil {
		t.Fatal(err)
	}
	calls := srv.RerankCalls()
	if len(calls) != 3 || len(calls[0].Texts) != 16 || len(calls[1].Texts) != 16 || len(calls[2].Texts) != 8 {
		t.Fatalf("calls = %d", len(calls))
	}
	for _, c := range calls {
		if c.Query != "q" {
			t.Fatalf("query = %q", c.Query)
		}
	}
	if len(scores) != 40 {
		t.Fatalf("scores = %d", len(scores))
	}
	seen := map[int]bool{}
	for k, s := range scores {
		if s.Index < 0 || s.Index > 39 || seen[s.Index] {
			t.Fatalf("bad index %d", s.Index)
		}
		seen[s.Index] = true
		if k > 0 && scores[k-1].Score < s.Score {
			t.Fatalf("not sorted at %d", k)
		}
	}
	if len(texts[scores[0].Index]) != len(texts[longest]) {
		t.Fatalf("top = %d, want a longest text", scores[0].Index)
	}
	want := 0
	for _, tx := range texts {
		want += len(tx)
	}
	want += 40 // len("q") * 40
	if usage.InputTokens != want {
		t.Fatalf("tokens = %d, want %d", usage.InputTokens, want)
	}
}

func TestTEI_FailedLaterBatchReportsCompletedUsage(t *testing.T) {
	tei, srv := newTEIT(t, teitest.Config{ModelID: "m"}, "")
	srv.Fail("/embed", 1, 503)
	inputs := make([]string, 40)
	for i := range inputs {
		inputs[i] = "ab"
	}
	out, err := tei.Embed(context.Background(), inputs, TEIEmbedOptions{BatchSize: 32})
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if out == nil || out.Usage.InputTokens != 64 {
		t.Fatalf("out = %+v", out)
	}
}

func TestTEI_DimensionChangeBetweenBatchesIsProfileMismatch(t *testing.T) {
	tei, srv := newTEIT(t, teitest.Config{ModelID: "m", Dimensions: 4}, "")
	srv.ChangeDimensionsAfter(1, 8)
	inputs := make([]string, 40)
	for i := range inputs {
		inputs[i] = "ab"
	}
	out, err := tei.Embed(context.Background(), inputs, TEIEmbedOptions{BatchSize: 32})
	if !errors.Is(err, iface.ErrLLMProfileMismatch) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if out == nil || out.Usage.InputTokens != 64 {
		t.Fatalf("completed usage lost: %+v", out)
	}
}

func TestTEI_SendsBearerOnlyWhenKeySet(t *testing.T) {
	ctx := context.Background()
	tei, srv := newTEIT(t, teitest.Config{ModelID: "m", APIKey: "k1"}, "k1")
	if _, err := tei.Info(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tei.Embed(ctx, []string{"a"}, TEIEmbedOptions{}); err != nil {
		t.Fatal(err)
	}
	rsrv := teitest.New(t, teitest.Config{ModelID: "r", Kind: "rerank", APIKey: "k1"})
	rt, _ := NewTEI(rsrv.URL, "k1", teiClient())
	if _, _, err := rt.Rerank(ctx, "q", []string{"a"}, TEIRerankOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, h := range append(srv.AuthHeaders(), rsrv.AuthHeaders()...) {
		if h != "Bearer k1" {
			t.Fatalf("auth = %q", h)
		}
	}
	nokey, _ := NewTEI(srv.URL, "", teiClient())
	if _, err := nokey.Info(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	open := teitest.New(t, teitest.Config{ModelID: "m"})
	ot, _ := NewTEI(open.URL, "", teiClient())
	if _, err := ot.Info(ctx); err != nil {
		t.Fatal(err)
	}
	for _, h := range open.AuthHeaders() {
		if h != "" {
			t.Fatalf("auth = %q, want empty", h)
		}
	}
}

func TestTEI_ClassifiesStatusesWithoutProviderText(t *testing.T) {
	for status, want := range map[int]error{
		401: ErrUnauthorized, 403: ErrUnauthorized,
		400: iface.ErrLLMInvalidRequest, 413: iface.ErrLLMInvalidRequest, 422: iface.ErrLLMInvalidRequest,
		404: iface.ErrLLMProviderUnavailable, 408: iface.ErrLLMProviderUnavailable, 424: iface.ErrLLMProviderUnavailable,
		429: iface.ErrLLMProviderUnavailable, 503: iface.ErrLLMProviderUnavailable,
	} {
		tei, srv := newTEIT(t, teitest.Config{ModelID: "m"}, "")
		srv.Fail("/embed", 0, status)
		_, err := tei.Embed(context.Background(), []string{"a"}, TEIEmbedOptions{})
		if !errors.Is(err, want) {
			t.Errorf("status %d: err = %v, want %v", status, err, want)
		}
		if (status == 401 || status == 403) && !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Errorf("status %d: not provider-unavailable: %v", status, err)
		}
		if err != nil && strings.Contains(err.Error(), "injected") {
			t.Errorf("status %d leaks provider text: %v", status, err)
		}
	}
}

func TestTEI_RefusesMalformedResponses(t *testing.T) {
	serve := func(path, body string) *TEI {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		tei, _ := NewTEI(srv.URL, "", teiClient())
		return tei
	}
	ctx := context.Background()
	cases := map[string]func() error{
		"embed count mismatch": func() error {
			_, err := serve("/embed", `[[0.1]]`).Embed(ctx, []string{"a", "b"}, TEIEmbedOptions{})
			return err
		},
		"embed empty vector": func() error {
			_, err := serve("/embed", `[[0.1],[]]`).Embed(ctx, []string{"a", "b"}, TEIEmbedOptions{})
			return err
		},
		"rerank duplicate index": func() error {
			_, _, err := serve("/rerank", `[{"index":0,"score":0.5},{"index":0,"score":0.5}]`).Rerank(ctx, "q", []string{"a", "b"}, TEIRerankOptions{})
			return err
		},
		"rerank out of range": func() error {
			_, _, err := serve("/rerank", `[{"index":5,"score":1}]`).Rerank(ctx, "q", []string{"a"}, TEIRerankOptions{})
			return err
		},
		"info without model_id": func() error {
			_, err := serve("/info", `{"model_type":{"embedding":{"pooling":"cls"}}}`).Info(ctx)
			return err
		},
	}
	for name, run := range cases {
		if err := run(); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestNewTEI_RequiresClientAndURL(t *testing.T) {
	if _, err := NewTEI("http://tei:80", "", nil); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := NewTEI("", "", teiClient()); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
}
