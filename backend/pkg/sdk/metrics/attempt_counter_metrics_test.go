package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordAuthAttemptStoreFailure_CountsByOperation(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthAttemptStoreFailure("record")

	if got := testutil.ToFloat64(c.attemptStoreFailures.WithLabelValues("peek")); got != 2 {
		t.Errorf("peek = %v, want 2", got)
	}
	if got := testutil.ToFloat64(c.attemptStoreFailures.WithLabelValues("record")); got != 1 {
		t.Errorf("record = %v, want 1", got)
	}
}

// ADR-0002: the label schema is frozen and cardinality is bounded by the
// caller collapsing anything unexpected. An email address or an IP must
// never become a time series.
func TestRecordAuthAttemptStoreFailure_CollapsesUnknownOperation(t *testing.T) {
	c := NewCollector()
	c.RecordAuthAttemptStoreFailure("victim@example.com")
	if got := testutil.ToFloat64(c.attemptStoreFailures.WithLabelValues("unknown")); got != 1 {
		t.Errorf("unknown = %v, want 1 — an unexpected operation must collapse", got)
	}
}

func TestRecordAuthLockout_CollapsesUnknownScope(t *testing.T) {
	c := NewCollector()
	c.RecordAuthLockout("ip")
	c.RecordAuthLockout("203.0.113.9")
	if got := testutil.ToFloat64(c.authLockouts.WithLabelValues("ip")); got != 1 {
		t.Errorf("ip = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.authLockouts.WithLabelValues("unknown")); got != 1 {
		t.Errorf("unknown = %v, want 1", got)
	}
}

func TestRecordAuthMailDropped_CollapsesUnknownTemplate(t *testing.T) {
	c := NewCollector()
	c.RecordAuthMailDropped("auth.reset_password")
	c.RecordAuthMailDropped("marketing.blast")
	if got := testutil.ToFloat64(c.authMailDropped.WithLabelValues("auth.reset_password")); got != 1 {
		t.Errorf("reset = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.authMailDropped.WithLabelValues("unknown")); got != 1 {
		t.Errorf("unknown = %v, want 1", got)
	}
}

// A nil collector is the "metrics not wired" case every Record* method
// already tolerates; the new three must not be the ones that panic.
func TestNewAuthRecorders_NilSafe(t *testing.T) {
	var c *Collector
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthLockout("ip")
	c.RecordAuthMailDropped("auth.reset_password")
}

// Register must include the new families or they never leave the process.
func TestRegister_IncludesAuthAttemptFamilies(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Record at least one value for each family so they appear in Gather().
	// Prometheus only includes metrics in Gather() that have been observed.
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthLockout("email")
	c.RecordAuthMailDropped("auth.reset_password")
	families, err := c.registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{
		"orkestra_auth_attempt_store_failures_total",
		"orkestra_auth_lockouts_total",
		"orkestra_auth_mail_dropped_total",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s not registered; got %s", want, joined)
		}
	}
}
