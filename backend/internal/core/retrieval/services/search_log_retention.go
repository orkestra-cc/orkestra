package services

import (
	"context"
	"fmt"
	"time"
)

// DefaultSearchLogRetentionDays is the default of config key
// search_log_retention_days.
const DefaultSearchLogRetentionDays = 90

const maxSearchLogRetentionDays = 3650

// SearchLogPurger deletes search-log rows older than a cutoff, across tenants.
type SearchLogPurger interface {
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// PurgeSearchLog deletes rows created before now - days. days outside 1..3650 is refused
// (an error, nothing deleted).
func PurgeSearchLog(ctx context.Context, p SearchLogPurger, days int, now time.Time) (int64, error) {
	if days < 1 || days > maxSearchLogRetentionDays {
		return 0, fmt.Errorf("retrieval: search log retention must be between 1 and %d days, got %d", maxSearchLogRetentionDays, days)
	}
	return p.PurgeBefore(ctx, now.UTC().AddDate(0, 0, -days))
}
