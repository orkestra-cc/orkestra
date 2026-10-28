package services

import (
	"context"
	"fmt"
	"time"
)

// DefaultSearchLogRetentionDays is the default of config key
// search_log_retention_days.
const DefaultSearchLogRetentionDays = 90

// SearchLogPurger deletes search-log rows older than a cutoff, across tenants.
type SearchLogPurger interface {
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// PurgeSearchLog deletes rows created before now - days. days < 1 is refused
// (an error, nothing deleted).
func PurgeSearchLog(ctx context.Context, p SearchLogPurger, days int, now time.Time) (int64, error) {
	if days < 1 {
		return 0, fmt.Errorf("retrieval: search log retention must be at least 1 day, got %d", days)
	}
	return p.PurgeBefore(ctx, now.AddDate(0, 0, -days))
}
