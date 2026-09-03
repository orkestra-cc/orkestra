package services

// Fakes for ServiceAccountService, modeled on gates_fakes_test.go: plain
// in-memory maps, no Mongo/Redis, and any UserProvider/PasswordService
// method the lifecycle paths don't exercise panics loudly so a future
// refactor that reaches a new dependency surfaces immediately.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- user fake -------------------------------------------------------

// errSAUserNotFound stands in for what a conforming iface.UserProvider
// returns for an account that is genuinely gone. It WRAPS the SDK sentinel
// because requireServiceAccount now classifies against it (spec §8 #17):
// with a free-standing error here, every unknown id would be answered as an
// outage instead of a 404 — the exact inversion of the bug being fixed.
var errSAUserNotFound = fmt.Errorf("service account test: user not found: %w", iface.ErrUserNotFound)

// errSAStoreDown is the other half of the same split: an infrastructure
// failure, which must NOT be answered as a verdict on the account.
var errSAStoreDown = errors.New("mongo: no reachable servers")

// saUserFake implements iface.UserProvider (fully, to satisfy the
// interface) plus iface.ServiceAccountLister. Only the methods the
// lifecycle service actually calls have real bodies; everything else
// panics.
type saUserFake struct {
	mu          sync.Mutex
	byEmail     map[string]*iface.User
	byUUID      map[string]*iface.User
	createCalls []iface.CreateUserInput
	updateCalls []iface.UpdateUserInput
	// createErr, when set, makes CreateUserWithPassword fail — used by
	// TestCreateAccountRaceReturnsConflict (Item 2) to simulate a
	// concurrent caller winning the insert race between CreateAccount's
	// existence pre-check and this call. Unless skipRaceSeed is set, the
	// fake also seeds the "winning" row as a side effect, mirroring what
	// a real duplicate-key failure implies: the row exists by the time
	// the caller re-checks, even though this call itself reports
	// failure.
	createErr    error
	skipRaceSeed bool
	// getByIDErr, when set, makes GetUserByID fail with an infrastructure
	// error instead of resolving the map — the input that used to reach an
	// operator as "service account not found".
	getByIDErr error
}

// setGetByIDErr breaks the directory read every lifecycle method gates on.
func (f *saUserFake) setGetByIDErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getByIDErr = err
}

func newSAUserFake() *saUserFake {
	return &saUserFake{
		byEmail: map[string]*iface.User{},
		byUUID:  map[string]*iface.User{},
	}
}

// seed pre-populates a user (human or service) without going through
// CreateUserWithPassword — used to set up "already exists" / "human
// target" / "pre-existing service account" fixtures.
func (f *saUserFake) seed(u *iface.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byEmail[u.Email] = u
	f.byUUID[u.UUID] = u
}

func (f *saUserFake) GetUserForAuth(_ context.Context, email string) (*iface.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byEmail[email]; ok {
		return u, nil
	}
	return nil, errSAUserNotFound
}

func (f *saUserFake) CreateUserWithPassword(_ context.Context, in *iface.CreateUserInput) (*iface.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, *in)

	if f.createErr != nil {
		if !f.skipRaceSeed {
			winner := &iface.User{
				UUID: uuid.NewString(), Email: in.Email, FullName: in.FullName,
				Role: in.Role, Kind: in.Kind, IsActive: true,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			f.byEmail[winner.Email] = winner
			f.byUUID[winner.UUID] = winner
		}
		return nil, f.createErr
	}

	uid := in.UUID
	if uid == "" {
		uid = uuid.NewString()
	}
	u := &iface.User{
		UUID:         uid,
		Email:        in.Email,
		FullName:     in.FullName,
		Role:         in.Role,
		Kind:         in.Kind,
		PasswordHash: in.PasswordHash,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.byEmail[u.Email] = u
	f.byUUID[u.UUID] = u
	return u, nil
}

func (f *saUserFake) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	if u, ok := f.byUUID[id]; ok {
		return u, nil
	}
	return nil, errSAUserNotFound
}

func (f *saUserFake) UpdateUser(_ context.Context, id string, in *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byUUID[id]
	if !ok {
		return nil, errSAUserNotFound
	}
	if in.FullName != "" {
		u.FullName = in.FullName
	}
	if in.IsActive != nil {
		u.IsActive = *in.IsActive
	}
	f.updateCalls = append(f.updateCalls, *in)
	return u.ToResponse(), nil
}

// ListUsersByKind satisfies iface.ServiceAccountLister.
func (f *saUserFake) ListUsersByKind(_ context.Context, kind string) ([]iface.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []iface.User
	for _, u := range f.byUUID {
		if u.Kind == kind {
			out = append(out, *u)
		}
	}
	return out, nil
}

// Unexercised UserProvider methods — panic loudly if a refactor reaches
// them.
func (f *saUserFake) GetUserByEmail(context.Context, string) (*iface.UserManagementResponse, error) {
	panic("not used")
}
func (f *saUserFake) CreateUserFromOAuth(context.Context, *iface.CreateUserInput) (*iface.User, error) {
	panic("not used")
}
func (f *saUserFake) UpdatePasswordHash(context.Context, string, string) error { panic("not used") }
func (f *saUserFake) MarkEmailVerified(context.Context, string) error          { panic("not used") }
func (f *saUserFake) RecordFailedLogin(context.Context, string, *time.Time) error {
	panic("not used")
}
func (f *saUserFake) ClearFailedLogins(context.Context, string) error   { panic("not used") }
func (f *saUserFake) UpdateUserLastLogin(context.Context, string) error { panic("not used") }
func (f *saUserFake) DeleteUser(context.Context, string) error          { panic("not used") }
func (f *saUserFake) SoftDeleteAndAliasEmail(context.Context, string) error {
	panic("not used")
}
func (f *saUserFake) GetUserOAuthLinks(context.Context, string) ([]iface.OAuthLink, error) {
	panic("not used")
}
func (f *saUserFake) AddOAuthLinkToUser(context.Context, string, iface.OAuthLink) error {
	panic("not used")
}
func (f *saUserFake) RemoveOAuthLinkFromUser(context.Context, string, iface.OAuthProvider, string) error {
	panic("not used")
}
func (f *saUserFake) SetPrimaryOAuthLink(context.Context, string, iface.OAuthProvider, string) error {
	panic("not used")
}
func (f *saUserFake) GetUserCount(context.Context, *iface.UserFilters) (int64, error) {
	panic("not used")
}
func (f *saUserFake) StartMFAGraceIfUnset(context.Context, string) error { panic("not used") }
func (f *saUserFake) ResetMFAGrace(context.Context, string) error        { panic("not used") }
func (f *saUserFake) ClearMFAGrace(context.Context, string) error        { panic("not used") }

// --- credential repository fake --------------------------------------

// saCredRepoFake implements repository.ServiceAccountCredentialRepository
// with plain in-memory maps keyed by UUID and by clientId (both entries
// point at the same underlying row, so a Revoke/StampLastUsed mutation
// is visible through either key — mirroring the single-collection
// behaviour of the real Mongo-backed repository).
type saCredRepoFake struct {
	mu          sync.Mutex
	byUUID      map[string]*models.ServiceAccountCredential
	byClientID  map[string]*models.ServiceAccountCredential
	createCalls []models.ServiceAccountCredential
	// stampCalls records every credentialUUID passed to StampLastUsed —
	// Task 7's Grant happy path asserts this fired exactly once.
	stampCalls []string
}

func newSACredRepoFake() *saCredRepoFake {
	return &saCredRepoFake{
		byUUID:     map[string]*models.ServiceAccountCredential{},
		byClientID: map[string]*models.ServiceAccountCredential{},
	}
}

// seed installs a credential row directly, bypassing Create — used to
// set up "already has N active credentials" / cross-account fixtures.
func (f *saCredRepoFake) seed(c *models.ServiceAccountCredential) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.byUUID[cp.UUID] = &cp
	f.byClientID[cp.ClientID] = &cp
}

func (f *saCredRepoFake) Create(_ context.Context, doc *models.ServiceAccountCredential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	cp := *doc
	f.byUUID[cp.UUID] = &cp
	f.byClientID[cp.ClientID] = &cp
	f.createCalls = append(f.createCalls, cp)
	return nil
}

func (f *saCredRepoFake) GetByClientID(_ context.Context, clientID string) (*models.ServiceAccountCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byClientID[clientID]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, repository.ErrServiceAccountCredentialNotFound
}

func (f *saCredRepoFake) ListByUser(_ context.Context, userUUID string) ([]models.ServiceAccountCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.ServiceAccountCredential
	for _, c := range f.byUUID {
		if c.UserUUID == userUUID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *saCredRepoFake) CountActiveByUser(_ context.Context, userUUID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, c := range f.byUUID {
		if c.UserUUID == userUUID && c.RevokedAt == nil {
			n++
		}
	}
	return n, nil
}

func (f *saCredRepoFake) Revoke(_ context.Context, credentialUUID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byUUID[credentialUUID]
	if !ok || c.RevokedAt != nil {
		return repository.ErrServiceAccountCredentialNotFound
	}
	t := at
	c.RevokedAt = &t
	return nil
}

func (f *saCredRepoFake) StampLastUsed(_ context.Context, credentialUUID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stampCalls = append(f.stampCalls, credentialUUID)
	c, ok := f.byUUID[credentialUUID]
	if !ok {
		return repository.ErrServiceAccountCredentialNotFound
	}
	t := at
	c.LastUsedAt = &t
	return nil
}

// countStampCalls reports how many times StampLastUsed was invoked for
// the given credential UUID.
func (f *saCredRepoFake) countStampCalls(credentialUUID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.stampCalls {
		if id == credentialUUID {
			n++
		}
	}
	return n
}

// --- PasswordService (hasher) fake ------------------------------------

// saHasherFake is a trivial, non-cryptographic stand-in so tests can
// assert the exact hash the service persisted. ValidatePolicy panics —
// the service must never call it on a generated machine secret (that
// method enforces human-password rules: length/complexity/HIBP).
//
// Pointer receiver + verifyCalls (Task 7): Grant's timing-parity dummy
// verify on an unknown/revoked credential is only observable as a
// recorded call against DummyHash() — there is no other return-value
// signal that it happened — so the fake tracks every Verify call.
type saHasherFake struct {
	mu          sync.Mutex
	verifyCalls []saVerifyCall
}

// saVerifyCall is one recorded (plaintext, encoded) pair passed to
// saHasherFake.Verify.
type saVerifyCall struct {
	Plaintext string
	Encoded   string
}

func (f *saHasherFake) Hash(plaintext string) (string, error) { return "h:" + plaintext, nil }
func (f *saHasherFake) Verify(plaintext, encoded string) (bool, error) {
	f.mu.Lock()
	f.verifyCalls = append(f.verifyCalls, saVerifyCall{Plaintext: plaintext, Encoded: encoded})
	f.mu.Unlock()
	return encoded == "h:"+plaintext, nil
}
func (f *saHasherFake) NeedsRehash(string) bool { panic("not used") }
func (f *saHasherFake) ValidatePolicy(context.Context, string, string) error {
	panic("ValidatePolicy must never be called on a service-account secret")
}
func (f *saHasherFake) DummyHash() string            { return "h:dummy" }
func (f *saHasherFake) SetPolicy(*AuthPolicyService) { panic("not used") }

// --- ServiceTokenMinter fake -------------------------------------------

type saMinterFake struct{}

func (saMinterFake) GenerateAccessToken(user *iface.User) (string, error) {
	return "tok-" + user.UUID, nil
}
func (saMinterFake) AccessTokenTTL(context.Context) time.Duration { return 900 * time.Second }

// --- compliance audit sink spy (Item 1) --------------------------------

// saAuditSinkFake implements iface.AuditSink, recording every Emit call
// so tests can assert the three-lane audit mechanism actually fired
// without standing up a real compliance module.
type saAuditSinkFake struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (f *saAuditSinkFake) Emit(_ context.Context, event iface.AuditEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

// last returns the most recently recorded event, if any.
func (f *saAuditSinkFake) last() (iface.AuditEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		return iface.AuditEvent{}, false
	}
	return f.events[len(f.events)-1], true
}

// --- security-event repository spy (Item 1) -----------------------------

// saSecurityEventRepoFake implements repository.SecurityEventRepository,
// recording every Insert call. Only Insert is exercised by
// ServiceAccountService; the rest panic loudly per this file's existing
// unexercised-method convention.
type saSecurityEventRepoFake struct {
	mu     sync.Mutex
	events []*models.SecurityEvent
}

func (f *saSecurityEventRepoFake) Insert(_ context.Context, event *models.SecurityEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

func (f *saSecurityEventRepoFake) ListByUser(context.Context, string, int) ([]*models.SecurityEvent, error) {
	panic("not used")
}
func (f *saSecurityEventRepoFake) ListByUserPaged(context.Context, string, int, int, *time.Time) ([]*models.SecurityEvent, int64, error) {
	panic("not used")
}
func (f *saSecurityEventRepoFake) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("not used")
}

// --- shared test scaffolding -------------------------------------------

func newSAService() (*ServiceAccountService, *saUserFake, *saCredRepoFake) {
	users := newSAUserFake()
	creds := newSACredRepoFake()
	svc := NewServiceAccountService(creds, users, users, &saHasherFake{}, saMinterFake{}, NewMemoryAttemptCounter())
	return svc, users, creds
}

func seedServiceUser(users *saUserFake, email, fullName string) *iface.User {
	u := &iface.User{
		UUID:      uuid.NewString(),
		Email:     email,
		FullName:  fullName,
		Role:      "guest",
		Kind:      iface.UserKindService,
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	users.seed(u)
	return u
}

func seedHumanUser(users *saUserFake, email, fullName string) *iface.User {
	u := &iface.User{
		UUID:      uuid.NewString(),
		Email:     email,
		FullName:  fullName,
		Role:      "operator",
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	users.seed(u)
	return u
}

func seedCredential(creds *saCredRepoFake, userUUID, clientID, label string) *models.ServiceAccountCredential {
	c := &models.ServiceAccountCredential{
		UUID:      uuid.NewString(),
		UserUUID:  userUUID,
		ClientID:  clientID,
		Label:     label,
		CreatedAt: time.Now(),
	}
	creds.seed(c)
	return c
}

// --- tests ---------------------------------------------------------------

func TestCreateAccountMintsUserAndCredential(t *testing.T) {
	svc, users, creds := newSAService()
	ctx := context.Background()

	acct, err := svc.CreateAccount(ctx, "Reporting Bot")
	if err != nil {
		t.Fatalf("CreateAccount: unexpected error: %v", err)
	}

	if len(users.createCalls) != 1 {
		t.Fatalf("expected exactly 1 CreateUserWithPassword call, got %d", len(users.createCalls))
	}
	in := users.createCalls[0]
	if in.Email != "sa-reporting-bot@service.invalid" {
		t.Errorf("Email = %q, want sa-reporting-bot@service.invalid", in.Email)
	}
	if in.FullName != "Reporting Bot" {
		t.Errorf("FullName = %q, want Reporting Bot", in.FullName)
	}
	if in.Role != "guest" {
		t.Errorf("Role = %q, want guest", in.Role)
	}
	if in.Kind != iface.UserKindService {
		t.Errorf("Kind = %q, want %q", in.Kind, iface.UserKindService)
	}
	if in.PasswordHash != "" {
		t.Errorf("PasswordHash = %q, want empty", in.PasswordHash)
	}

	if len(creds.createCalls) != 1 {
		t.Fatalf("expected exactly 1 credential Create call, got %d", len(creds.createCalls))
	}
	cred := creds.createCalls[0]
	if !hasPrefix(cred.ClientID, "sa_") {
		t.Errorf("ClientID = %q, want sa_ prefix", cred.ClientID)
	}
	if cred.SecretHash != "h:"+acct.ClientSecret {
		t.Errorf("SecretHash = %q, want h:%s", cred.SecretHash, acct.ClientSecret)
	}
	if cred.Label != "initial" {
		t.Errorf("credential Label = %q, want initial", cred.Label)
	}

	if !hasPrefix(acct.ClientSecret, "sas_") || acct.ClientSecret == "sas_" {
		t.Errorf("ClientSecret = %q, want non-empty sas_ prefix", acct.ClientSecret)
	}
}

func TestCreateAccountRejectsDuplicate(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	seedHumanUser(users, "sa-reporting-bot@service.invalid", "Someone Else")

	_, err := svc.CreateAccount(ctx, "Reporting Bot")
	if !errors.Is(err, ErrAccountAlreadyExists) {
		t.Fatalf("CreateAccount duplicate: got %v, want ErrAccountAlreadyExists", err)
	}
}

func TestCreateAccountRejectsEmptySlug(t *testing.T) {
	svc, _, _ := newSAService()
	ctx := context.Background()

	_, err := svc.CreateAccount(ctx, "!!!")
	if !errors.Is(err, ErrInvalidAccountName) {
		t.Fatalf("CreateAccount(\"!!!\"): got %v, want ErrInvalidAccountName", err)
	}
}

func TestIssueCredentialCapsAtTwoActive(t *testing.T) {
	svc, users, creds := newSAService()
	ctx := context.Background()
	u := seedServiceUser(users, "sa-two-active@service.invalid", "Two Active")
	c1 := seedCredential(creds, u.UUID, "sa_existing1", "one")
	seedCredential(creds, u.UUID, "sa_existing2", "two")

	_, err := svc.IssueCredential(ctx, u.UUID, "")
	if !errors.Is(err, ErrTooManyActiveCredentials) {
		t.Fatalf("IssueCredential at cap: got %v, want ErrTooManyActiveCredentials", err)
	}

	if err := creds.Revoke(ctx, c1.UUID, time.Now()); err != nil {
		t.Fatalf("test setup: revoke: %v", err)
	}

	cred, err := svc.IssueCredential(ctx, u.UUID, "")
	if err != nil {
		t.Fatalf("IssueCredential after freeing a slot: unexpected error: %v", err)
	}
	if !hasPrefix(cred.ClientSecret, "sas_") || cred.ClientSecret == "sas_" {
		t.Errorf("ClientSecret = %q, want non-empty sas_ prefix", cred.ClientSecret)
	}
	if cred.Label != "rotated" {
		t.Errorf("default Label = %q, want rotated", cred.Label)
	}
}

func TestLifecycleRefusesHumanTargets(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	human := seedHumanUser(users, "person@example.com", "A Human")

	if _, err := svc.GetAccount(ctx, human.UUID); !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("GetAccount(human): got %v, want ErrServiceAccountNotFound", err)
	}
	active := true
	if _, err := svc.UpdateAccount(ctx, human.UUID, nil, &active); !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("UpdateAccount(human): got %v, want ErrServiceAccountNotFound", err)
	}
	if _, err := svc.IssueCredential(ctx, human.UUID, ""); !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("IssueCredential(human): got %v, want ErrServiceAccountNotFound", err)
	}
	if err := svc.RevokeCredential(ctx, human.UUID, "whatever"); !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("RevokeCredential(human): got %v, want ErrServiceAccountNotFound", err)
	}
}

func TestRevokeCredentialChecksOwnership(t *testing.T) {
	svc, users, creds := newSAService()
	ctx := context.Background()
	userA := seedServiceUser(users, "sa-owner-a@service.invalid", "Owner A")
	userB := seedServiceUser(users, "sa-owner-b@service.invalid", "Owner B")
	credA := seedCredential(creds, userA.UUID, "sa_ownedbya", "initial")

	err := svc.RevokeCredential(ctx, userB.UUID, credA.UUID)
	if !errors.Is(err, repository.ErrServiceAccountCredentialNotFound) {
		t.Fatalf("cross-account revoke: got %v, want ErrServiceAccountCredentialNotFound", err)
	}

	// Sanity: the legitimate owner can still revoke it.
	if err := svc.RevokeCredential(ctx, userA.UUID, credA.UUID); err != nil {
		t.Fatalf("owner revoke: unexpected error: %v", err)
	}
}

func TestUpdateAccountTogglesActive(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	u := seedServiceUser(users, "sa-toggle@service.invalid", "Toggle Me")

	inactive := false
	view, err := svc.UpdateAccount(ctx, u.UUID, nil, &inactive)
	if err != nil {
		t.Fatalf("UpdateAccount: unexpected error: %v", err)
	}

	if len(users.updateCalls) != 1 {
		t.Fatalf("expected exactly 1 UpdateUser call, got %d", len(users.updateCalls))
	}
	got := users.updateCalls[0]
	if got.IsActive == nil || *got.IsActive != false {
		t.Errorf("UpdateUserInput.IsActive = %v, want pointer to false", got.IsActive)
	}
	if view.IsActive {
		t.Errorf("returned AccountView.IsActive = true, want false")
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// TestCreateAccountRaceReturnsConflict is Item 2's regression test: a
// concurrent CreateAccount (or duplicate-key failure of any origin) that
// wins between the existence pre-check and CreateUserWithPassword must
// surface as ErrAccountAlreadyExists (409), not the raw create error
// (which would map to 500).
func TestCreateAccountRaceReturnsConflict(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	users.createErr = errors.New("duplicate key error collection: orkestra.operator_users index: email_1")

	_, err := svc.CreateAccount(ctx, "Race Target")
	if !errors.Is(err, ErrAccountAlreadyExists) {
		t.Fatalf("CreateAccount race: got %v, want ErrAccountAlreadyExists", err)
	}
}

// TestCreateAccountRacePropagatesGenuineError confirms the fallback path
// of Item 2's fix: when the re-check ALSO fails to find the user (a
// genuine, non-race error — e.g. a transient Mongo outage), the original
// error propagates unchanged rather than being swallowed into a
// misleading 409.
func TestCreateAccountRacePropagatesGenuineError(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	wantErr := errors.New("mongo: connection refused")
	users.createErr = wantErr
	users.skipRaceSeed = true // no concurrent winner — the re-check must still miss

	_, err := svc.CreateAccount(ctx, "Genuine Failure")
	if !errors.Is(err, wantErr) {
		t.Fatalf("CreateAccount genuine failure: got %v, want %v", err, wantErr)
	}
}

// --- audit emission (Item 1) --------------------------------------------

func newSAServiceWithAudit() (*ServiceAccountService, *saUserFake, *saCredRepoFake, *saAuditSinkFake, *saSecurityEventRepoFake) {
	svc, users, creds := newSAService()
	sink := &saAuditSinkFake{}
	eventRepo := &saSecurityEventRepoFake{}
	svc.SetAuditSink(sink)
	svc.SetSecurityEventRepo(eventRepo)
	return svc, users, creds, sink, eventRepo
}

func TestCreateAccountEmitsAuditEvent(t *testing.T) {
	svc, _, _, sink, eventRepo := newSAServiceWithAudit()
	ctx := context.WithValue(context.Background(), "userUUID", "actor-create")

	acct, err := svc.CreateAccount(ctx, "Audited Bot")
	if err != nil {
		t.Fatalf("CreateAccount: unexpected error: %v", err)
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatalf("expected an audit event, got none")
	}
	if ev.Action != "service_account.created" {
		t.Errorf("Action = %q, want service_account.created", ev.Action)
	}
	if ev.ResourceID != acct.ID {
		t.Errorf("ResourceID = %q, want %q", ev.ResourceID, acct.ID)
	}
	if ev.ActorUserID != "actor-create" {
		t.Errorf("ActorUserID = %q, want actor-create", ev.ActorUserID)
	}
	if ev.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", ev.Outcome)
	}
	if secret, ok := ev.Metadata["clientSecret"]; ok {
		t.Errorf("metadata leaked a secret field: %v", secret)
	}

	if len(eventRepo.events) != 1 {
		t.Fatalf("expected exactly 1 persisted security event, got %d", len(eventRepo.events))
	}
	if eventRepo.events[0].EventType != "service_account.created" {
		t.Errorf("persisted EventType = %q, want service_account.created", eventRepo.events[0].EventType)
	}
}

func TestUpdateAccountEmitsAuditEventWithChangedFields(t *testing.T) {
	svc, users, _, sink, _ := newSAServiceWithAudit()
	u := seedServiceUser(users, "sa-audit-update@service.invalid", "Audit Update")
	ctx := context.WithValue(context.Background(), "userUUID", "actor-update")

	inactive := false
	if _, err := svc.UpdateAccount(ctx, u.UUID, nil, &inactive); err != nil {
		t.Fatalf("UpdateAccount: unexpected error: %v", err)
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatalf("expected an audit event, got none")
	}
	if ev.Action != "service_account.updated" {
		t.Errorf("Action = %q, want service_account.updated", ev.Action)
	}
	changed, _ := ev.Metadata["changed"].([]string)
	if len(changed) != 1 || changed[0] != "active" {
		t.Errorf("Metadata[changed] = %v, want [active] (name untouched)", changed)
	}
	if active, _ := ev.Metadata["active"].(bool); active {
		t.Errorf("Metadata[active] = %v, want false (explicit transition)", ev.Metadata["active"])
	}
}

func TestIssueCredentialEmitsAuditEvent(t *testing.T) {
	svc, users, _, sink, _ := newSAServiceWithAudit()
	u := seedServiceUser(users, "sa-audit-issue@service.invalid", "Audit Issue")
	ctx := context.WithValue(context.Background(), "userUUID", "actor-issue")

	cred, err := svc.IssueCredential(ctx, u.UUID, "ci-key")
	if err != nil {
		t.Fatalf("IssueCredential: unexpected error: %v", err)
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatalf("expected an audit event, got none")
	}
	if ev.Action != "service_account.credential_issued" {
		t.Errorf("Action = %q, want service_account.credential_issued", ev.Action)
	}
	if ev.Metadata["credentialId"] != cred.ID {
		t.Errorf("Metadata[credentialId] = %v, want %q", ev.Metadata["credentialId"], cred.ID)
	}
	if secret, ok := ev.Metadata["clientSecret"]; ok {
		t.Errorf("metadata leaked the plaintext secret: %v", secret)
	}
}

func TestRevokeCredentialEmitsAuditEvent(t *testing.T) {
	svc, users, creds, sink, _ := newSAServiceWithAudit()
	u := seedServiceUser(users, "sa-audit-revoke@service.invalid", "Audit Revoke")
	cred := seedCredential(creds, u.UUID, "sa_auditrevoke", "initial")
	ctx := context.WithValue(context.Background(), "userUUID", "actor-revoke")

	if err := svc.RevokeCredential(ctx, u.UUID, cred.UUID); err != nil {
		t.Fatalf("RevokeCredential: unexpected error: %v", err)
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatalf("expected an audit event, got none")
	}
	if ev.Action != "service_account.credential_revoked" {
		t.Errorf("Action = %q, want service_account.credential_revoked", ev.Action)
	}
	if ev.Metadata["credentialId"] != cred.UUID {
		t.Errorf("Metadata[credentialId] = %v, want %q", ev.Metadata["credentialId"], cred.UUID)
	}
}

// TestListAndGetDoNotEmitAuditEvents confirms reads stay unaudited —
// only mutations do.
func TestListAndGetDoNotEmitAuditEvents(t *testing.T) {
	svc, users, _, sink, eventRepo := newSAServiceWithAudit()
	u := seedServiceUser(users, "sa-audit-read@service.invalid", "Audit Read")
	ctx := context.Background()

	if _, err := svc.ListAccounts(ctx); err != nil {
		t.Fatalf("ListAccounts: unexpected error: %v", err)
	}
	if _, err := svc.GetAccount(ctx, u.UUID); err != nil {
		t.Fatalf("GetAccount: unexpected error: %v", err)
	}

	if _, ok := sink.last(); ok {
		t.Errorf("read paths must not emit audit events")
	}
	if len(eventRepo.events) != 0 {
		t.Errorf("read paths must not persist security events, got %d", len(eventRepo.events))
	}
}

// ===== §8 #17: the gate classifies, it no longer collapses =====
//
// requireServiceAccount is the gate all four lifecycle methods run first, and
// it used to fold EVERY error from the user directory into
// ErrServiceAccountNotFound — so a Mongo outage told an operator their service
// account had been deleted. That is §4.9's class one module over: an
// infrastructure failure reported as a verdict.

func TestLifecycleClassifiesLookupOutage(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	acct := seedServiceUser(users, "sa-outage@service.invalid", "Outage Bot")
	users.setGetByIDErr(errSAStoreDown)

	cases := []struct {
		name string
		call func() error
	}{
		{"GetAccount", func() error { _, err := svc.GetAccount(ctx, acct.UUID); return err }},
		{"UpdateAccount", func() error {
			active := true
			_, err := svc.UpdateAccount(ctx, acct.UUID, nil, &active)
			return err
		}},
		{"IssueCredential", func() error { _, err := svc.IssueCredential(ctx, acct.UUID, ""); return err }},
		{"RevokeCredential", func() error { return svc.RevokeCredential(ctx, acct.UUID, "whatever") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if !errors.Is(err, ErrServiceAccountLookupUnavailable) {
				t.Fatalf("got %v, want ErrServiceAccountLookupUnavailable — a directory the platform could not read is not proof the account is gone", err)
			}
			if !errors.Is(err, errSAStoreDown) {
				t.Fatalf("got %v, want the underlying cause preserved for whoever reads the log", err)
			}
			if errors.Is(err, ErrServiceAccountNotFound) {
				t.Fatalf("got %v — an outage must not keep the 404 that sends an operator hunting for a deleted account", err)
			}
		})
	}
}

// The negative that keeps the split honest: an id the directory answers for
// and does not know is still a 404, exactly as before.
func TestLifecycleUnknownIDStaysNotFound(t *testing.T) {
	svc, users, _ := newSAService()
	ctx := context.Background()
	_ = users // the fake is deliberately empty: nothing is seeded

	if _, err := svc.GetAccount(ctx, "no-such-account"); !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("GetAccount(unknown): got %v, want ErrServiceAccountNotFound", err)
	}
	if _, err := svc.GetAccount(ctx, "no-such-account"); errors.Is(err, ErrServiceAccountLookupUnavailable) {
		t.Error("an unknown id must not be reported as an outage — that would make every 404 a permanent retry")
	}
}
