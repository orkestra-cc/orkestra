# Auth/Authz Audit Remediation — PR D2: OAuth Tombstone (Contract) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make unlinking an OAuth identity actually refuse that identity at the next callback — the contract half of H-7 — by writing the tombstone PR D1 already knows how to read, and by defining what happens when the same user re-links it or a different user claims it.

**Architecture:** This is the *contract* half of an expand-before-contract pair. PR D1 made every reader tombstone-aware and wrote none; this release turns the writes on. Both unlink paths resolve the `providerID` from the provider collection (not from `user.oauthLinks`), call a new `MarkUnlinked`, and only when that **succeeds** `$pull` the embedded link — a failed tombstone removes nothing and answers 503, because a half-unlink that leaves the identity valid is worse than a refused one. Re-linking the same identity as the same user *revives* the document in place; a different user's claim *replaces* it, because the identity moved. From the moment the first tombstone exists on an environment, **PR D1 is the hard rollback floor**.

**Tech Stack:** Go 1.26.8, MongoDB 8 (`$set unlinkedAt` / `$unset` on revive), Huma v2.39.1.

**Spec:** `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` **v1.12** — this plan implements the **D2 (contract)** row of §7: §4.8 **item 2** and the revive/move rules of **item 4**, plus the rollback-floor row of §7 and the §6 tests for those.

**Depends on:** **PR D1 must be deployed and verified on EVERY environment first.** That is not a convenience: the rollback floor moves to D1 the moment the first tombstone is written, and a binary that cannot read `unlinkedAt` would silently re-enable every identity unlinked since.

## Global Constraints

- **Do not deploy this until PR D1 is live and verified on every environment**, including production. The release notes must name the floor.
- **The promote playbook's pre-flight gains one line:** *"does the target environment hold tombstones? then the rollback target is D1 or later."*
- **A failed tombstone removes NOTHING.** The provider-doc write is primary: if `MarkUnlinked` fails, the embedded `$pull` does not run, the identity stays valid, and the caller sees **503 `auth.oauth_store_unavailable`**. A half-unlink that leaves the identity able to sign in — while the UI says it is gone — is worse than a refusal.
- **The `providerID` comes from the provider collection, never from `user.oauthLinks`.** The embedded slice is a derived read-model (PR D1) and can be missing entirely for a login-created link.
- **Revive is in place, for the same owner only.** A tombstoned document owned by a *different* user is deleted and replaced: the identity moved, and the old owner never sees it again.
- **The embedded write stays best-effort** on both paths (WARN on failure). It decides nothing.
- **The lockout guard runs BEFORE any mutation,** unchanged: a user must not be able to remove their last way in.
- **Docs move in the same commit:** `backend/internal/core/auth/CLAUDE.md`, `docs/site/modules/core/auth.mdx`, and the release notes.
- **Test commands** (from `/home/tore/orkestra/backend`): `go test ./internal/core/auth/... -count=1`; `go vet ./...` before every commit; `make -C /home/tore/orkestra ci-backend`; live Mongo where guarded: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`.
- **Never start servers manually.** **Commit trailer:** `Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1`

## Declared deviations from the spec (read before executing)

1. **`MarkUnlinked` is keyed by `(userUUID, provider)`, not by the document UUID.** The spec writes `MarkUnlinked(ctx, userUUID, provider, now)`; this plan keeps that signature. The existing `(userUuid, provider)` unique index makes it address exactly one row.
2. **A `MarkUnlinked` that matches zero documents is `ErrOAuthLinkNotFound`, not success.** The spec does not say; reporting success for an unlink that unlinked nothing would be the same class of lie as the half-unlink this PR removes.
3. **The revive path is written as part of this PR even though `SelfLinkOAuthFromCallback` is not itself a tombstone writer.** Without it the first user to unlink and re-link an identity is permanently locked out of that method — the feature would ship broken.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `backend/internal/core/auth/repository/oauth_provider_repository.go` | + `MarkUnlinked`, `ReviveIdentity` | 1 |
| `backend/internal/core/auth/services/auth_service.go` | `AdminUnlinkOAuth` (`:612-684`), `SelfUnlinkOAuth` (`:724-770`), `SelfLinkOAuthFromCallback` (`:772-904`) | 1, 2 |
| `backend/internal/core/auth/services/auth_service_admin_unlink_test.go`, `_self_unlink_test.go`, `auth_service_self_link_test.go` | the §6 rows | 1, 2 |
| `backend/internal/core/auth/CLAUDE.md` | unlink semantics, revive, the rollback floor | 3 |
| `docs/site/modules/core/auth.mdx` | the same, human-facing | 3 |

---

## Task 1: Both unlink paths write the tombstone (item 2)

**Files:**
- Modify: `backend/internal/core/auth/repository/oauth_provider_repository.go`
- Modify: `backend/internal/core/auth/services/auth_service.go` (`AdminUnlinkOAuth` `:612-684`, `SelfUnlinkOAuth` `:724-770`)
- Test: `backend/internal/core/auth/services/auth_service_admin_unlink_test.go`, `auth_service_self_unlink_test.go`

**Interfaces:**
- Consumes: `models.OAuthProviderDoc.UnlinkedAt` (PR D1 Task 1), `GetByUserUUID` (tombstone-excluding, PR D1 Task 4), `ErrOAuthStoreUnavailable` (PR D1 Task 5), `wouldLockOutOAuthUnlink`.
- Produces: `repository.MarkUnlinked(ctx, userUUID string, provider models.OAuthProvider, at time.Time) error`

- [ ] **Step 1: Write the failing tests**

Extend `auth_service_self_unlink_test.go` (and mirror every case in `_admin_unlink_test.go`), with `fakeOAuthProviderRepo` gaining an `unlinked map[string]time.Time`:

```go
// H-7, contract half: unlink $pulled only user.oauthLinks, so the
// provider document — the thing LOGIN keys on — survived and the
// identity kept signing in. PR D1 taught every reader to honour a
// tombstone; this writes one.

func TestSelfUnlink_TombstonesTheProviderDocument(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("SelfUnlinkOAuth: %v", err)
	}
	if !repo.isUnlinked("google", "1234") {
		t.Fatal("the provider document must carry a tombstone")
	}
	if repo.userHasEmbeddedLink("u-1", "google", "1234") {
		t.Fatal("the embedded read-model must be pulled too")
	}
}

// The regression the whole PR exists for.
func TestSelfUnlink_ThenTheSameIdentitySignsIn(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); !errors.Is(err, ErrOAuthIdentityUnlinked) {
		t.Fatalf("err = %v, want ErrOAuthIdentityUnlinked — an unlinked identity must not sign in", err)
	}
}

// A FAILED tombstone removes nothing: the identity stays valid and the
// caller is told. A half-unlink that leaves the identity able to sign in
// while the UI says it is gone is worse than a refusal.
func TestSelfUnlink_TombstoneFailureRemovesNothing(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})
	repo.failMarkUnlinked(errors.New("mongo down"))

	err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable", err)
	}
	if repo.userHasEmbeddedLink("u-1", "google", "1234") == false {
		t.Fatal("the embedded link must NOT be pulled when the tombstone failed")
	}
	if repo.isUnlinked("google", "1234") {
		t.Fatal("nothing may be tombstoned")
	}
}

// The embedded write stays best-effort: it decides nothing.
func TestSelfUnlink_EmbeddedPullFailureIsNotFatal(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})
	repo.failRemoveOAuthLinkFromUser()

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("the read-model write is best-effort: %v", err)
	}
	if !repo.isUnlinked("google", "1234") {
		t.Fatal("the tombstone — the write that matters — must have landed")
	}
}

// The providerID comes from the PROVIDER COLLECTION. A login-created
// link has no embedded entry at all, and resolving from there would
// unlink nothing while reporting success.
func TestSelfUnlink_ResolvesProviderIDFromTheProviderCollection(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("a login-created link must be unlinkable: %v", err)
	}
	if !repo.isUnlinked("google", "1234") {
		t.Fatal("the tombstone must land on the row the provider collection holds")
	}
}

// An unlink that matched no document is NOT a success.
func TestSelfUnlink_NoSuchLinkIsNotFound(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrOAuthLinkNotFound) {
		t.Fatalf("err = %v, want ErrOAuthLinkNotFound", err)
	}
}

// The lockout guard still runs FIRST — a user must not remove their last
// way in, and it must be checked before anything is written.
func TestSelfUnlink_LastCredentialIsRefusedBeforeAnyWrite(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedUserWithNoPassword(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
		t.Fatalf("err = %v, want ErrLastCredentialRemoval", err)
	}
	if repo.isUnlinked("google", "1234") {
		t.Fatal("nothing may be written when the unlink is refused")
	}
}

// Admin path: identical, plus the self-action guard it already has.
func TestAdminUnlink_TombstonesAndKeepsTheSelfActionGuard(t *testing.T) {
	svc, repo := newUnlinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "target-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "target-1", provider: "github", providerID: "9999"})

	if err := svc.AdminUnlinkOAuth(context.Background(), "admin-1", "admin-1", "google"); !errors.Is(err, ErrAdminSelfAction) {
		t.Fatalf("the self-action guard must still fire: %v", err)
	}
	if err := svc.AdminUnlinkOAuth(context.Background(), "admin-1", "target-1", "google"); err != nil {
		t.Fatalf("AdminUnlinkOAuth: %v", err)
	}
	if !repo.isUnlinked("google", "1234") {
		t.Fatal("the admin path must tombstone too")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/core/auth/services/ -run 'SelfUnlink_|AdminUnlink_' -count=1`
Expected: FAIL — `MarkUnlinked` does not exist and nothing is tombstoned.

- [ ] **Step 3: Add the repository method**

```go
// MarkUnlinked tombstones the caller's identity for one provider.
//
// A hard delete would be undone by the very next callback: the unlinked
// branch auto-links by verified email, so the operator would have
// removed nothing. The tombstone is what makes an unlink stick, and
// every reader has honoured it since the previous release.
//
// Keyed by (userUuid, provider), which the existing unique index makes
// address exactly one row. A zero match is ErrOAuthLinkNotFound, never
// success: reporting an unlink that unlinked nothing would be the same
// lie as leaving the document live.
func (r *oauthProviderRepository) MarkUnlinked(ctx context.Context, userUUID string, provider models.OAuthProvider, at time.Time) error {
	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"userUuid": userUUID, "provider": provider, "unlinkedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"unlinkedAt": at, "updatedAt": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("mark oauth identity unlinked: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrOAuthProviderNotFound
	}
	return nil
}
```

- [ ] **Step 4: Rewrite both unlink paths**

In `SelfUnlinkOAuth` and `AdminUnlinkOAuth`, after the lockout guard and **replacing** the `RemoveOAuthLinkFromUser` call:

```go
	// The provider document is the source of truth, so it is the write
	// that must succeed. The embedded slice is a derived read-model —
	// a login-created link has no entry there at all, which is why the
	// providerID is resolved from the provider collection above.
	//
	// A FAILED tombstone removes nothing: the identity stays valid and
	// the caller is told. A half-unlink that leaves the identity able to
	// sign in while the UI reports it gone is worse than a refusal.
	if err := s.oauthProviderRepo.MarkUnlinked(ctx, userUUID, models.OAuthProvider(provider), time.Now()); err != nil {
		if errors.Is(err, repository.ErrOAuthProviderNotFound) {
			return ErrOAuthLinkNotFound
		}
		s.logger.Error("auth: failed to tombstone an oauth identity; nothing was removed",
			slog.String("user_uuid", userUUID),
			slog.String("provider", string(provider)),
			slog.String("error", err.Error()))
		return ErrOAuthStoreUnavailable
	}

	// Read-model only, best-effort: it decides nothing.
	if err := s.userService.RemoveOAuthLinkFromUser(ctx, userUUID, provider, providerID); err != nil {
		s.logger.Warn("auth: could not pull the embedded oauth link after unlinking",
			slog.String("user_uuid", userUUID),
			slog.String("provider", string(provider)),
			slog.String("error", err.Error()))
	}
```

`providerID` is resolved from `GetByUserUUID` (tombstone-excluding) rather than from `user.OAuthLinks`; `wouldLockOutOAuthUnlink` already reads the provider docs since PR D1.

- [ ] **Step 5: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
cd /home/tore/orkestra && git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): make an OAuth unlink actually stick (H-7)

Unlink $pulled only user.oauthLinks, so the provider document — the
thing login keys on — survived and the identity kept signing in. A hard
delete would not have helped: the callback's unlinked branch auto-links
by verified email, so the very next sign-in would have re-created it and
the operator would have removed nothing.

Both unlink paths now write the tombstone every reader has honoured
since the previous release, resolving the providerID from the provider
collection (a login-created link has no embedded entry at all). The
provider write is primary: when it fails, nothing is removed, the
identity stays valid and the caller sees 503 — a half-unlink that leaves
the identity able to sign in while the UI reports it gone is worse than
a refusal. The embedded pull stays best-effort; it decides nothing.

An unlink that matched no document is ErrOAuthLinkNotFound, not success.

⚠️ Rollback floor: from the first tombstone on an environment, the
lowest safe rollback target is the previous release (which reads
unlinkedAt and writes none). An older binary would re-enable every
identity unlinked since.

Spec §4.8 D32 item 2.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 2: Re-linking revives; a different user's claim replaces (item 4)

Without this the first user to unlink and re-link an identity is permanently locked out of that sign-in method — the unique index refuses the insert and the tombstone refuses the login.

**Files:**
- Modify: `backend/internal/core/auth/repository/oauth_provider_repository.go` (+ `ReviveIdentity`)
- Modify: `backend/internal/core/auth/services/auth_service.go` (`SelfLinkOAuthFromCallback` `:772-904`)
- Test: `backend/internal/core/auth/services/auth_service_self_link_test.go`

**Interfaces:**
- Consumes: `GetByProviderAndIDIncludingUnlinked` (PR D1 Task 4), `claimIdentity` (PR D1 Task 5), `DeleteProvider`.
- Produces: `repository.ReviveIdentity(ctx, docUUID string, at time.Time) error` — clears `unlinkedAt`, refreshes `linkedAt`.

- [ ] **Step 1: Write the failing tests**

```go
// Edge case 21: without revive, the first user to unlink and re-link an
// identity is permanently locked out of that method — the unique index
// refuses the insert and the tombstone refuses the login.
func TestSelfLink_RevivesTheSameUsersTombstonedIdentity(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.seedProvider(t, providerDoc{
		uuid: "doc-1", userUUID: "u-1", provider: "google", providerID: "1234",
		unlinkedAt: ptr(time.Now().Add(-time.Hour)),
	})

	if err := svc.SelfLinkOAuthFromCallback(context.Background(), "u-1", "google", userInfo("1234", "u1@example.com"), nil); err != nil {
		t.Fatalf("re-link: %v", err)
	}
	if repo.isUnlinked("google", "1234") {
		t.Fatal("the tombstone must be cleared")
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("revived IN PLACE — not a second document")
	}
	if !repo.linkedAtRefreshed("doc-1") {
		t.Fatal("linkedAt must be refreshed: this is a new link decision")
	}
	if !repo.userHasEmbeddedLink("u-1", "google", "1234") {
		t.Fatal("the read-model must be re-added")
	}
}

// And it signs in again afterwards — the whole point.
func TestSelfLink_RevivedIdentitySignsIn(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.seedProvider(t, providerDoc{uuid: "doc-1", userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})

	if err := svc.SelfLinkOAuthFromCallback(context.Background(), "u-1", "google", userInfo("1234", "u1@example.com"), nil); err != nil {
		t.Fatalf("re-link: %v", err)
	}
	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err != nil {
		t.Fatalf("a revived identity must sign in: %v", err)
	}
}

// Edge case 22: the identity MOVED. B links what A had unlinked — A's
// tombstone is deleted, B's document created, and A never sees it again.
func TestSelfLink_TombstonedIdentityOwnedByAnotherUserIsReplaced(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.seedProvider(t, providerDoc{uuid: "doc-a", userUUID: "user-a", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})

	if err := svc.SelfLinkOAuthFromCallback(context.Background(), "user-b", "google", userInfo("1234", "b@example.com"), nil); err != nil {
		t.Fatalf("the identity moved and must be linkable: %v", err)
	}
	if repo.providerDocOwner("google", "1234") != "user-b" {
		t.Fatalf("owner = %q, want user-b", repo.providerDocOwner("google", "1234"))
	}
	if repo.providerDocCount("google", "1234") != 1 {
		t.Fatal("exactly one document")
	}
}

// An ACTIVE identity owned by someone else is still refused, exactly as
// before — only a TOMBSTONED one can move.
func TestSelfLink_ActiveIdentityOwnedByAnotherUserIsStillRefused(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "user-a", provider: "google", providerID: "1234"})

	if err := svc.SelfLinkOAuthFromCallback(context.Background(), "user-b", "google", userInfo("1234", "b@example.com"), nil); !errors.Is(err, ErrOAuthLinkClaimedByOther) {
		t.Fatalf("err = %v, want ErrOAuthLinkClaimedByOther", err)
	}
}

// The provider write is PRIMARY on this path too (PR D1 item 4): an
// error fails the link.
func TestSelfLink_ProviderWriteFailureFailsTheLink(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.failReviveWith(errors.New("mongo down"))
	repo.seedProvider(t, providerDoc{uuid: "doc-1", userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})

	if err := svc.SelfLinkOAuthFromCallback(context.Background(), "u-1", "google", userInfo("1234", "u1@example.com"), nil); err == nil {
		t.Fatal("a failed provider write must fail the link")
	}
	if repo.userHasEmbeddedLink("u-1", "google", "1234") {
		t.Fatal("the read-model must not be written when the primary write failed")
	}
}

// The existing "already linked" guard is unchanged for an ACTIVE link.
func TestSelfLink_AlreadyActivelyLinkedIsStillRefused(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})

	if err := svc.SelfLinkOAuthFromCallback(context.Background(), "u-1", "google", userInfo("1234", "u1@example.com"), nil); !errors.Is(err, ErrOAuthLinkAlreadyExists) {
		t.Fatalf("err = %v, want ErrOAuthLinkAlreadyExists", err)
	}
}

// The audit row says what happened.
func TestSelfLink_ReviveEmitsSelfOAuthLink(t *testing.T) {
	svc, repo := newSelfLinkService(t)
	repo.seedProvider(t, providerDoc{uuid: "doc-1", userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})

	_ = svc.SelfLinkOAuthFromCallback(context.Background(), "u-1", "google", userInfo("1234", "u1@example.com"), nil)
	if !repo.sawSecurityEvent("self_oauth_link") {
		t.Fatal("a revive is a link and must be audited as one")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/core/auth/services/ -run SelfLink_ -count=1`
Expected: FAIL — the tombstoned document blocks the insert with a duplicate key and nothing revives.

- [ ] **Step 3: Add `ReviveIdentity`**

```go
// ReviveIdentity clears a tombstone and refreshes linkedAt.
//
// Revive rather than delete-and-insert: the document's uuid, its token
// rows and its history stay put, and there is no window in which the
// identity exists in neither state.
func (r *oauthProviderRepository) ReviveIdentity(ctx context.Context, docUUID string, at time.Time) error {
	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"uuid": docUUID},
		bson.M{
			"$unset": bson.M{"unlinkedAt": ""},
			"$set":   bson.M{"linkedAt": at, "updatedAt": time.Now()},
		},
	)
	if err != nil {
		return fmt.Errorf("revive oauth identity: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrOAuthProviderNotFound
	}
	return nil
}
```

- [ ] **Step 4: Branch `SelfLinkOAuthFromCallback`**

Before the existing claimed-by-other and already-exists guards:

```go
	// A tombstoned document is not "already linked" and not "claimed by
	// another user" — it is an identity nobody currently owns.
	existing, err := s.oauthProviderRepo.GetByProviderAndIDIncludingUnlinked(ctx, models.OAuthProvider(provider), providerID)
	if err != nil {
		return ErrOAuthStoreUnavailable
	}
	if existing != nil && existing.UnlinkedAt != nil {
		if existing.UserUUID == userUUID {
			// Same owner: REVIVE in place. Without this the first user to
			// unlink and re-link an identity is permanently locked out of
			// that method — the unique index refuses a fresh insert and
			// the tombstone refuses the login.
			if err := s.oauthProviderRepo.ReviveIdentity(ctx, existing.UUID, time.Now()); err != nil {
				return ErrOAuthStoreUnavailable
			}
			// Read-model, best-effort.
			if err := s.userService.AddOAuthLinkToUser(ctx, userUUID, link); err != nil {
				s.logger.Warn("auth: could not re-add the embedded oauth link after a revive",
					slog.String("user_uuid", userUUID), slog.String("error", err.Error()))
			}
			s.RecordSelfAuthEvent(ctx, "self_oauth_link", userUUID, map[string]interface{}{
				"provider": string(provider), "revived": true,
			})
			return nil
		}
		// A DIFFERENT owner: the identity moved. Delete the tombstone and
		// let the ordinary ownership-first write create this user's row —
		// the old owner never sees it again.
		if err := s.oauthProviderRepo.DeleteProvider(ctx, existing.UUID); err != nil {
			return ErrOAuthStoreUnavailable
		}
	}
```

An **active** document owned by someone else still returns `ErrOAuthLinkClaimedByOther`, and an active one owned by this user still returns `ErrOAuthLinkAlreadyExists`. Only a tombstoned document takes the new branches.

- [ ] **Step 5: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
feat(auth): revive a re-linked OAuth identity, and let one move users

Without this the first user to unlink and re-link an identity is
permanently locked out of that sign-in method: the unique index refuses
a fresh insert and the tombstone refuses the login.

Re-linking the same identity as the same user revives the document in
place — uuid, token rows and history stay put, and there is no window in
which the identity exists in neither state. A tombstoned document owned
by a DIFFERENT user is deleted and replaced: the identity moved, and the
old owner never sees it again.

An ACTIVE document is unchanged in both directions: another user's is
still ErrOAuthLinkClaimedByOther, and this user's own is still
ErrOAuthLinkAlreadyExists. Only a tombstone takes the new branches, and
the provider write stays primary — a failure fails the link rather than
leaving the read-model ahead of the truth.

Spec §4.8 D32 item 4.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 3: Documentation, the rollback floor, and the staging drill

- [ ] **Step 1: Document the semantics and the floor**

`backend/internal/core/auth/CLAUDE.md`:
- unlink writes a tombstone; the provider document is the source of truth and the embedded slice a derived read-model;
- a failed tombstone removes nothing and answers 503;
- re-link revives, a different user's claim replaces;
- **the rollback floor**: from the first tombstone on an environment, the lowest safe rollback target is the previous release. State plainly what an older binary does — it ignores `unlinkedAt` and re-enables every identity unlinked since.

`docs/site/modules/core/auth.mdx` — the same, human-facing, under the OAuth section.

- [ ] **Step 2: Write the release notes and the pre-flight line**

The release notes for this version must name the floor. Add one line to the promote playbook's pre-flight:

> *Does the target environment hold OAuth tombstones (`db.operator_oauth_providers.countDocuments({unlinkedAt: {$exists: true}})` > 0)? Then the rollback target is the D1 release or later.*

- [ ] **Step 3: Gate**

```bash
make -C /home/tore/orkestra ci-backend
git diff --check
```

- [ ] **Step 4: Commit and open the PR**

```bash
cd /home/tore/orkestra && git add docs backend
git commit -m "$(cat <<'EOF'
docs(auth): document tombstone semantics and the rollback floor

Spec §4.8 item 2, §7 rollback-floor table.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
git push -u origin feat/auth-authz-audit-remediation-pr-d2
gh pr create --base dev --title "PR D2: OAuth tombstone writes (contract) — auth/authz audit remediation" --body "$(cat <<'EOF'
Implements §4.8 item 2 and the revive/move rules of item 4 of `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` — the **D2 (contract)** row of §7.

**Closes:** H-7 (contract half — an unlink now actually refuses the identity).

⚠️ **Do not merge or deploy until PR D1 is live and verified on EVERY environment, production included.** From the first tombstone written, **PR D1 is the hard rollback floor**: an older binary ignores `unlinkedAt` and would re-enable every identity unlinked since.

The promote pre-flight gains one line: *does the target environment hold tombstones? then the rollback target is D1 or later.*

Plan: `docs/superpowers/plans/2026-09-03-auth-authz-audit-remediation-pr-d2-oauth-tombstone-contract.md`

https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

- [ ] **Step 5: Staging drill (spec §7, D2 row)**

1. Unlink Google from a test account, then sign in with Google → **`oauth_identity_unlinked`**.
2. Re-link it from Security → works, and signing in with Google works again.
3. Unlink it, then link the **same** Google identity from a **different** account → succeeds, and the first account no longer lists it.
4. Try to unlink an account's **only** way in → `ErrLastCredentialRemoval`, and nothing is written.
5. **Roll back staging to the D1 build** and confirm the unlinked identity is **still refused** — that is the property that makes the floor D1 rather than something older.
6. After the `main` push, verify the three touched docs pages **at the destination**, cache-busted. A green sender proves nothing (`project_orkestra_docs_site`); URLs without a trailing slash 308, so use `curl -sL`.

---

## Self-review

**Spec coverage (§4.8 item 2 + item 4's revive/move rules, §7 rollback floor):**

| Spec item | Task |
|---|---|
| item 2 — `MarkUnlinked` on both unlink paths, providerID from the provider collection, failed tombstone removes nothing (503), embedded `$pull` best-effort | 1 |
| item 4 — revive in place for the same owner; delete-and-replace for a different owner; provider write primary | 2 |
| §7 rollback floor + the pre-flight line | 3 |
| §6 rows: unlink tombstones then pulls; tombstone failure removes nothing; the unlink → callback regression; re-link revives; identity moves between users | 1, 2 |

**Placeholder scan:** none.

**Type consistency:** `MarkUnlinked(ctx, userUUID string, provider models.OAuthProvider, at time.Time) error` and `ReviveIdentity(ctx, docUUID string, at time.Time) error` are each declared once and used with those signatures. `ErrOAuthStoreUnavailable`, `ErrOAuthIdentityUnlinked`, `ErrOAuthLinkNotFound`, `ErrOAuthLinkClaimedByOther`, `ErrOAuthLinkAlreadyExists` and `ErrLastCredentialRemoval` all come from PR D1 or the existing tree — this PR declares no new sentinel.

**One risk worth naming:** Task 1's `MarkUnlinked` filter includes `unlinkedAt: {$exists: false}`, so unlinking an already-unlinked identity matches zero documents and returns `ErrOAuthLinkNotFound`. That is correct — but only because `wouldLockOutOAuthUnlink` reads tombstone-excluding provider docs (PR D1 Task 4) and would have reported `found=false` first. If that ordering is ever changed, the 404 becomes reachable for a plain double-click. A reviewer should confirm the lockout guard still runs before the write.
