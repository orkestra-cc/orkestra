package auth

import (
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// session.expiresAt IS the retention deadline (now + AuthSessionRetention),
// so a TTL index expresses the intent exactly — unlike refresh-token rows,
// whose deletion rule a TTL index cannot express. ADR-0017 D7.
func TestSessionCollectionsHaveRetentionTTLIndex(t *testing.T) {
	m := &AuthModule{}
	want := map[string]bool{
		models.OperatorSessionsCollection: false,
		models.ClientSessionsCollection:   false,
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
			t.Errorf("%s missing expiresAt ExpireAt index — session documents would accumulate forever", name)
		}
	}
}

// The TTL index must never be able to delete a session whose expiresAt was
// never set.
//
// AuthSessionDoc.ExpiresAt is `bson:"expiresAt"` with no omitempty, so a zero
// value round-trips as 0001-01-01T00:00:00Z — in the past — and a bare TTL
// index deletes that row on the monitor's next pass. The consequence is an
// irreversible delete of a session that has not expired at all, and it is
// silent. Guarding it with a documented pre-flight countDocuments makes the
// safety depend on an operator remembering; the partial filter makes such a
// row structurally absent from the index instead. ADR-0017 D7.
func TestSessionTTLIndexExcludesZeroExpiry(t *testing.T) {
	m := &AuthModule{}
	checked := map[string]bool{
		models.OperatorSessionsCollection: false,
		models.ClientSessionsCollection:   false,
	}
	for _, collection := range m.Collections() {
		if _, ok := checked[collection.Name]; !ok {
			continue
		}
		for _, index := range collection.Indexes {
			if !index.ExpireAt || index.Keys["expiresAt"] != 1 {
				continue
			}
			checked[collection.Name] = true

			// Mongo rejects partialFilterExpression together with sparse,
			// so pin that the spec never asks for both.
			if index.Sparse {
				t.Errorf("%s expiresAt index sets both Sparse and PartialFilter — Mongo rejects the combination and the index would never build", collection.Name)
			}

			clause, ok := index.PartialFilter["expiresAt"]
			if !ok {
				t.Fatalf("%s expiresAt TTL index has no partial filter on expiresAt — a zero expiresAt serialises as a year-1 date and the TTL monitor would delete that session immediately", collection.Name)
			}
			bounds, ok := clause.(map[string]any)
			if !ok {
				t.Fatalf("%s partial filter on expiresAt = %T, want map[string]any expressing a lower bound", collection.Name, clause)
			}
			floor, ok := bounds["$gt"].(time.Time)
			if !ok {
				t.Fatalf("%s partial filter %v has no time-typed $gt bound — only a lower bound keeps a zero timestamp out of the index", collection.Name, bounds)
			}
			// The floor has to sit strictly above the zero time (which is
			// what an unset field serialises to) and strictly below any
			// deadline this code actually writes (now + retention).
			if !floor.After(time.Time{}) {
				t.Errorf("%s TTL partial-filter floor %s does not exclude the zero time", collection.Name, floor)
			}
			if !floor.Before(time.Now().Add(models.AuthSessionRetention)) {
				t.Errorf("%s TTL partial-filter floor %s is at or above a freshly written retention deadline — real sessions would fall out of the index and never be reaped", collection.Name, floor)
			}
		}
	}
	for name, found := range checked {
		if !found {
			t.Errorf("%s has no expiresAt ExpireAt index to check", name)
		}
	}
}

// The two collections must not share one filter map: IndexSpec.PartialFilter
// is mutable, so a shared instance turns any future edit of one collection's
// spec into a silent edit of the other's.
func TestSessionTTLPartialFiltersAreNotShared(t *testing.T) {
	a := sessionRetentionPartialFilter()
	b := sessionRetentionPartialFilter()
	a["expiresAt"] = "mutated"
	if _, ok := b["expiresAt"].(map[string]any); !ok {
		t.Error("sessionRetentionPartialFilter returns a shared map — mutating one collection's filter rewrote the other's")
	}
}
