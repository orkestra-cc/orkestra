package services

import (
	"context"
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
// before the unchecked sweep deletes it; it is also the sweep's interval, so
// such a row is gone within about two graces (20 minutes). It is far longer
// than the re-check (searchLogErrorTimeout), so a row still unchecked after
// it is one whose re-check, delete or mark failed.
const UncheckedSearchLogGrace = 10 * time.Minute

// SearchLogPurger deletes search-log rows older than a cutoff, across tenants.
type SearchLogPurger interface {
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// UncheckedSearchLogSweeper deletes the provisional (unchecked) search-log
// rows older than a cutoff, across tenants.
type UncheckedSearchLogSweeper interface {
	PurgeUncheckedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// PurgeSearchLog deletes rows created before now - days. days outside 1..3650 is refused
// (an error, nothing deleted).
func PurgeSearchLog(ctx context.Context, p SearchLogPurger, days int, now time.Time) (int64, error) {
	if days < 1 || days > MaxSearchLogRetentionDays {
		return 0, fmt.Errorf("retrieval: search log retention must be between 1 and %d days, got %d", MaxSearchLogRetentionDays, days)
	}
	return p.PurgeBefore(ctx, now.UTC().AddDate(0, 0, -days))
}

// SweepUncheckedSearchLog deletes the unchecked rows created before now -
// UncheckedSearchLogGrace; on error it returns the count deleted so far and
// the next run retries. An unchecked row is dropped even when no erasure
// covered it (fail-closed for privacy: rarely, a legitimate audit row is lost
// when Mongo failed between its insert and its confirmation).
func SweepUncheckedSearchLog(ctx context.Context, s UncheckedSearchLogSweeper, now time.Time) (int64, error) {
	return s.PurgeUncheckedBefore(ctx, now.UTC().Add(-UncheckedSearchLogGrace))
}
