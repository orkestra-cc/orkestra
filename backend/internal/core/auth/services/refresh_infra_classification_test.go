package services

// §4.9 / defect C. Four infrastructure failures on the rotation path were
// wrapped in generic errors that writeRefreshErr answered as a plain, codeless
// 401 — the same answer a genuinely dead refresh token produces. A Mongo blip
// therefore reached the SPA as a sign-out, and no client-side rule could tell
// the two apart. ADR-0017 already decided this question for session
// enforcement and gave it a 503; these are its siblings.
//
// The negatives matter as much as the positives: this must not become a
// blanket 503, or a genuinely dead session never ends.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

var errStoreDown = errors.New("mongo: no reachable servers")

func TestRefreshInfra_TokenLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-lookup")
	env.refresh.setGetByTokenAnyErr(errStoreDown)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — an unreachable store answered as an authentication failure trains clients to discard a valid session", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved — whoever reads the log needs to know WHICH store failed", err)
	}
}

func TestRefreshInfra_UserLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-user")
	env.users.setGetByIDErr(errStoreDown)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — this is the site whose wording ('user not found') makes an outage read as a deleted account", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
}

// The negative that keeps the site honest: an account that is genuinely gone
// is terminal, and must stay a 401 or the client never signs out.
//
// The user is deliberately NOT seeded, so the fake's own not-found path is
// what classifies — injecting iface.ErrUserNotFound by hand would test the
// injection, not the store.
func TestRefreshInfra_UserGenuinelyDeleted_IsInvalidToken(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-deleted")

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken — a deleted account answered 503 leaves the SPA holding a token forever, never signed out", err)
	}
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatal("a deleted account must not be reported as an outage")
	}
}

func TestRefreshInfra_MintFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-mint")
	env.breakSigningKey()

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — a signing/key failure is ours, not the caller's", err)
	}
	if !errors.Is(err, ErrJWTKeysNotLoaded) {
		t.Fatalf("err = %v, want the underlying mint failure preserved", err)
	}
}

func TestRefreshInfra_RotateFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-rotate")
	env.refresh.setRotateErr(errStoreDown)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — a failed write is an outage", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
}

// A write that fails with the CAS sentinel is NOT an outage — PROVIDED the
// family state could then be read. Here it can: the re-read succeeds and shows
// a row that is not rotated, so the verdict is replay BY STATE, and RevokeFamily
// runs. The qualifier is the point (plan review finding #2): the unqualified
// "a lost CAS is never an outage" is exactly what let a failed family read be
// answered with a revocation. Task 2b holds the cases where the read fails.
func TestRefreshInfra_RotateCASLoss_WithReadableState_IsReplayByState(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-cas")
	env.refresh.setRotateErr(repository.ErrTokenAlreadyRotated)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("a lost CAS with a READABLE family state was classified as an outage: %v", err)
	}
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("err = %v, want ErrRefreshTokenReplay — the re-read showed a live row, so the lone presented token is a replay signature", err)
	}
	if env.refresh.revokeFamilyCalled() != 1 {
		t.Fatalf("RevokeFamily calls = %d, want 1 — replay by state must still revoke", env.refresh.revokeFamilyCalled())
	}
}

// The remaining negatives, table-driven: each is a genuinely dead credential
// and each must still be a 401. This is what stops §4.9 from turning into a
// blanket 503.
func TestRefreshInfra_TerminalCasesStay401(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, env *orchestrationEnv) string
	}{
		{"no row at all", func(t *testing.T, env *orchestrationEnv) string {
			user := seededUser()
			env.users.seed(user)
			raw, err := env.jwt.GenerateRefreshToken(user)
			if err != nil {
				t.Fatalf("GenerateRefreshToken: %v", err)
			}
			return raw // never seeded → GetByTokenAny returns (nil, nil)
		}},
		{"expired row", func(t *testing.T, env *orchestrationEnv) string {
			user := seededUser()
			env.users.seed(user)
			raw, _ := env.issueAndSeedRefresh(user, "fam-expired", func(d *authModels.RefreshTokenDoc) {
				d.ExpiresAt = time.Now().Add(-time.Hour)
			})
			return raw
		}},
		{"revoked for logout", func(t *testing.T, env *orchestrationEnv) string {
			user := seededUser()
			env.users.seed(user)
			now := time.Now()
			raw, _ := env.issueAndSeedRefresh(user, "fam-logout", func(d *authModels.RefreshTokenDoc) {
				d.IsRevoked = true
				d.RevokedAt = &now
				d.RevokedReason = "logout"
			})
			return raw
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newOrchestrationEnv(t)
			raw := tc.setup(t, env)
			_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
			if !errors.Is(err, ErrInvalidRefreshToken) {
				t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
			}
		})
	}
}

// The FIFTH site (spec v18), and the one that answers the browser: the cookie
// handlers classify every candidate through Peek BEFORE the rotation, so a
// lookup failure here never reaches RefreshTokensWithRiskAssessment at all.
func TestRefreshInfra_PeekLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-peek")
	env.refresh.setGetByTokenAnyErr(errStoreDown)

	_, err := env.auth.PeekRefreshToken(context.Background(), raw)
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — the picker treats every other error as 'not a candidate'", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
}

// ===== The read-only mint (spec §4.9 v20, follow-up 9) =====
//
// GET /v1/auth/session does NOT rotate: after the picker classifies the
// cookies it calls MintAccessTokenFromRefresh, which issues its own
// GetByTokenAny, its own GetUserByID and its own signing call. Those three
// were the residual generic wraps — Peek could succeed and the mint fail on
// the very next read, and the browser was told 401. Same sentinel, same
// not-found-first split, same negative.

func TestMintInfra_TokenLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-mint-lookup")
	env.refresh.setGetByTokenAnyErr(errStoreDown)

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — the mint's own lookup is a second read, and a blip between Peek and here reached the browser as a sign-out", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil — the mint must never hand back credentials it could not verify", resp)
	}
}

func TestMintInfra_UserLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-mint-user")
	env.users.setGetByIDErr(errStoreDown)

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — 'user not found' is the wording that makes an outage read as a deleted account", err)
	}
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
}

func TestMintInfra_MintFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-mint-sign")
	env.breakSigningKey()

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — a signing/key failure is ours, not the caller's", err)
	}
	if !errors.Is(err, ErrJWTKeysNotLoaded) {
		t.Fatalf("err = %v, want the underlying mint failure preserved", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
}

// The negative the mint must not skip: R2's permanent-503 loop in the
// bootstrap endpoint's own words. The user is deliberately NOT seeded, so the
// fake's errNotFound — which already wraps iface.ErrUserNotFound — is what
// classifies.
func TestMintInfra_UserGenuinelyDeleted_IsInvalidToken(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	raw, _ := env.issueAndSeedRefresh(user, "fam-mint-deleted")

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken — a deleted account answered 503 leaves the SPA retrying a session that is never coming back", err)
	}
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatal("a deleted account must not be reported as an outage")
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
}

// ===== A missing VERIFYING key is not a bad token (spec §4.9 v22, §8 #15) =====
//
// The last three sites are a second route into the same misclassification.
// validateTokenEnhanced returns ErrJWTKeysNotLoaded when no public key is
// loaded, and all three service entry points opened by a ValidateRefreshToken
// call used to fold EVERY validation failure into one opaque
// "invalid refresh token" string — so /refresh, /refresh-cookie and /session
// answered a server that cannot verify anything with the same codeless 401
// they answer a dead session with. A boot with no key material is the server's
// own failure: 503, and no client should read it as its session ending.
//
// breakVerifyingKey forces exactly that input, and the ordering inside
// validateTokenEnhanced keeps the test honest: the sentinel is returned before
// jwt.Parse runs, so the seeded row and the token below are genuinely valid.

func TestKeysNotLoaded_Rotate_Is503(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-keys-rotate")
	env.breakVerifyingKey()

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — a server with no verifying key cannot authenticate ANYONE, and answering that 401 signs out every live session", err)
	}
	if !errors.Is(err, ErrJWTKeysNotLoaded) {
		t.Fatalf("err = %v, want the underlying cause preserved — whoever reads the log needs to see it is the key material, not the store", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil — nothing may be handed back for a token that could not be verified", resp)
	}
}

func TestKeysNotLoaded_Peek_Is503(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-keys-peek")
	env.breakVerifyingKey()

	doc, err := env.auth.PeekRefreshToken(context.Background(), raw)
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — the picker treats every other error as 'not a candidate', so an unverifiable cookie would silently become a 401", err)
	}
	if !errors.Is(err, ErrJWTKeysNotLoaded) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
	if doc != nil {
		t.Fatalf("doc = %+v, want nil", doc)
	}
}

func TestKeysNotLoaded_Mint_Is503(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-keys-mint")
	env.breakVerifyingKey()

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — this is the session-bootstrap path, and a codeless 401 there is the one status a client reads as the end of the session", err)
	}
	if !errors.Is(err, ErrJWTKeysNotLoaded) {
		t.Fatalf("err = %v, want the underlying cause preserved", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
}

// The negative that stops the split becoming a blanket 503: every OTHER
// validation failure is a verdict on the credential and keeps the wrap — and
// the 401 — it has today. A garbage token with the keys perfectly loaded is
// the plainest instance.
func TestKeysNotLoaded_InvalidTokenAtRotation_Stays401(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), "not-a-jwt", &authModels.SecurityContext{})
	if err == nil {
		t.Fatal("a malformed refresh token must be rejected")
	}
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v — a malformed token is a verdict, not an outage; 503 there means a dead session never ends", err)
	}
	if !strings.Contains(err.Error(), "invalid refresh token") {
		t.Fatalf("err = %v, want the existing \"invalid refresh token\" wrap untouched", err)
	}
}
