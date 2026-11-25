package bench

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestReport_WriteText(t *testing.T) {
	r := &Report{
		Fixture: "smoke", Items: 40, Queries: 12, K: 10, Dimensions: 1024,
		EmbedModel: "BAAI/bge-m3", EmbedRevision: "abc", RerankModel: "BAAI/bge-reranker-v2-m3", RerankRevision: "def",
		IndexDuration: 2 * time.Second, ItemsPerSecond: 20, PeakRSSBytes: 300 << 20,
		Modes: []ModeResult{
			{Mode: "keyword", RecallAtK: 0.5, NDCGAtK: 0.4, P95: 12 * time.Millisecond, Samples: 36},
			{Mode: "dense", RecallAtK: 0.6, NDCGAtK: 0.5, P95: 30 * time.Millisecond, Samples: 36},
			{Mode: "hybrid", RecallAtK: 0.7, NDCGAtK: 0.6, P95: 40 * time.Millisecond, Samples: 36},
			{Mode: "hybrid+rerank", Skipped: "no rerank endpoint"},
		},
	}
	var b bytes.Buffer
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"recall@10", "ndcg@10", "p95", "keyword", "dense", "hybrid", "hybrid+rerank",
		"items/s", "BAAI/bge-m3@abc", "BAAI/bge-reranker-v2-m3@def", "skipped: no rerank endpoint", "drops out-of-scope", "result: OK"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	r.Modes[0].VisibilityViolations = 2
	if r.OK() {
		t.Error("OK with violations")
	}
	b.Reset()
	_ = r.WriteText(&b)
	if !strings.Contains(b.String(), "result: FAIL") {
		t.Error("missing FAIL line")
	}
}
