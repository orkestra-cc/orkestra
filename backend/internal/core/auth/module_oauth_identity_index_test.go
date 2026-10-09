package auth

import (
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// One identity, one owner (spec §4.8 D32 item 1): both OAuth provider
// collections declare a UNIQUE (provider, providerId) index, in that key
// order, so the registry's create-only ensureCollections builds on a fresh
// install exactly what migration 0010 builds on an existing one — the same
// name, provider_1_providerId_1, which the boot check and the migration
// both look for.
func TestOAuthProviderCollectionsDeclareTheIdentityUniqueIndex(t *testing.T) {
	m := &AuthModule{}
	want := map[string]bool{
		models.OperatorOAuthProvidersCollection: false,
		models.ClientOAuthProvidersCollection:   false,
	}
	for _, collection := range m.Collections() {
		if _, ok := want[collection.Name]; !ok {
			continue
		}
		for _, index := range collection.Indexes {
			if len(index.OrderedKeys) != 2 || !index.Unique {
				continue
			}
			if index.OrderedKeys[0] == (module.IndexKey{Field: "provider", Direction: 1}) &&
				index.OrderedKeys[1] == (module.IndexKey{Field: "providerId", Direction: 1}) {
				want[collection.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s missing the unique ordered (provider, providerId) index — the ownership-first flow detects a conflict by its duplicate key", name)
		}
	}
}
