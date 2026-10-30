package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
)

const (
	embedSHA  = "5617a9f61b028005a4858fdac845db406aefb181"
	rerankSHA = "953dc6f6f85a1b2dbfca4c34a2796e2c5e0bbe3a"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func endpointsJSON() string {
	return `[
 {"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"` + embedSHA + `","pooling":"cls","normalize":true,"truncate":true,"queryPrefix":"q: ","documentPrefix":"d: "},
 {"name":"rerank","url":"http://tei-rerank:80/","kind":"rerank","boundary":"local","model":"BAAI/bge-reranker-v2-m3","revision":"` + rerankSHA + `","truncate":true,"apiKeyEnv":"RETRIEVAL_INFERENCE_K2"}
]`
}

func TestRun_UsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, envOf(nil), &out, &errb); code != 2 || !strings.Contains(errb.String(), "-fixture") {
		t.Fatalf("no fixture: code %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"-nope"}, envOf(nil), &out, &errb); code != 2 {
		t.Fatalf("unknown flag: code %d", code)
	}
}

func TestRun_NeedsQdrant(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"-fixture", "x.jsonl"}, envOf(nil), &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "QDRANT_URL is not set — start the overlay (./orkestra.sh retrieval up)") {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
}

func TestRun_RefusesBenchPrefix(t *testing.T) {
	var out, errb bytes.Buffer
	env := envOf(map[string]string{"QDRANT_URL": "http://qdrant:6333", "QDRANT_COLLECTION_PREFIX": "bench"})
	code := run([]string{"-fixture", "x.jsonl"}, env, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "QDRANT_COLLECTION_PREFIX") {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
}

func TestResolveEndpoint(t *testing.T) {
	env := envOf(map[string]string{"RETRIEVAL_INFERENCE_API_KEY": "shared", "RETRIEVAL_INFERENCE_K2": "second"})
	ep, err := resolveEndpoint(endpointsJSON(), "embed", env, false)
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "http://tei-embed:80" || ep.APIKey != "shared" || ep.Model != "BAAI/bge-m3" ||
		ep.Revision != embedSHA || !ep.Normalize || !ep.Truncate || ep.QueryPrefix != "q: " || ep.DocumentPrefix != "d: " {
		t.Fatalf("embed endpoint: %+v", ep)
	}
	rr, err := resolveEndpoint(endpointsJSON(), "rerank", env, false)
	if err != nil || rr.APIKey != "second" || rr.URL != "http://tei-rerank:80" {
		t.Fatalf("rerank endpoint: %+v, %v", rr, err)
	}
	if _, err := resolveEndpoint(endpointsJSON(), "nope", env, false); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("unknown name: %v", err)
	}
	if _, err := resolveEndpoint(`{not json`, "embed", env, false); err == nil {
		t.Fatal("malformed JSON must fail")
	}
	// apiKeyEnv naming an unset variable drops the endpoint; the error says why.
	if _, err := resolveEndpoint(endpointsJSON(), "rerank", envOf(nil), false); err == nil {
		t.Fatal("unset apiKeyEnv must fail")
	}
}

// A served model that differs from the declared entry stops the run (P22).
func TestRun_DeclaredModelMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model_id":"other/model","model_sha":"` + embedSHA + `"}`))
	}))
	defer srv.Close()
	old := engineFactory
	engineFactory = func(string, string) (engine.Engine, error) { return enginefake.New(), nil }
	defer func() { engineFactory = old }()

	eps := `[{"name":"embed","url":"` + srv.URL + `","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"` + embedSHA + `","pooling":"cls"}]`
	env := envOf(map[string]string{"QDRANT_URL": "http://qdrant:6333", "RETRIEVAL_INFERENCE_ENDPOINTS": eps})
	var out, errb bytes.Buffer
	code := run([]string{"-fixture", "../../internal/core/retrieval/bench/testdata/smoke-it-en.jsonl", "-rerank-endpoint", ""}, env, &out, &errb)
	want := "embed endpoint serves other/model@" + embedSHA + ", declared BAAI/bge-m3@" + embedSHA
	if code != 1 || !strings.Contains(errb.String(), want) {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
}
