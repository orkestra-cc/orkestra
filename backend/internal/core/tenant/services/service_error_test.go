package services

import (
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
)

func TestTenantWriteError_MapsSlugDuplicateKey(t *testing.T) {
	t.Parallel()
	err := tenantWriteError(mongo.WriteException{WriteErrors: []mongo.WriteError{{
		Code:    11000,
		Message: "E11000 duplicate key error collection: tenants index: slug_1 dup key",
	}}})
	if !errors.Is(err, ErrSlugAlreadyInUse) {
		t.Fatalf("want ErrSlugAlreadyInUse, got %v", err)
	}
}

func TestTenantWriteError_LeavesOtherDuplicateKeysUnclassified(t *testing.T) {
	t.Parallel()
	err := tenantWriteError(mongo.WriteException{WriteErrors: []mongo.WriteError{{
		Code:    11000,
		Message: "E11000 duplicate key error collection: tenants index: uuid_1 dup key",
	}}})
	if errors.Is(err, ErrSlugAlreadyInUse) {
		t.Fatalf("unexpected ErrSlugAlreadyInUse: %v", err)
	}
}
