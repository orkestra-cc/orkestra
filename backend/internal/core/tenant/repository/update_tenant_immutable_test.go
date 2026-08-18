package repository

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestUpdateTenant_RejectsImmutableFields verifies the H-7 guard: identity /
// ownership / tier / hierarchy / deletion fields cannot be changed through the
// generic UpdateTenant $set path. The guard runs before any DB access, so a
// Repository with a nil db is sufficient — a rejected field never reaches Mongo.
func TestUpdateTenant_RejectsImmutableFields(t *testing.T) {
	r := &Repository{} // nil db: the guard must return before touching it
	for _, field := range []string{
		"ownerUserUUID", "kind", "parentTenantUUID", "deletedAt", "uuid", "_id", "createdAt",
	} {
		err := r.UpdateTenant(context.Background(), "t-1", bson.M{field: "x"})
		if err == nil {
			t.Errorf("field %q must be rejected as immutable", field)
			continue
		}
		if !strings.Contains(err.Error(), "immutable") {
			t.Errorf("field %q: expected an immutable-field error, got %v", field, err)
		}
	}
}
