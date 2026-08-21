package metrics

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ADR-0017 D8: tier is the ONLY label. Collection names, UUIDs,
// configuration values and error strings never become labels.
func TestTokenSweepLabelsAreClosed(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordTokenSweep("operator", 5000, 900*time.Millisecond)
	c.RecordTokenSweep("client", 12, 3*time.Millisecond)
	c.RecordTokenSweep("operator_refresh_tokens", 1, time.Millisecond) // a collection name is not a tier
	c.RecordTokenSweep("", 1, time.Millisecond)

	// "operator_refresh_tokens" and "" are both out-of-allowlist values, so
	// BOTH collapse to "unknown" — neither one leaks into "operator" just
	// because it shares a prefix or looks tier-shaped. ADR-0017 D8.
	for tier, want := range map[string]float64{"operator": 5000, "client": 12, "unknown": 2} {
		if got := testutil.ToFloat64(c.tokenSweepDeleted.WithLabelValues(tier)); got != want {
			t.Errorf("tier=%q deleted = %v, want %v", tier, got, want)
		}
	}
}

// TestTokenSweepDurationRecordedEvenWhenNothingDeleted pins a requirement
// TestTokenSweepLabelsAreClosed does not cover: the duration histogram
// must observe every completed batch, including one that deleted nothing.
// RecordTokenSweep's Observe call sits outside the "deleted > 0" branch on
// purpose — a sweep that scans a huge eligible range and finds nothing to
// delete is still slow, and that is exactly the pathology operators need
// orkestra_auth_token_sweep_duration_seconds to reveal. If a future edit
// moved Observe inside the deleted>0 guard (or dropped it), an empty batch
// would go untimed and a stalled-but-quiet sweep could hide as silence
// instead of showing up as a slow zero. This test would catch that: it
// records a zero-deletion batch and asserts the histogram still gained a
// sample for that tier.
func TestTokenSweepDurationRecordedEvenWhenNothingDeleted(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordTokenSweep("operator", 0, 250*time.Millisecond)

	hist, ok := c.tokenSweepDuration.WithLabelValues("operator").(prometheus.Histogram)
	if !ok {
		t.Fatalf("tokenSweepDuration observer does not implement prometheus.Histogram")
	}
	var m dto.Metric
	if err := hist.Write(&m); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := m.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("duration sample count for tier=%q (deleted=0) = %d, want 1 — an empty batch must still be timed", "operator", got)
	}
}

func TestTokenSweepBacklogGauge(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.SetTokenSweepBacklog("operator", 1_200_000)
	if got := testutil.ToFloat64(c.tokenSweepBacklog.WithLabelValues("operator")); got != 1_200_000 {
		t.Errorf("backlog = %v, want 1200000", got)
	}
	c.SetTokenSweepBacklog("operator", 0)
	if got := testutil.ToFloat64(c.tokenSweepBacklog.WithLabelValues("operator")); got != 0 {
		t.Errorf("backlog after drain = %v, want 0 — operators watch this reach zero to know the drain finished", got)
	}
}

// TestTokenSweepBacklogNeverGoesNegative pins SetTokenSweepBacklog's clamp.
// remaining is normally derived from a local decrement of the seeded
// count, and a race between two decrements (or the seeded count being
// slightly stale, since it is an estimate) can undershoot zero. The gauge
// must never publish a negative backlog to operators.
func TestTokenSweepBacklogNeverGoesNegative(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.SetTokenSweepBacklog("operator", -5) // a local decrement can undershoot; the gauge must not go negative
	if got := testutil.ToFloat64(c.tokenSweepBacklog.WithLabelValues("operator")); got != 0 {
		t.Errorf("backlog after negative input = %v, want 0", got)
	}
}

func TestNilCollectorSweepRecordersAreSafe(t *testing.T) {
	var c *Collector
	c.RecordTokenSweep("operator", 1, time.Second)
	c.SetTokenSweepBacklog("operator", 1)
}
