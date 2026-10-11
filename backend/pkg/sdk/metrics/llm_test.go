package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestLLMMetrics_ClosedLabelSets(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatal(err)
	}
	c.RecordLLMRequest("anthropic", "fallback")
	c.RecordLLMRequest("cohere", "weird")
	c.RecordLLMRequest("tenant-123", "ok")
	if got := testutil.ToFloat64(c.llmRequests.WithLabelValues("anthropic", "fallback")); got != 1 {
		t.Fatalf("anthropic/fallback = %v", got)
	}
	// ADR-0002: unknown values collapse to "unknown", never a new series.
	if got := testutil.ToFloat64(c.llmRequests.WithLabelValues("unknown", "unknown")); got != 1 {
		t.Fatalf("unknown/unknown = %v", got)
	}
	if got := testutil.ToFloat64(c.llmRequests.WithLabelValues("unknown", "ok")); got != 1 {
		t.Fatalf("unknown/ok = %v", got)
	}
	c.RecordLLMTokens("openai", "input", 7)
	c.RecordLLMTokens("openai", "output", 3)
	c.RecordLLMTokens("openai", "sideways", 2)
	c.RecordLLMTokens("openai", "input", 0)
	c.RecordLLMTokens("openai", "input", -4)
	if got := testutil.ToFloat64(c.llmTokens.WithLabelValues("openai", "input")); got != 7 {
		t.Fatalf("openai/input = %v", got)
	}
	if got := testutil.ToFloat64(c.llmTokens.WithLabelValues("openai", "output")); got != 3 {
		t.Fatalf("openai/output = %v", got)
	}
	if got := testutil.ToFloat64(c.llmTokens.WithLabelValues("openai", "unknown")); got != 2 {
		t.Fatalf("openai/unknown = %v", got)
	}
	if n := testutil.CollectAndCount(c.llmRequests); n != 3 {
		t.Fatalf("request series = %d, want 3", n)
	}
}

func TestLLMMetrics_SIWCLinkOutcomes(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatal(err)
	}
	for _, o := range []string{"linked", "denied", "state_invalid", "exchange_failed", "id_token_invalid", "plan_disabled"} {
		c.RecordLLMSIWCLink(o)
		if got := testutil.ToFloat64(c.llmSIWCLinks.WithLabelValues(o)); got != 1 {
			t.Fatalf("%s = %v", o, got)
		}
	}
	c.RecordLLMSIWCLink("tenant-123")
	c.RecordLLMSIWCLink("")
	if got := testutil.ToFloat64(c.llmSIWCLinks.WithLabelValues("unknown")); got != 2 {
		t.Fatalf("unknown = %v", got)
	}
	if n := testutil.CollectAndCount(c.llmSIWCLinks, "orkestra_llm_siwc_link_total"); n != 7 {
		t.Fatalf("siwc series = %d, want 7", n)
	}
}

func TestLLMMetrics_NilCollectorIsSafe(t *testing.T) {
	var c *Collector
	c.RecordLLMSIWCLink("linked")
	c.RecordLLMRequest("openai", "ok")
	c.RecordLLMTokens("openai", "input", 1)
}
