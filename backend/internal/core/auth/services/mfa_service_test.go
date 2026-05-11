package services

import (
	"context"
	"encoding/base32"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// fakeFactorRepo is an in-memory MFAFactorRepository that keeps the tests
// hermetic — no Mongo involvement. Keyed by userUUID+type for realism.
type fakeFactorRepo struct {
	mu      sync.Mutex
	byUser  map[string]*models.MFAFactorDoc
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
			delete(r.byUser, k)
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

func (u *testUser) toUser() *userModels.User {
	return &userModels.User{UUID: u.UUID, Email: u.Email}
}
