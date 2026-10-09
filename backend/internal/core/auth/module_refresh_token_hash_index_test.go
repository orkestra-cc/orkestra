package auth

// Every refresh entry point looks a presented token up by its hash —
// PeekRefreshToken, GetByTokenAny and the rotation CAS all filter on
// {"token": sha256} — and the collections grow by one row per rotation per
// active user for the whole refresh TTL before the sweep removes them. With
// no index on that field each refresh was two to four collection scans,
// reachable by an anonymous caller holding any validly-signed dead token.
// Non-unique on purpose: a unique index can fail to build on existing data,
// and uniqueness is the tracked M-9 follow-up, not this change.

import (
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
)

func TestRefreshTokenCollectionsIndexTheTokenHash(t *testing.T) {
	m := &AuthModule{}
	want := map[string]bool{
		models.OperatorRefreshTokensCollection: false,
		models.ClientRefreshTokensCollection:   false,
	}
	for _, collection := range m.Collections() {
		if _, ok := want[collection.Name]; !ok {
			continue
		}
		for _, index := range collection.Indexes {
			if index.Keys["token"] == 1 && len(index.Keys) == 1 {
				if index.Unique {
					t.Errorf("%s: the token index must stay non-unique here (M-9 owns uniqueness)", collection.Name)
				}
				want[collection.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s missing a single-field index on token — every refresh is a collection scan without it", name)
		}
	}
}
