# Migration 0010 — One OAuth identity, one owner

One-shot `mongosh` migration that adds a unique index on `(provider, providerId)` to `operator_oauth_providers` and `client_oauth_providers`. It is the precondition for the release that makes the provider collection the single source of truth for OAuth identity ownership (auth spec §4.8 D32): that release detects a conflicting claim by the duplicate key this index produces, so without the index two users could silently share one identity again.

Script: `backend/migrations/20260903_oauth_provider_identity_unique.js`. Test: `20260903_oauth_provider_identity_unique.test.js` next to it (`node --test`, needs a throwaway mongod — see its header).

## Why it refuses instead of deciding

The existing unique index on `(userUuid, provider)` already makes two rows of one user for one provider impossible. So every duplicate on `(provider, providerId)` is one identity — one Google account, one GitHub account — claimed by **two different users**. Which of them owns it is a question about people, not about data. An automatic rule ("keep the earliest-linked row", "keep the most recently used") would silently strip a real person of their way to sign in, and the row it discards may be the one whose user actually holds that provider account today.

The migration therefore **reports every conflicting group and exits non-zero without touching anything**. Reconciliation is an explicit decision per identity, recorded with the change.

## Reading a conflict report

```
CONFLICT operator_oauth_providers google:1234 — claimed by 2 users:
  userUuid=4b2f… linkedAt=2026-01-03T10:12:00Z lastUsed=2026-09-28T07:41:00Z tokenStatus=active
  userUuid=9e77… linkedAt=2026-02-14T09:03:00Z lastUsed=null tokenStatus=revoked

1 identity conflict(s) unresolved. Re-run with a RESOLVE map naming the keeper per identity.
No index was created. See docs/migrations/0010_oauth_provider_identity_unique.md
```

One block per identity, one line per claiming user. Nothing has been deleted and no index exists yet.

## Deciding a keeper

This is a human decision. For each identity:

1. Look at `lastUsed` and `tokenStatus`: the row that signed in recently with an active token is almost always the account the person actually uses.
2. Confirm with the account holders where possible: the user named by the keeper row must recognise that provider account as theirs. Check the two users' emails against the provider identity's email.
3. Record the reasoning in the change ticket alongside the migration output.

A keeper that does not hold the identity (a typo, a wrong UUID) is refused: the migration prints `RESOLVE names <uuid>, which does not hold this identity. Nothing deleted.` and keeps blocking.

## Running it

Plain run, expected on most installs (no duplicates exist on a database that only ever had one owner per identity):

```bash
set -a; . docker/.env; set +a
docker exec -i "${APP_NAME}-mongodb-${ENV}" mongosh --quiet \
  -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
  --authenticationDatabase admin "$MONGO_DATABASE" \
  < backend/migrations/20260903_oauth_provider_identity_unique.js
```

With a resolution map, after the decision above:

```bash
set -a; . docker/.env; set +a
{ echo 'var RESOLVE = {"google:1234": "4b2f…", "github:9999": "c0de…"};'
  cat backend/migrations/20260903_oauth_provider_identity_unique.js; } |
docker exec -i "${APP_NAME}-mongodb-${ENV}" mongosh --quiet \
  -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
  --authenticationDatabase admin "$MONGO_DATABASE"
```

Only rows of the listed identities are deleted, only the losers, and every deletion is printed (`deleted <collection> uuid=… userUuid=… identity=…`). **File that output with the change.** Identities not in the map still block, so the run is repeatable until the map is complete. With zero groups left the index is created on both collections and verified; the script then prints `migration 0010 complete`. A re-run on a migrated database is a no-op.

## Deploy gate

The release that ships ownership-first OAuth writes verifies this index at boot. If it is missing, the auth module's health check reports degraded and **every OAuth login and link path answers `oauth_store_unavailable`** until the migration has run — never a boot failure, auth is a core module, but no social sign-in either. So: **this migration must have exited zero on every environment before that release is deployed there.** A conflict report stops the rollout for that environment until an operator names the keeper per identity.

## Rollback

The index itself is harmless to an older binary — it only refuses a duplicate the old code would have created by accident. Dropping it is `db.operator_oauth_providers.dropIndex("provider_1_providerId_1")` (and the client twin), but there is no reason to: the release's rollback floors are documented in the auth spec §7 and none of them requires the index to go.
