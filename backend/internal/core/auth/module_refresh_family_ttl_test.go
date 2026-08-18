package auth

import (
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
)

func TestRefreshFamilyCollectionsHaveAbsoluteExpiryIndex(t *testing.T) {
	m := &AuthModule{}
	want := map[string]bool{
		models.OperatorRefreshTokenFamiliesCollection: false,
		models.ClientRefreshTokenFamiliesCollection:   false,
	}
	for _, collection := range m.Collections() {
		if _, ok := want[collection.Name]; !ok {
			continue
		}
		for _, index := range collection.Indexes {
			if index.ExpireAt && index.Keys["expiresAt"] == 1 {
				want[collection.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s missing expiresAt ExpireAt index", name)
		}
	}
}
