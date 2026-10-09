package repository

import (
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
)

// mongo.IsDuplicateKeyError fires for BOTH unique indexes on the provider
// collections; the sentinel must say which one.
func TestDuplicateKeySentinel_ClassifiesByIndex(t *testing.T) {
	identity := mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000,
		Message: `E11000 duplicate key error collection: orkestra.operator_oauth_providers index: provider_1_providerId_1 dup key: { provider: "google", providerId: "1234" }`}}}
	user := mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000,
		Message: `E11000 duplicate key error collection: orkestra.operator_oauth_providers index: userUuid_1_provider_1 dup key: { userUuid: "u-1", provider: "google" }`}}}
	if !errors.Is(duplicateKeySentinel(identity), ErrOAuthIdentityDuplicate) {
		t.Fatal("the identity index must classify as ErrOAuthIdentityDuplicate")
	}
	if !errors.Is(duplicateKeySentinel(user), ErrOAuthProviderAlreadyLinked) {
		t.Fatal("the per-user index must classify as ErrOAuthProviderAlreadyLinked")
	}
	// An unrecognised duplicate stays an identity duplicate: the re-read
	// decides, and a miss there is a refusal, never a silent continue.
	other := mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000, Message: "E11000 duplicate key error index: something_else"}}}
	if !errors.Is(duplicateKeySentinel(other), ErrOAuthIdentityDuplicate) {
		t.Fatal("an unknown index must fall back to the identity sentinel")
	}
}
