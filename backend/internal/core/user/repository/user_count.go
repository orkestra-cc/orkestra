package repository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
)

// preferencesByUserFilter selects the subject's user_preferences rows;
// DeleteByUser and CountByUser share it.
func preferencesByUserFilter(userUUID string) bson.M { return bson.M{"userUuid": userUUID} }

// rowByUUIDFilter selects the user row whatever its state (live or
// soft-deleted); HardDelete and CountByID share it.
func rowByUUIDFilter(id string) bson.M { return bson.M{"uuid": id} }

// CountByID counts the rows HardDelete would remove: 0 or 1, a soft-deleted
// row included (DSR dry-run preview). It is not part of the UserRepository
// interface; the PII producer reaches it through an optional interface.
func (r *mongoUserRepository) CountByID(ctx context.Context, id string) (int64, error) {
	//tenantscope:allow system: users are audience-tier scoped (one collection per tier); DSR count of one subject's row by uuid, like HardDelete.
	n, err := r.collection.CountDocuments(ctx, rowByUUIDFilter(id))
	if err != nil {
		return 0, fmt.Errorf("failed to count user: %w", err)
	}
	return n, nil
}

// The PII producer's optional count views (services.userRowCounter and
// services.preferenceCounter); asserted here because the types are unexported.
var (
	_ interface {
		CountByID(ctx context.Context, id string) (int64, error)
	} = (*mongoUserRepository)(nil)
	_ interface {
		CountByUser(ctx context.Context, userUUID string) (int64, error)
	} = (*preferenceRepository)(nil)
)
