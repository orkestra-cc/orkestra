package services

// Test-only fakes used by the Phase 11 integration tests for policy
// gates (Login, Register, ChangePassword, OAuth callback). Each fake
// implements just the methods the path under test exercises; anything
// else panics so accidental coverage drift surfaces loudly.
//
// The user fakes deliberately keep state in plain maps — no Mongo or
// Redis is involved. Tests that need RSA key material reuse a single
// generated keypair via testRSAKey() to keep startup cheap.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"sync"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/geoip"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// gateUserFake is a minimal in-memory iface.UserProvider. Tests pre-
// populate `byEmail` / `byUUID` with the users they expect to find.
// All methods that the gate paths might hit are implemented; anything
// else panics so a regression that adds a new dependency is visible
// immediately.
type gateUserFake struct {
	mu               sync.Mutex
	byEmail          map[string]*userModels.User
	byUUID           map[string]*userModels.User
	count            int64
	updateUserCalls  []userModels.UpdateUserInput
	lastLoginTouches []string
	createdUsers     []*userModels.User
	createWithPwdErr error
	updateUserErr    error
}

func newGateUserFake() *gateUserFake {
	return &gateUserFake{
		byEmail: map[string]*userModels.User{},
		byUUID:  map[string]*userModels.User{},
	}
}

func (f *gateUserFake) seed(u *userModels.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byEmail[u.Email] = u
	f.byUUID[u.UUID] = u
	f.count++
}

func (f *gateUserFake) GetUserByID(_ context.Context, id string) (*userModels.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[id]; ok {
		return u, nil
	}
	return nil, errNotFound
}

func (f *gateUserFake) GetUserByEmail(_ context.Context, email string) (*userModels.UserManagementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byEmail[email]; ok {
		return u.ToResponse(), nil
	}
	return nil, errNotFound
}

func (f *gateUserFake) GetUserForAuth(_ context.Context, email string) (*userModels.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byEmail[email]; ok {
		return u, nil
	}
	return nil, errNotFound
}

func (f *gateUserFake) CreateUserFromOAuth(_ context.Context, in *userModels.CreateUserInput) (*userModels.User, error) {
	return f.createInternal(in)
}

func (f *gateUserFake) CreateUserWithPassword(_ context.Context, in *userModels.CreateUserInput) (*userModels.User, error) {
	if f.createWithPwdErr != nil {
		return nil, f.createWithPwdErr
	}
	return f.createInternal(in)
}

func (f *gateUserFake) createInternal(in *userModels.CreateUserInput) (*userModels.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := &userModels.User{
		UUID:         in.UUID,
		Email:        in.Email,
		FullName:     in.FullName,
		Role:         in.Role,
		PasswordHash: in.PasswordHash,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if u.UUID == "" {
		u.UUID = uuid.NewString()
	}
	f.byEmail[u.Email] = u
	f.byUUID[u.UUID] = u
	f.count++
	f.createdUsers = append(f.createdUsers, u)
	return u, nil
}

func (f *gateUserFake) UpdatePasswordHash(_ context.Context, userUUID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok {
		u.PasswordHash = hash
	}
	return nil
}

func (f *gateUserFake) MarkEmailVerified(_ context.Context, userUUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok {
		u.EmailVerified = true
	}
	return nil
}

func (f *gateUserFake) RecordFailedLogin(_ context.Context, userUUID string, lockUntil *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok {
		u.FailedLoginCount++
		u.LockedUntil = lockUntil
	}
	return nil
}

func (f *gateUserFake) ClearFailedLogins(_ context.Context, userUUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok {
		u.FailedLoginCount = 0
		u.LockedUntil = nil
	}
	return nil
}

func (f *gateUserFake) UpdateUser(_ context.Context, id string, in *userModels.UpdateUserInput) (*userModels.UserManagementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateUserErr != nil {
		return nil, f.updateUserErr
	}
	u, ok := f.byUUID[id]
	if !ok {
		return nil, errNotFound
	}
	if in.IsActive != nil {
		u.IsActive = *in.IsActive
	}
	if in.Role != "" {
		u.Role = in.Role
	}
	if in.FullName != "" {
		u.FullName = in.FullName
	}
	f.updateUserCalls = append(f.updateUserCalls, *in)
	return u.ToResponse(), nil
}

func (f *gateUserFake) UpdateUserLastLogin(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastLoginTouches = append(f.lastLoginTouches, id)
	return nil
}

func (f *gateUserFake) DeleteUser(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[id]; ok {
		delete(f.byEmail, u.Email)
		delete(f.byUUID, id)
	}
	return nil
}

func (f *gateUserFake) SoftDeleteAndAliasEmail(_ context.Context, _ string) error { return nil }

func (f *gateUserFake) GetUserOAuthLinks(_ context.Context, _ string) ([]userModels.OAuthLink, error) {
	return nil, nil
}

func (f *gateUserFake) RemoveOAuthLinkFromUser(_ context.Context, _ string, _ userModels.OAuthProvider, _ string) error {
	return nil
}

func (f *gateUserFake) SetPrimaryOAuthLink(_ context.Context, _ string, _ userModels.OAuthProvider, _ string) error {
	return nil
}

func (f *gateUserFake) GetUserCount(_ context.Context, _ *userModels.UserFilters) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count, nil
}

func (f *gateUserFake) StartMFAGraceIfUnset(_ context.Context, userUUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok && u.MFAGraceStartedAt == nil {
		now := time.Now()
		u.MFAGraceStartedAt = &now
	}
	return nil
}

func (f *gateUserFake) ResetMFAGrace(_ context.Context, userUUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok {
		now := time.Now()
		u.MFAGraceStartedAt = &now
	}
	return nil
}

func (f *gateUserFake) ClearMFAGrace(_ context.Context, userUUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUUID[userUUID]; ok {
		u.MFAGraceStartedAt = nil
	}
	return nil
}

// AddOAuthLinkToUser appends to the user's embedded OAuthLinks slice.
// Used by the SelfLinkOAuth flow tests.
func (f *gateUserFake) AddOAuthLinkToUser(_ context.Context, userUUID string, link userModels.OAuthLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byUUID[userUUID]
	if !ok {
		return errNotFound
	}
	u.OAuthLinks = append(u.OAuthLinks, link)
	return nil
}

// errNotFound mirrors the "not found" sentinel the user service returns
// when an email/uuid is unknown. Callers in PasswordAuthService check
// non-nil err to mean "user does not exist" — they don't introspect
// the specific error type, so a plain error string is enough.
var errNotFound = &fakeNotFoundErr{}

type fakeNotFoundErr struct{}

func (*fakeNotFoundErr) Error() string { return "user not found" }

// gateRefreshRepo is an in-memory refresh-token repository. Light
// enough that the gate tests never reach beyond CreateRefreshToken;
// rich enough that the Phase-16 orchestration tests can exercise the
// full RotateWithFamily / GetByTokenAny / RevokeFamily lineage. Other
// methods stay as panics so a refactor that takes a new dependency
// surfaces immediately.
type gateRefreshRepo struct {
	mu      sync.Mutex
	created []*authModels.RefreshTokenDoc
	revoked []string // userUUIDs that hit RevokeTokensByUser
	// byHash mirrors the production repo's "primary lookup" path.
	// Tests can pre-seed via seedRefreshDoc for the orchestration paths.
	byHash map[string]*authModels.RefreshTokenDoc
}

func newGateRefreshRepo() *gateRefreshRepo {
	return &gateRefreshRepo{byHash: map[string]*authModels.RefreshTokenDoc{}}
}

func (r *gateRefreshRepo) CreateRefreshToken(_ context.Context, doc *authModels.RefreshTokenDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, doc)
	return nil
}
func (r *gateRefreshRepo) RevokeTokensByUser(_ context.Context, userUUID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revoked = append(r.revoked, userUUID)
	return nil
}

// seedRefreshDoc lets orchestration tests load a known token row
// keyed by its hashed-token primary key so RefreshTokensWithRiskAssessment
// can find it.
func (r *gateRefreshRepo) seedRefreshDoc(tokenHash string, doc *authModels.RefreshTokenDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *doc
	r.byHash[tokenHash] = &c
}

func (r *gateRefreshRepo) GetByTokenAny(_ context.Context, tokenHash string) (*authModels.RefreshTokenDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.byHash[tokenHash]; ok {
		c := *d
		return &c, nil
	}
	return nil, nil
}

func (r *gateRefreshRepo) RotateWithFamily(_ context.Context, oldHash string, newDoc *authModels.RefreshTokenDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.byHash[oldHash]
	if !ok || old.IsRevoked {
		return repository.ErrTokenAlreadyRotated
	}
	now := time.Now()
	old.IsRevoked = true
	old.RevokedAt = &now
	old.RevokedReason = authModels.RevokeReasonRotated
	old.SucceededBy = newDoc.UUID
	c := *newDoc
	r.byHash[newDoc.Token] = &c
	return nil
}

func (r *gateRefreshRepo) RevokeFamily(_ context.Context, familyID, reason string) (int64, error) {
	if familyID == "" {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var n int64
	for _, d := range r.byHash {
		if d.FamilyID == familyID && !d.IsRevoked {
			d.IsRevoked = true
			d.RevokedAt = &now
			d.RevokedReason = reason
			n++
		}
	}
	return n, nil
}

// Methods we never reach — panic loudly if a refactor crosses the line.
func (r *gateRefreshRepo) GetByToken(context.Context, string) (*authModels.RefreshTokenDoc, error) {
	panic("gateRefreshRepo.GetByToken not used by gate tests")
}
func (r *gateRefreshRepo) GetBySessionUUID(context.Context, string) (*authModels.RefreshTokenDoc, error) {
	panic("not used")
}
func (r *gateRefreshRepo) GetActiveTokensByUser(context.Context, string) ([]*authModels.RefreshTokenDoc, error) {
	panic("not used")
}
func (r *gateRefreshRepo) GetActiveTokensByDevice(context.Context, string, string) ([]*authModels.RefreshTokenDoc, error) {
	panic("not used")
}
func (r *gateRefreshRepo) UpdateLastActivity(context.Context, string) error { panic("not used") }
func (r *gateRefreshRepo) UpdateRiskScore(context.Context, string, float64, []string) error {
	panic("not used")
}
func (r *gateRefreshRepo) RotateToken(context.Context, string, string) error       { panic("not used") }
func (r *gateRefreshRepo) RevokeToken(context.Context, string, string) error       { panic("not used") }
func (r *gateRefreshRepo) RevokeTokenByUUID(context.Context, string, string) error { panic("not used") }

// RevokeTokensBySession is a no-op so the user-security session
// tests can drive the auth-service's revokeSessionInternal helper
// without needing per-session refresh-token state. The other fake
// methods stay as panics; if a future test grows a real expectation
// here, lift this into a recording counter then.
func (r *gateRefreshRepo) RevokeTokensBySession(context.Context, string, string) error {
	return nil
}
func (r *gateRefreshRepo) RevokeTokensByDevice(context.Context, string, string, string) error {
	panic("not used")
}
func (r *gateRefreshRepo) CleanupExpiredTokens(context.Context) (int64, error) { panic("not used") }
func (r *gateRefreshRepo) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("not used")
}
func (r *gateRefreshRepo) CleanupRevokedTokens(context.Context, time.Duration) (int64, error) {
	panic("not used")
}
func (r *gateRefreshRepo) GetTokenStats(context.Context, string) (*repository.TokenStats, error) {
	panic("not used")
}
func (r *gateRefreshRepo) GetDeviceTokenCount(context.Context, string, string) (int64, error) {
	panic("not used")
}
func (r *gateRefreshRepo) CountFamilyMembers(context.Context, string) (int64, error) {
	panic("not used")
}

// gateSessionRepo is a no-op auth-session repo. Records the
// most recent CreateSession arg so tests can inspect it.
type gateSessionRepo struct {
	mu               sync.Mutex
	created          []*authModels.AuthSessionDoc
	deviceHistory    map[string][]*authModels.AuthSessionDoc // key: userUUID|deviceID
	deviceHistoryErr error
}

func newGateSessionRepo() *gateSessionRepo {
	return &gateSessionRepo{deviceHistory: map[string][]*authModels.AuthSessionDoc{}}
}

func (r *gateSessionRepo) CreateSession(_ context.Context, doc *authModels.AuthSessionDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, doc)
	return nil
}

func (r *gateSessionRepo) GetDeviceSessionHistory(_ context.Context, userUUID, deviceID string, _ int) ([]*authModels.AuthSessionDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deviceHistoryErr != nil {
		return nil, r.deviceHistoryErr
	}
	return r.deviceHistory[userUUID+"|"+deviceID], nil
}

// Unused — panic loudly.
func (r *gateSessionRepo) GetByUUID(context.Context, string) (*authModels.AuthSessionDoc, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetByUserAndDevice(context.Context, string, string) (*authModels.AuthSessionDoc, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetActiveSessionsByUser(context.Context, string) ([]*authModels.AuthSessionDoc, error) {
	panic("not used")
}
func (r *gateSessionRepo) UpdateLastActivity(context.Context, string) error { panic("not used") }
func (r *gateSessionRepo) UpdateRiskScore(context.Context, string, float64, string) error {
	panic("not used")
}
func (r *gateSessionRepo) AddSecurityEvent(context.Context, string, *authModels.SecurityEventLog) error {
	panic("not used")
}
func (r *gateSessionRepo) UpdateDeviceInfo(context.Context, string, *authModels.DeviceInfo) error {
	panic("not used")
}
func (r *gateSessionRepo) TerminateSession(context.Context, string) error { panic("not used") }
func (r *gateSessionRepo) TerminateSessionByDevice(context.Context, string, string) error {
	panic("not used")
}
func (r *gateSessionRepo) TerminateAllUserSessions(context.Context, string) error { panic("not used") }
func (r *gateSessionRepo) TerminateExpiredSessions(context.Context) (int64, error) {
	panic("not used")
}
func (r *gateSessionRepo) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetSessionStats(context.Context, string) (*repository.SessionStats, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetActiveDevices(context.Context, string) ([]*repository.DeviceSession, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetSessionsByLocation(context.Context, string, string) ([]*authModels.AuthSessionDoc, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetHighRiskSessions(context.Context, float64) ([]*authModels.AuthSessionDoc, error) {
	panic("not used")
}
func (r *gateSessionRepo) RenameDevice(context.Context, string, string, string) error {
	panic("not used")
}
func (r *gateSessionRepo) GetRecentSecurityEvents(context.Context, string, string, time.Time) ([]*authModels.SecurityEventLog, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetSuspiciousSessions(context.Context, string) ([]*authModels.AuthSessionDoc, error) {
	panic("not used")
}
func (r *gateSessionRepo) CountSessionsByUserAndFingerprint(context.Context, string, string, time.Time) (int64, error) {
	panic("not used")
}
func (r *gateSessionRepo) CountSessionsByUserAndIP(context.Context, string, string, time.Time) (int64, error) {
	panic("not used")
}
func (r *gateSessionRepo) GetMostRecentSessionByUser(context.Context, string) (*authModels.AuthSessionDoc, error) {
	panic("not used")
}

// gateGeoResolver is a fixed-IP-to-country fake. Tests pre-load the
// (ip → country) map.
type gateGeoResolver struct {
	byIP map[string]string
}

func newGateGeoResolver(byIP map[string]string) *gateGeoResolver {
	return &gateGeoResolver{byIP: byIP}
}

func (g *gateGeoResolver) Lookup(_ context.Context, ip string) (*geoip.Location, error) {
	if g == nil {
		return nil, nil
	}
	if c, ok := g.byIP[ip]; ok {
		return &geoip.Location{IP: ip, Country: c}, nil
	}
	return nil, nil
}

func (g *gateGeoResolver) Close() error { return nil }

// gateClaimer is a no-op FirstAdminClaimer that always grants the
// first claim and silently accepts releases. Tests can swap it for a
// stricter variant if they need to assert the rollback path.
type gateClaimer struct {
	claimed  map[string]bool
	released []string
	claimErr error
}

func newGateClaimer() *gateClaimer { return &gateClaimer{claimed: map[string]bool{}} }

func (c *gateClaimer) ClaimFirstAdmin(_ context.Context, userUUID string) (bool, error) {
	if c.claimErr != nil {
		return false, c.claimErr
	}
	if len(c.claimed) > 0 {
		return false, nil
	}
	c.claimed[userUUID] = true
	return true, nil
}
func (c *gateClaimer) Release(_ context.Context, userUUID string) error {
	delete(c.claimed, userUUID)
	c.released = append(c.released, userUUID)
	return nil
}

// gateNotifier is a stub iface.NotificationSender that records every
// SendTemplated request. IsConfigured returns whatever `configured`
// is set to.
type gateNotifier struct {
	mu         sync.Mutex
	configured bool
	sends      []iface.TemplatedNotificationRequest
}

func (g *gateNotifier) IsConfigured(_ context.Context) bool { return g.configured }
func (g *gateNotifier) Send(context.Context, iface.NotificationRequest) (*iface.NotificationResult, error) {
	panic("Send not used in these tests")
}
func (g *gateNotifier) SendTemplated(_ context.Context, req iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sends = append(g.sends, req)
	return &iface.NotificationResult{Status: "sent"}, nil
}

// gateTenantProvider implements iface.TenantProvider with the bare
// minimum the gate paths exercise: ListUserMemberships returns no
// memberships so MFA evaluation skips the privileged-role branch and
// the user gets a non-MFA token. Methods used elsewhere panic.
type gateTenantProvider struct{}

func (gateTenantProvider) GetTenant(context.Context, string) (*iface.Tenant, error) {
	panic("not used")
}
func (gateTenantProvider) ListUserMemberships(context.Context, string) ([]iface.TenantMembership, error) {
	return nil, nil
}
func (gateTenantProvider) IsMember(context.Context, string, string) (bool, error) {
	panic("not used")
}
func (gateTenantProvider) ActivateTenant(context.Context, string) error {
	panic("not used")
}
func (gateTenantProvider) SetTenantStripeCustomerID(context.Context, string, string) error {
	panic("not used")
}
func (gateTenantProvider) EnsureTenantForUser(context.Context, string) (*iface.Tenant, error) {
	panic("not used")
}

// testRSAKey returns a process-wide cached RSA key pair for JWT
// signing in the gate tests. Generated lazily on first use so
// `go test` doesn't pay the cost when only the policy reader tests
// run.
var (
	testRSAKeyOnce sync.Once
	testRSAKeyPair *rsa.PrivateKey
)

func testRSAKey() *rsa.PrivateKey {
	testRSAKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testRSAKeyPair = k
	})
	return testRSAKeyPair
}

// activeUser builds a User row whose state is "live and clean" — the
// caller can mutate the returned pointer for cases that need a
// stale lastLogin / inactive flag / etc.
func activeUser(email, hash string) *userModels.User {
	return &userModels.User{
		UUID:          uuid.NewString(),
		Email:         email,
		FullName:      "Test User",
		Role:          "operator",
		PasswordHash:  hash,
		IsActive:      true,
		EmailVerified: true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}
