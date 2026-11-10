package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
)

// bindingsByUserFilter selects every binding of the subject (all tenants and
// global ones, expired included); the DSR purge (DeleteBindingsByUser) and its
// count share it.
func bindingsByUserFilter(userUUID string) bson.M { return bson.M{"userUUID": userUUID} }

// CountBindingsByUser counts what DeleteBindingsByUser would remove.
func (r *Repository) CountBindingsByUser(ctx context.Context, userUUID string) (int64, error) {
	if userUUID == "" {
		return 0, nil
	}
	//tenantscope:allow DSR count is cross-tenant by data-subject; the filter pins userUUID explicitly (mirrors DeleteBindingsByUser).
	return r.db.Collection(CollBindings).CountDocuments(ctx, bindingsByUserFilter(userUUID))
}
