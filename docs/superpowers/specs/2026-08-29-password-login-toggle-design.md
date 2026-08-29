# Per-surface password-login toggle — design

| Field | Value |
|---|---|
| **Date** | 2026-08-29 |
| **Status** | Draft — awaiting review |
| **Scope** | `backend/internal/core/auth`, `backend/internal/shared/{middleware,errcode}`, `frontend-admin`, `frontend-client`, `docs/site/modules/core/auth.mdx` |
| **ADR** | None. Both new fields default to `true`, so no inherited behaviour changes; the one policy decision (a disabled password is not a re-authentication proof either) is confined to the auth module and recorded in §4.5. |

## 1. Problem

The auth module lets an operator switch every OAuth provider on and off per
surface (`{google,apple,github,discord}Enabled{Admin,Client}`), but the
email+password method has no switch of its own. The only lever that stops a
password login is `loginEnabled{Admin,Client}`, and that is a maintenance kill
switch: it stops OAuth too (`AuthPolicyService.LoginAllowed` is consulted by
`PasswordAuthService.Login`, `InitiateOAuthLogin` and the mobile ID-token
handlers alike).

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
- G2. Turning it off is **complete**: no unauthenticated password entry point
  accepts the credential, and the password does not count as a credential in
  any decision that asks "does this user still have a usable way in".
- G3. An operator **cannot lock a surface out** by flipping the switch: the
  config write is refused unless another login method remains.
- G4. The two SPAs hide the password UI instead of surfacing a 403 on submit,
  exactly as they already do for `loginEnabled` / `registrationEnabled`.
- G5. Zero behaviour change for existing deployments: the fields default to
  `true` and have no `EnvVar`.

**Non-goals**

- Revoking sessions that were opened with a password before the flip (§5, #7).
- Hiding password *management* UI for authenticated users (`/user/security`
  PasswordTab, the Authentication Methods card). The credential legitimately
  still exists; see §4.3 for which routes stay open and why.
- A first-user bootstrap path through OAuth. The bootstrap remains
  password-based and the validator (§4.4) keeps it reachable.
- Mobile. `mobile/lib` is an 8-file skeleton with no login code.

## 3. Alternatives considered

### A — Two booleans, `passwordLoginEnabledAdmin` / `passwordLoginEnabledClient` (chosen)

Mirrors the existing `{provider}Enabled{Admin,Client}` and
`loginEnabled{Admin,Client}` pairs field-for-field: same group rail, same
`readBool(…, true)` default, same per-audience `AuthPolicyService` accessor,
same SPA consumption through `GET /v1/auth/{tier}/policy`. An operator who has
already configured OAuth providers finds the switch where they expect it, and
the `DependsOn` machinery is unnecessary because the field has no children.

### B — `loginMethods{Admin,Client}` as a `FieldStringList` of `password,oauth`

One field per surface listing the accepted methods. Rejected: it duplicates the
per-provider OAuth switches that already exist (what does `oauth` in the list
mean when `googleEnabledAdmin=false`?), and `FieldStringList` renders as a
free-text comma list, so a typo (`pasword`) would silently disable a method
with no 422. `mfaMethods` uses this shape today, but there the list is
"allowed factor types", a domain with no other switches to conflict with.

### C — A single `loginMode{Admin,Client}` enum (`all` / `oauth_only`)

Reads well in the UI but is the same bit as A with a worse extension story: the
next method (passkey-first login, magic link) forces a combinatorial enum.
Rejected.

## 4. Design

### 4.1 Schema — `module.go` `ConfigSchema()`

Two fields in the existing `login` group, immediately after
`loginEnabledClient` (`module.go:399-403`), so the rail reads
*Allow logins → Allow password logins → lockout → session cap*:

```go
{
    Key: "passwordLoginEnabledAdmin", Label: "Allow email/password logins on operator console", Group: "login",
    Description: "When off, the operator console accepts OAuth only: POST /v1/auth/operator/{login,register,forgot-password} return 403 auth.password_login_disabled, and a password no longer counts as a credential for step-up re-authentication or OAuth-unlink lockout checks. Cannot be turned off while no OAuth provider is enabled on this surface.",
    Type: module.FieldBool, Default: "true",
},
{
    Key: "passwordLoginEnabledClient", Label: "Allow email/password logins on client app", Group: "login",
    Description: "When off, the client app accepts OAuth only: POST /v1/auth/client/{login,register,forgot-password} return 403 auth.password_login_disabled, and a password no longer counts as a credential for step-up re-authentication or OAuth-unlink lockout checks. Cannot be turned off while no OAuth provider is enabled on this surface.",
    Type: module.FieldBool, Default: "true",
},
```

No `EnvVar` (G5) — identical to `loginEnabled{Admin,Client}`.

### 4.2 Policy accessor — `services/auth_policy_service.go`

```go
// PasswordLoginAllowed reports whether the email+password method is
// accepted on the audience's surface. Defaults to true when unset.
// Independent of LoginAllowed: the caller checks both.
func (s *AuthPolicyService) PasswordLoginAllowed(ctx context.Context, audience PolicyAudience) bool
```

Same shape as `LoginAllowed` (`auth_policy_service.go:157-170`): nil-safe,
`readBool(…, true)`, key chosen by audience suffix.

### 4.3 Gates — which routes refuse, which stay open

The rule: **a route is gated iff it uses the password as the credential that
authenticates an unauthenticated caller.** Routes that manage the credential
stay open, because the credential legitimately continues to exist — for the
other surface, for a later re-enable, and because deleting it is a different
decision that this toggle does not make.

| Route (`/v1/auth/{tier}/…`) | Verdict | Why |
|---|---|---|
| `POST login` | **403** | The password *is* the credential. |
| `POST register` | **403** | Creates a password credential the surface will not accept. The first-user bootstrap bypass at `password_auth_service.go:233-246` is **kept**, and the new check goes inside the same `!isFirstUser` block — the validator (§4.4) already makes "password off on a fresh install" unreachable through the admin API, so this bypass only matters for the DB-restore case in §5 #3. |
| `POST forgot-password` | **403** | Public, unauthenticated, and it mints a credential-setting token for a method the surface rejects. Today it always returns `nil` to avoid account enumeration (`:940-945`); the new refusal is **per surface, before the user lookup**, so it discloses nothing about any account. |
| `POST reset-password` | open | Consumes a token that only `forgot-password` (gated) or the admin `send-password-reset` (operator action) can mint. |
| `POST accept-invite` | open | Tier-2 onboarding: sets a password *and* verifies the email atomically. On a password-off surface the invitee then signs in via OAuth and `oauthAutoLinkByEmail` (default `true`) attaches the identity. Gating it would break invites on SSO-only clients for no security gain — the password set here is unusable for login by construction. |
| `POST verify-email`, `verify-email/resend` | open | Not a credential. |
| `POST change-password` | open | Authenticated credential management. |
| `POST me/password-confirm` | **409** | See §4.5 — different code, different reason. |
| admin `send-password-reset` | open | Explicit operator action. |

Gate placement in `PasswordAuthService.Login` (`:456-467`): immediately after
the `LoginAllowed` check and **before** `GetUserForAuth`, so:

- the account-lockout counters (`:583`) and the `RateLimiter` are never
  touched by a refused surface;
- the response is identical for every email → no enumeration oracle (§5 #8).

New sentinel + code:

```go
// services/password_auth_service.go
ErrPasswordLoginDisabled = stderrors.New("password login disabled for this surface")

// shared/errcode/codes.go
// AuthPasswordLoginDisabled signals that the email+password method is
// switched off on this surface while OAuth may still be accepted —
// distinct from AuthLoginDisabled, which stops every method.
const AuthPasswordLoginDisabled = "auth.password_login_disabled"
```

Mapped in `handlers/password_handler.go` next to `ErrLoginDisabled`
(`:432-434`) as `errcode.Forbidden(errcode.AuthPasswordLoginDisabled, "Email
and password sign-in is disabled for this surface. Use a linked identity
provider or contact an administrator.")`.

### 4.4 Anti-lockout validator — `internal/core/auth/config_validation.go` (new)

The auth module implements `module.HasConfigValidator` and
`module.HasConfigActivationValidator` (`pkg/sdk/module/config_validator.go`),
following `tenant/config_validation.go` exactly: both hooks delegate to one
pure function so a profile switch cannot smuggle in a state the PATCH would
refuse.

**Invariant, per surface:** *at least one login method remains enabled.*

```
passwordOn(s)  := readBool(values["passwordLoginEnabled"+s], true)
oauthOn(s)     := ∃ p ∈ {google,apple,github,discord}:
                     readBool(values[p+"Enabled"+s], true)
                     ∧ strings.TrimSpace(values[p+"ClientId"]) != ""
valid(s)       := passwordOn(s) ∨ oauthOn(s)
```

evaluated for `s ∈ {Admin, Client}`. The rule is symmetric by construction:
it refuses turning the password off with no live provider, **and** refuses
disabling the last provider (or blanking its `clientId`) while the password
is off. `{p}ClientId` is `FieldString`, not a secret (`module.go:210`), so it
is present in `mergedValues`; `{p}ClientSecret` is a secret and is not — see
§5 #2 for the residual.

Failure → `*module.ConfigValidationError` (422) naming the field the operator
just touched, with a stable code:

```go
// shared/errcode/codes.go
const AuthLoginMethodLockout = "auth.login_method_lockout"
```

Message: `"cannot disable the last login method on the operator console:
enable at least one OAuth provider with a client ID first"` (and the client
variant). The field named in the error is the *submitted* key when the PATCH
touched exactly one of the participating keys, else `passwordLoginEnabled<S>`.

`loginEnabled{Admin,Client}` is **deliberately outside** the invariant: it is
a maintenance lockout an operator chooses on purpose, and the pre-existing
behaviour must not change.

### 4.5 Step-up re-authentication

`RequireStepUp` offers a no-factor user the `password_confirm_required`
fallback, and `PasswordAuthService.ConfirmPassword` mints a token with
`amr += "reauth"` after checking the password (`password_auth_service.go:1161`,
`shared/middleware/auth.go:970-1000`). If a password is not an accepted
credential on the surface, it cannot be accepted as a proof of presence either
— otherwise an SSO-only policy is only SSO-only until the first destructive
action.

Two layers, both required:

1. **Service (security).** `ConfirmPasswordWithSecurity` returns the existing
   `ErrPasswordConfirmUnavailable` when `PasswordLoginAllowed(ctx, s.audience)`
   is false — same branch as "no password hash" (`:1178-1180`). The handler
   already maps it to 409 `auth.password_confirm_unavailable`, and
   `PasswordConfirmModal.tsx:59` already renders it. A crafted direct call
   therefore cannot downgrade.

2. **Middleware (UX).** `StepUpPolicy` (`shared/middleware/auth.go:68-78`)
   grows one method:

   ```go
   // PasswordLoginAllowed reports whether the email+password method is
   // accepted for the token's audience ("operator" | "client"). When it
   // is not, dispatchStepUpFailure must not offer the password-confirm
   // fallback: the only way to satisfy the gate is to enroll a factor.
   PasswordLoginAllowed(ctx context.Context, audience string) bool
   ```

   `dispatchStepUpFailure` treats `!PasswordLoginAllowed(claims.Audience)`
   like `roleRequiresMFA` and emits `mfa_enrollment_required`, so the SPA
   nudges to `/user/security` instead of opening a modal that 409s.
   `AuthPolicyService` implements it by mapping the audience string to
   `PolicyAudience`. In production the interface is satisfied only by
   `AuthPolicyService` (`SetStepUpPolicy` has one caller,
   `cmd/server/main.go:342`); the two in-tree test fakes
   (`shared/middleware/step_up_test.go:137`,
   `tenant/handlers/admin_mfa_routes_test.go:52`) gain the method. A fork
   that implements `StepUpPolicy` itself gets a compile error, which is the
   correct signal.

### 4.6 OAuth-unlink lockout guard

`wouldLockOutOAuthUnlink` (`services/auth_service.go:591-611`) decides
`locked = PasswordHash == "" && activeLinks <= 1`. With the password method off
on the user's surface, the hash is not a usable credential, so the guard must
treat it as absent. The helper stays pure; it gains a parameter:

```go
func wouldLockOutOAuthUnlink(target *iface.User, links []iface.OAuthLink,
    provider iface.OAuthProvider, passwordUsable bool) (providerID string, locked bool, found bool)
// locked = (!passwordUsable || target.PasswordHash == "") && activeCount <= 1
```

Both callers (`AdminUnlinkOAuth:567`, `SelfUnlinkOAuth:633`) compute
`passwordUsable := s.policy == nil || s.policy.PasswordLoginAllowed(ctx, s.audience)`.
The 409 `last_credential` message is unchanged.

### 4.7 Public policy endpoint

`GetAuthPolicyResponse` (`handlers/auth_handler.go:336-348`) gains

```go
PasswordLoginEnabled bool `json:"passwordLoginEnabled" doc:"Whether the email+password method is accepted on this surface — the login page hides the password form and the signup + forgot-password links when false"`
```

populated from `h.policy.PasswordLoginAllowed(ctx, audience)`. The OpenAPI
dump (`backend/openapi/*.json`) is regenerated; `make openapi-check` gates it.

### 4.8 SPAs

Both SPAs already read `/policy` with a fail-open default and gate on
`loginEnabled` / `registrationEnabled`. `passwordLoginEnabled` is added to
the policy type with default `true` in the same fallbacks.

**frontend-admin** (Tier-1 console — reference-first workflow applies)

| Component | Change |
|---|---|
| `store/api/authApi.ts:145` | add `passwordLoginEnabled: boolean`, default `true` in the fallback at `:226` |
| `components/authentication/EmailPasswordForm.tsx` | when `false`, render nothing (the form, the forgot-password link and the register CTA are all password-shaped) |
| `components/authentication/RegisterForm.tsx` | when `false`, render the same "disabled" alert path used for `!registrationEnabled` — direct navigation to `/register` must not show a working form |
| `components/authentication/Login.tsx` | when `passwordLoginEnabled === false` **and** `SocialLoginForm` has no providers, render a `no login method available — contact an administrator` alert so the card is never empty (§5 #2). `SocialLoginForm` exposes that it rendered nothing via a small callback/prop rather than a second providers query |
| i18n | new keys in the `auth` namespace: `pages.loginNoMethod`, `pages.passwordLoginDisabled` |

**frontend-client**

| File | Change |
|---|---|
| `api/auth.ts:65,80,88` | type + both fallbacks |
| `pages/LoginPage.tsx` | hide the credentials stage's form/links when `false`; same empty-card alert as the admin |
| `pages/SignupPage.tsx`, `components/Layout.tsx` | hide the signup form / nav CTA when `false` (they are password signups) |

No mobile work (§2).

### 4.9 Documentation (same commit as the code it describes)

- `backend/internal/core/auth/CLAUDE.md` — Login & Sessions row gains the
  pair and the validator; step-up section gains the `PasswordLoginAllowed`
  branch; route table marks the three gated routes; the config-validation
  file joins the layout table.
- `docs/site/modules/core/auth.mdx` — field count (63 → 65) and a short
  "SSO-only surface" paragraph under Login & Sessions; the operating guide
  `docs/site/operating/oauth-providers.mdx` gets a two-line pointer.
- `backend/internal/shared/middleware/auth.go` — the `StepUpPolicy` doc comment
  (there is no `middleware/CLAUDE.md`; the interface comment is the contract).

## 5. Edge cases

| # | Case | Decision |
|---|---|---|
| 1 | Operator turns password off with no OAuth provider enabled on that surface | Refused at write time, 422 `auth.login_method_lockout` (§4.4). Same rule refuses disabling the last provider while password is off. |
| 2 | Validator sees `clientId` but not `clientSecret`; provider passes validation yet `ConfiguredProviders` (`oauth_config_resolver.go:138`, requires `Get` to succeed) omits it → login page has no method | Accepted residual, mitigated in the SPAs by the "no login method" alert (§4.8). Recovery is the same as for `loginEnabled=false` today: `devtoken.sh` in dev, flip the `module_configs` document in prod. The save-time gate cannot see secrets by SDK contract (ADR-0017 D6); ADR-0019 accepted the same residual for sender profiles. |
| 3 | Fresh install (zero users) with password already off | Reachable only by restoring a `module_configs` document into an empty user DB — the validator refuses it through the API. Documented as a non-goal; the first-user `Register` bypass stays. |
| 4 | Which password routes refuse | Only `login`, `register`, `forgot-password` — the unauthenticated routes that *use* the credential (§4.3). Credential-management routes stay open. |
| 5 | Step-up `password-confirm` fallback | Refused at the service (409, existing code) **and** not offered by the middleware (`mfa_enrollment_required` instead) (§4.5). |
| 6 | OAuth self/admin unlink of the sole link while the user has a password hash but the method is off | Refused with the existing 409 `last_credential` (§4.6). |
| 7 | Sessions opened with a password before the flip | **Not revoked** — consistent with `loginEnabled=false`, which does not revoke either. The flag governs new authentications. An operator who wants a clean cut uses the existing per-user session revocation. |
| 8 | Enumeration / lockout counters | Gate sits before the user lookup; response identical for every email; failed-login counter and rate limiter untouched (§4.3). |
| 9 | MFA login challenge (`/mfa/login/verify`) | Unreachable on a password-off surface — a challenge is only minted by a successful password login. OAuth's MFA path (`evaluateMFAForOAuth`) unaffected. |
| 10 | Service accounts (ADR-0014 `client_credentials`), refresh-cookie rotation, `/dev/token` | Not password logins; untouched. `/dev/token` remains the dev recovery hatch. |
| 11 | `accept-invite` on a password-off client surface | Stays open; invitee signs in via OAuth and auto-link by email attaches the identity (§4.3). Invite email copy still says "set your password" — cosmetic, out of scope. |
| 12 | Both surfaces flipped independently | Each surface has its own key, own gate, own validator clause; a user with a password on both tiers is unaffected on the surface that keeps it on. |
| 13 | Existing deployment upgrades | Keys absent → `readBool` default `true` → identical behaviour (G5). No migration. |
| 14 | `/policy` unreachable from the SPA | Fail-open to `true` (existing behaviour) — the SPA shows the form and the backend 403s; never a lockout. |

## 6. Testing

**Backend** (all under `internal/core/auth` unless noted)

- `services/auth_policy_service_test.go` — `PasswordLoginAllowed`: unset →
  true; `"false"` per audience; audience isolation.
- `services/gates_test.go` — `Login`, `Register`, `ForgotPassword` return
  `ErrPasswordLoginDisabled` per audience; `Login` on the *other* audience
  still succeeds; first-user `Register` bypass still applies; `Login` refusal
  does not touch `FailedLoginCount` (assert via the user-service fake).
- `config_validation_test.go` (new) — table: password off + no provider →
  422; password off + provider enabled but blank clientId → 422; password off
  + one live provider → ok; disable last provider while password off → 422;
  blank the clientId of the last provider while password off → 422; both
  hooks (`ValidateConfig`, `ValidateConfigActivation`) agree; `loginEnabled`
  ignored.
- `services/password_confirm_test.go` — `ConfirmPassword` →
  `ErrPasswordConfirmUnavailable` when the method is off, even with a valid
  password and no factor.
- `services/auth_service_admin_unlink_test.go` + `auth_service_self_unlink_test.go` —
  sole link + password hash + method off → `last_credential`; method on →
  allowed.
- `shared/middleware/step_up_test.go` — `dispatchStepUpFailure` with no factor,
  role not requiring MFA, `PasswordLoginAllowed=false` →
  `mfa_enrollment_required`; `fakeStepUpPolicy` and the tenant test's
  `mfaMasterSwitchOnPolicy` implement the new method (default `true`).
- `handlers/error_mapping_test.go` — sentinel → 403 with code
  `auth.password_login_disabled`.
- `shared/errcode/codes_test.go` — the two new constants.
- `make ci-backend` — includes `openapi-check` for the `/policy` field.

**Frontend**

- `frontend-admin`: vitest on `EmailPasswordForm` (renders nothing when
  false), `RegisterForm` (disabled alert), `Login` (no-method alert when
  false + empty providers). MSW handlers for `/policy` and `/oauth/providers`
  must be registered — an unhandled request fails the run.
- `frontend-client`: `make ci-frontend-client` (typecheck + eslint + build; no
  test suite yet).

## 7. Rollout

Single PR against `dev`; no migration, no env change, no ADR. The feature is
inert until an operator flips a switch. Live verification on the staging stack
after merge: flip `passwordLoginEnabledAdmin` with Google enabled, confirm 403
on `/v1/auth/operator/login`, the hidden form on `console.*`, the 422 when
trying to disable Google, then flip back.
