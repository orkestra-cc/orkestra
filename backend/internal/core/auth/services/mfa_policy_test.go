package services

import (
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestRoleRequiresMFA(t *testing.T) {
	cases := []struct {
		name        string
		role        string
		memberships []authModels.TenantMembership
		want        bool
	}{
		{"super_admin without memberships", "super_admin", nil, true},
		{"administrator without memberships", "administrator", nil, true},
		{"developer not required", "developer", nil, false},
		{"operator not required", "operator", nil, false},
		{"guest not required", "guest", nil, false},
		{"org_owner in single org", "operator", []authModels.TenantMembership{{TenantUUID: "o1", Roles: []string{"org_owner"}}}, true},
		{"org_admin in single org", "operator", []authModels.TenantMembership{{TenantUUID: "o1", Roles: []string{"org_admin"}}}, true},
		{"org_member only", "operator", []authModels.TenantMembership{{TenantUUID: "o1", Roles: []string{"org_member"}}}, false},
		{"privileged in one org, member in another", "operator", []authModels.TenantMembership{
			{TenantUUID: "o1", Roles: []string{"org_member"}},
			{TenantUUID: "o2", Roles: []string{"org_admin"}},
		}, true},
		{"nil user is safe", "", nil, false}, // constructed via nil user below
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var user *iface.User
			if tc.role != "" || tc.memberships != nil {
				user = &iface.User{Role: tc.role}
			}
			got := RoleRequiresMFA(user, tc.memberships)
			if got != tc.want {
				t.Fatalf("role=%q memberships=%+v → got %v want %v", tc.role, tc.memberships, got, tc.want)
			}
		})
	}
}

func TestGraceExpired(t *testing.T) {
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	within := now.Add(-3 * 24 * time.Hour)
	past := now.Add(-10 * 24 * time.Hour)

	t.Run("nil stamp never expired", func(t *testing.T) {
		if GraceExpired(&iface.User{}, now) {
			t.Fatalf("nil stamp should not be expired")
		}
	})
	t.Run("within window", func(t *testing.T) {
		u := &iface.User{MFAGraceStartedAt: &within}
		if GraceExpired(u, now) {
			t.Fatalf("should be within grace")
		}
	})
	t.Run("past window", func(t *testing.T) {
		u := &iface.User{MFAGraceStartedAt: &past}
		if !GraceExpired(u, now) {
			t.Fatalf("should be expired")
		}
	})
}

func TestGraceExpiresAt(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	u := &iface.User{MFAGraceStartedAt: &start}
	got := GraceExpiresAt(u)
	want := start.Add(MFAEnrollmentGraceWindow)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}

	// Nil stamp → zero time
	if v := GraceExpiresAt(&iface.User{}); !v.IsZero() {
		t.Fatalf("expected zero time for unset stamp, got %v", v)
	}
}
