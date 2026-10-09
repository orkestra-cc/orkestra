package services

// Findings of the whole-branch review of PR D1, each pinned before its fix.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// A soft-deleted owner is not an orphan: GetUserByID answers not found,
// but the account exists (deleted). The identity row stays with it, and
// the sign-in is refused — never healed into a signup.
func TestCallback_SoftDeletedOwnerIsRefusedNotHealed(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.users.softDelete("u-1")

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("a soft-deleted owner's identity row must not be deleted")
	}
	if repo.createUserCalls() != 0 {
		t.Fatal("and no signup may start")
	}
}

// A reservation younger than the grace window may belong to an in-flight
// signup (double tap, two tabs): retryable, not deleted.
func TestCallback_OrphanYoungerThanTheGraceWindowIsRetryable(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedProvider(t, providerDoc{userUUID: "ghost", provider: "google", providerID: "1234", createdAt: time.Now()})

	_, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable (retry later)", err)
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("a young reservation must not be deleted")
	}
	if repo.createUserCalls() != 0 {
		t.Fatal("and no signup may start")
	}
}

// noProbeUsers is a fork's user provider that predates the lifecycle seam.
type noProbeUsers struct{ iface.UserProvider }

// Without a lifecycle probe "not found" cannot be told from "deleted":
// nothing is healed, the sign-in is retryable.
func TestCallback_NoLifecycleProbeMeansNoHealing(t *testing.T) {
	svc, repo := newOwnershipService(t)
	svc.userService = noProbeUsers{repo.users}
	repo.seedProvider(t, providerDoc{userUUID: "ghost", provider: "google", providerID: "1234"})

	_, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable", err)
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("nothing may be deleted without the probe")
	}
}

// A duplicate on the per-user (userUuid, provider) index is not an
// ownership conflict: the account already has a different identity of
// this provider. The answer is the typed already-linked error, not a
// retryable outage the user would retry forever.
func TestAutoLink_SecondIdentityOfTheSameProviderIsAlreadyLinked(t *testing.T) {
	svc, repo := newOwnershipService(t, withAutoLink(true))
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "G1"})

	_, err := completeOAuthCallback(t, svc, "google", "G2", "u1@example.com")
	if !errors.Is(err, ErrOAuthLinkAlreadyExists) {
		t.Fatalf("err = %v, want ErrOAuthLinkAlreadyExists", err)
	}
	if repo.providerDocCount("google", "G2") != 0 {
		t.Fatal("no row may be written for the second identity")
	}
}

// D1 moved every listing onto the provider collection, so an unlink that
// only $pulls the embedded copy is a silent no-op. Both paths delete the
// row (no tombstone yet — that is D2; an older binary sees no row, as
// before).
func TestSelfUnlink_DeletesTheProviderRow(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if repo.providerDocCount("google", "1234") != 0 {
		t.Fatal("the provider row must be gone")
	}
	links, _ := svc.GetOAuthLinks(context.Background(), "u-1")
	if len(links.Links) != 1 || links.Links[0].Provider != authModels.OAuthProviderGitHub {
		t.Fatalf("listing after unlink = %+v, want only github", links.Links)
	}
}

func TestAdminUnlink_DeletesTheProviderRow(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.AdminUnlinkOAuth(context.Background(), "admin-1", "u-1", "google"); err != nil {
		t.Fatalf("admin unlink: %v", err)
	}
	if repo.providerDocCount("google", "1234") != 0 {
		t.Fatal("the provider row must be gone")
	}
	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err == nil {
		t.Fatal("the unlinked identity must not sign in any more (auto-link is off here)")
	}
}

// Apple sends no name, and a type assertion on a missing key would panic
// AFTER the identity is reserved and the sentinel claimed. The signup
// falls back to the email's local part.
func TestSignup_MissingNameFallsBackToTheEmailLocalPart(t *testing.T) {
	svc, repo := newOwnershipService(t)
	_, err := completeOAuthCallbackInfo(t, context.Background(), svc, "google",
		map[string]interface{}{"email": "new.person@example.com", "provider_id": "1234", "email_verified": true})
	if err != nil {
		t.Fatalf("signup without a name: %v", err)
	}
	u, _ := repo.users.GetUserForAuth(context.Background(), "new.person@example.com")
	if u == nil || u.FullName != "new.person" {
		t.Fatalf("FullName = %+v, want the email local part", u)
	}
}

// The compensation must not run on the request context: a client that
// disconnects mid-signup would otherwise strand the reservation.
func TestSignup_CompensationSurvivesACancelledContext(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.failCreateUserWith(errors.New("mongo down"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := completeOAuthCallbackInfo(t, ctx, svc, "google",
		map[string]interface{}{"email": "new@example.com", "name": "N", "provider_id": "1234", "email_verified": true})
	if err == nil {
		t.Fatal("want the creation error")
	}
	if repo.providerDocCount("google", "1234") != 0 {
		t.Fatal("the reservation must be released even though the request context is cancelled")
	}
	if repo.compensationFailureMetric() != 0 {
		t.Fatal("and that must not count as a compensation failure")
	}
}

// Now that the provider collection decides ownership, an empty provider
// id must never resolve to whoever holds (provider, "").
func TestCallback_EmptyProviderIDIsRefused(t *testing.T) {
	svc, repo := newOwnershipService(t)
	_, err := completeOAuthCallbackInfo(t, context.Background(), svc, "google",
		map[string]interface{}{"email": "new@example.com", "name": "N", "provider_id": "", "email_verified": true})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if repo.providerDocCount("google", "") != 0 || repo.createUserCalls() != 0 {
		t.Fatal("nothing may be reserved or created for an empty provider id")
	}
}

// The real state service over the in-memory store: one take, then none.
func TestMobileNonce_StoreAndTakeOnTheMemoryStore(t *testing.T) {
	svc := NewOAuthStateService(NewMemoryOAuthStateStore())
	ctx := context.Background()
	if err := svc.StoreMobileNonce(ctx, "k1", &MobileNonceRecord{Provider: authModels.OAuthProviderGoogle, Tier: "operator", CodeChallenge: "c"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	rec, err := svc.TakeMobileNonce(ctx, "k1")
	if err != nil || rec == nil || rec.CodeChallenge != "c" || rec.Tier != "operator" {
		t.Fatalf("first take = %+v, %v", rec, err)
	}
	if _, err := svc.TakeMobileNonce(ctx, "k1"); err == nil {
		t.Fatal("a second take must find nothing")
	}
	// An expired record is refused even when the store still holds it.
	expired := &MobileNonceRecord{Provider: authModels.OAuthProviderGoogle, Tier: "operator", CodeChallenge: "c", ExpiresAt: time.Now().Add(-time.Second)}
	if err := svc.StoreMobileNonce(ctx, "k2", expired); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := svc.TakeMobileNonce(ctx, "k2"); err == nil {
		t.Fatal("an expired record must be refused")
	}
}
