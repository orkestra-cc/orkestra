package services

// Spec §4.9 (v19) / plan review finding #2. The rotation-race classifier has
// three honest answers — healthy family (409), revoked family or window passed
// (replay), or COULD NOT TELL — and the third used to be folded into the
// second, which is the one that mutates. A Mongo blip during a legitimate
// multi-tab race therefore revoked the family the winner had just renewed.
//
// Every positive asserts three things: the sentinel, that the family was NOT
// revoked (active members unchanged AND RevokeFamily never called — the
// second is what proves the classifier did not merely fail to persist a
// verdict it had reached), and that no credentials were issued.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

func assertNothingRevoked(t *testing.T, env *orchestrationEnv, family string, wantActive int, resp *authModels.TokenResponse) {
	t.Helper()
	if n := env.refresh.revokeFamilyCalled(); n != 0 {
		t.Fatalf("RevokeFamily called %d times — the classifier acted on a family state it could not read", n)
	}
	if active := env.refresh.activeFamilyMembers(family); active != wantActive {
		t.Fatalf("active family members = %d, want %d — a sibling's successor died for a store error", active, wantActive)
	}
	if resp != nil {
		t.Fatalf("credentials issued on an unclassifiable race: %+v", resp)
	}
}

// THE case: the benign multi-tab race, with the family read failing at the
// instant the loser presents the superseded cookie. This used to sign every
// tab out.
func TestRaceOutage_BenignRace_FamilyReadFails_Is503_NothingRevoked(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-race-blip")
	rotateOnce(t, env, raw) // the sibling won; its successor is the 1 active member
	env.refresh.setFamilyRevokedErr(errStoreDown)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — 'could not read the family' is not 'the family is revoked'", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved (ruling P9)", err)
	}
	assertNothingRevoked(t, env, doc.FamilyID, 1, resp)
}

// CAS lost, and the re-read that would classify it fails. The shape matters:
// the FIRST read must succeed (so the code reaches the CAS at all) and the
// SECOND must fail — a fake that fails every read tests Task 2's site, not
// this one.
func TestRaceOutage_CASLost_ReReadFails_Is503_NothingRevoked(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-race-reread")
	env.refresh.setRotateErr(repository.ErrTokenAlreadyRotated)
	env.refresh.setOnGetByTokenAny(func(call int, d *authModels.RefreshTokenDoc) (*authModels.RefreshTokenDoc, error) {
		if call >= 2 {
			return nil, errStoreDown
		}
		return d, nil
	})

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved (ruling P9)", err)
	}
	assertNothingRevoked(t, env, doc.FamilyID, 1, resp)
}

// CAS lost, the re-read succeeds and shows the row a sibling rotated inside
// the grace window — a race, by construction — and THEN the family read
// fails. The re-read must return a DIFFERENT row than the first read; that is
// what a race is.
func TestRaceOutage_CASLost_ReReadRotated_FamilyReadFails_Is503_NothingRevoked(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-race-family")
	env.refresh.setRotateErr(repository.ErrTokenAlreadyRotated)
	env.refresh.setOnGetByTokenAny(func(call int, d *authModels.RefreshTokenDoc) (*authModels.RefreshTokenDoc, error) {
		if call >= 2 && d != nil {
			now := time.Now()
			d.IsRevoked = true
			d.RevokedAt = &now
			d.RevokedReason = authModels.RevokeReasonRotated
		}
		return d, nil
	})
	env.refresh.setFamilyRevokedErr(errStoreDown)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved (ruling P9)", err)
	}
	assertNothingRevoked(t, env, doc.FamilyID, 1, resp)
}

// The boundary between "could not decide" (503) and "decided, could not
// persist" (401): a verdict that WAS reached is denied even when RevokeFamily
// fails. Pins that Task 2b did not soften genuine replay detection.
func TestRaceOutage_GenuineReplay_RevokeFails_Still401Replay(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-race-persist")
	hash := rotateOnce(t, env, raw)
	env.refresh.backdateRevocation(hash, RefreshRotationGrace+time.Second) // outside the window: replay by state
	env.refresh.revokeFamilyErr = errStoreDown

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("err = %v, want ErrRefreshTokenReplay — the verdict was reached; failing to persist it must not downgrade it", err)
	}
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatal("a reached replay verdict was reported as an outage")
	}
}
