package services

// SystemRoleHolderFinder (spec §4.7 D31): the auth module's first-admin
// sentinel backfill asks for the oldest holder of a system role. The answer
// must be DETERMINISTIC — every replica picks the same user — and must
// count a deactivated holder, because a deactivated super_admin still
// proves the install was bootstrapped.

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func day(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }

func newUserServiceForFinderTest(t *testing.T) (iface.SystemRoleHolderFinder, *fakeUserRepo) {
	t.Helper()
	repo := newFakeUserRepo()
	finder, ok := NewUserService(repo, &fakeOAuthProviderRepo{}).(iface.SystemRoleHolderFinder)
	if !ok {
		t.Fatal("the user service must implement iface.SystemRoleHolderFinder")
	}
	return finder, repo
}

func TestFindOldestUserWithRole_PicksTheEarliestCreatedAt(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(&iface.User{UUID: "b", Role: "super_admin", CreatedAt: day(2), IsActive: true})
	repo.seed(&iface.User{UUID: "a", Role: "super_admin", CreatedAt: day(1), IsActive: true})

	got, found, err := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got != "a" {
		t.Fatalf("got %q, want the earliest-created a", got)
	}
}

func TestFindOldestUserWithRole_TieIsBrokenByUUID(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(&iface.User{UUID: "b", Role: "super_admin", CreatedAt: day(1), IsActive: true})
	repo.seed(&iface.User{UUID: "a", Role: "super_admin", CreatedAt: day(1), IsActive: true})

	got, _, _ := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if got != "a" {
		t.Fatalf("got %q — an identical createdAt must be broken by uuid so replicas agree", got)
	}
}

func TestFindOldestUserWithRole_IgnoresDeletedUsers(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	gone := day(2)
	repo.seed(&iface.User{UUID: "deleted", Role: "super_admin", CreatedAt: day(1), DeletedAt: &gone})
	repo.seed(&iface.User{UUID: "live", Role: "super_admin", CreatedAt: day(3), IsActive: true})

	got, _, _ := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if got != "live" {
		t.Fatalf("got %q, want the non-deleted live", got)
	}
}

// A DEACTIVATED super_admin still proves the install was bootstrapped,
// so isActive is deliberately NOT filtered.
func TestFindOldestUserWithRole_IncludesInactiveUsers(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(&iface.User{UUID: "inactive", Role: "super_admin", CreatedAt: day(1), IsActive: false})

	got, found, _ := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if !found || got != "inactive" {
		t.Fatal("a deactivated super_admin still proves the install was bootstrapped")
	}
}

func TestFindOldestUserWithRole_OtherRolesDoNotCount(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(&iface.User{UUID: "admin", Role: "administrator", CreatedAt: day(1), IsActive: true})

	if _, found, err := svc.FindOldestUserWithRole(context.Background(), "super_admin"); found || err != nil {
		t.Fatalf("found=%v err=%v, want false/nil — an administrator is not a super_admin", found, err)
	}
}

func TestFindOldestUserWithRole_NoHolderReportsNotFound(t *testing.T) {
	svc, _ := newUserServiceForFinderTest(t)
	if _, found, err := svc.FindOldestUserWithRole(context.Background(), "super_admin"); found || err != nil {
		t.Fatalf("found=%v err=%v, want false/nil", found, err)
	}
}

func TestUserService_ImplementsSystemRoleHolderFinder(t *testing.T) {
	var _ iface.SystemRoleHolderFinder = (*userService)(nil)
}
