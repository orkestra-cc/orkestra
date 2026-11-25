package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
)

// ByUserFilter selects every row of one subject in an auth collection keyed by
// userUuid. The DSR purge (DeleteAllByUser) and the dry-run count (CountByUser)
// of each repository, and the security-event service, share it so the two
// cannot drift apart.
func ByUserFilter(userUUID string) bson.M { return bson.M{"userUuid": userUUID} }

// CountByUser counts the rows DeleteAllByUser would remove, with the same
// filter (DSR dry-run preview).
func (r *mfaFactorRepository) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow Auth state is audience-tier scoped, not org scoped, and keyed on userUuid; the DSR dry-run count follows the erasure (DeleteAllByUser) it previews.
	return r.coll.CountDocuments(ctx, ByUserFilter(userUUID))
}

// CountByUser counts the rows DeleteAllByUser would remove, with the same
// filter (DSR dry-run preview).
func (r *authSessionRepository) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow Auth state is audience-tier scoped, not org scoped, and keyed on userUuid; the DSR dry-run count follows the erasure (DeleteAllByUser) it previews.
	return r.collection.CountDocuments(ctx, ByUserFilter(userUUID))
}

// CountByUser counts the rows DeleteAllByUser would remove, with the same
// filter (DSR dry-run preview).
func (r *deviceTrustRepository) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	if userUUID == "" {
		return 0, nil
	}
	//tenantscope:allow Auth state is audience-tier scoped, not org scoped, and keyed on userUuid; the DSR dry-run count follows the erasure (DeleteAllByUser) it previews.
	return r.collection.CountDocuments(ctx, ByUserFilter(userUUID))
}

// CountByUser counts the rows DeleteAllByUser would remove, with the same
// filter (DSR dry-run preview).
func (r *emailTokenRepository) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow Auth state is audience-tier scoped, not org scoped, and keyed on userUuid; the DSR dry-run count follows the erasure (DeleteAllByUser) it previews.
	return r.coll.CountDocuments(ctx, ByUserFilter(userUUID))
}

// CountByUser counts the rows DeleteAllByUser would remove, with the same
// filter (DSR dry-run preview).
func (r *pinDeviceRepository) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	return r.coll.CountDocuments(ctx, ByUserFilter(userUUID))
}

// CountByUser counts the rows DeleteAllByUser would remove, with the same
// filter (DSR dry-run preview).
func (r *refreshTokenRepository) CountByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow Refresh-token state is audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	return r.collection.CountDocuments(ctx, ByUserFilter(userUUID))
}
