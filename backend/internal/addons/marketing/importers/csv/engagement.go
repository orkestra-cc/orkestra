package csv

import (
	"strings"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// engagementColumnKinds maps a normalized CSV header name to the
// Activity kind it produces. The detector is intentionally narrow
// (exact-match on lower-case) — operators who want to push engagement
// data through the CSV importer prefix their export columns with the
// canonical names from this map.
//
// When a row carries one of these columns alongside a contact-key
// column (primaryEmail or canonical person.email mapping), the row
// emits one or more Activity rows in addition to the
// CanonicalRecord. The Activity emission goes through ActivityService
// so dedupKey + listeners apply uniformly with manual / Odoo /
// future-webhook emissions.
var engagementColumnKinds = map[string]models.ActivityKind{
	"email_opened":       models.KindEmailOpened,
	"email_clicked":      models.KindEmailClicked,
	"email_bounced":      models.KindEmailBounced,
	"email_unsubscribed": models.KindEmailUnsubscribed,
	"email_complained":   models.KindEmailComplained,
	"form_submitted":     models.KindFormSubmitted,
	"page_visited":       models.KindPageVisited,
	"event_attended":     models.KindEventAttended,
}

// EngagementColumn describes one detected engagement column in the
// CSV header. ColumnIndex is 0-based into the source's column space;
// Kind is the Activity kind the column produces.
type EngagementColumn struct {
	ColumnIndex int
	Header      string
	Kind        models.ActivityKind
}

// DetectEngagementColumns scans the header row for engagement column
// patterns. Returns the matched columns + a boolean flag indicating
// whether an `occurred_at` column was also found (without one, the
// detector falls back to the import-job's timestamp, which is fine
// for ingestion but loses the original event time).
//
// Headers are case-insensitive after a TrimSpace + ToLower pass — the
// canonical CSV format uses snake_case lowercase but operators
// commonly upper-case their export headers.
func DetectEngagementColumns(header []string) (cols []EngagementColumn, occurredAtCol int, hasOccurredAt bool) {
	occurredAtCol = -1
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		if key == "occurred_at" {
			occurredAtCol = i
			hasOccurredAt = true
			continue
		}
		if kind, ok := engagementColumnKinds[key]; ok {
			cols = append(cols, EngagementColumn{
				ColumnIndex: i,
				Header:      h,
				Kind:        kind,
			})
		}
	}
	return cols, occurredAtCol, hasOccurredAt
}

// IsKnownEngagementColumn is the predicate the wizard's column-
// mapping step uses to decide whether to render an "engagement" tag
// next to a column suggestion.
func IsKnownEngagementColumn(header string) bool {
	_, ok := engagementColumnKinds[strings.ToLower(strings.TrimSpace(header))]
	return ok
}
