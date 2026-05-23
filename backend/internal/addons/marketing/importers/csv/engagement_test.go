package csv

import (
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

func TestDetectEngagementColumns(t *testing.T) {
	header := []string{
		"name", "email", "EMAIL_OPENED", "email_clicked", "occurred_at", "unmapped",
	}
	cols, occCol, hasOcc := DetectEngagementColumns(header)
	if len(cols) != 2 {
		t.Fatalf("expected 2 cols, got %d", len(cols))
	}
	if cols[0].Kind != models.KindEmailOpened || cols[0].ColumnIndex != 2 {
		t.Errorf("col[0] = %+v", cols[0])
	}
	if cols[1].Kind != models.KindEmailClicked || cols[1].ColumnIndex != 3 {
		t.Errorf("col[1] = %+v", cols[1])
	}
	if !hasOcc || occCol != 4 {
		t.Errorf("occ col = %d hasOcc=%v", occCol, hasOcc)
	}
}

func TestDetectEngagementColumns_NoMatches(t *testing.T) {
	cols, _, _ := DetectEngagementColumns([]string{"name", "email", "phone"})
	if len(cols) != 0 {
		t.Errorf("expected 0 cols, got %v", cols)
	}
}

func TestIsKnownEngagementColumn(t *testing.T) {
	cases := map[string]bool{
		"email_opened":     true,
		"EMAIL_OPENED":     true,
		"  page_visited  ": true,
		"event_attended":   true,
		"unknown_kind":     false,
		"":                 false,
		"primaryEmail":     false,
	}
	for in, want := range cases {
		if got := IsKnownEngagementColumn(in); got != want {
			t.Errorf("IsKnownEngagementColumn(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEngagementKindsCoverAllPlannedHeaders(t *testing.T) {
	// Sanity check: every key in engagementColumnKinds maps to a
	// kind that's actually declared in the models enum.
	for k, v := range engagementColumnKinds {
		if !models.IsKnownKind(v) {
			t.Errorf("engagement column %q maps to unknown kind %q", k, v)
		}
	}
}
