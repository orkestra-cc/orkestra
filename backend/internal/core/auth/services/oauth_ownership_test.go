package services

// D32 item 5: CreateOAuthProvider's error was ignored on the login path
// and a session was minted anyway, so a session could exist for an
// identity the store never recorded as the caller's. Under the new
// unique index a duplicate-key hit means "someone else owns this" —
// silently ignoring it would be worse than the original bug.

import (
	"errors"
	"testing"
)

func newOwnershipService(t *testing.T, opts ...callbackOpt) (*authService, *tombstoneFixture) {
	t.Helper()
	return newOAuthCallbackService(t, opts...)
}

func TestAutoLink_DuplicateWithTheSameOwnerProceedsOnce(t *testing.T) {
	svc, repo := newOwnershipService(t, withAutoLink(true))
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")
	repo.duplicateOnNextCreate("u-1") // a benign double callback (two tabs)

	res, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if err != nil {
		t.Fatalf("a benign double callback must proceed: %v", err)
	}
	if res == nil {
		t.Fatal("a session must be minted")
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("exactly one document, not two")
	}
}

func TestAutoLink_DuplicateWithAnotherOwnerIsRefused(t *testing.T) {
	svc, repo := newOwnershipService(t, withAutoLink(true))
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")
	repo.duplicateOnNextCreate("someone-else")

	res, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthIdentityClaimedByOther) {
		t.Fatalf("err = %v, want ErrOAuthIdentityClaimedByOther", err)
	}
	if res != nil {
		t.Fatal("no session may be minted for an identity another user owns")
	}
	if repo.addOAuthLinkCalls() != 0 {
		t.Fatal("the read-model must not be touched either")
	}
}

func TestAutoLink_StoreErrorRefusesAndMintsNothing(t *testing.T) {
	svc, repo := newOwnershipService(t, withAutoLink(true))
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")
	repo.failCreateWith(errors.New("mongo down"))

	res, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable", err)
	}
	if res != nil {
		t.Fatal("no session without recorded ownership")
	}
}

// Edge case 27: two first callbacks for one identity race. The first
// insert RESERVES it; the second gets the duplicate key, re-reads, and
// either continues (its own user) or is refused — before any user row
// exists.
func TestSignup_LostReservationRaceCreatesNoUser(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.duplicateOnNextCreate("someone-else")

	_, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com")
	if !errors.Is(err, ErrOAuthIdentityClaimedByOther) {
		t.Fatalf("err = %v", err)
	}
	if repo.createUserCalls() != 0 {
		t.Fatal("no user may be created for a lost reservation race")
	}
	if repo.claimerCalls() != 0 {
		t.Fatal("the sentinel must not be claimed either")
	}
}

// Compensation runs BACKWARDS: delete the identity doc, release the
// sentinel.
func TestSignup_UserCreationFailureCompensates(t *testing.T) {
	svc, repo := newOwnershipService(t, tier(PolicyAudienceOperator))
	repo.failCreateUserWith(errors.New("mongo down"))

	_, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com")
	if err == nil {
		t.Fatal("want an error")
	}
	if repo.providerDocCount("google", "1234") != 0 {
		t.Fatal("the reserved identity document must be deleted")
	}
	if !repo.sentinelReleased() {
		t.Fatal("a claimed sentinel must be released")
	}
}

func TestSignup_CompensationFailureIsCountedNotHidden(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.failCreateUserWith(errors.New("mongo down"))
	repo.failDeleteProvider()

	_, _ = completeOAuthCallback(t, svc, "google", "1234", "new@example.com")
	if repo.compensationFailureMetric() == 0 {
		t.Fatal("a compensation failure must be counted — item 8 heals it, but it must be visible")
	}
}

// Edge case 28 / item 8: a provider doc whose userUuid names a user that
// does NOT exist is an orphan reservation (a crash between the
// reservation and the user creation). It is not a linked identity: it is
// deleted and the flow continues as unlinked. Before, that state was
// terminal: any lookup error was fatal.
func TestCallback_OrphanReservationHealsItself(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedProvider(t, providerDoc{userUUID: "ghost", provider: "google", providerID: "1234"})
	// "ghost" was never created.

	res, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com")
	if err != nil {
		t.Fatalf("the orphan must heal and the signup must proceed: %v", err)
	}
	if res == nil {
		t.Fatal("a session must be minted for the completed signup")
	}
	if repo.providerDocOwner("google", "1234") == "ghost" {
		t.Fatal("the orphan document must be gone")
	}
}

// A user-lookup OUTAGE is not an orphan. Deleting a real user's identity
// because Mongo was briefly unavailable would be catastrophic.
func TestCallback_OrphanCheckDistinguishesOutageFromAbsence(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.failGetUserByIDWith(errors.New("mongo down"))

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable", err)
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("an outage must never delete an identity document")
	}
}

// D33: a degraded lookup must not fall through into the auto-link or
// signup branch.
func TestCallback_IdentityLookupErrorFailsClosed(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.failGetByProviderAndIDWith(errors.New("mongo down"))

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable", err)
	}
	if repo.createUserCalls() != 0 {
		t.Fatal("a degraded lookup must never start a signup")
	}
}

func TestCallback_EmailLookupErrorFailsClosed(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.failGetUserByEmailWith(errors.New("mongo down")) // NOT not-found

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable", err)
	}
	if repo.createUserCalls() != 0 {
		t.Fatal("a degraded email lookup must never start a signup")
	}
}

// A genuine not-found still starts a signup — that is the whole branch.
func TestCallback_EmailNotFoundStillSignsUp(t *testing.T) {
	svc, repo := newOwnershipService(t)
	if _, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com"); err != nil {
		t.Fatalf("a genuine not-found must sign up: %v", err)
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("the signup must leave exactly one identity document")
	}
}

// Existing-link login is UNCHANGED: token refresh, last-used and
// metadata updates stay best-effort — they are not ownership.
func TestExistingLinkLogin_MetadataUpdatesStayBestEffort(t *testing.T) {
	svc, repo := newOwnershipService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")
	repo.failUpdateOAuthTokens()

	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err != nil {
		t.Fatalf("a failed token refresh must not fail the login: %v", err)
	}
}
