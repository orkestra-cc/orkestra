package compliance

import (
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func indexKeys(s module.IndexSpec) string {
	if len(s.OrderedKeys) > 0 {
		parts := make([]string, len(s.OrderedKeys))
		for i, k := range s.OrderedKeys {
			parts[i] = k.Field
		}
		return strings.Join(parts, ",")
	}
	for k := range s.Keys {
		return k
	}
	return ""
}

func TestCollections_PolicyEngine(t *testing.T) {
	byName := map[string][]module.IndexSpec{}
	for _, c := range NewModule().Collections() {
		byName[c.Name] = c.Indexes
	}
	want := []struct {
		coll, keys string
		unique     bool
		partial    bool
	}{
		{models.PoliciesCollection, "uuid", true, false},
		{models.PoliciesCollection, "name", true, false},
		{models.PoliciesCollection, "isPlatformDefault", true, true},
		{models.PolicyVersionsCollection, "policyUuid,version", true, false},
		{models.PolicyVersionsCollection, "changedAt", false, false},
		{models.PolicyAssignmentsCollection, "tenantId", true, false},
		{models.PolicyAssignmentsCollection, "policyUuid", false, false},
		{models.PolicyAssignmentHistoryCollection, "tenantId,changedAt", false, false},
		{models.PolicyAssignmentHistoryCollection, "changedAt", false, false},
		{models.PolicyChangeRequestsCollection, "uuid", true, false},
		{models.PolicyChangeRequestsCollection, "status,requestedAt", false, false},
	}
	for _, w := range want {
		found := false
		for _, s := range byName[w.coll] {
			if indexKeys(s) == w.keys && s.Unique == w.unique && (len(s.PartialFilter) > 0) == w.partial {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing index %s (unique=%v partial=%v)", w.coll, w.keys, w.unique, w.partial)
		}
	}
}

// TestCollections_OneActiveRetentionErasurePerSubject pins the partial unique
// index that makes a retention run's insert of an incomplete request atomic
// against another replica's (ADR-0023 D12 retention, review finding on #464).
func TestCollections_OneActiveRetentionErasurePerSubject(t *testing.T) {
	for _, c := range NewModule().Collections() {
		if c.Name != models.ErasureRequestsCollection {
			continue
		}
		for _, s := range c.Indexes {
			if indexKeys(s) == "userUuid" && s.Unique &&
				s.PartialFilter["status"] == models.ErasureRequestIncomplete &&
				s.PartialFilter["source"] == models.ErasureSourceRetention && len(s.PartialFilter) == 2 {
				return
			}
		}
	}
	t.Fatalf("%s: missing unique userUuid index partial on {status: incomplete, source: retention}", models.ErasureRequestsCollection)
}
