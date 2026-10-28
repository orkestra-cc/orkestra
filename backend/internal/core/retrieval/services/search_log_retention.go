package services

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultSearchLogRetentionDays is the default of config key
// search_log_retention_days.
const DefaultSearchLogRetentionDays = 90

// MaxSearchLogRetentionDays is the upper bound of search_log_retention_days.
const MaxSearchLogRetentionDays = 3650

// UncheckedSearchLogGrace is how long a provisional search-log row
// (models.SearchLog.Unchecked) may wait for its erasure re-check to settle it
// before the retention sweep deletes it. It is far longer than the re-check
// (searchLogErrorTimeout), so a row still unchecked after it is one whose
// re-check, delete or mark failed.
const UncheckedSearchLogGrace = 10 * time.Minute

// SearchLogPurger deletes search-log rows across tenants: every row older
// than a cutoff, and the provisional (unchecked) rows older than a cutoff.
type SearchLogPurger interface {
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
	PurgeUncheckedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// PurgeSearchLog deletes rows created before now - days, and unchecked rows
// created before now - UncheckedSearchLogGrace. Both sweeps run even when one
// fails; the count is their total and the error joins theirs, so a failed
// sweep is retried at the next run. An unchecked row is dropped even when no
// erasure covered it (fail-closed for privacy: rarely, a legitimate audit
// row is lost when Mongo failed between its insert and its confirmation).
// days outside 1..3650 is refused (an error, nothing deleted).
func PurgeSearchLog(ctx context.Context, p SearchLogPurger, days int, now time.Time) (int64, error) {
	if days < 1 || days > MaxSearchLogRetentionDays {
		return 0, fmt.Errorf("retrieval: search log retention must be between 1 and %d days, got %d", MaxSearchLogRetentionDays, days)
	}
	now = now.UTC()
	n, err := p.PurgeBefore(ctx, now.AddDate(0, 0, -days))
	u, uerr := p.PurgeUncheckedBefore(ctx, now.Add(-UncheckedSearchLogGrace))
	return n + u, errors.Join(err, uerr)
}
