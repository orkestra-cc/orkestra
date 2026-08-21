package services

// Task 7: ServiceAccountService.Grant — the client-credentials exchange.
// Reuses the fakes and helpers declared in service_account_service_test.go
// (saUserFake, saCredRepoFake, saHasherFake, saMinterFake, seedServiceUser,
// seedHumanUser). Each TestGrant* case builds its own service instance
// (own fakes, own real *sharederrors.RateLimiter) via
// newSAServiceForGrant so no case can trip another's lockout bucket.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// newSAServiceForGrant wires a fresh ServiceAccountService with its own
// fakes and a real *sharederrors.RateLimiter (TestGrantRateLimited needs
// the genuine token-bucket behavior, not a stub).
func newSAServiceForGrant() (*ServiceAccountService, *saUserFake, *saCredRepoFake, *saHasherFake, *sharederrors.RateLimiter) {
	users := newSAUserFake()
	creds := newSACredRepoFake()
	hasher := &saHasherFake{}
	limiter := sharederrors.NewRateLimiter()
	svc := NewServiceAccountService(creds, users, users, hasher, saMinterFake{}, limiter)
	return svc, users, creds, hasher, limiter
}

// seedGrantFixture seeds the shared happy-path fixture referenced by
// every TestGrant* case below: an active service-account user plus one
// active credential (ClientID "sa_abc", SecretHash "h:good-secret").
func seedGrantFixture(users *saUserFake, creds *saCredRepoFake) (*iface.User, *models.ServiceAccountCredential) {
	u := seedServiceUser(users, "sa-grant-fixture@service.invalid", "Grant Fixture")
	c := &models.ServiceAccountCredential{
		UUID:       uuid.NewString(),
		UserUUID:   u.UUID,
		ClientID:   "sa_abc",
		SecretHash: "h:good-secret",
		Label:      "initial",
		CreatedAt:  time.Now(),
	}
	creds.seed(c)
	return u, c
}

// hasherVerifiedDummy reports whether hasher.Verify was ever called with
// hasher.DummyHash() as the encoded argument — the signal that Grant
// burned a constant-shape dummy verify for timing parity.
func hasherVerifiedDummy(hasher *saHasherFake) bool {
	dummy := hasher.DummyHash()
	hasher.mu.Lock()
	defer hasher.mu.Unlock()
	for _, c := range hasher.verifyCalls {
		if c.Encoded == dummy {
			return true
		}
	}
	return false
}

func TestGrantHappyPath(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	ctx := context.Background()
	u, cred := seedGrantFixture(users, creds)

	res, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_abc",
		ClientSecret: "good-secret",
		IP:           "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Grant: unexpected error: %v", err)
	}
	if res.AccessToken != "tok-"+u.UUID {
		t.Errorf("AccessToken = %q, want tok-%s", res.AccessToken, u.UUID)
	}
	if res.ExpiresIn != 900 {
		t.Errorf("ExpiresIn = %d, want 900", res.ExpiresIn)
	}
	if got := creds.countStampCalls(cred.UUID); got != 1 {
		t.Errorf("StampLastUsed called %d times for %s, want 1", got, cred.UUID)
	}
}

func TestGrantWrongSecret(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	ctx := context.Background()
	seedGrantFixture(users, creds)

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_abc",
		ClientSecret: "wrong-secret",
		IP:           "10.0.0.2",
	})
	if !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(wrong secret): got %v, want ErrInvalidClientCredentials", err)
	}
}

func TestGrantUnknownClientID(t *testing.T) {
	svc, users, creds, hasher, _ := newSAServiceForGrant()
	ctx := context.Background()
	seedGrantFixture(users, creds) // present but irrelevant — clientID below is unknown

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_does_not_exist",
		ClientSecret: "whatever",
		IP:           "10.0.0.3",
	})
	if !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(unknown clientId): got %v, want ErrInvalidClientCredentials", err)
	}
	if !hasherVerifiedDummy(hasher) {
		t.Errorf("expected a Verify call against DummyHash() for timing parity on an unknown clientId")
	}
}

func TestGrantRevokedCredential(t *testing.T) {
	svc, users, creds, hasher, _ := newSAServiceForGrant()
	ctx := context.Background()
	u := seedServiceUser(users, "sa-revoked@service.invalid", "Revoked Cred")
	revokedAt := time.Now()
	creds.seed(&models.ServiceAccountCredential{
		UUID:       uuid.NewString(),
		UserUUID:   u.UUID,
		ClientID:   "sa_revoked",
		SecretHash: "h:good-secret",
		Label:      "initial",
		CreatedAt:  time.Now(),
		RevokedAt:  &revokedAt,
	})

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_revoked",
		ClientSecret: "good-secret",
		IP:           "10.0.0.4",
	})
	if !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(revoked credential): got %v, want ErrInvalidClientCredentials", err)
	}
	if !hasherVerifiedDummy(hasher) {
		t.Errorf("expected a Verify call against DummyHash() for timing parity on a revoked credential")
	}
}

func TestGrantDisabledAccount(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	ctx := context.Background()
	u, _ := seedGrantFixture(users, creds)
	inactive := false
	if _, err := svc.UpdateAccount(ctx, u.UUID, nil, &inactive); err != nil {
		t.Fatalf("test setup UpdateAccount: %v", err)
	}

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_abc",
		ClientSecret: "good-secret",
		IP:           "10.0.0.5",
	})
	if !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(disabled account): got %v, want ErrInvalidClientCredentials", err)
	}
}

func TestGrantHumanUser(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	ctx := context.Background()
	human := seedHumanUser(users, "sa-human-cred@example.com", "A Human")
	creds.seed(&models.ServiceAccountCredential{
		UUID:       uuid.NewString(),
		UserUUID:   human.UUID,
		ClientID:   "sa_human",
		SecretHash: "h:good-secret",
		Label:      "initial",
		CreatedAt:  time.Now(),
	})

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_human",
		ClientSecret: "good-secret",
		IP:           "10.0.0.6",
	})
	if !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(credential owned by a human user): got %v, want ErrInvalidClientCredentials", err)
	}
}

func TestGrantBadGrantType(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	ctx := context.Background()
	seedGrantFixture(users, creds)

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "password",
		ClientID:     "sa_abc",
		ClientSecret: "good-secret",
		IP:           "10.0.0.7",
	})
	if !errors.Is(err, ErrUnsupportedGrantType) {
		t.Fatalf("Grant(bad grant type): got %v, want ErrUnsupportedGrantType", err)
	}
}

func TestGrantRateLimited(t *testing.T) {
	svc, users, creds, _, limiter := newSAServiceForGrant()
	limiter.SetAuthFailedConfig(1, time.Minute)
	ctx := context.Background()
	seedGrantFixture(users, creds)

	base := GrantInput{
		GrantType: "client_credentials",
		ClientID:  "sa_abc",
		IP:        "10.0.0.8",
	}

	wrong := base
	wrong.ClientSecret = "wrong-secret"
	if _, err := svc.Grant(ctx, wrong); !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(wrong secret, priming lockout): got %v, want ErrInvalidClientCredentials", err)
	}

	good := base
	good.ClientSecret = "good-secret"
	if _, err := svc.Grant(ctx, good); !errors.Is(err, ErrClientRateLimited) {
		t.Fatalf("Grant(correct secret, after lockout tripped): got %v, want ErrClientRateLimited", err)
	}
}

// TestGrantSuccessiveSuccessesNotRateLimited is the regression case for
// the timing/lockout bug the code review caught: Grant's pre-check used
// to call IsBlocked, whose underlying Check consumes a bucket token on
// every call — including successful ones. With a tight lockout
// (threshold 1) that meant the second of two back-to-back *correct*
// Grant calls spuriously returned ErrClientRateLimited, because the
// first call's own pre-check (not a failure) had already spent the
// budget. Grant now pre-checks via IsLockedOut, which peeks the same
// bucket without consuming — this proves two consecutive legitimate
// grants both succeed even under the tightest possible lockout config.
// TestGrantWrongSecretEmitsFailedAuditEvent is Item 1's grant-failure
// coverage: a rejected grant must emit a compliance-audit event carrying
// the internal reason, mirroring emitLoginFailed's shape (compliance
// sink only — no persisted auth_security_events row). The HTTP-visible
// error stays the single indistinguishable ErrInvalidClientCredentials
// regardless.
func TestGrantWrongSecretEmitsFailedAuditEvent(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	sink := &saAuditSinkFake{}
	svc.SetAuditSink(sink)
	ctx := context.Background()
	seedGrantFixture(users, creds)

	_, err := svc.Grant(ctx, GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_abc",
		ClientSecret: "wrong-secret",
		IP:           "10.0.0.30",
	})
	if !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(wrong secret): got %v, want ErrInvalidClientCredentials", err)
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatalf("expected a failed-grant audit event, got none")
	}
	if ev.Action != "service_account.grant_failed" {
		t.Errorf("Action = %q, want service_account.grant_failed", ev.Action)
	}
	if ev.Outcome != "failure" {
		t.Errorf("Outcome = %q, want failure", ev.Outcome)
	}
	if ev.Metadata["reason"] != "bad_secret" {
		t.Errorf("Metadata[reason] = %v, want bad_secret", ev.Metadata["reason"])
	}
	if secret, ok := ev.Metadata["clientSecret"]; ok {
		t.Errorf("metadata leaked the attempted secret: %v", secret)
	}
}

// TestGrantUnknownClientIDReasonGranularity confirms the unknown-client
// branch reports its own distinct reason (not folded into bad_secret).
func TestGrantUnknownClientIDReasonGranularity(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	sink := &saAuditSinkFake{}
	svc.SetAuditSink(sink)
	ctx := context.Background()
	seedGrantFixture(users, creds)

	if _, err := svc.Grant(ctx, GrantInput{
		GrantType: "client_credentials", ClientID: "sa_missing", ClientSecret: "whatever", IP: "10.0.0.31",
	}); !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(unknown clientId): got %v, want ErrInvalidClientCredentials", err)
	}
	if ev, ok := sink.last(); !ok || ev.Metadata["reason"] != "unknown_client" {
		t.Errorf("Metadata[reason] = %v, ok=%v, want unknown_client/true", ev.Metadata["reason"], ok)
	}
}

// TestGrantDisabledAccountReasonGranularity confirms the disabled-account
// branch reports its own distinct reason.
func TestGrantDisabledAccountReasonGranularity(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	sink := &saAuditSinkFake{}
	svc.SetAuditSink(sink)
	ctx := context.Background()
	u, _ := seedGrantFixture(users, creds)
	inactive := false
	if _, err := svc.UpdateAccount(ctx, u.UUID, nil, &inactive); err != nil {
		t.Fatalf("test setup UpdateAccount: %v", err)
	}

	if _, err := svc.Grant(ctx, GrantInput{
		GrantType: "client_credentials", ClientID: "sa_abc", ClientSecret: "good-secret", IP: "10.0.0.32",
	}); !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(disabled account): got %v, want ErrInvalidClientCredentials", err)
	}
	if ev, ok := sink.last(); !ok || ev.Metadata["reason"] != "disabled_account" {
		t.Errorf("Metadata[reason] = %v, ok=%v, want disabled_account/true", ev.Metadata["reason"], ok)
	}
}

// TestGrantRefreshesLockoutConfigFromPolicy is Item 4's coverage: Grant
// must refresh the rate limiter's lockout config from the live
// AuthPolicyService on every call, mirroring the login site. Without the
// fix, the limiter keeps its 3-failure default and a single wrong-secret
// attempt would not trip a threshold-1 lockout.
func TestGrantRefreshesLockoutConfigFromPolicy(t *testing.T) {
	svc, users, creds, _, _ := newSAServiceForGrant()
	seedGrantFixture(users, creds)
	svc.SetPolicy(newPolicy(map[string]string{
		"accountLockoutThreshold": "1",
		"accountLockoutDuration":  "1m",
	}))
	ctx := context.Background()

	wrong := GrantInput{GrantType: "client_credentials", ClientID: "sa_abc", ClientSecret: "wrong-secret", IP: "10.0.0.40"}
	if _, err := svc.Grant(ctx, wrong); !errors.Is(err, ErrInvalidClientCredentials) {
		t.Fatalf("Grant(wrong secret, priming lockout): got %v, want ErrInvalidClientCredentials", err)
	}

	good := wrong
	good.ClientSecret = "good-secret"
	if _, err := svc.Grant(ctx, good); !errors.Is(err, ErrClientRateLimited) {
		t.Fatalf("Grant(correct secret, after policy-driven threshold-1 lockout): got %v, want ErrClientRateLimited", err)
	}
}

func TestGrantSuccessiveSuccessesNotRateLimited(t *testing.T) {
	svc, users, creds, _, limiter := newSAServiceForGrant()
	limiter.SetAuthFailedConfig(1, time.Minute)
	ctx := context.Background()
	seedGrantFixture(users, creds)

	in := GrantInput{
		GrantType:    "client_credentials",
		ClientID:     "sa_abc",
		ClientSecret: "good-secret",
		IP:           "10.0.0.9",
	}

	if _, err := svc.Grant(ctx, in); err != nil {
		t.Fatalf("Grant #1 (correct secret): unexpected error: %v", err)
	}
	if _, err := svc.Grant(ctx, in); err != nil {
		t.Fatalf("Grant #2 (correct secret, immediately after #1): unexpected error: %v", err)
	}
}
