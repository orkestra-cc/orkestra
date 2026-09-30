package retentionclass

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestCatalog(t *testing.T) {
	all := All()
	if len(all) != len(iface.AllRetentionClasses()) {
		t.Fatalf("catalog has %d classes, iface declares %d", len(all), len(iface.AllRetentionClasses()))
	}
	for _, c := range all {
		if c.Purpose == "" || c.LegalBasis == "" || c.Events == "" {
			t.Fatalf("%s: purpose, legal basis and events are mandatory", c.Key)
		}
		if c.DefaultDays < c.MinDays || c.DefaultDays > MaxDays {
			t.Fatalf("%s: default %d outside [%d, %d]", c.Key, c.DefaultDays, c.MinDays, MaxDays)
		}
	}
	aa, ok := Get(iface.RetentionAdminAccess)
	if !ok || aa.MinDays != 184 || !aa.HardMinimum || aa.DefaultDays != 365 || !aa.TenantSettable {
		t.Fatalf("admin_access = %+v", aa)
	}
	for k, want := range map[iface.RetentionClass]bool{
		iface.RetentionAdminAccess: true, iface.RetentionPrivilegedChange: true, iface.RetentionClientActivity: true,
		iface.RetentionAuthenticationSecurity: false, iface.RetentionComplianceEvidence: false,
	} {
		c, _ := Get(k)
		if c.TenantSettable != want {
			t.Fatalf("%s tenant settable = %v, want %v", k, c.TenantSettable, want)
		}
	}
	d := DefaultRetention()
	d[iface.RetentionAdminAccess] = 1
	if DefaultRetention()[iface.RetentionAdminAccess] != 365 {
		t.Fatal("DefaultRetention shares its map")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		action, audience string
		want             iface.RetentionClass
	}{
		{"compliance.policy.updated", "operator", iface.RetentionComplianceEvidence},
		{"dsr.export.completed", "client", iface.RetentionComplianceEvidence},
		{"legalhold.placed", "", iface.RetentionComplianceEvidence},
		{"auth.login.succeeded", "operator", iface.RetentionAdminAccess},
		{"auth.login.failed", "client", iface.RetentionClientActivity},
		{"auth.logout", "operator", iface.RetentionAdminAccess},
		{"auth.mfa.enrolled", "client", iface.RetentionClientActivity},
		{"admin.tenant.impersonate", "operator", iface.RetentionAdminAccess},
		// An access event with no known audience keeps the longest floor.
		{"auth.login.succeeded", "service", iface.RetentionAdminAccess},
		{"auth.login.succeeded", "", iface.RetentionAdminAccess},
		{"user.updated", "client", iface.RetentionClientActivity},
		{"user.updated", "operator", iface.RetentionPrivilegedChange},
		{"module.config.updated", "service", iface.RetentionPrivilegedChange},
		{"user.updated", "", iface.RetentionPrivilegedChange},
		// Prefixes are whole segments.
		{"auth.login", "operator", iface.RetentionPrivilegedChange},
		{"compliancex.foo", "operator", iface.RetentionPrivilegedChange},
	}
	for _, c := range cases {
		if got := Classify(c.action, c.audience); got != c.want {
			t.Errorf("Classify(%q, %q) = %s, want %s", c.action, c.audience, got, c.want)
		}
	}
}
