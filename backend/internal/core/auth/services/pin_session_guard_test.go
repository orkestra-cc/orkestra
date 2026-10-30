package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// unreadableMemberships fails the membership lookup.
type unreadableMemberships struct{ gateTenantProvider }

func (unreadableMemberships) ListUserMemberships(context.Context, string) ([]iface.TenantMembership, error) {
	return nil, errors.New("tenant store down")
}

// A failed membership lookup refuses eligibility: read as "no
// memberships" it would let an org_owner hold a PIN during an outage.
func TestPIN_UnreadableMembershipsRefuse(t *testing.T) {
	f := newPINFixture(t, nil)
	f.svc.login.tenantProvider = unreadableMemberships{}
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("AdminSetPIN = %v, want ErrAuthPolicyUnavailable", err)
	}
	if f.user(t).PINHash != "" {
		t.Fatal("pin written although memberships could not be read")
	}
}

// CheckPINSession: a refresh continues only while the PIN, the device and
// the eligibility the session signed in under still hold.
func TestPIN_CheckPINSession(t *testing.T) {
	ctx := context.Background()
	f := newPINFixture(t, nil)
	f.enrolledDevice(t)
	if err := f.svc.CheckPINSession(ctx, pinWorkerUUID, "tab-1"); err != nil {
		t.Fatalf("a valid PIN session must continue: %v", err)
	}
	if err := f.svc.CheckPINSession(ctx, pinWorkerUUID, "tab-unknown"); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("revoked device = %v, want ErrRefreshSessionInactive", err)
	}

	// A privileged membership acquired after sign-in ends the session.
	f.svc.login.tenantProvider = obligatedByMembership{}
	if err := f.svc.CheckPINSession(ctx, pinWorkerUUID, "tab-1"); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("org_owner since sign-in = %v, want ErrRefreshSessionInactive", err)
	}
	// An unreadable membership store cannot decide: retry, do not sign out.
	f.svc.login.tenantProvider = unreadableMemberships{}
	if err := f.svc.CheckPINSession(ctx, pinWorkerUUID, "tab-1"); !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("membership outage = %v, want ErrSessionEnforcementUnavailable", err)
	}
	f.svc.login.tenantProvider = nil

	// A promotion out of the field roles ends it too.
	f.users.byUUID[pinWorkerUUID].Role = "manager"
	if err := f.svc.CheckPINSession(ctx, pinWorkerUUID, "tab-1"); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("promoted user = %v, want ErrRefreshSessionInactive", err)
	}
	f.users.byUUID[pinWorkerUUID].Role = "operator"

	// So does the PIN's removal.
	if err := f.svc.AdminRemovePIN(ctx, "admin-1", pinWorkerUUID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.CheckPINSession(ctx, pinWorkerUUID, "tab-1"); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("removed PIN = %v, want ErrRefreshSessionInactive", err)
	}
}

// Failure writes are bound to the hash the attempt was verified against:
// an attempt that began before the PIN was replaced neither locks nor
// removes the new one.
func TestPIN_OldFailuresLeaveANewPINAlone(t *testing.T) {
	ctx := context.Background()
	f := newPINFixture(t, nil)
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	old := f.user(t).PINHash
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, "73915"); err != nil {
		t.Fatal(err)
	}
	f.svc.disablePIN(ctx, pinWorkerUUID, old, 10)
	if f.user(t).PINHash == "" {
		t.Fatal("an attempt counted against the old PIN removed the new one")
	}
	f.svc.recordPINFailure(ctx, "10.0.0.1", AttemptKeyPIN(f.svc.audience, pinWorker), pinWorkerUUID, old, 5)
	if u := f.user(t); u.PINLockedUntil != nil && time.Now().Before(*u.PINLockedUntil) {
		t.Fatal("an attempt counted against the old PIN locked the new one")
	}
}

// A PIN replaced while a sign-in was verifying the old one: no session.
func TestPIN_CredentialChangedDuringSignIn(t *testing.T) {
	ctx := context.Background()
	f := newPINFixture(t, nil)
	f.enrolledDevice(t)
	// The sign-in's own read: a copy, not the fake's live row.
	snapshot := *f.user(t)
	user := &snapshot
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	device, err := f.devices.Find(ctx, pinWorkerUUID, "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.credentialStillCurrent(ctx, user, device); err != nil {
		t.Fatalf("unchanged credential: %v", err)
	}
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, "73915"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.credentialStillCurrent(ctx, user, device); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("replaced PIN = %v, want ErrInvalidCredentials", err)
	}
}

// The PIN route applies the geo block the password route does, before
// counting anything.
func TestPIN_CountryBlocked(t *testing.T) {
	f := newPINFixture(t, map[string]string{"geoBlockCountries": "kp"})
	secret := f.enrolledDevice(t)
	f.svc.login.geoResolver = newGateGeoResolver(map[string]string{"10.0.0.1": "KP"})
	if _, err := f.login(goodPIN, secret); !errors.Is(err, ErrCountryBlocked) {
		t.Fatalf("login from a blocked country = %v, want ErrCountryBlocked", err)
	}
	f.svc.login.geoResolver = newGateGeoResolver(map[string]string{"10.0.0.1": "IT"})
	if _, err := f.login(goodPIN, secret); err != nil {
		t.Fatalf("login from an allowed country: %v", err)
	}
}

// A dormant account is disabled by a PIN sign-in exactly as by a password
// one, and the sign-in is refused.
func TestPIN_InactiveAccountAutoDisabled(t *testing.T) {
	f := newPINFixture(t, map[string]string{"inactiveAccountAutoDisableDays": "30"})
	secret := f.enrolledDevice(t)
	old := time.Now().Add(-60 * 24 * time.Hour)
	f.users.byUUID[pinWorkerUUID].LastLogin = &old
	if _, err := f.login(goodPIN, secret); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("dormant login = %v, want ErrInvalidCredentials", err)
	}
	if f.user(t).IsActive {
		t.Fatal("the dormant account was not disabled")
	}
}

// An explicitly empty toggle reads as its default, as the save-time
// validation accepts it: no outage on the PIN routes.
func TestPIN_EmptyToggleReadsAsDefault(t *testing.T) {
	f := newPINFixture(t, map[string]string{"pinRequiresTrustedDevice": ""})
	require, err := f.svc.policy.PINRequiresTrustedDevice(context.Background())
	if err != nil || !require {
		t.Fatalf("empty pinRequiresTrustedDevice = %v, %v; want the default true", require, err)
	}
}

// An enrollment code is bound to the PIN it was issued for: once that PIN
// is replaced the code enrolls nothing.
func TestPIN_EnrollmentCodeBoundToItsPIN(t *testing.T) {
	ctx := context.Background()
	f := newPINFixture(t, nil)
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	code, _, err := f.svc.IssueEnrollmentCode(ctx, "admin-1", pinWorkerUUID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, "73915"); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.EnrollDevice(ctx, PINDeviceEnrollInput{Username: pinWorker, Code: code, DeviceID: "tab-2", IP: "10.0.0.1"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("code issued for the replaced PIN = %v, want ErrInvalidCredentials", err)
	}
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	if _, err := f.devices.Find(ctx, pinWorkerUUID, "tab-2"); err == nil {
		t.Fatal("a device was enrolled with a code for the replaced PIN")
	}
}
