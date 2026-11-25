package iface

import (
	"encoding/json"
	"testing"
)

func TestInstallationConstants(t *testing.T) {
	for got, want := range map[string]string{
		string(InferenceKindEmbed): "embed", string(InferenceKindRerank): "rerank", string(InferenceKindChat): "chat",
		string(InferenceBoundaryLocal): "local", string(InferenceBoundaryHosted): "hosted",
		string(EmbedInputQuery): "query", string(EmbedInputDocument): "document",
	} {
		if got != want {
			t.Errorf("constant = %q, want %q", got, want)
		}
	}
}

func TestInferenceProfile_JSONShape(t *testing.T) {
	b, _ := json.Marshal(InferenceProfile{ID: "ip_x", Endpoint: "embed", Kind: InferenceKindEmbed, Boundary: InferenceBoundaryLocal, ModelID: "m", Revision: "r"})
	const want = `{"id":"ip_x","endpoint":"embed","kind":"embed","boundary":"local","modelId":"m","revision":"r","normalize":false,"truncate":false}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

func TestRerankResult_JSONShape(t *testing.T) {
	b, _ := json.Marshal(RerankResult{Results: []RerankScore{{Index: 1, Score: 0.5}}, Profile: "ip_x", Usage: LLMUsage{InputTokens: 3}})
	const want = `{"results":[{"index":1,"score":0.5}],"profile":"ip_x","usage":{"inputTokens":3,"outputTokens":0}}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}
