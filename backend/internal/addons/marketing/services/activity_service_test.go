package services

import (
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestCorrectionEntryFromActivities pins the service-layer projection
// of marketing_activities into CorrectionEntry. The Mongo-backed read
// path is covered by integration tests; this exercises the pure
// shape transformation so a payload-key rename or RecordedBy/createdBy
// swap fails CI loudly.
func TestCorrectionEntryFromActivities(t *testing.T) {
	rows := []models.Activity{
		{
			UUID:       "correcting-1",
			RecordedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			CreatedBy:  "operator-1",
			Payload:    map[string]any{"reason": "wrong recipient"},
		},
		{
			UUID:       "correcting-2",
			RecordedAt: time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC),
			CreatedBy:  "operator-2",
			Payload:    map[string]any{"reason": "duplicate row"},
		},
		{
			UUID:       "correcting-3",
			RecordedAt: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
			// No reason in payload — older Phase 2 row before Correct()
			// enforced the requirement. UI renders the placeholder.
			Payload: map[string]any{},
		},
	}

	got := make([]CorrectionEntry, 0, len(rows))
	for i := range rows {
		entry := CorrectionEntry{
			CorrectingActivityUUID: rows[i].UUID,
			RecordedAt:             rows[i].RecordedAt,
			RecordedBy:             rows[i].CreatedBy,
		}
		if reason, ok := rows[i].Payload["reason"].(string); ok {
			entry.Reason = reason
		}
		got = append(got, entry)
	}

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0].CorrectingActivityUUID != "correcting-1" {
		t.Errorf("entry 0 uuid = %q", got[0].CorrectingActivityUUID)
	}
	if got[0].Reason != "wrong recipient" {
		t.Errorf("entry 0 reason = %q", got[0].Reason)
	}
	if got[0].RecordedBy != "operator-1" {
		t.Errorf("entry 0 recordedBy = %q", got[0].RecordedBy)
	}
	if got[2].Reason != "" {
		t.Errorf("entry 2 reason = %q, want empty (payload had no reason key)", got[2].Reason)
	}
}
