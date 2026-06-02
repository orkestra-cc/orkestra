package services

import (
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// MFAEnrollmentGraceWindow is the maximum allowed interval between a user
// acquiring a privileged role and having an enrolled MFA factor. Logins
// beyond this boundary without a factor return mfa_enrollment_required.
// Seven days is long enough to recover from an admin being out of office,
// short enough to meaningfully reduce the window of weakened auth.
const MFAEnrollmentGraceWindow = 7 * 24 * time.Hour

// Privileged role names — the two kinds that require a second factor.
const (
	// System roles (User.Role field / srole claim).
	SystemRoleSuperAdmin    = "super_admin"
	SystemRoleAdministrator = "administrator"

	// Org-scoped roles (authz role bindings, embedded in JWT memberships).
	OrgRoleOwner = "org_owner"
	OrgRoleAdmin = "org_admin"
)

// RoleRequiresMFA reports whether the caller's current privileges warrant
// mandatory MFA. We intentionally exclude developer — its in-prod downgrade
// to read-only, enforced in the authz layer, means a developer token can't
// cause the damage MFA is there to prevent. Adding a role here is a
// security-sensitive policy change that should show up in a PR diff.
func RoleRequiresMFA(user *iface.User, memberships []authModels.OrgMembership) bool {
	if user == nil {
		return false
	}
	switch user.Role {
	case SystemRoleSuperAdmin, SystemRoleAdministrator:
		return true
	}
	for _, m := range memberships {
		for _, r := range m.Roles {
			if r == OrgRoleOwner || r == OrgRoleAdmin {
				return true
			}
		}
	}
	return false
}

// GraceExpired reports whether the user's MFA enrollment grace window has
// lapsed. Nil MFAGraceStartedAt means "not yet started" — grace has not
// expired in that case because it hasn't even begun.
func GraceExpired(user *iface.User, now time.Time) bool {
	if user == nil || user.MFAGraceStartedAt == nil {
		return false
	}
	return now.Sub(*user.MFAGraceStartedAt) > MFAEnrollmentGraceWindow
}

// GraceExpiresAt returns the absolute deadline a user must enroll by. Zero
// time is returned when the grace clock hasn't started.
func GraceExpiresAt(user *iface.User) time.Time {
	if user == nil || user.MFAGraceStartedAt == nil {
		return time.Time{}
	}
	return user.MFAGraceStartedAt.Add(MFAEnrollmentGraceWindow)
}
