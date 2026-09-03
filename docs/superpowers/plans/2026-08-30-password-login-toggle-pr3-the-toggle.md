# Password-Login Toggle — PR 3: The Toggle — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give an operator a per-surface switch that turns the email/password method off completely — every unauthenticated password entry point refuses, in-flight password logins cannot complete, and the password stops counting as a credential in step-up and unlink decisions — with a strict fail-closed policy read, an anti-lockout validator that refuses any configuration with no other way in, and an operator-only break-glass env var for recovery.

**Architecture (spec v4.4 §4.1–§4.9):** Two `FieldBool` schema keys (`passwordLoginEnabled{Admin,Client}`, default `true`, no EnvVar) join the auth module's `login` group, and `AuthModule` declares `HotReloadConfig() → true`. A strict accessor pair on `AuthPolicyService` reads them through PR 1's `GetRawValueRequiredModule`: `PasswordLoginEnabled` (used by registration/reset gates, step-up, auth-method views, unlink — never sees the override) and `PasswordLoginDecision` (used ONLY by `Login` and the MFA/WebAuthn continuation of a challenge it created — the only places the boot-time `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` override can convert a stored `false` or a read failure into an audited allow, operator audience only). The gates of §4.3 land on the password service, the two admin send-password-reset routes, and both MFA completion handlers (via new required `MFAChallenge.Audience`/`BreakGlassUsed` fields). The auth validator migrates to PR 1's snapshot contract and enforces the §4.4 invariant `passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)` per surface, reusing PR 2's pure `ProviderStructurallyConfigured` over the target snapshot's effective values and secret presence. `wouldLockOutOAuthUnlink` learns to count only *usable* links; `AuthMethodsView` splits `hasUsablePassword` into `hasPasswordSet` + `passwordUsableForLogin`; `GET /v1/auth/{tier}/policy` exposes the persisted state (nullable) plus an operator-only break-glass display flag; the operator SPA hides the password UI instead of surfacing 403s, and renders a labelled emergency form only under break-glass.

**Tech Stack:** Go 1.25 (Huma v2, chi), MongoDB via PR 1's CAS repository (no new collections), React 19 + TypeScript (RTK Query, react-hook-form + yup, react-i18next), Vitest + MSW.

**Spec:** `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md` (v4.5 — v4.4 plus the §0 entry recording D1/D10/D11). PR 3 implements the §7 row "3 — Password-login toggle"; every §-reference below is to that file. PR 1 (SDK config integrity, merged as `7574368a`) and PR 2 (OAuth callback hygiene, merged as `8bde7e5e`) are both in `dev`; this plan was verified against `dev` at **`ca24e614`** — every file:line below refers to that commit.

## Global Constraints

- **Branch:** `feat/auth-password-login-toggle`, created from `dev` = `ca24e614`. PR 3 targets `dev`. PR 4 (client OAuth login) branches from `dev` after PR 3 merges.
- **Two accessors, one override.** `PasswordLoginEnabled(ctx, audience) (bool, error)` is the persisted policy: absent key in an existing auth document → `(true, nil)` (G6); missing document, repository failure, malformed/empty present value, nil service/reader, unknown audience → error wrapping `ErrAuthPolicyUnavailable`. `PasswordLoginDecision(ctx, audience) (PasswordAuthDecision, error)` may additionally apply the boot-time break-glass — **operator audience only**, converting stored `false` **or a read failure** into `{Allowed:true, BreakGlassUsed:true}`. No other code path may consult the override: registration, forgot-password, admin resets, password-confirm, auth-method views and unlink protection read `PasswordLoginEnabled` and stay closed under break-glass (§4.2).
- **Fail closed, never open (G4).** A nil policy, nil ConfigService, read failure or parse failure on the new keys is `ErrAuthPolicyUnavailable` → **503 `auth.policy_unavailable`** — never a silent `true`, never a consumed challenge (the MFA continuation retains the challenge on 503 so a transient outage is retryable inside the original 5-minute TTL). `LoginAllowed`, `RegistrationAllowed` and the other existing toggles keep their current permissive reads — hardening them is a named follow-up (§8), not this PR.
- **Gate placement (§4.3).** The `Login` check sits immediately after the `LoginAllowed` kill switch and **before `GetUserForAuth`**: rate-limiter buckets, failed-login counters and the audit trail see nothing, and every email receives the identical response. `Register` keeps the two **operator-only** bootstrap exceptions (first-user branch, `RegisterInitialAdmin`) and the Tier-2 route never gains one. `ForgotPassword` evaluates policy before the user lookup and its handler propagates ONLY `ErrPasswordLoginDisabled` / `ErrAuthPolicyUnavailable` — every account-specific outcome stays swallowed (enumeration posture unchanged). `reset-password`, `accept-invite`, `verify-email`, `resend`, `change-password` stay open.
- **Wire contract:** 403 `auth.password_login_disabled` on the three self-service routes and password-sourced MFA completions; **409** with the same code on the two admin send-password-reset routes; 422 `auth.login_method_lockout` from the validator; 503 `auth.policy_unavailable` for policy uncertainty. Codes are `errcode` constants, snapshotted in `goldenCodes` (`internal/shared/errcode/codes_test.go`) in the same commit that declares them.
- **The invariant, verbatim (§4.4), per surface `S ∈ {Admin, Client}`:** `valid(S) := passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)` where `passwordOn(S) := strictBool(Values["passwordLoginEnabled"+S], default true)`, `providerOn(p,S) := strictBool(Values[p+"Enabled"+S], default false)`, `structural(p)` is PR 2's exported `services.ProviderStructurallyConfigured` over the snapshot's `EffectiveValues` + `SecretPresent` (Apple: team+key non-empty ∧ (inline-PEM presence ∨ readable non-empty key file via `services.ReadableNonEmptyFile`)), `oauthOn(S) := ∃p: providerOn(p,S) ∧ structural(p)`, `autoLink := strictBool(Values["oauthAutoLinkByEmail"], default true)`. It runs on **all three mutation surfaces** through PR 1's `HasConfigSnapshotValidator` dispatch — active PATCH, named-environment PATCH, activation — always judging the target profile's own values and secret presence (§4.5). A malformed present boolean among the eleven keys is a 422 naming that key; the cross-field lockout failure names `passwordLoginEnabled<S>` and carries `Code: errcode.AuthLoginMethodLockout` with both exits in the message. The existing duration bounds move into the same snapshot validator unchanged.
- **The break-glass is narrow (§4.2).** Env var read once at boot into `cfg.Auth.OperatorPasswordLoginBreakGlass`; a WARN logs at boot while set; `docker/.env.example` documents the procedure (set → restart → log in → repair OAuth → unset → restart) and all three compose files enumerate the variable. It never bypasses `loginEnabledAdmin`, MFA, low-risk or RBAC gates on the repair itself. `auth.policy.break_glass_used` is emitted (best-effort, via the password service's existing nil-guarded `emitAudit`) on a direct full-token rescued login and once by the winning MFA/WebAuthn completion when either the initial check or the completion decision used the override; it carries audience, user UUID, session ID and source IP — never a password, token or full email.
- **Challenges are re-checked at completion (§4.3).** `MFAChallenge` gains `Audience` + `BreakGlassUsed`; both are stamped by every `BeginLogin` caller. Both completion handlers re-evaluate `PasswordLoginDecision` for password-sourced challenges (LoginMethod `"password"` or SourceAMR containing `"pwd"`) BEFORE verifying the factor: disabled → atomic `Consume` + 403; policy unavailable (no operator break-glass) → 503 **without consuming**; empty/unknown audience → consumed + 401 (pre-v3 in-flight challenge; rollout waits one 5-minute TTL before exposing the switch). OAuth-sourced challenges are untouched.
- **Step-up (§4.6).** `middleware.StepUpPolicy` gains `PasswordReauthAllowed(ctx, audience string) (bool, error)`; `dispatchStepUpFailure` treats `false` like `roleRequiresMFA` (→ `mfa_enrollment_required`) and an error **or a missing StepUpPolicy** as 503 `auth.policy_unavailable` — never a fabricated enrollment requirement. `ConfirmPasswordWithSecurity` refuses with the existing `ErrPasswordConfirmUnavailable` (409) when the persisted method is off for its audience — same branch as "no password hash"; break-glass is ignored; a policy error is 503.
- **Unlink counts usable links (§4.7).** `wouldLockOutOAuthUnlink` gains `passwordUsable bool` + `usableProviders map[iface.OAuthProvider]bool`; `locked := targetUsable ∧ (¬passwordUsable ∨ hash=="") ∧ remainingUsable==0`; removing a disabled/unconfigured target link is allowed. Callers precompute the usability map for every active link through the `SetProviderUsability` seam (wired in `module.go` from PR 2's `OAuthWebProviderUsable`, per bundle audience) and refuse with 503 on any config/decrypt/parse uncertainty rather than counting an uncertain link.
- **View split (§4.8).** `AuthMethodsView` gains `hasPasswordSet` (hash present) + `passwordUsableForLogin` (hash present ∧ method on for this user's surface); `hasUsablePassword` stays for one release as a deprecated alias of `hasPasswordSet`. In-tree consumers migrate in this PR: unlink decisions → `passwordUsableForLogin`; set/change/last-updated UI → `hasPasswordSet`. Policy failure → 503, never a guess.
- **`/policy` (§4.9).** `passwordLoginEnabled *bool` (persisted state; null only in the operator emergency case) + `passwordLoginBreakGlassEffective bool` (operator endpoint only; true only when the env var is set AND the persisted result is false/unavailable; always false on the client endpoint). Read error without break-glass → 503. OpenAPI regenerated in the same task (`make -C /home/tore/orkestra/backend openapi-dump`); `openapi-check` gates it.
- **No secret, token, password or full email** in any log line, audit event, error message or response introduced by this PR. WARNs and validation errors name keys, never values.
- **SDK self-containment:** the only `pkg/sdk` change is two additive `iface` error sentinels (deviation 1). No file under `backend/pkg/sdk/` may import `backend/internal/*` — verify with `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"` (doc-comment hits only) before every commit touching the SDK. The `Module` interface stays frozen (`HotReloadConfig` is already part of it; `BaseModule` defaults it to false and `AuthModule` overrides it).
- **SPA rules (frontend-admin only; `orkestra-frontend-admin` skill applies):** RTK Query only; `react-hook-form` + `yup` for forms (stack mandate — `EmailPasswordForm` migrates as it is reworked, deviation 8); every string through `t()` with EN **and** IT keys (parity test enforces); path aliases without `@/`; react-router 8 (`react-router`). The SPA hides password UI on persisted false/null instead of showing a 403 on submit (G5); the ONLY visible password form under persisted-off is the operator emergency form while `passwordLoginBreakGlassEffective` is true, clearly labelled, with forgot-password + register CTAs hidden. On an ordinary `/policy` failure the SPAs keep their fail-open display fallback (the backend still refuses — §4.9).
- **Docs move in the same commit as the code** (`feedback_commit_doc_hygiene`): `backend/internal/core/auth/CLAUDE.md`, `backend/pkg/sdk/CLAUDE.md` + `docs/site/sdk/shared-iface.mdx` (the two new sentinels), `docs/site/modules/core/auth.mdx` (63 → 65 fields + "SSO-only surface"), `docs/site/operating/oauth-providers.mdx` ("Going SSO-only"), `docs/site/architecture/authentication-flow.mdx`, `docker/.env.example`, the `StepUpPolicy` doc comment in `backend/internal/shared/middleware/auth.go`, `frontend-admin/CLAUDE.md` — The mapping is explicit: Task 1 → `backend/pkg/sdk/CLAUDE.md` + `docs/site/sdk/shared-iface.mdx` + `docker/.env.example`; Tasks 2–7 → the `backend/internal/core/auth/CLAUDE.md` rows for what each changes; Task 5 → the `StepUpPolicy` doc comment; Task 7 → `backend/openapi/enterprise.json`; Task 8 → `frontend-admin/CLAUDE.md` (its authentication-components paragraph describes exactly the components Task 8 reworks); Task 9's field migration is not described by any standing doc; Task 10 is the cross-cutting VERIFICATION sweep — it completes and reconciles, it is never the first touch.
- **Test commands** (absolute paths — `cd` drifts the shell between calls): backend `go test ./internal/core/auth/... ./internal/shared/... ./pkg/sdk/... ./internal/core/user/... -count=1` run from `/home/tore/orkestra/backend` after every step; `go vet ./...` before every commit (a bare `go build` does not compile `_test.go`); full gate `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true' make -C /home/tore/orkestra ci-backend` (0 SKIP with the `ork-errquality-ci-mongo` helper up). Frontend: `cd /home/tore/orkestra/frontend-admin && npx vitest run src/components/authentication src/pages/user src/pages/admin/user-profile && npm run typecheck && npm run lint`; full gate `make -C /home/tore/orkestra ci-frontend-admin`. OpenAPI: `make -C /home/tore/orkestra/backend openapi-dump` (self-configures from `docker/.env` against the staging infra on `localhost:27017/6379` — `grep "^ENV=" /home/tore/orkestra/docker/.env` first; the local stack is `orkestra-public-*-staging`). Docs render: fresh clone of `orkestra-docs`, `npm ci`, `MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync` (**full** sync, not `sync:site`), `CI=true npm run build`.
- **Never start servers manually**; never `git push --tags`; never `--amend`; stage by path, never `git add -A`; conventional-commit subjects (the `conventional-pre-commit` hook rejects anything else). **Every commit carries the `Claude-Session:` trailer**: once per shell run `export CLAUDE_SESSION=<your session id>` (it is in your task brief / harness environment) — the commit commands below all pass it as a second `-m`.
- **errquality (CI):** no `err.Error()` as a client-facing detail, no detail that merely repeats the status, no 4xx from the `default:` branch of an `errors.Is` switch. Every new mapping case is an explicit `errors.Is`.

## Findings against `ca24e614` that spec v4.4 does not state

Each is folded into a numbered deviation below where it changes the design; none contradicts the spec's contracts.

- **F1 — The "client-user twin" of send-password-reset lives in the `user` module, behind a message-matched error boundary.** `POST /v1/admin/client-users/{id}/send-password-reset` (`internal/core/user/routes.go:176-183`) resolves the client-tier password service through `iface.AdminAuthInviter` (`module.ServiceClientPasswordAuthService`, `admin_client_handler.go:425-427`) — so gating `AdminTriggerPasswordReset` at the service covers both routes with one check. But its error mapper `mapInviteErr` (`admin_client_handler.go:575-593`) cannot import `auth/services` and today matches `err.Error()` by **exact string equality** ("notifications disabled — cannot send email"); the auth module's own `mapAdminInviterError` (`admin_user_auth_handler.go:206-217`) does the same for "user not found". Exact-match breaks on the *wrapped* errors the strict policy accessors return (`auth policy unavailable: read …: …`). Deviation 1 gives both mappers a real error identity.
- **F2 — The admin auth-methods surface exists for operator users only.** `GET /v1/admin/users/{userId}/auth-methods` is wired to the operator bundle (`auth/module.go:1138`); client users have list/create/invite/reset actions from the `user` module but no auth-methods aggregator and no `AdminAuthMethodsCard`. §4.8's frontend migration therefore touches the operator card only; the client-users table's send-reset action simply starts receiving the new 409 (its existing toast pipeline renders the backend detail).
- **F3 — The completion handlers verify the factor before consuming, and `WebAuthnHandler` has no policy seam.** `MFAHandler.LoginVerify` (`mfa_handler.go:467-548`) Peeks → verifies → Consumes; `WebAuthnHandler.LoginFinish` (`webauthn_handler.go:366-441`) Peeks → FinishAssertion → Consumes. `MFAHandler` already carries `policy` via `SetPolicy` (`mfa_handler.go:103-106`); `WebAuthnHandler` carries none and gains an identical setter. The re-check runs before factor verification (deviation 6).
- **F4 — A missing `StepUpPolicy` currently yields `password_confirm_required`.** With `stepUpPolicy == nil`, `roleRequiresMFA` returns false (`middleware/auth.go:1010-1012`) and `dispatchStepUpFailure` falls through to the password-confirm envelope. Under §4.6 that terminal branch requires a policy answer, so a nil policy becomes 503 `auth.policy_unavailable`; in-tree tests that reach the no-factor branch without `SetStepUpPolicy` change outcome and are updated (Task 5). Production wiring already exists (`cmd/server/main.go:359-361`).
- **F5 — `GetAuthPolicy` is infallible today.** Every field comes from nil-safe permissive accessors (`auth_handler.go:362-379`). The new pair makes the endpoint fallible (503 on read error without operator break-glass); the admin SPA's `getAuthPolicy` queryFn already substitutes a fail-open fallback on ANY error (`authApi.ts:219-231`), so the display contract of §4.9/§5 #15 holds without SPA changes beyond the new fallback fields.
- **F6 — Challenge provenance is already consistent.** The password path stamps `LoginMethod:"password"` + `SourceAMR:["pwd"]` (`password_auth_service.go:701-714`); the OAuth path stamps `LoginMethod:"oauth"` + `SourceAMR:["oauth"]` (`auth_service.go:2249-2254`). The gate's "password-sourced" predicate (LoginMethod=="password" ∨ SourceAMR∋"pwd") is decidable for every challenge either path can mint today; the new `Audience` field removes the remaining ambiguity for post-v3 challenges, and an empty `Audience` marks a pre-v3 in-flight challenge (invalid + consumed).
- **F7 — Compose files enumerate env vars explicitly.** The backend service blocks in `docker-compose.{dev,staging,prod}.yml` list every variable they forward (e.g. `ALLOW_LOCALHOST_REDIRECTS`, `docker-compose.prod.yml:82`); a variable not listed never reaches the container, so the documented break-glass procedure would silently no-op without deviation 4. (Same finding PR 2 hit with `CLIENT_API_URL`.)

## Declared deviations — decision table (all resolved; contract rows recorded in spec v4.5 §0)

Every row is resolved: the reviewer approved the twelve deviations in plan review round 2 (2026-08-31), and the three contract-shaped readings are recorded in **spec v4.5 §0** (same branch, commit `ee95efb7`) — the spec is the contract, a plan cannot amend it. The executor re-checks this table before Task 1; a Status regression back to PENDING blocks execution.

| # | Deviation | Shape | Status |
|---|---|---|---|
| D1 | `iface.ErrPasswordLoginDisabled` / `iface.ErrAuthPolicyUnavailable` + services aliases | **Contract** (additive SDK surface; changes §4.3's declared sentinel homes) | **Approved — spec v4.5 §0** |
| D2 | `strictBool` exported as `services.StrictBool` | Implementation (in-tree visibility) | Approved (round 2) |
| D3 | Break-glass flag carried by `AuthPolicyService` (setter + display accessor) | Implementation (spec names env+config field, not the carrier) | Approved (round 2) |
| D4 | Compose files enumerate `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` | Implementation (deployment artifact; F7) | Approved (round 2) |
| D5 | `LoginTokenIssuer` gains `EmitBreakGlassUsed` | Implementation (in-tree handler interface; §4.2 fixes the event, not the seam) | Approved (round 2) |
| D6 | Completion re-check runs before factor verification | Implementation (spec fixes outcomes, not placement) | Approved (round 2) |
| D7 | `completeLogin` gains the decision param; OAuth `BeginLogin` stamps `Audience` | Implementation (delivers §4.3's required fields) | Approved (round 2) |
| D8 | `EmailPasswordForm` migrates to RHF + yup | Implementation (stack mandate; PR 2 dev-27 precedent) | Approved (round 2) |
| D9 | `SocialLoginForm.onProvidersResolved(count)` | Implementation (the seam §4.10's own table names) | Approved (round 2) |
| D10 | Malformed booleans among the eleven invariant keys rejected up-front | **Contract-adjacent** (wire-visible 422 on writes; reading of §4.4 + edge #29) | **Approved — spec v4.5 §0** |
| D11 | `Register`: nil policy = 503 for non-bootstrap signups | **Contract-adjacent** (wire-visible only during an outage; G4's own demand) | **Approved — spec v4.5 §0** |
| D12 | `/policy`'s pre-existing fields keep permissive reads | Implementation (spec §4.2 states it; restated to bound scope) | Approved (round 2) |

1. **Two error sentinels move to `pkg/sdk/iface`, next to `AdminAuthInviter`.** `iface.ErrPasswordLoginDisabled` and `iface.ErrAuthPolicyUnavailable` are declared beside the interface both admin reset routes consume (precedent: `iface.ErrKMSKeyNotFound` beside `KMSProvider`), and the services vars become aliases preserving identity: `services.ErrAuthPolicyUnavailable = iface.ErrAuthPolicyUnavailable` (re-homing PR 2's `errors.New`) and `services.ErrPasswordLoginDisabled = iface.ErrPasswordLoginDisabled` (instead of §4.3's `stderrors.New` literal). Every existing `errors.Is`/`%w` use keeps working — only the declaration site changes. Why: F1 — the user-module twin must map the sentinels across a module boundary that forbids importing `auth/services`, and message equality breaks on wrapped errors. Rejected alternative: `strings.Contains` on `err.Error()` — a copy-editing change to an error message would silently turn a 409 into a 500. Cost if wrong: two additive exported SDK vars a fork could reference.
2. **`strictBool` is exported as `services.StrictBool`.** The snapshot validator lives in package `auth` and implements the §4.4 formula; the parser it must share (PR 2's, `auth_policy_service.go:52-61`) is unexported. Straight rename + the two internal call-site updates (`OAuthAutoLinkByEmailEnabled`, `usableFromView`). Cost: none — additive visibility.
3. **The boot-time break-glass flag is carried by `AuthPolicyService`** (`SetOperatorBreakGlass(bool)` called once from auth `module.go` Init with `cfg.Auth.OperatorPasswordLoginBreakGlass`; `OperatorBreakGlassConfigured()` read by the operator `/policy` handler). The spec names the env var and the config struct field but not the carrier of the runtime flag; the policy service is where `PasswordLoginDecision` lives and both tier bundles already share the one instance. The boot WARN logs from the same Init site.
4. **The three compose files enumerate `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS`** (`${AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS:-false}` on the backend service) in the same commit as the config field — F7. `docker/.env.example` ships it commented out with the §4.2 procedure.
5. **`handlers.LoginTokenIssuer` gains `EmitBreakGlassUsed(ctx, audience, userUUID, sessionID, ip string)`.** The winning MFA/WebAuthn completion must emit `auth.policy.break_glass_used` (§4.2) but neither handler owns an audit sink; the password service behind `h.tokens` does (`emitAudit`, nil-guarded, the same seam `auth.login.succeeded` uses). Compile-enforced on the interface so a fake cannot silently drop the audit contract; the direct-login emission is the same method called from `Login`. Cost if wrong: one more method on an in-tree handler interface and its test fakes.
6. **The completion re-check runs after the challenge sanity checks (purpose, session) and before factor verification.** The spec fixes outcomes, not placement; checking before `mfa.Verify`/`FinishAssertion` keeps a disabled login from burning the challenge's attempt budget or probing factor validity, and the 403-consume still claims the challenge atomically via `Consume`. Cost if wrong: none — every specified outcome is preserved.
7. **`completeLogin` gains the decision parameter** (`completeLogin(ctx, user, in, sourceAMR, decision)`) so the partial-login challenge copies `BreakGlassUsed` and stamps `Audience` (§4.3 "MFAChallenge gains required Audience and BreakGlassUsed fields"); the OAuth-side `BeginLogin` (`auth_service.go:2249`) stamps `Audience` too, so the field is uniformly present on post-v3 challenges of both provenances. In-package signature; no SDK surface.
8. **`EmailPasswordForm` migrates to `react-hook-form` + `yup` while PR 3 reworks it** — the stack mandate beats page precedent (the form predates it), and PR 2's deviation 27 set the precedent with `MfaVerifyPanel`. Behaviour, copy and the MFA hand-off via `location.state` (never in a URL) are unchanged.
9. **`SocialLoginForm` gains `onProvidersResolved?: (count: number) => void`**, fired only when the provider query resolves (success), never on error — the seam §4.10's `Login.tsx` row names for the no-method alert without a second query.
10. **Malformed booleans among the eleven §4.4 keys are rejected up-front by the validator**, before the invariant is evaluated and regardless of which surface is off — edge #29's "the validator rejects the malformed value on the next write" and §4.4's "a malformed bool … is not silently coerced" read together. The 422 names the malformed key (no lockout code); only the cross-field failure names `passwordLoginEnabled<S>` with `Code: auth.login_method_lockout`.
11. **`Register` restructures its policy block:** operator bootstrap detection (`isFirstUser`) moves out of the `if s.policy != nil` guard, because a nil policy is now an outage (503) for every non-bootstrap signup rather than "allow everything" (G4; §4.2 "production wiring is mandatory"). The first-user branch and `RegisterInitialAdmin` remain reachable with no policy read at all, exactly as G2 names them.
12. **`/policy`'s existing fields keep their permissive reads.** Only the new pair is strict; making `registrationEnabled` etc. fallible belongs to the §8 follow-up ("fail-closed reads for the other auth toggles"), not this PR.

## File Structure

**Backend — modify:**

| File | Change |
|---|---|
| `backend/pkg/sdk/iface/interfaces.go` | two sentinels beside `AdminAuthInviter` (deviation 1) |
| `backend/internal/shared/config/config.go` | `AuthConfig.OperatorPasswordLoginBreakGlass` + env parse |
| `backend/internal/shared/errcode/codes.go` (+ `codes_test.go`) | `AuthPasswordLoginDisabled`, `AuthLoginMethodLockout` + golden rows |
| `backend/internal/core/auth/services/auth_policy_service.go` | `StrictBool` export; `PasswordLoginEnabled`, `PasswordAuthDecision`, `PasswordLoginDecision`, `SetOperatorBreakGlass`, `OperatorBreakGlassConfigured`, `PasswordReauthAllowed`; `ErrAuthPolicyUnavailable` re-homed |
| `backend/internal/core/auth/services/oauth_provider_usability.go` | `strictBool` → `StrictBool` call site |
| `backend/internal/core/auth/services/password_auth_service.go` | `ErrPasswordLoginDisabled`; gates on `Login`/`Register`/`ForgotPassword`/`AdminTriggerPasswordReset`; `completeLogin` decision param; `ConfirmPasswordWithSecurity` gate; `EmitBreakGlassUsed` |
| `backend/internal/core/auth/services/mfa_challenge_service.go` | `MFAChallenge.Audience`/`.BreakGlassUsed` + `LoginChallengeInput` twins + `BeginLogin` copy |
| `backend/internal/core/auth/services/auth_service.go` | OAuth `BeginLogin` stamps `Audience`; `SetProviderUsability` seam; `wouldLockOutOAuthUnlink` re-signed + callers; `GetUserAuthMethods` split fields |
| `backend/internal/core/auth/models/auth_methods.go` | `HasPasswordSet`, `PasswordUsableForLogin`, deprecated alias |
| `backend/internal/core/auth/config_validation.go` | full rewrite → `ValidateConfigSnapshot` (durations + §4.4 invariant) |
| `backend/internal/core/auth/module.go` | schema pair; `HotReloadConfig`; break-glass wiring + boot WARN; `SetProviderUsability` wiring (both bundles); `WebAuthnHandler.SetPolicy` wiring |
| `backend/internal/core/auth/handlers/password_handler.go` | `ForgotPassword` propagates the two sentinels; `mapPasswordError` cases |
| `backend/internal/core/auth/handlers/mfa_handler.go` | completion re-check; `LoginTokenIssuer.EmitBreakGlassUsed`; break-glass emission |
| `backend/internal/core/auth/handlers/webauthn_handler.go` | `SetPolicy`; completion re-check; emission |
| `backend/internal/core/auth/handlers/admin_user_auth_handler.go` | `mapAdminInviterError` 409/503 cases; `mapAdminUserAuthError` policy case |
| `backend/internal/core/auth/handlers/self_user_auth_handler.go` | unlink error mapping policy case |
| `backend/internal/core/auth/handlers/auth_handler.go` | `/policy` new fields + operator break-glass display flag |
| `backend/internal/core/user/handlers/admin_client_handler.go` | `mapInviteErr` 409/503 via `iface` sentinels |
| `backend/openapi/enterprise.json` | regenerated (`/policy`, auth-methods, 409s) |
| `docker/.env.example`, `docker/docker-compose.{dev,staging,prod}.yml` | break-glass variable (deviation 4) |

**Backend — new test files:** `internal/core/auth/handlers/mfa_login_verify_test.go` (completion re-check pair). Everything else extends existing suites: `services/auth_policy_service_test.go`, `services/gates_test.go` (+ `gates_fakes_test.go`), `config_validation_test.go`, `services/password_confirm_test.go`, `services/auth_service_admin_unlink_test.go`, `services/auth_service_self_unlink_test.go`, `services/auth_service_get_methods_test.go`, `shared/middleware/step_up_test.go`, `handlers/error_mapping_test.go`, `internal/core/user/handlers` (client twin mapping), `shared/errcode/codes_test.go`, plus new `/policy` handler tests in `handlers/auth_policy_endpoint_test.go`.

**Frontend-admin — modify:** `src/store/api/authApi.ts`, `src/store/api/userApi.ts`, `src/components/authentication/{EmailPasswordForm,RegisterForm,ForgotPasswordForm,Login,SocialLoginForm}.tsx`, `src/pages/user/security/{PasswordTab,LinkedProvidersTab}.tsx`, `src/pages/user/settings/SecuritySummaryCard.tsx`, `src/pages/admin/user-profile/AdminAuthMethodsCard.tsx`, `src/locales/{en,it}.json`. Also `src/test/handlers.ts` and `frontend-admin/CLAUDE.md`. Tests: `EmailPasswordForm.test.tsx` + `SocialLoginForm.test.tsx` extended; new `Login.test.tsx`, `RegisterForm.test.tsx`, `ForgotPasswordForm.test.tsx`, `PasswordTab.test.tsx`, `LinkedProvidersTab.test.tsx`, `SecuritySummaryCard.test.tsx`, `AdminAuthMethodsCard.test.tsx`.

**Docs:** `backend/internal/core/auth/CLAUDE.md`, `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/shared-iface.mdx`, `docs/site/modules/core/auth.mdx`, `docs/site/operating/oauth-providers.mdx`, `docs/site/architecture/authentication-flow.mdx`, `frontend-admin/CLAUDE.md`.

---

### Task 1: Strict policy pair + break-glass plumbing end-to-end

The foundation every later task consumes: the two sentinels with cross-module identity, the strict `PasswordLoginEnabled` / `PasswordLoginDecision` pair on `AuthPolicyService`, the boot-time break-glass flag from env to policy service (with WARN + audit constants), and the two new error codes.

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go:220-232` (sentinels beside `AdminAuthInviter`)
- Modify: `backend/internal/core/auth/services/auth_policy_service.go` (accessors; `StrictBool`; re-homed sentinel)
- Modify: `backend/internal/core/auth/services/oauth_provider_usability.go:186` (`strictBool` → `StrictBool`)
- Modify: `backend/internal/core/auth/services/password_auth_service.go:26-46` (sentinel alias in the var block)
- Modify: `backend/internal/shared/errcode/codes.go` + `codes_test.go`
- Modify: `backend/internal/shared/config/config.go:130-138` + the `AuthConfig` literal (`:347` area)
- Modify: `backend/internal/core/auth/module.go:980` area (break-glass wiring + boot WARN)
- Modify: `docker/.env.example`, `docker/docker-compose.dev.yml`, `docker/docker-compose.staging.yml`, `docker/docker-compose.prod.yml`
- Test: `backend/internal/core/auth/services/auth_policy_service_test.go` (extend)

**Interfaces:**
- Consumes: PR 1's `configValueReader.GetRawValueRequiredModule` (already on the interface, `auth_policy_service.go:114-120`), PR 2's `strictBool` (`:52-61`) and `ErrAuthPolicyUnavailable` (`:44`).
- Produces (used by Tasks 2–9):
  - `iface.ErrPasswordLoginDisabled`, `iface.ErrAuthPolicyUnavailable` (`errors.New` vars)
  - `services.ErrPasswordLoginDisabled = iface.ErrPasswordLoginDisabled` (in `password_auth_service.go`'s var block), `services.ErrAuthPolicyUnavailable = iface.ErrAuthPolicyUnavailable`
  - `services.StrictBool(raw string) (bool, error)`
  - `func (s *AuthPolicyService) PasswordLoginEnabled(ctx context.Context, audience PolicyAudience) (bool, error)`
  - `type PasswordAuthDecision struct { Allowed, BreakGlassUsed bool }`
  - `func (s *AuthPolicyService) PasswordLoginDecision(ctx context.Context, audience PolicyAudience) (PasswordAuthDecision, error)`
  - `func (s *AuthPolicyService) SetOperatorBreakGlass(active bool)` / `OperatorBreakGlassConfigured() bool` (nil-safe)
  - `errcode.AuthPasswordLoginDisabled = "auth.password_login_disabled"`, `errcode.AuthLoginMethodLockout = "auth.login_method_lockout"`
  - `config.AuthConfig.OperatorPasswordLoginBreakGlass bool` (env `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS`)

- [ ] **Step 1: Write the failing accessor tests**

Append to `backend/internal/core/auth/services/auth_policy_service_test.go` (the file already provides `stubReader` — `values` map, `rawErr`, `requiredMissing` — and `newPolicy`):

```go
// --- PR 3: strict per-surface password-login policy (spec §4.2) ---

func TestPasswordLoginEnabled_Strict(t *testing.T) {
	ctx := context.Background()
	repoErr := errors.New("mongo down")
	cases := []struct {
		name     string
		policy   *AuthPolicyService
		audience PolicyAudience
		want     bool
		wantErr  bool
	}{
		// Compatibility applies to the KEY, not the document: an absent key
		// in an existing auth document means legacy true (G6).
		{"absent key means true (operator)", newPolicy(map[string]string{}), PolicyAudienceOperator, true, false},
		{"absent key means true (client)", newPolicy(map[string]string{}), PolicyAudienceClient, true, false},
		{"explicit true", newPolicy(map[string]string{"passwordLoginEnabledAdmin": "true"}), PolicyAudienceOperator, true, false},
		{"explicit false", newPolicy(map[string]string{"passwordLoginEnabledAdmin": "false"}), PolicyAudienceOperator, false, false},
		{"canonical is case-insensitive and trimmed", newPolicy(map[string]string{"passwordLoginEnabledClient": "  False "}), PolicyAudienceClient, false, false},
		// Audience isolation: the client key never answers an operator read.
		{"audience isolation", newPolicy(map[string]string{"passwordLoginEnabledClient": "false"}), PolicyAudienceOperator, true, false},
		// Strictness: a PRESENT malformed or empty value is an outage, not a default.
		{"malformed present value", newPolicy(map[string]string{"passwordLoginEnabledAdmin": "treu"}), PolicyAudienceOperator, false, true},
		{"readBool truthiness is rejected", newPolicy(map[string]string{"passwordLoginEnabledAdmin": "1"}), PolicyAudienceOperator, false, true},
		{"empty present value", newPolicy(map[string]string{"passwordLoginEnabledAdmin": ""}), PolicyAudienceOperator, false, true},
		// Outages: missing document, read failure, nil wiring, unknown audience.
		{"missing auth document", &AuthPolicyService{cs: &stubReader{requiredMissing: true}}, PolicyAudienceOperator, false, true},
		{"repository failure", &AuthPolicyService{cs: &stubReader{rawErr: repoErr}}, PolicyAudienceOperator, false, true},
		{"nil service", nil, PolicyAudienceOperator, false, true},
		{"nil reader", &AuthPolicyService{}, PolicyAudienceOperator, false, true},
		{"unknown audience", newPolicy(map[string]string{}), PolicyAudience("service"), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.policy.PasswordLoginEnabled(ctx, tc.audience)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got (%v, nil)", got)
				}
				if !errors.Is(err, ErrAuthPolicyUnavailable) {
					t.Fatalf("error must wrap ErrAuthPolicyUnavailable, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// The sentinel must keep one identity across the iface boundary — the
// user module's client-user reset twin matches it with errors.Is.
func TestAuthPolicySentinels_IfaceIdentity(t *testing.T) {
	if !errors.Is(ErrAuthPolicyUnavailable, iface.ErrAuthPolicyUnavailable) {
		t.Fatal("services.ErrAuthPolicyUnavailable must BE iface.ErrAuthPolicyUnavailable")
	}
	if !errors.Is(ErrPasswordLoginDisabled, iface.ErrPasswordLoginDisabled) {
		t.Fatal("services.ErrPasswordLoginDisabled must BE iface.ErrPasswordLoginDisabled")
	}
}

func TestPasswordLoginDecision_BreakGlassIsOperatorOnly(t *testing.T) {
	ctx := context.Background()
	offBoth := map[string]string{
		"passwordLoginEnabledAdmin":  "false",
		"passwordLoginEnabledClient": "false",
	}

	t.Run("stored true is an ordinary allow, no break-glass claimed", func(t *testing.T) {
		p := newPolicy(map[string]string{"passwordLoginEnabledAdmin": "true"})
		p.SetOperatorBreakGlass(true)
		d, err := p.PasswordLoginDecision(ctx, PolicyAudienceOperator)
		if err != nil || !d.Allowed || d.BreakGlassUsed {
			t.Fatalf("want ordinary allow, got (%+v, %v)", d, err)
		}
	})
	t.Run("stored false without override stays false", func(t *testing.T) {
		p := newPolicy(offBoth)
		d, err := p.PasswordLoginDecision(ctx, PolicyAudienceOperator)
		if err != nil || d.Allowed || d.BreakGlassUsed {
			t.Fatalf("want plain deny, got (%+v, %v)", d, err)
		}
	})
	t.Run("override converts stored false, operator only", func(t *testing.T) {
		p := newPolicy(offBoth)
		p.SetOperatorBreakGlass(true)
		d, err := p.PasswordLoginDecision(ctx, PolicyAudienceOperator)
		if err != nil || !d.Allowed || !d.BreakGlassUsed {
			t.Fatalf("operator: want rescued allow, got (%+v, %v)", d, err)
		}
		cd, err := p.PasswordLoginDecision(ctx, PolicyAudienceClient)
		if err != nil || cd.Allowed || cd.BreakGlassUsed {
			t.Fatalf("client must never see the override, got (%+v, %v)", cd, err)
		}
	})
	t.Run("override converts a read failure, operator only", func(t *testing.T) {
		p := &AuthPolicyService{cs: &stubReader{rawErr: errors.New("mongo down")}}
		p.SetOperatorBreakGlass(true)
		d, err := p.PasswordLoginDecision(ctx, PolicyAudienceOperator)
		if err != nil || !d.Allowed || !d.BreakGlassUsed {
			t.Fatalf("operator outage under break-glass must rescue, got (%+v, %v)", d, err)
		}
		if _, err := p.PasswordLoginDecision(ctx, PolicyAudienceClient); err == nil {
			t.Fatal("client outage must stay an error under break-glass")
		}
	})
	t.Run("outage without override stays an error — never fails open", func(t *testing.T) {
		p := &AuthPolicyService{cs: &stubReader{requiredMissing: true}}
		if _, err := p.PasswordLoginDecision(ctx, PolicyAudienceOperator); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("nil service cannot be rescued", func(t *testing.T) {
		var p *AuthPolicyService
		if _, err := p.PasswordLoginDecision(ctx, PolicyAudienceOperator); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("OperatorBreakGlassConfigured is nil-safe and reports the flag", func(t *testing.T) {
		var pnil *AuthPolicyService
		if pnil.OperatorBreakGlassConfigured() {
			t.Fatal("nil service must report false")
		}
		p := newPolicy(nil)
		if p.OperatorBreakGlassConfigured() {
			t.Fatal("default must be false")
		}
		p.SetOperatorBreakGlass(true)
		if !p.OperatorBreakGlassConfigured() {
			t.Fatal("flag must latch")
		}
	})
}
```

- [ ] **Step 2: Run to verify the tests fail**

Run from `/home/tore/orkestra/backend`: `go test ./internal/core/auth/services/ -run 'TestPasswordLogin|TestAuthPolicySentinels' -count=1`
Expected: compile FAILURE — `PasswordLoginEnabled`, `PasswordLoginDecision`, `SetOperatorBreakGlass`, `iface.ErrAuthPolicyUnavailable`, `ErrPasswordLoginDisabled` undefined.

- [ ] **Step 3: Declare the iface sentinels**

In `backend/pkg/sdk/iface/interfaces.go`, directly below the `AdminAuthInviter` interface (line 232):

```go
// ErrPasswordLoginDisabled reports that the email/password method is
// administratively disabled for the surface the target user signs in on
// (auth module keys passwordLoginEnabled{Admin,Client}). Declared here —
// not in the auth module — because AdminAuthInviter consumers in other
// modules must map it across the module boundary with errors.Is: the
// admin send-password-reset routes answer 409 with it. The auth module's
// services package aliases it, so both names are one identity.
var ErrPasswordLoginDisabled = errors.New("password login disabled for this surface")

// ErrAuthPolicyUnavailable reports that a persisted sign-in policy — or
// the auth config document it lives in — could not be read or parsed.
// The decision fails closed (503 auth.policy_unavailable), never open.
// Shares this file for the same reason as ErrPasswordLoginDisabled:
// AdminAuthInviter consumers map it with errors.Is.
var ErrAuthPolicyUnavailable = errors.New("auth policy unavailable")
```

(`errors` is already imported — `interfaces.go:575` uses `errors.New`.)

- [ ] **Step 4: Re-home the services sentinels and export the parser**

In `backend/internal/core/auth/services/auth_policy_service.go` replace the PR 2 declaration block (`:39-61`):

```go
// ErrAuthPolicyUnavailable is returned by every STRICT policy accessor when
// the answer cannot be established: nil service, nil reader, missing auth
// document, repository failure, or a present value that is not a canonical
// boolean. Callers map it to 503 auth.policy_unavailable and never
// substitute a default — an outage must not re-enable anything.
//
// It IS iface.ErrAuthPolicyUnavailable: the admin send-password-reset twin
// in the user module matches the sentinel across the module boundary with
// errors.Is, so both packages must share one identity.
var ErrAuthPolicyUnavailable = iface.ErrAuthPolicyUnavailable

// StrictBool accepts only canonical, case-insensitive "true" / "false" after
// trimming. It deliberately does NOT accept readBool's "1"/"yes": an
// out-of-band "treu" or "" must surface as an error, never as a default.
// Exported because the auth module's snapshot validator implements the
// §4.4 invariant with the same parser the runtime accessors use.
func StrictBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("not a canonical boolean")
}
```

Update the two internal call sites: `OAuthAutoLinkByEmailEnabled` (`auth_policy_service.go:713`) and `usableFromView` (`oauth_provider_usability.go:186`) call `StrictBool(raw)`.

In `backend/internal/core/auth/services/password_auth_service.go`, add to the sentinel var block (`:26-35`):

```go
	// ErrPasswordLoginDisabled is iface.ErrPasswordLoginDisabled (one
	// identity across the AdminAuthInviter boundary); the per-surface
	// method gates of spec §4.3 return it.
	ErrPasswordLoginDisabled = iface.ErrPasswordLoginDisabled
```

- [ ] **Step 5: Implement the accessors and break-glass state**

In `auth_policy_service.go`: add the field to the struct (`:127-129`):

```go
type AuthPolicyService struct {
	cs configValueReader
	// operatorBreakGlass mirrors AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS,
	// read once at boot and handed here by auth module Init. Consulted
	// ONLY by PasswordLoginDecision (operator audience) and, as a display
	// flag, by the operator /policy endpoint. Never by PasswordLoginEnabled.
	operatorBreakGlass bool
}
```

Then, next to `OAuthAutoLinkByEmailEnabled` (after `:718`):

```go
// SetOperatorBreakGlass records the boot-time emergency override. Called
// once from auth module Init before traffic; there is no unset path short
// of a restart, matching the env var's boot-time semantics.
func (s *AuthPolicyService) SetOperatorBreakGlass(active bool) {
	if s == nil {
		return
	}
	s.operatorBreakGlass = active
}

// OperatorBreakGlassConfigured reports whether the boot-time override is
// set. Display-flag input for the operator /policy endpoint (§4.9) — it
// says nothing about whether the override was ever NEEDED.
func (s *AuthPolicyService) OperatorBreakGlassConfigured() bool {
	return s != nil && s.operatorBreakGlass
}

// passwordLoginKeyFor maps the audience to its schema key. Unknown
// audiences are an error — a policy read must never guess a surface.
func passwordLoginKeyFor(audience PolicyAudience) (string, error) {
	switch audience {
	case PolicyAudienceOperator:
		return "passwordLoginEnabledAdmin", nil
	case PolicyAudienceClient:
		return "passwordLoginEnabledClient", nil
	}
	return "", fmt.Errorf("%w: unknown policy audience %q", ErrAuthPolicyUnavailable, string(audience))
}

// PasswordLoginEnabled returns the persisted per-surface method policy
// (spec §4.2). Compatibility applies to the key, not to the document: an
// absent key in an existing auth document means true; a missing document,
// malformed value, invalid audience or unavailable reader returns an error
// wrapping ErrAuthPolicyUnavailable. It never sees the break-glass:
// registration/reset gates, auth-method views and unlink protection must
// treat the override as invisible.
func (s *AuthPolicyService) PasswordLoginEnabled(ctx context.Context, audience PolicyAudience) (bool, error) {
	if s == nil || s.cs == nil {
		return false, fmt.Errorf("%w: policy service not wired", ErrAuthPolicyUnavailable)
	}
	key, err := passwordLoginKeyFor(audience)
	if err != nil {
		return false, err
	}
	raw, present, err := s.cs.GetRawValueRequiredModule(ctx, "auth", key)
	if err != nil {
		return false, fmt.Errorf("%w: read %s: %w", ErrAuthPolicyUnavailable, key, err)
	}
	if !present {
		return true, nil
	}
	v, err := StrictBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s is not a canonical boolean", ErrAuthPolicyUnavailable, key)
	}
	return v, nil
}

// PasswordAuthDecision is PasswordLoginDecision's answer: whether the
// password may authenticate now, and whether the boot-time break-glass —
// not the persisted policy — is what allowed it (audit context).
type PasswordAuthDecision struct {
	Allowed        bool
	BreakGlassUsed bool
}

// PasswordLoginDecision is used ONLY by Login and by completion of the
// MFA/WebAuthn challenge it created (spec §4.2). It first evaluates the
// persisted policy; a stored true is an ordinary allow. For the operator
// audience only, the boot-time override converts a stored false OR a
// policy read/parse failure into {Allowed:true, BreakGlassUsed:true} —
// so an outage never silently fails open, while the recovery switch still
// works when ConfigService itself is the outage. It does not consult the
// separate loginEnabledAdmin maintenance switch.
func (s *AuthPolicyService) PasswordLoginDecision(ctx context.Context, audience PolicyAudience) (PasswordAuthDecision, error) {
	allowed, err := s.PasswordLoginEnabled(ctx, audience)
	if err == nil && allowed {
		return PasswordAuthDecision{Allowed: true}, nil
	}
	if audience == PolicyAudienceOperator && s.OperatorBreakGlassConfigured() {
		return PasswordAuthDecision{Allowed: true, BreakGlassUsed: true}, nil
	}
	if err != nil {
		return PasswordAuthDecision{}, err
	}
	return PasswordAuthDecision{Allowed: false}, nil
}
```

- [ ] **Step 6: Run the accessor tests**

Run: `go test ./internal/core/auth/services/ -run 'TestPasswordLogin|TestAuthPolicySentinels|TestOAuthAutoLink' -count=1`
Expected: PASS (including the pre-existing strict auto-link suite over the renamed parser).

- [ ] **Step 7: Error codes + golden snapshot**

In `backend/internal/shared/errcode/codes.go`, after `AuthOAuthEmailUnverified` (`:79`):

```go
// AuthPasswordLoginDisabled signals that the email/password method is
// administratively disabled for the surface the request came in on
// (auth module keys passwordLoginEnabled{Admin,Client}). 403 on the
// self-service routes (login, register, forgot-password, password-
// sourced MFA completion); 409 on the two admin send-password-reset
// routes, where the operator asked to mint a reset for a method the
// target's surface refuses.
const AuthPasswordLoginDisabled = "auth.password_login_disabled"

// AuthLoginMethodLockout signals that a module-config mutation would
// leave a surface with no way to authenticate: password off with no
// structurally configured OAuth provider, or with verified-email
// auto-link off. Emitted by the auth module's snapshot validator on
// every mutation surface. 422.
const AuthLoginMethodLockout = "auth.login_method_lockout"
```

In `codes_test.go` add both rows to `goldenCodes`:

```go
	"AuthPasswordLoginDisabled":          "auth.password_login_disabled",
	"AuthLoginMethodLockout":             "auth.login_method_lockout",
```

Run: `go test ./internal/shared/errcode/ -count=1` — expected PASS (`TestEveryConstSnapshotted` would fail without the rows).

- [ ] **Step 8: Config field, env parse, boot wiring, compose + .env.example**

`backend/internal/shared/config/config.go` — add to `AuthConfig` (`:137`):

```go
	AllowLocalhostRedirects bool // Allow localhost OAuth redirects (should be false in production)
	// OperatorPasswordLoginBreakGlass mirrors AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS:
	// a boot-time, operator-login-only override of passwordLoginEnabledAdmin
	// (spec §4.2). It never opens client login, registration, resets,
	// password-confirm or unlink decisions, and never bypasses the
	// loginEnabledAdmin maintenance switch or the MFA/low-risk/RBAC gates
	// on the subsequent config repair.
	OperatorPasswordLoginBreakGlass bool
```

and to the literal (`:347`):

```go
		AllowLocalhostRedirects:         getEnvAsBool("ALLOW_LOCALHOST_REDIRECTS", true), // Default true for development
		OperatorPasswordLoginBreakGlass: getEnvAsBool("AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS", false),
```

`backend/internal/core/auth/module.go` — immediately after `authPolicy := services.NewAuthPolicyService(deps.ConfigService)` (`:980`):

```go
	// Operator-only break-glass (spec §4.2): read once at boot, handed to
	// the ONE decision path allowed to see it. The WARN repeats on every
	// boot while the variable is set so a forgotten override stays loud.
	if cfg.Auth.OperatorPasswordLoginBreakGlass {
		authPolicy.SetOperatorBreakGlass(true)
		logger.Warn("auth: operator password-login BREAK-GLASS override is ACTIVE — " +
			"persisted policy is bypassed for operator login (and its MFA continuation) only; " +
			"repair the OAuth configuration, then unset AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS and restart")
	}
```

`docker/.env.example` — after `ALLOW_LOCALHOST_REDIRECTS` (`:158`):

```bash
# --------------------------------------------
# EMERGENCY: operator password-login break-glass (spec: password-login toggle §4.2)
# --------------------------------------------
# If the operator surface is SSO-only (passwordLoginEnabledAdmin=false) and
# OAuth breaks (wrong credential, IdP outage, lost secret), restore operator
# password login WITHOUT reopening signups, resets or client login:
#   1. set AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS=true here
#   2. restart the backend through the sanctioned stack lifecycle (orkestra.sh)
#   3. log in on the labelled emergency form (MFA still applies)
#   4. repair the OAuth provider config at /admin/modules/auth
#   5. unset the variable and restart again
# While set, the backend logs a WARN on every boot and audits every rescued
# login. It applies ONLY to POST /v1/auth/operator/login and the MFA
# continuation of a challenge that login created.
#AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS=true
```

All three compose files (`docker-compose.dev.yml`, `docker-compose.staging.yml`, `docker-compose.prod.yml`): add to the backend service's `environment:` block, next to the other auth vars:

```yaml
      AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS: ${AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS:-false}
```

- [ ] **Step 9: Same-commit docs (the SDK surface this task adds)**

`backend/pkg/sdk/CLAUDE.md` — in the iface section, one entry:

```markdown
- **Cross-module auth-policy sentinels** — `iface.ErrPasswordLoginDisabled` and
  `iface.ErrAuthPolicyUnavailable` live beside `AdminAuthInviter` because its
  consumers (the user module's client-user reset routes) must map them across
  the module boundary with `errors.Is`; message matching breaks on wrapped
  errors. `auth/services` aliases both, so each name is ONE identity. Same
  pattern as `ErrKMSKeyNotFound` beside `KMSProvider`.
```

`docs/site/sdk/shared-iface.mdx` — in the error-sentinel/reference list (match the page's existing row format):

```markdown
- `ErrPasswordLoginDisabled` — the email/password method is administratively
  disabled for the target user's surface; the admin send-password-reset routes
  answer 409 `auth.password_login_disabled` with it.
- `ErrAuthPolicyUnavailable` — a persisted sign-in policy could not be read or
  parsed; consumers fail closed with 503 `auth.policy_unavailable`.
```

- [ ] **Step 10: Full package pass + vet + commit**

Run: `go test ./internal/core/auth/... ./pkg/sdk/... ./internal/shared/... -count=1` then `go vet ./...`, and `grep -rn "internal/" pkg/sdk/ --include="*.go"` (doc-comment hits only).
Expected: PASS; no new SDK→internal imports.

```bash
git add backend/pkg/sdk/iface/interfaces.go backend/pkg/sdk/CLAUDE.md docs/site/sdk/shared-iface.mdx backend/internal/core/auth/services/auth_policy_service.go backend/internal/core/auth/services/oauth_provider_usability.go backend/internal/core/auth/services/password_auth_service.go backend/internal/core/auth/services/auth_policy_service_test.go backend/internal/shared/errcode/codes.go backend/internal/shared/errcode/codes_test.go backend/internal/shared/config/config.go backend/internal/core/auth/module.go docker/.env.example docker/docker-compose.dev.yml docker/docker-compose.staging.yml docker/docker-compose.prod.yml
git commit -m "feat(auth): strict per-surface password-login policy read with operator-only break-glass" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 2: Schema pair, `HotReloadConfig`, and the snapshot validator with the anti-lockout invariant

The two new fields, the hot-reload declaration, and the migration of every auth config rule onto PR 1's `HasConfigSnapshotValidator` — durations unchanged, plus the §4.4 login-method invariant judged from the target snapshot's own values and secret presence on all three mutation surfaces.

**Files:**
- Modify: `backend/internal/core/auth/module.go:400-404` (schema pair after `loginEnabledClient`), `:113-115` area (`HotReloadConfig`)
- Modify: `backend/internal/core/auth/config_validation.go` (full rewrite)
- Test: `backend/internal/core/auth/config_validation_test.go` (migrate + extend), `backend/internal/core/auth/config_groups_test.go` (counts 63 → 65, `login` 7 → 9)

**Interfaces:**
- Consumes: `module.ConfigValidationSnapshot{Environment, Values, EffectiveValues, SecretPresent}` + `module.HasConfigSnapshotValidator` (PR 1, `pkg/sdk/module/config_validator.go:84-103`); `module.ConfigValidationError{Field, Message, Code}`; `services.StrictBool` (Task 1); `services.ProviderStructurallyConfigured`, `services.ProviderStructuralFields`, `services.KeyFileProbe`, `services.ReadableNonEmptyFile`, `services.WebProviderOrder` (PR 2, `oauth_provider_usability.go`); `errcode.AuthLoginMethodLockout` (Task 1).
- Produces: `AuthModule.ValidateConfigSnapshot(ctx, snapshot) error` (replaces `ValidateConfig` — the SDK dispatch prefers the snapshot interface and never calls the old hook, `config_snapshot.go:220-225`); `AuthModule.HotReloadConfig() bool == true`; package-level `validateLoginMethodInvariant(snap module.ConfigValidationSnapshot, probe services.KeyFileProbe) error` (probe-injectable, tested pure).

- [ ] **Step 1: Write the failing tests**

In `backend/internal/core/auth/config_validation_test.go`:

1. Change the duration table's call site to the snapshot contract — replace lines 50-60's call with:

```go
			err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{Values: tc.values})
```

(the duration rows carry no password/provider keys, so the invariant's legacy-true defaults hold and only the duration rule can fire — the migration is behaviour-preserving by construction).

2. Replace `TestAuthModuleImplementsConfigValidator` with:

```go
// The auth module is judged through the snapshot contract on all three
// mutation surfaces; the legacy value-only hook is gone so the SDK can
// never fall back to a validator that cannot see secret presence.
func TestAuthModuleImplementsSnapshotValidator(t *testing.T) {
	var _ module.HasConfigSnapshotValidator = (*AuthModule)(nil)
	var mod interface{} = &AuthModule{}
	if _, ok := mod.(module.HasConfigValidator); ok {
		t.Fatal("AuthModule must NOT keep HasConfigValidator — the snapshot validator replaces it")
	}
	if !(&AuthModule{}).HotReloadConfig() {
		t.Fatal("auth reads config lazily at request time; HotReloadConfig must be true so successful writes persist needsRestart=false")
	}
}
```

3. Append the invariant suite:

```go
// snapFor builds a target snapshot with a structurally complete Google
// provider available for wiring into either surface. Tests override or
// delete keys per case. probe(nil) means "no readable Apple key file".
func snapFor(overrides map[string]string, secrets map[string]bool) module.ConfigValidationSnapshot {
	values := map[string]string{}
	effective := map[string]string{
		"googleClientId":    "gid.apps.example",
		"googleRedirectURL": "https://api.example.com/auth/oauth/google/callback",
	}
	present := map[string]bool{"googleClientSecret": true}
	for k, v := range overrides {
		values[k] = v
		// Non-secret overrides participate in the structural predicate via
		// EffectiveValues exactly as ConfigService merges them (§4.5).
		effective[k] = v
	}
	for k, v := range secrets {
		present[k] = v
	}
	return module.ConfigValidationSnapshot{
		Environment:     "production",
		Values:          values,
		EffectiveValues: effective,
		SecretPresent:   present,
	}
}

func TestLoginMethodInvariant(t *testing.T) {
	googleOnAdmin := map[string]string{"googleEnabledAdmin": "true"}

	cases := []struct {
		name      string
		overrides map[string]string
		secrets   map[string]bool
		probe     services.KeyFileProbe
		wantField string
		wantCode  string
	}{
		// Defaults: both password keys absent → legacy true → always valid.
		{name: "empty snapshot is valid", overrides: nil},
		// Password off with a usable provider + auto-link (default true) is the SSO-only happy path.
		{name: "admin off with usable google", overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false"})},
		// Cross-field failures name passwordLoginEnabled<S> with the lockout code (§4.4).
		{name: "admin off with no provider at all", overrides: map[string]string{"passwordLoginEnabledAdmin": "false"},
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		{name: "client off while only admin has google", overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledClient": "false"}),
			wantField: "passwordLoginEnabledClient", wantCode: errcode.AuthLoginMethodLockout},
		{name: "admin off, provider toggled but structurally incomplete (no secret)",
			overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false"}),
			secrets:   map[string]bool{"googleClientSecret": false},
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		{name: "admin off, provider structurally complete but toggle off",
			overrides: map[string]string{"passwordLoginEnabledAdmin": "false", "googleEnabledAdmin": "false"},
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		{name: "admin off, auto-link explicitly off closes the linking loop",
			overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false", "oauthAutoLinkByEmail": "false"}),
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		// Blanking a structural field of the last usable provider while password is off (§4.4 symmetric).
		{name: "admin off, redirect URL blanked",
			overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false", "googleRedirectURL": ""}),
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		// Apple structural rules: inline PEM presence or a readable key file (probe-injected).
		{name: "admin off, apple usable via key file",
			overrides: map[string]string{
				"passwordLoginEnabledAdmin": "false", "appleEnabledAdmin": "true",
				"appleClientId": "com.example.svc", "appleRedirectURL": "https://api.example.com/cb",
				"appleTeamId": "TEAM1", "appleKeyId": "KEY1", "applePrivateKeyPath": "/keys/apple.p8",
			},
			probe: func(path string) bool { return path == "/keys/apple.p8" }},
		{name: "admin off, apple key file unreadable",
			overrides: map[string]string{
				"passwordLoginEnabledAdmin": "false", "appleEnabledAdmin": "true",
				"appleClientId": "com.example.svc", "appleRedirectURL": "https://api.example.com/cb",
				"appleTeamId": "TEAM1", "appleKeyId": "KEY1", "applePrivateKeyPath": "/keys/apple.p8",
			},
			probe:     func(string) bool { return false },
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		// Malformed booleans among the eleven §4.4 keys are 422 naming THAT key,
		// up-front, regardless of surface state (deviation 10, edge #29).
		{name: "malformed password toggle", overrides: map[string]string{"passwordLoginEnabledAdmin": "treu"},
			wantField: "passwordLoginEnabledAdmin"},
		{name: "malformed provider toggle rejected even with password on",
			overrides: map[string]string{"githubEnabledClient": "treu"},
			wantField: "githubEnabledClient"},
		{name: "malformed auto-link", overrides: map[string]string{"oauthAutoLinkByEmail": "yes"},
			wantField: "oauthAutoLinkByEmail"},
		{name: "empty present password toggle is malformed", overrides: map[string]string{"passwordLoginEnabledClient": ""},
			wantField: "passwordLoginEnabledClient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := tc.probe
			if probe == nil {
				probe = func(string) bool { return false }
			}
			err := validateLoginMethodInvariant(snapFor(tc.overrides, tc.secrets), probe)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			var typed *module.ConfigValidationError
			if !errors.As(err, &typed) {
				t.Fatalf("want *ConfigValidationError, got %v", err)
			}
			if typed.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", typed.Field, tc.wantField)
			}
			if typed.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", typed.Code, tc.wantCode)
			}
			if tc.wantCode == errcode.AuthLoginMethodLockout {
				// Both exits must be named (§4.4 error shape).
				for _, needle := range []string{"password", "provider", "auto-link"} {
					if !strings.Contains(strings.ToLower(typed.Message), needle) {
						t.Errorf("message %q must name the %s exit", typed.Message, needle)
					}
				}
			}
		})
	}
}

func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
```

(Add `strings`, `github.com/orkestra/backend/internal/core/auth/services` and `github.com/orkestra/backend/internal/shared/errcode` to the test file's imports.)

4. In `config_groups_test.go`: `login` bucket 7 → 9, `len(schema)` 63 → 65, and the doc comment sentence becomes "…ADR-0017 D1 added `sessionAbsoluteTTL` to `login`, and the password-login toggle added `passwordLoginEnabled{Admin,Client}` there too, bringing the module's full field count to 65".

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/core/auth/ -run 'TestLoginMethodInvariant|TestAuthModuleImplementsSnapshotValidator|TestAuthDurationPatchValidation|TestConfigGroups' -count=1`
Expected: compile FAILURE (`ValidateConfigSnapshot`, `validateLoginMethodInvariant`, `HotReloadConfig` override undefined) — and the group-count test failing at 63.

- [ ] **Step 3: Schema pair + HotReloadConfig**

`module.go` — insert both fields verbatim from spec §4.1 immediately after the `loginEnabledClient` field (`:400-403`), before `accountLockoutThreshold`:

```go
		{
			Key: "passwordLoginEnabledAdmin", Label: "Allow email/password sign-in on operator console", Group: "login",
			Description: "When off, the operator console accepts OAuth only: new password sign-ins, signups and reset requests on /v1/auth/operator/* are refused (403 auth.password_login_disabled), in-flight password logins cannot complete, and a password no longer counts as a credential for step-up re-authentication or OAuth-unlink checks. Sessions opened before the change are not revoked. Cannot be turned off unless at least one OAuth provider is fully configured for this surface and 'Auto-link OAuth provider to existing email account' is on.",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "passwordLoginEnabledClient", Label: "Allow email/password sign-in on client app", Group: "login",
			Description: "When off, the client app accepts OAuth only: new password sign-ins, signups and reset requests on /v1/auth/client/* are refused (403 auth.password_login_disabled), in-flight password logins cannot complete, and a password no longer counts as a credential for step-up re-authentication or OAuth-unlink checks. Sessions opened before the change are not revoked. Cannot be turned off unless at least one OAuth provider is fully configured for this surface and 'Auto-link OAuth provider to existing email account' is on.",
			Type: module.FieldBool, Default: "true",
		},
```

No `EnvVar` (G6) — identical posture to `loginEnabled{Admin,Client}`. Then after `Description()` (`:114`):

```go
// HotReloadConfig declares what has always been true: AuthPolicyService and
// the OAuth resolver read module config at request time, so a successful
// config write is live immediately and must persist needsRestart=false in
// the same atomic update (spec §4.1) instead of leaving a false restart
// banner in the admin UI.
func (m *AuthModule) HotReloadConfig() bool { return true }
```

- [ ] **Step 4: Rewrite `config_validation.go`**

Replace the file's validator half (keep `durationBound` and `authDurationBounds` as they are, `:17-47`); the interface assertion, the method and the new invariant:

```go
var _ module.HasConfigSnapshotValidator = (*AuthModule)(nil)

// ValidateConfigSnapshot judges the complete target snapshot on all three
// mutation surfaces (active PATCH, named-environment PATCH, activation —
// spec §4.5): the duration bounds of ADR-0017 D6, then the login-method
// anti-lockout invariant of the password-login toggle (spec §4.4). The
// legacy value-only ValidateConfig hook is gone: the invariant depends on
// secret PRESENCE, which only the snapshot carries — no secret value
// crosses this boundary.
func (m *AuthModule) ValidateConfigSnapshot(_ context.Context, snap module.ConfigValidationSnapshot) error {
	if err := validateAuthDurations(snap.Values); err != nil {
		return err
	}
	return validateLoginMethodInvariant(snap, services.ReadableNonEmptyFile)
}

// validateAuthDurations is the ValidateConfig loop verbatim: an empty value
// is always accepted (emptiness has field-specific meaning), a present
// malformed or out-of-range duration is a 422 naming the field.
func validateAuthDurations(values map[string]string) error {
	for _, b := range authDurationBounds {
		raw, present := values[b.key]
		if !present {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		d, ok := utils.ParseDuration(raw)
		if !ok || d <= 0 {
			return &module.ConfigValidationError{
				Field:   b.key,
				Message: fmt.Sprintf("must be a positive duration such as 15m, 2h or 30d (%s)", b.why),
			}
		}
		if d < b.min || d > b.max {
			return &module.ConfigValidationError{
				Field:   b.key,
				Message: fmt.Sprintf("must be between %s and %s (%s)", b.min, b.max, b.why),
			}
		}
	}
	return nil
}

// loginMethodSurfaces are the schema-key suffixes of the two tenancy
// surfaces (§4.4: S ∈ {Admin, Client}).
var loginMethodSurfaces = []string{"Admin", "Client"}

// validateLoginMethodInvariant enforces, per surface S:
//
//	valid(S) := passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)
//
// judged from the TARGET snapshot's raw values (strict booleans, schema
// defaults for absent keys), effective values (EnvVar/default fallback for
// the structural fields) and secret presence — never from the active
// profile, never from a secret value. Malformed booleans among the eleven
// participating keys are rejected up-front naming that key (edge #29);
// only the cross-field lockout failure names passwordLoginEnabled<S> and
// carries the stable auth.login_method_lockout code.
func validateLoginMethodInvariant(snap module.ConfigValidationSnapshot, probe services.KeyFileProbe) error {
	autoLink, err := snapshotBool(snap.Values, "oauthAutoLinkByEmail", true)
	if err != nil {
		return err
	}
	// Parse all ten surface-scoped booleans first so a malformed value is
	// refused on every write, not only when its surface happens to be off.
	passwordOn := map[string]bool{}
	providerOn := map[string]map[string]bool{}
	for _, surface := range loginMethodSurfaces {
		on, err := snapshotBool(snap.Values, "passwordLoginEnabled"+surface, true)
		if err != nil {
			return err
		}
		passwordOn[surface] = on
		providerOn[surface] = map[string]bool{}
		for _, p := range services.WebProviderOrder {
			pv, err := snapshotBool(snap.Values, string(p)+"Enabled"+surface, false)
			if err != nil {
				return err
			}
			providerOn[surface][string(p)] = pv
		}
	}
	for _, surface := range loginMethodSurfaces {
		if passwordOn[surface] {
			continue
		}
		usable := false
		for _, p := range services.WebProviderOrder {
			if !providerOn[surface][string(p)] {
				continue
			}
			if _, ok := providerStructuralFromSnapshot(snap, p, probe); ok {
				usable = true
				break
			}
		}
		if usable && autoLink {
			continue
		}
		return &module.ConfigValidationError{
			Field: "passwordLoginEnabled" + surface,
			Code:  errcode.AuthLoginMethodLockout,
			Message: "turning email/password sign-in off would lock this surface out: keep the password method enabled, " +
				"or leave at least one fully configured OAuth provider enabled for this surface " +
				"(client ID, redirect URL and secret — for Apple also team ID, key ID and a private key) " +
				"together with 'Auto-link OAuth provider to existing email account'",
		}
	}
	return nil
}

// snapshotBool applies §4.4's strictBool over a raw snapshot value: absent
// key → schema default; present canonical boolean → its value; present
// malformed or empty → 422 naming the key. Never readBool.
func snapshotBool(values map[string]string, key string, def bool) (bool, error) {
	raw, present := values[key]
	if !present {
		return def, nil
	}
	v, err := services.StrictBool(raw)
	if err != nil {
		return false, &module.ConfigValidationError{
			Field:   key,
			Message: "must be exactly true or false",
		}
	}
	return v, nil
}

// providerStructuralFromSnapshot mirrors usableFromView's field mapping
// (oauth_provider_usability.go) over a VALIDATION snapshot instead of the
// active view, so the validator and the runtime agree field-for-field.
func providerStructuralFromSnapshot(snap module.ConfigValidationSnapshot, p models.OAuthProvider, probe services.KeyFileProbe) (string, bool) {
	fields := services.ProviderStructuralFields{
		ClientID:       snap.EffectiveValues[string(p)+"ClientId"],
		RedirectURL:    snap.EffectiveValues[string(p)+"RedirectURL"],
		SecretPresent:  snap.SecretPresent[string(p)+"ClientSecret"],
		TeamID:         snap.EffectiveValues["appleTeamId"],
		KeyID:          snap.EffectiveValues["appleKeyId"],
		PrivateKeyPath: snap.EffectiveValues["applePrivateKeyPath"],
	}
	if p == models.OAuthProviderApple {
		fields.SecretPresent = snap.SecretPresent["applePrivateKey"]
	}
	return services.ProviderStructurallyConfigured(p, fields, probe)
}
```

Imports for the file become: `context`, `fmt`, `strings`, `time`, `github.com/orkestra/backend/internal/core/auth/models`, `github.com/orkestra/backend/internal/core/auth/services`, `github.com/orkestra/backend/internal/shared/errcode`, `github.com/orkestra/backend/internal/shared/utils`, `github.com/orkestra/backend/pkg/sdk/module`. The old `var _ module.HasConfigValidator` assertion and the `ValidateConfig` method are DELETED.

- [ ] **Step 5: Run the task's suites**

Run: `go test ./internal/core/auth/ -count=1`
Expected: PASS — durations behave identically through the snapshot path; the invariant table passes; group counts read 65.

**Why the pure table plus the interface assertion covers all three mutation surfaces** (the three-surface claim is NOT re-proven here; it rests on this exact PR 1 chain — verify each link exists before relying on it):

1. All three exported surfaces route every mutation through `validateCandidate`: `UpdateConfig` (`pkg/sdk/module/config_service.go:776` → `:827`), `UpdateEnvironmentConfig` (`:880` → `:905`), `SetActiveEnvironment` (`:950` → `:968`).
2. `TestValidateCandidate_Dispatch` (`pkg/sdk/module/config_snapshot_test.go:193-242`) proves a `HasConfigSnapshotValidator` module is judged through the snapshot on PATCH **and** activation, sees the target environment, and its legacy hooks are never called.
3. `TestBuildValidationSnapshot_RawVersusEffective` (`:24`), `_EnvVarBeatsDefault` (`:67`), `_SecretPresence` (`:78`) and `_CorruptCiphertext` (`:121`) prove the snapshot's raw/effective/secret-presence semantics, including "a stored secret that cannot decrypt aborts the mutation".
4. `TestSnapshot_TargetSecretsNotActiveSecrets` (`pkg/sdk/module/config_service_cas_test.go:172`) and `TestUpdateConfig_ConcurrentWritersCannotSkew` (`:109`) prove, through the exported service API, that the snapshot carries the TARGET profile's secret presence and that two individually-valid snapshots cannot skew into an invalid document.
5. This task's `TestAuthModuleImplementsSnapshotValidator` closes the chain: `AuthModule` implements the snapshot seam and no longer the legacy one, so per link 2 the invariant runs on every surface.

(§6's "successful writes atomically persist needsRestart=false" is the same kind of PR 1 machinery keyed off this declaration: the registry's `SupportsHotReload` reads `HotReloadConfig()` (`pkg/sdk/module/module.go:414`, `registry.go:671`) and the CAS write persists its inverse — `TestUpdateConfig_NeedsRestartWithoutResolverOrForColdModule`, `config_service_cas_test.go:90`.)

- [ ] **Step 6: Docs touched by this task**

`backend/internal/core/auth/CLAUDE.md`: in the config/validation section, state that auth validates through `HasConfigSnapshotValidator` on all three surfaces (durations + login-method invariant), that `HotReloadConfig()` is true, and add the two keys to the Login & Sessions row with the strict-read semantics. (Full docs sweep remains Task 10; this is the same-commit minimum for the paths touched here.)

- [ ] **Step 7: Vet + commit**

Run: `go vet ./...`

```bash
git add backend/internal/core/auth/module.go backend/internal/core/auth/config_validation.go backend/internal/core/auth/config_validation_test.go backend/internal/core/auth/config_groups_test.go backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): password-login schema pair, hot-reload declaration and snapshot validator with anti-lockout invariant" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 3: The gates — Login, Register, ForgotPassword, both admin send-password-reset routes

The §4.3 route table for the password service and the two admin reset routes, with the enumeration posture intact and the break-glass emission on a direct full-token rescue. (The pending-challenge re-check is Task 4; password-confirm is Task 5.)

**Files:**
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`Login:456-476`, `completeLogin:649`, `Register:217-252`, `ForgotPassword:940-1001`, `AdminTriggerPasswordReset:1815-1866`, new `EmitBreakGlassUsed`)
- Modify: `backend/internal/core/auth/handlers/password_handler.go` (`ForgotPassword:200-214`, `mapPasswordError:409-473`)
- Modify: `backend/internal/core/auth/handlers/admin_user_auth_handler.go` (`mapAdminInviterError:206-217`)
- Modify: `backend/internal/core/user/handlers/admin_client_handler.go` (`mapInviteErr:576-593`)
- Test: `backend/internal/core/auth/services/gates_test.go` + `gates_fakes_test.go` (extend), `backend/internal/core/auth/handlers/error_mapping_test.go` (extend), `backend/internal/core/user/handlers/admin_client_users_reset_gate_test.go` (new, mapping only)

**Interfaces:**
- Consumes: Task 1's `PasswordLoginEnabled` / `PasswordLoginDecision` / `PasswordAuthDecision` / sentinels / codes.
- Produces (used by Task 4): `completeLogin(ctx, user, in LoginInput, sourceAMR []string, decision PasswordAuthDecision)` (new final param; Task 4 copies it into the challenge); `func (s *PasswordAuthService) EmitBreakGlassUsed(ctx context.Context, audience, userUUID, sessionID, ip string)`.

- [ ] **Step 1: Add the two fakes the tests need**

Append to `backend/internal/core/auth/services/gates_fakes_test.go`:

```go
// gateAuditSink captures emitted audit events so the break-glass tests
// can assert exactly one auth.policy.break_glass_used with the right
// minimized fields.
type gateAuditSink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (g *gateAuditSink) Emit(_ context.Context, e iface.AuditEvent) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.events = append(g.events, e)
}

func (g *gateAuditSink) byAction(action string) []iface.AuditEvent {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []iface.AuditEvent
	for _, e := range g.events {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// gateEmailTokenRepo is the minimal EmailTokenRepository the ForgotPassword
// and AdminTriggerPasswordReset paths touch. Everything else panics.
type gateEmailTokenRepo struct {
	mu      sync.Mutex
	created []*authModels.EmailTokenDoc
}

func (g *gateEmailTokenRepo) Create(_ context.Context, d *authModels.EmailTokenDoc) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.created = append(g.created, d)
	return nil
}
func (g *gateEmailTokenRepo) GetByHash(context.Context, string) (*authModels.EmailTokenDoc, error) {
	panic("GetByHash not used in these tests")
}
func (g *gateEmailTokenRepo) MarkUsed(context.Context, string) error {
	panic("MarkUsed not used in these tests")
}
func (g *gateEmailTokenRepo) InvalidateByUserAndPurpose(context.Context, string, string) error {
	return nil
}
func (g *gateEmailTokenRepo) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("DeleteAllByUser not used in these tests")
}
```

In `gates_test.go`'s `newGatesEnv`, wire both: add `emailTokens: &gateEmailTokenRepo{}` and `audit: &gateAuditSink{}` fields to `gatesEnv`, pass `EmailTokenRepo: env.emailTokens` in the `PasswordAuthConfig`, and call `env.auth.SetAuditSink(env.audit)` after construction. (`SetAuditSink` exists — `password_auth_service.go:769`.)

- [ ] **Step 2: Write the failing gate tests**

Append to `gates_test.go`:

```go
// --- PR 3 §4.3: per-surface password-method gates ---

func passwordOff(surface string) map[string]string {
	return map[string]string{"passwordLoginEnabled" + surface: "false"}
}

func TestLogin_PasswordMethodGate(t *testing.T) {
	t.Run("off refuses before the user lookup, per audience", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		// Deliberately NO user seeded: the gate must answer before
		// GetUserForAuth, so the outcome cannot be ErrInvalidCredentials.
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "who@example.com", Password: "pw", IP: "203.0.113.9"})
		if !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
		// Counters untouched: same email must still have its full budget.
		if env.rateLimiter.IsBlocked(context.Background(), "email:who@example.com") {
			t.Fatal("rate limiter must not tick on a policy refusal")
		}
	})
	t.Run("other audience unaffected", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Admin"), nil)
		u := env.hashedUser("c@example.com", "correct horse battery staple")
		u.EmailVerified = true
		resp, err := env.auth.Login(context.Background(), LoginInput{Email: "c@example.com", Password: "correct horse battery staple"})
		if err != nil || resp == nil || resp.AccessToken == "" {
			t.Fatalf("client login with only Admin off must succeed, got (%v, %v)", resp, err)
		}
	})
	t.Run("policy outage is 503-shaped, never open", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "a@example.com", Password: "pw"})
		if !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("nil policy is an outage, not legacy-allow", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
		env.auth.policy = nil
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "a@example.com", Password: "pw"})
		if !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("break-glass rescues operator login and audits once", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		env.policy.SetOperatorBreakGlass(true)
		u := env.hashedUser("op@example.com", "correct horse battery staple")
		u.EmailVerified = true
		resp, err := env.auth.Login(context.Background(), LoginInput{Email: "op@example.com", Password: "correct horse battery staple", IP: "203.0.113.9"})
		if err != nil || resp == nil || resp.AccessToken == "" {
			t.Fatalf("rescued login must mint tokens, got (%v, %v)", resp, err)
		}
		got := env.audit.byAction("auth.policy.break_glass_used")
		if len(got) != 1 {
			t.Fatalf("want exactly one break-glass event, got %d", len(got))
		}
		e := got[0]
		if e.ActorUserID != u.UUID || e.IPAddress != "203.0.113.9" {
			t.Errorf("event must carry user UUID + source IP, got %+v", e)
		}
		if e.Metadata["audience"] != "operator" || e.Metadata["sessionId"] != resp.SessionID {
			t.Errorf("event must carry audience + session id, got %+v", e.Metadata)
		}
		if e.ActorEmail != "" {
			t.Errorf("event must not carry a full email, got %q", e.ActorEmail)
		}
	})
	t.Run("break-glass never rescues client login", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		env.policy.SetOperatorBreakGlass(true)
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "c@example.com", Password: "pw"})
		if !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
	})
	t.Run("failed break-glass attempt claims nothing", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		env.policy.SetOperatorBreakGlass(true)
		u := env.hashedUser("op@example.com", "correct horse battery staple")
		u.EmailVerified = true
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "op@example.com", Password: "WRONG", IP: "203.0.113.9"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("want ErrInvalidCredentials, got %v", err)
		}
		if n := len(env.audit.byAction("auth.policy.break_glass_used")); n != 0 {
			t.Fatalf("failed attempts must not claim the override was used, got %d events", n)
		}
	})
}

func TestRegister_PasswordMethodGate(t *testing.T) {
	in := RegisterInput{Email: "new@example.com", Password: "correct horse battery staple", FullName: "New User"}

	t.Run("off refuses, per audience, break-glass ignored", func(t *testing.T) {
		for _, tc := range []struct {
			aud     PolicyAudience
			surface string
		}{{PolicyAudienceOperator, "Admin"}, {PolicyAudienceClient, "Client"}} {
			env := newGatesEnv(t, tc.aud, passwordOff(tc.surface), nil)
			env.policy.SetOperatorBreakGlass(true)
			// A non-first signup: seed one existing user so the operator
			// bootstrap branch cannot fire.
			env.users.seed(&iface.User{UUID: "existing", Email: "e@example.com", IsActive: true})
			_, err := env.auth.Register(context.Background(), in)
			if !errors.Is(err, ErrPasswordLoginDisabled) {
				t.Fatalf("%s: want ErrPasswordLoginDisabled, got %v", tc.aud, err)
			}
		}
	})
	t.Run("operator first-user bootstrap bypasses the gate", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		u, err := env.auth.Register(context.Background(), in)
		if err != nil || u == nil {
			t.Fatalf("first operator signup must bypass the method gate, got (%v, %v)", u, err)
		}
		if u.Role != "super_admin" {
			t.Fatalf("first user must claim super_admin, got %q", u.Role)
		}
	})
	t.Run("RegisterInitialAdmin bootstrap stays reachable with password off", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		resp, err := env.auth.RegisterInitialAdmin(context.Background(), "root@example.com", "correct horse battery staple", "Root", "203.0.113.9")
		if err != nil || resp == nil || resp.AccessToken == "" {
			t.Fatalf("setup-wizard bootstrap is an explicit G2 exception; got (%v, %v)", resp, err)
		}
	})
	t.Run("empty Tier-2 collection gets no bypass", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		if _, err := env.auth.Register(context.Background(), in); !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
	})
	t.Run("policy outage refuses non-bootstrap signups", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		if _, err := env.auth.Register(context.Background(), in); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}

func TestForgotPassword_PasswordMethodGate(t *testing.T) {
	t.Run("off refuses before the lookup, identically for any email", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		env.policy.SetOperatorBreakGlass(true) // must be invisible here
		known := env.hashedUser("known@example.com", "correct horse battery staple")
		known.IsActive = true
		for _, email := range []string{"known@example.com", "unknown@example.com"} {
			err := env.auth.ForgotPassword(context.Background(), email, "203.0.113.9")
			if !errors.Is(err, ErrPasswordLoginDisabled) {
				t.Fatalf("%s: want ErrPasswordLoginDisabled, got %v", email, err)
			}
		}
		if n := len(env.emailTokens.created); n != 0 {
			t.Fatalf("no reset token may be minted under the gate, got %d", n)
		}
	})
	t.Run("on keeps the generic swallow for known and unknown email", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		known := env.hashedUser("known@example.com", "correct horse battery staple")
		known.IsActive = true
		for _, email := range []string{"known@example.com", "unknown@example.com"} {
			if err := env.auth.ForgotPassword(context.Background(), email, "203.0.113.9"); err != nil {
				t.Fatalf("%s: account-specific outcomes must stay swallowed, got %v", email, err)
			}
		}
	})
	t.Run("policy outage propagates", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		if err := env.auth.ForgotPassword(context.Background(), "a@example.com", ""); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}

func TestAdminTriggerPasswordReset_PasswordMethodGate(t *testing.T) {
	t.Run("off refuses; break-glass ignored", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		env.policy.SetOperatorBreakGlass(true)
		u := env.hashedUser("victim@example.com", "correct horse battery staple")
		if err := env.auth.AdminTriggerPasswordReset(context.Background(), u.UUID); !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
		if n := len(env.emailTokens.created); n != 0 {
			t.Fatalf("no reset token may be minted under the gate, got %d", n)
		}
	})
	t.Run("on still works", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		u := env.hashedUser("victim@example.com", "correct horse battery staple")
		if err := env.auth.AdminTriggerPasswordReset(context.Background(), u.UUID); err != nil {
			t.Fatalf("want success, got %v", err)
		}
		if n := len(env.emailTokens.created); n != 1 {
			t.Fatalf("want one reset token, got %d", n)
		}
	})
	t.Run("policy outage propagates", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		if err := env.auth.AdminTriggerPasswordReset(context.Background(), "any"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}
```

(If `hashedUser` does not already set `IsActive`/`EmailVerified`, follow the file's existing login tests for the exact seeding idiom.)

Append to `handlers/error_mapping_test.go`'s `TestMapPasswordError_KnownCodes` table:

```go
		{"password login disabled", services.ErrPasswordLoginDisabled, http.StatusForbidden, errcode.AuthPasswordLoginDisabled},
		{"policy unavailable", services.ErrAuthPolicyUnavailable, http.StatusServiceUnavailable, errcode.AuthPolicyUnavailable},
```

(match the table's existing column shape — name, in, wantStatus, wantCode.)

Append to `backend/internal/core/auth/handlers/admin_user_auth_security_events_test.go` (the §6 row that names this file) — handler-level proof that the operator reset route answers 409/503 (`NewAdminUserAuthHandler(auth, inviter, eventRepo)` takes interfaces, `admin_user_auth_handler.go:41-43`; the error paths never touch `auth` or `eventRepo`, so nils are honest):

```go
// PR 3 §4.3: the admin reset route refuses a method the target's surface
// rejects (409) and reports an unanswerable policy as an outage (503).
type fakeInviter struct{ err error }

func (f fakeInviter) AdminSendInvite(context.Context, string, string) error { return f.err }
func (f fakeInviter) AdminResendVerification(context.Context, string) error { return f.err }
func (f fakeInviter) AdminTriggerPasswordReset(context.Context, string) error { return f.err }

func TestSendPasswordReset_PasswordPolicyOutcomes(t *testing.T) {
	ctx := context.Background()
	req := &AdminSendPasswordResetRequest{UserID: "u1"}

	h := NewAdminUserAuthHandler(nil, fakeInviter{err: services.ErrPasswordLoginDisabled}, nil)
	_, err := h.SendPasswordReset(ctx, req)
	assertStatusAndCode(t, err, 409, "auth.password_login_disabled")

	h = NewAdminUserAuthHandler(nil, fakeInviter{err: fmt.Errorf("read passwordLoginEnabledAdmin: %w", services.ErrAuthPolicyUnavailable)}, nil)
	_, err = h.SendPasswordReset(ctx, req)
	assertStatusAndCode(t, err, 503, "auth.policy_unavailable")

}
```

(No success case here on purpose: success calls `h.auth.RecordAdminAuthEvent`, which a nil `services.AuthService` cannot answer — the happy path is covered by the Task 3 service tests plus live wiring.)

`assertStatusAndCode` does not exist yet — define it in this file, once for the whole package (Task 4's suites reuse it; `statusOf` already lives in `error_mapping_test.go`, same package, and `errcode.Error.Code` is the field — `internal/shared/errcode/errcode.go:23-28`):

```go
// assertStatusAndCode asserts the HTTP status and, when wantCode is
// non-empty, the stable body code of an errcode envelope. A wantCode of
// "" accepts any body (the generic huma 401 has none).
func assertStatusAndCode(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error")
	}
	if got := statusOf(t, err); got != wantStatus {
		t.Fatalf("status = %d, want %d", got, wantStatus)
	}
	if wantCode == "" {
		return
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) {
		t.Fatalf("want *errcode.Error, got %T (%v)", err, err)
	}
	if ec.Code != wantCode {
		t.Fatalf("code = %q, want %q", ec.Code, wantCode)
	}
}
```

Create `backend/internal/core/user/handlers/admin_client_users_reset_gate_test.go`:

```go
package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The client-user reset twin maps the auth sentinels across the module
// boundary by IDENTITY (errors.Is on the iface vars), never by message —
// wrapped errors must keep their status.
func TestMapInviteErr_PasswordPolicySentinels(t *testing.T) {
	cases := []struct {
		name       string
		in         error
		wantStatus int
	}{
		{"disabled maps to 409", iface.ErrPasswordLoginDisabled, http.StatusConflict},
		{"disabled survives wrapping", fmt.Errorf("outer: %w", iface.ErrPasswordLoginDisabled), http.StatusConflict},
		{"policy unavailable maps to 503", iface.ErrAuthPolicyUnavailable, http.StatusServiceUnavailable},
		{"policy unavailable survives wrapping", fmt.Errorf("read passwordLoginEnabledClient: %w", iface.ErrAuthPolicyUnavailable), http.StatusServiceUnavailable},
		{"unknown stays 500", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mapInviteErr(tc.in, "generic")
			var se huma.StatusError
			if !errors.As(out, &se) {
				t.Fatalf("want huma.StatusError, got %T", out)
			}
			if se.GetStatus() != tc.wantStatus {
				t.Fatalf("status = %d, want %d", se.GetStatus(), tc.wantStatus)
			}
		})
	}
}
```

- [ ] **Step 3: Run to verify failure**

`go test ./internal/core/auth/... ./internal/core/user/handlers/ -run 'Gate|TestMapPasswordError|TestMapInviteErr' -count=1`
Expected: FAIL — sentinels unmapped, gates absent, `completeLogin` signature unchanged.

- [ ] **Step 4: Implement the service gates**

`password_auth_service.go`:

1. `Login` — after the `LoginAllowed` check (`:466-468`):

```go
	// Per-surface method gate (spec §4.3): sits before GetUserForAuth so
	// lockout counters, the rate limiter and the audit trail see nothing
	// and every email receives the identical response. Only the operator
	// surface can be rescued by the boot-time break-glass; a nil policy
	// or failed read is an outage (503), never a pass.
	decision, err := s.policy.PasswordLoginDecision(ctx, s.audience)
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, ErrPasswordLoginDisabled
	}
```

2. `Login`'s tail (`:620`) becomes:

```go
	resp, err := s.completeLogin(ctx, user, in, []string{"pwd"}, decision)
	if err != nil {
		return nil, err
	}
	// A direct full-token success under the override is a rescued login
	// (spec §4.2). The MFA-partial case is audited by the winning
	// completion instead, which re-evaluates the decision itself.
	if decision.BreakGlassUsed && !resp.RequiresMFA {
		s.EmitBreakGlassUsed(ctx, string(s.audience), user.UUID, resp.SessionID, in.IP)
	}
	return resp, nil
```

3. `completeLogin` gains the parameter (used in Task 4; accepted and ignored here beyond plumb-through):

```go
func (s *PasswordAuthService) completeLogin(ctx context.Context, user *iface.User, in LoginInput, sourceAMR []string, decision PasswordAuthDecision) (*authModels.TokenResponse, error) {
```

4. `Register` — restructure the policy block (`:224-252`, deviation 11): bootstrap detection first, then the strict gate, then the existing registration checks:

```go
	isOperatorBootstrap := s.audience != PolicyAudienceClient

	// Bypass detection for the very first operator account — outside the
	// policy guard because the bootstrap exceptions must stay reachable
	// with no policy read at all (G2); the firstAdminClaimer's atomic
	// claim later still races correctly.
	isFirstUser := false
	if isOperatorBootstrap {
		if count, err := s.userService.GetUserCount(ctx, nil); err == nil && count == 0 {
			isFirstUser = true
		}
	}
	if !isFirstUser {
		// Per-surface method gate (spec §4.3): registration creates a
		// password credential the surface will not accept. Strict read —
		// break-glass never opens registration, and a nil policy is an
		// outage (503), not the legacy allow.
		enabled, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
		if err != nil {
			return nil, err
		}
		if !enabled {
			return nil, ErrPasswordLoginDisabled
		}
		if s.policy != nil {
			if !s.policy.RegistrationAllowed(ctx, s.audience) {
				return nil, ErrRegistrationDisabled
			}
			if !s.policy.EmailDomainAllowed(ctx, s.audience, email) {
				return nil, ErrEmailDomainNotAllowed
			}
		}
	}
```

(the original leading comment block about the operator-only bootstrap stays; `RegisterInitialAdmin` is untouched.)

5. `ForgotPassword` — prepend the gate (before the `GetUserForAuth` at `:942`):

```go
	// Per-surface method gate (spec §4.3): public, and it mints a
	// credential-setting token for a rejected method. Strict read, never
	// break-glass, evaluated BEFORE the user lookup so the outcome cannot
	// depend on account state. These two errors are the ONLY ones this
	// method returns; every account-specific outcome below stays swallowed.
	enabled, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrPasswordLoginDisabled
	}
```

6. `AdminTriggerPasswordReset` — same gate at the top (`:1816`), commented for the 409:

```go
	// Per-surface method gate (spec §4.3): an operator-minted reset for a
	// method the target's surface rejects would also revoke the target's
	// sessions and leave an unusable password — the handlers map this to
	// 409. Strict read; break-glass never opens it.
	enabled, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrPasswordLoginDisabled
	}
```

7. New method beside `emitLoginFailed` (`:626`):

```go
// EmitBreakGlassUsed records one rescued password authentication (spec
// §4.2): the boot-time operator break-glass — not persisted policy — is
// what allowed it. Called by Login on a direct full-token success and by
// the winning MFA/WebAuthn completion of a rescued challenge (via the
// handlers' LoginTokenIssuer). Best-effort through the nil-guarded audit
// sink; carries audience, user UUID, session id and source IP — never a
// password, a token or a full email.
func (s *PasswordAuthService) EmitBreakGlassUsed(ctx context.Context, audience, userUUID, sessionID, ip string) {
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID: userUUID,
		ActorType:   "user",
		Action:      "auth.policy.break_glass_used",
		Outcome:     "success",
		IPAddress:   ip,
		Metadata: map[string]any{
			"audience":  audience,
			"sessionId": sessionID,
		},
	})
}
```

- [ ] **Step 5: Implement the handler mappings**

`password_handler.go` — `ForgotPassword` (`:200-214`) stops discarding:

```go
func (h *PasswordAuthHandler) ForgotPassword(ctx context.Context, req *ForgotPasswordRequest) (*ForgotPasswordResponse, error) {
	ip := clientIPFromCtx(ctx)
	// The service returns ONLY the two per-surface policy errors (spec
	// §4.3) and swallows every account-specific outcome itself, so
	// propagating err verbatim cannot become an enumeration oracle.
	if err := h.svc.ForgotPassword(ctx, req.Body.Email, ip); err != nil {
		return nil, mapPasswordError(err)
	}
	resp := &ForgotPasswordResponse{}
	resp.Body.Success = true
	resp.Body.Message = "If an account with that email exists, a password reset email has been sent."
	return resp, nil
}
```

(the success body is byte-for-byte the current one — `password_handler.go:203-206`; only the error propagation is new.)

`mapPasswordError` gains, next to the `ErrLoginDisabled` case:

```go
	case errors.Is(err, services.ErrPasswordLoginDisabled):
		return errcode.Forbidden(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in is disabled on this surface. Use a configured sign-in provider, or contact an administrator.")
	case errors.Is(err, services.ErrAuthPolicyUnavailable):
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
```

`admin_user_auth_handler.go` — `mapAdminInviterError` gains, before the message matches:

```go
	if errors.Is(err, services.ErrPasswordLoginDisabled) {
		return errcode.Conflict(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in is disabled on this user's surface; a reset link would mint a credential the surface refuses. Re-enable the method first.")
	}
	if errors.Is(err, services.ErrAuthPolicyUnavailable) {
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
```

(add the `errcode` import if absent.)

`internal/core/user/handlers/admin_client_handler.go` — `mapInviteErr` gains the same pair via the **iface** sentinels (this package must not import auth/services):

```go
	if errors.Is(err, iface.ErrPasswordLoginDisabled) {
		return errcode.Conflict(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in is disabled on the client surface; a reset link would mint a credential the surface refuses. Re-enable the method first.")
	}
	if errors.Is(err, iface.ErrAuthPolicyUnavailable) {
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
```

(add `github.com/orkestra/backend/internal/shared/errcode` to its imports; `iface` is already imported.)

- [ ] **Step 6: Fix the ripple**

Verified at `ca24e614` — the ripple is empty, so this step is a confirmation run, not a hunt:

- `completeLogin` has exactly one caller, `Login` (`password_auth_service.go:620`), edited in Step 4.
- Only three test suites construct a `PasswordAuthService`: `gates_test.go` (via `newGatesEnv`, whose policy is wired with empty values → legacy true → every existing login/register/forgot case passes the new gates untouched); `password_credential_revocation_test.go` (calls only the ungated `ResetPassword`/`ChangePassword`); `session_identity_test.go` (its own constructor at `:62-69` calls only `IssueLoginTokens`, and its one `env.auth.Login` at `:160` runs through `newGatesEnv`'s legacy-true policy).

Run `go build ./... && go vet ./...` and expect zero fallout beyond the files this task edits; any surprise is a NEW caller worth reading, not something to silence.

- [ ] **Step 7: Same-commit docs**

`backend/internal/core/auth/CLAUDE.md` — the route rows this task changes: login/register/forgot-password refuse with 403 `auth.password_login_disabled` per surface (gate before the user lookup, counters untouched); both admin send-password-reset routes answer 409 with the same code (client twin via `iface.AdminAuthInviter` + the iface sentinels); `reset-password`/`accept-invite`/`verify-email`/`change-password` stay open; break-glass rescues operator `Login` only and emits `auth.policy.break_glass_used`.

- [ ] **Step 8: Run the suites, vet, commit**

Run: `go test ./internal/core/auth/... ./internal/core/user/... -count=1` then `go vet ./...`
Expected: PASS.

```bash
git add backend/internal/core/auth/CLAUDE.md backend/internal/core/auth/services/password_auth_service.go backend/internal/core/auth/services/gates_test.go backend/internal/core/auth/services/gates_fakes_test.go backend/internal/core/auth/handlers/password_handler.go backend/internal/core/auth/handlers/admin_user_auth_handler.go backend/internal/core/auth/handlers/error_mapping_test.go backend/internal/core/auth/handlers/admin_user_auth_security_events_test.go backend/internal/core/user/handlers/admin_client_handler.go backend/internal/core/user/handlers/admin_client_users_reset_gate_test.go
git commit -m "feat(auth): per-surface password-method gates on login, register, forgot-password and both admin reset routes" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 4: The pending-challenge re-check — `MFAChallenge.Audience`/`BreakGlassUsed`, both completion handlers

G2's "no in-flight password login can complete after the flip": the challenge learns its audience and break-glass provenance, and both completion endpoints re-evaluate `PasswordLoginDecision` before verifying the factor.

**Files:**
- Modify: `backend/internal/core/auth/services/mfa_challenge_service.go:44-70` (fields), `:76-94` (input), `:142-176` (`BeginLogin` copy)
- Modify: `backend/internal/core/auth/services/password_auth_service.go:701-714` (stamp in `completeLogin`)
- Modify: `backend/internal/core/auth/services/auth_service.go:2249-2254` (stamp in the OAuth branch)
- Modify: `backend/internal/core/auth/handlers/mfa_handler.go` (`LoginTokenIssuer:21-23`, `LoginVerify:467+`, new shared helper)
- Modify: `backend/internal/core/auth/handlers/webauthn_handler.go` (struct + `SetPolicy`, `LoginFinish:366+`)
- Modify: `backend/internal/core/auth/module.go:1119-1131`, `:1271+` (wire `SetPolicy` on both WebAuthn handlers)
- Test: `backend/internal/core/auth/handlers/mfa_login_verify_test.go` (new)

**Interfaces:**
- Consumes: Task 1's `PasswordLoginDecision`; Task 3's `completeLogin(…, decision)` and `EmitBreakGlassUsed`.
- Produces:
  - `MFAChallenge.Audience string` (`json:"audience,omitempty"` — "operator"/"client"; empty marks a pre-v3 in-flight challenge), `MFAChallenge.BreakGlassUsed bool` (`json:"breakGlassUsed,omitempty"`); same two fields on `LoginChallengeInput`.
  - `LoginTokenIssuer` gains `EmitBreakGlassUsed(ctx context.Context, audience, userUUID, sessionID, ip string)` (deviation 5; `*PasswordAuthService` already satisfies it after Task 3).
  - `WebAuthnHandler.SetPolicy(p *services.AuthPolicyService)`.
  - handlers-package helper `recheckPasswordChallenge(ctx, policy, challenges, ch) (breakGlassUsed bool, err error)` shared by both completion endpoints.
  - `services.NewAuthPolicyServiceForTest(values map[string]string) *AuthPolicyService` / `NewAuthPolicyServiceForTestErr(err error) *AuthPolicyService` — exported test constructors (consumed again by Task 7's endpoint tests).

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/handlers/mfa_login_verify_test.go`. Two layers, both in this file:

*Layer 1 — the helper matrix.* `recheckPasswordChallenge` accepts a narrow local interface so its decision matrix runs against a two-line fake, with the REAL challenge service over the in-memory store (`services.NewMemoryOAuthStateStore()` + `services.NewMFAChallengeService` — the same pair `mfa_challenge_service_test.go` uses) proving the consume/retain side effects:

```go
// passwordLoginDecider is the one-method slice of AuthPolicyService the
// completion re-check consumes; a fake satisfies it in tests.
type passwordLoginDecider interface {
	PasswordLoginDecision(ctx context.Context, audience services.PolicyAudience) (services.PasswordAuthDecision, error)
}
```

With that seam the handler tests are direct. The test file:

```go
package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

type fakeDecider struct {
	d   services.PasswordAuthDecision
	err error
}

func (f *fakeDecider) PasswordLoginDecision(context.Context, services.PolicyAudience) (services.PasswordAuthDecision, error) {
	return f.d, f.err
}

// consumeTracker wraps the real in-memory challenge service so tests can
// assert consumption without re-implementing challenge semantics.
func newLoginChallenge(t *testing.T, svc services.MFAChallengeService, audience string) *services.MFAChallenge {
	t.Helper()
	ch, err := svc.BeginLogin(context.Background(), services.LoginChallengeInput{
		UserUUID:  "u-1",
		SessionID: "sid-1",
		SourceAMR: []string{"pwd"},
		LoginMethod: "password",
		Audience:  audience,
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	return ch
}

func newChallengeService(t *testing.T) services.MFAChallengeService {
	t.Helper()
	return services.NewMFAChallengeService(services.NewMemoryOAuthStateStore())
}

func TestRecheckPasswordChallenge(t *testing.T) {
	ctx := context.Background()

	t.Run("oauth-sourced challenge is untouched, decider never called", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"oauth"}, LoginMethod: "oauth", Audience: "operator",
		})
		bg, err := recheckPasswordChallenge(ctx, nil, svc, ch) // nil decider must not matter
		if err != nil || bg {
			t.Fatalf("oauth challenge must pass untouched, got (%v, %v)", bg, err)
		}
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("challenge must be retained")
		}
	})
	t.Run("allowed proceeds and reports the decision's break-glass", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		bg, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{Allowed: true, BreakGlassUsed: true}}, svc, ch)
		if err != nil || !bg {
			t.Fatalf("want rescued allow, got (%v, %v)", bg, err)
		}
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("allowed path must not consume — the one-winner Consume happens later")
		}
	})
	t.Run("disabled consumes atomically and maps 403", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "client")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{}}, svc, ch)
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("refused challenge must be consumed")
		}
	})
	t.Run("policy outage is 503 and RETAINS the challenge", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{err: services.ErrAuthPolicyUnavailable}, svc, ch)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("transient outage must retain the challenge for retry within its TTL")
		}
	})
	t.Run("nil decider on a password challenge is an outage, challenge retained", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		_, err := recheckPasswordChallenge(ctx, nil, svc, ch)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("missing wiring must not consume")
		}
	})
	t.Run("empty audience is invalid and consumed", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{Allowed: true}}, svc, ch)
		assertStatusAndCode(t, err, 401, "")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("pre-v3 challenge must be consumed")
		}
	})
	t.Run("unknown audience is invalid and consumed", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "service")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{Allowed: true}}, svc, ch)
		assertStatusAndCode(t, err, 401, "")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("unknown-audience challenge must be consumed")
		}
	})
	t.Run("pwd in SourceAMR alone marks the challenge password-sourced", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"pwd"}, LoginMethod: "", Audience: "operator",
		})
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{}}, svc, ch)
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
	})
}
```

using `assertStatusAndCode` — already defined once for this package by Task 3 (`admin_user_auth_security_events_test.go`); do not redefine it.

*Layer 2 — the two handlers end-to-end* (spec §6's `mfa_login_verify_test.go` row: refusal consumes + 403, transient policy error 503 + retained, OAuth-sourced unaffected, break-glass permits one winning completion and produces **exactly one** rescued-login audit event). Drive the real policy service through the `services.NewAuthPolicyServiceForTest` / `NewAuthPolicyServiceForTestErr` constructors (introduced in Step 5 below) and fake the rest with the interface-embedding idiom `oauth_callback_flow_test.go:157-162` already uses (`fakeJWT` embeds `services.JWTService` and overrides one method — a call to anything else panics loudly):

```go
type completionJWT struct{ services.JWTService }

func (completionJWT) RefreshTokenTTL() time.Duration { return time.Hour }

type completionMFA struct{ services.MFAService }

func (completionMFA) Verify(context.Context, string, string) error { return nil }

type completionWebAuthn struct{ services.WebAuthnService }

func (completionWebAuthn) FinishAssertion(context.Context, *iface.User, string, services.MFAChallengePurpose, []byte) error {
	return nil
}

type completionUsers struct{ iface.UserProvider }

func (completionUsers) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	return &iface.User{UUID: id, Email: "u@example.com", IsActive: true, EmailVerified: true, Role: "operator"}, nil
}

type completionIssuer struct {
	mu         sync.Mutex
	issued     int
	breakGlass []string // audiences EmitBreakGlassUsed was called with
}

func (c *completionIssuer) IssueLoginTokensForSession(_ context.Context, user *iface.User, in services.LoginTokenContext, _ []string, _ int64) (*authModels.TokenResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.issued++
	return &authModels.TokenResponse{AccessToken: "at", TokenType: "Bearer", SessionID: in.SessionID, RefreshToken: "rt"}, nil
}

func (c *completionIssuer) EmitBreakGlassUsed(_ context.Context, audience, _, _, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.breakGlass = append(c.breakGlass, audience)
}

func newCompletionMFAHandler(t *testing.T, policy *services.AuthPolicyService, challenges services.MFAChallengeService, issuer *completionIssuer) *MFAHandler {
	t.Helper()
	h := NewMFAHandler(completionMFA{}, challenges, completionJWT{}, completionUsers{}, issuer, "cookie", "", false)
	h.SetPolicy(policy)
	return h
}
```

```go
func TestLoginVerify_PasswordPolicyRecheck(t *testing.T) {
	ctx := context.Background()
	verifyReq := func(id string) *MFALoginVerifyRequest {
		r := &MFALoginVerifyRequest{}
		r.Body.ChallengeID = id
		r.Body.Code = "123456"
		return r
	}

	t.Run("post-flip refusal consumes and answers 403", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		issuer := &completionIssuer{}
		_, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("refused challenge must be consumed")
		}
		if issuer.issued != 0 || len(issuer.breakGlass) != 0 {
			t.Fatal("nothing may be minted or audited on a refusal")
		}
	})
	t.Run("transient policy error answers 503 and retains the challenge", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		issuer := &completionIssuer{}
		_, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("challenge must survive a transient outage for retry within its TTL")
		}
	})
	t.Run("oauth-sourced challenge completes under a password-off policy", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"oauth"}, LoginMethod: "oauth", Audience: "operator",
		})
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		issuer := &completionIssuer{}
		resp, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("oauth challenge must be unaffected, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 0 {
			t.Fatal("no rescue happened; nothing to audit")
		}
	})
	t.Run("break-glass permits the completion and audits exactly once", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		policy.SetOperatorBreakGlass(true)
		issuer := &completionIssuer{}
		resp, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("rescued completion must succeed, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 1 || issuer.breakGlass[0] != "operator" {
			t.Fatalf("want exactly one operator rescue event, got %v", issuer.breakGlass)
		}
	})
	t.Run("challenge minted WITH BreakGlassUsed audits even when completion reads true", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"pwd"}, LoginMethod: "password",
			Audience: "operator", BreakGlassUsed: true,
		})
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"})
		issuer := &completionIssuer{}
		resp, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("completion must succeed, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 1 {
			t.Fatalf("the initial check's rescue must still be audited once, got %v", issuer.breakGlass)
		}
	})
}

func TestWebAuthnLoginFinish_PasswordPolicyRecheck(t *testing.T) {
	ctx := context.Background()
	finishReq := func(id string) *webAuthnLoginFinishRequest {
		r := &webAuthnLoginFinishRequest{}
		r.Body.LoginChallengeID = id
		r.Body.WebAuthnChallengeID = "wa-1"
		r.Body.AssertionResponse = json.RawMessage(`{}`)
		return r
	}
	newHandler := func(policy *services.AuthPolicyService, challenges services.MFAChallengeService, issuer *completionIssuer) *WebAuthnHandler {
		h := NewWebAuthnHandler(completionWebAuthn{}, challenges, completionJWT{}, completionUsers{}, issuer, "cookie", "", false)
		h.SetPolicy(policy)
		return h
	}

	t.Run("post-flip refusal consumes and answers 403", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "client")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledClient": "false"})
		issuer := &completionIssuer{}
		_, err := newHandler(policy, svc, issuer).LoginFinish(ctx, finishReq(ch.ID))
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("refused challenge must be consumed")
		}
	})
	t.Run("break-glass rescue audits exactly once", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		policy.SetOperatorBreakGlass(true)
		issuer := &completionIssuer{}
		resp, err := newHandler(policy, svc, issuer).LoginFinish(ctx, finishReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("rescued completion must succeed, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 1 {
			t.Fatalf("want exactly one rescue event, got %v", issuer.breakGlass)
		}
	})
}
```

(`ValidateTokenEligibleUser` — `auth_service.go:2181-2186` — requires only a non-nil user with a non-empty UUID and `IsActive: true`; `completionUsers`' fixture satisfies it as written.)

Also append to `backend/internal/core/auth/services/mfa_challenge_service_test.go`:

```go
func TestBeginLogin_RoundTripsAudienceAndBreakGlass(t *testing.T) {
	svc := NewMFAChallengeService(NewMemoryOAuthStateStore())
	ch, err := svc.BeginLogin(context.Background(), LoginChallengeInput{
		UserUUID:       "u-1",
		SessionID:      "sid-1",
		SourceAMR:      []string{"pwd"},
		LoginMethod:    "password",
		Audience:       "operator",
		BreakGlassUsed: true,
	})
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	if ch.Audience != "operator" || !ch.BreakGlassUsed {
		t.Fatalf("BeginLogin must copy the new fields, got %+v", ch)
	}
	got, err := svc.Peek(context.Background(), ch.ID)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if got.Audience != "operator" || !got.BreakGlassUsed {
		t.Fatalf("audience/break-glass must survive the JSON round-trip, got %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/core/auth/handlers/ -run TestRecheckPasswordChallenge -count=1`
Expected: compile FAILURE (`recheckPasswordChallenge`, `Audience` field undefined).

- [ ] **Step 3: Add the challenge fields**

`mfa_challenge_service.go` — `MFAChallenge` gains, after `SourceAMR` (`:69`):

```go
	// Audience names the surface the login challenge was minted for
	// ("operator" | "client"). REQUIRED on post-toggle challenges: the
	// completion re-check (spec §4.3) evaluates the per-surface password
	// policy against it, and an empty/unknown value marks a pre-toggle
	// in-flight challenge, which completion refuses and consumes.
	Audience string `json:"audience,omitempty"`
	// BreakGlassUsed records that the INITIAL password check was allowed
	// by the operator break-glass rather than persisted policy. Audit
	// context only — completion still requires a fresh decision.
	BreakGlassUsed bool `json:"breakGlassUsed,omitempty"`
```

`LoginChallengeInput` gains the same two fields (`:76-94`), and `BeginLogin` copies them (`Audience: in.Audience, BreakGlassUsed: in.BreakGlassUsed`) into the struct literal (`:147-167`).

- [ ] **Step 4: Stamp both producers**

`password_auth_service.go` `completeLogin` (`:701`): add to the `LoginChallengeInput` literal:

```go
			Audience:       string(s.audience),
			BreakGlassUsed: decision.BreakGlassUsed,
```

`auth_service.go` OAuth branch (`:2249`): add `Audience: string(s.audience),` to its `LoginChallengeInput` literal (OAuth challenges are never break-glass).

- [ ] **Step 5: Implement the handler re-check**

In `mfa_handler.go`:

1. `LoginTokenIssuer` (`:21-23`) gains the method:

```go
type LoginTokenIssuer interface {
	IssueLoginTokensForSession(ctx context.Context, user *iface.User, in services.LoginTokenContext, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error)
	// EmitBreakGlassUsed records a rescued password authentication (spec
	// §4.2). On the interface — not an optional assertion — so a fake
	// cannot silently drop the audit contract.
	EmitBreakGlassUsed(ctx context.Context, audience, userUUID, sessionID, ip string)
}
```

2. New shared helper + seam next to it:

```go
// passwordLoginDecider is the one-method slice of AuthPolicyService the
// completion re-check consumes; tests inject a fake.
type passwordLoginDecider interface {
	PasswordLoginDecision(ctx context.Context, audience services.PolicyAudience) (services.PasswordAuthDecision, error)
}

// recheckPasswordChallenge enforces spec §4.3's pending-challenge rule on
// both completion endpoints, BEFORE the factor is verified (a disabled
// login must not burn attempt budget or probe factor state):
//
//   - challenge not password-sourced → untouched, (false, nil);
//   - empty/unknown audience → pre-toggle in-flight challenge: consumed,
//     401 (rollout waits one challenge TTL before exposing the switch);
//   - decision error → 503 auth.policy_unavailable WITHOUT consuming, so
//     a transient outage is retryable inside the original TTL;
//   - !Allowed → atomically consumed, 403 auth.password_login_disabled;
//   - Allowed → (BreakGlassUsed, nil); the one-winner Consume stays with
//     the caller's existing flow.
//
// A nil decider on a password-sourced challenge is missing wiring — an
// outage, never a pass (G4).
func recheckPasswordChallenge(ctx context.Context, policy passwordLoginDecider, challenges services.MFAChallengeService, ch *services.MFAChallenge) (bool, error) {
	if !passwordSourcedChallenge(ch) {
		return false, nil
	}
	var audience services.PolicyAudience
	switch ch.Audience {
	case string(services.PolicyAudienceOperator):
		audience = services.PolicyAudienceOperator
	case string(services.PolicyAudienceClient):
		audience = services.PolicyAudienceClient
	default:
		_, _ = challenges.Consume(ctx, ch.ID)
		return false, huma.Error401Unauthorized("invalid or expired challenge")
	}
	if policy == nil {
		return false, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
	decision, err := policy.PasswordLoginDecision(ctx, audience)
	if err != nil {
		return false, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
	if !decision.Allowed {
		_, _ = challenges.Consume(ctx, ch.ID)
		return false, errcode.Forbidden(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in was disabled while this login was in flight. Start over with a configured sign-in provider.")
	}
	return decision.BreakGlassUsed, nil
}

// passwordSourcedChallenge reports whether the login challenge was minted
// by a password login (either provenance marker; both are stamped today —
// password_auth_service.go completeLogin, auth_service.go OAuth branch).
func passwordSourcedChallenge(ch *services.MFAChallenge) bool {
	if ch.LoginMethod == "password" {
		return true
	}
	for _, v := range ch.SourceAMR {
		if v == "pwd" {
			return true
		}
	}
	return false
}
```

3. One tiny method on both handler types keeps the typed-nil trap out of the call sites (`h.policy` is a concrete `*services.AuthPolicyService`; wrapping a nil pointer in the interface directly would dodge the helper's `policy == nil` branch):

```go
// decider adapts the handler's concrete policy pointer to the helper's
// interface, mapping a nil pointer to a nil INTERFACE so missing wiring
// takes the fail-closed 503 branch instead of a typed-nil surprise.
func (h *MFAHandler) decider() passwordLoginDecider {
	if h.policy == nil {
		return nil
	}
	return h.policy
}
```

(and the identical `func (h *WebAuthnHandler) decider() passwordLoginDecider` once that handler has its `policy` field.)

4. `LoginVerify` — after the purpose/session checks (`:483`), before `req.Body.UseBackup`:

```go
	// Spec §4.3: a password-sourced challenge must still be allowed NOW
	// (before the factor is verified, so a disabled login can neither
	// burn attempt budget nor probe factor state — deviation 6).
	rescued, err := recheckPasswordChallenge(ctx, h.decider(), h.challenges, ch)
	if err != nil {
		return nil, err
	}
```

and after the successful `IssueLoginTokensForSession` (`:519`):

```go
	// One rescued-login audit event per winning completion (spec §4.2):
	// either the initial password check or this completion's decision
	// used the break-glass. Only the Consume winner reaches this line.
	if rescued || ch.BreakGlassUsed {
		h.tokens.EmitBreakGlassUsed(ctx, ch.Audience, ch.UserUUID, ch.SessionID, ch.IPAddress)
	}
```

5. `webauthn_handler.go`: struct gains `policy *services.AuthPolicyService`; add:

```go
// SetPolicy wires the admin-managed AuthPolicyService so LoginFinish can
// re-evaluate a password-sourced challenge's per-surface policy at
// completion time (spec §4.3). Wired unconditionally in module.go; nil
// makes password-sourced completions fail closed (503).
func (h *WebAuthnHandler) SetPolicy(p *services.AuthPolicyService) {
	h.policy = p
}
```

`LoginFinish` — after its purpose/session checks (`:383`), before `GetUserByID`:

```go
	// Spec §4.3: a password-sourced challenge must still be allowed NOW
	// (before the assertion ceremony — deviation 6).
	rescued, err := recheckPasswordChallenge(ctx, h.decider(), h.mfaChallenges, loginCh)
	if err != nil {
		return nil, err
	}
```

and after its successful `IssueLoginTokensForSession` (`:412`):

```go
	// One rescued-login audit event per winning completion (spec §4.2).
	if rescued || loginCh.BreakGlassUsed {
		h.tokens.EmitBreakGlassUsed(ctx, loginCh.Audience, loginCh.UserUUID, loginCh.SessionID, loginCh.IPAddress)
	}
```

6. `module.go`: after each `NewWebAuthnHandler` construction (`:1119-1131` operator, the client twin below `:1271`), add `m.operatorWebAuthnHandler.SetPolicy(authPolicy)` / `m.clientWebAuthnHandler.SetPolicy(authPolicy)`.

7. In `services/auth_policy_service.go`, the two exported test constructors the handler tests (and Task 7's) build policy states with — small, honest, logic-free:

```go
// NewAuthPolicyServiceForTest builds a policy service over a fixed value
// map with GetRawValue semantics (absent key = absent; present empty =
// present). For consumer-package tests only — production wiring uses
// NewAuthPolicyService(*module.ModuleConfigService).
func NewAuthPolicyServiceForTest(values map[string]string) *AuthPolicyService {
	return &AuthPolicyService{cs: fixedValueReader{values: values}}
}

// NewAuthPolicyServiceForTestErr builds a policy service whose reads all
// fail with err — the "module_configs unreachable" fixture.
func NewAuthPolicyServiceForTestErr(err error) *AuthPolicyService {
	return &AuthPolicyService{cs: fixedValueReader{err: err}}
}

// fixedValueReader is the minimal configValueReader behind the ForTest
// constructors. A missing document is modelled only through err — a nil
// map simply has every key absent (= legacy defaults).
type fixedValueReader struct {
	values map[string]string
	err    error
}

func (r fixedValueReader) GetValue(_ context.Context, _, key string) string { return r.values[key] }
func (r fixedValueReader) GetRawValue(_ context.Context, _, key string) (string, bool, error) {
	if r.err != nil {
		return "", false, r.err
	}
	v, ok := r.values[key]
	return v, ok, nil
}
func (r fixedValueReader) GetRawValueRequiredModule(ctx context.Context, moduleName, key string) (string, bool, error) {
	return r.GetRawValue(ctx, moduleName, key)
}
```

- [ ] **Step 6: Fix the ripple**

Verified at `ca24e614`: exactly ONE baseline fake implements `LoginTokenIssuer` — `countingLoginTokenIssuer` (`handlers/step_up_session_identity_test.go:69-77`). It gains the no-op:

```go
func (i *countingLoginTokenIssuer) EmitBreakGlassUsed(context.Context, string, string, string, string) {}
```

(this task's own `completionIssuer` already implements the method). Run `go build ./... && go vet ./...`; anything else that fails to compile is a NEW implementor added since `ca24e614` and gets the same one-line no-op.

- [ ] **Step 7: Same-commit docs**

`backend/internal/core/auth/CLAUDE.md` — the MFA-completion row: password-sourced challenges (LoginMethod `"password"` ∨ SourceAMR∋`"pwd"`) are re-checked at completion — disabled → consumed + 403; policy outage → 503 with the challenge retained; empty/unknown `Audience` → consumed + 401 (pre-toggle in-flight; rollout waits one 5-minute TTL); OAuth-sourced untouched; the winning rescued completion emits one `auth.policy.break_glass_used`. `MFAChallenge` carries `Audience` + `BreakGlassUsed`.

- [ ] **Step 8: Run the suites + commit**

Run: `go test ./internal/core/auth/... -count=1`
Expected: PASS, including the new `TestRecheckPasswordChallenge`, both handler-level suites and the challenge round-trip.

```bash
git add backend/internal/core/auth/CLAUDE.md backend/internal/core/auth/services/mfa_challenge_service.go backend/internal/core/auth/services/mfa_challenge_service_test.go backend/internal/core/auth/services/password_auth_service.go backend/internal/core/auth/services/auth_service.go backend/internal/core/auth/services/auth_policy_service.go backend/internal/core/auth/handlers/mfa_handler.go backend/internal/core/auth/handlers/webauthn_handler.go backend/internal/core/auth/handlers/mfa_login_verify_test.go backend/internal/core/auth/module.go
git commit -m "feat(auth): re-check password policy at MFA/WebAuthn completion; challenges carry audience and break-glass provenance" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 5: Step-up — `PasswordReauthAllowed` in the middleware, strict gate in password-confirm

§4.6 both halves: the middleware stops offering a password reconfirm for a method the surface refuses (and stops fabricating enrollment requirements on outages), and the service refuses to mint a `reauth` proof from a disabled method.

**Files:**
- Modify: `backend/internal/shared/middleware/auth.go` (`StepUpPolicy:68-79`, `dispatchStepUpFailure:987-1005`, new `sendPolicyUnavailable`, doc comments)
- Modify: `backend/internal/core/auth/services/auth_policy_service.go` (adapter method)
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`ConfirmPasswordWithSecurity:1161+`)
- Modify: `backend/internal/shared/middleware/step_up_test.go:137-152` (fake + new cases), `backend/internal/core/tenant/handlers/admin_mfa_routes_test.go:53-58` (fake)
- Test: `backend/internal/core/auth/services/password_confirm_test.go` (extend), `backend/internal/core/auth/services/auth_policy_service_test.go` (adapter cases)

**Interfaces:**
- Consumes: Task 1's `PasswordLoginEnabled`, sentinels/codes; the existing `ErrPasswordConfirmUnavailable` (`password_auth_service.go:46`) and its 409 mapping (`password_handler.go:326-328`); `mapPasswordError`'s new 503 case (Task 3) for the policy-error path.
- Produces:
  - `middleware.StepUpPolicy` gains `PasswordReauthAllowed(ctx context.Context, audience string) (bool, error)` — **breaking for any implementor**, which is the correct signal (§4.6); both in-tree fakes updated.
  - `(*AuthPolicyService).PasswordReauthAllowed(ctx, audience string) (bool, error)` adapter over strict `PasswordLoginEnabled` (production wiring unchanged: `main.go:359-361` registers the same `*AuthPolicyService`).

- [ ] **Step 1: Write the failing middleware tests**

In `backend/internal/shared/middleware/step_up_test.go`, extend `fakeStepUpPolicy` (`:138-152`):

```go
type fakeStepUpPolicy struct {
	required bool
	// mfaDisabled flips the master MFA switch off. Defaults false so the
	// zero value reports the switch ON — every existing step-up test keeps
	// its behaviour without being updated.
	mfaDisabled bool
	// reauthDisabled / reauthErr drive the PR 3 PasswordReauthAllowed
	// branch: zero values report (true, nil) so pre-existing tests that
	// reach the password-confirm envelope keep passing unchanged.
	reauthDisabled bool
	reauthErr      error
}

func (f *fakeStepUpPolicy) PasswordReauthAllowed(_ context.Context, _ string) (bool, error) {
	if f.reauthErr != nil {
		return false, f.reauthErr
	}
	return !f.reauthDisabled, nil
}
```

(keep the two existing methods). Then append cases — follow `runStepUpWithEnrollment`'s harness (`:154+`), adding a policy-injecting variant if its signature can't carry the new knobs:

```go
// PR 3 §4.6: the password-confirm fallback is offered only when the
// password is an accepted credential for the token's audience.
func TestRequireStepUp_PasswordReauthDisabledBecomesEnrollmentRequired(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false /*hasFactor*/, nil, &fakeStepUpPolicy{reauthDisabled: true})
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusForbidden || body["code"] != "mfa_enrollment_required" {
		t.Fatalf("want 403 mfa_enrollment_required, got %d %v", status, body["code"])
	}
}

func TestRequireStepUp_PolicyErrorIs503NotEnrollment(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false, nil, &fakeStepUpPolicy{reauthErr: errors.New("mongo down")})
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusServiceUnavailable || body["code"] != "auth.policy_unavailable" {
		t.Fatalf("an outage must be reported as an outage, got %d %v", status, body["code"])
	}
}

func TestRequireStepUp_MissingPolicyIs503OnPasswordConfirmBranch(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false, nil, nil /*no StepUpPolicy*/)
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusServiceUnavailable || body["code"] != "auth.policy_unavailable" {
		t.Fatalf("missing wiring must fail closed, got %d %v", status, body["code"])
	}
}

func TestRequireStepUp_ReauthAllowedKeepsPasswordConfirm(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false, nil, &fakeStepUpPolicy{})
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusUnauthorized || body["code"] != "password_confirm_required" {
		t.Fatalf("allowed method keeps today's envelope, got %d %v", status, body["code"])
	}
}
```

with the harness variant (modelled on `runStepUpWithEnrollment`, same file):

```go
// runStepUpWithPolicy is runStepUpWithEnrollment with an explicit policy
// (nil = SetStepUpPolicy never called), for the PR 3 branch matrix.
func runStepUpWithPolicy(t *testing.T, claims *authModels.JWTClaims, hasFactor bool, lookupErr error, policy StepUpPolicy) (bool, int, map[string]any) {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	m.SetMFAEnrollmentLookup(func(_ context.Context, _, _ string) (bool, error) {
		return hasFactor, lookupErr
	})
	if policy != nil {
		m.SetStepUpPolicy(policy)
	}
	return runStepUpThrough(t, m, claims)
}
```

`runStepUpThrough` is the request-driving tail of today's `runStepUpWithEnrollment` (`step_up_test.go:161-177`), extracted verbatim:

```go
// runStepUpThrough drives one request with claims on the context through
// RequireStepUp(5m) on the given middleware and decodes the JSON body.
// Extracted from runStepUpWithEnrollment so the PR 3 policy matrix can
// wire its own StepUpPolicy (or none) before driving the gate.
func runStepUpThrough(t *testing.T, m *AuthMiddleware, claims *authModels.JWTClaims) (bool, int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/anything", nil)
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxClaims, claims))
	}
	rec := httptest.NewRecorder()
	called := false
	handler := m.RequireStepUp(5 * time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	handler.ServeHTTP(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return called, rec.Code, body
}
```

and `runStepUpWithEnrollment` (`:150-177`) becomes a one-line wrapper, so every existing caller keeps passing:

```go
// runStepUpWithEnrollment is the pre-PR-3 harness shape: enrollment
// lookup + a policy that always answers. Kept as a wrapper over
// runStepUpWithPolicy so its many existing callers stay untouched.
func runStepUpWithEnrollment(t *testing.T, claims *authModels.JWTClaims, hasFactor bool, lookupErr error, mfaRequired bool) (bool, int, map[string]any) {
	t.Helper()
	return runStepUpWithPolicy(t, claims, hasFactor, lookupErr, &fakeStepUpPolicy{required: mfaRequired})
}
```

Existing tests that reach the password-confirm envelope through `runStepUpWithEnrollment` (which always sets a policy) keep passing because the zero-value fake answers `(true, nil)`. Any pre-existing test that reaches the no-factor branch with NO policy set now expects 503 — update those assertions; the spec changed that outcome deliberately (F4).

In `backend/internal/core/tenant/handlers/admin_mfa_routes_test.go`, `mfaMasterSwitchOnPolicy` (`:53-58`) gains:

```go
func (mfaMasterSwitchOnPolicy) PasswordReauthAllowed(context.Context, string) (bool, error) {
	return true, nil
}
```

- [ ] **Step 2: Write the failing service tests**

Append to `backend/internal/core/auth/services/password_confirm_test.go` (reuse its existing fixture idiom — read the file's `TestConfirmPassword_HappyPath` for the constructor):

```go
// PR 3 §4.6: a password that is not an accepted credential cannot be a
// proof of presence either. Same 409 branch as "no password hash";
// break-glass is ignored; an unreadable policy is a 503, not a guess.
func TestConfirmPassword_MethodDisabledIsUnavailable(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
	env.policy.SetOperatorBreakGlass(true) // must be invisible here
	u := env.hashedUser("op@example.com", "correct horse battery staple")
	_, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct horse battery staple",
		[]string{"pwd"}, &authModels.DeviceInfo{}, &authModels.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if !errors.Is(err, ErrPasswordConfirmUnavailable) {
		t.Fatalf("want ErrPasswordConfirmUnavailable, got %v", err)
	}
}

func TestConfirmPassword_PolicyOutageIs503Shaped(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("op@example.com", "correct horse battery staple")
	env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
	_, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct horse battery staple",
		[]string{"pwd"}, &authModels.DeviceInfo{}, &authModels.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
	}
}
```

(if `password_confirm_test.go` uses its own fixture instead of `gatesEnv`, follow that file's builder and inject the policy the same way.)

And the adapter cases in `auth_policy_service_test.go`:

```go
func TestPasswordReauthAllowed_AdapterContract(t *testing.T) {
	ctx := context.Background()
	p := newPolicy(map[string]string{"passwordLoginEnabledAdmin": "false"})
	if ok, err := p.PasswordReauthAllowed(ctx, "operator"); err != nil || ok {
		t.Fatalf("operator off → (false, nil), got (%v, %v)", ok, err)
	}
	if ok, err := p.PasswordReauthAllowed(ctx, "client"); err != nil || !ok {
		t.Fatalf("client untouched → (true, nil), got (%v, %v)", ok, err)
	}
	if _, err := p.PasswordReauthAllowed(ctx, "service"); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("unknown audience must be an outage, got %v", err)
	}
	p.SetOperatorBreakGlass(true)
	if ok, _ := p.PasswordReauthAllowed(ctx, "operator"); ok {
		t.Fatal("break-glass must never count as a durable method")
	}
}
```

- [ ] **Step 3: Run to verify failure**

`go test ./internal/shared/middleware/ ./internal/core/auth/services/ ./internal/core/tenant/handlers/ -run 'StepUp|ConfirmPassword|PasswordReauth' -count=1`
Expected: compile FAILURE (interface method missing on fakes and on `AuthPolicyService`).

- [ ] **Step 4: Implement the middleware half**

`middleware/auth.go` — `StepUpPolicy` (`:68-79`) gains (this doc comment is the §4.12 contract for the interface — there is no middleware/CLAUDE.md):

```go
	// PasswordReauthAllowed reports whether a password may serve as the
	// re-authentication proof for the token's audience ("operator" |
	// "client"). False means the per-surface method is administratively
	// disabled (auth module passwordLoginEnabled{Admin,Client}); an error
	// means the policy could not be evaluated and the caller must answer
	// a retryable 503 auth.policy_unavailable — never mfa_enrollment_
	// required, which would misreport an outage as a user obligation.
	// The operator break-glass is deliberately invisible here: a
	// temporary override must not look like a durable login method.
	PasswordReauthAllowed(ctx context.Context, audience string) (bool, error)
```

`dispatchStepUpFailure` (`:987-1005`) — replace the tail:

```go
	// No factor enrolled. If the role requires MFA, the right answer is
	// "enroll first" — letting them bypass with a password would defeat
	// the policy. Otherwise the password-confirm fallback is offered ONLY
	// when the password is an accepted credential for this audience
	// (PR 3 §4.6): a disabled method also answers "enroll first", and an
	// unanswerable policy is a 503, never a fabricated obligation.
	if m.roleRequiresMFA(r.Context(), claims) {
		m.sendMFAEnrollmentRequired(w, r)
		return
	}
	if m.stepUpPolicy == nil {
		m.sendPolicyUnavailable(w)
		return
	}
	allowed, err := m.stepUpPolicy.PasswordReauthAllowed(r.Context(), claims.Audience)
	if err != nil {
		m.sendPolicyUnavailable(w)
		return
	}
	if !allowed {
		m.sendMFAEnrollmentRequired(w, r)
		return
	}
	m.sendPasswordConfirmRequired(w, r, maxAge)
```

New sender beside `sendPasswordConfirmRequired` (`:1132`):

```go
// sendPolicyUnavailable emits the 503 envelope for a sign-in policy that
// could not be evaluated (nil StepUpPolicy or a failed read). Retryable;
// deliberately NOT one of the step-up prompts — the frontend must show
// "try again shortly", not open a password modal or an enrollment nudge.
func (m *AuthMiddleware) sendPolicyUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusServiceUnavailable,
		"title":  "sign-in policy unavailable",
		"detail": "the sign-in policy could not be evaluated; try again shortly",
		"type":   "about:blank",
		"code":   "auth.policy_unavailable",
	})
}
```

- [ ] **Step 5: Implement the adapter and the service gate**

`auth_policy_service.go` — beside `PasswordLoginEnabled`:

```go
// PasswordReauthAllowed adapts strict PasswordLoginEnabled to the
// middleware's StepUpPolicy shape (§4.6). The name differs from the
// service method deliberately: Go has no overloading and the middleware
// cannot import PolicyAudience. Never break-glass: a temporary override
// is not a durable login method.
func (s *AuthPolicyService) PasswordReauthAllowed(ctx context.Context, audience string) (bool, error) {
	switch audience {
	case string(PolicyAudienceOperator):
		return s.PasswordLoginEnabled(ctx, PolicyAudienceOperator)
	case string(PolicyAudienceClient):
		return s.PasswordLoginEnabled(ctx, PolicyAudienceClient)
	}
	return false, fmt.Errorf("%w: unknown audience %q for password reauth", ErrAuthPolicyUnavailable, audience)
}
```

`password_auth_service.go` — `ConfirmPasswordWithSecurity`, immediately after the `PasswordHash == ""` branch (`:1180-1182`):

```go
	// PR 3 §4.6: a password the surface refuses cannot prove presence.
	// Strict read — break-glass is invisible here — and same 409 shape as
	// "no password hash"; a policy outage is a 503, never a guess.
	usable, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
	if err != nil {
		return nil, err
	}
	if !usable {
		return nil, ErrPasswordConfirmUnavailable
	}
```

- [ ] **Step 6: Same-commit docs**

The `StepUpPolicy` doc comment written in Step 4 IS the middleware contract (§4.12 — there is no middleware/CLAUDE.md). `backend/internal/core/auth/CLAUDE.md` — the step-up row: no-factor + method disabled → `mfa_enrollment_required`; policy error or missing wiring → 503 `auth.policy_unavailable`, never a fabricated obligation; `password-confirm` 409s a disabled method (same branch as no-hash), break-glass invisible.

- [ ] **Step 7: Run the suites + vet + commit**

Run: `go test ./internal/shared/middleware/ ./internal/core/auth/... ./internal/core/tenant/handlers/ -count=1` then `go vet ./...`
Expected: PASS (including every pre-existing step-up test — the zero-value fake keeps them green; any no-policy no-factor case now asserts 503).

```bash
git add backend/internal/core/auth/CLAUDE.md backend/internal/shared/middleware/auth.go backend/internal/shared/middleware/step_up_test.go backend/internal/core/tenant/handlers/admin_mfa_routes_test.go backend/internal/core/auth/services/auth_policy_service.go backend/internal/core/auth/services/auth_policy_service_test.go backend/internal/core/auth/services/password_auth_service.go backend/internal/core/auth/services/password_confirm_test.go
git commit -m "feat(auth): step-up honours the per-surface password policy; password-confirm refuses a disabled method" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 6: Unlink guard counts *usable* links; `AuthMethodsView` splits the password concept

§4.7 + §4.8 backend: the last-credential guard stops counting disabled/unconfigured providers and a disabled password as ways in, and the auth-methods view stops lying about what "usable" means. Frontend migration is Task 9.

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (`AuthService` interface setter; `wouldLockOutOAuthUnlink:596-615` re-signed; `AdminUnlinkOAuth:552-588`; `SelfUnlinkOAuth:623-652`; `GetUserAuthMethods:797-830`; new `usableProvidersForLinks`)
- Modify: `backend/internal/core/auth/models/auth_methods.go:10-30`
- Modify: `backend/internal/core/auth/module.go` (wire `SetProviderUsability` on both bundles)
- Modify: `backend/internal/core/auth/handlers/admin_user_auth_handler.go` (`mapAdminUserAuthError:180-201`), `backend/internal/core/auth/handlers/self_user_auth_handler.go` (`mapSelfAuthError:196+`)
- Test: `backend/internal/core/auth/services/auth_service_admin_unlink_test.go`, `auth_service_self_unlink_test.go`, `auth_service_get_methods_test.go` (extend)

**Interfaces:**
- Consumes: Task 1's `PasswordLoginEnabled`; PR 2's `OAuthWebProviderUsable` (resolver, `oauth_provider_usability.go:138`) via a module.go closure; `authService.policy` / `.audience` (`auth_service.go:296-303`).
- Produces:
  - `AuthService` interface + `authService` gain `SetProviderUsability(f func(ctx context.Context, audience PolicyAudience, provider iface.OAuthProvider) (bool, error))` (§4.7's seam — resolves `providerOn ∧ ProviderStructurallyConfigured` against the active snapshot without importing the resolver type).
  - `wouldLockOutOAuthUnlink(target, links, provider, passwordUsable bool, usableProviders map[iface.OAuthProvider]bool) (providerID string, locked bool, found bool)` — spec-verbatim semantics.
  - `models.AuthMethodsView` fields (§4.8, wire names exact): `HasPasswordSet` → `json:"hasPasswordSet"`, `PasswordUsableForLogin` → `json:"passwordUsableForLogin"`, `HasUsablePassword` → `json:"hasUsablePassword"` (Deprecated: alias of HasPasswordSet, removed after one release).

- [ ] **Step 1: Write the failing tests**

Append to `auth_service_admin_unlink_test.go` (its fixtures: `adminUnlinkUserFake`, `newAdminUnlinkSvc(fake) → &authService{userService: fake}`; extend the constructor so the new inputs are explicit):

```go
// newGuardedUnlinkSvc is newAdminUnlinkSvc plus the PR 3 policy inputs:
// a per-surface password policy and the provider-usability seam.
func newGuardedUnlinkSvc(fake *adminUnlinkUserFake, policyValues map[string]string, usable map[iface.OAuthProvider]bool, usabilityErr error) *authService {
	s := newAdminUnlinkSvc(fake)
	s.policy = &AuthPolicyService{cs: &stubReader{values: policyValues}}
	s.audience = PolicyAudienceOperator
	s.SetProviderUsability(func(_ context.Context, _ PolicyAudience, p iface.OAuthProvider) (bool, error) {
		if usabilityErr != nil {
			return false, usabilityErr
		}
		return usable[p], nil
	})
	return s
}

// PR 3 §4.7: the guard counts USABLE links, not active rows.
func TestWouldLockOutOAuthUnlink_UsableSemantics(t *testing.T) {
	google := iface.OAuthLink{Provider: "google", ProviderID: "g-1", IsActive: true}
	github := iface.OAuthLink{Provider: "github", ProviderID: "h-1", IsActive: true}
	userNoPw := &iface.User{UUID: "u", PasswordHash: ""}
	userPw := &iface.User{UUID: "u", PasswordHash: "argon2id$..."}

	cases := []struct {
		name           string
		target         *iface.User
		links          []iface.OAuthLink
		provider       iface.OAuthProvider
		passwordUsable bool
		usable         map[iface.OAuthProvider]bool
		wantLocked     bool
		wantFound      bool
	}{
		{"sole usable link, no password hash → locked",
			userNoPw, []iface.OAuthLink{google}, "google", true, map[iface.OAuthProvider]bool{"google": true}, true, true},
		{"sole usable link, hash present but method off → locked",
			userPw, []iface.OAuthLink{google}, "google", false, map[iface.OAuthProvider]bool{"google": true}, true, true},
		{"sole usable link, usable password → allowed",
			userPw, []iface.OAuthLink{google}, "google", true, map[iface.OAuthProvider]bool{"google": true}, false, true},
		{"target provider itself unusable → removable (not a credential)",
			userNoPw, []iface.OAuthLink{google}, "google", false, map[iface.OAuthProvider]bool{"google": false}, false, true},
		{"another USABLE link remains → allowed",
			userNoPw, []iface.OAuthLink{google, github}, "google", false, map[iface.OAuthProvider]bool{"google": true, "github": true}, false, true},
		{"the other link is unusable → still locked",
			userNoPw, []iface.OAuthLink{google, github}, "google", false, map[iface.OAuthProvider]bool{"google": true, "github": false}, true, true},
		{"inactive other link never counts",
			userNoPw, []iface.OAuthLink{google, {Provider: "github", ProviderID: "h-1", IsActive: false}}, "google", false, map[iface.OAuthProvider]bool{"google": true, "github": true}, true, true},
		{"provider not linked → not found",
			userNoPw, []iface.OAuthLink{google}, "discord", true, map[iface.OAuthProvider]bool{"google": true}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, locked, found := wouldLockOutOAuthUnlink(tc.target, tc.links, tc.provider, tc.passwordUsable, tc.usable)
			if found != tc.wantFound || locked != tc.wantLocked {
				t.Fatalf("got (locked=%v, found=%v), want (%v, %v)", locked, found, tc.wantLocked, tc.wantFound)
			}
		})
	}
}

func TestAdminUnlinkOAuth_UsableLinkGuard(t *testing.T) {
	seedTarget := func(fake *adminUnlinkUserFake, hash string) *iface.User {
		u := &iface.User{UUID: "target-uuid", PasswordHash: hash,
			OAuthLinks: []iface.OAuthLink{{Provider: "google", ProviderID: "g-1", IsActive: true}}}
		fake.seed(u)
		return u
	}
	t.Run("password off makes the sole usable link last_credential", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, map[string]string{"passwordLoginEnabledAdmin": "false"},
			map[iface.OAuthProvider]bool{"google": true}, nil)
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
			t.Fatalf("want ErrLastCredentialRemoval, got %v", err)
		}
	})
	t.Run("unusable target link is removable even with password off", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "")
		svc := newGuardedUnlinkSvc(fake, map[string]string{"passwordLoginEnabledAdmin": "false"},
			map[iface.OAuthProvider]bool{"google": false}, nil)
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); err != nil {
			t.Fatalf("disabled link is not a credential; want removal, got %v", err)
		}
	})
	t.Run("password policy uncertainty refuses with the policy sentinel", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)
		svc.policy = &AuthPolicyService{cs: &stubReader{rawErr: errors.New("mongo down")}}
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("usability uncertainty refuses rather than counting the link", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, nil, nil, fmt.Errorf("%w: undecryptable secret", ErrAuthPolicyUnavailable))
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("missing usability wiring is an outage, not a pass", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newAdminUnlinkSvc(fake)
		svc.policy = &AuthPolicyService{cs: &stubReader{values: map[string]string{}}}
		svc.audience = PolicyAudienceOperator
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}
```

Append to `auth_service_self_unlink_test.go` (same package — `newGuardedUnlinkSvc` and the fakes are shared; `SelfUnlinkOAuth` short-circuits before persistence, so `fake.removedCall` is the mutation probe):

```go
// PR 3 §4.7: the self-service guard counts usable links too.
func TestSelfUnlinkOAuth_UsableLinkGuard(t *testing.T) {
	seed := func(fake *adminUnlinkUserFake, hash string) {
		fake.seed(&iface.User{UUID: "u-1", PasswordHash: hash,
			OAuthLinks: []iface.OAuthLink{{Provider: "google", ProviderID: "g-1", IsActive: true}}})
	}
	t.Run("password off makes the sole usable link last_credential", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seed(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, map[string]string{"passwordLoginEnabledAdmin": "false"},
			map[iface.OAuthProvider]bool{"google": true}, nil)
		if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
			t.Fatalf("want ErrLastCredentialRemoval, got %v", err)
		}
		if fake.removedCall != nil {
			t.Errorf("guard must short-circuit before persistence; got %+v", fake.removedCall)
		}
	})
	t.Run("unusable target link is removable even with no password", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seed(fake, "")
		svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": false}, nil)
		if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
			t.Fatalf("disabled link is not a credential; want removal, got %v", err)
		}
		if fake.removedCall == nil || fake.removedCall.providerID != "g-1" {
			t.Fatalf("expected RemoveOAuthLinkFromUser(g-1); got %+v", fake.removedCall)
		}
	})
	t.Run("usability uncertainty refuses with the policy sentinel", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seed(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, nil, nil, fmt.Errorf("%w: undecryptable secret", ErrAuthPolicyUnavailable))
		if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
		if fake.removedCall != nil {
			t.Errorf("uncertainty must not mutate; got %+v", fake.removedCall)
		}
	})
}
```

The pre-existing cases that reach the guard swap `newAdminUnlinkSvc(fake)` for the guarded constructor — same assertions, unchanged subjects, all-usable input (`nil` policyValues = absent keys = legacy password-on):

| Test | New constructor call |
|---|---|
| `TestAdminUnlinkOAuth_Success` (`:156`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)` |
| `TestAdminUnlinkOAuth_LastCredentialLockout` (`:203`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)` |
| `TestAdminUnlinkOAuth_LastOAuthLinkButPasswordSet` (`:221`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)` |
| `TestAdminUnlinkOAuth_ProviderNotLinked` (`:239`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)` |
| `TestSelfUnlinkOAuth_Success` (`self:17`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)` |
| `TestSelfUnlinkOAuth_LastCredentialLockout` (`self:40`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)` |
| `TestSelfUnlinkOAuth_ProviderNotLinked` (`self:57`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)` |
| `TestSelfUnlinkOAuth_SelfActionAllowed` (`self:75`) | `newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)` |

`TestAdminUnlinkOAuth_SelfAction` (`:185`), `TestAdminUnlinkOAuth_RejectsEmptyArgs` (`:253`) and `TestSelfUnlinkOAuth_RejectsEmptyUUID` (`self:92`) keep the bare `newAdminUnlinkSvc(fake)`: they short-circuit (self-action check, empty-args check) before the new policy/usability prelude runs.

Append to `auth_service_get_methods_test.go` (update `newGetMethodsSvc` to take `policyValues map[string]string` and set `policy`+`audience` like `newGuardedUnlinkSvc`; existing calls pass `nil` for legacy-true):

```go
// PR 3 §4.8: the view separates "hash present" from "method usable".
func TestGetUserAuthMethods_PasswordSplit(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{UUID: "u1", Email: "u1@example.com", Role: "operator", PasswordHash: "argon2id$..."})

	on := newGetMethodsSvc(fake, newFakeFactorRepo(), nil)
	view, err := on.GetUserAuthMethods(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if !view.HasPasswordSet || !view.PasswordUsableForLogin || !view.HasUsablePassword {
		t.Fatalf("method on: want all three true, got %+v", view)
	}

	off := newGetMethodsSvc(fake, newFakeFactorRepo(), map[string]string{"passwordLoginEnabledAdmin": "false"})
	view, err = off.GetUserAuthMethods(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if !view.HasPasswordSet || view.PasswordUsableForLogin {
		t.Fatalf("method off: hash stays set, usable must be false, got %+v", view)
	}
	if !view.HasUsablePassword {
		t.Fatal("deprecated alias must mirror hasPasswordSet, not usability")
	}
}

func TestGetUserAuthMethods_PolicyOutageIs503Shaped(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{UUID: "u1", Email: "u1@example.com", Role: "operator", PasswordHash: "argon2id$..."})
	svc := newGetMethodsSvc(fake, newFakeFactorRepo(), nil)
	svc.policy = &AuthPolicyService{cs: &stubReader{rawErr: errors.New("mongo down")}}
	if _, err := svc.GetUserAuthMethods(context.Background(), "u1"); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/core/auth/services/ -run 'Unlink|GetUserAuthMethods|WouldLockOut' -count=1`
Expected: compile FAILURE (new signature, setter, fields).

- [ ] **Step 3: Implement the model split**

`models/auth_methods.go` — replace the password fields (`:11-16`):

```go
	// HasPasswordSet is true iff the user has a non-empty password hash.
	// Presence only: whether the surface currently ACCEPTS the password
	// is PasswordUsableForLogin.
	HasPasswordSet bool `json:"hasPasswordSet"`
	// PasswordUsableForLogin is HasPasswordSet ∧ the per-surface
	// passwordLoginEnabled policy for this user's tier. Unlink and reset
	// decisions read this; "set / change / last updated" UI reads
	// HasPasswordSet.
	PasswordUsableForLogin bool `json:"passwordUsableForLogin"`
	// HasUsablePassword is the pre-toggle name.
	//
	// Deprecated: alias of HasPasswordSet (NOT of usability), kept one
	// release for wire compatibility; remove per spec §8.
	HasUsablePassword bool       `json:"hasUsablePassword"`
	PasswordUpdatedAt *time.Time `json:"passwordUpdatedAt,omitempty"`
```

- [ ] **Step 4: Implement the guard and the seam**

`auth_service.go`:

1. Interface (next to `SetPolicy`/`SetAudience`, `:243-249` area) + struct field + setter:

```go
	// SetProviderUsability wires the per-audience "is this provider a
	// usable web login method" resolver (§4.7): providerOn ∧ structurally
	// configured against the active snapshot, without this package
	// importing the resolver type. Unlink guards refuse with a policy
	// error when it is missing — an uncertain link must not be counted.
	SetProviderUsability(f func(ctx context.Context, audience PolicyAudience, provider iface.OAuthProvider) (bool, error))
```

```go
	providerUsability func(ctx context.Context, audience PolicyAudience, provider iface.OAuthProvider) (bool, error)
```

```go
func (s *authService) SetProviderUsability(f func(ctx context.Context, audience PolicyAudience, provider iface.OAuthProvider) (bool, error)) {
	s.providerUsability = f
}

// usableProvidersForLinks precomputes usability for every ACTIVE link's
// provider before any mutation (§4.7). Any config read, decrypt or parse
// error — or missing wiring — refuses with the policy sentinel rather
// than counting an uncertain link.
func (s *authService) usableProvidersForLinks(ctx context.Context, links []iface.OAuthLink) (map[iface.OAuthProvider]bool, error) {
	if s.providerUsability == nil {
		return nil, fmt.Errorf("%w: provider usability not wired", ErrAuthPolicyUnavailable)
	}
	out := make(map[iface.OAuthProvider]bool, len(links))
	for _, link := range links {
		if !link.IsActive {
			continue
		}
		if _, seen := out[link.Provider]; seen {
			continue
		}
		ok, err := s.providerUsability(ctx, s.audience, link.Provider)
		if err != nil {
			return nil, err
		}
		out[link.Provider] = ok
	}
	return out, nil
}
```

2. The helper, spec-verbatim (replacing `:596-615`):

```go
// wouldLockOutOAuthUnlink computes the shared lockout decision used by
// AdminUnlinkOAuth and SelfUnlinkOAuth, counting USABLE credentials
// (§4.7): a link whose provider is disabled or structurally incomplete
// is not a way in, and neither is a password the surface refuses.
//
//	targetUsable    := target link IsActive ∧ usableProviders[provider]
//	remainingUsable := count(other links: IsActive ∧ usableProviders[Provider])
//	locked          := targetUsable ∧ (¬passwordUsable ∨ PasswordHash == "") ∧ remainingUsable == 0
//
// Removing an unusable target link is always allowed. found=false keeps
// meaning "provider not linked" (404).
func wouldLockOutOAuthUnlink(target *iface.User, links []iface.OAuthLink,
	provider iface.OAuthProvider, passwordUsable bool,
	usableProviders map[iface.OAuthProvider]bool) (providerID string, locked bool, found bool) {
	if target == nil {
		return "", false, false
	}
	targetActive := false
	remainingUsable := 0
	for _, link := range links {
		if link.Provider == provider && providerID == "" {
			providerID = link.ProviderID
			targetActive = link.IsActive
			continue
		}
		if link.IsActive && usableProviders[link.Provider] {
			remainingUsable++
		}
	}
	found = providerID != ""
	if !found {
		return "", false, false
	}
	targetUsable := targetActive && usableProviders[provider]
	locked = targetUsable && (!passwordUsable || target.PasswordHash == "") && remainingUsable == 0
	return providerID, locked, true
}
```

3. Both callers gain the prelude between the links read and the guard. In `AdminUnlinkOAuth` (`:571`, its user variable is `target`):

```go
	// §4.7: resolve what actually counts as a way in BEFORE mutating.
	// Strict reads — break-glass never counts as a lasting credential,
	// and any uncertainty refuses with 503 rather than guessing.
	passwordUsable, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
	if err != nil {
		return err
	}
	usable, err := s.usableProvidersForLinks(ctx, links)
	if err != nil {
		return err
	}
	providerID, locked, found := wouldLockOutOAuthUnlink(target, links, provider, passwordUsable, usable)
```

and in `SelfUnlinkOAuth` (`:637`, its user variable is `user`):

```go
	// §4.7: resolve what actually counts as a way in BEFORE mutating.
	// Strict reads — break-glass never counts as a lasting credential,
	// and any uncertainty refuses with 503 rather than guessing.
	passwordUsable, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
	if err != nil {
		return err
	}
	usable, err := s.usableProvidersForLinks(ctx, links)
	if err != nil {
		return err
	}
	providerID, locked, found := wouldLockOutOAuthUnlink(user, links, provider, passwordUsable, usable)
```

4. `GetUserAuthMethods` (`:809-817`): the view literal sets all three fields, then the strict read:

```go
	// §4.8: presence and usability are different facts. The strict read
	// fails the whole call (503) rather than guessing — an outage must
	// not render an unlock the backend would refuse.
	passwordUsable, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
	if err != nil {
		return nil, err
	}
	view := &models.AuthMethodsView{
		HasPasswordSet:         user.PasswordHash != "",
		PasswordUsableForLogin: user.PasswordHash != "" && passwordUsable,
		HasUsablePassword:      user.PasswordHash != "", // Deprecated alias of HasPasswordSet
		PasswordUpdatedAt:      user.PasswordUpdatedAt,
		EmailVerified:          user.EmailVerified,
		LastLoginAt:            user.LastLogin,
		MFAGraceStartedAt:      user.MFAGraceStartedAt,
		MFAFactors:             []models.MFAFactorView{},
		OAuthProviders:         []models.OAuthProviderView{},
	}
```

5. Error mapping: `mapAdminUserAuthError` (`:184`) and `mapSelfAuthError` gain, as the first `errors.Is` case:

```go
	case errors.Is(err, services.ErrAuthPolicyUnavailable):
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
```

6. `module.go` — after the client bundle exists (below `:1271`), one closure wired into both:

```go
	// §4.7: the unlink guards count usable links through the same strict
	// one-read resolver the web flow uses; the closure keeps the resolver
	// type out of the services package.
	providerUsability := func(ctx context.Context, audience services.PolicyAudience, p iface.OAuthProvider) (bool, error) {
		_, ok, err := oauthResolver.OAuthWebProviderUsable(ctx, audience, models.OAuthProvider(string(p)))
		if err != nil {
			return false, err
		}
		return ok, nil
	}
	opBundle.authService.SetProviderUsability(providerUsability)
	clBundle.authService.SetProviderUsability(providerUsability)
```

(`clBundle` is the client bundle's variable at that site — `module.go:1221`.)

- [ ] **Step 5: Fix the ripple**

Verified at `ca24e614` — the complete ripple:

- `wouldLockOutOAuthUnlink` has NO reference outside `auth_service.go` and this task's tests (grep-verified), so the re-signature rips nothing else.
- The unlink-suite constructor swaps are exactly the Step 1 table (eight guarded, three untouched).
- `newGetMethodsSvc` has six existing call sites, all in `auth_service_get_methods_test.go` — lines 38 (`_PasswordOnly`), 83 (`_TOTPEnrolled`), 130 (`_BothFactors`), 177 (`_OAuthOnly`), 199 (`_UnknownUser`), 208 (`_RejectsEmptyTarget`); each gains the third argument `nil` (legacy-true policy). `_UnknownUser` and `_RejectsEmptyTarget` fail before the policy read (user lookup / empty-target guard), so `nil` is inert there too.

Run `go build ./... && go vet ./...` and expect zero fallout beyond these files.

- [ ] **Step 6: Same-commit docs**

`backend/internal/core/auth/CLAUDE.md` — the unlink-guard row (usable links only: `targetUsable ∧ (¬passwordUsable ∨ no hash) ∧ remainingUsable == 0`; a disabled/unconfigured link is removable and never satisfies the guard; any config uncertainty → 503 via the `SetProviderUsability` seam) and the `AuthMethodsView` row (`hasPasswordSet` / `passwordUsableForLogin`; `hasUsablePassword` deprecated alias for one release).

- [ ] **Step 7: Run + vet + commit**

Run: `go test ./internal/core/auth/... -count=1` then `go vet ./...`
Expected: PASS.

```bash
git add backend/internal/core/auth/CLAUDE.md backend/internal/core/auth/services/auth_service.go backend/internal/core/auth/models/auth_methods.go backend/internal/core/auth/module.go backend/internal/core/auth/handlers/admin_user_auth_handler.go backend/internal/core/auth/handlers/self_user_auth_handler.go backend/internal/core/auth/services/auth_service_admin_unlink_test.go backend/internal/core/auth/services/auth_service_self_unlink_test.go backend/internal/core/auth/services/auth_service_get_methods_test.go
git commit -m "feat(auth): unlink guard counts usable links only; auth-methods view splits password presence from usability" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 7: `/policy` exposes the persisted state and the operator break-glass display flag

§4.9: the SPAs learn the persisted per-surface method state (nullable only in the operator emergency case) and whether the emergency login form should render. OpenAPI regenerated.

**Files:**
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (`GetAuthPolicyResponse:338-352`, `GetAuthPolicy:362-379`)
- Modify: `backend/openapi/enterprise.json` (regenerated)
- Test: `backend/internal/core/auth/handlers/auth_policy_endpoint_test.go` (new)

**Interfaces:**
- Consumes: Task 1's `PasswordLoginEnabled` + `OperatorBreakGlassConfigured`; `policyAudience()` (`auth_handler.go:140-145`); `services.AudienceClient` (`jwt_service.go:232`).
- Produces (SPA contract, §4.9): response body gains `passwordLoginEnabled *bool` and `passwordLoginBreakGlassEffective bool`. Truth table — read OK → non-null value, flag true only on the operator endpoint when the override is set AND the value is false; read error + operator override → 200 with `passwordLoginEnabled: null`, flag true; read error otherwise → **503** `auth.policy_unavailable`. The client endpoint never sets the flag.

- [ ] **Step 1: Write the failing handler tests**

Create `backend/internal/core/auth/handlers/auth_policy_endpoint_test.go` (same package; `AuthHandler` fields are unexported, so build the fixture through the exported setters):

```go
package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

// policyHandlerFor builds the minimal AuthHandler surface GetAuthPolicy
// touches: the policy reader plus the tier string. Every other field
// stays zero — the endpooint reads nothing else.
func policyHandlerFor(t *testing.T, p *services.AuthPolicyService, tier string) *AuthHandler {
	t.Helper()
	h := &AuthHandler{}
	h.SetPolicy(p)
	h.SetTier(tier)
	return h
}

func TestGetAuthPolicy_PasswordLoginFields(t *testing.T) {
	ctx := context.Background()

	t.Run("persisted true is non-null, flag false even with the env set", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"})
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("GetAuthPolicy: %v", err)
		}
		if resp.Body.PasswordLoginEnabled == nil || !*resp.Body.PasswordLoginEnabled {
			t.Fatalf("want non-null true, got %v", resp.Body.PasswordLoginEnabled)
		}
		if resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("a stored true needs no override; flag must be false")
		}
	})
	t.Run("persisted false + operator override → non-null false, flag true", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("GetAuthPolicy: %v", err)
		}
		if resp.Body.PasswordLoginEnabled == nil || *resp.Body.PasswordLoginEnabled {
			t.Fatalf("want non-null false, got %v", resp.Body.PasswordLoginEnabled)
		}
		if !resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("override over a stored false must flag the emergency form")
		}
	})
	t.Run("read error without override → 503", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		_, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
	})
	t.Run("read error + operator override → 200 null + flag", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("emergency read must answer 200, got %v", err)
		}
		if resp.Body.PasswordLoginEnabled != nil {
			t.Fatalf("unknown persisted state must be null, got %v", *resp.Body.PasswordLoginEnabled)
		}
		if !resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("the emergency form must be reachable")
		}
	})
	t.Run("client endpoint never exposes the override", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledClient": "false"})
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceClient).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("GetAuthPolicy: %v", err)
		}
		if resp.Body.PasswordLoginEnabled == nil || *resp.Body.PasswordLoginEnabled {
			t.Fatalf("want non-null false, got %v", resp.Body.PasswordLoginEnabled)
		}
		if resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("client endpoint must never flag the override")
		}
		pErr := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		pErr.SetOperatorBreakGlass(true)
		_, err = policyHandlerFor(t, pErr, services.AudienceClient).GetAuthPolicy(ctx, nil)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
	})
}
```

(`services.NewAuthPolicyServiceForTest` / `NewAuthPolicyServiceForTestErr` landed in Task 4 step 5.7; `auth_policy_service_test.go`'s `stubReader` stays for in-package tests — it has richer knobs.)

- [ ] **Step 2: Run to verify failure**

`go test ./internal/core/auth/handlers/ -run TestGetAuthPolicy -count=1`
Expected: compile FAILURE — the two response-body fields are undefined (the test constructors already exist from Task 4).

- [ ] **Step 3: Implement**

`GetAuthPolicyResponse` body gains (after `MFAEnabled`):

```go
		PasswordLoginEnabled             *bool `json:"passwordLoginEnabled" doc:"Persisted per-surface email/password policy. Null ONLY when the policy store is unreadable while the operator break-glass is active — the emergency case; every ordinary read is non-null."`
		PasswordLoginBreakGlassEffective bool  `json:"passwordLoginBreakGlassEffective" doc:"Operator endpoint only: the boot-time break-glass override is set AND the persisted policy is false or unreadable, so the console must render the labelled emergency login form. Always false on the client endpoint."`
```

`GetAuthPolicy` gains, before `return resp, nil`:

```go
	// §4.9: the new pair is STRICT — the display contract must never show
	// a working password form because a read failed. A read error without
	// the operator override is a retryable 503; with it, the emergency
	// form must stay reachable, so the unknown state is explicit null.
	operatorSurface := audience == services.PolicyAudienceOperator
	enabled, perr := h.policy.PasswordLoginEnabled(ctx, audience)
	switch {
	case perr == nil:
		resp.Body.PasswordLoginEnabled = &enabled
		if operatorSurface && !enabled && h.policy.OperatorBreakGlassConfigured() {
			resp.Body.PasswordLoginBreakGlassEffective = true
		}
	case operatorSurface && h.policy.OperatorBreakGlassConfigured():
		resp.Body.PasswordLoginBreakGlassEffective = true
	default:
		return nil, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
```

(`h.policy` may be nil in embedded setups: `PasswordLoginEnabled` is nil-receiver-safe and errors, and `OperatorBreakGlassConfigured` is nil-safe false → 503, fail closed. `errcode` is already imported in this file.)

- [ ] **Step 4: Run + regenerate OpenAPI**

`go test ./internal/core/auth/... -count=1` — PASS.
`grep "^ENV=" /home/tore/orkestra/docker/.env` then `make -C /home/tore/orkestra/backend openapi-dump`; verify with `git diff --stat backend/openapi/enterprise.json` that only the intended operations changed (`/policy` pair on both tiers, auth-methods fields from Task 6, the 409s carry no schema change).

- [ ] **Step 5: Same-commit docs**

`backend/internal/core/auth/CLAUDE.md` — the `/policy` row: `passwordLoginEnabled` (nullable only in the operator emergency case) + `passwordLoginBreakGlassEffective` (operator endpoint only), read error without break-glass → 503; the pre-existing fields keep their permissive reads (D12).

- [ ] **Step 6: Vet + commit**

```bash
git add backend/internal/core/auth/CLAUDE.md backend/internal/core/auth/handlers/auth_handler.go backend/internal/core/auth/handlers/auth_policy_endpoint_test.go backend/openapi/enterprise.json
git commit -m "feat(auth): /policy exposes persisted password-login state and the operator break-glass display flag" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 8: frontend-admin — login/register/forgot gating, the emergency form, the no-method alert

G5 for the operator console's unauthenticated surface: hide the password UI on persisted false/null, render the labelled emergency form only under break-glass, and tell the visitor when no sign-in method resolves at all. `EmailPasswordForm` migrates to react-hook-form + yup as it is reworked (deviation 8).

Pre-flight (orkestra-frontend-admin skill): production precedent = `src/components/authentication/{RegisterForm,SocialLoginForm}.tsx` (policy-gated auth forms, this session's shape); reference = `src/reference/components/forms/FormValidation.tsx` (RHF + yup idiom); primitives = React Bootstrap `Form.*`/`Alert`, `orkestra-primary` variants, `t()` with EN+IT parity. The executor re-reads those files before writing JSX.

**Files:**
- Modify: `frontend-admin/src/store/api/authApi.ts:143-147` (`AuthPolicy` type) + `:219-231` (fallback)
- Modify: `frontend-admin/src/components/authentication/EmailPasswordForm.tsx` (rework), `RegisterForm.tsx`, `ForgotPasswordForm.tsx`, `Login.tsx`, `SocialLoginForm.tsx`
- Modify: `frontend-admin/src/test/handlers.ts` (shared `operatorPolicyHandler`), `frontend-admin/src/locales/en.json`, `it.json`, `frontend-admin/CLAUDE.md` (authentication paragraph)
- Test: `EmailPasswordForm.test.tsx`, `SocialLoginForm.test.tsx` (extend), `Login.test.tsx`, `RegisterForm.test.tsx`, `ForgotPasswordForm.test.tsx` (new)

**Interfaces:**
- Consumes: Task 7's `/policy` fields.
- Produces:
  - `AuthPolicy` gains `passwordLoginEnabled: boolean | null; passwordLoginBreakGlassEffective: boolean;` — the queryFn error fallback adds `passwordLoginEnabled: true, passwordLoginBreakGlassEffective: false` (fail-open DISPLAY on transport failure only, §5 #15; a served `null` is never coerced to true).
  - Gating predicate used by all four components, in one exported helper (put it in `authApi.ts` next to the type so the definition can't drift per file):

```typescript
// Whether the persisted policy allows rendering password-credential UI.
// A served null (emergency-unknown state) hides it — only the emergency
// login form may render then, and only under the break-glass flag.
export const passwordUiVisible = (policy: AuthPolicy | undefined): boolean =>
  policy ? policy.passwordLoginEnabled === true : true;
```

  - `SocialLoginForm` prop `onProvidersResolved?: (count: number) => void` (deviation 9), fired from an effect only when the provider query succeeds (never on error), with the count of KNOWN providers after the `PROVIDER_META` filter.

- [ ] **Step 1: The shared policy stub, then the failing tests**

First the stub — WITHOUT it every new test file below fails at import resolution, and Step 2 must fail on BEHAVIOUR, not on a missing module. `frontend-admin/src/test/handlers.ts`, next to the existing helpers (five test files consume it: `EmailPasswordForm.test.tsx`, `Login.test.tsx`, `RegisterForm.test.tsx`, `ForgotPasswordForm.test.tsx` here, `PasswordTab.test.tsx` in Task 9 — the components behind them all fetch `/v1/auth/operator/policy`, and MSW runs with `onUnhandledRequest: 'error'`):

```typescript
// Operator /policy with everything enabled; per-test overrides flip the
// PR 3 password-login fields.
export const operatorPolicyHandler = (
  overrides: Record<string, unknown> = {}
) =>
  http.get(url('/v1/auth/operator/policy'), () =>
    HttpResponse.json({
      registrationEnabled: true,
      loginEnabled: true,
      passwordMinLength: 10,
      passwordLoginEnabled: true,
      passwordLoginBreakGlassEffective: false,
      ...overrides
    })
  );
```

Then the tests. `EmailPasswordForm.test.tsx` — the file's `policyOk` handler gains the two new fields (`passwordLoginEnabled: true, passwordLoginBreakGlassEffective: false`); the new suite imports the shared stub (`import { operatorPolicyHandler } from 'test/handlers';`):

```typescript
describe('password-login policy gating (PR 3 §4.10)', () => {
  it('renders nothing when the persisted method is off', async () => {
    server.use(operatorPolicyHandler({ passwordLoginEnabled: false }));
    renderForm();
    await waitFor(() =>
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument()
    );
    expect(
      screen.queryByRole('button', { name: /sign in/i })
    ).not.toBeInTheDocument();
  });

  it('renders nothing on the emergency-null state without break-glass', async () => {
    server.use(operatorPolicyHandler({ passwordLoginEnabled: null }));
    renderForm();
    await waitFor(() =>
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument()
    );
  });

  it('break-glass renders a labelled emergency form without forgot/register CTAs', async () => {
    server.use(
      operatorPolicyHandler({
        passwordLoginEnabled: false,
        passwordLoginBreakGlassEffective: true
      })
    );
    renderForm();
    // auth.pages.passwordBreakGlassActive (added in Step 7)
    expect(
      await screen.findByText(/emergency access mode/i)
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    // existing copy: auth.forgotPassword = "Forgot password?" (en.json:215),
    // auth.createOne = "Create one" (en.json:219)
    expect(screen.queryByText(/forgot password\?/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/create one/i)).not.toBeInTheDocument();
  });

  it('policy transport failure keeps the form (display fail-open)', async () => {
    server.use(
      http.get('*/v1/auth/operator/policy', () => HttpResponse.error())
    );
    renderForm();
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
  });
});
```

(every matcher above is pinned to verified copy: `auth.pages.passwordBreakGlassActive` from Step 7, `auth.forgotPassword` = "Forgot password?" at `en.json:215`, `auth.createOne` = "Create one" at `en.json:219`.)

`SocialLoginForm.test.tsx` — append (the file already stubs `initiateSocialLogin` via `vi.mock` and overrides `*/v1/auth/operator/providers` per test):

```tsx
describe('onProvidersResolved (PR 3 §4.10)', () => {
  it('fires with the filtered count when the query resolves', async () => {
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ providers: ['google', 'github', 'not-a-provider'] })
      )
    );
    const resolved = vi.fn();
    renderWithProviders(<SocialLoginForm onProvidersResolved={resolved} />);
    await screen.findByRole('button', { name: /google/i });
    await waitFor(() => expect(resolved).toHaveBeenCalledWith(2));
  });

  it('fires with 0 on a resolved-empty list', async () => {
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ providers: [] })
      )
    );
    const resolved = vi.fn();
    renderWithProviders(<SocialLoginForm onProvidersResolved={resolved} />);
    await waitFor(() => expect(resolved).toHaveBeenCalledWith(0));
  });

  it('never fires on a query error — an outage is not "no method"', async () => {
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ detail: 'boom' }, { status: 503 })
      )
    );
    const resolved = vi.fn();
    renderWithProviders(<SocialLoginForm onProvidersResolved={resolved} />);
    // auth.social.loadError, en.json:340: "Could not load the social
    // sign-in options. Please try again."
    await screen.findByText(/could not load the social sign-in options/i);
    expect(resolved).not.toHaveBeenCalled();
  });
});
```

New `Login.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { operatorPolicyHandler } from 'test/handlers';
import Login from './Login';

vi.mock('utils/socialAuthUtils', async () => {
  const actual = await vi.importActual<typeof import('utils/socialAuthUtils')>(
    'utils/socialAuthUtils'
  );
  return { ...actual, initiateSocialLogin: vi.fn().mockResolvedValue(undefined) };
});

const providersWith = (providers: string[]) =>
  http.get('*/v1/auth/operator/providers', () =>
    HttpResponse.json({ providers })
  );

describe('Login no-method alert (PR 3 §4.10)', () => {
  it('renders the alert when password is off and providers resolve empty', async () => {
    server.use(operatorPolicyHandler({ passwordLoginEnabled: false }), providersWith([]));
    renderWithProviders(<Login />);
    expect(
      await screen.findByText(/no sign-in method/i)
    ).toBeInTheDocument();
  });

  it('no alert when a provider resolves', async () => {
    server.use(operatorPolicyHandler({ passwordLoginEnabled: false }), providersWith(['google']));
    renderWithProviders(<Login />);
    await screen.findByRole('button', { name: /google/i });
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('a provider-query error shows the retryable error, never the alert', async () => {
    server.use(
      operatorPolicyHandler({ passwordLoginEnabled: false }),
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ detail: 'boom' }, { status: 503 })
      )
    );
    renderWithProviders(<Login />);
    // Anchor on the SETTLED error state first (auth.social.loadError,
    // en.json:340) — asserting the alert's absence before the query
    // resolves would pass vacuously.
    await screen.findByText(/could not load the social sign-in options/i);
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('no alert while password is on, even with zero providers', async () => {
    server.use(operatorPolicyHandler({}), providersWith([]));
    renderWithProviders(<Login />);
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('no alert under break-glass — the emergency form is a method', async () => {
    server.use(
      operatorPolicyHandler({
        passwordLoginEnabled: false,
        passwordLoginBreakGlassEffective: true
      }),
      providersWith([])
    );
    renderWithProviders(<Login />);
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });
});
```

New `RegisterForm.test.tsx` and `ForgotPasswordForm.test.tsx` — the two direct-navigation gates (§4.10: "direct navigation must not show a working form, including during break-glass"):

```tsx
// RegisterForm.test.tsx
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { operatorPolicyHandler } from 'test/handlers';
import RegisterForm from './RegisterForm';

describe('RegisterForm password-method gate (PR 3 §4.10)', () => {
  it.each([
    ['persisted false', { passwordLoginEnabled: false }],
    ['emergency null', { passwordLoginEnabled: null }],
    [
      'break-glass does not reopen it',
      { passwordLoginEnabled: false, passwordLoginBreakGlassEffective: true }
    ]
  ])('renders only the disabled alert — %s', async (_name, overrides) => {
    server.use(operatorPolicyHandler(overrides));
    renderWithProviders(<RegisterForm />);
    expect(
      await screen.findByText(/email\/password sign-in is disabled/i)
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /create|register/i })
    ).not.toBeInTheDocument();
  });

  it('renders the working form when the method is on', async () => {
    server.use(operatorPolicyHandler({}));
    renderWithProviders(<RegisterForm />);
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
  });
});
```

```tsx
// ForgotPasswordForm.test.tsx
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { operatorPolicyHandler } from 'test/handlers';
import ForgotPasswordForm from './ForgotPasswordForm';

describe('ForgotPasswordForm password-method gate (PR 3 §4.10)', () => {
  it.each([
    ['persisted false', { passwordLoginEnabled: false }],
    ['emergency null', { passwordLoginEnabled: null }],
    [
      'break-glass does not reopen it',
      { passwordLoginEnabled: false, passwordLoginBreakGlassEffective: true }
    ]
  ])('renders only the disabled alert — %s', async (_name, overrides) => {
    server.use(operatorPolicyHandler(overrides));
    renderWithProviders(<ForgotPasswordForm />);
    expect(
      await screen.findByText(/email\/password sign-in is disabled/i)
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /send|submit|reset/i })
    ).not.toBeInTheDocument();
  });

  it('renders the working form when the method is on', async () => {
    server.use(operatorPolicyHandler({}));
    renderWithProviders(<ForgotPasswordForm />);
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
  });
});
```

(every gated-component test imports the ONE `operatorPolicyHandler` from `test/handlers.ts` — declared at the top of Step 1; no test file declares its own policy stub. Five consumers in total: the four files here plus Task 9's `PasswordTab.test.tsx`.)

- [ ] **Step 2: Run to verify failure**

`npx vitest run src/components/authentication`
Expected: FAIL — new fields/props/copy absent.

- [ ] **Step 3: Types + fallback**

`authApi.ts`:

```typescript
export interface AuthPolicy {
  registrationEnabled: boolean;
  loginEnabled: boolean;
  passwordMinLength: number;
  // Persisted per-surface email/password policy. null is the emergency
  // state: the store was unreadable while the operator break-glass is
  // active. Only a literal true may render ordinary password UI.
  passwordLoginEnabled: boolean | null;
  // Operator surface only: render the labelled emergency login form.
  passwordLoginBreakGlassEffective: boolean;
}
```

fallback (`:224-229`) gains `passwordLoginEnabled: true, passwordLoginBreakGlassEffective: false`, and `passwordUiVisible` is exported beside the type (code in Interfaces above).

- [ ] **Step 4: Rework `EmailPasswordForm`**

The complete replacement (`frontend-admin/src/components/authentication/EmailPasswordForm.tsx` — behaviour, copy and the MFA `location.state` hand-off preserved from the current file; only form state moves to RHF + yup and the two policy gates are added; the submit button keeps `variant="primary"` for visual parity with the sibling auth forms on the same layout — D8 scopes the rework to form state, not restyling):

```tsx
import { useState } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useAppDispatch } from 'store/hooks';
import {
  passwordUiVisible,
  useGetAuthPolicyQuery,
  useLoginMutation
} from 'store/api/authApi';
import { login as loginAction } from 'store/slices/authSlice';
import {
  DEFAULT_POST_LOGIN,
  locationToReturnTo,
  sanitizeReturnTo
} from 'utils/returnTo';

const schema = yup.object({
  email: yup.string().email().required(),
  password: yup.string().required()
});

type LoginFormData = yup.InferType<typeof schema>;

const EmailPasswordForm = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const dispatch = useAppDispatch();
  // Where ProtectedRoute wanted to send the user before bouncing them to login.
  // Sanitised against open-redirect / auth-loop targets; null falls back to the
  // dashboard. Survives the MFA hop by riding along in /mfa/verify's state.
  const returnTo = sanitizeReturnTo(
    locationToReturnTo((location.state as { from?: unknown } | null)?.from)
  );
  const [localError, setLocalError] = useState<string | null>(null);
  const [login, { isLoading }] = useLoginMutation();
  const {
    register,
    handleSubmit,
    formState: { errors }
  } = useForm<LoginFormData>({ resolver: yupResolver(schema) });
  // Surface admin-managed kill switches. The transport-failure fallback in
  // authApi keeps everything enabled so a degraded /policy fetch doesn't
  // block legitimate users; a SERVED false/null is honoured strictly.
  const { data: policy } = useGetAuthPolicyQuery();
  const loginEnabled = policy?.loginEnabled ?? true;
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const breakGlass = policy?.passwordLoginBreakGlassEffective ?? false;
  const persistedOn = passwordUiVisible(policy);

  // G5: persisted false/null hides the password UI entirely — the backend
  // would 403 anyway; a sign-in page must not advertise a dead method.
  // The ONE exception is the labelled emergency form under break-glass.
  if (!persistedOn && !breakGlass) return null;

  // Under break-glass with the persisted method off, this is an emergency
  // surface: label it, and hide every credential-minting CTA.
  const emergencyOnly = breakGlass && !persistedOn;

  const onSubmit = handleSubmit(async ({ email, password }) => {
    setLocalError(null);
    try {
      const result = await login({ email, password }).unwrap();

      // Account has an enrolled second factor — hold the credentials flow
      // and send the user to the verify page with the challenge id.
      if (result.requiresMfa && result.mfaToken) {
        navigate('/mfa/verify', {
          state: {
            challengeId: result.mfaToken,
            email,
            webauthnAvailable: result.webauthnAvailable ?? false,
            returnTo
          }
        });
        return;
      }

      if (!result.user) {
        setLocalError(t('auth.errors.unableToSignIn'));
        return;
      }
      dispatch(loginAction({ userData: result.user }));

      navigate(returnTo ?? DEFAULT_POST_LOGIN, { replace: true });
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      if (anyErr?.status === 401) {
        setLocalError(t('auth.errors.invalidCredentials'));
      } else if (anyErr?.status === 403) {
        setLocalError(
          anyErr?.data?.detail || t('auth.errors.emailNotVerified')
        );
      } else if (anyErr?.status === 429) {
        setLocalError(t('auth.errors.tooManyAttempts'));
      } else {
        setLocalError(anyErr?.data?.detail || t('auth.errors.unableToSignIn'));
      }
    }
  });

  return (
    <Form onSubmit={onSubmit} noValidate>
      {emergencyOnly && (
        <Alert variant="warning" className="mb-3">
          {t('auth.pages.passwordBreakGlassActive')}
        </Alert>
      )}
      {!loginEnabled && (
        <Alert variant="warning" className="mb-3">
          {t('auth.loginDisabled')}
        </Alert>
      )}
      {localError && (
        <Alert
          variant="danger"
          className="mb-3"
          onClose={() => setLocalError(null)}
          dismissible
        >
          {localError}
        </Alert>
      )}

      <Form.Group className="mb-3" controlId="login-email">
        <Form.Label>{t('auth.email')}</Form.Label>
        <Form.Control
          type="email"
          placeholder={t('auth.emailPlaceholder')}
          autoComplete="email"
          isInvalid={!!errors.email}
          {...register('email')}
        />
        <Form.Control.Feedback type="invalid">
          {errors.email?.type === 'email'
            ? t('auth.errors.invalidEmail')
            : t('auth.errors.missingFields')}
        </Form.Control.Feedback>
      </Form.Group>

      <Form.Group className="mb-3" controlId="login-password">
        <div className="d-flex justify-content-between">
          <Form.Label>{t('auth.password')}</Form.Label>
          {!emergencyOnly && (
            <Link to="/forgot-password" className="fs-10">
              {t('auth.forgotPassword')}
            </Link>
          )}
        </div>
        <Form.Control
          type="password"
          placeholder={t('auth.passwordPlaceholder')}
          autoComplete="current-password"
          isInvalid={!!errors.password}
          {...register('password')}
        />
        <Form.Control.Feedback type="invalid">
          {t('auth.errors.missingFields')}
        </Form.Control.Feedback>
      </Form.Group>

      <div className="d-grid mb-3">
        <Button
          type="submit"
          variant="primary"
          size="lg"
          disabled={isLoading || !loginEnabled}
        >
          {isLoading ? t('auth.signingIn') : t('auth.signIn')}
        </Button>
      </div>

      {registrationEnabled && !emergencyOnly && (
        <div className="text-center">
          <small className="text-muted">
            {t('auth.noAccount')}{' '}
            <Link to="/register">{t('auth.createOne')}</Link>
          </small>
        </div>
      )}
    </Form>
  );
};

export default EmailPasswordForm;
```

Behavioural invariants to hold against the current file (`EmailPasswordForm.tsx:1-158` at `ca24e614`): same mutation and unwrap flow, same MFA `navigate('/mfa/verify', { state })` payload, same 401/403/429/default error mapping, same `disabled={isLoading || !loginEnabled}`; with persisted true the form renders exactly as today even when the env var is set (a stored true needs no override — the flag is false then, §4.9). The pre-existing `EmailPasswordForm.test.tsx` suite must keep passing unmodified except for the policy-handler fields.

- [ ] **Step 5: Gate `RegisterForm` and `ForgotPasswordForm`**

Both: read the policy, and when `!passwordUiVisible(policy)` render ONLY the disabled-alert path — the same shape as `RegisterForm`'s existing `!registrationEnabled` alert (`RegisterForm.tsx:70-74`) with the new copy, no form controls, "including during break-glass":

```tsx
  if (!passwordUiVisible(policy)) {
    return (
      <Alert variant="warning" className="mb-3">
        {t('auth.pages.passwordLoginDisabled')}
      </Alert>
    );
  }
```

(`ForgotPasswordForm` gains the `useGetAuthPolicyQuery()` read it doesn't have today.)

- [ ] **Step 6: `Login` no-method alert + `SocialLoginForm` callback**

`SocialLoginForm.tsx`: the React import becomes `import { useEffect, useState } from 'react';` (`useEffect` is new); then add the prop and the resolution effect:

```tsx
interface SocialLoginFormProps {
  backendUrl?: string;
  onError?: (error: Error) => void;
  // Fired when the provider query RESOLVES (success only, never on
  // error) with the count of renderable providers — the seam Login uses
  // for the no-sign-in-method alert without issuing a second query.
  onProvidersResolved?: (count: number) => void;
}
```

```tsx
  useEffect(() => {
    if (data && !isError) {
      onProvidersResolved?.(socialProviders.length);
    }
    // socialProviders derives from data; its length is the stable dep.
  }, [data, isError, socialProviders.length, onProvidersResolved]);
```

`Login.tsx`:

```tsx
  const { data: policy } = useGetAuthPolicyQuery();
  const [providerCount, setProviderCount] = useState<number | null>(null);
  const breakGlass = policy?.passwordLoginBreakGlassEffective ?? false;
  // The alert renders only when BOTH methods are conclusively absent: the
  // password UI is policy-hidden (no break-glass) AND the provider query
  // RESOLVED empty. A provider-query error keeps SocialLoginForm's own
  // retryable alert instead — an outage is not "no method" (§4.10).
  const noMethod =
    policy !== undefined &&
    !passwordUiVisible(policy) &&
    !breakGlass &&
    providerCount === 0;
```

with `{noMethod && <Alert variant="warning">{t('auth.pages.loginNoMethod')}</Alert>}` above `<EmailPasswordForm />`, and `<SocialLoginForm onProvidersResolved={setProviderCount} />`.

- [ ] **Step 7: i18n (EN + IT, parity test enforces)**

`en.json` `auth.pages` gains:

```json
"loginNoMethod": "No sign-in method is currently available on this console. Contact an administrator.",
"passwordLoginDisabled": "Email/password sign-in is disabled on this surface. Use a configured sign-in provider, or contact an administrator.",
"passwordBreakGlassActive": "Emergency access mode — email/password sign-in is temporarily restored for operators only. Repair the sign-in provider configuration, then disable the override."
```

and `auth.errors` gains (yup owns email-shape validation now that the form is `noValidate`):

```json
"invalidEmail": "Enter a valid email address."
```

`it.json` twins:

```json
"loginNoMethod": "Nessun metodo di accesso è al momento disponibile su questa console. Contatta un amministratore.",
"passwordLoginDisabled": "L'accesso con email e password è disabilitato su questa superficie. Usa un provider di accesso configurato o contatta un amministratore.",
"passwordBreakGlassActive": "Modalità di accesso di emergenza — l'accesso con email e password è temporaneamente ripristinato per i soli operatori. Ripara la configurazione dei provider, poi disattiva l'override."
```

```json
"invalidEmail": "Inserisci un indirizzo email valido."
```

Task note: the spec's key list also names `pages.providersUnavailable`; the provider-query error state is already rendered by the existing `auth.social.loadError` — no dead key is added, recorded here so the reviewer sees the delta deliberately.

- [ ] **Step 8: Same-commit docs**

`frontend-admin/CLAUDE.md` — the `components/authentication/` paragraph (the long entry around line 66) currently describes the pre-toggle forms; append to it, in the same voice:

```markdown
The unauthenticated password surface is policy-gated (PR 3): `EmailPasswordForm`
(now `react-hook-form` + `yup`) renders only when `/policy`'s
`passwordLoginEnabled` is literally `true` — a served `false` OR `null` hides
it (the shared predicate is `passwordUiVisible` in `store/api/authApi.ts`;
only a TRANSPORT failure falls open, via the queryFn fallback) — except under
`passwordLoginBreakGlassEffective`, which renders a labelled emergency form
with the forgot-password and register CTAs hidden. `RegisterForm` and
`ForgotPasswordForm` render only the disabled alert in that state, break-glass
included. `SocialLoginForm` reports its resolved provider count through
`onProvidersResolved` (success only, never on a query error) so `Login` can
show the no-sign-in-method alert without a second query — an outage renders
the retryable provider error instead, never "no method".
```

- [ ] **Step 9: Run, typecheck, lint, commit**

`cd /home/tore/orkestra/frontend-admin && npx vitest run src/components/authentication && npm run typecheck && npm run lint`
Expected: PASS (locale parity included).

```bash
git add frontend-admin/CLAUDE.md frontend-admin/src/store/api/authApi.ts frontend-admin/src/test/handlers.ts frontend-admin/src/components/authentication/EmailPasswordForm.tsx frontend-admin/src/components/authentication/EmailPasswordForm.test.tsx frontend-admin/src/components/authentication/RegisterForm.tsx frontend-admin/src/components/authentication/RegisterForm.test.tsx frontend-admin/src/components/authentication/ForgotPasswordForm.tsx frontend-admin/src/components/authentication/ForgotPasswordForm.test.tsx frontend-admin/src/components/authentication/Login.tsx frontend-admin/src/components/authentication/Login.test.tsx frontend-admin/src/components/authentication/SocialLoginForm.tsx frontend-admin/src/components/authentication/SocialLoginForm.test.tsx frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(frontend-admin): hide password UI per policy, labelled break-glass form, no-method alert" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 9: frontend-admin — security pages on the split view fields

§4.8's consumer migration: unlink decisions read `passwordUsableForLogin`, set/change UI reads `hasPasswordSet`, the retained-password notice appears where a set-but-unusable password would otherwise mislead, and the admin reset button stops offering what the backend now 409s.

Pre-flight (orkestra-frontend-admin skill): production precedent = `src/pages/admin/user-profile/AdminAuthMethodsCard.tsx` + `src/pages/user/security/SessionsTab.test.tsx` (MSW page-test idiom, read this session); reference = `src/reference/components/ui/Cards.tsx`; primitives = `SubtleBadge`, `OverlayTrigger`+`Tooltip` (already imported in the card).

**Files:**
- Modify: `frontend-admin/src/store/api/authApi.ts:178-196` (`SelfAuthMethods`), `frontend-admin/src/store/api/userApi.ts:165-196` (admin mirror)
- Modify: `frontend-admin/src/pages/user/security/PasswordTab.tsx`, `LinkedProvidersTab.tsx`, `frontend-admin/src/pages/user/settings/SecuritySummaryCard.tsx`, `frontend-admin/src/pages/admin/user-profile/AdminAuthMethodsCard.tsx`
- Modify: `frontend-admin/src/locales/en.json`, `it.json`
- Modify: `frontend-admin/src/test/handlers.ts` (`emptySelfAuthMethods` gains the split fields)
- Test: `frontend-admin/src/pages/user/security/PasswordTab.test.tsx`, `frontend-admin/src/pages/user/security/LinkedProvidersTab.test.tsx`, `frontend-admin/src/pages/user/settings/SecuritySummaryCard.test.tsx`, `frontend-admin/src/pages/admin/user-profile/AdminAuthMethodsCard.test.tsx` (all new)

**Interfaces:**
- Consumes: Task 6's wire fields (`hasPasswordSet`, `passwordUsableForLogin`, deprecated `hasUsablePassword`).
- Produces: both TS mirrors gain

```typescript
  hasPasswordSet: boolean;
  passwordUsableForLogin: boolean;
  /** @deprecated alias of hasPasswordSet (NOT usability); removed after one release */
  hasUsablePassword: boolean;
```

- [ ] **Step 1: Extend the shared fixture, then write the failing tests**

`frontend-admin/src/test/handlers.ts` — `emptySelfAuthMethods` (`:21-37`) gains the three new fields so every existing security-center test keeps type-checking:

```typescript
export const emptySelfAuthMethods = {
  hasPasswordSet: true,
  passwordUsableForLogin: true,
  hasUsablePassword: true, // deprecated alias, mirrors hasPasswordSet
  emailVerified: true,
  mfaRequired: false,
  mfaFactors: [] as Array<{
    type: 'totp' | 'webauthn';
    enrolledAt?: string;
    lastUsedAt?: string;
    backupCodesRemaining?: number;
  }>,
  oauthProviders: [] as Array<{
    provider: 'google' | 'apple' | 'github' | 'discord';
    email: string;
    linkedAt: string;
    isPrimary: boolean;
  }>
};
```

(the two typed arrays are verbatim from the current `handlers.ts:25-37`; only the three password fields are new.)

New `PasswordTab.test.tsx` (`selfAuthMethodsHandler` + the policy handler; `renderWithProviders` as in `SessionsTab.test.tsx`):

```tsx
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  emptySelfAuthMethods,
  operatorPolicyHandler,
  selfAuthMethodsHandler
} from 'test/handlers';
import PasswordTab from './PasswordTab';

// The tab fires TWO queries on mount (PasswordTab.tsx:18-19: the auth
// policy for passwordMinLength, and the self auth methods); MSW runs with
// onUnhandledRequest: 'error', so both are stubbed in every test.
describe('PasswordTab split password fields (PR 3 §4.8)', () => {
  it('set-but-unusable: form stays (credential management), notice shows', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: true,
        passwordUsableForLogin: false
      })
    );
    renderWithProviders(<PasswordTab />);
    expect(
      await screen.findByText(/disabled on this surface/i)
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/current password/i)).toBeInTheDocument();
  });

  it('usable password: no notice', async () => {
    server.use(operatorPolicyHandler(), selfAuthMethodsHandler());
    renderWithProviders(<PasswordTab />);
    await screen.findByLabelText(/current password/i);
    expect(screen.queryByText(/disabled on this surface/i)).toBeNull();
  });

  it('no hash: current password stays rendered but stops being required', async () => {
    // The component never removes the field — it flips `required`
    // (PasswordTab.tsx:96, `required={hasPassword}`); the PR 3 change is
    // only WHICH view field feeds hasPassword (hasPasswordSet, not the
    // deprecated alias).
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: false,
        passwordUsableForLogin: false,
        hasUsablePassword: false
      })
    );
    renderWithProviders(<PasswordTab />);
    const current = await screen.findByLabelText(/current password/i);
    expect(current).toBeInTheDocument();
    expect(current).not.toBeRequired();
  });
});
```

(label matchers are the exact EN copy: `userSecurity.passwordTab.labelCurrent` = "Current password"; the notice matcher is the new `keptNotice` copy's "disabled on this surface".)

New `AdminAuthMethodsCard.test.tsx` (admin endpoint, not the self one):

```tsx
import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import AdminAuthMethodsCard from './AdminAuthMethodsCard';
import type { User } from 'store/api/userApi';

// Honest User value — every required field of the interface
// (userApi.ts:11-26), no cast.
const targetUser: User = {
  id: 'u-1',
  email: 'target@example.com',
  username: 'target',
  fullName: 'Target User',
  role: 'operator',
  providers: [],
  isActive: true,
  emailVerified: true,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

// The card reads the CURRENT admin from the auth slice to compute isSelf
// (AdminAuthMethodsCard.tsx:50-52); seed a different id so isSelf is
// false. Same AuthState shape useUserTable.test.tsx:57-84 preloads.
const preloadedAuthState = {
  auth: {
    user: {
      id: 'admin-1',
      email: 'admin@example.com',
      username: 'admin',
      fullName: 'Admin One',
      role: 'administrator',
      providers: [],
      isActive: true,
      emailVerified: true,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z'
    },
    isAuthenticated: true,
    isLoading: false,
    error: null,
    sessionExpiry: null,
    permissions: [] as string[],
    preferences: {
      theme: 'light' as const,
      language: 'en',
      notifications: true
    },
    _isLoggingOut: false,
    accessToken: 'test-token',
    tokenExpiry: null
  }
};

const adminAuthMethods = (overrides: Record<string, unknown>) =>
  http.get('*/v1/admin/users/u-1/auth-methods', () =>
    HttpResponse.json({
      hasPasswordSet: true,
      passwordUsableForLogin: true,
      hasUsablePassword: true,
      passwordUpdatedAt: '2026-05-01T00:00:00Z',
      emailVerified: true,
      mfaRequired: false,
      mfaFactors: [],
      oauthProviders: [],
      ...overrides
    })
  );

describe('AdminAuthMethodsCard split password fields (PR 3 §4.8)', () => {
  // Button-name matchers are the exact EN copy:
  //   adminUserProfile.authMethods.passwordSendResetButton = "Send password-reset email"
  //   adminUserProfile.authMethods.oauthActionsAria       = "actions for {{provider}}"
  it('method off: reset button disabled; presence badge still reads hasPasswordSet', async () => {
    server.use(adminAuthMethods({ passwordUsableForLogin: false }));
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    const reset = await screen.findByRole('button', {
      name: /send password-reset email/i
    });
    expect(reset).toBeDisabled();
    // presence badge (adminUserProfile.authMethods.passwordBadgeSet = "Set")
    expect(screen.getByText(/^set$/i)).toBeInTheDocument();
  });

  it('method on: reset button enabled', async () => {
    server.use(adminAuthMethods({}));
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    expect(
      await screen.findByRole('button', { name: /send password-reset email/i })
    ).toBeEnabled();
  });

  it('provider actions block keys off usability even with a hash present', async () => {
    server.use(
      adminAuthMethods({
        hasPasswordSet: true,
        passwordUsableForLogin: false,
        oauthProviders: [
          { provider: 'google', email: 'u@example.com', linkedAt: '2026-05-01T00:00:00Z', isPrimary: true }
        ]
      })
    );
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    // The blocked ProviderActions button carries the same aria-label as
    // the enabled dropdown toggle (Step 3.4).
    const actions = await screen.findByRole('button', {
      name: /actions for google/i
    });
    expect(actions).toBeDisabled();
  });
});
```

(the preloaded auth state above is what makes `isSelf` resolve false; without it the card blocks every provider action with the self-action reason.)

The other two consumers get their own complete test files.

`LinkedProvidersTab.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { emptySelfAuthMethods, selfAuthMethodsHandler } from 'test/handlers';
import LinkedProvidersTab from './LinkedProvidersTab';

// The tab mounts ONE query (useGetSelfAuthMethodsQuery,
// LinkedProvidersTab.tsx:51); the link/unlink mutations fire only on click.
describe('LinkedProvidersTab only-credential (PR 3 §4.8)', () => {
  it('sole provider + unusable password blocks unlink', async () => {
    server.use(
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: true,
        passwordUsableForLogin: false,
        oauthProviders: [
          { provider: 'google', email: 'u@example.com', linkedAt: '2026-05-01T00:00:00Z', isPrimary: true }
        ]
      })
    );
    renderWithProviders(<LinkedProvidersTab />);
    await screen.findByText(/google/i);
    // userSecurity.linkedProvidersTab.rowUnlink = "Unlink" — a real text
    // button (LinkedProvidersTab.tsx:276-282, disabled={onlyCredential || isFetching}).
    expect(screen.getByRole('button', { name: /^unlink$/i })).toBeDisabled();
  });

  it('two providers keep unlink available', async () => {
    server.use(
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        passwordUsableForLogin: false,
        oauthProviders: [
          { provider: 'google', email: 'u@example.com', linkedAt: '2026-05-01T00:00:00Z', isPrimary: true },
          { provider: 'github', email: 'u@example.com', linkedAt: '2026-05-02T00:00:00Z', isPrimary: false }
        ]
      })
    );
    renderWithProviders(<LinkedProvidersTab />);
    await screen.findByText(/github/i);
    // onlyCredential is false with two rows, so BOTH unlink buttons stay enabled.
    for (const b of screen.getAllByRole('button', { name: /^unlink$/i })) {
      expect(b).toBeEnabled();
    }
  });
});
```

`SecuritySummaryCard.test.tsx` (the card fires THREE queries on mount — `SecuritySummaryCard.tsx:45-49`: `useGetCurrentUserQuery` → `GET */v1/auth/operator/me`, `useGetSelfAuthMethodsQuery`, `useGetMySessionsQuery` — and an unhandled MSW request fails the run, so every test stubs all three):

```tsx
import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  emptySelfAuthMethods,
  emptySessions,
  mySessionsHandler,
  selfAuthMethodsHandler
} from 'test/handlers';
import SecuritySummaryCard from './SecuritySummaryCard';

// Complete required BackendUser body (authApi.ts:39-62: id, email,
// username, fullName, role, isActive, emailVerified, createdAt,
// updatedAt are the non-optional fields).
const meHandler = http.get('*/v1/auth/operator/me', () =>
  HttpResponse.json({
    id: 'u-1',
    email: 'op@example.com',
    username: 'op',
    fullName: 'Operator One',
    role: 'administrator',
    isActive: true,
    emailVerified: true,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z'
  })
);

describe('SecuritySummaryCard password row (PR 3 §4.8)', () => {
  it('set-but-unusable shows the kept note', async () => {
    server.use(
      meHandler,
      mySessionsHandler(emptySessions),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: true,
        passwordUsableForLogin: false
      })
    );
    renderWithProviders(<SecuritySummaryCard />);
    // settings.security.summary.passwordKeptNotice (added in Step 4)
    expect(
      await screen.findByText(/sign-in with it is disabled/i)
    ).toBeInTheDocument();
  });

  it('no hash hides the password row', async () => {
    server.use(
      meHandler,
      mySessionsHandler(emptySessions),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: false,
        passwordUsableForLogin: false,
        hasUsablePassword: false
      })
    );
    renderWithProviders(<SecuritySummaryCard />);
    // Anchor on a SETTLED auth-methods element first — with zero factors
    // the card renders settings.security.summary.mfaOff = "Two-factor off";
    // asserting an absence before the query resolves passes vacuously.
    await screen.findByText(/two-factor off/i);
    // settings.security.summary.passwordAgeUnknown = "Password update date
    // unknown" is the password row's copy when no updatedAt is present.
    expect(
      screen.queryByText(/password update date unknown/i)
    ).not.toBeInTheDocument();
  });
});
```

(the zero-factor branch is verified: `SecuritySummaryCard.tsx:107-111` renders `settings.security.summary.mfaOff` = "Two-factor off" whenever `hasMfa` is false, and the whole list replaces the loading `Placeholder` only once the auth-methods query settles — so the anchor proves settlement.)

- [ ] **Step 2: Run to verify failure**

`npx vitest run src/pages/user src/pages/admin/user-profile` — FAIL (new fields absent from the TS mirrors, notices unrendered, buttons not gated).

- [ ] **Step 3: Migrate the four consumers**

- `PasswordTab.tsx:29`: `const hasPassword = authMethods?.hasPasswordSet ?? true;` plus the notice under the title when `authMethods?.hasPasswordSet && authMethods?.passwordUsableForLogin === false`:

```tsx
        <Alert variant="info" className="mb-3">
          {t('userSecurity.passwordTab.keptNotice')}
        </Alert>
```

- `LinkedProvidersTab.tsx:107`: `const onlyCredential = !data?.passwordUsableForLogin && providers.length === 1;` (§4.8: unlink decisions → usability; the backend guard is authoritative, this only pre-explains the 409).
- `SecuritySummaryCard.tsx:137`: the summary row keys off `hasPasswordSet`; when set-but-unusable append the muted note `t('settings.security.summary.passwordKeptNotice')`.
- `AdminAuthMethodsCard.tsx`, three edits:
  1. `:178`/`:189`: the badge + last-changed sub read `hasPasswordSet` instead of `hasUsablePassword`.
  2. `:320`: `onlyCredential` reads `!data.passwordUsableForLogin && data.oauthProviders.length === 1`.
  3. The send-reset `action` button (label stays the EXISTING key `adminUserProfile.authMethods.passwordSendResetButton`, `en.json:1865` — no new label key; the tooltip appears only in the blocked state, carrying the new `resetBlockedPolicy` copy):

```tsx
            action={
              data.passwordUsableForLogin ? (
                <Button
                  variant="orkestra-default"
                  size="sm"
                  disabled={pwBusy}
                  onClick={handleSendReset}
                >
                  {t('adminUserProfile.authMethods.passwordSendResetButton')}
                </Button>
              ) : (
                <OverlayTrigger
                  placement="left"
                  overlay={
                    <Tooltip>
                      {t('adminUserProfile.authMethods.resetBlockedPolicy')}
                    </Tooltip>
                  }
                >
                  {/* span keeps the tooltip alive over a disabled button —
                      Bootstrap's documented idiom */}
                  <span className="d-inline-block">
                    <Button variant="orkestra-default" size="sm" disabled>
                      {t('adminUserProfile.authMethods.passwordSendResetButton')}
                    </Button>
                  </span>
                </OverlayTrigger>
              )
            }
```

  4. `ProviderActions`'s BLOCKED branch (`:472-488`): the icon-only disabled button today has no accessible name (the enabled `Dropdown.Toggle` at `:495-500` already carries `aria-label={t('adminUserProfile.authMethods.oauthActionsAria', { provider })}` — "actions for {{provider}}"). Give the blocked button the SAME aria-label so it is the same control to assistive tech and to the tests:

```tsx
          <Button
            variant="link"
            size="sm"
            disabled
            className="text-body-tertiary"
            aria-label={t('adminUserProfile.authMethods.oauthActionsAria', {
              provider: provider.provider
            })}
          >
            <FontAwesomeIcon icon="ellipsis-h" />
          </Button>
```

- [ ] **Step 4: i18n (EN + IT)**

```json
"userSecurity": { "passwordTab": { "keptNotice": "Email/password sign-in is disabled on this surface; your stored password is retained for a later re-enable." } },
"settings":     { "security": { "summary": { "passwordKeptNotice": "set, but sign-in with it is disabled on this surface" } } },
"adminUserProfile": { "authMethods": { "resetBlockedPolicy": "Email/password sign-in is disabled on this user's surface — a reset link would mint a credential the surface refuses." } }
```

and the `it.json` twins:

```json
"userSecurity": { "passwordTab": { "keptNotice": "L'accesso con email e password è disabilitato su questa superficie; la password memorizzata è conservata per una futura riattivazione." } },
"settings":     { "security": { "summary": { "passwordKeptNotice": "impostata, ma l'accesso con essa è disabilitato su questa superficie" } } },
"adminUserProfile": { "authMethods": { "resetBlockedPolicy": "L'accesso con email e password è disabilitato sulla superficie di questo utente — un link di reset creerebbe una credenziale che la superficie rifiuta." } }
```

(merge into the existing nested objects — do not replace the parent keys. Task note: the spec's single `auth.security.passwordKeptNotice` key is realised per-page-namespace — `userSecurity.*`, `settings.security.*`, `adminUserProfile.*` — because that is the repo's i18n convention; one shared key across three namespaces would break the feature-namespacing mandate.)

- [ ] **Step 5: Run the full frontend gate + commit**

`npx vitest run src/pages/user src/pages/admin/user-profile src/components/authentication && npm run typecheck && npm run lint`
Expected: PASS.

```bash
git add frontend-admin/src/store/api/authApi.ts frontend-admin/src/store/api/userApi.ts frontend-admin/src/test/handlers.ts frontend-admin/src/pages/user/security/PasswordTab.tsx frontend-admin/src/pages/user/security/PasswordTab.test.tsx frontend-admin/src/pages/user/security/LinkedProvidersTab.tsx frontend-admin/src/pages/user/security/LinkedProvidersTab.test.tsx frontend-admin/src/pages/user/settings/SecuritySummaryCard.tsx frontend-admin/src/pages/user/settings/SecuritySummaryCard.test.tsx frontend-admin/src/pages/admin/user-profile/AdminAuthMethodsCard.tsx frontend-admin/src/pages/admin/user-profile/AdminAuthMethodsCard.test.tsx frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(frontend-admin): security pages read the split password fields; reset button honours the per-surface policy" -m "Claude-Session: $CLAUDE_SESSION"
```

### Task 10: Documentation reconciliation sweep and the full gates

Earlier tasks each carried their same-commit docs (the mapping in Global Constraints); this task RECONCILES them — reads every touched doc against the final branch state, completes what an earlier task could only sketch (the docs-site narrative pages, which no code task owns), and runs the complete local gate set. Nothing here is a first touch except the three docs-site pages.

**Files:**
- Modify: `backend/internal/core/auth/CLAUDE.md`, `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/shared-iface.mdx`, `docs/site/modules/core/auth.mdx`, `docs/site/operating/oauth-providers.mdx`, `docs/site/architecture/authentication-flow.mdx`, `frontend-admin/CLAUDE.md` (reconcile Task 8's paragraph against the final branch — first touched in Task 8)

**Interfaces:** none — prose only. Every claim below is written against the code as merged by Tasks 1–9, not against this plan.

- [ ] **Step 1: `backend/internal/core/auth/CLAUDE.md` — reconcile**

Tasks 2–7 each added their rows; this step re-reads the whole file against the final diff and reconciles overlap/ordering (§4.12 list, adapted to what PR 3 actually shipped):
- **Login & Sessions row**: the `passwordLoginEnabled{Admin,Client}` pair — strict fail-closed read (`PasswordLoginEnabled`, absent-key-means-true, everything else `ErrAuthPolicyUnavailable` → 503), the §4.4 validator invariant (snapshot contract, all three surfaces, `auth.login_method_lockout`), verified-email auto-link already documented by PR 2 — cross-reference it.
- **Break-glass**: `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` — operator login + its MFA continuation ONLY; the decision/accessor split (`PasswordLoginDecision` vs `PasswordLoginEnabled`); boot WARN; `auth.policy.break_glass_used` audit event (audience, user UUID, SID, IP; no email/token/password).
- **Route table**: the §4.3 verdict table verbatim (login/register/forgot 403; both admin resets 409; MFA/WebAuthn completion re-check incl. `MFAChallenge.Audience` + consume/retain semantics; reset-password/accept-invite/verify-email/change-password open).
- **Step-up**: `StepUpPolicy.PasswordReauthAllowed`, the 503-not-enrollment rule, password-confirm's 409.
- **Unlink guard**: usable-links semantics + the `SetProviderUsability` seam.
- **`AuthMethodsView`**: the three fields and the one-release deprecation of `hasUsablePassword`.
- **`HotReloadConfig`**: true, and why.

- [ ] **Step 2: SDK + iface docs**

`backend/pkg/sdk/CLAUDE.md`: in the iface section, the two sentinels beside `AdminAuthInviter` — why they live in the SDK (cross-module `errors.Is` at the admin reset boundary) and the alias pattern in `auth/services`. `docs/site/sdk/shared-iface.mdx`: same two vars in the reference list with one-line semantics.

- [ ] **Step 3: docs.orkestra.cc pages**

- `docs/site/modules/core/auth.mdx:173`: "63 fields" → "65 fields"; add the "SSO-only surface" paragraph under Login & Sessions: the invariant (both exits), the strict fail-closed read, "blocks new authentications except the two operator-only bootstrap paths", sessions-not-revoked, the break-glass procedure pointer, the §8 named follow-ups (bulk revoke, invite-bound OAuth onboarding).
- `docs/site/operating/oauth-providers.mdx`: a short "Going SSO-only" section — configure + verify a provider, keep auto-link on, flip `passwordLoginEnabled<S>` off, what the validator refuses, the staging checklist pointer (spec §7), the break-glass drill.
- `docs/site/architecture/authentication-flow.mdx`: the password-login policy check in the login sequence (before user lookup), the completion re-check on MFA/WebAuthn, `/policy`'s new pair, the step-up change. (The client-SPA OAuth path narrative belongs to PR 4 — do not pre-document it.)

- [ ] **Step 4: Verify the docs build**

Fresh clone of `orkestra-docs`; `npm ci`; `MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync` (FULL sync); `CI=true npm run build`.
Expected: build succeeds; the three changed pages render.

- [ ] **Step 5: The full gates on the branch**

```bash
MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true' make -C /home/tore/orkestra ci-backend
make -C /home/tore/orkestra ci-frontend-admin
git -C /home/tore/orkestra diff --check $(git -C /home/tore/orkestra merge-base dev HEAD)..HEAD
```

Expected: ci-backend green with **0 SKIP** (the `ork-errquality-ci-mongo` helper up); ci-frontend-admin green (locale parity, typecheck, eslint, tests, audit, build); no whitespace damage. `openapi-check` runs inside ci-backend and must see Task 7's regenerated dump.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/auth/CLAUDE.md backend/pkg/sdk/CLAUDE.md docs/site/sdk/shared-iface.mdx docs/site/modules/core/auth.mdx docs/site/operating/oauth-providers.mdx docs/site/architecture/authentication-flow.mdx frontend-admin/CLAUDE.md
git commit -m "docs(auth): SSO-only surface, break-glass procedure, split auth-methods fields and the iface sentinels" -m "Claude-Session: $CLAUDE_SESSION"
```

---

## Post-plan verification (not a task — the executor's exit checklist)

1. Every §4.3 route verdict has a test that names it (`gates_test.go`, `mfa_login_verify_test.go`, `error_mapping_test.go`, the client-twin mapping test).
2. `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"` — doc-comment hits only.
3. `grep -rn "OperatorBreakGlass\|break_glass" backend/ --include="*.go" | grep -v _test` — consumers are exactly: config parse, module Init wiring/WARN, `PasswordLoginDecision`, `OperatorBreakGlassConfigured`, `/policy` handler, `EmitBreakGlassUsed` emissions. Nothing else may read the flag.
4. No new logline/audit event carries an email, token, password or secret value (structured-logging safety suite stays green).
5. The staging verification script of spec §7 (steps 1–9) is executable as written against this branch — walk it mentally against the final diff; anything it would trip over goes back into a task, not into a doc footnote.
