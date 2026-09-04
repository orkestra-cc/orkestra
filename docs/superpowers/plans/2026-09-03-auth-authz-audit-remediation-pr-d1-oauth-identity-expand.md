# Auth/Authz Audit Remediation — PR D1: OAuth Identity (Expand) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the global `super_admin` seat claimable only by the operator tier and never on an install that already has one (H-6); make the provider-side OAuth collection the single source of truth for identity ownership, so no session is ever minted for an identity the store does not record as the caller's (H-7, the read and write halves); and bind the OAuth and mobile flows to a challenge the client actually holds (M-18, M-20).

**Architecture:** Expand-before-contract. This release makes every reader **tombstone-aware** — a provider document carrying `unlinkedAt` is refused at the callback and hidden from every listing — but writes **no tombstone**; PR D2 turns the writes on and sets the hard rollback floor here. Ownership becomes the *first* durable step of every link path, enforced by a new unique index on `(provider, providerId)` that a migration creates only after an operator has resolved every cross-user conflict by hand; a duplicate key is re-read and either continued (same owner) or refused (`oauth_identity_conflict`), and any other store error refuses too. Signup becomes a reservation with backwards compensation, and an orphan reservation heals itself on the next callback. The OAuth start endpoints mint a PKCE verifier for providers that accept one, and the mobile flow becomes `begin` + `complete` with a server-issued nonce bound to a client-held verifier.

**Tech Stack:** Go 1.26.8, MongoDB 8 (unique index + a `mongosh` migration), Redis 8.2 (`OAuthStateStore.Take` = GETDEL), Huma v2.39.1, `crypto/subtle` for the constant-time verifier compare, RS256 ID-token validation (`google_oauth_service.go`, `apple_oauth_service.go`).

**Spec:** `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` **v1.12** — this plan implements the **D1 (expand)** row of §7: §4.7 (**D30, D31**), §4.8 **items 1 and 3–8** plus **D33** (item 2's tombstone *writes* and item 4's revive/move rules are PR D2), §4.9 (**D34**) and §4.10 (**D35**).

**Independent of PRs A, B and C.**

## Global Constraints

- **Expand only. This release WRITES NO TOMBSTONE.** Every reader honours `unlinkedAt`; the unlink routes keep today's `$pull`-only behaviour. PR D2 turns the writes on, and from the moment the first tombstone exists the hard rollback floor is **this release**.
- **Migration 0010 must have exited zero on every environment BEFORE this deploys.** A conflict report stops the rollout until an operator names the keeper per identity.
- **A deploy that skipped the migration must not run the ownership-first flow.** The auth module verifies the unique index at `Start`; when it is missing the health check reports degraded and every OAuth login and link path answers `oauth_store_unavailable`.
- **The migration never decides ownership.** The existing `(userUuid, provider)` unique index makes two rows of one user for one provider impossible, so every duplicate on `(provider, providerId)` is an identity claimed by two *different* users — a conflict no rule can settle. It reports and refuses; reconciliation is an explicit per-identity resolution map.
- **Ownership is written FIRST, and nothing is minted without it.** `CreateOAuthProvider` stops being best-effort on the login path. Duplicate key → re-read: same owner continues, other owner is refused with `oauth_identity_conflict`. Any other error → `oauth_store_unavailable`. No session either way.
- **A claim error is fatal.** `ClaimFirstAdmin` failing must not be swallowed: a swallowed error is how a lost race becomes a silent `guest`, and the password path already treats it as fatal (`password_auth_service.go:310`).
- **Degraded lookups fail closed.** A non-nil error from `GetByProviderAndID`, or a non-not-found error from `GetUserByEmail`, is `oauth_store_unavailable` — never a fall-through into the auto-link or signup branch.
- **`SupportsPKCE` starts `true` for Google and Discord only.** GitHub and Apple stay `false` until the staging round-trip in §7 confirms their token endpoints accept `code_verifier`; promoting them is then a one-line change.
- **The mobile `access_token` field is removed.** It was stored unverified.
- **Every mobile failure is ONE opaque 401.** A record that was taken is not restored — a failed completion burns the challenge, as the web relay does.
- **Docs move in the same commit as the code:** `backend/internal/core/auth/CLAUDE.md`, `docs/site/modules/core/auth.mdx`, `docs/site/architecture/authentication-flow.mdx`, `docs/migrations/0010_oauth_provider_identity_unique.md`, `mobile/README.md` + `mobile/CLAUDE.md`.
- **Test commands** (from `/home/tore/orkestra/backend` unless stated):
  - `go test ./internal/core/auth/... -count=1`; `go vet ./...` before every commit
  - migration: `node --test migrations/20260903_oauth_provider_identity_unique.test.js` (match how `20260823_authz_bindings_unique.test.js` is run — read its header)
  - full gate: `make -C /home/tore/orkestra ci-backend`
  - live Mongo: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`
- **Never start servers manually.** **Commit trailer:** `Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1`

## Declared deviations from the spec (read before executing)

1. **The boot index check is a flagged addition** (spec D32 item 1 says so itself). It is what stops a deploy that skipped the migration from running the ownership-first flow without its constraint.
2. **D33 is a flagged addition** (spec says so). It rewrites the same ten lines D32 rewrites.
3. **`ErrOAuthStoreUnavailable` and `ErrOAuthIdentityClaimedByOther` are declared next to the existing OAuth sentinels in `auth_service.go`,** not in a new file — every sibling lives there.
4. **`UnlinkedAt` is added to the model in THIS release even though nothing writes it.** That is the expand half: a reader that does not know the field cannot honour it, and PR D2's rollback floor depends on this release already reading it.
5. **The mobile `begin` route is registered as a public route** alongside the existing mobile completion routes, and its record is stored in the `OAuthStateStore` under a distinct key prefix so it can never collide with a web state row.

## File Structure

**Migration**

| File | Responsibility | Task |
|---|---|---|
| `backend/migrations/20260903_oauth_provider_identity_unique.js` (new) | report-and-refuse, `RESOLVE` map, index creation | 1 |
| `backend/migrations/20260903_oauth_provider_identity_unique.test.js` (new) | conflict / resolve / clean / re-run | 1 |
| `docs/migrations/0010_oauth_provider_identity_unique.md` (new) | runbook | 1 |

**Backend — `backend/internal/core/auth/`**

| File | Responsibility | Task |
|---|---|---|
| `models/collections.go` | + `OAuthProviderDoc.UnlinkedAt` | 1 |
| `module.go` | index specs; boot index verification; health degrade; backfill in `Start` | 1, 3 |
| `maintenance.go` | the sentinel backfill | 3 |
| `services/auth_service.go` | D30 audience guard; tombstone-aware readers; ownership-first writes; compensation; orphan healing; D33 | 2, 4, 5 |
| `repository/oauth_provider_repository.go` | tombstone-aware queries; duplicate-key surface | 4, 5 |
| `handlers/oauth_callback_redirect.go` | + `oauth_identity_unlinked`, `oauth_identity_conflict`, `oauth_store_unavailable` | 4, 5 |
| `handlers/auth_handler.go` | PKCE at the two start endpoints; mobile `begin` + `complete` | 6, 7 |
| `handlers/oauth_callback_flow.go` | `codeVerifier` threaded into the exchange | 6 |
| `services/*_oauth_service.go` | `SupportsPKCE`; `Issuers` + `ExpectedNonce` validation | 6, 7 |
| `services/oauth_provider_interface.go` | + `SupportsPKCE`, `IDTokenValidationRequest.Issuers`/`.ExpectedNonce` | 6, 7 |
| `utils/pkce.go` + `utils/pkce_test.go` | keep; delete the duplicates in `shared/utils/crypto.go:95-118` | 6 |

**Backend — elsewhere**

| File | Responsibility | Task |
|---|---|---|
| `pkg/sdk/iface/interfaces.go` | + `SystemRoleHolderFinder` | 3 |
| `internal/core/user/services/user_service.go` | implement it; `CreateUserFromOAuth` honours `input.UUID` | 2, 3 |
| `internal/core/user/repository/user_repository.go` | `FindOldestUserWithRole` | 3 |
| `pkg/sdk/CLAUDE.md` | record the seam | 3 |

---

## Task 1: The unique index, its migration, and the boot check (D32 item 1)

Ownership-first writes rely on a constraint that does not exist yet. `ensureCollections` is create-only and non-fatal, so the index needs a migration — and the migration must refuse to guess.

**Files:**
- Create: `backend/migrations/20260903_oauth_provider_identity_unique.js`, `.test.js`
- Create: `docs/migrations/0010_oauth_provider_identity_unique.md`
- Modify: `backend/internal/core/auth/models/collections.go`
- Modify: `backend/internal/core/auth/module.go` (`:742-747` index specs; `Start` verification; health check)
- Test: `backend/internal/core/auth/module_index_check_test.go` (new)

**Interfaces:**
- Consumes: `module.IndexSpec`, the module `HealthCheck` contract.
- Produces:
  - `models.OAuthProviderDoc.UnlinkedAt *time.Time`
  - `func (m *AuthModule) verifyOAuthIdentityIndex(ctx context.Context) error`
  - `m.oauthIndexMissing atomic.Bool` — read by the health check and by every OAuth entry point
  - `services.ErrOAuthStoreUnavailable`

- [ ] **Step 1: Write the failing migration test**

Create `backend/migrations/20260903_oauth_provider_identity_unique.test.js`, following the harness of `20260823_authz_bindings_unique.test.js` (read its header first — it establishes how a migration test is run and how it seeds a database):

```js
// Migration 0010 — unique (provider, providerId) on both OAuth provider
// collections.
//
// It NEVER decides ownership. The existing (userUuid, provider) unique
// index already makes two rows of one user for one provider impossible,
// so every duplicate on (provider, providerId) is one identity claimed
// by two DIFFERENT users — a conflict no rule can settle. Keeping the
// earliest-linked row would be an automatic answer to a question that
// has none.

test('a cross-user duplicate group blocks the migration and changes nothing', async (t) => {
  const db = await seed(t, {
    operator_oauth_providers: [
      { uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234', linkedAt: new Date('2026-01-01') },
      { uuid: 'b', userUuid: 'u-2', provider: 'google', providerId: '1234', linkedAt: new Date('2026-02-01') },
    ],
  })

  const res = await runMigration(db)

  assert.notEqual(res.exitCode, 0, 'a conflict must exit non-zero')
  assert.match(res.stdout, /google:1234/, 'the group must be printed')
  assert.match(res.stdout, /u-1/)
  assert.match(res.stdout, /u-2/)
  assert.equal(await db.collection('operator_oauth_providers').countDocuments(), 2, 'nothing may be deleted')
  assert.equal(await hasUniqueIndex(db, 'operator_oauth_providers'), false, 'no index while a conflict stands')
})

test('a RESOLVE entry deletes only the losing rows of that identity', async (t) => {
  const db = await seed(t, {
    operator_oauth_providers: [
      { uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' },
      { uuid: 'b', userUuid: 'u-2', provider: 'google', providerId: '1234' },
      { uuid: 'c', userUuid: 'u-3', provider: 'github', providerId: '9999' },
      { uuid: 'd', userUuid: 'u-4', provider: 'github', providerId: '9999' },
    ],
  })

  const res = await runMigration(db, { RESOLVE: { 'google:1234': 'u-1' } })

  assert.notEqual(res.exitCode, 0, 'the unresolved github group must still block')
  assert.equal(await db.collection('operator_oauth_providers').countDocuments({ uuid: 'b' }), 0, 'the named loser is deleted')
  assert.equal(await db.collection('operator_oauth_providers').countDocuments({ uuid: 'a' }), 1, 'the keeper survives')
  assert.equal(await db.collection('operator_oauth_providers').countDocuments({ provider: 'github' }), 2, 'an unresolved group is untouched')
  assert.match(res.stdout, /deleted .*b/i, 'every deletion must be printed for the change record')
})

test('a RESOLVE entry naming a userUuid that does not own the identity is refused', async (t) => {
  const db = await seed(t, {
    operator_oauth_providers: [
      { uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' },
      { uuid: 'b', userUuid: 'u-2', provider: 'google', providerId: '1234' },
    ],
  })
  const res = await runMigration(db, { RESOLVE: { 'google:1234': 'u-999' } })
  assert.notEqual(res.exitCode, 0)
  assert.equal(await db.collection('operator_oauth_providers').countDocuments(), 2, 'nothing deleted on a bad keeper')
})

test('no conflicts: the index is created and verified on BOTH collections', async (t) => {
  const db = await seed(t, {
    operator_oauth_providers: [{ uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' }],
    client_oauth_providers: [{ uuid: 'z', userUuid: 'c-1', provider: 'google', providerId: '5555' }],
  })
  const res = await runMigration(db)
  assert.equal(res.exitCode, 0)
  assert.equal(await hasUniqueIndex(db, 'operator_oauth_providers'), true)
  assert.equal(await hasUniqueIndex(db, 'client_oauth_providers'), true)
})

test('a re-run on a migrated database is a no-op', async (t) => {
  const db = await seed(t, { operator_oauth_providers: [{ uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' }] })
  assert.equal((await runMigration(db)).exitCode, 0)
  const second = await runMigration(db)
  assert.equal(second.exitCode, 0)
  assert.equal(await db.collection('operator_oauth_providers').countDocuments(), 1)
})

// A duplicate where BOTH rows belong to the same user cannot exist —
// the (userUuid, provider) unique index forbids it — but if one is found
// the migration must still refuse rather than invent a rule.
test('an empty collection migrates cleanly', async (t) => {
  const db = await seed(t, {})
  assert.equal((await runMigration(db)).exitCode, 0)
})
```

- [ ] **Step 2: Run it to verify it fails**

Run the migration test the way `20260823_authz_bindings_unique.test.js` is run.
Expected: FAIL — the migration file does not exist.

- [ ] **Step 3: Write the migration**

Create `backend/migrations/20260903_oauth_provider_identity_unique.js`, following `20260823_authz_bindings_unique.js` in shape:

```js
// 20260903_oauth_provider_identity_unique.js
//
// Adds a unique index on (provider, providerId) to
// operator_oauth_providers and client_oauth_providers.
//
// It NEVER decides ownership. The existing (userUuid, provider) unique
// index already makes two rows of one user for one provider impossible,
// so every duplicate on (provider, providerId) is one identity claimed
// by two DIFFERENT users. Which of them owns it is a question about
// people, not about data: keeping the earliest-linked row would be an
// automatic answer to a question that has none, and the loser silently
// loses their ability to sign in.
//
// So: report every group, exit non-zero, change nothing. Reconciliation
// is explicit —
//
//   mongosh "$URI" --eval 'var RESOLVE={"google:1234":"<userUuid>"}' \
//     migrations/20260903_oauth_provider_identity_unique.js
//
// naming, per identity, the userUuid that KEEPS it. Only rows of listed
// identities are deleted, only the losers, and every deletion is printed
// so the output can be filed with the change.
//
// Run this BEFORE deploying the release that ships ownership-first OAuth
// writes. That release verifies the index at boot and degrades OAuth to
// oauth_store_unavailable when it is missing, rather than running the
// ownership flow without the constraint it relies on.

const COLLECTIONS = ['operator_oauth_providers', 'client_oauth_providers']
const INDEX_NAME = 'provider_1_providerId_1'
const resolve = typeof RESOLVE === 'undefined' ? {} : RESOLVE

let blocked = 0

for (const name of COLLECTIONS) {
  const coll = db.getCollection(name)
  const groups = coll
    .aggregate([
      { $group: { _id: { provider: '$provider', providerId: '$providerId' }, rows: { $push: '$$ROOT' }, n: { $sum: 1 } } },
      { $match: { n: { $gt: 1 } } },
    ])
    .toArray()

  for (const g of groups) {
    const identity = `${g._id.provider}:${g._id.providerId}`
    const keeper = resolve[identity]

    if (!keeper) {
      blocked++
      print(`CONFLICT ${name} ${identity} — claimed by ${g.n} users:`)
      for (const r of g.rows) {
        print(`  userUuid=${r.userUuid} linkedAt=${r.linkedAt} lastUsed=${r.lastUsed} tokenStatus=${r.tokenStatus}`)
      }
      continue
    }

    const owns = g.rows.some((r) => r.userUuid === keeper)
    if (!owns) {
      blocked++
      print(`CONFLICT ${name} ${identity} — RESOLVE names ${keeper}, which does not hold this identity. Nothing deleted.`)
      continue
    }

    for (const r of g.rows) {
      if (r.userUuid === keeper) continue
      coll.deleteOne({ _id: r._id })
      print(`deleted ${name} uuid=${r.uuid} userUuid=${r.userUuid} identity=${identity}`)
    }
  }
}

if (blocked > 0) {
  print('')
  print(`${blocked} identity conflict(s) unresolved. Re-run with a RESOLVE map naming the keeper per identity.`)
  print('No index was created. See docs/migrations/0010_oauth_provider_identity_unique.md')
  quit(1)
}

for (const name of COLLECTIONS) {
  const coll = db.getCollection(name)
  coll.createIndex({ provider: 1, providerId: 1 }, { unique: true, name: INDEX_NAME })
  const present = coll.getIndexes().some((i) => i.name === INDEX_NAME && i.unique === true)
  if (!present) {
    print(`FAILED to create ${INDEX_NAME} on ${name}`)
    quit(1)
  }
  print(`ok ${name} ${INDEX_NAME}`)
}
quit(0)
```

- [ ] **Step 4: Write the runbook**

Create `docs/migrations/0010_oauth_provider_identity_unique.md` covering: why the migration refuses, how to read a conflict report, how to decide a keeper (the row whose `lastUsed` is recent and whose user recognises the identity — a human decision), the `RESOLVE` invocation, that the output must be filed with the change, and that the deploy is blocked until the run exits zero on **every** environment.

- [ ] **Step 5: Add the model field and the index specs**

`models/collections.go`, on `OAuthProviderDoc`:

```go
	// UnlinkedAt tombstones an identity the user (or an admin) has
	// unlinked. READ from this release; WRITTEN from the next one.
	//
	// A hard delete would be undone by the very next callback: the
	// unlinked branch auto-links by verified email, so the operator
	// would have removed nothing. The tombstone is what makes an unlink
	// stick.
	//
	// Splitting read from write is what keeps the rollback safe — a
	// binary that does not know the field would re-enable every unlinked
	// identity. Once a tombstone exists anywhere, this release is the
	// hard rollback floor.
	UnlinkedAt *time.Time `bson:"unlinkedAt,omitempty" json:"-"`
```

`module.go:742-747`, both collections:

```go
		{Name: models.OperatorOAuthProvidersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"userUuid": 1, "provider": 1}, Unique: true},
			// One identity, one owner. ensureCollections is create-only
			// and non-fatal, so migration 0010 is what actually
			// guarantees this on an existing install — Start verifies it.
			{Keys: map[string]int{"provider": 1, "providerId": 1}, Unique: true},
		}},
```

- [ ] **Step 6: Write the failing boot-check test**

```go
// A deploy that skipped migration 0010 must NOT run the ownership-first
// flow without the constraint it relies on: a duplicate key is how a
// conflict is detected, and without the index there is no duplicate key.
func TestStart_MissingIdentityIndexDegradesOAuth(t *testing.T) {
	m, db := newAuthModuleForTest(t)
	dropIndex(t, db, "operator_oauth_providers", "provider_1_providerId_1")

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start must not fail — auth is a core module: %v", err)
	}
	if h := m.HealthCheck(context.Background()); h.Status == "healthy" {
		t.Fatal("the health check must report degraded")
	}
	if _, err := m.operatorAuthService.HandleOAuthCallbackWithLinking(ctx, validCallbackArgs()); !errors.Is(err, services.ErrOAuthStoreUnavailable) {
		t.Fatal("every OAuth path must answer oauth_store_unavailable while the index is missing")
	}
}

func TestStart_PresentIndexLeavesOAuthEnabled(t *testing.T) {
	m, _ := newAuthModuleForTest(t) // ensureCollections created it
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if h := m.HealthCheck(context.Background()); h.Status != "healthy" {
		t.Fatalf("health = %q, want healthy", h.Status)
	}
}
```

- [ ] **Step 7: Implement the boot check**

In `maintenance.go`'s `Start`, before the sweep block:

```go
	// Migration 0010 is what guarantees the (provider, providerId) unique
	// index on an existing install; ensureCollections is create-only and
	// non-fatal, so it cannot be relied on. The ownership-first link flow
	// DETECTS a conflict by the duplicate key this index produces —
	// without it, two users would silently share an identity again.
	//
	// Never a boot failure: auth is a core module and StartAll propagates
	// its error to log.Fatalf. Degrade OAuth instead, loudly.
	if err := m.verifyOAuthIdentityIndex(ctx); err != nil {
		m.oauthIndexMissing.Store(true)
		m.logger.Error("auth: the OAuth identity unique index is missing — OAuth is degraded until migration 0010 has run",
			slog.String("error", err.Error()))
	}
```

`verifyOAuthIdentityIndex` lists indexes on both provider collections and returns an error naming the collection when the unique `(provider, providerId)` index is absent. `HealthCheck` reports degraded while `oauthIndexMissing` is set, and every OAuth entry point checks it first and returns `ErrOAuthStoreUnavailable`.

- [ ] **Step 8: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
# plus the migration test
cd /home/tore/orkestra && git add backend/migrations backend/internal/core/auth docs/migrations
git commit -m "$(cat <<'EOF'
feat(auth): add migration 0010 and the OAuth identity unique index

One identity, one owner: a unique index on (provider, providerId) for
both provider collections. ensureCollections is create-only and
non-fatal, so migration 0010 is what actually guarantees it on an
existing install.

The migration NEVER decides ownership. The existing (userUuid, provider)
index already forbids two rows of one user for one provider, so every
duplicate here is one identity claimed by two DIFFERENT users — a
question about people, not data. It reports every group and exits
non-zero, changing nothing; reconciliation is an explicit RESOLVE map
naming the keeper per identity, and every deletion is printed for the
change record.

The module verifies the index at Start and degrades OAuth to
oauth_store_unavailable when it is missing, so a deploy that skipped the
migration cannot run the ownership-first flow without the constraint
that detects a conflict. Never a boot failure — auth is a core module.

OAuthProviderDoc gains UnlinkedAt, read from this release and written
from the next: a binary that does not know the field would re-enable
every unlinked identity on a rollback.

Spec §4.8 D32 item 1.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 2: The OAuth path mirrors the password path (D30)

`ClaimFirstAdmin` is called on the OAuth signup branch with no audience guard, and the claimer is wired into the client bundle — so the first client-tier OAuth signup on a fresh install becomes the platform `super_admin`. The password path has had the guard all along.

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (`:2178-2185`)
- Modify: `backend/internal/core/user/services/user_service.go` (`CreateUserFromOAuth` `:694`)
- Test: `backend/internal/core/auth/services/gates_test.go`

**Interfaces:**
- Consumes: `s.audience`, `PolicyAudienceClient`, `s.firstAdminClaimer` (`systeminit.Repo`).
- Produces: `CreateUserFromOAuth` honours `input.UUID`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/core/auth/services/gates_test.go`, as the twin of the existing password-path test at `:301`:

```go
// H-6: the OAuth signup branch called ClaimFirstAdmin with no audience
// guard, and the claimer is wired into the CLIENT bundle
// (tier_bundle.go:138, :166). So on a fresh install the first
// client-tier OAuth signup — an external customer — became the platform
// super_admin.
func TestOAuthCallback_ClientFirstUser_NeverClaimsSuperAdmin(t *testing.T) {
	svc, claimer := newOAuthGateService(t, PolicyAudienceClient)

	user, err := completeOAuthSignup(t, svc, "someone@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if claimer.claimCalls() != 0 {
		t.Fatal("the client tier must never attempt the first-admin claim")
	}
	if user.Role == "super_admin" {
		t.Fatalf("role = %q — a client-tier signup must never be super_admin", user.Role)
	}
}

func TestOAuthCallback_OperatorFirstUser_StillClaims(t *testing.T) {
	svc, claimer := newOAuthGateService(t, PolicyAudienceOperator)

	user, err := completeOAuthSignup(t, svc, "founder@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if claimer.claimCalls() != 1 {
		t.Fatal("the operator tier must still claim on a fresh install")
	}
	if user.Role != "super_admin" {
		t.Fatalf("role = %q, want super_admin", user.Role)
	}
}

// A claim ERROR is fatal, as it is on the password path
// (password_auth_service.go:310). Swallowing it is how a lost race
// becomes a silent guest — and how a genuinely broken sentinel store
// silently stops minting super_admins on an install that looks fresh.
func TestOAuthCallback_ClaimErrorIsFatal(t *testing.T) {
	svc, claimer := newOAuthGateService(t, PolicyAudienceOperator)
	claimer.failWith(errors.New("mongo down"))

	if _, err := completeOAuthSignup(t, svc, "founder@example.com"); err == nil {
		t.Fatal("a claim error must fail the signup, not fall through to guest")
	}
}

// A LOST race is not an error: the sentinel is already taken, so the
// signup proceeds with the tier default.
func TestOAuthCallback_LostClaimRaceFallsBackToTheTierDefault(t *testing.T) {
	svc, claimer := newOAuthGateService(t, PolicyAudienceOperator)
	claimer.alreadyClaimed()

	user, err := completeOAuthSignup(t, svc, "second@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if user.Role != "guest" {
		t.Fatalf("role = %q, want the operator tier default 'guest'", user.Role)
	}
}

// The sentinel and the created user must carry the SAME uuid, or a
// rollback's Release (which deletes only a matching uuid) can never
// match and the sentinel is stranded.
func TestOAuthCallback_SentinelAndUserShareTheUUID(t *testing.T) {
	svc, claimer := newOAuthGateService(t, PolicyAudienceOperator)
	user, err := completeOAuthSignup(t, svc, "founder@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if claimer.claimedUUID() != user.UUID {
		t.Fatalf("sentinel uuid %q != user uuid %q", claimer.claimedUUID(), user.UUID)
	}
}

// CreateUserFromOAuth ignores input.UUID today, which is what breaks the
// pairing above.
func TestCreateUserFromOAuth_HonoursTheSuppliedUUID(t *testing.T) {
	svc := newUserServiceForTest(t)
	want := "01890000-0000-7000-8000-000000000001"
	u, err := svc.CreateUserFromOAuth(context.Background(), &iface.CreateUserInput{
		UUID: want, Email: "a@example.com", Role: "guest", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("CreateUserFromOAuth: %v", err)
	}
	if u.UUID != want {
		t.Fatalf("UUID = %q, want the supplied %q", u.UUID, want)
	}
}

func TestCreateUserFromOAuth_GeneratesAUUIDWhenNoneSupplied(t *testing.T) {
	svc := newUserServiceForTest(t)
	u, err := svc.CreateUserFromOAuth(context.Background(), &iface.CreateUserInput{
		Email: "b@example.com", Role: "guest", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("CreateUserFromOAuth: %v", err)
	}
	if u.UUID == "" {
		t.Fatal("an empty input UUID must still produce one")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ ./internal/core/user/services/ -run 'OAuthCallback_Client|OAuthCallback_Claim|OAuthCallback_Lost|SentinelAndUser|CreateUserFromOAuth_' -count=1`
Expected: FAIL — the client tier claims, the error is swallowed, the UUID is ignored.

- [ ] **Step 3: Guard the claim**

Replace `auth_service.go:2178-2185`:

```go
			// The claim is OPERATOR-tier only. The claimer is wired into
			// both bundles (tier_bundle.go:138, :166), so without this
			// guard the first client-tier OAuth signup on a fresh
			// install — an external customer — became the platform
			// super_admin. The password path has had this guard all
			// along (password_auth_service.go:234, :307); this is the
			// mirror.
			claimed := false
			if s.audience != PolicyAudienceClient && s.firstAdminClaimer != nil {
				c, err := s.firstAdminClaimer.ClaimFirstAdmin(ctx, newUUID)
				if err != nil {
					// Fatal, as on the password path: a swallowed claim
					// error is how a lost race becomes a silent guest,
					// and how a broken sentinel store silently stops
					// minting super_admins on an install that looks
					// fresh.
					return nil, fmt.Errorf("claim first admin: %w", err)
				}
				if c {
					claimed = true
					role = "super_admin"
				}
			}
```

- [ ] **Step 4: Honour the supplied UUID**

In `user_service.go`'s `CreateUserFromOAuth` (`:694`), use `input.UUID` when non-empty and generate one otherwise. Both the sentinel and the created user must carry it, or `Release`'s guarded delete (`firstadmin.go:49-62`) can never match on a rollback.

- [ ] **Step 5: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... ./internal/core/user/... -count=1
git add backend/internal/core/auth backend/internal/core/user
git commit -m "$(cat <<'EOF'
fix(auth): guard the OAuth first-admin claim by tier (H-6)

The OAuth signup branch called ClaimFirstAdmin with no audience guard,
and the claimer is wired into the CLIENT bundle — so on a fresh install
the first client-tier OAuth signup, an external customer, became the
platform super_admin. The password path has had the guard all along;
this is the mirror.

A claim error is now fatal rather than swallowed, as it already is on
the password path: swallowing it is how a lost race becomes a silent
guest, and how a broken sentinel store silently stops minting
super_admins on an install that only looks fresh. A LOST race stays
non-fatal and falls back to the tier default.

CreateUserFromOAuth honours input.UUID, which it ignored: the sentinel
and the created user must carry the same uuid or the rollback Release —
a guarded delete that matches on uuid — can never fire.

Spec §4.7 D30.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 3: The sentinel is seeded on installs that already have an administrator (D31)

The guard in Task 2 protects a *fresh* install. An install upgraded from before the sentinel existed has an administrator and an **unclaimed** sentinel, so the next operator-tier OAuth signup still wins it.

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go`
- Modify: `backend/internal/core/user/repository/user_repository.go`, `services/user_service.go`
- Modify: `backend/internal/core/auth/maintenance.go` (`Start`)
- Modify: `backend/pkg/sdk/CLAUDE.md`, `backend/internal/core/auth/CLAUDE.md` (`:184`, `:1040`)
- Test: `backend/internal/core/auth/sentinel_backfill_test.go` (new), `backend/internal/core/user/services/user_service_test.go`

**Interfaces:**
- Consumes: `iface.UserProvider.GetUserCount`, `module.GetTyped`, `module.ServiceOperatorUserProvider`, `systeminit.Repo.ClaimFirstAdmin`.
- Produces: `iface.SystemRoleHolderFinder interface { FindOldestUserWithRole(ctx context.Context, role string) (userUUID string, found bool, err error) }`

- [ ] **Step 1: Write the failing tests**

```go
// H-6, second half: the tier guard protects a FRESH install. An install
// upgraded from before the sentinel existed has an administrator and an
// UNCLAIMED sentinel, so the next operator-tier OAuth signup still wins
// it. Start seeds it.
func TestSentinelBackfill_ClaimsForTheOldestSuperAdmin(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.seedSuperAdmins("u-old" /* createdAt earliest */, "u-new")

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.claimedUUID() != "u-old" {
		t.Fatalf("claimed %q, want the oldest holder u-old", deps.claimer.claimedUUID())
	}
}

// Every replica must pick the SAME user, or two replicas race with
// different uuids and whichever loses leaves a sentinel pointing at
// somebody else.
func TestSentinelBackfill_IsIdempotentAcrossRuns(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.seedSuperAdmins("u-old")

	_ = m.Start(context.Background())
	_ = m.Stop(context.Background())
	_ = m.Start(context.Background())

	if deps.claimer.insertCount() != 1 {
		t.Fatalf("%d inserts across two runs, want 1 — $setOnInsert makes the second a no-op", deps.claimer.insertCount())
	}
}

// A fork's user provider may predate the seam. The backfill still runs,
// on the interface that exists, with a placeholder that is safe by the
// sentinel's own contract: nothing reads its userUUID back, and Release
// deletes only a MATCHING uuid, so no signup rollback can ever remove it.
func TestSentinelBackfill_FallsBackToGetUserCount(t *testing.T) {
	m, deps := newBackfillModule(t, withoutRoleFinder())
	deps.users.setSuperAdminCount(3)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.claimedUUID() != "legacy-backfill" {
		t.Fatalf("claimed %q, want the legacy-backfill placeholder", deps.claimer.claimedUUID())
	}
}

// Edge case 18: a fresh install has zero super_admins, so nothing is
// claimed and the bootstrap paths are untouched.
func TestSentinelBackfill_FreshInstallClaimsNothing(t *testing.T) {
	m, deps := newBackfillModule(t)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.claimCalls() != 0 {
		t.Fatal("a fresh install must not claim")
	}
}

// A lookup or claim failure logs ERROR and Start still returns nil —
// auth is a core module, and the tier guard of D30 already closes the
// client side. The next boot retries.
func TestSentinelBackfill_ErrorsDoNotFailStart(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.failFindOldest(errors.New("mongo down"))

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start must return nil: %v", err)
	}
	if deps.claimer.claimCalls() != 0 {
		t.Fatal("nothing may be claimed on a failed lookup")
	}
}

// Edge case 19: a backfill racing a live first signup converges —
// $setOnInsert means whoever upserts first wins and the loser is a
// no-op. A signup that loses gets 'guest', which is CORRECT: a
// super_admin already existed.
func TestSentinelBackfill_RacesASignupSafely(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.seedSuperAdmins("u-old")
	deps.claimer.simulateConcurrentSignupClaim("u-signup")

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.insertCount() != 1 {
		t.Fatal("exactly one insert wins")
	}
}
```

and for the finder:

```go
// The finder must be DETERMINISTIC — every replica picks the same user.
func TestFindOldestUserWithRole_PicksTheEarliestCreatedAt(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(t, user{uuid: "b", role: "super_admin", createdAt: day(2)})
	repo.seed(t, user{uuid: "a", role: "super_admin", createdAt: day(1)})

	got, found, err := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got != "a" {
		t.Fatalf("got %q, want the earliest-created a", got)
	}
}

func TestFindOldestUserWithRole_TieIsBrokenByUUID(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(t, user{uuid: "b", role: "super_admin", createdAt: day(1)})
	repo.seed(t, user{uuid: "a", role: "super_admin", createdAt: day(1)})

	got, _, _ := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if got != "a" {
		t.Fatalf("got %q — an identical createdAt must be broken by uuid so replicas agree", got)
	}
}

func TestFindOldestUserWithRole_IgnoresDeletedUsers(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(t, user{uuid: "deleted", role: "super_admin", createdAt: day(1), deletedAt: day(2)})
	repo.seed(t, user{uuid: "live", role: "super_admin", createdAt: day(3)})

	got, _, _ := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if got != "live" {
		t.Fatalf("got %q, want the non-deleted live", got)
	}
}

// A DEACTIVATED super_admin still proves the install was bootstrapped,
// so isActive is deliberately NOT filtered.
func TestFindOldestUserWithRole_IncludesInactiveUsers(t *testing.T) {
	svc, repo := newUserServiceForFinderTest(t)
	repo.seed(t, user{uuid: "inactive", role: "super_admin", createdAt: day(1), isActive: false})

	got, found, _ := svc.FindOldestUserWithRole(context.Background(), "super_admin")
	if !found || got != "inactive" {
		t.Fatal("a deactivated super_admin still proves the install was bootstrapped")
	}
}

func TestFindOldestUserWithRole_NoHolderReportsNotFound(t *testing.T) {
	svc, _ := newUserServiceForFinderTest(t)
	if _, found, err := svc.FindOldestUserWithRole(context.Background(), "super_admin"); found || err != nil {
		t.Fatalf("found=%v err=%v, want false/nil", found, err)
	}
}

func TestUserService_ImplementsSystemRoleHolderFinder(t *testing.T) {
	var _ iface.SystemRoleHolderFinder = (*userService)(nil)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/ ./internal/core/user/services/ -run 'SentinelBackfill|FindOldestUserWithRole|ImplementsSystemRoleHolder' -count=1`
Expected: FAIL — the seam and the backfill do not exist.

- [ ] **Step 3: Add the seam**

`backend/pkg/sdk/iface/interfaces.go`:

```go
// ---------------------------------------------------------------------------
// SystemRoleHolderFinder — consumed by: the auth module's first-admin
// sentinel backfill. Narrow on purpose (the UserLifecycleStateProvider
// precedent): one deterministic answer, no paging, no DTO.
//
// UserProvider exposes GetUserCount but no listing, and widening it would
// break every external implementor. A provider that lacks this seam still
// gets a backfill — the caller falls back to GetUserCount and a placeholder
// uuid, which is safe by the sentinel's own contract.
// ---------------------------------------------------------------------------

type SystemRoleHolderFinder interface {
	// FindOldestUserWithRole returns the UUID of the oldest non-deleted
	// user holding role, ordered by createdAt then uuid so every replica
	// picks the same one. Deactivated users are INCLUDED: a deactivated
	// super_admin still proves the install was bootstrapped.
	FindOldestUserWithRole(ctx context.Context, role string) (userUUID string, found bool, err error)
}
```

Implement it on `*userService` with one repository query: the existing `buildFilter` (role, `deletedAt` excluded, `isActive` deliberately unfiltered), sorted `createdAt asc, uuid asc`, limit 1.

- [ ] **Step 4: Add the backfill to `Start`**

In `maintenance.go`, on the operator bundle only:

```go
	// First-admin sentinel backfill.
	//
	// D30's tier guard protects a FRESH install. An install upgraded from
	// before the sentinel existed has an administrator and an UNCLAIMED
	// sentinel, so the next operator-tier OAuth signup would still win
	// it. Claiming it on behalf of the existing super_admin closes that.
	//
	// No migration script: one idempotent query and one $setOnInsert
	// upsert per boot, and Start runs before ListenAndServe, so no
	// request can reach the callback first. Concurrent replicas converge.
	//
	// Errors log ERROR and Start still returns nil — auth is a core
	// module, the client tier is already closed by D30, and the next boot
	// retries.
	m.backfillFirstAdminSentinel(ctx)
```

```go
func (m *AuthModule) backfillFirstAdminSentinel(ctx context.Context) {
	if m.firstAdminClaimer == nil {
		return
	}

	uuid, found := "", false
	if finder, ok := module.GetTyped[iface.SystemRoleHolderFinder](m.services, module.ServiceOperatorUserProvider); ok && finder != nil {
		u, f, err := finder.FindOldestUserWithRole(ctx, "super_admin")
		if err != nil {
			m.logger.Error("auth: first-admin sentinel backfill lookup failed",
				slog.String("error", err.Error()))
			return
		}
		uuid, found = u, f
	} else if m.operatorUsers != nil {
		// A fork's provider that predates the seam. The placeholder is
		// safe by the sentinel's own contract: nothing reads its
		// userUUID back (setup/service.go:226-245), and Release deletes
		// only a MATCHING uuid (firstadmin.go:49-62), so no signup
		// rollback can ever remove it.
		n, err := m.operatorUsers.GetUserCount(ctx, &iface.UserFilters{Role: "super_admin"})
		if err != nil {
			m.logger.Error("auth: first-admin sentinel backfill count failed",
				slog.String("error", err.Error()))
			return
		}
		if n > 0 {
			uuid, found = "legacy-backfill", true
		}
	}

	if !found {
		return // fresh install: nothing to backfill
	}
	claimed, err := m.firstAdminClaimer.ClaimFirstAdmin(ctx, uuid)
	if err != nil {
		m.logger.Error("auth: first-admin sentinel backfill claim failed",
			slog.String("user_uuid", uuid), slog.String("error", err.Error()))
		return
	}
	if claimed {
		m.logger.Info("first-admin sentinel backfilled",
			slog.String("user_uuid", uuid), slog.String("source", "backfill"))
	}
}
```

- [ ] **Step 5: Run, document, commit**

Correct `backend/internal/core/auth/CLAUDE.md:184` (the first-user heuristic) and `:1040` (the OAuth sentinel), and record `SystemRoleHolderFinder` in `backend/pkg/sdk/CLAUDE.md`.

```bash
go vet ./... && go test ./internal/core/auth/... ./internal/core/user/... ./pkg/sdk/... -count=1
git add backend
git commit -m "$(cat <<'EOF'
fix(auth): backfill the first-admin sentinel on upgraded installs

The tier guard protects a fresh install. An install upgraded from before
the sentinel existed has an administrator and an UNCLAIMED sentinel, so
the next operator-tier OAuth signup would still win the global
super_admin seat.

Start now claims it on behalf of the oldest existing super_admin,
resolved through a new additive seam, SystemRoleHolderFinder — one
deterministic query (createdAt then uuid, so every replica agrees;
deleted users excluded, deactivated ones included, because a deactivated
super_admin still proves the install was bootstrapped). A provider that
predates the seam falls back to GetUserCount and a placeholder uuid,
which is safe by the sentinel's own contract: nothing reads it back and
Release deletes only a matching uuid.

No migration script — one idempotent query and one $setOnInsert per
boot, before any request can reach the callback. Errors log and Start
still returns nil: auth is a core module and the next boot retries.

Spec §4.7 D31.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 4: The provider collection is authoritative — readers (D32 items 3, 6, 7)

Login resolves identity from `*_oauth_providers`, but listing and unlink read `user.oauthLinks`. So a login-created link is invisible to auth-methods and cannot be unlinked, and an unlinked identity keeps signing in. This task moves every reader to the provider collection and makes them all tombstone-aware.

**Files:**
- Modify: `backend/internal/core/auth/repository/oauth_provider_repository.go` (`GetByProviderAndID` `:113-130`, `GetByUserUUID` `:132`)
- Modify: `backend/internal/core/auth/services/auth_service.go` (`GetOAuthLinks` `:569-595`, `wouldLockOutOAuthUnlink` `:686-722`, `GetUserAuthMethods` `:918-1008`, the callback's `existingProvider` branch)
- Modify: `backend/internal/core/auth/handlers/oauth_callback_redirect.go` (allowlist)
- Test: `backend/internal/core/auth/services/auth_service_get_methods_test.go`, `oauth_inactive_user_test.go`, a new `oauth_tombstone_test.go`

**Interfaces:**
- Consumes: `models.OAuthProviderDoc.UnlinkedAt` (Task 1).
- Produces:
  - `services.ErrOAuthIdentityUnlinked`
  - repository: `GetByProviderAndID` and `GetByUserUUID` exclude tombstoned rows by default; a new `GetByProviderAndIDIncludingUnlinked` returns them (PR D2's revive path needs it, and the callback needs to *distinguish* unlinked from absent)
- Later tasks rely on: `ErrOAuthIdentityUnlinked` → callback code `oauth_identity_unlinked` (Task 5's error mapping).

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/oauth_tombstone_test.go`:

```go
package services

// H-7: unlink $pulls only user.oauthLinks, so the provider document —
// which is what LOGIN keys on — survives and the identity keeps signing
// in. This release makes every reader tombstone-aware; the next one
// writes the tombstone.

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCallback_TombstonedIdentityIsRefused(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{
		userUUID: "u-1", provider: "google", providerID: "1234",
		unlinkedAt: ptr(time.Now().Add(-time.Hour)),
	})

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthIdentityUnlinked) {
		t.Fatalf("err = %v, want ErrOAuthIdentityUnlinked", err)
	}
}

// The account itself is unaffected (edge case 20) — the user can still
// sign in another way.
func TestCallback_TombstoneDoesNotDisableTheAccount(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if _, err := completeOAuthCallback(t, svc, "github", "9999", "u1@example.com"); err != nil {
		t.Fatalf("another linked identity must still work: %v", err)
	}
}

// A tombstoned identity must NOT fall through to the auto-link branch —
// which matches by verified email and would silently re-create the link
// the operator just removed.
func TestCallback_TombstoneDoesNotFallThroughToAutoLink(t *testing.T) {
	svc, repo := newOAuthCallbackService(t, withAutoLink(true))
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthIdentityUnlinked) {
		t.Fatalf("err = %v — a tombstone must not be re-linked by the email branch", err)
	}
}

func TestGetUserAuthMethods_ReadsTheProviderCollection(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	// A link created by LOGIN: the provider doc exists, the embedded
	// slice does not. It was invisible to auth-methods.
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", email: "u1@example.com"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")

	methods, err := svc.GetUserAuthMethods(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if !hasProvider(methods, "google") {
		t.Fatal("a login-created link must be visible in auth-methods")
	}
}

func TestGetUserAuthMethods_ExcludesTombstonedIdentities(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})

	methods, err := svc.GetUserAuthMethods(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if hasProvider(methods, "google") {
		t.Fatal("an unlinked identity must not be listed")
	}
}

func TestGetOAuthLinks_ReadsTheProviderCollection(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")

	links, err := svc.GetOAuthLinks(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetOAuthLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1 — the embedded slice is a derived read-model, not the source", len(links))
	}
}

// The lockout calculation must count what actually EXISTS, or a user
// with a login-created link is told they would be locked out when they
// would not.
func TestWouldLockOut_CountsProviderDocsNotEmbeddedLinks(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1") // and no password
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("unlinking one of two real links must be allowed: %v", err)
	}
}

func TestWouldLockOut_TombstonedLinkDoesNotCountAsAWayIn(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999", unlinkedAt: ptr(time.Now())})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
		t.Fatalf("err = %v — an unlinked identity is not a remaining way in", err)
	}
}

// Lazy repair: a login through an existing provider doc re-adds the
// embedded link when it is missing. Best-effort — the embedded slice no
// longer decides anything, so a failure is a WARN.
func TestCallback_RepairsTheEmbeddedReadModel(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")

	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !repo.userHasEmbeddedLink("u-1", "google", "1234") {
		t.Fatal("the read-model must be repaired lazily on login")
	}
}

func TestCallback_RepairFailureDoesNotFailTheLogin(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.failAddOAuthLink()

	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err != nil {
		t.Fatalf("the read-model repair is best-effort: %v", err)
	}
}
```

Also add `unlinkedAt` to the `oauth_inactive_user_test.go` fixture and keep its "logs in regardless of the bit" carve-out test — it documents live behaviour.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'Tombstone|GetUserAuthMethods_Reads|GetOAuthLinks_Reads|WouldLockOut_|Callback_Repairs' -count=1`
Expected: FAIL — readers use `user.OAuthLinks`, and nothing knows about `unlinkedAt`.

- [ ] **Step 3: Make the repository tombstone-aware**

```go
// GetByProviderAndID resolves an ACTIVE identity. A tombstoned document
// (UnlinkedAt != nil) is deliberately invisible here: every caller of
// this method is asking "who owns this identity right now", and an
// unlinked identity is owned by nobody.
//
// Use GetByProviderAndIDIncludingUnlinked when the caller needs to tell
// "unlinked" apart from "never seen" — the callback does, so it can
// answer oauth_identity_unlinked instead of starting a signup.
func (r *oauthProviderRepository) GetByProviderAndID(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.OAuthProviderDoc, error) {
	return r.findIdentity(ctx, provider, providerID, false)
}

func (r *oauthProviderRepository) GetByProviderAndIDIncludingUnlinked(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.OAuthProviderDoc, error) {
	return r.findIdentity(ctx, provider, providerID, true)
}

func (r *oauthProviderRepository) findIdentity(ctx context.Context, provider models.OAuthProvider, providerID string, includeUnlinked bool) (*models.OAuthProviderDoc, error) {
	filter := bson.M{"provider": provider, "providerId": providerID}
	if !includeUnlinked {
		filter["unlinkedAt"] = bson.M{"$exists": false}
	}
	var result models.OAuthProviderDoc
	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	err := r.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find OAuth provider: %w", err)
	}
	return &result, nil
}
```

`GetByUserUUID` gains the same `unlinkedAt` exclusion (its callers list what the user *has*).

- [ ] **Step 4: Move the readers**

- `GetUserAuthMethods` (`:918-1008`), `GetOAuthLinks` (`:569-595`) and `wouldLockOutOAuthUnlink` (`:686-722`) read the non-tombstoned provider docs (`Email`, `LinkedAt`, `LastUsed`, `IsPrimary`). "Active" means "no tombstone".
- In the callback, look the identity up **including** tombstones; a hit with `UnlinkedAt != nil` returns `ErrOAuthIdentityUnlinked` **before** the email branch — otherwise the auto-link path (`:2206-2217`) would silently re-create the link the operator just removed.
- On a login through an existing provider doc, if the user's embedded links lack that `(provider, providerId)`, call `AddOAuthLinkToUser` best-effort (WARN on failure). No full backfill: the embedded slice no longer decides anything.

- [ ] **Step 5: Add the redirect codes**

`oauth_callback_redirect.go`'s allowlist gains `oauth_identity_unlinked`, plus the two codes Task 5 needs (`oauth_identity_conflict`, `oauth_store_unavailable`), with SPA copy for the first: *"This sign-in method was unlinked from your account. Sign in another way and re-link it from Security."*

- [ ] **Step 6: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): make the provider collection the source of OAuth identity (reads)

Login resolves identity from *_oauth_providers, but listing and unlink
read user.oauthLinks. So a link created by a login was invisible to
auth-methods and could not be unlinked, and an unlink that only $pulled
the embedded slice left the provider document — the thing login keys on
— standing, so the identity kept signing in (H-7).

Every reader moves to the provider collection and honours a tombstone:
auth-methods, the link listing, and the last-credential lockout
calculation. The callback distinguishes "unlinked" from "never seen" and
answers oauth_identity_unlinked rather than falling through to the
auto-link branch, which matches by verified email and would silently
re-create the link an operator just removed.

The embedded slice becomes a derived read-model, repaired lazily on
login and best-effort: it no longer decides anything.

This release READS tombstones and writes none — the writes and the
rollback floor are the next one.

Spec §4.8 D32 items 3, 6, 7.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 5: Ownership is written first (D32 item 5, item 8, D33)

`CreateOAuthProvider`'s error is ignored on the login path and a session is minted anyway — so a session can exist for an identity the store never recorded as the caller's. And a degraded lookup falls through into signup.

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (`:2111-2114`, `:2146`, `:2146-2205` signup, `:2206-2217` auto-link, `:2370-2373` write)
- Modify: `backend/internal/core/auth/repository/oauth_provider_repository.go` (surface the duplicate key)
- Modify: `backend/pkg/sdk/metrics/metrics.go` (compensation-failure counter)
- Test: `backend/internal/core/auth/services/oauth_ownership_test.go` (new)

**Interfaces:**
- Consumes: `mongo.IsDuplicateKeyError`, `GetByProviderAndIDIncludingUnlinked` (Task 4), `firstAdminClaimer.Release`.
- Produces:
  - `services.ErrOAuthIdentityClaimedByOther`, `services.ErrOAuthStoreUnavailable`
  - `repository.ErrOAuthIdentityDuplicate` (the wrapped duplicate-key sentinel)
  - metric `orkestra_auth_oauth_compensation_failures_total`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/oauth_ownership_test.go`:

```go
package services

// D32 item 5: CreateOAuthProvider's error was ignored on the login path
// and a session was minted anyway, so a session could exist for an
// identity the store never recorded as the caller's. Under the new
// unique index a duplicate-key hit means "someone else owns this" —
// silently ignoring it would be worse than the original bug.

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
// deleted and the flow continues as unlinked. Today that state is
// terminal — :2118-2125 treats any lookup error as fatal.
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
	svc, _ := newOwnershipService(t)
	if _, err := completeOAuthCallback(t, svc, "google", "1234", "new@example.com"); err != nil {
		t.Fatalf("a genuine not-found must sign up: %v", err)
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

// The redirect contract's allowlist must carry every new code, or the
// SPA gets a bare error.
func TestCallbackRedirect_AllowlistCarriesTheNewCodes(t *testing.T) {
	for _, code := range []string{"oauth_identity_unlinked", "oauth_identity_conflict", "oauth_store_unavailable"} {
		if !redirectCodeAllowed(code) {
			t.Errorf("%s is not in the callback redirect allowlist", code)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ ./internal/core/auth/handlers/ -run 'AutoLink_|Signup_|Callback_Orphan|Callback_Identity|Callback_Email|ExistingLink|CallbackRedirect_Allowlist' -count=1`
Expected: FAIL — the write is best-effort, lookups fall through, there is no orphan handling.

- [ ] **Step 3: Surface the duplicate key**

In the repository, wrap a duplicate-key error so the service can branch on it:

```go
// ErrOAuthIdentityDuplicate wraps the unique-index violation on
// (provider, providerId). Under that index a duplicate means one thing:
// this identity is already recorded, and the caller must re-read to find
// out whose it is.
var ErrOAuthIdentityDuplicate = errors.New("oauth identity already recorded")

func (r *oauthProviderRepository) CreateOAuthProvider(ctx context.Context, provider *models.OAuthProviderDoc) error {
	// … existing body …
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: %v", ErrOAuthIdentityDuplicate, err)
		}
		return err
	}
	return nil
}
```

- [ ] **Step 4: Write ownership first**

Add one helper and use it on every link path:

```go
// ErrOAuthIdentityClaimedByOther — the identity is recorded against a
// different user. Callback code oauth_identity_conflict; no session.
var ErrOAuthIdentityClaimedByOther = errors.New("oauth identity is claimed by another account")

// ErrOAuthStoreUnavailable — the identity store could not answer. A
// retryable outage; callback code oauth_store_unavailable. Never a
// fall-through into the auto-link or signup branch.
var ErrOAuthStoreUnavailable = errors.New("oauth identity store unavailable")

// claimIdentity writes ownership and is the FIRST durable step of every
// link path. Nothing is minted without it.
//
// CreateOAuthProvider used to be best-effort here: its error was ignored
// and a session was minted anyway, so a session could exist for an
// identity the store never recorded as the caller's. Under the unique
// index on (provider, providerId) a duplicate key means "already
// recorded", so the outcome of THIS write decides the whole flow:
//
//	success            → continue
//	duplicate, same owner   → continue (a benign double callback, two tabs)
//	duplicate, other owner  → ErrOAuthIdentityClaimedByOther, no session
//	any other error         → ErrOAuthStoreUnavailable, no session
func (s *authService) claimIdentity(ctx context.Context, doc *models.OAuthProviderDoc) (*models.OAuthProviderDoc, error) {
	err := s.oauthProviderRepo.CreateOAuthProvider(ctx, doc)
	if err == nil {
		return doc, nil
	}
	if !errors.Is(err, repository.ErrOAuthIdentityDuplicate) {
		s.logger.Error("auth: oauth identity write failed",
			slog.String("provider", string(doc.Provider)), slog.String("error", err.Error()))
		return nil, ErrOAuthStoreUnavailable
	}

	existing, rerr := s.oauthProviderRepo.GetByProviderAndIDIncludingUnlinked(ctx, doc.Provider, doc.ProviderID)
	if rerr != nil || existing == nil {
		return nil, ErrOAuthStoreUnavailable
	}
	if existing.UserUUID != doc.UserUUID {
		return nil, ErrOAuthIdentityClaimedByOther
	}
	return existing, nil
}
```

**Auto-link** (existing user, `:2206-2217`): insert the doc for that user through `claimIdentity`, then `AddOAuthLinkToUser` (read-model, best-effort WARN), then mint.

**Signup** (`:2146-2205`) becomes a reservation with compensation — two writes in two modules cannot share a transaction:

```go
			// 1. RESERVE the identity with the UUID the user will be
			//    created with. A conflict here means no user is ever
			//    created for a lost race.
			newUUID := models.GenerateUUIDv7()
			reserved, err := s.claimIdentity(ctx, &models.OAuthProviderDoc{
				UUID: uuid.NewString(), UserUUID: newUUID,
				Provider: provider, ProviderID: providerID, Email: email,
				LinkedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
			})
			if err != nil {
				return nil, err
			}

			// 2. Claim the first-admin sentinel when the tier allows it.
			// … D30's block …

			// 3. Create the user with that UUID.
			userModel, cerr := s.userService.CreateUserFromOAuth(ctx, createInput)
			if cerr != nil {
				// 4. Compensate BACKWARDS. A failure here is logged and
				//    counted; item 8 heals the residue on the next
				//    callback.
				if derr := s.oauthProviderRepo.DeleteProvider(ctx, reserved.UUID); derr != nil {
					metrics.Default().RecordOAuthCompensationFailure()
					s.logger.Error("auth: failed to release a reserved oauth identity",
						slog.String("provider_doc", reserved.UUID), slog.String("error", derr.Error()))
				}
				if claimed && s.firstAdminClaimer != nil {
					if rerr := s.firstAdminClaimer.Release(ctx, newUUID); rerr != nil {
						metrics.Default().RecordOAuthCompensationFailure()
						s.logger.Error("auth: failed to release the first-admin sentinel",
							slog.String("user_uuid", newUUID), slog.String("error", rerr.Error()))
					}
				}
				return nil, fmt.Errorf("failed to create user: %w", cerr)
			}
```

Only after step 3 does the branch add the embedded link and mint. Delete the old best-effort write at `:2370-2373`.

- [ ] **Step 5: Fail closed on degraded lookups, and heal orphans**

Replace `:2111-2114`:

```go
	// D33: a lookup that could not be answered is an OUTAGE, not
	// "no link". Falling through would start a signup — or an
	// auto-link — for an identity that may already belong to someone.
	// GetByProviderAndIDIncludingUnlinked already returns (nil, nil) on
	// no-documents, so the string comparison this replaces is gone.
	existingProvider, err := s.oauthProviderRepo.GetByProviderAndIDIncludingUnlinked(ctx, provider, providerID)
	if err != nil {
		return nil, ErrOAuthStoreUnavailable
	}
	if existingProvider != nil && existingProvider.UnlinkedAt != nil {
		return nil, ErrOAuthIdentityUnlinked
	}
```

and, in the `existingProvider != nil` branch, replace the fatal lookup (`:2118-2125`):

```go
		userModel, err := s.userService.GetUserByID(ctx, existingProvider.UserUUID)
		switch {
		case err == nil && userModel != nil:
			user = convertUserModelToAuthModel(userModel)
		case isNotFound(err) || userModel == nil:
			// An ORPHAN reservation: a crash between the identity
			// reservation and the user creation, or a compensation that
			// failed. This is not a linked identity — nobody owns it —
			// so delete it and continue as unlinked. The same person
			// completes their signup on this very attempt.
			//
			// Today this state is terminal: any lookup error was fatal.
			if derr := s.oauthProviderRepo.DeleteProvider(ctx, existingProvider.UUID); derr != nil {
				return nil, ErrOAuthStoreUnavailable
			}
			existingProvider = nil
		default:
			// An OUTAGE is not an absence. Deleting a real user's
			// identity because Mongo was briefly unavailable would be
			// catastrophic.
			return nil, ErrOAuthStoreUnavailable
		}
```

Apply the same fail-closed rule to `GetUserByEmail` at `:2146`: a non-not-found error is `ErrOAuthStoreUnavailable`.

- [ ] **Step 6: Add the metric**

`orkestra_auth_oauth_compensation_failures_total` (unlabelled) in `pkg/sdk/metrics`, following the PR A Task 2 shape.

- [ ] **Step 7: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... ./pkg/sdk/... -count=1
git add backend
git commit -m "$(cat <<'EOF'
fix(auth): write OAuth identity ownership first, and mint nothing without it

CreateOAuthProvider's error was ignored on the login path and a session
was minted anyway, so a session could exist for an identity the store
never recorded as the caller's. Under the new unique index a
duplicate-key hit means "someone else owns this" — silently ignoring
that would be worse than the original bug.

Ownership is now the FIRST durable step of every link path and its
outcome decides the flow: success continues; a duplicate is re-read and
either continues (same owner — a benign double callback) or is refused
with oauth_identity_conflict; any other error is
oauth_store_unavailable. No session either way.

Signup becomes a reservation with backwards compensation, since two
writes in two modules cannot share a transaction: reserve the identity
with the future user's UUID, claim the sentinel, create the user, and on
failure delete the reservation and release the sentinel. A compensation
failure is counted, and an orphan reservation heals itself on the next
callback — a state that was terminal before, because any user lookup
error was fatal. An outage is carefully distinguished from an absence:
deleting a real user's identity because Mongo blinked would be
catastrophic.

Degraded identity and email lookups fail closed instead of falling
through into the auto-link or signup branch.

Spec §4.8 D32 items 5 and 8, D33. Closes H-7's write half and L-30.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 6: PKCE (D34)

Every provider adapter already emits `code_challenge`/`S256` and `code_verifier` when non-empty. Nothing ever supplies them.

**Files:**
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (the two start endpoints, `:513-545` and `:621-642`)
- Modify: `backend/internal/core/auth/handlers/oauth_callback_flow.go` (`:34-37`, `:219`)
- Modify: `backend/internal/core/auth/services/oauth_provider_interface.go` (+ `SupportsPKCE`)
- Modify: the four provider services
- Delete: `backend/internal/core/auth/services/oauth_state_service.go`'s dead `ValidateOAuthCallback` (`:453-483`); the duplicate PKCE helpers in `backend/internal/shared/utils/crypto.go:95-118`
- Create: `backend/internal/core/auth/utils/pkce_test.go`
- Test: `backend/internal/core/auth/handlers/oauth_callback_flow_test.go`

**Interfaces:**
- Consumes: `utils.GenerateCodeVerifier()`, `utils.GenerateCodeChallenge(v)` (`internal/core/auth/utils/pkce.go:12,25`), `StoreOAuthStateRequest.CodeVerifier` (already persisted, `oauth_state_service.go:172-173`).
- Produces:
  - `OAuthProviderInterface.SupportsPKCE() bool`
  - `oauthExchange(… , codeVerifier string)`

- [ ] **Step 1: Write the failing tests**

```go
// M-18: the state row has carried a CodeVerifier field all along and
// every provider adapter already emits code_challenge/S256 and
// code_verifier when non-empty. Nothing ever supplied them, so the
// authorization code was interceptable with no proof of possession.

func TestOAuthStart_StoresAVerifierAndSendsAChallengeForPKCEProviders(t *testing.T) {
	for _, p := range []string{"google", "discord"} {
		t.Run(p, func(t *testing.T) {
			h, store, prov := newOAuthStartHandler(t)
			if _, err := callOAuthStart(t, h, p); err != nil {
				t.Fatalf("start: %v", err)
			}
			row := store.lastStored()
			if row.CodeVerifier == "" {
				t.Fatal("the verifier must be stored in the state row")
			}
			if prov.lastChallenge() == "" {
				t.Fatal("the challenge must be sent to the provider")
			}
			if prov.lastChallenge() != utils.GenerateCodeChallenge(row.CodeVerifier) {
				t.Fatal("the challenge must be the S256 of the stored verifier")
			}
		})
	}
}

// Edge case 24: a provider that ignores code_challenge but REJECTS
// code_verifier would break entirely. SupportsPKCE is what prevents it.
func TestOAuthStart_NonPKCEProvidersGetNeither(t *testing.T) {
	for _, p := range []string{"github", "apple"} {
		t.Run(p, func(t *testing.T) {
			h, store, prov := newOAuthStartHandler(t)
			if _, err := callOAuthStart(t, h, p); err != nil {
				t.Fatalf("start: %v", err)
			}
			if store.lastStored().CodeVerifier != "" {
				t.Fatal("no verifier for a provider that has not been proven to accept one")
			}
			if prov.lastChallenge() != "" {
				t.Fatal("no challenge either")
			}
		})
	}
}

func TestOAuthCallback_VerifierReachesTheExchange(t *testing.T) {
	h, store, prov := newOAuthCallbackHandler(t)
	store.seedState(t, stateRow{Provider: "google", CodeVerifier: "the-verifier"})

	if _, err := callOAuthCallback(t, h, "google"); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if prov.lastExchange().CodeVerifier != "the-verifier" {
		t.Fatalf("CodeVerifier = %q, want the stored one", prov.lastExchange().CodeVerifier)
	}
}

func TestOAuthCallback_EmptyVerifierIsSentAsEmpty(t *testing.T) {
	h, store, prov := newOAuthCallbackHandler(t)
	store.seedState(t, stateRow{Provider: "github"})

	if _, err := callOAuthCallback(t, h, "github"); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if prov.lastExchange().CodeVerifier != "" {
		t.Fatal("a non-PKCE provider must receive no verifier — today's behaviour exactly")
	}
}
```

`backend/internal/core/auth/utils/pkce_test.go`:

```go
package utils

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// RFC 7636: 43–128 characters from the unreserved set.
func TestGenerateCodeVerifier_LengthAndCharset(t *testing.T) {
	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	v, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Fatalf("length = %d, want 43..128", len(v))
	}
	for _, c := range v {
		if !strings.ContainsRune(allowed, c) {
			t.Fatalf("character %q is outside the unreserved set", c)
		}
	}
}

func TestGenerateCodeVerifier_IsRandom(t *testing.T) {
	a, _ := GenerateCodeVerifier()
	b, _ := GenerateCodeVerifier()
	if a == b {
		t.Fatal("two verifiers must differ")
	}
}

func TestGenerateCodeChallenge_IsBase64URLUnpaddedSHA256(t *testing.T) {
	const v = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(v))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := GenerateCodeChallenge(v); got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
	if strings.Contains(GenerateCodeChallenge(v), "=") {
		t.Fatal("the challenge must be unpadded")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/... -run 'OAuthStart_|OAuthCallback_Verifier|OAuthCallback_EmptyVerifier|CodeVerifier|CodeChallenge' -count=1`
Expected: FAIL — `SupportsPKCE` undefined, no verifier stored.

- [ ] **Step 3: Add `SupportsPKCE`**

On `OAuthProviderInterface`:

```go
	// SupportsPKCE reports whether this provider's token endpoint has
	// been PROVEN to accept a code_verifier. It is deliberately not
	// "does the provider document PKCE": a provider that ignores
	// code_challenge but rejects code_verifier breaks the exchange
	// entirely (edge case 24), so a provider stays false until a
	// staging round-trip confirms it. Promoting one is a one-line
	// change.
	SupportsPKCE() bool
```

`true` for Google and Discord; `false` for GitHub and Apple, each with a one-line comment saying it awaits the staging round-trip of §7.

- [ ] **Step 4: Mint, store and send**

At both start endpoints: if `provider.SupportsPKCE()`, `utils.GenerateCodeVerifier()`, derive the S256 challenge, put the verifier in `StoreOAuthStateRequest.CodeVerifier`, and pass the challenge to `GetAuthURL` (which currently receives `""`). Otherwise leave both empty — exactly today's behaviour.

At the callback: `oauthExchange` (`oauth_callback_flow.go:34-37`) gains a `codeVerifier string` argument filled from `res.info.CodeVerifier` at `:219`, and both closures set `CodeExchangeRequest.CodeVerifier`.

- [ ] **Step 5: Delete the dead code**

`ValidateOAuthCallback` (`oauth_state_service.go:453-483`) and the duplicate PKCE helpers in `internal/shared/utils/crypto.go:95-118`. Confirm with `grep -rn "ValidateOAuthCallback\|shared/utils.*CodeVerifier" --include="*.go" backend/` that nothing calls them.

- [ ] **Step 6: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth backend/internal/shared/utils
git commit -m "$(cat <<'EOF'
feat(auth): send PKCE on the providers proven to accept it

The state row has carried a CodeVerifier field all along and every
provider adapter already emits code_challenge/S256 and code_verifier
when non-empty — nothing ever supplied them, so the authorization code
travelled with no proof of possession.

The start endpoints now mint a verifier, store it in the state row and
send the S256 challenge; the callback threads the stored verifier into
the token exchange.

SupportsPKCE gates it per provider and starts true for Google and
Discord only. It deliberately does not mean "documents PKCE": a provider
that ignores code_challenge but rejects code_verifier would break the
exchange entirely, so GitHub and Apple stay false until the staging
round-trip proves them, after which promoting one is a one-line change.

Deletes the dead ValidateOAuthCallback and the duplicate PKCE helpers in
shared/utils, and gives utils/pkce.go its test.

Spec §4.9 D34. Closes M-18.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 7: Mobile ID tokens — a server-issued challenge bound to a client-held verifier (D35)

The mobile routes accept an ID token with no nonce, no `iss` check, and an `aud` check that is **skipped** when the config read fails. The client-supplied `access_token` is stored unverified.

**Files:**
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (`:1222-1345` Google, `:1346-…` Apple; new `begin` routes)
- Modify: `backend/internal/core/auth/services/oauth_provider_interface.go` (`IDTokenValidationRequest` `:47-54`)
- Modify: `backend/internal/core/auth/services/google_oauth_service.go` (`:190-263`), `apple_oauth_service.go` (`:256-325`)
- Modify: `mobile/README.md`, `mobile/CLAUDE.md`
- Test: `backend/internal/core/auth/services/mobile_idtoken_test.go` (new), `backend/internal/core/auth/handlers/mobile_flow_test.go` (new)

**Interfaces:**
- Consumes: `OAuthStateStore.Take` (GETDEL, `oauth_state_service.go:329-335`), `crypto/subtle.ConstantTimeCompare`.
- Produces:
  - `POST /v1/auth/{tier}/{google,apple}/mobile/begin` — body `{ code_challenge }` → `{ nonce }`
  - `POST /v1/auth/{tier}/{google,apple}/mobile` — body `{ id_token, code_verifier }`
  - `IDTokenValidationRequest.Issuers []string`, `.ExpectedNonce string`
  - Redis key prefix `oauth:mobile:nonce:`

- [ ] **Step 1: Write the failing validator tests**

Create `backend/internal/core/auth/services/mobile_idtoken_test.go` with a locally-signed RSA key and table cases per validator:

```go
// M-20: the mobile validators checked the signature and, when the config
// read succeeded, the audience. No issuer check at all, and an EMPTY
// expected audience SKIPPED the check — so MobileAudience returning ""
// disabled it rather than failing the login.

func TestGoogleValidateIDToken(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*claims)
		req     func(*IDTokenValidationRequest)
		wantErr bool
	}{
		{"happy path", nil, nil, false},
		{"wrong issuer", func(c *claims) { c.Iss = "https://evil.example.com" }, nil, true},
		{"accounts.google.com is accepted", func(c *claims) { c.Iss = "accounts.google.com" }, nil, false},
		{"wrong audience", func(c *claims) { c.Aud = "someone-elses-client-id" }, nil, true},
		{"EMPTY expected audience is an ERROR, not a skip", nil, func(r *IDTokenValidationRequest) { r.Audience = "" }, true},
		{"missing exp", func(c *claims) { c.Exp = 0 }, nil, true},
		{"expired", func(c *claims) { c.Exp = time.Now().Add(-time.Hour).Unix() }, nil, true},
		{"nonce matches", func(c *claims) { c.Nonce = "abc" }, func(r *IDTokenValidationRequest) { r.ExpectedNonce = "abc" }, false},
		{"nonce mismatch", func(c *claims) { c.Nonce = "abc" }, func(r *IDTokenValidationRequest) { r.ExpectedNonce = "xyz" }, true},
		{"nonce missing when expected", nil, func(r *IDTokenValidationRequest) { r.ExpectedNonce = "abc" }, true},
		{"no nonce expected (web Apple exchange)", func(c *claims) { c.Nonce = "" }, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// … sign, validate, assert …
		})
	}
}

// The same table for Apple, with iss https://appleid.apple.com.
func TestAppleValidateIDToken(t *testing.T) { /* … */ }

// The error returned to the CALLER must not carry the wrapped parse
// error (L-27): details go to the log.
func TestValidateIDToken_ErrorDoesNotLeakParserDetail(t *testing.T) {
	_, err := validateWithGarbage(t)
	if strings.Contains(err.Error(), "token is malformed") {
		t.Fatal("the caller's error must be opaque; details belong in the log")
	}
}
```

- [ ] **Step 2: Write the failing flow tests**

Create `backend/internal/core/auth/handlers/mobile_flow_test.go`:

```go
// The two-step flow: the backend issues the nonce and holds it against a
// client-committed PKCE challenge, so a token exfiltrated on its own
// (SDK logs, crash reports, a leaked debug build) is worthless without
// the verifier, and a replay finds no record.

func TestMobileFlow_HappyPathTakesTheRecordExactlyOnce(t *testing.T) {
	h, store := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))

	if _, err := completeMobile(t, h, "google", signedIDToken(t, withNonce(nonce)), verifier); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if store.recordExists(nonce) {
		t.Fatal("the record must be consumed")
	}
}

func TestMobileFlow_ReplayIsRefused(t *testing.T) {
	h, _ := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))
	token := signedIDToken(t, withNonce(nonce))

	if _, err := completeMobile(t, h, "google", token, verifier); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if code := completeMobileStatus(t, h, "google", token, verifier); code != http.StatusUnauthorized {
		t.Fatalf("second presentation = %d, want 401", code)
	}
}

// A stolen token WITHOUT the verifier is worthless — the residual this
// whole design exists to close.
func TestMobileFlow_WrongVerifierIsRefusedAndBurnsTheRecord(t *testing.T) {
	h, store := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))

	if code := completeMobileStatus(t, h, "google", signedIDToken(t, withNonce(nonce)), "wrong-verifier"); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	if store.recordExists(nonce) {
		t.Fatal("a failed completion must BURN the challenge, as the web relay does")
	}
}

func TestMobileFlow_RecordFromAnotherProviderIsRefused(t *testing.T) {
	h, _ := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))

	if code := completeMobileStatus(t, h, "apple", signedIDToken(t, withNonce(nonce)), verifier); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a cross-provider record", code)
	}
}

func TestMobileFlow_RecordFromAnotherTierIsRefused(t *testing.T) {
	h, _ := newMobileHandler(t, tier("client"))
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))

	operatorH, _ := newMobileHandler(t, tier("operator"), sharingStore(h))
	if code := completeMobileStatus(t, operatorH, "google", signedIDToken(t, withNonce(nonce)), verifier); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a cross-tier record", code)
	}
}

func TestMobileFlow_ExpiredRecordIsRefused(t *testing.T) {
	h, store := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))
	store.fastForward(11 * time.Minute)

	if code := completeMobileStatus(t, h, "google", signedIDToken(t, withNonce(nonce)), verifier); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
}

// GETDEL gives the record to exactly one racer — the
// TestMFAChallengeConsume_AllowsExactlyOneConcurrentWinner shape.
func TestMobileFlow_ConcurrentCompletionsHaveOneWinner(t *testing.T) {
	h, _ := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))
	token := signedIDToken(t, withNonce(nonce))

	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := completeMobileStatus(t, h, "google", token, verifier); code == http.StatusOK {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want exactly 1", wins.Load())
	}
}

// The client-supplied access_token is GONE — it was stored unverified.
func TestMobileFlow_NoAccessTokenIsAcceptedOrPersisted(t *testing.T) {
	h, store := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))

	if _, err := completeMobileRaw(t, h, "google", map[string]string{
		"id_token": signedIDToken(t, withNonce(nonce)), "code_verifier": verifier,
		"access_token": "attacker-supplied",
	}); err != nil {
		t.Fatalf("an extra field must simply be ignored: %v", err)
	}
	if store.persistedAnyAccessToken() {
		t.Fatal("no client-supplied access token may be persisted")
	}
}

// Every miss is ONE opaque 401 — the caller must not learn WHICH check
// failed.
func TestMobileFlow_EveryFailureIsTheSameAnswer(t *testing.T) {
	h, _ := newMobileHandler(t)
	bodies := []string{"no record", "wrong verifier", "wrong provider", "expired"}
	var seen []string
	for _, b := range bodies {
		seen = append(seen, completeMobileBody(t, h, b))
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[0] {
			t.Fatalf("answers differ: %q vs %q", seen[0], seen[i])
		}
	}
}

// The Google handler hardcoded platform "android" (:1284); it must read
// deviceInfo.Platform like the Apple one.
func TestMobileFlow_GooglePlatformComesFromDeviceInfo(t *testing.T) {
	h, _ := newMobileHandler(t)
	verifier := "v-0123456789012345678901234567890123456789012"
	nonce := beginMobile(t, h, "google", challengeOf(verifier))

	res := completeMobileWithDevice(t, h, "google", signedIDToken(t, withNonce(nonce)), verifier, "ios")
	if res.Platform != "ios" {
		t.Fatalf("platform = %q, want the device-reported ios", res.Platform)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/core/auth/... -run 'MobileFlow_|ValidateIDToken' -count=1`
Expected: FAIL — no `begin` route, no nonce, no issuer check.

- [ ] **Step 4: Extend the validation request**

```go
type IDTokenValidationRequest struct {
	// … existing fields …

	// Issuers is the set of acceptable `iss` values. Empty is an ERROR:
	// a token minted by anyone would otherwise pass the signature check
	// against a key set the attacker also controls.
	Issuers []string

	// ExpectedNonce, when non-empty, must equal the token's `nonce`
	// claim. Empty on the web Apple exchange, which is unchanged and
	// keeps its state-cookie binding.
	ExpectedNonce string
}
```

Both validators check, in order: signature (as today), `jwt.WithExpirationRequired()`, `iss ∈ Issuers` (Google: `https://accounts.google.com`, `accounts.google.com`; Apple: `https://appleid.apple.com`), `aud` **always** — an empty `request.Audience` is an error, no longer a skip (`:234`, `:303`) — and the nonce when expected. Errors returned to the caller drop the wrapped `err` (`:1279`, `:1294`, `:1390`, `:1411`); details go to the log.

- [ ] **Step 5: Implement the two-step flow**

```go
// mobileNonceRecord is what `begin` stores and `complete` takes.
//
// The nonce is issued and held by the BACKEND, against a challenge the
// client commits to before it ever sees the nonce. So an ID token
// exfiltrated on its own — SDK logs, a crash report, a leaked debug
// build — is worthless: it has no verifier. A token minted for another
// app or flow has no record. A replay finds none, because the take is a
// GETDEL.
//
// What it does NOT defeat: an attacker who observes the completion
// request itself (a compromised device, a broken TLS channel) holds both
// token and verifier and can race the legitimate completion. No
// request-level control closes that; it is the same boundary the web
// flow's cookie binding has, and the platform accepts it there.
type mobileNonceRecord struct {
	Provider      string    `json:"provider"`
	Tier          string    `json:"tier"`
	CodeChallenge string    `json:"codeChallenge"`
	CreatedAt     time.Time `json:"createdAt"`
}

const mobileNonceKeyPrefix = "oauth:mobile:nonce:"
```

`begin`: validate `code_challenge` (non-empty, base64url, plausible S256 length), mint a 32-byte nonce, store the record under `mobileNonceKeyPrefix + sha256hex(nonce)` with the 10-minute TTL the web state uses, return `{ nonce }`.

`complete`: validate the token (above) → require the `nonce` claim → derive the key per provider (Google: `sha256(claim)`; Apple: the claim *is* the hash the SDK sent, so use it directly) → `OAuthStateStore.Take` → check `record.Provider` and `record.Tier` against the route → `subtle.ConstantTimeCompare(sha256(code_verifier), record.CodeChallenge)` → only then `HandleOAuthCallbackWithLinking`. Any miss is one opaque 401, and a taken record is **not** restored.

Remove `access_token` from both request bodies.

- [ ] **Step 6: Document the mobile contract**

`mobile/README.md` and `mobile/CLAUDE.md` gain the `begin` + `complete` sequence. The in-tree Flutter app does not call these routes (`mobile/lib` has no OAuth code), so nothing shipped breaks — say that explicitly, and note that a shipped app must target this release or later.

- [ ] **Step 7: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
make -C /home/tore/orkestra openapi-dump
git add backend mobile
git commit -m "$(cat <<'EOF'
fix(auth): bind mobile ID tokens to a server-issued, single-use challenge

The mobile routes accepted an ID token with no nonce, no issuer check,
and an audience check that was SKIPPED when the config read failed — so
MobileAudience returning "" disabled the check rather than failing the
login. The client-supplied access_token was stored unverified.

The flow becomes begin + complete. The backend mints and holds the nonce
against a PKCE challenge the client commits to before it ever sees the
nonce, takes the record atomically at completion (GETDEL) and checks the
client's verifier in constant time. A token exfiltrated on its own is
worthless without the verifier; a token minted for another app or flow
has no record; a replay finds none. Every miss is one opaque 401 and a
taken record is not restored — a failed completion burns the challenge,
as the web relay does.

Both validators now require exp, check iss against an explicit set, and
check aud ALWAYS — an empty expected audience is an error, not a skip.
The Google handler reads the platform from deviceInfo instead of
hardcoding "android", and caller-facing errors drop the wrapped parser
detail.

The in-tree Flutter app has no OAuth code, so the contract change breaks
nothing shipped; a shipped app must target this release or later.

Spec §4.10 D35. Closes M-20 and L-27.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 8: Documentation, gate and the staging drill

- [ ] **Step 1: Docs sweep**

- `backend/internal/core/auth/CLAUDE.md` — `:38`, `:49`, `:847`, `:856`, `:947`, `:1065` (identity store, index, unlink, auth-methods source); `:41`, `:766`, `:805-806` (PKCE, mobile nonce/iss); new sections on the identity store and the tombstone read/write split.
- `docs/site/modules/core/auth.mdx` — `:13` ("OAuth 2.1" is now true — PKCE is actually sent).
- `docs/site/architecture/authentication-flow.mdx` — `:275`, `:280`, `:304`, `:318`, `:332`.
- `docs/migrations/0010_oauth_provider_identity_unique.md` — written in Task 1; re-read it now that the code exists and correct anything that drifted.

- [ ] **Step 2: Gate**

```bash
make -C /home/tore/orkestra ci-backend
git diff --check
```

- [ ] **Step 3: Commit and open the PR**

```bash
cd /home/tore/orkestra && git add docs backend
git commit -m "$(cat <<'EOF'
docs(auth): document the OAuth identity store, PKCE and the mobile flow

Spec §4.11.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
git push -u origin feat/auth-authz-audit-remediation-pr-d1
gh pr create --base dev --title "PR D1: OAuth identity, PKCE and mobile (expand) — auth/authz audit remediation" --body "$(cat <<'EOF'
Implements §4.7, §4.8 items 1 and 3–8 (**no tombstone writes**), §4.9 and §4.10 of `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` — the **D1 (expand)** row of §7.

**Closes:** H-6, H-7 (read + write halves), M-18, M-20, L-27, L-29, L-30.

⚠️ **Migration 0010 must have exited zero on every environment BEFORE this deploys.** A conflict report stops the rollout until an operator names the keeper per identity. A deploy that skips it boots with OAuth degraded to `oauth_store_unavailable` rather than running the ownership-first flow without its constraint.

**Expand only:** every reader honours `unlinkedAt`; nothing writes one. PR D2 turns the writes on, and from the first tombstone this release is the hard rollback floor.

Plan: `docs/superpowers/plans/2026-09-03-auth-authz-audit-remediation-pr-d1-oauth-identity-expand.md`

https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

- [ ] **Step 4: Staging drill (spec §7, D1 row)**

1. Run migration 0010 on staging. If it reports conflicts, resolve them with a `RESOLVE` map and file the output. It must exit zero **before** the deploy.
2. Seed one provider doc with `unlinkedAt` **by hand**, then sign in with that identity → **`oauth_identity_unlinked`**, and it is absent from `/me/auth-methods`.
3. A **normal** unlink still behaves exactly as today (no tombstone written) — that is the expand contract.
4. Boot with the index dropped → the health check is degraded and the callback answers `oauth_store_unavailable`. Recreate it and confirm the flow returns.
5. Sign in with Google and with Discord → both succeed **with** PKCE (check the provider request carries `code_challenge`).
6. Flip `SupportsPKCE` to `true` for GitHub and Apple **on a staging branch only** and round-trip each. Promote the constants only if they succeed.
7. Mobile: `begin` + `complete` with a locally-signed token via `curl`. Then replay the same token → 401. Then complete with a wrong `code_verifier` → 401, and confirm the record is gone.
8. On an install that already has a super_admin, restart the backend and confirm the log line `first-admin sentinel backfilled` with that user's UUID; restart again and confirm it does **not** repeat.

---

## Self-review

**Spec coverage (§4.7, §4.8 items 1 + 3–8, §4.9, §4.10 + §6 "PR D — OAuth" minus the tombstone-write rows):**

| Spec item | Task |
|---|---|
| D30 audience guard, fatal claim error, shared UUID | 2 |
| D31 `SystemRoleHolderFinder`, backfill, `GetUserCount` fallback | 3 |
| D32 item 1 index + migration + boot check | 1 |
| D32 item 2 — model field READ only (writes are PR D2) | 1, 4 |
| D32 item 3 tombstoned identity refused at the callback | 4 |
| D32 item 5 ownership-first, duplicate re-read, signup reservation + compensation | 5 |
| D32 item 6 readers move to the provider collection | 4 |
| D32 item 7 lazy read-model repair | 4 |
| D32 item 8 orphan reservation heals | 5 |
| D33 degraded lookups fail closed | 5 |
| D34 PKCE, `SupportsPKCE`, dead-code deletion, `pkce_test.go` | 6 |
| D35 two-step mobile flow, issuer/audience/nonce validation, no `access_token` | 7 |
| §4.11 docs | 1, 3, 7, 8 |

**Explicitly deferred to PR D2:** §4.8 item 2's tombstone **writes** (`MarkUnlinked` on both unlink paths) and item 4's revive/move rules. Task 4 ships the readers those depend on.

**Placeholder scan:** none. Task 1's migration test references a `seed` / `runMigration` / `hasUniqueIndex` harness that `20260823_authz_bindings_unique.test.js` already establishes; the step says to read that file first, which is the right instruction — reproducing a harness that exists would be worse.

**Type consistency:** `ErrOAuthStoreUnavailable`, `ErrOAuthIdentityUnlinked` and `ErrOAuthIdentityClaimedByOther` are declared once (Tasks 1, 4, 5 respectively) and consumed with the same names throughout. `GetByProviderAndIDIncludingUnlinked` is introduced in Task 4 and used in Task 5. `claimIdentity(ctx, doc) (*models.OAuthProviderDoc, error)` has one signature. `SupportsPKCE() bool` is on the interface in Task 6 and nowhere else. `mobileNonceRecord` fields match what `begin` writes and `complete` reads.

**Two risks worth naming:**
1. Task 5's orphan-healing branch must distinguish *not found* from *outage*. Getting that backwards deletes real users' identities during a Mongo blip. `TestCallback_OrphanCheckDistinguishesOutageFromAbsence` is the guard; a reviewer should check `isNotFound` matches what the user service actually returns.
2. Task 7's Apple nonce derivation differs from Google's — Apple's SDK hashes the nonce before the token carries it, Google's passes it through. The key derivation must match per provider or every Apple completion 401s. The cross-provider test covers the record check, not this; verify it against a real Apple token in drill step 7.
