package services

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/core/auth/repository"
)

// subjectEventsFilter selects every security event of one subject: the DSR
// erasure (DeleteAllByUser) and its count (CountByUser) share it with the
// auth repositories (repository.ByUserFilter).
func subjectEventsFilter(userUUID string) bson.M { return repository.ByUserFilter(userUUID) }

// CountByUser counts the rows DeleteAllByUser would remove (DSR dry-run
// preview), with the same filter.
func (s *securityEventService) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow auth_security_events is a non-tier-split audit log keyed on userUUID alone; the DSR dry-run count follows the erasure it previews.
	return s.collection.CountDocuments(ctx, subjectEventsFilter(userUUID))
}
