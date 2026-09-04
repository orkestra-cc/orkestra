package services

import (
	"context"
	"encoding/base32"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeFactorRepo is an in-memory MFAFactorRepository that keeps the tests
// hermetic — no Mongo involvement. Keyed by userUUID+type for realism.
//
// failTypes marks factor types whose Delete call should fail — used by
// TestRemoveFactor_PartialDeletionIsAnError to simulate one row deleting
// successfully while the other errors.
type fakeFactorRepo struct {
	mu        sync.Mutex
	byUser    map[string]*models.MFAFactorDoc
	failTypes map[models.MFAFactorType]bool
}

func newFakeFactorRepo() *fakeFactorRepo {
	return &fakeFactorRepo{byUser: map[string]*models.MFAFactorDoc{}}
}

func (r *fakeFactorRepo) key(userUUID string, t models.MFAFactorType) string {
	return userUUID + "|" + string(t)
}

func (r *fakeFactorRepo) Insert(_ context.Context, doc *models.MFAFactorDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byUser[r.key(doc.UserUUID, doc.Type)] = doc
	return nil
}

func (r *fakeFactorRepo) FindByUserAndType(_ context.Context, userUUID string, t models.MFAFactorType) (*models.MFAFactorDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if doc, ok := r.byUser[r.key(userUUID, t)]; ok {
		return doc, nil
	}
	return nil, repository.ErrMFAFactorNotFound
}

func (r *fakeFactorRepo) UpdateLastUsed(_ context.Context, uuid string, when time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byUser {
		if d.UUID == uuid {
			d.LastUsedAt = &when
		}
	}
	return nil
}

func (r *fakeFactorRepo) AdvanceLastUsedStep(_ context.Context, uuid string, step int64, when time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byUser {
		if d.UUID != uuid {
			continue
		}
		if d.LastUsedStep > 0 && step <= d.LastUsedStep {
			return false, nil
		}
		d.LastUsedStep = step
		d.LastUsedAt = &when
		return true, nil
	}
	return false, nil
}

func (r *fakeFactorRepo) ConsumeBackupCode(_ context.Context, userUUID, hashed string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byUser {
		if d.UserUUID != userUUID {
			continue
		}
		for i, c := range d.BackupCodesHashed {
			if c == hashed {
				d.BackupCodesHashed = append(d.BackupCodesHashed[:i], d.BackupCodesHashed[i+1:]...)
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *fakeFactorRepo) ReplaceBackupCodes(_ context.Context, userUUID string, hashed []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byUser {
		if d.UserUUID == userUUID && d.Type == models.MFAFactorTOTP {
			d.BackupCodesHashed = append([]string{}, hashed...)
			return nil
		}
	}
	return repository.ErrMFAFactorNotFound
}

func (r *fakeFactorRepo) Delete(_ context.Context, uuid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, d := range r.byUser {
		if d.UUID == uuid {
			if r.failTypes[d.Type] {
				return errors.New("fake delete failure")
			}
			delete(r.byUser, k)
			return nil
		}
	}
	return nil
}

func (r *fakeFactorRepo) DeleteAllByUser(_ context.Context, userUUID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for k, d := range r.byUser {
		if d.UserUUID == userUUID {
			delete(r.byUser, k)
			n++
		}
	}
	return n, nil
}

func (r *fakeFactorRepo) AppendWebAuthnCredential(_ context.Context, userUUID string, cred models.WebAuthnCredential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(userUUID, models.MFAFactorWebAuthn)
	doc, ok := r.byUser[k]
	if !ok {
		doc = &models.MFAFactorDoc{
			UUID:     userUUID + ":webauthn",
			UserUUID: userUUID,
			Type:     models.MFAFactorWebAuthn,
		}
		r.byUser[k] = doc
	}
	doc.WebAuthnCredentials = append(doc.WebAuthnCredentials, cred)
	return nil
}

func (r *fakeFactorRepo) UpdateWebAuthnCredential(_ context.Context, userUUID string, credentialID []byte, signCount uint32, when time.Time, cloneWarning bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.byUser[r.key(userUUID, models.MFAFactorWebAuthn)]
	if !ok {
		return false, nil
	}
	for i := range doc.WebAuthnCredentials {
		if bytesEq(doc.WebAuthnCredentials[i].CredentialID, credentialID) {
			doc.WebAuthnCredentials[i].SignCount = signCount
			doc.WebAuthnCredentials[i].LastUsedAt = &when
			doc.WebAuthnCredentials[i].CloneWarning = cloneWarning
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeFactorRepo) RemoveWebAuthnCredential(_ context.Context, userUUID string, credentialID []byte) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(userUUID, models.MFAFactorWebAuthn)
	doc, ok := r.byUser[k]
	if !ok {
		return false, nil
	}
	out := doc.WebAuthnCredentials[:0]
	removed := false
	for _, c := range doc.WebAuthnCredentials {
		if !removed && bytesEq(c.CredentialID, credentialID) {
			removed = true
			continue
		}
		out = append(out, c)
	}
	doc.WebAuthnCredentials = out
	if len(out) == 0 {
		delete(r.byUser, k)
	}
	return removed, nil
}

func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- RemoveFactor fixture family (D15) ---
//
// The rest of this file drives fakeFactorRepo directly through the
// repository interface (Insert/AppendWebAuthnCredential/etc). RemoveFactor's
// tests need to seed both factor rows directly and inspect whether they
// survived, so these helpers extend fakeFactorRepo rather than duplicating
// it — there is exactly one fake factor repo in this package.

// seedTOTP inserts a bare TOTP row for userUUID, bypassing enrollment.
func (r *fakeFactorRepo) seedTOTP(t *testing.T, userUUID string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byUser[r.key(userUUID, models.MFAFactorTOTP)] = &models.MFAFactorDoc{
		UUID:     userUUID + ":totp",
		UserUUID: userUUID,
		Type:     models.MFAFactorTOTP,
	}
}

// seedWebAuthn inserts a WebAuthn row for userUUID with credentialCount
// placeholder credentials. credentialCount 0 seeds a row that exists but
// holds no credentials — the "empty WebAuthn row" case, which
// MFAEnrollmentLookup (auth/module.go) and RemoveFactor must agree is NOT
// a factor.
func (r *fakeFactorRepo) seedWebAuthn(t *testing.T, userUUID string, credentialCount int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byUser[r.key(userUUID, models.MFAFactorWebAuthn)] = &models.MFAFactorDoc{
		UUID:                userUUID + ":webauthn",
		UserUUID:            userUUID,
		Type:                models.MFAFactorWebAuthn,
		WebAuthnCredentials: make([]models.WebAuthnCredential, credentialCount),
	}
}

// hasTOTP reports whether a TOTP row still exists for userUUID.
func (r *fakeFactorRepo) hasTOTP(userUUID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byUser[r.key(userUUID, models.MFAFactorTOTP)]
	return ok
}

// hasWebAuthn reports whether a WebAuthn row still exists for userUUID.
func (r *fakeFactorRepo) hasWebAuthn(userUUID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byUser[r.key(userUUID, models.MFAFactorWebAuthn)]
	return ok
}

// failDeleteFor makes Delete return an error for any row of the given
// factor type, regardless of which user it belongs to. Scoped to type
// rather than a specific UUID because that's the granularity RemoveFactor
// itself deletes at.
func (r *fakeFactorRepo) failDeleteFor(t models.MFAFactorType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failTypes == nil {
		r.failTypes = map[models.MFAFactorType]bool{}
	}
	r.failTypes[t] = true
}

// fakeDeviceTrust is a minimal recording DeviceTrustService. RemoveFactor
// only calls RevokeAllByUser, so that's the only method with real
// behaviour; the rest exist to satisfy the interface.
type fakeDeviceTrust struct {
	mu      sync.Mutex
	reasons []string
}

func (f *fakeDeviceTrust) MarkTrusted(context.Context, MarkTrustedInput) error { return nil }

func (f *fakeDeviceTrust) IsTrusted(context.Context, string, string, string) (bool, *models.DeviceTrustDoc, error) {
	return false, nil, nil
}

func (f *fakeDeviceTrust) ListActive(context.Context, string) ([]*models.DeviceTrustDoc, error) {
	return nil, nil
}

func (f *fakeDeviceTrust) RevokeByDevice(context.Context, string, string, string) error { return nil }

func (f *fakeDeviceTrust) RevokeAllByUser(_ context.Context, _ string, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reasons = append(f.reasons, reason)
	return nil
}

func (f *fakeDeviceTrust) revokeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reasons)
}

func (f *fakeDeviceTrust) lastReason() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reasons) == 0 {
		return ""
	}
	return f.reasons[len(f.reasons)-1]
}

// newMFAServiceForTest builds an MFAService the same way every other test
// in this file does (see TestEnrollmentAndVerify) — a fresh fakeFactorRepo,
// in-memory challenge store, non-bcrypt PasswordService, and the
// deterministic all-zero MFA_SECRET_ENCRYPTION_KEY — plus a fakeDeviceTrust
// wired via SetDeviceTrust so RemoveFactor's revoke-once behaviour is
// observable. Returns the service, the repo (for seeding/inspection), and
// the device-trust fake (for revoke assertions).
func newMFAServiceForTest(t *testing.T) (MFAService, *fakeFactorRepo, *fakeDeviceTrust) {
	t.Helper()
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())
	trust := &fakeDeviceTrust{}
	svc.SetDeviceTrust(trust)
	return svc, repo, trust
}

// Test validateTOTP against a freshly generated secret — we don't need to
// reach for RFC 6238's SHA-1 test vectors because our only contract is
// "codes generated by the same secret at the same time match". Round-tripping
// ensures the Period/Digits/Skew are compatible with whatever authenticator
// the user happens to use.
func TestValidateTOTPRoundTrip(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Orkestra",
		AccountName: "alice@example.com",
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if !validateTOTP(key.Secret(), code) {
		t.Fatalf("expected valid code to be accepted")
	}
	if validateTOTP(key.Secret(), "000000") {
		t.Fatalf("expected zero code to be rejected")
	}
}

func TestEnrollmentAndVerify(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	user := &testUser{UUID: "u-1", Email: "alice@example.com"}
	begin, err := svc.BeginEnrollment(context.Background(), user.toUser())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if begin.SecretBase32 == "" || begin.ChallengeID == "" {
		t.Fatalf("begin payload incomplete: %+v", begin)
	}
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(begin.SecretBase32); err != nil {
		t.Fatalf("secret is not valid base32: %v", err)
	}

	code, err := totp.GenerateCode(begin.SecretBase32, time.Now())
	if err != nil {
		t.Fatalf("code: %v", err)
	}

	backupCodes, err := svc.ConfirmEnrollment(context.Background(), user.UUID, begin.ChallengeID, code)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(backupCodes) != BackupCodeCount {
		t.Fatalf("expected %d backup codes, got %d", BackupCodeCount, len(backupCodes))
	}

	// Verify should now succeed with a live code.
	code2, _ := totp.GenerateCode(begin.SecretBase32, time.Now())
	if err := svc.Verify(context.Background(), user.UUID, code2); err != nil {
		t.Fatalf("verify after enrollment: %v", err)
	}

	// Wrong code rejected.
	if err := svc.Verify(context.Background(), user.UUID, "000000"); err == nil {
		t.Fatalf("expected wrong code to fail")
	}

	// Status reports enrolled.
	snap, err := svc.Status(context.Background(), user.UUID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if snap.Status != models.MFAStatusEnrolled {
		t.Fatalf("expected enrolled, got %v", snap.Status)
	}
	if snap.BackupCodesRemaining != BackupCodeCount {
		t.Fatalf("expected %d remaining, got %d", BackupCodeCount, snap.BackupCodesRemaining)
	}
}

func TestBackupCodeSingleUse(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	user := &testUser{UUID: "u-2", Email: "bob@example.com"}
	begin, _ := svc.BeginEnrollment(context.Background(), user.toUser())
	code, _ := totp.GenerateCode(begin.SecretBase32, time.Now())
	codes, err := svc.ConfirmEnrollment(context.Background(), user.UUID, begin.ChallengeID, code)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	// First use succeeds.
	if err := svc.VerifyBackupCode(context.Background(), user.UUID, codes[0]); err != nil {
		t.Fatalf("first use failed: %v", err)
	}
	// Second use with the same code fails.
	if err := svc.VerifyBackupCode(context.Background(), user.UUID, codes[0]); err == nil {
		t.Fatalf("expected re-use to fail")
	}

	snap, _ := svc.Status(context.Background(), user.UUID)
	if snap.BackupCodesRemaining != BackupCodeCount-1 {
		t.Fatalf("remaining count: got %d want %d", snap.BackupCodesRemaining, BackupCodeCount-1)
	}
}

func TestEnrollmentIdempotentReset(t *testing.T) {
	// Beginning enrollment twice before confirm invalidates the earlier
	// challenge (via Redis TTL); a second confirm with the *new* code must
	// still succeed.
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	user := &testUser{UUID: "u-3", Email: "carol@example.com"}
	_, _ = svc.BeginEnrollment(context.Background(), user.toUser())
	begin2, _ := svc.BeginEnrollment(context.Background(), user.toUser())
	code, _ := totp.GenerateCode(begin2.SecretBase32, time.Now())
	if _, err := svc.ConfirmEnrollment(context.Background(), user.UUID, begin2.ChallengeID, code); err != nil {
		t.Fatalf("confirm on second begin: %v", err)
	}
}

func TestTOTPReplayRejected(t *testing.T) {
	// Two verifies of the same code within its 30s window: first wins, second
	// loses. This is the core of the Block B replay guard — a captured code
	// can't be used to satisfy a fresh login challenge while still valid.
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	user := &testUser{UUID: "u-replay", Email: "r@example.com"}
	begin, _ := svc.BeginEnrollment(context.Background(), user.toUser())
	code, _ := totp.GenerateCode(begin.SecretBase32, time.Now())
	if _, err := svc.ConfirmEnrollment(context.Background(), user.UUID, begin.ChallengeID, code); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	live, _ := totp.GenerateCode(begin.SecretBase32, time.Now())
	if err := svc.Verify(context.Background(), user.UUID, live); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	// Second verify with the same live code inside the same step must fail.
	if err := svc.Verify(context.Background(), user.UUID, live); err == nil {
		t.Fatalf("replay should have been rejected")
	}
}

func TestBackupCodeNormalisation(t *testing.T) {
	// Users may retype codes with or without the dash, in any case. The
	// service must normalise before hashing.
	if got := normaliseBackupCode("abcd-efgh"); got != "ABCDEFGH" {
		t.Fatalf("expected ABCDEFGH, got %s", got)
	}
	if got := normaliseBackupCode(" ABCD-EFGH "); got != "ABCDEFGH" {
		t.Fatalf("whitespace not trimmed: %s", got)
	}
}

// hex32 returns a 32-byte hex key suitable for MFA_SECRET_ENCRYPTION_KEY.
// All zeros keeps the test deterministic and reproducible across CI runs.
func hex32() string {
	return strings.Repeat("00", 32)
}

// testUser keeps the test call sites terse; the MFA service only reads UUID
// and Email from the full user model.
type testUser struct {
	UUID  string
	Email string
}

func (u *testUser) toUser() *iface.User {
	return &iface.User{UUID: u.UUID, Email: u.Email}
}

// --- RemoveFactor (D15): removal must remove every factor ---

// "Remove MFA" that leaves the passkeys standing is not a removal. The
// admin reset path depends on this too: it 404s today for a passkey-only
// target, so an operator cannot recover such an account at all.
func TestRemoveFactor_DeletesBothRows(t *testing.T) {
	svc, repo, _ := newMFAServiceForTest(t)
	ctx := context.Background()
	repo.seedTOTP(t, "u-1")
	repo.seedWebAuthn(t, "u-1", 2 /* credentials */)

	if err := svc.RemoveFactor(ctx, "u-1", "u-1"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
	if repo.hasTOTP("u-1") {
		t.Error("the TOTP row must be gone")
	}
	if repo.hasWebAuthn("u-1") {
		t.Error("the WebAuthn row must be gone too")
	}
}

func TestRemoveFactor_PasskeyOnlyUserSucceeds(t *testing.T) {
	svc, repo, _ := newMFAServiceForTest(t)
	repo.seedWebAuthn(t, "u-2", 1)

	if err := svc.RemoveFactor(context.Background(), "u-2", "admin-1"); err != nil {
		t.Fatalf("RemoveFactor for a passkey-only user = %v, want nil", err)
	}
	if repo.hasWebAuthn("u-2") {
		t.Error("the WebAuthn row must be gone")
	}
}

// Regression pin, not a TDD driver: this already passed against the
// pre-fix code, which deleted the lone TOTP row and never looked at
// WebAuthn at all. Kept so a future change can't quietly break the
// TOTP-only path while touching the WebAuthn half.
func TestRemoveFactor_TOTPOnlyUserStillSucceeds(t *testing.T) {
	svc, repo, _ := newMFAServiceForTest(t)
	repo.seedTOTP(t, "u-3")
	if err := svc.RemoveFactor(context.Background(), "u-3", "u-3"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
}

func TestRemoveFactor_NoFactorsAtAllIsNotEnrolled(t *testing.T) {
	svc, _, _ := newMFAServiceForTest(t)
	if err := svc.RemoveFactor(context.Background(), "u-4", "u-4"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("err = %v, want ErrMFANotEnrolled only when NEITHER row exists", err)
	}
}

// A WebAuthn row with zero credentials is not a factor — the enrolment
// lookup (MFAEnrollmentLookup, auth/module.go) already treats it that
// way, and the two must agree.
//
// Regression pin, not a TDD driver, though for a different reason than
// the brief that introduced this test claimed: pre-fix code never looks
// at WebAuthn at all, so it already answers ErrMFANotEnrolled here by
// accident (no TOTP row exists for u-5 either). Verified empirically —
// this test PASSES against the pre-fix RemoveFactor. It still earns its
// place post-fix: it guards against a shallower fix that treats "a
// WebAuthn row exists" as "has a factor" without checking
// len(credentials) > 0, which would regress this exact case.
func TestRemoveFactor_EmptyWebAuthnRowIsNotAFactor(t *testing.T) {
	svc, repo, _ := newMFAServiceForTest(t)
	repo.seedWebAuthn(t, "u-5", 0)
	if err := svc.RemoveFactor(context.Background(), "u-5", "u-5"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("err = %v, want ErrMFANotEnrolled", err)
	}
}

// A partial failure must not report success: if one row deletes and the
// other errors, the caller has to know the account is half-reset.
func TestRemoveFactor_PartialDeletionIsAnError(t *testing.T) {
	svc, repo, _ := newMFAServiceForTest(t)
	repo.seedTOTP(t, "u-6")
	repo.seedWebAuthn(t, "u-6", 1)
	repo.failDeleteFor(models.MFAFactorWebAuthn)

	if err := svc.RemoveFactor(context.Background(), "u-6", "u-6"); err == nil {
		t.Fatal("a failed deletion of one row must surface as an error")
	}
}

// Regression pin, not a TDD driver: revoke-once-with-correct-reason already
// worked against the pre-fix code — the device-trust block runs after the
// (single, TOTP-only) deletion and was untouched by this change. Kept to
// guard against a future refactor moving it inside a per-row loop, which
// would revoke once per row instead of once per call.
func TestRemoveFactor_RevokesDeviceTrustOnce(t *testing.T) {
	svc, repo, trust := newMFAServiceForTest(t)
	repo.seedTOTP(t, "u-7")
	repo.seedWebAuthn(t, "u-7", 1)

	if err := svc.RemoveFactor(context.Background(), "u-7", "admin-9"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
	if trust.revokeCalls() != 1 {
		t.Fatalf("device trust revoked %d times, want 1", trust.revokeCalls())
	}
	if trust.lastReason() != models.DeviceTrustRevokedOnAdminReset {
		t.Fatalf("reason = %q, want the admin-reset reason", trust.lastReason())
	}
}
