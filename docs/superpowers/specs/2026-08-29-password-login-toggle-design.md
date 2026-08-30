# Per-surface password-login toggle — design

| Field | Value |
|---|---|
| **Date** | 2026-08-29 |
| **Last review** | 2026-08-30 |
| **Status** | Draft v4.3 — v4.2 plus the four blocking findings of the PR 2 plan review (§0); ready for implementation approval |
| **Scope** | `backend/internal/core/auth`, `backend/internal/shared/{middleware,errcode,config}`, `backend/pkg/sdk/{module,iface}` (additive validation snapshot + atomic config writes + audit wiring), `backend/cmd/server/main.go`, `frontend-admin`, `frontend-client` (adds OAuth login + Vitest), root CI targets, `docker/.env.example`, `docs/site` |
| **ADR** | None. Both new fields default to `true`, so the feature is inert until explicitly changed. The SDK work is additive or repairs existing persistence guarantees: the frozen `Module` interface is unchanged; existing validator interfaces and `AuditSink` remain source-compatible. **One declared exception to additive-only:** `module.ConfigRepository` — provided *to* the config service by the host, never implemented *by* a module (the `RedisClient` category) — changes shape for atomic writes (§4.5). |

## 0. Revision log

v2 answered the first review: it completed the route inventory, added the
Tier-2 OAuth path, closed pending-challenge and enumeration gaps, split
password presence from usability, and introduced structural anti-lockout
validation, break-glass recovery and config audit.

v3 answers the implementability/security review of v2. Material changes:

- the validation contract now receives the **target profile's** effective
  secret-presence map, so an inactive environment is never judged with the
  active environment's secrets (§4.4–4.5);
- provider toggles use their real schema default (`false`), simultaneous
  secret+toggle writes are supported, and config mutations use one atomic
  Mongo update guarded against concurrent write skew (§4.4–4.5);
- password policy reads distinguish an absent legacy key from a missing
  module document, reject malformed values/audiences, and fail closed when
  wiring is absent; the required auth document cannot be lazily recreated with
  permissive defaults during a running process (§4.2);
- break-glass is operator-only and applies only to login plus its pending MFA
  continuation — never to client login, registration, reset requests,
  auth-method reporting or unlink decisions — and the console exposes only a
  labelled emergency login form while it is effective (§4.2–4.3, §4.9);
- email auto-link requires a provider-verified email; GitHub must source the
  address and `verified` bit from `/user/emails` (§4.4);
- OAuth callback redirects carry no access token, email or user ID; the client
  bootstrap explicitly adopts the new refresh cookie before refreshing, while
  the admin removes its timeout race and router-stored MFA challenge (§4.10);
- config audit is described honestly as best-effort under the existing
  `AuditSink` contract, and the Tier-2 OAuth state machine gains automated
  tests (§4.10–4.11).

v4 applies the four corrections and two suggestions from the v3 code check:
`module.config_revision_stale` is SDK-owned, not an `errcode` constant (§4.5,
§6); a required module makes `GetConfig` fail but leaves `GetAllConfigs`
serving the modules list with a per-module `missing` row (§4.2); the
absent-key exposure of the provider toggles is stated with its bounding facts
and closed by a boot-time schema backfill (§4.4, §5 #14); GitHub's verified
email sourcing is recorded as existing behaviour with the fallback kept (§4.4);
a malformed single provider toggle degrades only that provider instead of
503-ing the whole list (§4.4, §5 #29); delivery is split into four sequenced
PRs (§7).

v4.1 (2026-08-30) records four contract decisions the PR 1 implementation
plan surfaced, so they are approved here rather than decided in code:

- `module.ConfigRepository` is provided to the config service, not
  implemented by modules, and changes shape as a **declared exception** to
  the SDK's additive-only rule — `CompareAndSwapConfig` added,
  `CompareAndSwapEnvironment` and `MigrateToEnvironments` re-signed, the four
  two-step write methods removed (§4.5, ADR row). A parallel CAS interface
  was rejected: the service would have to keep the non-atomic path alive as
  its fallback.
- The admin API's two request lanes are enforced server-side: a secret key
  in `config`, a non-secret or unknown key in `secrets`, the SDK-owned
  roster key or any undeclared key is refused with 422
  `module.config_key_invalid` before validation, encryption or persistence,
  against the module's **live** schema (§4.5).
- The boot backfill writes only schema keys whose `EnvVar`/`Default` is
  non-empty and rebuilds the legacy mirror from the active profile; a key
  with an empty fallback stays absent because absence is meaningful to
  `GetRawValue` readers (§4.4, §5 #14).
- `RequirePersistedConfig` is the **boot gate** for the modules it names: a
  missing document, or a seeding / metadata-refresh / backfill failure
  recorded for one of them, stops the server before it serves (§4.2).

v4.2 (2026-08-30) records the decisions the PR 2 implementation plan
surfaced against the code at `7574368a`, so they are approved here rather
than decided in code:

- **Four findings the v4 code check missed.** GitHub's `GetUserInfo` marks
  the public-profile `email` verified by assumption
  (`github_oauth_service.go:185-192`) — the §4.4 rule stands and the code
  changes to match §6. The GitHub Huma callback never sets the refresh
  cookie, so a GitHub web login produces no session today. The Huma
  `HandleAppleCallback` that emits `access_token=` is unregistered dead
  code. Apple's `form_post` callback cannot carry the `SameSite=Lax` state
  cookie, so a same-host Apple flow fails the browser binding — recorded as
  a follow-up (§8), not fixed by PR 2.
- One **additive** SDK accessor,
  `ModuleConfigService.ActiveConfigRequiredModule(ctx, name)` →
  `ActiveConfigView`, is the "one required config read" §4.4 demands: raw
  and effective values, every stored secret decrypted once, and the
  revision, from a single repository read; a stored secret that no longer
  decrypts fails the whole read (§4.4, §7). `NewActiveConfigView` is
  exported for a fork's fakes. The `Module` interface is untouched.
- `ErrAuthPolicyUnavailable`, `errcode.AuthPolicyUnavailable`, the strict
  boolean parser and `configValueReader.GetRawValueRequiredModule` land in
  PR 2 — the strict auto-link and provider-toggle reads need them; PR 3
  reuses them unchanged (§4.3, §7).
- All four web callbacks are raw chi handlers sharing one implementation;
  the `github-oauth-callback` Huma operation leaves the OpenAPI document;
  the dead Huma Apple callback and Apple's dev-only "missing state"
  fallback are removed — a missing state is a terminal 400 in every
  environment (§4.10).
- The callback re-resolves provider usability through the same strict
  one-read path OAuth start uses, so a provider disabled or blanked
  mid-flow is refused with `oauth_provider_unavailable`, and the provider
  is built from the value that answered the check (§4.4, §4.10).
- On the web redirect surface, config uncertainty — the strict auto-link
  read or a document-level provider resolution failing — maps to the
  allowlisted `oauth_provider_unavailable`, because a redirect cannot carry
  a 503; the JSON surfaces (mobile ID-token endpoints, `/providers`, OAuth
  start) answer 503 `auth.policy_unavailable`. Both fail closed before
  lookup, link or token issuance (§4.4, §4.10).
- Every callback redirect sets `Cache-Control: no-store` next to
  `Referrer-Policy: no-referrer`; the link-mode `code` allowlist gains
  `access_denied` and `provider_unavailable`; the stored `RedirectURI` is
  always the configured tier SPA (§4.10).
- The mobile ID-token endpoints keep the permissive `OAuthProviderEnabled`
  gate (native semantics stay outside this web-only change) and gain only
  the mappings of the two new sentinels (§4.4).
- In the operator console only the OAuth path is router-state-free: the
  verify form is extracted into `MfaVerifyPanel`, rendered by the callback
  from component memory; the password path's `LoginMfaVerify` page keeps
  `location.state`, which never travels in a URL (PR 3 owns that form).
  The OAuth landing falls back to the SPA's `DEFAULT_POST_LOGIN` (§4.10).

v4.3 (2026-08-30) answers the review of the PR 2 plan, which found that the
callback design v4.2 inherited from the code cannot work for the client tier
and that the state machine is weaker than described:

- **The operator-host callback cannot set the client tier's refresh
  cookie.** Every provider callback is mounted on the operator host only
  (one redirect URI per provider, ADR-0003 D-6), so a client-tier flow ends
  with `console.example.com` answering `Set-Cookie … Domain=api.example.com`
  — a cookie the browser must reject because the domain does not match the
  response host (RFC 6265 §4.1.2.3 / §5.3), and which the cross-tier
  isolation model (`docs/site/operating/cookie-hardening-cross-tier.mdx`)
  forbids weakening with a shared parent domain. Client-tier web OAuth is
  therefore broken today. The fix is a **one-shot relay**: the operator-host
  callback completes the IdP half (state, exchange, user info) and hands the
  result to the client API host through a 60-second, single-use, encrypted
  relay record; `GET /v1/auth/client/oauth/complete?relay=<id>` on the
  client API host takes the record atomically, verifies the browser binding
  against the state cookie that host set at start, runs the application
  half, sets the refresh cookie on its own host and redirects to the client
  SPA (§4.10).
- **The cross-host exception of the browser binding was the normal path.**
  `verifyOAuthStateBinding` accepts a callback with no state cookie whenever
  start and callback hosts differ, which for the client tier is every flow —
  so login CSRF was not actually prevented there. The exception becomes a
  *deferral*: the operator-host callback never completes a client-tier flow;
  the relay endpoint requires the cookie and fails closed without it (§4.10).
- **The OAuth state is not one-shot.** `ValidateOAuthState` reads with `Get`
  and deletes in a goroutine, so two concurrent callbacks can both read it.
  The store already owns an atomic `Take` (Redis `GETDEL`); the state read
  moves to it, and §6 requires a concurrent test with exactly one winner and
  a replay test against the real service (§4.10).
- **State and provider were not bound.** The Redis row records the provider
  but the callback never compares it with the endpoint's provider. The
  comparison happens inside state resolution, before the IdP `error`, the
  code or any profile is interpreted; a mismatch is the generic 400 (§4.10).
- The client API's public origin for the relay redirect is a new
  process-scoped `CLIENT_API_URL` (falling back to `https://` +
  `CLIENT_API_HOST` in production-like environments, `http://` in
  development); the operator console's callback page parser is **closed**:
  provider must be one of the four names, `webauthnAvailable` must be
  exactly `true`/`false`, and a payload that mixes an MFA fragment with a
  query outcome, or a success with an `error`, is treated as the generic
  failure (§4.10). The return-target take-and-delete runs in an effect,
  never during render.

## 1. Problem

The auth module lets an operator switch every OAuth provider on and off per
surface (`{google,apple,github,discord}Enabled{Admin,Client}`), but the
email+password method has no switch of its own. The only lever that stops a
password login is `loginEnabled{Admin,Client}`, and that is a maintenance kill
switch: it stops OAuth too (`AuthPolicyService.LoginAllowed` is consulted by
`PasswordAuthService.Login` and by the OAuth start handler alike).

The asymmetry blocks a common deployment posture — *SSO-only*: a customer wants
every operator (or every Tier-2 client user) to authenticate through the
identity provider, with no password credential accepted anywhere on that
surface. Today the closest approximation is "tell people not to use it", which
is not a control.

Every v3 claim below was re-verified against the code at `98911486`; file:line
references are to that commit unless the text explicitly describes a new file.

## 2. Goals and non-goals

**Goals**

- G1. An operator can turn the password method off **per surface** (operator
  console / client app) at `/admin/modules/auth`, live, no restart — the same
  way they turn an OAuth provider off.
- G2. Turning it off is **complete** for new authentications, except the two
  explicitly preserved **operator-only** bootstrap paths (`Register`'s
  first-user branch and `RegisterInitialAdmin`): no ordinary
  unauthenticated password entry point accepts the credential, no in-flight
  password login can complete after the flip, and the password does not count
  as a credential in any decision that asks "does this user still have a usable
  way in" (step-up re-auth, OAuth-unlink lockout).
- G3. An operator **cannot lock a surface out by configuration**: the write is
  refused unless at least one OAuth provider on that surface is *structurally
  configured* (every field the flow needs is present) and the auto-link path
  that lets password-only users in is open. G3 promises structural
  completeness, not that the IdP works — wrong credentials or an IdP outage
  remain operational risks. The break-glass in §4.2 can restore operator
  authentication, but it deliberately does not bypass MFA, low-risk or RBAC
  gates on the subsequent config repair.
- G4. A missing config document, malformed stored value, invalid audience,
  missing policy wiring or config-read failure never *re-enables* the
  password: the check fails closed (503), never open. Only an absent new key
  inside an otherwise valid existing auth document means legacy `true`.
- G5. Both SPAs hide the password UI instead of surfacing a 403 on submit; the
  sole exception is the clearly labelled operator login form while explicit
  break-glass is active. The Tier-2 client SPA gains the OAuth login it needs
  to survive the flip.
- G6. Zero behaviour change for existing deployments: the fields default to
  `true` and have no `EnvVar`.
- G7. Every authenticated module-config mutation that reaches the in-tree
  admin handler emits a structured audit event containing actor, module,
  surface and outcome. Transport/schema rejections before handler dispatch are
  covered by request logs. Persistence is best-effort under the existing
  error-blind `iface.AuditSink`; sink failures are visible in structured logs
  and do not roll back a successful config mutation.

**Non-goals**

- Revoking sessions opened with a password before the flip. The switch blocks
  *new* password authentications; existing sessions keep rotating their refresh
  token until they expire or are revoked. A bulk "revoke password sessions"
  action is a named follow-up (§8), not part of this change.
- Hiding password *management* UI for authenticated users. The credential
  legitimately still exists (§4.3); the UI gets policy-aware copy, not removal.
- An invite-bound OAuth onboarding flow (consuming an invite token inside the
  OAuth callback). The auto-link constraint in §4.4 prevents a configuration
  dead end, but it does not guarantee that every invitee owns a matching IdP
  identity; the dedicated flow remains a follow-up (§8).
- A first-user bootstrap path through OAuth. Bootstrap stays password-based;
  the two existing **operator-only** first-user/setup exceptions are named
  explicitly in G2 and §4.3 rather than being presented as ordinary password
  login. Tier-2 registration never gets this bypass.
- Mobile. `mobile/lib` is an 8-file skeleton with no login code.

## 3. Alternatives considered

### A — Two booleans, `passwordLoginEnabledAdmin` / `passwordLoginEnabledClient` (chosen)

Mirrors the existing `{provider}Enabled{Admin,Client}` and
`loginEnabled{Admin,Client}` pairs field-for-field: same group rail, same
per-audience `AuthPolicyService` accessor, same SPA consumption through
`GET /v1/auth/{tier}/policy`. An operator who has already configured OAuth
providers finds the switch where they expect it.

### B — `loginMethods{Admin,Client}` as a `FieldStringList` of `password,oauth`

Rejected: it duplicates the per-provider OAuth switches (what does `oauth` in
the list mean when `googleEnabledAdmin=false`?), and `FieldStringList` renders
as a free-text comma list, so a typo (`pasword`) silently disables a method with
no 422. `mfaMethods` uses this shape, but there the list is "allowed factor
types", a domain with no other switches to conflict with.

### C — A single `loginMode{Admin,Client}` enum (`all` / `oauth_only`)

Same bit as A with a worse extension story: the next method (passkey-first,
magic link) forces a combinatorial enum. Rejected.

## 4. Design

### 4.1 Schema — `module.go` `ConfigSchema()`

Two fields in the existing `login` group, immediately after
`loginEnabledClient` (`module.go:399-403`):

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

No `EnvVar` (G6) — identical to `loginEnabled{Admin,Client}`.

**Ownership and authorization.** These are deployment-global auth-module
settings, not per-org records. The Admin key governs every Tier-1 operator
login and the Client key every Tier-2 login across external organizations.
Reads keep the existing operator-only `system.modules.admin` guard; mutations
keep `system.modules.admin` + `RequireMFA` + `RequireLowRisk`. No public/client
route can mutate policy, and no new permission or tenant-scoped collection is
introduced.

`AuthModule` adds `HotReloadConfig() bool { return true }`. Its policy service
and OAuth resolver already read module config at request time; declaring the
existing behaviour prevents successful auth config/environment changes from
leaving a false `needsRestart` signal in the admin UI. The generic handler
passes hot-reload capability into the atomic mutation; the same write persists
`needsRestart=false` rather than setting then clearing it in a second update.

### 4.2 Policy accessor — strict fail-closed read, narrow break-glass

The stored policy and the emergency override are deliberately separate. Code
that asks whether a password is a durable login method (registration/reset-
request gates, auth-method views and unlink protection) must never see the
override. The operator public-policy response exposes it as a separate display
flag solely so the recovery form remains reachable (§4.9).

```go
// PasswordLoginEnabled returns the persisted per-surface method policy.
// Compatibility applies to the key, not to the document: an absent key in an
// existing auth document means true; a missing document, malformed value,
// invalid audience or unavailable reader returns an error.
func (s *AuthPolicyService) PasswordLoginEnabled(
    ctx context.Context, audience PolicyAudience,
) (bool, error)

type PasswordAuthDecision struct {
    Allowed        bool
    BreakGlassUsed bool
}

// PasswordLoginDecision is used only by Login and by completion of the MFA /
// WebAuthn challenge it created. It may apply the operator-only break-glass.
func (s *AuthPolicyService) PasswordLoginDecision(
    ctx context.Context, audience PolicyAudience,
) (PasswordAuthDecision, error)
```

`PasswordLoginEnabled` reads through a new additive
`ModuleConfigService.GetRawValueRequiredModule`. It preserves
`GetRawValue`'s three outcomes but treats `doc == nil` as an error; changing
the existing accessor would alter `SessionAbsoluteTTL`'s compatibility
contract. Parsing accepts only canonical, case-insensitive `true` / `false`
after trimming. It does not use the permissive `readBool`, so an out-of-band
`"treu"`, empty present value or non-boolean cannot become the `true` default.
Only `PolicyAudienceOperator` and `PolicyAudienceClient` are accepted.

This accessor reads the repository directly; it never calls `GetConfig`'s
lazy-seed path. More importantly, after boot seeding has run, `cmd/server`
marks `auth` as a **required persisted config** through an additive
`ModuleConfigService.RequirePersistedConfig(ctx, "auth")` call. That call is
also the **boot gate** for the modules it names: it refuses — and the server
exits before serving — when a named module's document is missing or boot
seeding recorded a failure for it (first-boot upsert, schema metadata
refresh, or the §4.4 backfill, including a secret that could not be
encrypted), because a strict reader must never be handed an incomplete
document; "log and continue" is the posture for ordinary modules only. For a
required module the lazy-seed path is disabled for the rest of the process:
`GetConfig("auth")` returns an error if the document disappears instead of
rebuilding it from schema defaults, and `GetAllConfigs` — which feeds the
`/admin/modules` list, the very page an operator repairs from — **keeps
serving every other module** and reports the required module as a per-module
`missing` row (`ModuleConfigStatus{Name, Missing: true}`, rendered by the
list with a "configuration document missing — restore or restart" badge)
rather than failing the whole list. This is the intentional exception to the
SDK's dev-wipe self-healing (`internal/core/CLAUDE.md:109`, table row
updated): otherwise an admin page read could recreate both password keys as
`true` after the strict accessor had correctly observed the outage. Recovery
is restore the document, or fix Mongo and perform a controlled restart so
normal boot seeding can run; the operator-only break-glass remains available
meanwhile. The required-module set is populated once before serving traffic
and is read-only thereafter.

The password services treat a nil policy, a policy with a nil ConfigService,
or any read/parse failure as `ErrAuthPolicyUnavailable` → **503**
`auth.policy_unavailable` ("Sign-in policy is temporarily unavailable; try
again shortly"). Production wiring is mandatory and tested. Both SPAs render
503 as retryable; it never clears an existing session or session marker.
`LoginAllowed`, `RegistrationAllowed` and unrelated auth toggles keep their
current read behaviour; hardening those separate policies remains a follow-up
(§8). The provider/auto-link reads that participate in this feature's lockout
invariant become strict in §4.4.

**Break-glass.** `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS=true`
(`shared/config/config.go`, `AuthConfig.OperatorPasswordLoginBreakGlass`) may
override `PasswordLoginEnabled` only when `audience == operator`, and only in:

1. `POST /v1/auth/operator/login`; and
2. the MFA/WebAuthn continuation of a challenge created by that login.

It never enables client login, either registration route, forgot-password,
admin reset sends, password-confirm, auth-method reporting, or the password
credential counted by an OAuth-unlink guard. This is the minimum capability
needed to regain operator authentication without reopening public credential
creation or making a temporary override look like a durable login method. It
does not bypass the existing MFA, low-risk or `system.modules.admin` gates on
the repair itself; an operator must satisfy them normally (including enrolling
a factor if policy allows) or use the deployment's authorized ops/DB recovery
procedure.

The decision first evaluates persisted policy. A stored `true` is an ordinary
allow. For the operator audience only, an explicit boot-time override converts
either stored `false` **or a policy read/parse failure** into
`{Allowed:true, BreakGlassUsed:true}`; without it the original false/error is
returned. Thus an outage never silently fails open, while the recovery switch
still works when ConfigService itself is the outage. It does not override the
separate `loginEnabledAdmin` maintenance switch.

The env var is read once at boot. While set, the server logs a WARN at boot.
`PasswordAuthService.Login` retains the returned decision; a direct full-token
success emits `auth.policy.break_glass_used` after authentication. A partial
login copies `BreakGlassUsed` into `MFAChallenge`. Completion must be allowed
by a fresh decision; the challenge flag is audit context, never permission by
itself. The winning MFA/WebAuthn completion emits the event when either the
initial password check or the completion decision used break-glass. Failed
password attempts do not claim the override was used successfully. The event
contains audience, user UUID, SID and source IP but no password, token or full
email. `docker/.env.example` ships the variable commented out with the
procedure: set, restart, log in, repair OAuth, unset, restart. Audit
persistence has the best-effort semantics of §4.11.

### 4.3 Gates — which routes refuse, which stay open

The rule: **a route is gated iff it authenticates, or mints an unauthenticated
path to authenticate, with the password.** Routes that manage the credential
stay open, because the credential legitimately continues to exist — for the
other surface, for a later re-enable, and because deleting it is a different
decision this toggle does not make.

| Route | Verdict | Why |
|---|---|---|
| `POST /v1/auth/{tier}/login` | **403** `auth.password_login_disabled` | The password *is* the credential. The check sits right after `LoginAllowed` (`password_auth_service.go:466`) and **before** `GetUserForAuth`: lockout counters and the `RateLimiter` are untouched, and every email receives the same response. It uses `PasswordLoginDecision`; only the operator surface can receive `BreakGlassUsed=true`. |
| `POST /v1/auth/{tier}/register` | **403** | Creates a password credential the surface will not accept. It uses strict `PasswordLoginEnabled`, so break-glass never opens registration. The existing **operator-only** `isFirstUser` branch and `RegisterInitialAdmin` remain explicit bootstrap exceptions to G2; the Tier-2 client route never receives a first-user bypass. All three cases are tested and documented. |
| `POST /v1/auth/{tier}/forgot-password` | **403** | Public, and it mints a credential-setting token for a rejected method. It uses strict `PasswordLoginEnabled`, never break-glass. **The handler currently discards every service error** (`password_handler.go:201`, `_ = h.svc.ForgotPassword(...)`) to avoid enumeration; it now propagates *only* `ErrPasswordLoginDisabled` and `ErrAuthPolicyUnavailable`, both evaluated before the user lookup. Every account-specific error stays swallowed. |
| `POST /v1/auth/{tier}/mfa/login/verify`, `POST /v1/auth/{tier}/mfa/webauthn/login/finish` | **403** when the login challenge originated from a password | A challenge lives five minutes and records `SourceAMR:["pwd"]` / `LoginMethod:"password"`. `MFAChallenge` gains required `Audience` and `BreakGlassUsed` fields. Both completion handlers re-evaluate `PasswordLoginDecision` for password-sourced challenges: disabled/unset break-glass → 403 and atomic consume; policy unavailable without an active operator break-glass → 503 **without consuming**, so a transient failure can be retried within the original TTL; allowed → normal one-winner completion. An empty/unknown audience from an in-flight pre-v3 challenge is invalid and consumed; rollout waits one challenge TTL before exposing the switch. `MFAHandler` and `WebAuthnHandler` both gain mandatory policy wiring in the tier bundle. OAuth-sourced challenges are unaffected. |
| `POST /v1/admin/users/{userId}/send-password-reset` (and the client-user twin) | **409** `auth.password_login_disabled` | An operator action that would mint a reset for a method the target's surface rejects; the reset would also revoke the user's sessions (`password_auth_service.go:1032`) and leave them with an unusable password. Refusing is the honest answer. |
| `POST /v1/auth/{tier}/reset-password` | open | Consumes a token that only the gated forgot/admin routes can mint; tokens issued before the flip expire within `passwordResetTokenTTL` (≤24h). Completion revokes sessions but does **not** prove the user owns a matching IdP identity. The password remains stored for a later re-enable; the UI explains that it cannot currently sign in. |
| `POST /v1/auth/client/accept-invite` | open | Tier-2 onboarding sets a password and verifies the Orkestra email atomically. Auto-link permits a later OAuth login only when the IdP returns the same **verified** email (§4.4); it is a surface-level escape from a closed linking loop, not a per-user reachability guarantee. |
| `verify-email`, `verify-email/resend`, `change-password` | open | Not a credential (or authenticated credential management). |
| `POST /v1/auth/{tier}/me/password-confirm` | **409** `auth.password_confirm_unavailable` | §4.6 — different reason, existing code. |

New sentinels + codes:

```go
// services/password_auth_service.go
ErrPasswordLoginDisabled = stderrors.New("password login disabled for this surface")

// services/auth_policy_service.go
ErrAuthPolicyUnavailable = stderrors.New("auth policy unavailable")

// services/auth_service.go
ErrOAuthEmailUnverified  = stderrors.New("oauth provider did not verify the email")

// shared/errcode/codes.go
const AuthPasswordLoginDisabled = "auth.password_login_disabled" // 403 / 409 per table
const AuthPolicyUnavailable     = "auth.policy_unavailable"      // 503
const AuthLoginMethodLockout    = "auth.login_method_lockout"    // 422, §4.4
const AuthOAuthEmailUnverified  = "auth.oauth_email_unverified"  // 403 JSON / safe web callback code, §4.4
```

`ErrOAuthEmailUnverified`, `ErrAuthPolicyUnavailable` and their two codes
land in **PR 2** (§7) together with the strict boolean parser they share;
`ErrPasswordLoginDisabled` and `AuthLoginMethodLockout` land in PR 3.

### 4.4 Anti-lockout validator + verified-email auto-link

`internal/core/auth/config_validation.go` migrates from the old value-only
validator to the additive snapshot contract in §4.5. One policy function
validates active-config PATCH, named-environment PATCH and activation using the
exact non-secret values and secret presence of the target that would become
effective. The existing duration bounds run through the same function.

**Invariant, per surface `S ∈ {Admin, Client}`:**

```
passwordOn(S)   := strictBool(rawValues["passwordLoginEnabled"+S], default true)
providerOn(p,S) := strictBool(rawValues[p+"Enabled"+S], default false)
structural(p)   := effectiveClientId(p) ≠ "" ∧ effectiveRedirectURL(p) ≠ "" ∧ secrets(p)
    where secrets(google|github|discord) := secretPresent[p+"ClientSecret"]
          secrets(apple)                 := effectiveTeamId ≠ "" ∧ effectiveKeyId ≠ "" ∧
                                            (secretPresent["applePrivateKey"] ∨
                                             readableNonEmptyFile(effectivePrivateKeyPath))
oauthOn(S)      := ∃ p : providerOn(p,S) ∧ structural(p)
autoLink        := strictBool(rawValues["oauthAutoLinkByEmail"], default true)

valid(S)        := passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)
```

- **Defaults match the schema — and the stored document matches the schema.**
  Password and auto-link retain legacy `true`; all eight provider toggles
  default to `false` (`module.go:500-539`). Today the runtime accessor
  `OAuthProviderEnabled` treats an *absent* key as `true`
  (`auth_policy_service.go:499`), the opposite of the schema, and
  `SeedFromModules` never adds keys that a schema gained after the document
  was created (`config_service.go:137-157`: `RefreshMetadata` leaves
  `configValues` untouched). Two facts bound the exposure: the toggles were
  introduced in `14261ec` (2026-05-07), before `v0.1.0` (2026-05-23), so every
  released install seeded all eight keys at first boot; absent keys can exist
  only in documents created on pre-release dev/staging stacks. The design
  closes the drift source anyway: `SeedFromModules` gains a **backfill** step
  for existing documents — every schema key absent from the **active
  environment** whose `EnvVar`/`Default` is non-empty is written with that
  value, secrets included through the same encrypted path (each encrypted
  once), and the legacy mirror is rewritten as an exact copy of the resulting
  profile: profiles are the source of truth, and a mirror is never backfilled
  on its own, so a value present only in the profile reaches the mirror as
  that value rather than as a schema default. `configRevision` advances once
  (a lost compare-and-swap against a concurrently booting replica is re-read
  and retried), the backfilled key names are logged at INFO, and a failure —
  including a secret that cannot be encrypted — is recorded so
  `RequirePersistedConfig` (§4.2) refuses to serve a required module. A key
  whose fallback is empty stays **absent** on purpose: absence is meaningful
  to `GetRawValue` readers (ADR-0017 D1 — an absent `sessionAbsoluteTTL` is
  the default cap, a present empty one disables it), and a runtime read of
  such a key needs no guess because its answer is empty either way. It runs
  once per boot before traffic, so the runtime, the validator and the admin
  UI all read a document in which every schema key that has a value to be
  present with is present. The runtime usable-provider and
  auto-link accessors then use the same strict parser as the validator: an
  absent key (possible only for a fork that skips seeding) gets the schema
  default, and a malformed present value is an error rather than a silent
  default. G6 holds for every released install; a pre-release document is
  corrected by the backfill, not by a runtime guess.
- **Structural, not "has a client ID".** `OAuthConfigResolver.Get`
  currently treats any provider with a client ID as configured. The predicate
  above names every field the web flow needs. A strict
  `OAuthWebProviderUsable(ctx, audience, provider) (resolvedConfig, bool, error)`
  combines canonical provider-toggle parsing with the single exported pure
  `ProviderStructurallyConfigured`. Provider listing, web OAuth start, the web
  callback (which re-resolves the provider, so one disabled or blanked
  mid-flow is refused) and §4.7 use it. Runtime resolution comes from one
  required config read: the additive
  `ModuleConfigService.ActiveConfigRequiredModule(ctx, "auth")`, returning an
  `ActiveConfigView` of the active profile — raw and effective values with
  schema-secret keys stripped, every stored secret decrypted once, secret
  presence, the revision — whose read fails as a whole when a stored secret
  cannot be decrypted, so a check and the value it guards can never observe
  two different documents. Failure
  granularity is deliberate: a **document-level** failure (missing document,
  repository error, undecryptable document) makes
  `GET /v1/auth/{tier}/providers` and OAuth start return 503; a
  **per-provider** defect (a malformed toggle such as `"treu"`, a missing
  structural field) makes only that provider unusable — it is omitted from
  the list, OAuth start for it returns the existing 403
  `auth.oauth_provider_disabled`, and a WARN names the offending key — so one bad
  value cannot remove every social button while the password method still
  works. OAuth start builds the provider from the same resolved value rather
  than checking and then rereading. A
  path-backed Apple key counts only when the path identifies a readable regular
  file with non-empty content; PEM/credential correctness remains operational
  validation rather than G3. Native/mobile provider semantics remain outside this web-only change: the
  mobile ID-token endpoints keep the permissive `OAuthProviderEnabled` gate
  and gain only the JSON mappings of the two new sentinels.
- **Secrets belong to the target snapshot.** The validator never calls
  `ConfigService.GetSecret`: that would read the active environment while
  validating an inactive target. `ConfigValidationSnapshot.SecretPresent`
  contains names/booleans only, computed by ConfigService from the target
  environment's merged encrypted values, submitted secret values and schema
  env/default fallback. Stored ciphertext is decrypted only inside
  ConfigService to decide non-empty presence; a decrypt error aborts the
  mutation. A submitted replacement takes precedence before stored decryption,
  so an operator can repair corrupt ciphertext in one PATCH rather than being
  permanently blocked by it. No secret value crosses the validator boundary.
  A PATCH may save a non-empty provider secret and disable password atomically
  in the same request; an empty submitted secret cannot satisfy the invariant.
- **Auto-link configuration.** With `oauthAutoLinkByEmail=false`, an existing
  local account cannot acquire its first provider link without authenticating
  first. On a password-off surface that is a closed loop, so the validator
  requires auto-link. The callback migrates to strict
  `OAuthAutoLinkByEmailEnabled(ctx) (bool, error)`, read **before** the email
  lookup so an outage can never depend on account state; config
  uncertainty — a nil policy included — returns 503 `auth.policy_unavailable`
  on JSON surfaces and the allowlisted `oauth_provider_unavailable` on the
  web redirect (§4.10), in both cases before lookup/link/token issuance. This guarantees that the path is open,
  not that every local user owns a matching IdP identity.
- **Verified email is mandatory before any email lookup.** An existing
  provider-ID link may log in as today. For an unlinked provider identity,
  `HandleOAuthCallbackWithLinking` requires
  `userInfo["email_verified"] == true` **before** calling `GetUserByEmail` or
  entering either the auto-link or new-signup branch. False/missing returns the
  same 403 `auth.oauth_email_unverified` whether or not a matching local account
  exists, preventing an account-existence oracle as well as an unsafe link.
  Google, Apple and Discord keep their provider claims. GitHub's
  `getPrimaryEmail` (`github_oauth_service.go:210-256`) already queries
  `/user/emails` with `user:email`, picks the primary verified address and
  falls back to the first verified one, and the handler copies the verified
  bit into `userInfo["email_verified"]` (`auth_handler.go:1399`) — but
  `GetUserInfo` consults it only when the public profile carries no `email`
  and otherwise marks that free-text profile address verified by assumption
  (`:185-192`; v4.2 finding). `GetUserInfo` therefore always takes the
  address and its bit from `/user/emails` (primary verified, then any
  verified — a non-primary address is still one GitHub verified); the
  public-profile `email` survives only as an *unverified* fallback, which the
  callback then refuses. A public profile `email` is never marked verified by
  assumption. Comparison
  continues through the user service's canonical email lookup — no
  provider-specific dot/plus rewriting.
- **Symmetric.** The policy refuses turning password off with no usable
  provider, disabling the last usable provider or blanking one of its fields
  while password is off, and turning auto-link off while either surface is
  password-off. `loginEnabled{Admin,Client}` stays outside the invariant: it
  is the intentional all-method maintenance lockout.
- **Error shape.** Cross-field failures name
  `passwordLoginEnabled<S>` and return 422 `auth.login_method_lockout` with
  both exits: keep password enabled, or leave a structurally configured OAuth
  provider plus verified-email auto-link enabled. A malformed bool or
  unreadable/decrypt-failed snapshot is not silently coerced.

### 4.5 ConfigService snapshot validation + atomic writes

The frozen `Module` interface and existing `HasConfigValidator` /
`HasConfigActivationValidator` contracts remain unchanged. The SDK adds:

```go
type ConfigValidationSnapshot struct {
    Environment     string
    Values          map[string]string // raw merged target; presence/empty retained
    EffectiveValues map[string]string // runtime EnvVar/default fallback applied
    SecretPresent   map[string]bool   // effective names/presence; never plaintext
}

type HasConfigSnapshotValidator interface {
    ValidateConfigSnapshot(context.Context, ConfigValidationSnapshot) error
}
```

Dispatch is source-compatible: a module implementing the snapshot interface
uses it on all three mutation surfaces; otherwise PATCH continues through
`HasConfigValidator` and activation through
`HasConfigActivationValidator`. Auth migrates all duration and login-method
rules to the snapshot interface; tenant/notification remain untouched.

`Values` deliberately preserves absent versus explicitly empty values, which
strict boolean and duration validators need. `EffectiveValues` applies the
same non-secret empty → schema `EnvVar` → schema default fallback as runtime
`GetValue`; the structural provider predicate reads IDs, redirect URLs and the
Apple path from this map. `SecretPresent` applies the analogous target-profile
encrypted/submitted-secret → secret `EnvVar`/default fallback, but exposes only
a boolean. Thus validation and runtime agree even when credentials are supplied
by deployment environment rather than stored config.

ConfigService builds the complete snapshot **before encryption or
persistence**. Environment profiles are the source of truth; the top-level
legacy maps are compatibility mirrors, not a second independently merged
configuration:

- `UpdateConfig`: the active environment merged with the request (a legacy
  document first completes the existing lazy production/sandbox migration and
  is reloaded before its revision is captured); the accepted candidate is
  copied identically to active + legacy fields, repairing any pre-existing
  divergence;
- `UpdateEnvironmentConfig`: the named environment's merged candidate,
  including submitted secret presence;
- `SetActiveEnvironment`: the stored target environment, including its own
  encrypted secrets and schema fallback — never the current active profile.

`ModuleConfig` gains `ConfigRevision int64` (`bson:"configRevision,omitempty"`;
absent in an old document means zero). A monotone integer is deliberate:
`updatedAt` remains display metadata and cannot safely be a compare token
because BSON datetimes have millisecond precision. Every values/secrets or
activation mutation reads and validates revision `r`, filters the write on
`configRevision == r` (also matching missing when `r == 0`), and increments it
to `r+1`. A zero-match is 409 with body code `module.config_revision_stale`;
the constant is **SDK-owned** — it lives in `pkg/sdk/module` next to
`ErrRevisionStale` (`recordlist_mutation.go:12`), because the SDK cannot
import `internal/shared/errcode` — and the caller reloads and retries. Environment writes also increment the existing
`EnvironmentConfig.Revision`, so record-list clients retain their current
stale-removal protection. The record-list CAS path increments
`ConfigRevision` in the same update, so ordinary and record-list config writes
cannot pass each other unseen.

The repository interface gains one service-facing `CompareAndSwapConfig`
operation carrying the expected revision and an explicit mutation shape
(legacy map, one named environment and/or active-environment name);
`CompareAndSwapEnvironment` (record lists) takes the `needsRestart` value and
increments `configRevision` in the same update, deciding "is this the active
profile" server-side; the legacy-profile migration `MigrateToEnvironments`
becomes a compare-and-swap that matches only a document still without
profiles at the read revision; the four two-step write methods leave the
interface. `module.ConfigRepository` is provided *to* `ModuleConfigService`
by the host and never implemented *by* a module — the `RedisClient` category
in `pkg/sdk/CLAUDE.md` — so this is a **declared exception** to the SDK's
additive-only rule, not a breach of it: the only code that tracks it is a
fork's substitute repository (a test double), and the alternative — a parallel
CAS interface — would keep the non-atomic two-write path alive as the
service's fallback, which is the defect being removed. The production
repository translates it to **one** Mongo `UpdateOne` on the single
`module_configs` document:

- active `UpdateConfig` / `UpdateEnvironmentConfig` set the environment,
  its revision and the identical legacy values/secrets;
- inactive environment PATCH sets only that environment;
- activation sets the active name and copies the target's values/secrets into
  the legacy fields in the same server-side pipeline update;
- the same update sets `needsRestart` to the inverse of the registry's
  `SupportsHotReload(name)` result, so the presentation flag cannot diverge
  through a later best-effort clear.

The three service paths stop composing the old separate repository methods;
in-tree fakes are updated to the CAS contract. No path logs and swallows a
second-write failure because there is no second write. This optimistic guard
closes the write-skew case where one operator disables the last provider while
another concurrently disables password — two individually valid snapshots
cannot combine into an invalid result. Secret values never enter logs,
revision errors or validation errors.

Audit remains outside the repository transaction and is emitted from the
handler after the mutation result, with the best-effort semantics in §4.11.

**Request lanes are enforced server-side.** `PATCH /v1/admin/modules/{name}`
and `PATCH …/environments/{env}` carry `config` and `secrets` as two blocks;
the service refuses, before validation, encryption or persistence, any key
that does not belong where it was sent: a declared secret (scalar or
record-list sub-field) in `config`, a declared non-secret or a label key in
`secrets`, the SDK-owned roster key `<field>.__items` from either, and any
key the module does not declare — 422 with the SDK-owned code
`module.config_key_invalid`, naming the key. Classification uses the
module's **live** `ConfigSchema()`, never the stored snapshot (whose boot
refresh may have failed), so a field that became a secret in the binary can
no longer be written in plaintext through the older declaration. A module
that declares no schema keeps accepting anything. This is what makes "no
secret value crosses the validator boundary" true for the non-secret
`Values`/`EffectiveValues` maps, which are otherwise copied verbatim.

### 4.6 Step-up re-authentication

`RequireStepUp` offers a no-factor user the `password_confirm_required`
fallback and `ConfirmPasswordWithSecurity` mints a token with `amr += "reauth"`
after checking the password (`password_auth_service.go:1161`,
`shared/middleware/auth.go:970-1000`). A password that is not an accepted
credential cannot be a proof of presence either.

1. **Service (security).** `ConfirmPasswordWithSecurity` uses strict
   `PasswordLoginEnabled`, never break-glass, and returns the existing
   `ErrPasswordConfirmUnavailable` when the persisted method is off for
   `s.audience` — same branch as "no password hash". The handler already maps
   it to 409 and `PasswordConfirmModal` already renders it. A policy error →
   `ErrAuthPolicyUnavailable` → 503.
2. **Middleware (UX).** `StepUpPolicy` (`auth.go:68-78`) gains

   ```go
   // PasswordReauthAllowed reports whether a password may serve as the
   // re-authentication proof for the token's audience ("operator" |
   // "client"). False means the method is disabled; error means policy
   // could not be evaluated and must remain a retryable 503.
   PasswordReauthAllowed(ctx context.Context, audience string) (bool, error)
   ```

   The name differs from the service method deliberately: Go has no method
   overloading, and the middleware cannot import `services.PolicyAudience`.
   `AuthPolicyService` implements it as an adapter over strict
   `PasswordLoginEnabled`. `dispatchStepUpFailure` treats `allowed=false`
   like `roleRequiresMFA` and emits `mfa_enrollment_required`; an error (or a
   missing `StepUpPolicy`) emits 503 `auth.policy_unavailable`, preserving
   G4 rather than misreporting an outage as a need to enroll. Production
   wiring is `cmd/server/main.go:342`; the two in-tree fakes
   (`middleware/step_up_test.go:137`, `tenant/handlers/admin_mfa_routes_test.go:52`)
   gain the method. A fork implementing `StepUpPolicy` itself gets a compile
   error, which is the correct signal.

### 4.7 OAuth-unlink lockout guard — count *usable* links

`wouldLockOutOAuthUnlink` (`auth_service.go:591-611`) decides
`locked = PasswordHash == "" && activeLinks <= 1`, where `activeLinks` counts
every `IsActive` link regardless of whether its provider is enabled for the
audience or configured at all. Two corrections, one helper:

```go
func wouldLockOutOAuthUnlink(target *iface.User, links []iface.OAuthLink,
    provider iface.OAuthProvider, passwordUsable bool,
    usableProviders map[iface.OAuthProvider]bool) (providerID string, locked bool, found bool)
// targetUsable    := target link IsActive && usableProviders[provider]
// remainingUsable := count(other links where IsActive && usableProviders[Provider])
// locked := targetUsable && (!passwordUsable || target.PasswordHash == "") && remainingUsable == 0
```

Both callers (`AdminUnlinkOAuth:567`, `SelfUnlinkOAuth:633`) pass
`passwordUsable` from strict `PasswordLoginEnabled` — break-glass never counts
as a lasting credential. `AuthService` gains a
`SetProviderUsability(func(ctx, audience, provider) (bool, error))` seam wired
in `module.go`; it resolves `providerOn ∧ ProviderStructurallyConfigured`
against the active snapshot without importing the resolver type. The caller
precomputes the map for every active link before mutation; any config read,
decrypt or parse error refuses with 503 rather than counting an uncertain
link. The pure helper then enforces the existing 409 `last_credential` shape.

### 4.8 `AuthMethodsView` — split the password concept

`HasUsablePassword` (`models/auth_methods.go:11`) means "hash present" and is
read by six components, three of which decide whether an unlink is allowed
(`LinkedProvidersTab.tsx:107`, `AdminAuthMethodsCard.tsx:320`, and the
backend guard). Once the policy exists the name lies. The view gains:

```go
HasPasswordSet         bool `json:"hasPasswordSet"`         // hash present
PasswordUsableForLogin bool `json:"passwordUsableForLogin"` // hash present ∧ method on for this user's surface
HasUsablePassword      bool `json:"hasUsablePassword"`      // Deprecated: alias of HasPasswordSet, removed after one release
```

In-tree consumers migrate in this change: unlink decisions →
`passwordUsableForLogin`; "set / change / last updated" UI → `hasPasswordSet`.
The service resolves the usable flag through strict `PasswordLoginEnabled`;
policy failure returns 503 instead of guessing.
`PasswordTab` and `SecuritySummaryCard` show a policy-aware note when
`hasPasswordSet && !passwordUsableForLogin` ("Email/password sign-in is
disabled on this surface; the stored password is retained for a later
re-enable"). Operator and client users live in separate tier collections, so
the copy does not imply that one user row's hash is shared across surfaces.

### 4.9 Public policy endpoint

`GetAuthPolicyResponse` (`handlers/auth_handler.go:336-348`) gains
`passwordLoginEnabled *bool` (persisted state; nullable only in the emergency
case) and `passwordLoginBreakGlassEffective bool`. The second field is true
only on the operator endpoint when the boot-time env var is set **and** the
persisted result is false/unavailable; a stored true needs no override. It is
always false for the client endpoint.

Normal read success returns a non-null persisted bool. A read error without
operator break-glass returns **503**, never a guessed value. A read error with
operator break-glass returns 200 with `passwordLoginEnabled:null` and
`passwordLoginBreakGlassEffective:true`, allowing the emergency login form
while making the unknown persisted state explicit. The break-glass flag does
not change registration/recovery/link UI. On an ordinary `/policy` 503 the
SPAs keep their existing fail-open display fallback; the backend still
refuses. OpenAPI dump regenerated; `make openapi-check` gates it.

### 4.10 SPAs

Both SPAs read `/policy` with a fail-open default. The new fields default to
`passwordLoginEnabled:true` and `passwordLoginBreakGlassEffective:false` in the
fallbacks.

**frontend-admin** (Tier-1 console — reference-first workflow applies)

| Component | Change |
|---|---|
| `store/api/authApi.ts:145,179` | nullable `passwordLoginEnabled` + `passwordLoginBreakGlassEffective` on the policy; `hasPasswordSet` + `passwordUsableForLogin` on `AuthMethodsView` |
| `components/authentication/EmailPasswordForm.tsx` | renders the login fields when persisted policy is true **or** operator break-glass is active. Under break-glass it shows an emergency-access warning and hides forgot-password + register CTAs; when persisted false/null without the override it renders nothing. |
| `components/authentication/RegisterForm.tsx`, `ForgotPasswordForm.tsx` | when persisted false/null, the same "disabled" alert path used for `!registrationEnabled`; direct navigation must not show a working form, including during break-glass |
| `components/authentication/Login.tsx` | when persisted false/null, break-glass false, and `SocialLoginForm` resolves an empty provider list, render a *no sign-in method available — contact an administrator* alert (`onProvidersResolved(count)`, no second query). A provider-query error renders a retryable error, not the empty-method state. |
| `pages/user/security/{LinkedProvidersTab,PasswordTab}.tsx`, `pages/user/settings/SecuritySummaryCard.tsx`, `pages/admin/user-profile/AdminAuthMethodsCard.tsx` | §4.8 field migration + policy-aware copy |
| `pages/admin/user-profile/AdminAuthMethodsCard.tsx` | send-password-reset button disabled with a tooltip when `!passwordUsableForLogin` (the backend 409s anyway) |
| `pages/admin/modules/useModuleConfigController.ts` | distinguishes `module.config_revision_stale` by body code, not every 409 as a record-list conflict. It latches a conflict, disables Save and offers **Reload & review**. Refetch adopts the newest baseline and recomputes the unsaved diff; non-secret draft and any unsent secret remain only in component memory, are never auto-submitted, and clear on unmount. |
| `components/authentication/SocialAuthCallback.tsx`, `LoginMfaVerify.tsx` | synchronously copies then scrubs query/fragment before the first await. Direct success force-dispatches and awaits the session endpoint; it removes the current invalidate + fixed 100 ms race and navigates only after the refresh-cookie session is confirmed. MFA renders an extracted `MfaVerifyPanel` locally with challenge props held only in component memory, never `location.state` (the password path's `LoginMfaVerify` page keeps reading router state — which never travels in a URL — until PR 3 reworks that form). Direct success lands on the SPA's `DEFAULT_POST_LOGIN` when no fresh return target exists. Stable error codes are allowlisted/i18n-mapped; raw URL text is never rendered. The parser is **closed**: `provider` must be one of `google|apple|github|discord`, `webauthnAvailable` must be exactly `true` or `false`, `mfaToken` must be non-empty, and a payload that combines an MFA fragment with a query outcome, or `success=true` with an `error`, is the generic failure. The existing sanitized return target becomes a ten-minute timestamped take-and-delete record on every outcome; the take runs in a layout effect, never during render. |
| i18n | `auth`: `pages.loginNoMethod`, `pages.passwordLoginDisabled`, `pages.passwordBreakGlassActive`, `pages.providersUnavailable`, `security.passwordKeptNotice`, callback error-code mappings; `adminModules`: config-revision conflict/reload copy |

**frontend-client** (Tier-2 demo — TanStack Query, react-router v8, Tailwind;
no RTK). Today it has **no OAuth path at all** (`api/auth.ts` wraps password,
MFA and account routes only; `App.tsx` has no callback route), so a client
switch would strand it. This change adds the minimum web OAuth login:

| Piece | Change |
|---|---|
| `api/auth.ts` | `fetchOAuthProviders()` → `GET /v1/auth/client/providers`; `initiateOAuthLogin(provider)` → `POST /v1/auth/client/oauth/login` `{provider}` → `{authUrl, state}` → `window.location.assign(authUrl)`. Both calls use `credentials:'include'`; provider names are an allowlisted union, not arbitrary strings. |
| `pages/LoginPage.tsx` | provider buttons under the credentials stage. Loading, empty and query-error are distinct states; an error is retryable and never reported as "no method". When password is false the form/links are hidden; false + resolved-empty providers renders the no-method alert. A validated same-origin relative `next` value plus creation time is saved in `sessionStorage` before OAuth start. The callback take-and-deletes it on every outcome and uses it only on success within ten minutes. The validator parses against `window.location.origin`, requires the same origin and a path beginning with exactly one `/`, and rejects protocol-relative, raw/encoded backslash and callback-self-loop targets. |
| `auth/AuthProvider.tsx`, `auth/tokenStore.ts` | add `bootstrapFromRefreshCookie()`: stamp the non-secret session marker **before** calling `refreshAccessToken`, because the existing function short-circuits without it. `ok` installs the token; `signed-out` clears the speculative marker; `unavailable` keeps it and exposes a retry action. Access tokens remain memory-only. |
| `pages/OAuthCallbackPage.tsx` (new) + `App.tsx` route `/auth/callback` | captures and immediately scrubs callback query/fragment via `history.replaceState`. Success calls `bootstrapFromRefreshCookie` then navigates to validated `next` or `/account`. MFA renders the same extracted `MfaChallenge` component used by `LoginPage` locally — no challenge in router state/storage — and `signIn`s the returned access token. Error values are matched against an allowlist of stable backend codes and translated; raw URL text is never rendered. |
| `pages/SignupPage.tsx`, `components/Layout.tsx` | hide the signup form / nav CTA when `false` (password signups) |
| `api/auth.ts` policy type + both fallbacks | nullable `passwordLoginEnabled` + break-glass field (always false on this tier); policy 503 remains fail-open for display only, while direct forms show the backend's retryable policy error |

**Backend prerequisite and safe callback contract.** Every provider currently
builds its own redirect against `target.config.Server.FrontendURL`; a client
flow can therefore land on the operator console. One `target.spaURL()` uses
the tier's resolved `deps.frontendURL` (`OPERATOR_FRONTEND_URL` /
`CLIENT_FRONTEND_URL`, falling back to `FRONTEND_URL`), handed to each
tier's handler by `module.go`, for success, signup-disabled, error and
MFA-partial redirects. The `Origin`-derived frontend `RedirectURI` is populated only from the configured tier SPA for
stored-state compatibility, never concatenated from request input.
The provider's backend callback URI remains the resolver's configured value.
The configured SPA URL is the sole post-login destination.

**The state is one-shot and bound to provider, tier and browser.** The
Redis row is consumed with the store's atomic `Take` (Redis `GETDEL`) — a
`Get` followed by a deferred delete lets two concurrent callbacks both read
it — so exactly one presentation of a state can proceed and a replay is a
terminal 400. State resolution compares, in order, the JWT signature and
expiry, the browser binding, the row's `tier`, **the row's `provider` against
the endpoint's provider**, and the link-mode pair; every mismatch is the same
generic 400, and all of it happens before the IdP `error`, the code or any
profile is interpreted.

**Client-tier flows complete through a one-shot relay on the client API
host.** Every provider callback is mounted on the operator host (one
redirect URI per provider), and a response from `console.example.com` cannot
set a cookie for `api.example.com` — the browser rejects a `Domain` that
does not match the response host, and the cross-tier isolation model
deliberately has no shared parent domain. The operator-host callback
therefore never completes a client-tier flow. For `tier=client` (login mode
only — the link route is operator-only) the browser binding is **deferred**,
not skipped: the callback resolves the state (signature, atomic take, tier
and provider cross-checks), performs the IdP half (code exchange, user info)
with the provider resolved strictly from one config read, then stores a
**relay record** — tier, provider, the state's CSRF nonce, the user-info map,
the provider tokens, security context and device info — encrypted at rest
under a fresh random id with a 60-second TTL, and redirects (with the same
no-referrer/no-store headers) to `GET {CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`.
That endpoint, on the client API host, takes the record atomically (a second
presentation finds nothing), requires the `orkestra_oauth_state` cookie the
same host set at start to equal the record's nonce in constant time —
missing, foreign or link-mode/wrong-tier records are a terminal 400 with no
redirect and no token minted — clears that cookie, runs the application
half (`HandleOAuthCallbackWithLinking` on the client authService), sets the
refresh cookie on its own host and cookie domain, and redirects to the
client SPA under the closed contract below. A login-CSRF attempt — an
attacker-started state finished by a victim's browser — thus reaches the
relay without the attacker's nonce cookie and is refused before any token
exists. `CLIENT_API_URL` (the client API's public origin, falling back to
`https://`/`http://` + `CLIENT_API_HOST` per environment) is the relay's
destination; when the client surface is not configured a client-tier state
is refused at the callback. Operator-tier and legacy (`tier=""`) flows start
and end on the operator host: the binding cookie is required there and the
flow completes inline. The relay id is a single-use, browser-bound handle
like the IdP `code`, never a credential; the forbidden-field rule applies to
the relay redirect as to every other.

One `target.oauthLoginCallbackURL(result)` replaces every provider's literal
login/signup/MFA redirect and sets `Referrer-Policy: no-referrer` and
`Cache-Control: no-store` on the redirect response. Its wire shape is closed:

- success query: `?success=true&provider=<allowlisted-provider>`;
- failure query: `?success=false&error=<allowlisted-stable-code>`;
- MFA continuation: `#requiresMfa=true&mfaToken=<one-shot-id>&webauthnAvailable=<bool>`.

The failure allowlist is closed to `oauth_access_denied`,
`oauth_signup_disabled`, `oauth_link_disabled`,
`auth.oauth_email_unverified`, `oauth_provider_unavailable` and the generic
`oauth_login_failed`; unrecognized/internal/provider-specific errors collapse
to the generic code. Account status and local lookup results are never encoded.
Config uncertainty on this surface — the strict auto-link read or the
document-level provider resolution failing — maps to
`oauth_provider_unavailable`, because a redirect cannot carry a 503; the JSON
surfaces keep 503 `auth.policy_unavailable`.

The backend resolves trust before destination: a missing/invalid/replayed state
or failed CSRF-cookie binding gets a terminal generic 400 with no SPA redirect,
because no trusted tier exists yet. With a valid one-shot state, IdP denial,
missing code, provider/user-info failure and application rejection redirect to
the configured tier SPA with an allowlisted coarse code; raw IdP/error text is
logged only in sanitized server fields and never copied to the URL. State is validated before interpreting an IdP `error`, and the state cookie is cleared
on every valid-state terminal outcome. Apple's dev-only fallback that
fabricated a state when the form-post carried none is removed. Every provider
callback — GitHub included, which moves from a Huma operation that could not
set the refresh cookie to the same raw chi handler as the others — runs one
shared implementation that re-resolves provider usability from the same
strict read OAuth start used.

The fragment keeps the five-minute one-shot challenge out of HTTP requests,
reverse-proxy logs and referrers; both SPAs copy it into component memory and
scrub it immediately. **No callback URL may contain an access token, refresh
token, email or user ID.** This explicitly removes the legacy, unregistered Apple Huma
callback (`access_token=...` in its query) and the PII fields emitted by all
success redirects. Success authentication is recovered only from the audience-scoped
HttpOnly refresh cookie. Structural tests scan every callback builder for the
forbidden parameter names in addition to behavioural redirect tests.

Authenticated link mode keeps its distinct `/user/security?tab=oauth` return
contract, but its helper accepts only the provider enum plus a stable result
code (`already_linked`, `duplicate_provider`, `invalid_userinfo`,
`access_denied`, `provider_unavailable`, `internal`) and applies the same
no-referrer header/forbidden-field rule. It is not
forced through the login callback state machine.

### 4.11 Audit — one best-effort generic event per mutation attempt

No module-config PATCH is audited today (`pkg/sdk/module/admin_routes.go`,
`handler.go` — no sink, no actor). Rather than an auth-only event, the SDK
admin handler emits one event for every authenticated mutation that reaches
it, which covers this switch and every other toggle in the platform:

| Route | `Action` | `Outcome` |
|---|---|---|
| `PATCH /v1/admin/modules/{name}` | `module.config.updated` | existing `success` / `failure` vocabulary |
| `PATCH …/environments/{env}` | `module.config.updated` | same, `Metadata.env` set |
| `PUT …/active-environment` | `module.config.environment_activated` | same |
| enable/disable via the PATCH body | `module.enabled` / `module.disabled` | same |

`ResourceType: "module"`, `ResourceID: <name>`, `Metadata: {env, keys:
[non-secret keys changed], secretKeys: [names only], code: <stable code if
any>, requestId: <server request ID>}`. **Values are never recorded** — a 422
on `passwordLoginEnabledAdmin` is visible as the key plus the code, which is
what an auditor needs and can be correlated with structured request logs.
Key lists are derived from the server-side schema, sorted/deduplicated and
bounded; record-list element slugs are collapsed to their schema field/item
names, and unknown request keys contribute only an `unknownKeyCount`. This
prevents operator-supplied key text from becoming a PII/log-injection channel.

One HTTP PATCH may carry both an enabled-state mutation and config values;
the handler emits separate events and records the actual result of each
operation. Candidate validation **and the config CAS write** happen before an
enable/disable side effect. Therefore validation, encryption, persistence or
stale-revision failure cannot still change module lifecycle state. If config
succeeds and the later lifecycle transition fails, the config remains changed
and the two events report those distinct actual results; the response is still
an error. Validation 422, stale-revision 409 and infrastructure errors use
`outcome=failure`; stable errors also carry their code, including
`module.config_revision_stale`. This reuses the compliance model's existing
outcome vocabulary instead of inventing values its consumers do not know.

Wiring: `pkg/sdk` cannot import `internal/shared/middleware`, so
`ModuleAdminHandler` gains two setters, `SetAuditSink(iface.AuditSink)` and
`SetActorResolver(func(ctx) module.AdminActor)` (`AdminActor{UserID, TenantID,
TenantKind, IP, UserAgent, RequestID string}`), both nil-tolerant. Full actor
email is deliberately omitted: the immutable user UUID is sufficient
attribution and avoids duplicating mutable PII. `cmd/server/main.go` wires them
after `RegisterAll`, where the compliance module has registered
`ServiceAuditSink` (`services.go:163`) and the middleware's claims accessor is
importable — the same post-init pattern the existing `SetAuditSink` seams use
(`main.go:310`). Nil remains tolerated for SDK embedding and isolated tests,
but the in-tree server wiring test requires both setters.

`iface.AuditSink.Emit` intentionally returns no error and the compliance sink
logs insert failures rather than changing the hot-path result. The current
implementation performs the insert on a detached context with a two-second
timeout: it may add up to that bounded latency, despite returning no error.
Therefore "emits" is the guarantee, not durable persistence: every sink
failure produces a structured WARN with action/resource/outcome (never values
or secrets), the config response retains its real result, and no spec/doc calls
the write transactional, guaranteed or zero-latency. A future durable outbox
is a separate compliance decision rather than an implicit change to every
current audit producer.

The events reuse `compliance_audit_events` and its existing two-year TTL; no
new PII store is created. Actor UUID, internal tenant context, IP and user
agent are retained only for privileged-change/security forensics; values,
secrets and email are excluded. Deployers remain responsible for documenting
the applicable lawful basis and retention in their RoPA/privacy materials.
Because emission is best-effort, this design does **not** claim complete SOC2
evidence or operating effectiveness; deployments that require guaranteed
evidence need the durable-outbox follow-up in §8.

### 4.12 Documentation (same commit as the code it describes)

- `backend/internal/core/auth/CLAUDE.md` — Login & Sessions row gains the pair,
  strict-read semantics, validator invariant, verified-email auto-link and the
  operator-only break-glass; step-up and route tables gain the pending-
  challenge re-check; the callback contract documents no token/PII in URLs.
- `backend/pkg/sdk/CLAUDE.md` + `docs/site/sdk/config-service.mdx` — validation
  snapshot contract, target secret-presence semantics, optimistic concurrency,
  single-document atomic writes, required-persisted-module exception,
  best-effort audit event and handler setters. `backend/internal/core/CLAUDE.md`
  updates its blanket lazy-rebuild statement with the `auth` fail-closed
  exception.
- `backend/internal/core/compliance/CLAUDE.md` +
  `docs/site/modules/core/compliance.mdx` — module-config event vocabulary,
  minimized actor fields, existing two-year TTL and explicit best-effort/SOC2
  limitation.
- `docs/site/modules/core/auth.mdx` — field count (63 → 65), an "SSO-only
  surface" paragraph under Login & Sessions covering the invariant, the
  break-glass and the "blocks new authentications except bootstrap" semantics.
- `docs/site/operating/oauth-providers.mdx` — a short "Going SSO-only" section
  pointing at the invariant; `docs/site/architecture/authentication-flow.mdx`
  — the client-SPA OAuth path.
- `frontend-client/CLAUDE.md` — OAuth login, callback bootstrap/session-marker
  choreography and Vitest in the current surface; `docker/.env.example` —
  `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` commented out with the procedure.
- `backend/internal/shared/middleware/auth.go` — the `StepUpPolicy` doc comment
  (the interface comment is the contract; there is no `middleware/CLAUDE.md`).

## 5. Edge cases

| # | Case | Decision |
|---|---|---|
| 1 | Password turned off with no usable provider on that surface | 422 `auth.login_method_lockout` (§4.4), in both directions, on PATCH and on environment activation. |
| 2 | Provider passes structural validation but fails operationally (wrong credential, IdP outage, IdP-side redirect mismatch) | Outside G3 by design. Break-glass restores only operator authentication; the operator must still satisfy MFA, low-risk and RBAC to repair config. A client user waits or an authorized operator re-enables the client password method. The UI never claims each user is guaranteed an IdP identity. |
| 3 | Fresh/empty user DB with a restored password-off auth document | Existing **operator-only** first-user `Register` and setup-wizard exceptions remain reachable and are explicitly outside G2. Tier-2 `Register` remains blocked even when `client_users` is empty. The exceptions are not enabled by break-glass and cannot create a second bootstrap user; concurrent first-user ownership remains protected by the existing sentinel. |
| 4 | Which password routes refuse | §4.3 table. |
| 5 | Password login started before the flip, MFA verify after | Refused and atomically consumed. Policy store temporarily unavailable → 503 and challenge retained; operator break-glass active → allowed and audited after the winning completion. |
| 6 | Step-up `password-confirm` fallback | Disabled method → 409 at the service and `mfa_enrollment_required` from middleware. Policy failure/missing wiring → 503, not a fabricated enrollment requirement. Break-glass does not enable it. |
| 7 | OAuth unlink of the sole *usable* link while the user has a password hash but the method is off | 409 `last_credential` (§4.7). Removing a disabled/unconfigured target link is allowed because it is not a usable credential; disabled links are also excluded when deciding whether another usable link remains. |
| 8 | Sessions opened with a password before the flip | Not revoked; refresh rotation continues until expiry or revocation. Stated in the field description; bulk revoke by `LoginMethod` is a follow-up (§8). |
| 9 | Enumeration / lockout counters | Gate sits before the user lookup; identical response for every email; failed-login counter and rate limiter untouched; `forgot-password` propagates only the two per-surface errors. |
| 10 | Config read failure, missing auth document, malformed bool, nil reader/service or unknown audience | 503 `auth.policy_unavailable`; never a silent `true`. `auth` is not lazy-reseeded during the running process. An absent new key in an existing valid document alone preserves legacy `true`. |
| 11 | Invitee on a password-off client surface | Invite redemption remains open and stores a password. A later OAuth attempt auto-links only with the same provider-verified email. No match means the user cannot enter until an operator changes policy or an invite-bound OAuth flow is added. Copy states this limitation. |
| 12 | Password-only user with no prior OAuth link | Auto-link is available but conditional on a matching verified IdP email. The validator prevents a closed configuration loop, not per-user lockout. |
| 13 | Both surfaces flipped independently | Own key, gate and validator clause. Operator/client rows are separate; no copy or API implies one row's password is shared across tiers. |
| 14 | Existing deployment upgrades | Existing auth document + absent password keys → `true`; the boot backfill (§4.4) writes every other absent schema key that has a non-empty default before traffic and rebuilds the legacy mirror from the active profile, so no runtime read guesses; a backfill failure on `auth` stops the boot (§4.2). Provider toggles were seeded on every released install (they predate `v0.1.0`). Missing document is an outage, not an upgrade default. `hasUsablePassword` remains one-release compatibility output. |
| 15 | `/policy` unreachable from the SPA | Fail-open display (existing); the backend still refuses; never a lockout. |
| 16 | PATCH saves provider secret and disables password together | Accepted when the complete merged snapshot is valid; values+secrets persist in one atomic document update. Empty secret is not presence. |
| 17 | Inactive target lacks a secret that the active profile has (or vice versa) | Target is judged from its own `SecretPresent` map. Invalid target is refused; valid target is not rejected because another profile differs. |
| 18 | Stored secret cannot decrypt / Apple path is missing or unreadable | Mutation/activation fails; the validator never borrows another environment's secret or counts a non-existent file. A PATCH supplying a replacement secret can repair corrupt ciphertext. |
| 19 | Concurrent PATCH disables password while another disables the last provider | One optimistic update wins; the other gets 409 `module.config_revision_stale`, reloads and then fails the invariant. No write skew. |
| 20 | Config repository write fails | The single Mongo update applies all target fields or none. No legacy/environment partial state and no success response. |
| 21 | Unlinked OAuth identity has an unverified/missing email | 403 `auth.oauth_email_unverified` before local email lookup, with the same result whether an account exists; no signup, provider link or token. Existing provider-ID links are unaffected. GitHub public profile email is never assumed verified. |
| 22 | OAuth callback URL leakage | Success/error query contains only allowlisted status/provider/code. MFA one-shot ID travels in a fragment and is immediately scrubbed. Access/refresh tokens, email and user ID are forbidden in every callback URL. |
| 23 | Client OAuth callback has refresh cookie but no session marker | `bootstrapFromRefreshCookie` stamps the marker before refreshing; signed-out clears it, 503 retains it for retry. Access token remains memory-only. |
| 24 | Break-glass left on | WARN on every boot; the operator policy response and login page visibly flag emergency access; successful rescued operator logins are logged/audited. Client/login-method views/register/forgot/unlink remain governed by persisted policy. Docs end with unset + restart; no automatic expiry because env is boot-time operator control. |
| 25 | Audit sink absent/fails | In-tree wiring is tested; SDK embedding may omit it. Emit failure is WARN-visible and may add the sink's bounded two-second timeout, but does not roll back config. Documentation says best-effort, never guaranteed persistence. |
| 26 | Service accounts (ADR-0014), refresh rotation, `/dev/token` | Not password logins; untouched. `/dev/token` remains development-only and is not a production recovery path. |
| 27 | Callback says success but session bootstrap is signed-out/unavailable | Neither SPA enters a protected route on the status flag alone. Signed-out returns to a generic login error; unavailable retains only the minimum bootstrap state and offers retry. Admin awaits the session query; client awaits refresh-cookie adoption. |
| 28 | OAuth return target is stale or crafted | It is take-and-deleted on every callback outcome, ignored after ten minutes, and accepted only after same-origin canonical validation; fallback is the fixed account/profile route. |
| 29 | One provider toggle holds a malformed value (`"treu"`) while password is still on | That provider alone is unusable: omitted from `/providers`, 403 on its OAuth start, WARN naming the key; other providers and the password form keep working. Only a document-level read failure escalates to 503. The validator rejects the malformed value on the next write. |
| 30 | Required `auth` document missing while an operator opens `/admin/modules` | The list renders with every other module and a `missing` badge on `auth`; `GetConfig("auth")` and every strict policy read fail closed. Nothing lazy-reseeds. |
| 31 | Client-tier web OAuth callback lands on the operator host | The callback never sets the client cookie or mints tokens there: it stores a one-shot encrypted relay record and redirects to `CLIENT_API_URL/v1/auth/client/oauth/complete?relay=`; the client API host verifies the browser binding, completes, sets its own cookie (§4.10). With no client surface configured the state is refused with 400. |
| 32 | Relay id replayed, expired, presented without the start-host state cookie, or with a foreign nonce | Terminal 400, no redirect, no token; the record was consumed by the first take. |
| 33 | Two concurrent callbacks present the same state | Atomic `Take`: exactly one proceeds, the other is a generic 400. |
| 34 | A state started for one provider is presented to another provider's callback | Generic 400 inside state resolution, before the IdP `error`, code or profile is read. |

## 6. Testing

**Backend**

- `services/auth_policy_service_test.go` — existing auth document with an
  absent key → `(true,nil)`; explicit true/false for each audience; audience
  isolation. Missing auth document, repository/read failure, malformed or
  empty-present value, nil reader/service and unknown audience all return an
  error. The decision helper may apply break-glass only to operator login;
  the persisted-policy accessor used elsewhere never does.
- `pkg/sdk/module/config_required_test.go` — boot seeding may create `auth`,
  but once marked required, `GetConfig("auth")` errors on a missing document
  and `GetAllConfigs` neither lazy-seeds it nor fails: it returns every other
  module plus a `missing` row for `auth`. Ordinary non-required modules retain
  current self-healing. The required set is immutable once traffic starts.
- `pkg/sdk/module/config_backfill_test.go` (new) — an existing document
  missing schema keys gains those with a non-empty `EnvVar`/`Default` on the
  next boot in the active environment, and the legacy mirror becomes an exact
  copy of the resulting profile (a stale or divergent mirror is realigned, a
  profile-only value is never replaced by a default); present keys (including
  explicit empty strings) and empty-fallback keys are untouched; secrets are
  backfilled encrypted, once, with identical ciphertext in profile and mirror;
  `configRevision` advances exactly once; a lost compare-and-swap is re-read
  and retried; a document with every key present is not rewritten; a secret
  that cannot be encrypted fails the backfill, is recorded, and
  `RequirePersistedConfig` refuses the module.
- `pkg/sdk/module/config_lanes_test.go` (new) — a secret in the `config`
  block, a non-secret or label in `secrets`, the roster key from either, an
  undeclared key or sub-field are refused with `module.config_key_invalid` on
  the bare PATCH, the named-environment PATCH and the record-list path (the
  roster key is refused, never silently stripped); the live schema decides
  over a stale stored one; a schema-less module accepts anything; nothing
  reaches the validator, the encryptor or the repository.
- `pkg/sdk/module/config_required_test.go` also covers the boot gate: a
  missing document, a recorded seeding, metadata-refresh or backfill failure
  makes `RequirePersistedConfig` refuse; a healthy document seals the set.
- `services/gates_test.go` — `Login`, `Register` and `ForgotPassword` return
  `ErrPasswordLoginDisabled` per audience, while the other audience is
  unaffected. Operator break-glass permits only `Login`; client login and
  both registration/recovery paths remain blocked. First-user `Register` and
  `RegisterInitialAdmin` bootstrap remain intact, while an empty Tier-2 user
  collection receives no bypass. Policy refusal/read failure occurs before
  lookup and leaves failed-login counters untouched.
- `handlers/error_mapping_test.go` — disabled, policy-unavailable, lockout and
  unverified-OAuth-email sentinels map to their documented status/code.
  `ForgotPassword` policy-off returns 403 without lookup; policy-on returns the
  same 200 generic response for known and unknown email.
- Public-policy handler tests — normal persisted true/false is non-null;
  read error without break-glass → 503; operator read error with break-glass →
  nullable state + effective flag; stored true keeps the flag false even when
  the env var is set; the client endpoint never exposes an effective override.
- `handlers/mfa_login_verify_test.go` (new) — both MFA and WebAuthn finish
  handlers re-check password-sourced challenges. A post-flip refusal consumes
  the challenge and returns 403; a transient policy error returns 503 and
  retains it; an unknown challenge audience is consumed; OAuth-sourced
  challenges are unaffected. Operator break-glass permits one winning
  completion and produces exactly one rescued-login audit event.
- `config_validation_test.go` (extend) — table over both surfaces and every
  mutation hook: provider defaults are false; missing client ID, redirect URL,
  target-environment secret, Apple team/key/file, or auto-link is rejected
  when password is off. A simultaneous secret submission plus password disable
  succeeds. Active and target secret maps cannot satisfy each other; decrypt
  failure and unreadable Apple key fail validation, while a submitted
  replacement can repair corrupt ciphertext in the same PATCH. Disabling or
  blinding the last usable provider and activating an invalid profile are
  rejected. The provider-list and web OAuth-start paths use the same
  structural predicate;
  a document-level read failure returns 503 rather than an empty list; a
  malformed toggle on one provider omits only that provider (WARN, 403 on its
  OAuth start) and never implicitly enables it.
- OAuth auto-link tests (`services/gates_test.go`) — false or missing
  `email_verified` never links or issues a token **or calls local email
  lookup/signup**, with the same external response for a would-be
  known/unknown address; true permits the existing-email link/new-user
  branch; an already linked provider ID is unaffected. Missing/malformed
  auto-link policy, or a nil policy → 503 before lookup/link/token issuance.
  The former `TestOAuthCallback_PropagatesEmailVerifiedFromIdP`, whose
  false/missing cases asserted an unverified signup, is replaced.
  `services/github_oauth_service_test.go` (new) drives `GetUserInfo` through
  an injected transport: GitHub ignores public-profile email and selects only
  the primary verified address returned by `/user/emails`, then any verified
  one; a profile-only address comes back unverified.
- `services/oauth_provider_usability_test.go` (new) — the pure structural
  predicate per provider (Apple inline key vs readable key file via an
  injected probe), `ReadableNonEmptyFile`, and the resolver's granularity: a
  document-level failure is an error, an absent toggle is the schema default
  `false`, a malformed toggle or missing field makes only that provider
  unusable with a WARN naming the key and no value, audience isolation, and
  the usable list in canonical order from one read.
- `pkg/sdk/module/config_active_view_test.go` (new) — a missing document, a
  repository error and an undecryptable stored secret fail the read;
  raw/effective/secret/presence semantics; a plaintext under a secret key is
  stripped; the live schema supplies fallbacks over a stale stored copy; the
  active profile only.
- `services/password_confirm_test.go` — persisted method off →
  `ErrPasswordConfirmUnavailable`; read failure →
  `ErrAuthPolicyUnavailable`; break-glass is ignored.
- `services/auth_service_admin_unlink_test.go`,
  `auth_service_self_unlink_test.go` — sole usable link + hash + password off
  → `last_credential`; removing a disabled/unconfigured target is allowed;
  disabled remaining links do not save removal of the last usable target;
  policy/config uncertainty → 503; password on permits the expected case.
- `services/auth_service_get_methods_test.go` — the three §4.8 fields, tier
  isolation and policy failure → 503.
- `shared/middleware/step_up_test.go` — no factor, role not requiring MFA,
  `PasswordReauthAllowed=false` → `mfa_enrollment_required`; error or nil
  policy → 503. All fakes implement the new signature.
- `handlers/admin_user_auth_security_events_test.go` (extend) —
  `send-password-reset` → 409 when the target's surface is password-off and
  503 when policy cannot be established.
- `handlers/oauth_callback_redirect_test.go` (new, builders) plus
  `handlers/oauth_callback_flow_test.go` (new, the four raw handlers through
  fakes) — success, stable failure,
  signup-disabled and MFA redirects use the correct tier URL with the
  documented fallback. MFA data is fragment-only; the response sets
  `Referrer-Policy: no-referrer`. Missing/invalid/replayed state never
  redirects; valid-state IdP denial/failure uses only a coarse allowlisted code
  and clears the CSRF cookie. Behavioural assertions plus a structural scan
  reject callback parameters named `access_token`, `refresh_token`, `email` or
  `user_id`, and confine every callback-path literal to the builder file, so
  the legacy Apple path cannot return. `handlers/oauth_providers_handler_test.go`
  (new) covers the list/start granularity (503 document-level, 403
  per-provider, the stored `RedirectURI` from the tier SPA). The flow tests
  also prove: a client-tier state is never completed on the operator host —
  no cookie, no token, a redirect to the relay endpoint carrying only the
  relay id — and the relay endpoint sets the client cookie only when the
  start-host state cookie matches the record's nonce, refusing a missing or
  foreign cookie, a replayed id, a link-mode or wrong-tier record with 400
  and no redirect; a state stored for one provider presented to another
  provider's callback is a 400 before any exchange.
- `services/oauth_state_service_test.go` (new) — against the real service on
  the in-memory store: a state is consumed on first validation and a replay
  fails; N concurrent validations of one state have exactly one winner; a
  relay record round-trips encrypted, is single-use, and expires.
- `handlers/oauth_state_binding_test.go` (extend) — the cross-host tier
  split is reported as *deferred*, never as bound; the relay-side check
  requires the cookie.
- `pkg/sdk/module/config_validate_test.go` — snapshot precedence and exact
  target-environment secret presence for legacy/active PATCH, named-profile
  PATCH and activation; newly submitted secrets are present to validation but
  plaintext is never exposed. Raw absent/empty values and effective
  EnvVar/default-resolved values remain distinguishable. Existing non-snapshot
  validators remain compatible.
- Config repository/handler integration tests — values, secrets and metadata
  persist through one Mongo `UpdateOne`; an injected write error leaves the
  document unchanged. Monotone `configRevision` compare-and-swap (including
  legacy missing = zero) rejects stale writers with 409
  `module.config_revision_stale`; environment and record-list writes advance
  both relevant revisions. A two-writer password/provider race has one winner
  and one 409, and the reloaded loser then fails the invariant.
- Auth module/handler tests — `HotReloadConfig` is true; successful active,
  named-environment and activation writes atomically persist
  `needsRestart=false`, while validation/write failure leaves the prior
  flag/state untouched and performs no follow-up clear.
- `pkg/sdk/module/admin_audit_test.go` (new) — one event per actual mutation
  result, enable/disable separate from config update, config CAS before an
  enabled-state side effect (and config retained if the later lifecycle step
  fails), documented metadata with names but no values,
  record-list slugs/unknown keys sanitized and bounded, stale 409 as failure,
  no actor email, nil sink/resolver tolerated, and sink failure WARNed without
  changing the HTTP result. An in-tree server wiring test requires the real
  sink and actor resolver to be installed.
- `shared/errcode/codes_test.go` — the four auth constants remain unique and
  stable, and the test additionally asserts that no `errcode` constant
  collides with the SDK-owned `module.config_revision_stale`, which is itself
  covered in `pkg/sdk/module/config_revision_test.go` alongside the CAS.
- `make ci-backend` (includes `openapi-check`).

**Frontend**

- `frontend-admin` Vitest: password form hidden; registration/recovery disabled
  alert; operator break-glass shows only a labelled login form (no signup or
  forgot links); provider loading/error/resolved-empty states remain distinct;
  callback URL is scrubbed before asynchronous work, direct success awaits an
  explicit session fetch (no timeout race), MFA stays out of router/storage,
  stale return targets are discarded, and raw callback error text is never
  rendered. `passwordUsableForLogin` drives unlink/reset controls and the
  retained-password notice. A config-revision 409 latches Reload & review,
  never auto-retries or re-sends a secret, and is distinguished from a
  record-list conflict by error code. MSW covers policy, provider, session and
  auth-method endpoints; unhandled requests fail the run.
- `frontend-client` adds Vitest + React Testing Library + MSW to the existing
  TypeScript/ESLint/build gate. Tests cover password-off rendering, provider
  loading/error/empty, allowlisted initiation, safe `next`, callback error
  allowlisting, immediate URL scrubbing, local-only MFA challenge, and success
  bootstrap. The bootstrap tests prove the marker exists before refresh,
  signed-out clears it, unavailable retains it for retry, and no access token
  or MFA token is written to persistent storage.
- `make ci-frontend-client` runs typecheck, ESLint, **tests**, then build;
  `Makefile`, workflow, `package.json` and lockfile change together.
- Manual OAuth round-trips on staging remain required for provider integration;
  they do not replace the automated callback/security tests.

## 7. Rollout and verification

**Delivery: four sequenced PRs against `dev`**, one spec, no ADR, no data
migration beyond the boot backfill. Each PR is independently valuable,
reviewable on its own and leaves `dev` releasable; the feature flag itself
lands in PR 3 and stays inert until an operator changes it.

| PR | Content | Depends on |
|---|---|---|
| **1 — SDK config integrity** | `ConfigValidationSnapshot` + `HasConfigSnapshotValidator` dispatch; `configRevision` CAS + single `UpdateOne` repository write; `needsRestart` in the same write; `RequirePersistedConfig` + per-module `missing` row; `SeedFromModules` backfill; `ModuleAdminHandler` audit setters + generic events; config-before-lifecycle ordering; `useModuleConfigController` 409-by-code handling. §6 tests for `pkg/sdk/module` and the admin controller. | — |
| **2 — OAuth callback hygiene** | Additive SDK `ActiveConfigRequiredModule` → `ActiveConfigView`; `ErrAuthPolicyUnavailable` + `auth.policy_unavailable`, `ErrOAuthEmailUnverified` + `auth.oauth_email_unverified`, strict boolean parser; `target.spaURL()` per-tier redirect; `oauthLoginCallbackURL` closed contract (allowlisted query, MFA in fragment, `Referrer-Policy: no-referrer` + `Cache-Control: no-store`, no token/email/user ID); all four callbacks on one raw shared flow (GitHub gains the refresh cookie; dead Huma Apple callback and Apple no-state fallback removed); atomic one-shot state (`Take`), state↔provider binding, client-tier completion through the one-shot relay on the client API host (`CLIENT_API_URL`, `GET /v1/auth/client/oauth/complete`) with the browser binding verified there; trust-before-destination for invalid state; callback re-checks provider usability; verified-email requirement before any email lookup + strict `OAuthAutoLinkByEmailEnabled` read before the lookup; GitHub email from `/user/emails` only; per-provider failure granularity on `/providers` and OAuth start; admin `SocialAuthCallback` rework (scrub, awaited session, `MfaVerifyPanel` from component memory, allowlisted codes, ten-minute return target). | — (standalone security fixes; ships before the toggle so PR 4's client SPA targets a safe contract) |
| **3 — Password-login toggle** | Schema pair + `HotReloadConfig`; strict `PasswordLoginEnabled` / `PasswordLoginDecision` + break-glass; gates (§4.3) incl. pending-challenge re-check with `MFAChallenge.Audience`; auth validator migration to the snapshot contract + login-method invariant; step-up `PasswordReauthAllowed`; usable-link unlink guard; `AuthMethodsView` split; `/policy` fields; admin SPA gating and copy; docs. | 1, 2 |
| **4 — Client OAuth login** | `frontend-client` providers / initiate / callback page, `bootstrapFromRefreshCookie`, safe `next`, policy gating; Vitest + RTL + MSW and the `client-test` target in `Makefile` / `frontend-client.yml`; `frontend-client/CLAUDE.md`. | 2 (contract), 3 (`passwordLoginEnabled` on `/policy`) |

PR 3 is the only one that changes what an operator can do; PRs 1–2 fix defects
that exist today and are worth merging even if PR 3 never shipped. The switch
appears in the admin UI only from PR 3, so the challenge-TTL rule below applies
to that deploy. Each PR carries its own `make ci` evidence and the documentation
for the paths it touches, in the same commits.

Before the first staging flip, verify that the auth module document exists,
both current profiles validate, and at least one test user has a verified IdP
email. Wait one maximum login-challenge TTL (five minutes) after deploying the
new backend before treating pre-deploy challenges as representative.

Staging verification, per surface (operator with Google; client through the
new client-SPA OAuth path):

1. Save a provider secret and set `passwordLoginEnabled<S>=false` in the same
   PATCH. The atomic write succeeds; password login returns the stable 403,
   the form is hidden, and OAuth succeeds. A new password-sourced MFA challenge
   started before the flip is refused and consumed at completion.
2. Try to disable the last provider, blank a required field, remove its target
   secret or turn auto-link off → 422 `auth.login_method_lockout`; config and
   enabled state remain unchanged; audit emits `outcome=failure` without
   values.
3. Attempt to activate a profile whose own snapshot is invalid while another
   profile is valid → 422. Repair its own secret/fields, then activate it.
4. Confirm admin password reset → 409, no-factor step-up →
   `mfa_enrollment_required`, and unlink of the sole usable link → 409. A
   linked but disabled provider must not satisfy the unlink guard.
5. Race two config PATCHes from the same `configRevision`: one disables
   password and one disables the last provider. Exactly one succeeds, one
   returns 409 `module.config_revision_stale`, and retrying the loser after
   reload returns the invariant's 422.
6. Verify missing-document, malformed-value and repository-failure behaviour
   in automated/integration tests, including that an admin config read cannot
   lazy-reseed `auth` and that `/admin/modules` still renders with a `missing`
   badge on `auth`. If repeated manually, use only a disposable cloned
   staging database and restore the exact document afterward (or restart to
   invoke boot seeding); never rename or remove the shared `module_configs`
   collection. Each case returns 503, not a password-login success.
7. Inspect operator and client callback redirects: correct SPA, allowlisted
   query fields, MFA in the fragment, `Referrer-Policy: no-referrer`, and no
   token/email/user ID in browser history, proxy logs or referrers. Confirm a
   missing/unverified IdP email cannot auto-link.
8. Break-glass drill: set
   `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS=true`, restart through the
   sanctioned stack lifecycle, and confirm the labelled emergency login form,
   an operator login, plus WARN/audit. Signup/forgot links stay absent. Client
   login, registration, recovery, password-confirm, unlink and durable-method
   views remain blocked. Verify that module mutations still demand the existing
   MFA, low-risk and RBAC gates. Unset the variable, restart, and verify the
   form flag and WARN are gone.
9. Flip both switches back and confirm normal behaviour. Existing sessions
   were intentionally not revoked at any point.

## 8. Follow-ups (named, not started)

- **Bulk revoke of password sessions** on flip — sessions record
  `LoginMethod` (`models/collections.go:151`), so a "revoke all sessions opened
  with a password on this surface" admin action is feasible; deliberately not
  automatic.
- **Fail-closed reads for the other auth toggles** (`loginEnabled`,
  `registrationEnabled`, `oauthAllowSignup`, native/mobile provider checks,
  etc.) — same defect class as §4.2; separate decision because it changes
  outage behaviour of existing switches.
- **Invite-bound OAuth onboarding** — consume the invite token inside the OAuth
  callback so SSO-only clients need not rely on auto-link by email.
- **Durable module-config audit evidence** — replace the current best-effort
  `AuditSink.Emit` seam if compliance requirements later demand guaranteed,
  transactionally coupled persistence; assess configurable retention and
  tamper-evident/WORM export rather than claiming the current two-year TTL is
  sufficient for every audit scope.
- **Remove the `hasUsablePassword` alias** after one release.
- **Apple `form_post` vs the `SameSite=Lax` state cookie** — browsers do not
  attach a Lax cookie to Apple's cross-site POST, so a same-host Apple flow
  fails `verifyOAuthStateBinding` today and only the cross-host client-tier
  hop passes. Needs its own decision (a `None` cookie scoped to the Apple
  flow, or a GET bounce); recorded by PR 2's plan, not fixed by it.
