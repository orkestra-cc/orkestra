// Package retentionclass is the fixed catalog of audit-evidence retention
// classes (compliance spec §1.3): purpose, legal basis, covered events,
// minimum and default. None of these is configurable; a policy only sets
// the days, within [MinDays, MaxDays]. The console, the evidence export and
// the RoPA template read the same catalog.
package retentionclass

import (
	"slices"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// MaxDays caps every class (ten years).
const MaxDays = 3650

// AdminAccessMinDays is the Garante AdS floor: six calendar months are at
// most 184 days, so 184 always covers them.
const AdminAccessMinDays = 184

type Class struct {
	Key            iface.RetentionClass `json:"key"`
	Events         string               `json:"events"`
	Purpose        string               `json:"purpose"`
	LegalBasis     string               `json:"legalBasis"`
	MinDays        int                  `json:"minDays"`
	HardMinimum    bool                 `json:"hardMinimum"`
	DefaultDays    int                  `json:"defaultDays"`
	TenantSettable bool                 `json:"tenantSettable"`
}

var catalog = []Class{
	{
		Key:            iface.RetentionAdminAccess,
		Events:         "Tier-1 operator logins, logouts, failed logins, MFA and impersonation",
		Purpose:        "Control of administrator access",
		LegalBasis:     "GDPR art. 6(1)(c) — Garante, system administrators measure of 27/11/2008",
		MinDays:        AdminAccessMinDays,
		HardMinimum:    true,
		DefaultDays:    365,
		TenantSettable: true,
	},
	{
		Key:            iface.RetentionPrivilegedChange,
		Events:         "Configuration, role, user, tenant and policy changes made by operators",
		Purpose:        "Accountability and security of privileged changes",
		LegalBasis:     "GDPR art. 6(1)(f) — legitimate interest (LIA)",
		MinDays:        1,
		DefaultDays:    730,
		TenantSettable: true,
	},
	{
		Key:            iface.RetentionClientActivity,
		Events:         "Actions and logins of Tier-2 client users",
		Purpose:        "Security of the service provided to the client",
		LegalBasis:     "GDPR art. 6(1)(b) or (f)",
		MinDays:        1,
		DefaultDays:    365,
		TenantSettable: true,
	},
	{
		Key:         iface.RetentionAuthenticationSecurity,
		Events:      "Authentication security events (auth_security_events)",
		Purpose:     "Detection of fraud and abuse",
		LegalBasis:  "GDPR art. 6(1)(f) — legitimate interest",
		MinDays:     1,
		DefaultDays: 365,
	},
	{
		Key:         iface.RetentionComplianceEvidence,
		Events:      "Policy versions and assignments, change requests, DSR, legal holds, job results, verifications and alarms",
		Purpose:     "Demonstrating compliance (GDPR art. 5(2), 30)",
		LegalBasis:  "GDPR art. 6(1)(c)",
		MinDays:     1,
		DefaultDays: 1825,
	},
}

// All returns a copy of the catalog in its fixed order.
func All() []Class { return slices.Clone(catalog) }

// Get returns the class with key k.
func Get(k iface.RetentionClass) (Class, bool) {
	for _, c := range catalog {
		if c.Key == k {
			return c, true
		}
	}
	return Class{}, false
}

// DefaultRetention returns a fresh map of every class to its default days.
func DefaultRetention() map[iface.RetentionClass]int {
	out := make(map[iface.RetentionClass]int, len(catalog))
	for _, c := range catalog {
		out[c.Key] = c.DefaultDays
	}
	return out
}

// accessActions are the access events of spec §1.3: whole action names or
// whole-segment prefixes (ending with a dot).
var accessActions = []string{"auth.login.", "auth.logout", "auth.mfa.", "admin.tenant.impersonate"}

// Classify maps an audit action and the audience of its actor to a class
// (spec §1.3). An unknown audience falls on the most prudent class: the
// longest floor for access events, privileged_change for the rest.
func Classify(action, audience string) iface.RetentionClass {
	for _, p := range []string{"compliance.", "dsr.", "legalhold."} {
		if strings.HasPrefix(action, p) {
			return iface.RetentionComplianceEvidence
		}
	}
	for _, a := range accessActions {
		if action == a || (strings.HasSuffix(a, ".") && strings.HasPrefix(action, a)) {
			if audience == "client" {
				return iface.RetentionClientActivity
			}
			return iface.RetentionAdminAccess
		}
	}
	if audience == "client" {
		return iface.RetentionClientActivity
	}
	return iface.RetentionPrivilegedChange
}
