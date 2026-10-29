package cedar

import "testing"

func TestRetrievalPolicies(t *testing.T) {
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}
	mfa := func(p Principal) Principal { p.MFAEnrolled = true; return p }
	cases := []struct {
		name   string
		env    string
		p      Principal
		action string
		allow  bool
		policy string // must be among d.Reasons when allow; "" = do not check
	}{
		{"administrator reads corpora", "production", mfa(Principal{UserUUID: "u", SystemRole: "administrator"}), "retrieval.corpus.read", true, "retrieval.corpus_read.platform_roles"},
		{"prod developer reads corpora", "production", Principal{UserUUID: "u", SystemRole: "developer"}, "retrieval.corpus.read", true, ""},
		{"administrator administers corpora", "production", mfa(Principal{UserUUID: "u", SystemRole: "administrator"}), "retrieval.corpus.admin", true, "retrieval.corpus_admin.platform_admins"},
		{"administrator without MFA is refused", "production", Principal{UserUUID: "u", SystemRole: "administrator"}, "retrieval.corpus.admin", false, ""},
		{"prod developer cannot administer", "production", Principal{UserUUID: "u", SystemRole: "developer"}, "retrieval.corpus.admin", false, ""},
		{"org_admin reads the search log", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_admin"}}, "retrieval.searchlog.admin", true, "retrieval.searchlog_admin.org_admins"},
		{"org_member cannot read the search log", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_member"}}, "retrieval.searchlog.admin", false, ""},
		{"org_viewer cannot read the search log", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_viewer"}}, "retrieval.searchlog.admin", false, ""},
		{"org_viewer reads tenant indexing", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_viewer"}}, "retrieval.tenant.read", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newTestEngine(t, c.env).IsAuthorized(c.p, c.action, r)
			if d.Allowed != c.allow {
				t.Fatalf("allowed = %v, want %v (reasons %v)", d.Allowed, c.allow, d.Reasons)
			}
			if len(d.Errors) != 0 {
				t.Fatalf("errors = %v", d.Errors)
			}
			if c.allow && c.policy != "" && !decidedBy(d, c.policy) {
				t.Fatalf("policy %s not among reasons %v", c.policy, d.Reasons)
			}
		})
	}
}
