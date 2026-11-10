package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
)

// membershipsByUserFilter selects every membership row of the subject, expired
// ones included: the DSR purge (DeleteMembershipsByUser) and its count share it.
func membershipsByUserFilter(userUUID string) bson.M { return bson.M{"userUUID": userUUID} }

// CountMembershipsByUser counts what DeleteMembershipsByUser would remove.
func (r *Repository) CountMembershipsByUser(ctx context.Context, userUUID string) (int64, error) {
	if userUUID == "" {
		return 0, nil
	}
	//tenantscope:allow DSR count is cross-tenant by data-subject; the filter pins userUUID explicitly (mirrors DeleteMembershipsByUser).
	return r.db.Collection(CollMemberships).CountDocuments(ctx, membershipsByUserFilter(userUUID))
}
