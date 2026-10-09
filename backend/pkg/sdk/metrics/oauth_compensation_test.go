package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// orkestra_auth_oauth_compensation_failures_total (spec §4.8 D32 item 5):
// a failed backwards compensation on the OAuth signup reservation is
// counted, never hidden — the orphan heals on the next callback, but the
// operator must see that it happened.
func TestRecordOAuthCompensationFailure(t *testing.T) {
	c := NewCollector()
	c.RecordOAuthCompensationFailure()
	c.RecordOAuthCompensationFailure()
	if got := testutil.ToFloat64(c.oauthCompensationFailures); got != 2 {
		t.Fatalf("compensation failures = %v, want 2", got)
	}
	var nilC *Collector
	nilC.RecordOAuthCompensationFailure() // nil-safe like every other recorder
}
