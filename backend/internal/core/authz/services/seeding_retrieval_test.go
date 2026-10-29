package services

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

var retrievalSpecs = []iface.PermissionSpec{
	{Key: "retrieval.corpus.read", Module: "retrieval", System: true},
	{Key: "retrieval.corpus.admin", Module: "retrieval", System: true},
	{Key: "retrieval.tenant.read", Module: "retrieval"},
	{Key: "retrieval.searchlog.admin", Module: "retrieval"},
	{Key: "tenant.read", Module: "tenant"},
	{Key: "system.modules.admin", Module: "system", System: true},
}

var allSeededRoles = []string{"super_admin", "administrator", "developer", "manager", "operator", "guest",
	"org_owner", "org_admin", "org_member", "org_billing", "org_viewer"}

// holders returns the seeded roles whose permission set contains key or "*", sorted.
func holders(t *testing.T, repo *fakeRepo, key string) []string {
	t.Helper()
	out := []string{}
	for _, name := range allSeededRoles {
		role := repo.roleByName(name)
		if role == nil {
			t.Fatalf("role %s not seeded", name)
		}
		if slices.Contains(role.Permissions, "*") || slices.Contains(role.Permissions, key) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func TestSeedSystemRoles_RetrievalMatrix(t *testing.T) {
	cases := []struct {
		production bool
		want       map[string][]string
	}{
		{false, map[string][]string{
			"retrieval.corpus.read":     {"administrator", "developer", "guest", "manager", "operator", "super_admin"},
			"retrieval.corpus.admin":    {"administrator", "developer", "super_admin"},
			"retrieval.tenant.read":     {"administrator", "developer", "guest", "manager", "operator", "org_admin", "org_member", "org_owner", "org_viewer", "super_admin"},
			"retrieval.searchlog.admin": {"administrator", "developer", "org_admin", "org_owner", "super_admin"},
		}},
		{true, map[string][]string{
			"retrieval.corpus.read":     {"administrator", "developer", "guest", "manager", "operator", "super_admin"},
			"retrieval.corpus.admin":    {"administrator", "super_admin"},
			"retrieval.tenant.read":     {"administrator", "developer", "guest", "manager", "operator", "org_admin", "org_member", "org_owner", "org_viewer", "super_admin"},
			"retrieval.searchlog.admin": {"administrator", "org_admin", "org_owner", "super_admin"},
		}},
	}
	for _, c := range cases {
		env := "development"
		if c.production {
			env = "production"
		}
		t.Run(env, func(t *testing.T) {
			ctx := context.Background()
			svc, repo := newTier1Service(t, staticRoleLookup(""))
			svc.production = c.production
			if err := svc.RegisterPermissions(ctx, retrievalSpecs); err != nil {
				t.Fatalf("RegisterPermissions: %v", err)
			}
			if err := svc.SeedSystemRoles(ctx); err != nil {
				t.Fatalf("SeedSystemRoles: %v", err)
			}
			for key, want := range c.want {
				if got := holders(t, repo, key); !reflect.DeepEqual(got, want) {
					t.Errorf("%s %s holders = %v, want %v", env, key, got, want)
				}
			}
		})
	}
}
