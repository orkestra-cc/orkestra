package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ADR-0017 D8 freezes these label sets. The anomaly kind is a closed
// allowlist of exactly two values: anything else collapses to "unknown"
// rather than minting a new time series, because an unbounded label is
// how a Prometheus cardinality explosion starts and history breaks when
// labels change.
func TestSessionAnchorAnomalyLabelsAreClosed(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordSessionAnchorAnomaly("missing")
	c.RecordSessionAnchorAnomaly("zero_timestamp")
	c.RecordSessionAnchorAnomaly("sess-9f3a-uuid-leak")
	c.RecordSessionAnchorAnomaly("")

	for kind, want := range map[string]float64{"missing": 1, "zero_timestamp": 1, "unknown": 2} {
		got := testutil.ToFloat64(c.sessionAnchorAnomalies.WithLabelValues(kind))
		if got != want {
			t.Errorf("kind=%q counter = %v, want %v", kind, got, want)
		}
	}
}

func TestSessionCapCountersAreUnlabelled(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordSessionCapExpiry()
	c.RecordSessionCapExpiry()
	c.RecordSessionCapEventFailure()

	if got := testutil.ToFloat64(c.sessionCapExpiries); got != 2 {
		t.Errorf("cap expiries = %v, want 2", got)
	}
	if got := testutil.ToFloat64(c.sessionCapEventFailures); got != 1 {
		t.Errorf("cap event failures = %v, want 1", got)
	}
}

func TestNilCollectorRecordersAreSafe(t *testing.T) {
	var c *Collector
	c.RecordSessionCapExpiry()
	c.RecordSessionCapEventFailure()
	c.RecordSessionAnchorAnomaly("missing")
}
