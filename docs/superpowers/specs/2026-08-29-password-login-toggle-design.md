# Per-surface password-login toggle — design

| Field | Value |
|---|---|
| **Date** | 2026-08-29 |
| **Status** | Draft v2 — revised after review; awaiting approval |
| **Scope** | `backend/internal/core/auth`, `backend/internal/shared/{middleware,errcode,config}`, `backend/pkg/sdk/module` (three bounded fixes), `backend/cmd/server/main.go` (wiring), `frontend-admin`, `frontend-client` (adds OAuth login), `docker/.env.example`, `docs/site` |
| **ADR** | None. Both new fields default to `true`, so no inherited behaviour changes. The three SDK fixes (§4.5) correct defects in existing contracts rather than changing them. |

## 0. Revision log

v2 answers a review of v1 that found seven blockers and eight gaps; every
finding was verified against the code. The material changes: a Go method-name
collision fixed (§4.6); the Tier-2 client SPA gains OAuth login so the client
switch cannot lock it out (§4.10); `forgot-password`'s error handling made
selective (§4.3); pending MFA challenges re-check the policy (§4.3);
fail-closed policy reads with a break-glass recovery (§4.2); a *structural*
configuration predicate for the validator plus an auto-link constraint (§4.4);
the ConfigService write made atomic and validated against the map the runtime
reads (§4.5); usable-link counting and a split of `hasUsablePassword` (§4.7,
§4.8); the admin `send-password-reset` gated (§4.3); a generic config-change
audit event (§4.11); the SSO-only claim reworded as "blocks *new* password
authentications" with a bulk revoke named as a follow-up (§8).

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

Every claim below was verified against the code at `faf61a47`; file:line
references are to that commit.

## 2. Goals and non-goals

**Goals**

- G1. An operator can turn the password method off **per surface** (operator
  console / client app) at `/admin/modules/auth`, live, no restart — the same
  way they turn an OAuth provider off.
- G2. Turning it off is **complete** for new authentications: no
  unauthenticated password entry point accepts the credential, no in-flight
  password login can complete after the flip, and the password does not count
  as a credential in any decision that asks "does this user still have a usable
  way in" (step-up re-auth, OAuth-unlink lockout).
- G3. An operator **cannot lock a surface out by configuration**: the write is
  refused unless at least one OAuth provider on that surface is *structurally
  configured* (every field the flow needs is present) and the auto-link path
  that lets password-only users in is open. G3 promises structural
  completeness, not that the IdP works — wrong credentials or an IdP outage
  remain operational risks, covered by the break-glass in §4.2.
- G4. A config-read failure never *re-enables* the password: the check fails
  closed (503), never open.
- G5. Both SPAs hide the password UI instead of surfacing a 403 on submit, and
  the Tier-2 client SPA gains the OAuth login it needs to survive the flip.
- G6. Zero behaviour change for existing deployments: the fields default to
  `true` and have no `EnvVar`.
- G7. Every change to the switch is auditable: actor, module, surface, outcome.

**Non-goals**

- Revoking sessions opened with a password before the flip. The switch blocks
  *new* password authentications; existing sessions keep rotating their refresh
  token until they expire or are revoked. A bulk "revoke password sessions"
  action is a named follow-up (§8), not part of this change.
- Hiding password *management* UI for authenticated users. The credential
  legitimately still exists (§4.3); the UI gets policy-aware copy, not removal.
- An invite-bound OAuth onboarding flow (consuming an invite token inside the
  OAuth callback). The auto-link constraint in §4.4 closes the same hole with
  configuration; the flow is a follow-up (§8).
- A first-user bootstrap path through OAuth. Bootstrap stays password-based;
  the validator keeps it reachable (§5 #3).
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

### 4.2 Policy accessor — fail closed, with a break-glass

```go
// PasswordLoginAllowed reports whether the email+password method is
// accepted on the audience's surface. Absent → true. A failed config
// read is returned as an error: the caller must fail closed, because
// substituting the default during an outage would re-enable a method
// the operator deliberately disabled. The break-glass (below) wins
// over both the stored value and a read error.
func (s *AuthPolicyService) PasswordLoginAllowed(ctx context.Context, audience PolicyAudience) (bool, error)
```

Reads through `ModuleConfigService.GetRawValue` (`config_service.go:660`),
which distinguishes *absent* from *failed* — `GetValue` (`:646`) folds a read
error into the schema default, which is exactly the fail-open the reviewer
flagged. `LoginAllowed`, `RegistrationAllowed` and the provider switches keep
their existing fail-open reads; making them fail closed is a separate decision
(§8).

Callers map the error to a new sentinel `ErrAuthPolicyUnavailable` → **503**
`auth.policy_unavailable` ("Sign-in policy is temporarily unavailable; try
again shortly"). Both SPAs render it as a retryable error on the login form;
the client's refresh middleware already models a 503 as `unavailable` rather
than a session end (`frontend-client/src/api/client.ts:47-53`, ADR-0017), so
the same posture is consistent across the surface.

**Break-glass.** A boot-time env var `AUTH_PASSWORD_LOGIN_BREAK_GLASS=true`
(`shared/config/config.go`, `AuthConfig.PasswordLoginBreakGlass`) forces
`PasswordLoginAllowed` to `(true, nil)` on both surfaces. It exists for the
case G3 cannot cover: the validator passed but OAuth does not actually work
(wrong secret, IdP outage, redirect mismatch) and no operator can get in. While
set, the module logs at WARN on boot and on every password login that only
succeeded because of it, and emits `auth.policy.break_glass_used` to the audit
sink. It is documented as "set, log in, fix the config, unset" — never left on.
`docker/.env.example` ships it commented out. It replaces the v1 "flip the
Mongo document" recovery, which is not an acceptable production procedure.

### 4.3 Gates — which routes refuse, which stay open

The rule: **a route is gated iff it authenticates, or mints an unauthenticated
path to authenticate, with the password.** Routes that manage the credential
stay open, because the credential legitimately continues to exist — for the
other surface, for a later re-enable, and because deleting it is a different
decision this toggle does not make.

| Route | Verdict | Why |
|---|---|---|
| `POST /v1/auth/{tier}/login` | **403** `auth.password_login_disabled` | The password *is* the credential. Check sits right after `LoginAllowed` (`password_auth_service.go:466`) and **before** `GetUserForAuth`: the lockout counters (`:583`) and `RateLimiter` are never touched, and the response is identical for every email (no enumeration). |
| `POST /v1/auth/{tier}/register` | **403** | Creates a password credential the surface will not accept. The first-user bootstrap bypass (`:233-246`) is **kept**; the new check goes inside the same `!isFirstUser` block. `RegisterInitialAdmin` (setup wizard, `:345`) is untouched. |
| `POST /v1/auth/{tier}/forgot-password` | **403** | Public, and it mints a credential-setting token for a rejected method. **The handler currently discards every service error** (`password_handler.go:201`, `_ = h.svc.ForgotPassword(...)`) to avoid enumeration; it now propagates *only* `ErrPasswordLoginDisabled` and `ErrAuthPolicyUnavailable` — both per-surface, both evaluated before the user lookup, both disclosing nothing about any account. Every account-specific error stays swallowed. |
| `POST /v1/auth/mfa/login/verify`, `POST /v1/auth/webauthn/login/finish` | **403** when the login challenge originated from a password | A challenge lives five minutes and records `SourceAMR:["pwd"]` / `LoginMethod:"password"` (`mfa_challenge_service.go:53-63`); `LoginVerify` (`mfa_handler.go:467`) and `LoginFinish` (`webauthn_handler.go:366`) mint a full token pair from it. Both re-check `PasswordLoginAllowed` for the challenge's audience when `SourceAMR` contains `"pwd"`, and consume the challenge on refusal so it cannot be retried. Without this, a password login started 30 s before the flip completes after it. `MFAHandler` already holds `policy` (`:44`). Neither the challenge nor the two handlers currently record the audience (`NewMFAHandler` takes only the cookie domain, `:61-63`), so `MFAChallenge` gains an `Audience` field written by `completeLogin` / `evaluateMFAForOAuth` at creation, and the verify handlers read it — the check is then correct regardless of how the handlers are wired. |
| `POST /v1/admin/users/{userId}/send-password-reset` (and the client-user twin) | **409** `auth.password_login_disabled` | An operator action that would mint a reset for a method the target's surface rejects; the reset would also revoke the user's sessions (`password_auth_service.go:1032`) and leave them with an unusable password. Refusing is the honest answer. |
| `POST /v1/auth/{tier}/reset-password` | open | Consumes a token that only the two gated routes can mint; tokens issued before the flip expire within `passwordResetTokenTTL` (≤ 24h). A user who completes one has their sessions revoked and re-enters via OAuth — guaranteed reachable by the auto-link constraint (§4.4). |
| `POST /v1/auth/client/accept-invite` | open | Tier-2 onboarding (`user/routes.go:154`): sets a password *and* verifies the email atomically. The invitee then signs in via OAuth; `oauthAutoLinkByEmail` attaches the identity to the row the invite created — which is why §4.4 requires auto-link to be on. |
| `verify-email`, `verify-email/resend`, `change-password` | open | Not a credential (or authenticated credential management). |
| `POST /v1/auth/{tier}/me/password-confirm` | **409** `auth.password_confirm_unavailable` | §4.6 — different reason, existing code. |

New sentinels + codes:

```go
// services/password_auth_service.go
ErrPasswordLoginDisabled = stderrors.New("password login disabled for this surface")
ErrAuthPolicyUnavailable = stderrors.New("auth policy unavailable")

// shared/errcode/codes.go
const AuthPasswordLoginDisabled = "auth.password_login_disabled" // 403 / 409 per table
const AuthPolicyUnavailable     = "auth.policy_unavailable"      // 503
const AuthLoginMethodLockout    = "auth.login_method_lockout"    // 422, §4.4
```

### 4.4 Anti-lockout validator — extend `internal/core/auth/config_validation.go`

The file exists (duration bounds, ADR-0017) and implements
`HasConfigValidator` only. It gains the login-method rule and, like
`tenant/config_validation.go`, also implements `HasConfigActivationValidator`
so a profile switch cannot activate a state the PATCH would refuse — that hook
also starts covering the duration bounds, which today it does not.

**Invariant, per surface `S ∈ {Admin, Client}`:**

```
passwordOn(S)   := bool(values["passwordLoginEnabled"+S], default true)
providerOn(p,S) := bool(values[p+"Enabled"+S], default true)
structural(p)   := clientId(p) ≠ "" ∧ redirectURL(p) ≠ "" ∧ secrets(p)
    where secrets(google|github|discord) := storedSecret(p+"ClientSecret") ≠ ""
          secrets(apple)                 := teamId ≠ "" ∧ keyId ≠ "" ∧
                                            (storedSecret("applePrivateKey") ≠ "" ∨ privateKeyPath ≠ "")
oauthOn(S)      := ∃ p : providerOn(p,S) ∧ structural(p)
autoLink        := bool(values["oauthAutoLinkByEmail"], default true)

valid(S)        := passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)
```

- **Structural, not "has a client ID".** `OAuthConfigResolver.Get`
  (`oauth_config_resolver.go:30-100`) treats any provider with a client ID as
  configured, so a provider can be listed on the login page and fail at the
  token exchange. The predicate above names every field the web flow needs.
  The predicate is one exported function, `ProviderStructurallyConfigured`,
  reused by §4.7 so the validator and the unlink guard cannot disagree.
- **Secrets.** `mergedValues` never carries secrets (SDK contract,
  `config_validator.go:20-24`, ADR-0017 D6). The auth module reads the
  *stored* secret through its own `ConfigService.GetSecret` — the validator
  runs before persistence, so a PATCH that saves a secret and disables the
  password in the same request is refused until the secret is saved. The 422
  message says so. Changing the SDK to pass secret-presence flags would be a
  contract change and is not done here.
- **Auto-link.** With `oauthAutoLinkByEmail=false` the callback refuses to
  attach an identity to an existing account (`auth_service.go:1980`,
  `ErrOAuthLinkDisabled`). On a password-off surface every user without a
  prior link — invitees, admin-created users, anyone who only ever used a
  password — must authenticate to link and cannot authenticate: a closed loop.
  The constraint makes it a configuration error instead. The rationale is
  recorded in the field description: in an SSO-only posture the IdP's email
  claim is the trusted identity by definition.
- **Symmetric.** The same function refuses turning the password off with no
  usable provider, disabling the last usable provider or blanking one of its
  fields while the password is off, and turning auto-link off while any
  surface is password-off.
- **Field naming.** The validator sees only the merged map
  (`config_validator.go:20`) and cannot know which key the PATCH carried, so
  the error always names `passwordLoginEnabled<S>` and the message lists both
  ways out: *"cannot leave the operator console with no sign-in method: keep
  email/password on, or enable at least one OAuth provider with client ID,
  client secret and redirect URL saved, and keep auto-link by email on"*.
- `loginEnabled{Admin,Client}` is **outside** the invariant: it is a
  maintenance lockout an operator chooses on purpose.

### 4.5 ConfigService fixes — `pkg/sdk/module/config_service.go`

Three defects in `UpdateConfig` (`:407-458`) would make the switch report
success without being in force. They are fixed in this change because the
guarantee in G2 depends on them; each is small and covered by its own test.

1. **Non-atomic write.** The legacy top-level map is written, then the active
   environment; a failure on the second write logs a warning and returns
   `nil` (`:451-453`). The runtime reads the active environment
   (`GetValue:651`), so the operator sees "saved" while the backend keeps
   accepting passwords. Fix: the second failure returns an error. The partial
   state (legacy updated, environment stale) is self-healing — the next
   successful PATCH rewrites both — and the response now says the write
   failed.
2. **Validator judges the wrong map.** `validateModuleConfig` receives the
   merge of the *legacy* map (`:423`, the comment calls it "the config of
   record"), but the runtime reads the *active environment*. When the two
   diverge, the validator can accept a state the runtime will not have, or
   reject one it would. Fix: validate the merge of **both** maps; both must
   pass. `UpdateEnvironmentConfig` already validates the target environment's
   merge and is unchanged.
3. **No post-commit hook for audit.** Covered by §4.11 without a new SDK seam
   inside `UpdateConfig`.

### 4.6 Step-up re-authentication

`RequireStepUp` offers a no-factor user the `password_confirm_required`
fallback and `ConfirmPasswordWithSecurity` mints a token with `amr += "reauth"`
after checking the password (`password_auth_service.go:1161`,
`shared/middleware/auth.go:970-1000`). A password that is not an accepted
credential cannot be a proof of presence either.

1. **Service (security).** `ConfirmPasswordWithSecurity` returns the existing
   `ErrPasswordConfirmUnavailable` when the method is off for `s.audience` —
   same branch as "no password hash" (`:1178-1180`); the handler already maps
   it to 409 and `PasswordConfirmModal.tsx:59` already renders it. A read
   error → `ErrAuthPolicyUnavailable` → 503.
2. **Middleware (UX).** `StepUpPolicy` (`auth.go:68-78`) gains

   ```go
   // PasswordReauthAllowed reports whether a password may serve as the
   // re-authentication proof for the token's audience ("operator" |
   // "client"). When false — or when the policy cannot be read —
   // dispatchStepUpFailure must not offer the password-confirm
   // fallback: enrolling a factor is the only way to satisfy the gate.
   PasswordReauthAllowed(ctx context.Context, audience string) bool
   ```

   The name differs from the service method deliberately: Go has no method
   overloading, and the middleware cannot import `services.PolicyAudience`.
   `AuthPolicyService` implements it as an adapter over
   `PasswordLoginAllowed` that returns `false` on error (fail closed).
   `dispatchStepUpFailure` treats `!PasswordReauthAllowed` like
   `roleRequiresMFA` and emits `mfa_enrollment_required`. Production wiring is
   `cmd/server/main.go:342`; the two in-tree fakes
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
    linkUsable func(iface.OAuthProvider) bool) (providerID string, locked bool, found bool)
// usable := count(links where IsActive && linkUsable(Provider))
// locked  = (!passwordUsable || target.PasswordHash == "") && usable <= 1
```

Both callers (`AdminUnlinkOAuth:567`, `SelfUnlinkOAuth:633`) pass
`passwordUsable` from `PasswordLoginAllowed` (error → refuse with 503) and
`linkUsable := providerOn(p, audience) ∧ ProviderStructurallyConfigured(p)` —
the §4.4 predicate. `AuthService` gains a `SetProviderUsability(func)` seam
wired in `module.go`, keeping the service free of the resolver type. The 409
`last_credential` message is unchanged.

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
`PasswordTab` and `SecuritySummaryCard` show a policy-aware note when
`hasPasswordSet && !passwordUsableForLogin` ("Email/password sign-in is
disabled on this console; this password is kept for other surfaces or a later
re-enable").

### 4.9 Public policy endpoint

`GetAuthPolicyResponse` (`handlers/auth_handler.go:336-348`) gains
`passwordLoginEnabled bool`. On a read error the endpoint returns **503**
rather than a guessed value — the SPAs already fail open on `/policy` errors
(`authApi.ts:226`, `api/auth.ts:80`), which is correct for a *display* hint:
the form shows, the backend refuses. OpenAPI dump regenerated;
`make openapi-check` gates it.

### 4.10 SPAs

Both SPAs read `/policy` with a fail-open default. `passwordLoginEnabled` is
added to the type with default `true` in the fallbacks.

**frontend-admin** (Tier-1 console — reference-first workflow applies)

| Component | Change |
|---|---|
| `store/api/authApi.ts:145,179` | `passwordLoginEnabled` on the policy; `hasPasswordSet` + `passwordUsableForLogin` on `AuthMethodsView` |
| `components/authentication/EmailPasswordForm.tsx` | renders nothing when `false` — form, forgot-password link and register CTA are all password-shaped |
| `components/authentication/RegisterForm.tsx`, `ForgotPasswordForm.tsx` | when `false`, the same "disabled" alert path used for `!registrationEnabled`; direct navigation must not show a working form |
| `components/authentication/Login.tsx` | when `false` **and** `SocialLoginForm` reports no providers, render a *no sign-in method available — contact an administrator* alert (`SocialLoginForm` gets an `onProvidersResolved(count)` prop; no second providers query) |
| `pages/user/security/{LinkedProvidersTab,PasswordTab}.tsx`, `pages/user/settings/SecuritySummaryCard.tsx`, `pages/admin/user-profile/AdminAuthMethodsCard.tsx` | §4.8 field migration + policy-aware copy |
| `pages/admin/user-profile/AdminAuthMethodsCard.tsx` | send-password-reset button disabled with a tooltip when `!passwordUsableForLogin` (the backend 409s anyway) |
| i18n (`auth` namespace) | `pages.loginNoMethod`, `pages.passwordLoginDisabled`, `security.passwordKeptNotice` |

**frontend-client** (Tier-2 demo — TanStack Query, react-router v8, Tailwind;
no RTK). Today it has **no OAuth path at all** (`api/auth.ts` wraps password,
MFA and account routes only; `App.tsx` has no callback route), so a client
switch would strand it. This change adds the minimum web OAuth login:

| Piece | Change |
|---|---|
| `api/auth.ts` | `fetchOAuthProviders()` → `GET /v1/auth/client/providers`; `initiateOAuthLogin(provider)` → `POST /v1/auth/client/oauth/login` `{provider}` → `{authUrl, state}` → `window.location.href = authUrl` (mirrors `frontend-admin/src/utils/socialAuthUtils.ts`) |
| `pages/LoginPage.tsx` | provider buttons under the credentials stage (hidden while the query loads; nothing when empty); when `passwordLoginEnabled === false` the form and its links are hidden; when both are absent, the same *no sign-in method* alert |
| `pages/OAuthCallbackPage.tsx` (new) + `App.tsx` route `/auth/callback` | reads `success`, `error`, `requiresMfa`, `mfaToken`, `webauthnAvailable`; on success runs the existing bootstrap refresh (`auth/AuthProvider.tsx:36` — the callback sets the httpOnly refresh cookie, so `refreshAccessToken` yields the access token) and navigates to `/account`; on `requiresMfa` hands the challenge to the existing MFA stage of `LoginPage` (same `mfaToken` contract the password path uses); on `error` shows it |
| `pages/SignupPage.tsx`, `components/Layout.tsx` | hide the signup form / nav CTA when `false` (password signups) |
| `api/auth.ts` policy type + both fallbacks | `passwordLoginEnabled` |

**Backend prerequisite for the client callback.** Every provider's success
redirect targets `target.config.Server.FrontendURL` (legacy `FRONTEND_URL`,
e.g. `auth_handler.go:1011-1012`, `:1115`, `:1284`) and so does
`redirectOAuthSignupDisabled` and the MFA-partial redirect. The success
redirects are five literal `fmt.Sprintf` sites (`:1012` google, `:1115`
discord, `:1284` and `:1350` apple, `:1428` github) plus the MFA-partial
(`:717`) and signup-disabled (`:304`) helpers. A flow started by the client
SPA therefore lands on the operator console with a cookie scoped to the
client domain. The redirect must use the tier's configured frontend URL —
the per-tier `deps.frontendURL` the module already resolves for reset links
(`module.go:1051`, `:1217`; `OPERATOR_FRONTEND_URL` / `CLIENT_FRONTEND_URL`,
falling back to `FRONTEND_URL`). The `Origin`-derived `RedirectURI` stored in
the state at initiate time (`:454`) stays unused for the success redirect, as
today: a configured URL is not attacker-influenced. One helper,
`target.spaURL()`, replaces the seven literal uses.

### 4.11 Audit — one generic event for every module-config mutation

No module-config PATCH is audited today (`pkg/sdk/module/admin_routes.go`,
`handler.go` — no sink, no actor). Rather than an auth-only event, the SDK
admin handler emits one event for every mutation, which covers this switch and
every other toggle in the platform:

| Route | `Action` | `Outcome` |
|---|---|---|
| `PATCH /v1/admin/modules/{name}` | `module.config.updated` | `success` / `rejected` (422) / `error` |
| `PATCH …/environments/{env}` | `module.config.updated` | same, `Metadata.env` set |
| `PUT …/active-environment` | `module.config.environment_activated` | same |
| enable/disable via the PATCH body | `module.enabled` / `module.disabled` | same |

`ResourceType: "module"`, `ResourceID: <name>`, `Metadata: {env, keys:
[non-secret keys changed], secretKeys: [names only], code: <422 code if
any>}`. **Values are never recorded** — a 422 on `passwordLoginEnabledAdmin`
is visible as the key plus the code, which is what an auditor needs.

Wiring: `pkg/sdk` cannot import `internal/shared/middleware`, so
`ModuleAdminHandler` gains two setters, `SetAuditSink(iface.AuditSink)` and
`SetActorResolver(func(ctx) module.AdminActor)` (`AdminActor{UserID, Email,
IP, UserAgent string}`), both nil-tolerant. `cmd/server/main.go` wires them
after `RegisterAll`, where the compliance module has registered
`ServiceAuditSink` (`services.go:163`) and the middleware's claims accessor is
importable — the same post-init pattern the existing `SetAuditSink` seams use
(`main.go:310`).

### 4.12 Documentation (same commit as the code it describes)

- `backend/internal/core/auth/CLAUDE.md` — Login & Sessions row gains the pair,
  the validator invariant and the break-glass; step-up section gains
  `PasswordReauthAllowed`; route table marks the gated routes and the pending-
  challenge re-check; `config_validation.go` row updated; the OAuth callback
  redirect helper documented.
- `backend/pkg/sdk/CLAUDE.md` + `docs/site/sdk/config-service.mdx` — atomic
  write, dual-map validation, the audit event and the two handler setters.
- `docs/site/modules/core/auth.mdx` — field count (63 → 65), an "SSO-only
  surface" paragraph under Login & Sessions covering the invariant, the
  break-glass and the "blocks new authentications" semantics.
- `docs/site/operating/oauth-providers.mdx` — a short "Going SSO-only" section
  pointing at the invariant; `docs/site/architecture/authentication-flow.mdx`
  — the client-SPA OAuth path.
- `frontend-client/CLAUDE.md` — OAuth login in the current surface;
  `docker/.env.example` — `AUTH_PASSWORD_LOGIN_BREAK_GLASS` commented out with
  the procedure.
- `backend/internal/shared/middleware/auth.go` — the `StepUpPolicy` doc comment
  (the interface comment is the contract; there is no `middleware/CLAUDE.md`).

## 5. Edge cases

| # | Case | Decision |
|---|---|---|
| 1 | Password turned off with no usable provider on that surface | 422 `auth.login_method_lockout` (§4.4), in both directions, on PATCH and on environment activation. |
| 2 | Provider passes the structural predicate but does not work (wrong secret, IdP down, redirect mismatch at the IdP) | Out of G3's promise by design. Recovery is the break-glass env var (§4.2), logged and audited. The SPAs additionally show the *no sign-in method* alert when the providers list is empty. |
| 3 | Fresh install (zero users) with password already off | Reachable only by restoring a `module_configs` document into an empty user DB; the API refuses it. The first-user `Register` bypass stays, and the break-glass covers the restore case. |
| 4 | Which password routes refuse | §4.3 table. |
| 5 | Password login started before the flip, MFA verify after | Refused at `LoginVerify` / `LoginFinish`; the challenge is consumed (§4.3). |
| 6 | Step-up `password-confirm` fallback | Refused at the service (409) and not offered by the middleware (§4.6). |
| 7 | OAuth unlink of the sole *usable* link while the user has a password hash but the method is off | 409 `last_credential` (§4.7). Unlinking a disabled or unconfigured provider's link never counts as removing a credential. |
| 8 | Sessions opened with a password before the flip | Not revoked; refresh rotation continues until expiry or revocation. Stated in the field description; bulk revoke by `LoginMethod` is a follow-up (§8). |
| 9 | Enumeration / lockout counters | Gate sits before the user lookup; identical response for every email; failed-login counter and rate limiter untouched; `forgot-password` propagates only the two per-surface errors. |
| 10 | Config read fails (Mongo/Redis) during a password login, step-up or unlink | 503 `auth.policy_unavailable`; never a silent `true` (§4.2). Note that a Mongo outage also breaks the user lookup, so the practical exposure is a ConfigService-specific failure (decrypt, malformed document). |
| 11 | Invitee on a password-off client surface | `accept-invite` sets the password and verifies the email; OAuth login then auto-links — guaranteed by the auto-link constraint (§4.4). Invite copy still says "set your password"; cosmetic, out of scope. |
| 12 | Password-only user with no prior OAuth link on a surface that goes SSO-only | Enters via OAuth; auto-link attaches the identity (§4.4 constraint). |
| 13 | Both surfaces flipped independently | Own key, own gate, own validator clause; a user with a password on both tiers is unaffected on the surface that keeps it on. |
| 14 | Existing deployment upgrades | Keys absent → `true` (G6). No migration. `hasUsablePassword` stays on the wire for one release. |
| 15 | `/policy` unreachable from the SPA | Fail-open display (existing); the backend still refuses; never a lockout. |
| 16 | PATCH that saves a provider secret and disables the password together | Refused (the validator cannot see the new secret); message says to save the secret first (§4.4). |
| 17 | Environment write fails after the legacy write | The PATCH now returns an error; next successful PATCH heals both maps (§4.5). |
| 18 | `set-active-environment` to a profile with password off and no usable provider | Refused by `ValidateConfigActivation` (§4.4). |
| 19 | Break-glass left on | WARN on every boot and every rescued login, audit event per use; the docs procedure ends with "unset". Not enforced by code beyond logging — a deliberate operator override. |
| 20 | Service accounts (ADR-0014), refresh rotation, `/dev/token` | Not password logins; untouched. `/dev/token` is mounted only when `Server.Environment == "development"` (`main.go:624`) — not a production recovery. |

## 6. Testing

**Backend**

- `services/auth_policy_service_test.go` — `PasswordLoginAllowed`: absent →
  `(true,nil)`; `"false"` per audience; audience isolation; repo error →
  `(false, err)`; break-glass overrides both stored `false` and an error.
- `services/gates_test.go` — `Login`, `Register`, `ForgotPassword` return
  `ErrPasswordLoginDisabled` per audience; the other audience unaffected;
  first-user `Register` bypass intact; `Login` refusal leaves
  `FailedLoginCount` untouched; read error → `ErrAuthPolicyUnavailable`.
- `handlers/error_mapping_test.go` — the sentinels → 403 / 503 with codes;
  **`ForgotPassword` HTTP test**: policy-off → 403 body code; unknown email
  with policy on → 200 generic message (enumeration guard preserved).
- `handlers/mfa_login_verify_test.go` (new) — `LoginVerify` and `LoginFinish`
  with a challenge carrying `SourceAMR:["pwd"]` after the flip → 403 and the
  challenge is gone; OAuth-sourced challenge unaffected; audience taken from
  the challenge.
- `config_validation_test.go` (extend) — table over both surfaces: password off
  + no provider; provider enabled but missing clientId / redirectURL / stored
  secret / Apple team+key; password off + one structural provider → ok; disable
  the last usable provider while password off; blank a required field while
  password off; auto-link off while any surface is password-off; both hooks
  agree; `loginEnabled` ignored; duration bounds now also checked on
  activation.
- `services/password_confirm_test.go` — method off → `ErrPasswordConfirmUnavailable`
  with a valid password and no factor; read error → `ErrAuthPolicyUnavailable`.
- `services/auth_service_admin_unlink_test.go`, `auth_service_self_unlink_test.go` —
  sole usable link + hash + method off → `last_credential`; two links, one on
  a disabled provider → `last_credential`; method on → allowed.
- `services/auth_service_get_methods_test.go` — the three fields of §4.8.
- `shared/middleware/step_up_test.go` — no factor, role not requiring MFA,
  `PasswordReauthAllowed=false` → `mfa_enrollment_required`; fakes updated.
- `handlers/admin_user_auth_security_events_test.go` (extend) —
  `send-password-reset` → 409 when the target's surface is password-off.
- `handlers/oauth_callback_redirect_test.go` (new) — success / MFA-partial /
  signup-disabled redirects target the per-tier frontend URL for both tiers,
  falling back to `FRONTEND_URL` when the tier value is empty.
- `pkg/sdk/module/config_merge_test.go` + `config_validate_test.go` (extend) —
  environment write failure surfaces as an error; validator receives both
  merges and a failure on either rejects. `pkg/sdk/module/admin_audit_test.go`
  (new) — audit event per mutation with the documented shape, no values,
  nil sink and nil resolver tolerated.
- `shared/errcode/codes_test.go` — the three new constants.
- `make ci-backend` (includes `openapi-check`).

**Frontend**

- `frontend-admin` vitest: `EmailPasswordForm` (nothing when false),
  `RegisterForm`/`ForgotPasswordForm` (alert), `Login` (no-method alert when
  false + empty providers), `LinkedProvidersTab` (`passwordUsableForLogin`
  drives `onlyCredential`). MSW handlers for `/policy`, `/oauth/providers`,
  `/me/auth-methods` registered — an unhandled request fails the run.
- `frontend-client`: no test suite yet; `make ci-frontend-client` (typecheck +
  eslint + **build** — load-bearing). Manual: OAuth round-trip on staging.

## 7. Rollout and verification

Single PR against `dev`; no migration, no ADR. Inert until an operator flips a
switch. Staging verification after merge, per surface (operator with Google;
client with Google via the new SPA path):

1. Flip `passwordLoginEnabled<S>` with the provider structurally configured →
   `login` 403 with code; form hidden; OAuth login succeeds; a password login
   started just before the flip fails at MFA verify.
2. Try to disable the provider / blank its redirect URL / turn auto-link off →
   422 `auth.login_method_lockout`, audit event with `outcome=rejected`.
3. Switch environment profile to one with password off and no provider → 422.
4. `send-password-reset` for a user on the surface → 409; step-up on a
   no-factor user → `mfa_enrollment_required`; unlink the sole usable link → 409.
5. Simulate a ConfigService read failure (rename the `module_configs`
   collection on staging) → password login 503, not 200.
6. Break-glass: set the env var, restart, password login succeeds with the
   WARN and the audit event; unset.
7. Flip back → everything restored; no session was revoked at any point.

## 8. Follow-ups (named, not started)

- **Bulk revoke of password sessions** on flip — sessions record
  `LoginMethod` (`models/collections.go:151`), so a "revoke all sessions opened
  with a password on this surface" admin action is feasible; deliberately not
  automatic.
- **Fail-closed reads for the other auth toggles** (`loginEnabled`,
  `registrationEnabled`, provider switches) — same defect class as §4.2;
  separate decision because it changes outage behaviour of existing switches.
- **Invite-bound OAuth onboarding** — consume the invite token inside the OAuth
  callback so SSO-only clients need not rely on auto-link by email.
- **Remove the `hasUsablePassword` alias** after one release.
