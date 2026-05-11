package services

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// newGetMethodsSvc wires a minimal *authService for the aggregator
// tests. Only userService and (optionally) mfaFactorRepo are needed —
// the policy reader is left nil so MFAGraceExpiresAt isn't populated;
// tenantProvider is left nil so the role-only RoleRequiresMFA branch
// is exercised.
func newGetMethodsSvc(fake *adminUnlinkUserFake, factors *fakeFactorRepo) *authService {
	return &authService{
		userService:   fake,
		mfaFactorRepo: factors,
	}
}

func TestGetUserAuthMethods_PasswordOnly(t *testing.T) {
	t.Parallel()
	pwTime := time.Now().Add(-30 * 24 * time.Hour)
	last := time.Now().Add(-1 * time.Hour)
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{
		UUID:              "u1",
		Email:             "u1@example.com",
		Role:              "operator",
		PasswordHash:      "argon2id$...",
		PasswordUpdatedAt: &pwTime,
		EmailVerified:     true,
		LastLogin:         &last,
	})
	svc := newGetMethodsSvc(fake, newFakeFactorRepo())

	view, err := svc.GetUserAuthMethods(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if !view.HasUsablePassword {
		t.Errorf("HasUsablePassword = false, want true")
	}
	if view.PasswordUpdatedAt == nil || !view.PasswordUpdatedAt.Equal(pwTime) {
		t.Errorf("PasswordUpdatedAt mismatch")
	}
	if !view.EmailVerified {
		t.Errorf("EmailVerified = false, want true")
	}
	if view.LastLoginAt == nil || !view.LastLoginAt.Equal(last) {
		t.Errorf("LastLoginAt mismatch")
	}
	if view.MFARequired {
		t.Errorf("operator role should not require MFA")
	}
	if len(view.MFAFactors) != 0 {
		t.Errorf("MFAFactors should be empty, got %+v", view.MFAFactors)
	}
	if len(view.OAuthProviders) != 0 {
		t.Errorf("OAuthProviders should be empty")
	}
}

func TestGetUserAuthMethods_TOTPEnrolled(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{UUID: "u-totp", Role: "administrator", PasswordHash: "x"})

	enrolled := time.Now().Add(-7 * 24 * time.Hour)
	used := time.Now().Add(-2 * time.Hour)
	factors := newFakeFactorRepo()
	_ = factors.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:              "fact-1",
		UserUUID:          "u-totp",
		Type:              models.MFAFactorTOTP,
		VerifiedAt:        &enrolled,
		LastUsedAt:        &used,
		BackupCodesHashed: []string{"h1", "h2", "h3", "h4", "h5"},
	})
	svc := newGetMethodsSvc(fake, factors)

	view, err := svc.GetUserAuthMethods(context.Background(), "u-totp")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if !view.MFARequired {
		t.Errorf("administrator role must require MFA")
	}
	if len(view.MFAFactors) != 1 {
		t.Fatalf("expected 1 MFA factor, got %d", len(view.MFAFactors))
	}
	totp := view.MFAFactors[0]
	if totp.Type != string(models.MFAFactorTOTP) {
		t.Errorf("type = %q, want totp", totp.Type)
	}
	if totp.BackupCodesRemaining != 5 {
		t.Errorf("BackupCodesRemaining = %d, want 5", totp.BackupCodesRemaining)
	}
	if totp.EnrolledAt == nil || !totp.EnrolledAt.Equal(enrolled) {
		t.Errorf("EnrolledAt mismatch")
	}
}

func TestGetUserAuthMethods_BothFactors(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{UUID: "u-both", Role: "administrator", PasswordHash: "x"})

	factors := newFakeFactorRepo()
	totpEnrolled := time.Now().Add(-30 * 24 * time.Hour)
	_ = factors.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:              "fact-totp",
		UserUUID:          "u-both",
		Type:              models.MFAFactorTOTP,
		VerifiedAt:        &totpEnrolled,
		BackupCodesHashed: []string{"h1"},
	})
	wAtCreated := time.Now().Add(-1 * 24 * time.Hour)
	_ = factors.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:     "fact-wa",
		UserUUID: "u-both",
		Type:     models.MFAFactorWebAuthn,
		WebAuthnCredentials: []models.WebAuthnCredential{
			{CredentialID: []byte{0x01, 0x02, 0x03}, Name: "MacBook Touch ID", CreatedAt: wAtCreated},
		},
	})
	svc := newGetMethodsSvc(fake, factors)

	view, err := svc.GetUserAuthMethods(context.Background(), "u-both")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if len(view.MFAFactors) != 2 {
		t.Fatalf("expected 2 factors, got %d (%+v)", len(view.MFAFactors), view.MFAFactors)
	}
	var sawTOTP, sawWA bool
	for _, f := range view.MFAFactors {
		switch f.Type {
		case string(models.MFAFactorTOTP):
			sawTOTP = true
			if f.BackupCodesRemaining != 1 {
				t.Errorf("totp backup codes = %d, want 1", f.BackupCodesRemaining)
			}
		case string(models.MFAFactorWebAuthn):
			sawWA = true
			if len(f.Credentials) != 1 {
				t.Errorf("webauthn credentials = %d, want 1", len(f.Credentials))
			} else if f.Credentials[0].Name != "MacBook Touch ID" {
				t.Errorf("credential name = %q", f.Credentials[0].Name)
			} else if f.Credentials[0].CredentialID != "AQID" {
				// base64url(0x01,0x02,0x03) = "AQID" (no padding)
				t.Errorf("credential id = %q, want AQID", f.Credentials[0].CredentialID)
			}
		}
	}
	if !sawTOTP || !sawWA {
		t.Errorf("expected both TOTP and WebAuthn factors, sawTOTP=%v sawWA=%v", sawTOTP, sawWA)
	}
}

func TestGetUserAuthMethods_OAuthOnly(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	linked := time.Now().Add(-90 * 24 * time.Hour)
	fake.seed(&userModels.User{
		UUID:         "u-oauth",
		Role:         "operator",
		PasswordHash: "", // no password
		OAuthLinks: []userModels.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@x.com", IsActive: true, IsPrimary: true, LinkedAt: linked},
			{Provider: "github", ProviderID: "gh-1", Email: "u@x.com", IsActive: false}, // inactive — should be filtered out
		},
	})
	svc := newGetMethodsSvc(fake, newFakeFactorRepo())

	view, err := svc.GetUserAuthMethods(context.Background(), "u-oauth")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if view.HasUsablePassword {
		t.Errorf("HasUsablePassword = true, want false (OAuth-only)")
	}
	if len(view.OAuthProviders) != 1 {
		t.Fatalf("expected 1 active OAuth provider, got %d", len(view.OAuthProviders))
	}
	if view.OAuthProviders[0].Provider != "google" {
		t.Errorf("provider = %q, want google", view.OAuthProviders[0].Provider)
	}
	if !view.OAuthProviders[0].IsPrimary {
		t.Errorf("expected IsPrimary=true on the active link")
	}
}

func TestGetUserAuthMethods_UnknownUser(t *testing.T) {
	t.Parallel()
	svc := newGetMethodsSvc(newAdminUnlinkUserFake(), newFakeFactorRepo())
	_, err := svc.GetUserAuthMethods(context.Background(), "missing")
	if err == nil {
		t.Errorf("expected error for unknown user")
	}
}

func TestGetUserAuthMethods_RejectsEmptyTarget(t *testing.T) {
	t.Parallel()
	svc := newGetMethodsSvc(newAdminUnlinkUserFake(), newFakeFactorRepo())
	_, err := svc.GetUserAuthMethods(context.Background(), "")
	if err == nil {
		t.Errorf("expected error for empty target")
	}
}
