# Auth roadmap — remaining work and verification checks

Status snapshot at commit `68529ab` (2026-04-24). Sections A, B, C of the 2026-04-24 auth audit ([memory](../home/tore/.claude/projects/-mnt-c-Users-tore-orkestra/memory/project_auth_audit_2026_04_24.md)) are shipped. Sections D, E, F are open, plus a tail of deferred follow-ups from the completed sections. This document is the single source of truth for "what's left".

## Contents

- [0. Verification of shipped work (before any new section)](#0-verification-of-shipped-work-before-any-new-section)
- [1. Section D — Customer-facing features](#1-section-d--customer-facing-features)
- [2. Section E — Operational / compliance](#2-section-e--operational--compliance)
- [3. Section F — DX / quality](#3-section-f--dx--quality)
- [4. Deferred follow-ups from Sections A / B / C](#4-deferred-follow-ups-from-sections-a--b--c)
- [5. Confirmed issues from the original audit still open](#5-confirmed-issues-from-the-original-audit-still-open)
- [6. Suggested sequencing](#6-suggested-sequencing)

---

## 0. Verification of shipped work (before any new section)

Run these before starting new work so a green baseline is established. Local Go + Docker were unavailable during the session that authored Sections A–C; CI is the first execution for 8 of the recent commits.

### 0.1 CI must be green on `feat/architecture-modernization`

| Job | What it checks |
|---|---|
| `backend.lint` | golangci-lint across the full tree. New files use `log/slog` + structured attrs; any `fmt.Errorf` wrapping check should pass cleanly. |
| `backend.tenantscope` | Analyzer for tenantrepo usage — should still pass; nothing in A/B/C touches addon Mongo queries. |
| `backend.policycoverage` | Permission-to-Cedar-policy coverage gate. Baseline was trimmed in commit `138dfa5`; CI should still pass without that trim, but the tight baseline is the cleaner state. |
| `backend.test` | Full test suite including the 39 new tests added across Sections A–C. |
| `frontend.*` | Unchanged in this block. |

**Checklist before any new section starts:**

- [ ] Push the branch and wait for CI green
- [ ] Investigate any new flaky test that didn't exist before commit `5e886e8`
- [ ] Verify `policycoverage` baseline has no dead entries after the commit `138dfa5` trim
- [ ] Confirm no `go mod tidy` drift on go.mod (`go mod verify` locally once available)

### 0.2 Smoke tests against a running stack

Spin up the dev stack (`./orkestra.sh` → full dev profile) and exercise:

- [ ] **C1 scorer**: log in twice from different IPs within 5 minutes → session 2 should have `RiskScore > 0` in `db.auth_sessions` (currently only reachable by querying Mongo directly — the admin UI doesn't surface it yet).
- [ ] **C2 step-up**: with `AUTH_RISK_STEP_UP_THRESHOLD=0.01` exported, any admin mutation (`PATCH /v1/admin/modules/<m>`) should return `401 code=step_up_required`.
- [ ] **C3 device trust**: complete MFA on the verify endpoint with `trustDevice:true`, log out, log back in → the second login should not prompt for TOTP.
- [ ] **C3 self-service**: `GET /v1/auth/me/devices/trust` lists the grant; `DELETE /v1/auth/me/devices/trust/<id>` revokes it.
- [ ] **C3 auto-revoke**: change password → the previously trusted device no longer skips MFA.
- [ ] **C4 geoip**: with `AUTH_GEOIP_DB_PATH` set to any non-empty string, boot should log `geoip: AUTH_GEOIP_DB_PATH set but MaxMind backend unavailable` with the remediation hint.
- [ ] **C5 email**: with `NOTIFICATION_EMAIL_PROVIDER=noop` exported, trigger a high-risk login (new device + new IP + rapid IP change via VPN switch) → backend stdout should show the rendered `auth.suspicious_login` email body.
- [ ] **C5 security event**: `db.auth_security_events.find({ userUuid: "<u>" })` should show a `suspicious_login` row with `severity: "warning"` and the factor list in `metadata.factors`.

### 0.3 Divergence telemetry sanity

Section B #4 adds ABAC forbids that shadow-evaluate. Monitor for a day:

- [ ] Grafana panel `orkestra_cedar_divergence_total` — new policy IDs (`abac.require_mfa_for_admin_suffix`, `abac.deny_system_actions_from_public_ip_in_prod`) should appear but with low volume on internal traffic.
- [ ] Verify no Cedar eval errors in the stdout logs (`cedar: shadow eval panicked` should stay zero).

---

## 1. Section D — Customer-facing features

All four items are tenant-facing (Tier 2). Each is independently valuable; pick one at a time.

### D1 — SCIM 2.0 provisioning (per-tenant SCIM endpoint)

**What:** full SCIM 2.0 `/scim/v2/Users` + `/scim/v2/Groups` implementation so external clients can provision users from Okta / Azure AD. Phase 3 already shipped stub handlers; this lights them up.

**Why:** unlocks enterprise SSO deployments where the client's IdP owns user lifecycle.

**Scope:** large — 3–5 commits. Users CRUD, Groups CRUD, `filter` + `startIndex`/`count` pagination, PATCH operations, bearer-token auth per-tenant.

**Key files:**
- `backend/internal/addons/identity/handlers/scim_*.go` (stubs exist from Phase 3)
- `backend/internal/addons/identity/services/scim_service.go` (new)
- New collection: `scim_external_ids` (maps SCIM `externalId` ↔ Orkestra userUUID)

**Checks:**
- [ ] SCIM 2.0 compliance test suite (Microsoft's `ScimValidator` or similar) passes
- [ ] Bearer token rotation works without breaking an in-flight sync
- [ ] PATCH Remove on Members of a Group cascades to the authz binding
- [ ] Soft-delete via PATCH `active:false` respects GDPR DSR — doesn't force-purge
- [ ] Manual test with Okta pointing at a staging tenant

### D2 — BYO OIDC/SAML per external tenant

**What:** external tenants can register their own OIDC IdP or SAML IdP so their users log in via their own SSO. Phase 3 shipped OIDC foundation; SAML and the per-tenant SSO-first login page are missing.

**Why:** required by enterprise buyers; also lets clients revoke access through their own IdP without touching Orkestra.

**Scope:** large — 4–6 commits. Per-tenant IdP config, SAML metadata import/export, SP-initiated + IdP-initiated flows, just-in-time user provisioning, claim mapping, IdP rotation.

**Key files:**
- `backend/internal/addons/identity/` — already has the OIDC scaffold
- New: `backend/internal/addons/identity/saml/` (gosaml2 or crewjam/saml)
- New collection: `identity_tenant_idps` (per-tenant IdP configs, AES-256-GCM for secrets)

**Checks:**
- [ ] OIDC login completes end-to-end against a test Auth0 app
- [ ] SAML login completes against a test OneLogin / simpleSAMLphp IdP
- [ ] IdP rotation: flipping the IdP config mid-session keeps active sessions valid until their TTL
- [ ] Account-linking flow: existing password user → SSO user merges cleanly
- [ ] Audit: every SSO login emits `auth.login.succeeded` with `source: "sso"`

### D3 — Fine-grained API keys (per-tenant, capability-scoped)

**What:** tenants generate API keys for programmatic access. Each key is scoped to a subset of capabilities (e.g. "only RAG query, no billing"). Rotatable, revocable, rate-limited per key.

**Why:** CI/CD integrations and partner consumption need machine credentials; today the only way is a long-lived JWT that's also UI-capable.

**Scope:** medium — 2–3 commits.

**Key files:**
- `backend/internal/addons/apikeys/` (new module)
- Middleware `RequireAPIKey` parallel to `RequireAuth`
- Collection: `api_keys` (hashed key + capability scope + rate-limit bucket + expiry)

**Checks:**
- [ ] Key format: `ork_<env>_<26-char-random>` (prefix lets ops spot a leaked key in logs)
- [ ] Hashed storage (SHA-256); raw key returned exactly once on creation
- [ ] Capability scope enforced via the existing RequireCapability middleware
- [ ] Revocation takes effect within 1 second (Redis-cached, cache invalidated on revoke)
- [ ] Rate limit per key tracked separately from per-user quotas
- [ ] GDPR DSR: PII producer drops rows for the deleted user

### D4 — Audit log viewer for tenant admins

**What:** a self-service view in the tenant admin UI showing compliance-audit events scoped to their tenant. Reads `compliance_audit_events` filtered by `tenantId`.

**Why:** customers expect an activity log; lets them investigate their own users' actions without filing an operator ticket.

**Scope:** small — 1–2 commits. Backend filter endpoint, frontend view.

**Key files:**
- `backend/internal/core/compliance/handlers/audit_viewer.go` (new)
- `frontend-admin/src/pages/settings/audit.tsx` (new)

**Checks:**
- [ ] Filter: by actor, action, outcome, date range
- [ ] Cross-tenant isolation: tenant A's admin cannot see tenant B's events (tenantrepo `Scope()` usage required)
- [ ] Pagination with stable cursor
- [ ] Export to CSV (one-off, not streaming)
- [ ] Performance: 100k-event tenant returns under 500ms with an index on `(tenantId, timestamp DESC)`

---

## 2. Section E — Operational / compliance

### E1 — JWT key rotation ceremony (JWKS + `kid` + overlapping validity)

**What:** today the JWT signing key is a single PEM read from env. Rotation means a deploy. E1 adds JWKS publishing (`/.well-known/jwks.json`), `kid` claims on new tokens, and a grace window where two keys are valid simultaneously.

**Why:** industry norm; required by some compliance frameworks; lets a leaked key be rotated without logging everyone out.

**Scope:** medium — 2 commits (publish + rotation runbook).

**Key files:**
- `backend/internal/core/auth/services/jwt_service.go` — multi-key support, `kid` generation
- `backend/internal/core/auth/handlers/jwks_handler.go` (new)
- `backend/internal/shared/middleware/jwt_validator.go` — `kid` lookup in the key ring

**Checks:**
- [ ] `GET /.well-known/jwks.json` returns the current + rotating-out keys
- [ ] Tokens minted with the old key still validate during the grace window
- [ ] Tokens minted with the new key validate immediately on every node (Redis broadcast of the new kid)
- [ ] Rotation runbook tested on staging: key-rotate → deploy → verify no 401s → drop the old key
- [ ] AI sidecar's `JWTValidator` also follows the JWKS

### E2 — Admin action quorum (2-person approval for destructive ops)

**What:** catastrophic operations (tenant delete, role assignment to super_admin, KMS key rotation) require a second admin's approval within a time window before executing.

**Why:** SOC2 "separation of duties" control; prevents a single compromised admin account from mass-deleting tenants.

**Scope:** medium — 2 commits. Generic "quorum request" model + one or two wired operations.

**Key files:**
- `backend/internal/core/authz/services/quorum_service.go` (new)
- Collection: `authz_quorum_requests` (requester, action, payload-hash, approvers[], expiresAt)
- Middleware `RequireQuorum(action, N)` that either creates the request or executes when N approvals exist

**Checks:**
- [ ] A single admin posting `DELETE /v1/tenants/{id}` gets a `202 pending_approval` with a request ID
- [ ] A second admin approving via `POST /v1/authz/quorum/{id}/approve` triggers the actual delete
- [ ] Self-approval is refused (actor cannot approve their own request)
- [ ] TTL: unapproved requests expire (default 1h) and the original action is not auto-executed
- [ ] Audit: both the request and the approvals emit separate rows, both with the original payload-hash

### E3 — Session management UI (user-facing)

**What:** a self-service page where users see all their active sessions (device, IP, last activity, location) and can revoke any individual session.

**Why:** standard expectation in 2026; also the landing page for the "if this wasn't you, log out everywhere" link from the C5 suspicious-login email.

**Scope:** small — the backend endpoints already exist (`GET /v1/auth/sessions`, `DELETE /v1/auth/sessions/{id}`). Shipping is mostly a frontend view.

**Key files:**
- `frontend-admin/src/pages/account/security.tsx` (new) — rendering + revoke action
- Backend: verify existing endpoints surface the fields the UI needs (location, trust level, last activity)

**Checks:**
- [ ] Current session is flagged (can't accidentally revoke yourself)
- [ ] Revoke latency: a revoked session's token is rejected within 1s (session revocation list is Redis-backed, fail-open acceptable)
- [ ] C5 email link `{frontend}/account/security` lands on this page
- [ ] Passes a11y audit on the table + revoke buttons

### E4 — Scheduled revoked-token reaper

**What:** `CleanupRevokedTokens` exists in the refresh token repo but isn't cron'd. `auth_refresh_tokens` grows unbounded.

**Why:** storage cost; also the replay-detection window (>= 30d) is a floor not a ceiling — we should reap rows older than that.

**Scope:** tiny — 1 commit.

**Key files:**
- `backend/internal/core/auth/services/auth_service.go::CleanupRevokedTokens` (exists)
- New: NATS JetStream cron job or a goroutine started in `Module.Start()`

**Checks:**
- [ ] Job runs every 24h, reaps rows with `revokedAt < now - 31d`
- [ ] Log entry on each run: how many rows reaped
- [ ] Never touches non-revoked rows
- [ ] Doesn't interact with the family-revoke replay detection (already guarded by the 30d floor)

---

## 3. Section F — DX / quality

### F1 — Auth test harness

**What:** a package `backend/internal/testkit/authflows/` exposing helpers to spin up common auth fixtures — MFA-enrolled user, super_admin, privileged user past grace, expired-token holder. Each test that touches auth currently stitches the same boilerplate.

**Why:** the risk-scorer tests alone carry 300+ lines of stub plumbing. A shared harness halves that. Also the documented path for addon authors to write protected-endpoint tests.

**Scope:** medium — 1 commit for the harness + 1 refactor commit that migrates the highest-traffic tests.

**Key files:**
- `backend/internal/testkit/authflows/` (new)
- Migrated callers in `backend/internal/core/auth/services/*_test.go`

**Checks:**
- [ ] Harness covers: password login, OAuth login, MFA-login-verify, step-up verify, logout, session revoke, device-trust grant/revoke
- [ ] Works against a real mongo (via testcontainers) AND a pure in-memory stub (toggle)
- [ ] Migrated tests do not lose coverage
- [ ] CI runtime: harness shouldn't add more than 30s to the test job

### F2 — Rewrite `docs/Authentication_flow.md`

**What:** the current doc is ~50% stale (claims 5 Italian roles when there are 6 English; says refresh TTL is 7d when it's 30d; documents the HS256→RS256 migration as pending when it shipped long ago). Rewrite against actual code.

**Why:** this doc is what contributors read first. Stale security docs are actively harmful — they prescribe patterns that don't match the code.

**Scope:** small — 1 commit, pure docs.

**Checklist of things to cover:**
- [ ] 6 system roles in English (`super_admin / administrator / developer / manager / operator / guest`)
- [ ] 5 tenant-level org roles (`org_owner / org_admin / org_member / org_billing / org_viewer`)
- [ ] Refresh TTL = 30d; access TTL = 15m (current defaults)
- [ ] RS256-only (no HS256 fallback)
- [ ] Refresh rotation with family detection
- [ ] Session revocation list (Section A)
- [ ] WebAuthn / passkeys (Section A)
- [ ] Step-up middleware (Section A)
- [ ] Risk-score gate (Section C2)
- [ ] Device trust (Section C3)
- [ ] Impossible-travel (Section C4)
- [ ] Suspicious-login email (Section C5)
- [ ] Cedar ABAC attribute reference
- [ ] Sequence diagrams: password login, OAuth login, MFA verify, refresh rotation, step-up

---

## 4. Deferred follow-ups from Sections A / B / C

Each is a self-contained small commit unless noted. Order within a section is priority-descending.

### 4.1 Section B follow-ups

- [ ] **Enable Cedar enforce mode on the 4 system.* actions in dev compose**. One-line env var addition (`CEDAR_ENFORCE_ACTIONS=system.modules.admin,system.tenants.admin,system.users.admin,system.users.mfa_reset`) + a smoke test that super_admin × external tenant returns 403.
- [ ] **Quorum / audit on Cedar overrides**. Today `cedar_override_allow` / `cedar_override_deny` emit Warn log + metric. SOC2 evidence collection should also stamp the override into the `audit_events` collection.
- [ ] **Per-action Grafana panel for enforce-mode counter**. Panel against `orkestra_cedar_enforced_total{action_suffix,outcome}` so ops see override rate climb as policies tighten.
- [ ] **Migration script for pre-2026-04-24 tenants**. Find every membership with `Roles=["administrator"]` + `IsOwner=true`, switch to `["org_owner"]`, create the missing org_owner binding. One-shot script in `backend/cmd/`.
- [ ] **End-to-end cascade test for OwnerRoleBinder bypass**. Current tests cover the helper; add a test through `CreateTenant` → bindOwner → `CreateBinding` with `grantedBy="system"` to prove the wired path.
- [ ] **Cleanup legacy permits in `tenant_roles.cedar`**. After the migration above, remove the 4 legacy permits (manager/operator/guest/administrator-as-tenant-role).
- [ ] **Extend policycoverage to recognize `context.action_suffix != X`**. org_admin's deny-list permit is invisible to the gate today.
- [ ] **Capability-direct coverage in policycoverage**. New `capability.cedar.unreferenced` diagnostic so capability authors name their capability directly rather than relying on the generic forbid-unless-entitled rule.

### 4.2 Section C follow-ups (including the one explicit blocker)

**Explicit blocker for C4 to be load-bearing:**

- [ ] **MaxMind library integration** (~30 lines + go.mod). Add `github.com/oschwald/geoip2-golang` to `backend/go.mod`, run `go mod tidy`, implement `newMaxMindResolver` + `maxMindResolver.Lookup` in `backend/internal/shared/geoip/maxmind.go` following the docstring instructions. No other file changes. **Until this lands, impossible_travel is inert in prod.**

**Other C follow-ups:**

- [ ] **One-click revoke link on C5 email**. Signed-token endpoint that directly revokes the flagged session on click. Reuses the email-token repository + SessionRevocationService. ~150 lines.
- [ ] **OAuth + mobile login hooks for C5 notifier**. Only the password login path calls `createSessionDoc` (and thus the notifier) today. OAuth + mobile paths need parallel hooks.
- [ ] **Web fingerprint collection**. LoginInput now carries `Fingerprint` but only the mobile path populates it. A real fingerprint (user-agent hash + IP class + canvas FP) would tighten both C1's `new_device_fingerprint` and C3's trust cross-check.
- [ ] **Risk-score update on refresh**. `RefreshTokensWithRiskAssessment` rolls the prior doc's RiskScore forward unchanged. Re-scoring on rotation would catch IP-change-during-session.
- [ ] **Session doc `Location` enrichment**. `AuthSessionDoc.Location` is in the model but never populated. Once MaxMind lands, stamp it on `createSessionDoc` so audit queries filter by geography.
- [ ] **Grafana panel for risk-driven denials**. Convert the Warn-log on `RequireLowRisk` blocks into a Prometheus counter (`orkestra_auth_risk_gate_denied_total{path}`).
- [ ] **ABAC policy `abac.deny_capability_exercise_at_high_risk`**. Forbid capability-gated actions when `principal.risk_score >= 70`. First real consumer of the Cedar risk attributes from C2.
- [ ] **Cedar `principal.device_trust: Bool` attribute**. No policy reads it today; add when a rule needs it (e.g. `abac.forbid_billing_on_device_trust_only_session`).
- [ ] **Logout-all cascade to device trust**. Today logout-all revokes sessions but leaves trust grants alone — the current semantic lets a trusted device log back in after "everywhere logout" without re-completing MFA. Decide whether that's desired, then wire or don't.
- [ ] **Step-up verify endpoints accept `trustDevice`**. Today only login-verify flows grant trust. Step-up is for existing sessions so it's lower priority.
- [ ] **Frontend UI for C3 device trust**. "Trust this device for 30 days" checkbox on the MFA verify page + management view. Backend surface exists; this is a frontend PR.
- [ ] **Additional locales for C5 template**. English only; Italian first, then others on demand.
- [ ] **GeoIP DB hot-reload**. MaxMind publishes weekly GeoLite2 updates; SIGHUP handler would be nicer than restart-for-update.
- [ ] **Admin security-history view**. `auth_security_events` is now populated but no admin UI reads it. Useful for triaging flagged accounts.
- [ ] **Automatic device trust earned after N logins**. Explicitly out of scope for the C3 first cut; revisit if user feedback shows they want less friction even before they've manually ticked "trust this device".

### 4.3 Section A follow-ups

Section A was closed in commit `8fb2fdf` with all 8 items shipped. The only standing follow-up the memory records is:

- [ ] **WebAuthn discoverable-credential login** (passwordless). Today the flow requires password first, then offers passkey as second factor. Full passwordless would need a `BeginDiscoverableLogin` entry point.

---

## 5. Confirmed issues from the original audit still open

From `project_auth_audit_2026_04_24.md`, the seven confirmed issues the audit flagged. Post-A/B/C status:

| # | Issue | Status |
|---|---|---|
| 1 | localStorage access-token leak (XSS foothold) | ⏳ **open** — the audit flagged `frontend-admin/src/components/authentication/EmailPasswordForm.tsx:30` and `frontend-admin/src/pages/setup/steps/AdminStep.tsx:58-60` |
| 2 | `docs/Authentication_flow.md` is ~50% stale | ⏳ **open** — tracked as [F2](#f2--rewrite-docsauthentication_flowmd) |
| 3 | No revoked-token reaper scheduled | ⏳ **open** — tracked as [E4](#e4--scheduled-revoked-token-reaper) |
| 4 | Rate-limit constants hardcoded | ⏳ **open** — should become admin-tunable via ConfigSchema |
| 5 | Permissions loading race in `ProtectedRoute.tsx:45` | ⏳ **open** — `permissions.length === 0` heuristic misgates users with empty sets |
| 6 | Auto-refresh on expired bearer mid-request | ⏳ **open** — interacts badly with absence of revocation list… but revocation list shipped in A, so severity lower |
| 7 | No access-token revocation list | ✅ **closed** by Section A (Redis-backed session revocation) |

### 5.1 Frontend localStorage leak (CRITICAL)

Original flag was "CRITICAL — XSS foothold". Access tokens land in `localStorage` in two places, contradicting the HttpOnly cookie design. Fix: access token lives only in Redux memory via `setAccessToken` dispatch; refresh token stays in the HttpOnly cookie.

**Checks:**
- [ ] No `localStorage.setItem.*access.*token` references in `frontend-admin/src/`
- [ ] Page reload still works (access token rehydrates from refresh cookie on first API call)
- [ ] XSS smoke test: injected `<script>document.write(localStorage.accessToken)</script>` writes nothing

### 5.2 Rate-limit constants admin-tunable

Today `password_auth_service.go` hardcodes the limiter windows. Move to `module_configs` under the auth module with sensible defaults. Keep the current in-memory `RateLimiter` implementation — only the windows become runtime-tunable.

**Checks:**
- [ ] `ConfigSchema()` gains `login.rate_limit.*` fields (Int type)
- [ ] Live edit at `/admin/modules/auth` takes effect within 30s (ConfigService cache TTL)
- [ ] Existing tests still pass against the defaults

### 5.3 Permissions loading race

`ProtectedRoute.tsx:45` uses `permissions.length === 0` as "still loading". For users with legitimately empty permission sets (e.g. a guest with no binding in the current tenant) the component misgates. Fix: track a separate `permissionsLoaded: boolean` in the store.

**Checks:**
- [ ] A user with empty permissions in a tenant still sees a protected page where the gate is `RequireGlobal()`
- [ ] Fresh login → protected page → no flash of "access denied"
- [ ] Tenant switch → the gate re-evaluates after the new permission set loads, not before

### 5.4 Auto-refresh on expired bearer

Middleware refreshes mid-request today. With the revocation list in place the concrete risk is lower but the pattern is still fragile. Investigate whether the refresh-cookie path can be moved to an explicit refresh-before-call flow in the frontend interceptor, and whether the middleware auto-refresh can be removed.

**Checks:**
- [ ] If middleware auto-refresh is kept: `isSessionRevoked` is checked on the *refreshed* token, not just the original
- [ ] If removed: the frontend's axios interceptor performs the refresh + retry pattern cleanly, with 1 retry cap

---

## 6. Suggested sequencing

Pick one track at a time; don't interleave.

**Track 1 — Close open confirmed issues** (1–2 weeks, low risk, high value)
- localStorage fix (CRITICAL)
- Docs rewrite (F2)
- Revoked-token reaper (E4)
- Rate-limit tunables
- Permissions loading race

**Track 2 — Complete Section C activation** (1 week, unlocks the whole pipeline)
- MaxMind library integration (30-line follow-up that makes C4 load-bearing)
- Enable Cedar enforce on the 4 system.* actions in dev compose (Section B follow-up)
- Grafana panels for risk + enforce counters
- One-click revoke link for C5
- OAuth + mobile login hooks for C5

**Track 3 — Operational hardening (Section E)** (2–3 weeks)
- JWT key rotation (E1)
- Session management UI (E3) — short, unblocks the C5 email CTA
- Admin action quorum (E2)

**Track 4 — Customer-facing features (Section D)** (4–6 weeks)
- Audit log viewer (D4) — short
- API keys (D3)
- SCIM (D1)
- BYO OIDC/SAML (D2)

**Track 5 — DX (Section F)** (can run in parallel)
- Test harness (F1)
- Docs rewrite already in Track 1

Each track's items are small enough to land as individual commits. Track 2 is the highest-leverage next step — it activates work that's already shipped but currently inert.
