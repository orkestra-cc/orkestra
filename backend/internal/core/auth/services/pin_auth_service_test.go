package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fakes ---

// pinStoreFake implements iface.PINCredentialStore over the gate user
// fake's rows, so a PIN written here is what GetUserByID returns.
type pinStoreFake struct {
	users *gateUserFake
	mu    sync.Mutex
	// cleared records ClearPIN targets.
	cleared []string
}

func (s *pinStoreFake) GetUserForPINAuth(_ context.Context, username string) (*iface.User, error) {
	s.users.mu.Lock()
	defer s.users.mu.Unlock()
	var found *iface.User
	for _, u := range s.users.byUUID {
		if u.Username == username {
			if found != nil {
				return nil, iface.ErrUsernameAmbiguous
			}
			found = u
		}
	}
	if found == nil {
		return nil, iface.ErrUserNotFound
	}
	// A copy, as the repository decodes a fresh row: the PIN service holds
	// it across a verification while concurrent attempts write the live one.
	c := *found
	return &c, nil
}

// pinUsersCopying hands the PIN service copies of the fake's rows, as the
// user module does; tests keep editing the live rows through f.users.
type pinUsersCopying struct{ *gateUserFake }

func (u pinUsersCopying) GetUserByID(ctx context.Context, id string) (*iface.User, error) {
	row, err := u.gateUserFake.GetUserByID(ctx, id)
	if err != nil || row == nil {
		return row, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	c := *row
	return &c, nil
}

func (s *pinStoreFake) CountUsersByUsername(_ context.Context, username string) (int64, error) {
	s.users.mu.Lock()
	defer s.users.mu.Unlock()
	var n int64
	for _, u := range s.users.byUUID {
		if u.Username == username {
			n++
		}
	}
	return n, nil
}

func (s *pinStoreFake) with(userUUID string, fn func(u *iface.User)) error {
	s.users.mu.Lock()
	defer s.users.mu.Unlock()
	u, ok := s.users.byUUID[userUUID]
	if !ok {
		return iface.ErrUserNotFound
	}
	fn(u)
	return nil
}

// hashOf is the stored PIN hash of a user, "" when missing.
func (s *pinStoreFake) hashOf(userUUID string) string {
	s.users.mu.Lock()
	defer s.users.mu.Unlock()
	if u, ok := s.users.byUUID[userUUID]; ok {
		return u.PINHash
	}
	return ""
}

func (s *pinStoreFake) SetPINHash(_ context.Context, userUUID, hash string) error {
	return s.with(userUUID, func(u *iface.User) {
		now := time.Now()
		u.PINHash, u.PINUpdatedAt, u.PINFailedCount, u.PINLockedUntil = hash, &now, 0, nil
	})
}

func (s *pinStoreFake) ClearPIN(_ context.Context, userUUID, expectedHash string) error {
	if expectedHash != "" && s.hashOf(userUUID) != expectedHash {
		return iface.ErrPINChanged
	}
	s.mu.Lock()
	s.cleared = append(s.cleared, userUUID)
	s.mu.Unlock()
	return s.with(userUUID, func(u *iface.User) {
		u.PINHash, u.PINUpdatedAt, u.PINFailedCount, u.PINLockedUntil = "", nil, 0, nil
	})
}

func (s *pinStoreFake) RecordFailedPIN(_ context.Context, userUUID string) (int, error) {
	var n int
	err := s.with(userUUID, func(u *iface.User) {
		u.PINFailedCount++
		n = u.PINFailedCount
	})
	return n, err
}

func (s *pinStoreFake) LockPIN(_ context.Context, userUUID, expectedHash string, until time.Time) error {
	if s.hashOf(userUUID) != expectedHash {
		return iface.ErrPINChanged
	}
	return s.with(userUUID, func(u *iface.User) { t := until; u.PINLockedUntil = &t })
}

func (s *pinStoreFake) ClearFailedPINs(_ context.Context, userUUID string) error {
	return s.with(userUUID, func(u *iface.User) { u.PINFailedCount, u.PINLockedUntil = 0, nil })
}

type pinDeviceFake struct {
	mu   sync.Mutex
	rows map[string]authModels.PINDeviceDoc
}

func newPINDeviceFake() *pinDeviceFake {
	return &pinDeviceFake{rows: map[string]authModels.PINDeviceDoc{}}
}

func (f *pinDeviceFake) key(u, d string) string { return u + "|" + d }

func (f *pinDeviceFake) Upsert(_ context.Context, doc *authModels.PINDeviceDoc) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[f.key(doc.UserUUID, doc.DeviceID)] = *doc
	return nil
}

func (f *pinDeviceFake) Find(_ context.Context, u, d string) (*authModels.PINDeviceDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[f.key(u, d)]
	if !ok {
		return nil, repository.ErrPINDeviceNotFound
	}
	return &row, nil
}

func (f *pinDeviceFake) ListByUser(_ context.Context, u string) ([]authModels.PINDeviceDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []authModels.PINDeviceDoc{}
	for _, r := range f.rows {
		if r.UserUUID == u {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *pinDeviceFake) TouchLastUsed(_ context.Context, u, d string, when time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[f.key(u, d)]; ok {
		r.LastUsedAt = &when
		f.rows[f.key(u, d)] = r
	}
	return nil
}

func (f *pinDeviceFake) Delete(_ context.Context, u, d string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[f.key(u, d)]; !ok {
		return repository.ErrPINDeviceNotFound
	}
	delete(f.rows, f.key(u, d))
	return nil
}

func (f *pinDeviceFake) DeleteAllByUser(_ context.Context, u string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for k, r := range f.rows {
		if r.UserUUID == u {
			delete(f.rows, k)
			n++
		}
	}
	return n, nil
}

type pinAuthzFake struct {
	deny map[string]bool
	err  error
}

func (a *pinAuthzFake) HasPermission(_ context.Context, userUUID, org, perm string) (bool, error) {
	if a.err != nil {
		return false, a.err
	}
	return perm == PINLoginPermission && org == "" && !a.deny[userUUID], nil
}

type pinRevokerFake struct {
	mu    sync.Mutex
	calls []string
}

func (r *pinRevokerFake) RevokePINSessions(_ context.Context, userUUID, deviceID, reason string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, userUUID+"|"+deviceID+"|"+reason)
	return 1, nil
}

type pinEventsFake struct {
	mu     sync.Mutex
	events []string
}

func (e *pinEventsFake) RecordAdminAuthEvent(_ context.Context, eventType, _, _ string, _ map[string]interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, eventType)
}

func (e *pinEventsFake) RecordSelfAuthEvent(_ context.Context, eventType, _ string, _ map[string]interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, eventType)
}

func (e *pinEventsFake) has(t string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.events {
		if v == t {
			return true
		}
	}
	return false
}

// --- fixture ---

type pinFixture struct {
	svc      *PINAuthService
	users    *gateUserFake
	store    *pinStoreFake
	devices  *pinDeviceFake
	codes    *MemoryPINEnrollmentCodeStore
	authz    *pinAuthzFake
	revoker  *pinRevokerFake
	events   *pinEventsFake
	refresh  *sessionIdentityRefreshRepo
	sessions *sessionIdentitySessionRepo
	jwt      JWTService
	pwd      *countingPasswordService
	counter  AttemptCounter
}

const (
	pinWorkerUUID = "018f0000-0000-7000-8000-0000000000p1"
	pinWorker     = "mario.rossi"
	goodPIN       = "48213"
)

func newPINFixture(t *testing.T, policy map[string]string) *pinFixture {
	t.Helper()
	return newPINFixtureFor(t, policy, PolicyAudienceOperator)
}

// newPINFixtureFor builds the fixture for one audience. On the client
// audience the administrator lives in a SEPARATE operator store (actors),
// as in production, and the worker in the client store.
func newPINFixtureFor(t *testing.T, policy map[string]string, aud PolicyAudience) *pinFixture {
	t.Helper()
	values := map[string]string{"pinLoginEnabledAdmin": "true", "pinLoginEnabledClient": "true", "pinLength": "5"}
	for k, v := range policy {
		values[k] = v
	}
	f := &pinFixture{
		users:    newGateUserFake(),
		devices:  newPINDeviceFake(),
		codes:    NewMemoryPINEnrollmentCodeStore(),
		authz:    &pinAuthzFake{deny: map[string]bool{}},
		revoker:  &pinRevokerFake{},
		events:   &pinEventsFake{},
		refresh:  &sessionIdentityRefreshRepo{},
		sessions: &sessionIdentitySessionRepo{},
		pwd:      &countingPasswordService{PasswordService: NewPasswordService(silentLogger(), false)},
		counter:  NewMemoryAttemptCounter(),
	}
	f.store = &pinStoreFake{users: f.users}
	login, jwt := newSessionIdentityPasswordAuth(t, f.refresh, f.sessions)
	// Rebind the login service onto this fixture's users and policy.
	login.userService = f.users
	login.policy = NewAuthPolicyServiceForTest(values)
	login.audience = aud
	login.attempts = f.counter
	f.jwt = jwt
	actors := f.users
	if aud == PolicyAudienceClient {
		actors = newGateUserFake()
	}
	f.svc = NewPINAuthService(PINAuthConfig{
		Login: login, Store: f.store, Users: pinUsersCopying{f.users}, Actors: actors, Devices: f.devices, Codes: f.codes,
		Passwords: f.pwd, Policy: login.policy, Audience: aud,
		Attempts: f.counter, Authz: f.authz, Sessions: f.revoker, Events: f.events,
		Logger: silentLogger(),
	})
	f.users.seed(&iface.User{UUID: pinWorkerUUID, Email: "mario@example.com", Username: pinWorker, Role: "operator", IsActive: true})
	actors.seed(&iface.User{UUID: "admin-1", Email: "admin@example.com", Username: "admin", Role: "administrator", IsActive: true})
	return f
}

func (f *pinFixture) user(t *testing.T) *iface.User {
	t.Helper()
	u, err := f.users.GetUserByID(context.Background(), pinWorkerUUID)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// enrolledDevice sets the worker's PIN and enrolls a device through the
// real code flow, returning the device secret.
func (f *pinFixture) enrolledDevice(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatalf("AdminSetPIN: %v", err)
	}
	code, _, err := f.svc.IssueEnrollmentCode(ctx, "admin-1", pinWorkerUUID, false, nil)
	if err != nil {
		t.Fatalf("IssueEnrollmentCode: %v", err)
	}
	secret, err := f.svc.EnrollDevice(ctx, PINDeviceEnrollInput{Username: pinWorker, Code: strings.ToLower(code), DeviceID: "tab-1", DeviceName: "Tablet 1", Platform: "android", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("EnrollDevice: %v", err)
	}
	return secret
}

func (f *pinFixture) login(pin, secret string) (*authModels.TokenResponse, error) {
	return f.svc.Login(context.Background(), PINLoginInput{
		Username: pinWorker, PIN: pin, DeviceID: "tab-1", DeviceSecret: secret, Platform: "android", IP: "10.0.0.1",
	})
}

// --- tests ---

func TestPIN_FullFlowMintsAPINSession(t *testing.T) {
	f := newPINFixture(t, nil)
	secret := f.enrolledDevice(t)

	resp, err := f.login(goodPIN, secret)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	claims, err := f.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.AMR) != 1 || claims.AMR[0] != authModels.AMRPIN || claims.LastOTPAt != 0 {
		t.Fatalf("amr=%v last_otp_at=%d, want [pin] and 0", claims.AMR, claims.LastOTPAt)
	}
	if resp.ExpiresIn != int64(DefaultPINAccessTokenTTL.Seconds()) || resp.RefreshExpiresIn != int64(DefaultPINRefreshTokenTTL.Seconds()) {
		t.Fatalf("expiresIn=%d refreshExpiresIn=%d", resp.ExpiresIn, resp.RefreshExpiresIn)
	}
	if f.refresh.created == nil || f.refresh.created.LoginMethod != authModels.LoginMethodPIN {
		t.Fatalf("refresh row loginMethod not pin: %+v", f.refresh.created)
	}
	if d := f.refresh.created.ExpiresAt.Sub(f.refresh.created.IssuedAt); d != DefaultPINRefreshTokenTTL {
		t.Fatalf("refresh row lifetime %v, want %v", d, DefaultPINRefreshTokenTTL)
	}
	if f.sessions.created == nil || f.sessions.created.LoginMethod != authModels.LoginMethodPIN || f.sessions.created.DeviceID != "tab-1" {
		t.Fatalf("session doc: %+v", f.sessions.created)
	}
	//tenantscope:allow fake repository, user-keyed
	dev, _ := f.devices.Find(context.Background(), pinWorkerUUID, "tab-1")
	if dev.LastUsedAt == nil || dev.Via != authModels.PINDeviceViaAdmin || dev.EnrolledBy != "admin-1" {
		t.Fatalf("device row: %+v", dev)
	}
	if strings.Contains(dev.SecretHash, secret) || dev.SecretHash == "" {
		t.Fatal("device secret stored in clear")
	}
}

func TestPIN_StoredAsArgonHashNeverPlain(t *testing.T) {
	f := newPINFixture(t, nil)
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	h := f.user(t).PINHash
	if !strings.HasPrefix(h, "$argon2id$") || strings.Contains(h, goodPIN) {
		t.Fatalf("pin hash %q", h)
	}
	if !f.events.has("admin_pin_set") || len(f.revoker.calls) != 1 {
		t.Fatalf("events=%v revocations=%v", f.events.events, f.revoker.calls)
	}
}

func TestPIN_NewPINValidation(t *testing.T) {
	f := newPINFixture(t, nil)
	for pin, want := range map[string]error{
		"1234":   ErrPINInvalid, // length is 5 here
		"123456": ErrPINInvalid,
		"12a45":  ErrPINInvalid,
		"11111":  ErrPINTooWeak,
		"12345":  ErrPINTooWeak,
		"98765":  ErrPINTooWeak,
		"48213":  nil,
	} {
		if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, pin); !errors.Is(err, want) {
			t.Fatalf("pin %q: got %v, want %v", pin, err, want)
		}
	}
}

func TestPIN_EligibilityRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(f *pinFixture, u *iface.User)
		want error
	}{
		{"super_admin", func(f *pinFixture, u *iface.User) {
			u.Role = "super_admin"
			f.users.byUUID["admin-1"].Role = "super_admin" // past the target-tier guard
		}, ErrPINNotAllowed},
		{"administrator", func(_ *pinFixture, u *iface.User) { u.Role = "administrator" }, ErrPINNotAllowed},
		{"service principal", func(_ *pinFixture, u *iface.User) { u.Kind = iface.UserKindService }, ErrPINNotAllowed},
		{"no permission", func(f *pinFixture, u *iface.User) { f.authz.deny[u.UUID] = true }, ErrPINNotAllowed},
		{"no username", func(_ *pinFixture, u *iface.User) { u.Username = "" }, ErrPINUsernameRequired},
		{"shared username", func(f *pinFixture, u *iface.User) {
			f.users.seed(&iface.User{UUID: "other", Email: "o@example.com", Username: u.Username, Role: "operator", IsActive: true})
		}, ErrPINUsernameNotUnique},
		{"authz outage", func(f *pinFixture, _ *iface.User) { f.authz.err = errors.New("down") }, ErrAuthPolicyUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newPINFixture(t, nil)
			c.edit(f, f.user(t))
			if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if f.user(t).PINHash != "" {
				t.Fatal("pin written despite refusal")
			}
		})
	}
}

func TestPIN_ConfiguredMFARoleRefused(t *testing.T) {
	f := newPINFixture(t, map[string]string{"mfaEnabled": "true", "mfaRequiredForRoles": "operator"})
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); !errors.Is(err, ErrPINNotAllowed) {
		t.Fatalf("got %v, want ErrPINNotAllowed for a role MFA is required for", err)
	}
}

func TestPIN_SurfaceToggle(t *testing.T) {
	f := newPINFixture(t, map[string]string{"pinLoginEnabledAdmin": "false"})
	// An admin may prepare a PIN while the method is off…
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	// …but nobody can sign in or enroll a device, and nothing is counted.
	if _, err := f.login(goodPIN, "x"); !errors.Is(err, ErrPINLoginDisabled) {
		t.Fatalf("login: %v", err)
	}
	if _, err := f.svc.EnrollDevice(context.Background(), PINDeviceEnrollInput{Username: pinWorker, Code: "AAAA-AAAA", DeviceID: "d"}); !errors.Is(err, ErrPINLoginDisabled) {
		t.Fatalf("enroll: %v", err)
	}
	if f.pwd.verifyCalls() != 0 {
		t.Fatal("the disabled gate must cost nothing and verify nothing")
	}

	absent := newPINFixture(t, map[string]string{})
	delete(absent.svc.policy.cs.(fixedValueReader).values, "pinLoginEnabledAdmin")
	if _, err := absent.login(goodPIN, "x"); !errors.Is(err, ErrPINLoginDisabled) {
		t.Fatalf("absent toggle must mean off, got %v", err)
	}
	broken := newPINFixture(t, map[string]string{"pinLoginEnabledAdmin": "yes"})
	if _, err := broken.login(goodPIN, "x"); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("malformed toggle must be an outage, got %v", err)
	}
}

func TestPIN_DeviceBinding(t *testing.T) {
	f := newPINFixture(t, nil)
	secret := f.enrolledDevice(t)

	for name, in := range map[string]PINLoginInput{
		"no secret":      {Username: pinWorker, PIN: goodPIN, DeviceID: "tab-1"},
		"wrong secret":   {Username: pinWorker, PIN: goodPIN, DeviceID: "tab-1", DeviceSecret: secret + "x"},
		"unknown device": {Username: pinWorker, PIN: goodPIN, DeviceID: "tab-2", DeviceSecret: secret},
	} {
		before := f.pwd.verifyCalls()
		if _, err := f.svc.Login(context.Background(), in); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: got %v", name, err)
		}
		// One verify, against the DUMMY: the PIN itself is never checked
		// without a valid device secret.
		if got := f.pwd.verifyCalls() - before; got != 1 {
			t.Fatalf("%s: %d verifies, want exactly one dummy", name, got)
		}
	}
	// Not a PIN guess: rate-limited on the counters, never on the durable
	// count — otherwise a username alone could walk the PIN to its cap.
	if f.user(t).PINFailedCount != 0 {
		t.Fatalf("device failures reached the durable PIN count: %d", f.user(t).PINFailedCount)
	}
	if v, _ := f.counter.Locked(context.Background(), AttemptKeyPIN(PolicyAudienceOperator, pinWorker), Limit{Threshold: 3, Window: time.Minute}); !v.Locked {
		t.Fatal("device failures not charged to the attempt counter")
	}

	// Binding off: the PIN works from anywhere.
	open := newPINFixture(t, map[string]string{"pinRequiresTrustedDevice": "false"})
	if err := open.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	if _, err := open.svc.Login(context.Background(), PINLoginInput{Username: pinWorker, PIN: goodPIN, IP: "10.0.0.2"}); err != nil {
		t.Fatalf("binding off: %v", err)
	}
}

func TestPIN_WrongPINLocksThenRemovesPIN(t *testing.T) {
	f := newPINFixture(t, map[string]string{"accountLockoutThreshold": "5", "ipLockoutThreshold": "1000"})
	secret := f.enrolledDevice(t)

	for i := 0; i < 5; i++ {
		if _, err := f.login("99999", secret); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// Counter and durable lock both engage on the fifth failure.
	_, err := f.login(goodPIN, secret)
	if !errors.Is(err, ErrAccountLocked) || RetryAfterFor(err) <= 0 {
		t.Fatalf("sixth attempt: got %v, want a lockout with Retry-After", err)
	}
	if f.user(t).PINLockedUntil == nil {
		t.Fatal("durable PIN lock not stamped")
	}
	if f.user(t).LockedUntil != nil || f.user(t).FailedLoginCount != 0 {
		t.Fatal("PIN failures touched the password lock")
	}

	// After the window, five more wrong guesses reach the cap; the next
	// attempt — even with the right PIN — is refused unverified and removes it.
	unlock := func() {
		_ = f.counter.Reset(context.Background(), AttemptKeyPIN(PolicyAudienceOperator, pinWorker))
		_ = f.store.with(pinWorkerUUID, func(u *iface.User) { past := time.Now().Add(-time.Minute); u.PINLockedUntil = &past })
	}
	unlock()
	for i := 0; i < 5; i++ {
		_, _ = f.login("99999", secret)
	}
	if f.user(t).PINHash == "" || f.user(t).PINFailedCount != PINMaxConsecutiveFailures {
		t.Fatalf("after %d failures: hash set=%v count=%d", PINMaxConsecutiveFailures, f.user(t).PINHash != "", f.user(t).PINFailedCount)
	}
	unlock()
	if _, err := f.login(goodPIN, secret); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("attempt past the cap: %v", err)
	}
	if f.user(t).PINHash != "" {
		t.Fatalf("PIN survived %d consecutive failures", PINMaxConsecutiveFailures)
	}
	if !f.events.has("pin_disabled_after_failures") {
		t.Fatal("removal not audited")
	}
	if devs, _ := f.devices.ListByUser(context.Background(), pinWorkerUUID); len(devs) != 0 {
		t.Fatal("devices survived the PIN removal")
	}
}

func TestPIN_UnknownAndKnownUsernameLookAlike(t *testing.T) {
	f := newPINFixture(t, map[string]string{"pinRequiresTrustedDevice": "false"})
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{pinWorker, "nobody"} {
		before := f.pwd.verifyCalls()
		_, err := f.svc.Login(context.Background(), PINLoginInput{Username: name, PIN: "99999", IP: "10.0.0.3"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: %v", name, err)
		}
		if f.pwd.verifyCalls()-before != 1 {
			t.Fatalf("%s: verify count differs", name)
		}
	}
}

func TestPIN_CorrectPINButNotAllowed(t *testing.T) {
	f := newPINFixture(t, nil)
	secret := f.enrolledDevice(t)
	f.authz.deny[pinWorkerUUID] = true // permission revoked after the PIN was set
	if _, err := f.login(goodPIN, secret); !errors.Is(err, ErrPINNotAllowed) {
		t.Fatalf("got %v, want ErrPINNotAllowed", err)
	}
	if f.refresh.created != nil {
		t.Fatal("tokens minted for a user without auth.pin_login")
	}
}

func TestPIN_PINSessionCannotManageCredentials(t *testing.T) {
	f := newPINFixture(t, nil)
	f.enrolledDevice(t)
	pinAMR := []string{authModels.AMRPIN}
	if err := f.svc.ChangeOwnPIN(context.Background(), pinWorkerUUID, pinAMR, "73915"); !errors.Is(err, ErrPINSessionForbidden) {
		t.Fatalf("change: %v", err)
	}
	if err := f.svc.RemoveOwnPIN(context.Background(), pinWorkerUUID, pinAMR); !errors.Is(err, ErrPINSessionForbidden) {
		t.Fatalf("remove: %v", err)
	}
	if _, _, err := f.svc.IssueEnrollmentCode(context.Background(), pinWorkerUUID, pinWorkerUUID, true, []string{authModels.AMRPIN, authModels.AMRReauth}); !errors.Is(err, ErrPINSessionForbidden) {
		t.Fatalf("enrollment code: %v", err)
	}
	// A password session may.
	if err := f.svc.ChangeOwnPIN(context.Background(), pinWorkerUUID, []string{authModels.AMRPassword}, "73915"); err != nil {
		t.Fatalf("password session change: %v", err)
	}
	if !f.events.has("self_pin_changed") {
		t.Fatal("self change not audited")
	}
}

func TestPIN_EnrollmentCodeIsSingleUseAndGuessesDoNotBurnIt(t *testing.T) {
	f := newPINFixture(t, nil)
	ctx := context.Background()
	if err := f.svc.AdminSetPIN(ctx, "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	code, expires, err := f.svc.IssueEnrollmentCode(ctx, "admin-1", pinWorkerUUID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 9 || code[4] != '-' || time.Until(expires) > PINEnrollmentCodeTTL {
		t.Fatalf("code %q expires %v", code, expires)
	}
	if _, err := f.svc.EnrollDevice(ctx, PINDeviceEnrollInput{Username: pinWorker, Code: "ZZZZ-ZZZZ", DeviceID: "tab-9"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := f.svc.EnrollDevice(ctx, PINDeviceEnrollInput{Username: pinWorker, Code: code, DeviceID: "tab-9"}); err != nil {
		t.Fatalf("real code after a wrong guess: %v", err)
	}
	if _, err := f.svc.EnrollDevice(ctx, PINDeviceEnrollInput{Username: pinWorker, Code: code, DeviceID: "tab-10"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("code reused: %v", err)
	}
}

func TestPIN_EnrollmentCodeNeedsAPIN(t *testing.T) {
	f := newPINFixture(t, nil)
	if _, _, err := f.svc.IssueEnrollmentCode(context.Background(), "admin-1", pinWorkerUUID, false, nil); !errors.Is(err, ErrPINNotSet) {
		t.Fatalf("got %v, want ErrPINNotSet", err)
	}
}

func TestPIN_RemovalAndDeviceRevocationEndSessions(t *testing.T) {
	f := newPINFixture(t, nil)
	f.enrolledDevice(t)
	ctx := context.Background()
	f.revoker.calls = nil

	if err := f.svc.RevokeDevice(ctx, "admin-1", pinWorkerUUID, "tab-1", false); err != nil {
		t.Fatal(err)
	}
	if len(f.revoker.calls) != 1 || f.revoker.calls[0] != pinWorkerUUID+"|tab-1|pin_device_revoked" {
		t.Fatalf("device revocation: %v", f.revoker.calls)
	}
	if err := f.svc.RevokeDevice(ctx, "admin-1", pinWorkerUUID, "tab-1", false); !errors.Is(err, ErrPINDeviceNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if err := f.svc.AdminRemovePIN(ctx, "admin-1", pinWorkerUUID); err != nil {
		t.Fatal(err)
	}
	if f.user(t).PINHash != "" || !f.events.has("admin_pin_removed") {
		t.Fatal("pin not removed or not audited")
	}
	if err := f.svc.AdminRemovePIN(ctx, "admin-1", pinWorkerUUID); !errors.Is(err, ErrPINNotSet) {
		t.Fatalf("second removal: %v", err)
	}
}

func TestPIN_ViewNeverLeaksSecrets(t *testing.T) {
	f := newPINFixture(t, nil)
	f.enrolledDevice(t)
	v := f.svc.View(context.Background(), f.user(t))
	if !v.Available || !v.Set || v.Length != 5 || !v.RequiresTrustedDevice || len(v.Devices) != 1 {
		t.Fatalf("view: %+v", v)
	}
	if v.Devices[0].SecretHash == "" {
		t.Fatal("fixture should carry the hash internally")
	}
	// The JSON shape hides it: json:"-" on SecretHash and UserUUID.
}

func TestPIN_AdminCannotReachAboveTheirRole(t *testing.T) {
	f := newPINFixture(t, nil)
	ctx := context.Background()
	f.users.seed(&iface.User{UUID: "mgr-1", Email: "m@example.com", Username: "mgr", Role: "manager", IsActive: true})
	f.users.seed(&iface.User{UUID: "dev-1", Email: "d@example.com", Username: "dev", Role: "developer", IsActive: true})
	if err := f.svc.AdminSetPIN(ctx, "mgr-1", "dev-1", goodPIN); !errors.Is(err, ErrPINTargetAboveCaller) {
		t.Fatalf("manager on developer: %v", err)
	}
	if err := f.svc.AdminSetPIN(ctx, "mgr-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatalf("manager on operator: %v", err)
	}
	if err := f.svc.AdminSetPIN(ctx, "ghost", pinWorkerUUID, goodPIN); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("unknown caller: %v", err)
	}
	for role, want := range map[string]int{"super_admin": 5, "administrator": 4, "developer": 3, "manager": 2, "operator": 1, "guest": 0, "custom": -1} {
		if got := pinRoleTier(role); got != want {
			t.Fatalf("pinRoleTier(%q)=%d, want %d (keep in step with user/handlers systemRoleTier)", role, got, want)
		}
	}
}

// passwordVerifyCounter tells real verifications from the dummy ones.
type passwordVerifyCounter struct {
	PasswordService
	mu          sync.Mutex
	real, dummy int
}

func (c *passwordVerifyCounter) Verify(plain, encoded string) (bool, error) {
	c.mu.Lock()
	if encoded == c.DummyHash() {
		c.dummy++
	} else {
		c.real++
	}
	c.mu.Unlock()
	return c.PasswordService.Verify(plain, encoded)
}

// A parallel burst gets at most the cap of real PIN verifications: the
// attempt is reserved atomically before the verify, so concurrent
// requests cannot all decide on the same stale count.
func TestPIN_ParallelBurstIsBoundedByTheCap(t *testing.T) {
	f := newPINFixture(t, map[string]string{"pinRequiresTrustedDevice": "false", "accountLockoutThreshold": "1000", "ipLockoutThreshold": "1000"})
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatal(err)
	}
	counter := &passwordVerifyCounter{PasswordService: f.svc.passwords}
	f.svc.passwords = counter
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.svc.Login(context.Background(), PINLoginInput{Username: pinWorker, PIN: "99999", IP: "10.0.0.9"})
		}()
	}
	wg.Wait()
	if counter.real > PINMaxConsecutiveFailures {
		t.Fatalf("%d real verifications in a burst, want at most %d", counter.real, PINMaxConsecutiveFailures)
	}
	if f.user(t).PINHash != "" {
		t.Fatal("a burst past the cap must remove the PIN")
	}
}

func TestPIN_FieldRolesOnly(t *testing.T) {
	for _, role := range []string{"manager", "developer", "custom"} {
		f := newPINFixture(t, nil)
		f.user(t).Role = role
		if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); !errors.Is(err, ErrPINNotAllowed) {
			t.Fatalf("%s: %v, want ErrPINNotAllowed", role, err)
		}
	}
	f := newPINFixture(t, nil)
	f.user(t).Role = "guest"
	if err := f.svc.AdminSetPIN(context.Background(), "admin-1", pinWorkerUUID, goodPIN); err != nil {
		t.Fatalf("guest: %v", err)
	}
}

// On the client tier the administrator is an operator user (separate
// store) and auth.pin_login is not evaluated — the authz evaluator cannot
// answer it for a client user.
func TestPIN_ClientTier(t *testing.T) {
	f := newPINFixtureFor(t, nil, PolicyAudienceClient)
	f.authz.deny[pinWorkerUUID] = true
	secret := f.enrolledDevice(t)
	if _, err := f.login(goodPIN, secret); err != nil {
		t.Fatalf("client PIN sign-in: %v", err)
	}
	if _, err := f.users.GetUserByID(context.Background(), "admin-1"); err == nil {
		t.Fatal("fixture: the administrator must not be in the client store")
	}
}

func TestPIN_ReenrollingADeviceEndsItsSessions(t *testing.T) {
	f := newPINFixture(t, nil)
	f.enrolledDevice(t)
	f.revoker.calls = nil
	code, _, err := f.svc.IssueEnrollmentCode(context.Background(), "admin-1", pinWorkerUUID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EnrollDevice(context.Background(), PINDeviceEnrollInput{Username: pinWorker, Code: code, DeviceID: "tab-1"}); err != nil {
		t.Fatal(err)
	}
	if len(f.revoker.calls) != 1 || f.revoker.calls[0] != pinWorkerUUID+"|tab-1|pin_device_reenrolled" {
		t.Fatalf("revocations: %v", f.revoker.calls)
	}
}
