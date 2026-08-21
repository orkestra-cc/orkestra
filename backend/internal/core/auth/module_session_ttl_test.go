package auth

import (
	"testing"

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
