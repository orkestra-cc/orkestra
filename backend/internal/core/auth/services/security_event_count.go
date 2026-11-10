package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/auth/repository"
)

// CountByUser counts the rows DeleteAllByUser would remove (DSR dry-run
// preview), with the same filter.
func (s *securityEventService) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow auth_security_events is a non-tier-split audit log keyed on userUUID alone; the DSR dry-run count follows the erasure it previews.
	return s.collection.CountDocuments(ctx, repository.ByUserFilter(userUUID))
}
