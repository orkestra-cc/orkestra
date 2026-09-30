# Tier-1 OAuth initial-password enrollment — design

| Field | Value |
|---|---|
| **Date** | 2026-09-30 |
| **Status** | v1 — approved; implementation planned |
| **Scope** | `backend/internal/core/{auth,user}`, `backend/internal/shared/errcode`, `backend/pkg/sdk/iface`, `backend/openapi/enterprise.json`, `frontend-admin`, the auth module contract and canonical docs-site auth pages |
| **Source** | Code review on `dev` at `0f328be4` |
| **Audience** | Tier 1 only: operator users on the operator API surface and `frontend-admin` |
| **ADR** | None. The SDK change is an additive capability interface; no persistence schema migration or new collection is required. |

## 1. Problem

A Tier-1 account created by social OAuth has a verified account email and an
OAuth identity, but no `passwordHash`. The operator console's Password tab
detects that state, says that the user can add a password, makes the current
password field optional, and still calls the ordinary change-password
endpoint. The backend's `ChangePassword` method deliberately rejects an empty
stored hash because it can verify only an existing password. The advertised
self-service operation therefore always ends in `401 auth.invalid_credentials`
for the exact OAuth-only account it claims to support.

The public forgot/reset-password route can create the missing hash, but it is
not a satisfactory primary UX: it depends on notification delivery, forces a
mail round trip, and treats credential enrollment as account recovery. It
remains a valid fallback.

Simply allowing an empty `currentPassword` on the existing protected route is
not acceptable. That route is mounted under `RequireGlobal()` only. A stolen
ordinary bearer could use such a branch to establish a durable password and
turn a temporary session compromise into a persistent login method.

## 2. Goals and non-goals

### Goals

- G1. A signed-in OAuth-only Tier-1 human user can add email/password sign-in
  from `/user/security?tab=password`.
- G2. Adding the first password requires fresh proof of presence: recent
  interactive OAuth authentication for a no-factor user, or fresh MFA for a
  user who has a factor.
- G3. The password is attached to the existing account email. The operation
  cannot introduce or change an email address.
- G4. The password is validated by the live password policy, hashed through
  the existing Argon2id service, and stored exactly once under concurrency.
- G5. Existing password change, forgot/reset password, OAuth linking, and
  Tier-2 behavior retain their current contracts.
- G6. The UI represents password presence, policy usability, loading, and
  failure as distinct states and never promises a login method the backend
  will refuse.
- G7. Credential creation is auditable and does not expose plaintext,
  hashes, tokens, or full OAuth profile data.

### Non-goals

- NG1. Changing the user's account email or adding a second login email.
- NG2. Adding the same feature to the Tier-2 client SPA or client API surface.
- NG3. Passwordless WebAuthn or discoverable passkey login.
- NG4. Removing the forgot/reset-password fallback.
- NG5. Changing password-login policy defaults, OAuth auto-linking, or the
  first-admin bootstrap behavior.
- NG6. Replacing the current password-change endpoint or relaxing its
  current-password verification.

## 3. Approaches considered

### 3.1 Dedicated initial-password endpoint with enrollment proof — chosen

Create a Tier-1-only endpoint whose sole operation is setting an absent
password. Protect it with the existing enrollment-proof middleware used for
adding MFA credentials. This gives a recent OAuth login a smooth path while
requiring reauthentication once that proof is stale. It also produces a small,
explicit API contract and permits an atomic create-only repository operation.

### 3.2 Reuse forgot/reset-password as the only path — retained as fallback

The current email token proves mailbox control and safely creates a hash. This
approach needs little backend work but adds notification dependency and account-
recovery semantics to an authenticated settings operation. It also terminates
the current session, which is appropriate for recovery but unnecessarily
disruptive for deliberate credential enrollment.

### 3.3 Overload change-password when the stored hash is empty — rejected

This makes one endpoint mean both "verify and replace a credential" and
"create a credential without verification." It is easy to mount under the
wrong middleware, makes error behavior ambiguous, and creates a persistent-
access path for a stolen session. `ChangePassword` keeps its empty-hash guard.

## 4. Contract decisions

### D1. Tier and route

The feature is operator-surface only:

```http
POST /v1/auth/operator/me/password
Authorization: Bearer <operator access token>
Content-Type: application/json

{
  "newPassword": "the new password"
}
```

The request does not accept `email`, `currentPassword`, `confirmPassword`, a
user ID, or an audience. Identity comes only from the authenticated context;
confirmation is a client-side typo guard, not an independent server fact.

Success is `200` with the existing simple response shape:

```json
{
  "success": true,
  "message": "Password sign-in added."
}
```

The operation is create-only, not idempotent. Once a password exists, callers
must use `/v1/auth/operator/change-password`.

### D2. Authentication and proof-of-presence gate

The route is mounted under, in order:

1. the operator surface's normal bearer/audience middleware;
2. `RequireGlobal()`;
3. `operatorEnrolmentGate` with a five-minute maximum age.

The existing `RequireEnrolmentProof(5m)` behavior is reused unchanged:

- a caller with an enrolled MFA factor needs a fresh factor proof;
- a caller without a factor may use `auth_time` from an interactive login if
  it is no more than five minutes old;
- a stale no-factor session receives `401 reauthentication_required`, and the
  console returns the user through OAuth before retrying;
- an unresolved proof state fails closed.

The gate is resolved once per surface and reused, following the MFA enrollment
wiring. Adding a password is credential enrollment and receives the same
security boundary as adding a passkey or TOTP factor.

### D3. Password-method policy

The service strictly reads `passwordLoginEnabledAdmin` before writing. A false
value returns `403 auth.password_login_disabled`; an absent/unreadable auth
configuration returns `503 auth.policy_unavailable`. The operator break-glass
override does not open this endpoint: it rescues login only and cannot create
credentials.

The public reset redemption route remains redeemable under its existing
contract. This design changes only new authenticated enrollment.

### D4. Eligible account

The service loads the caller by `userUUID` and defensively requires:

- a live, non-deleted user;
- `IsActive == true`;
- a human user (`Kind != service`);
- `EmailVerified == true`; and
- no existing password hash.

The email-verification check is load-bearing. A manually linked OAuth identity
may use a different provider email from the account's login email. OAuth proof
therefore does not automatically prove control of an unverified local email.
OAuth-created users already satisfy this requirement because creation records
the provider-verified email as verified.

An unverified caller receives the existing `403 auth.email_not_verified`
contract and must verify the account email before adding password sign-in.

### D5. Live password policy and hashing

The service calls the existing `PasswordService.ValidatePolicy(ctx,
newPassword, user.Email)` and `PasswordService.Hash`. Length, character-class,
email-substring, and breached-password settings remain live and centralized.
No password policy logic is copied into the handler or repository.

The plaintext exists only in the request and service call stack. It is never
logged, audited, stored, returned, or included in an error.

### D6. Atomic create-only persistence

`UpdatePasswordHash` remains the replacement primitive used by password
change and reset. Initial enrollment gains a separate additive SDK seam:

```go
type InitialPasswordSetter interface {
    SetPasswordHashIfUnset(ctx context.Context, userUUID, hash string) error
}
```

The core user service implements it through a repository operation whose
Mongo filter includes the user UUID, non-deleted state, and either an absent or
empty `passwordHash`. The same update sets `passwordHash`,
`passwordUpdatedAt`, and `updatedAt`.

When no document matches after the service has loaded an eligible user, the
operation returns the stable `iface.ErrPasswordAlreadySet` declared beside the
capability interface. This is the concurrency fence: two simultaneous
enrollment attempts can both validate and hash, but exactly one can persist.
The loser receives `409 auth.password_already_set`. The auth package may alias
the iface sentinel to preserve its own service vocabulary, but neither module
compares error strings.

`iface.UserProvider` is not widened. Auth type-asserts the provider to the
additive `iface.InitialPasswordSetter` during initialization and passes the
capability into `PasswordAuthService`. A fork whose provider lacks the new
capability remains source-compatible; the enrollment service fails closed with
`503 auth.unavailable`. The in-tree user module always provides it.

### D7. Session and device-trust consequences

After the atomic write succeeds, the service unconditionally applies the
credential-change teardown with a new `password_added` reason:

- keep the caller's current session ID;
- revoke every other refresh-token/session path;
- add every other session ID to the access-token revocation set; and
- revoke all device-trust grants with reason `password_added`.

This is not controlled by `revokeSessionsOnPasswordChange`: creating a new
durable login method is a security boundary, not an ordinary replacement that
an administrator may stage. The current session survives because it supplied
the fresh proof.

As with existing password-change teardown, post-write revocation is best
effort. A failure cannot safely roll the hash back. The service logs bounded
identifiers and records the revocation result in audit metadata without
exposing credentials.

### D8. Audit and security-event vocabulary

Success emits `auth.password.added` with:

- actor and resource user UUID;
- actor type `user`;
- operator audience;
- source IP;
- outcome `success`;
- current session ID; and
- count/status of other-session teardown.

Expected refusals may be recorded as bounded failure outcomes but never include
the submitted password, hash, email address, OAuth subject, access token, or
refresh token. The compliance audit-action constants and the auth security-
event presentation map gain the new action so the Authentication Methods
timeline renders a meaningful label rather than a raw fallback.

### D9. Error contract

| Condition | HTTP | Code | Client behavior |
|---|---:|---|---|
| Missing/invalid bearer or audience | 401 | existing middleware contract | End/recover session as today |
| Stale no-factor interactive login | 401 | `reauthentication_required` | Sign in again and return to the password tab |
| Fresh MFA proof required | 401 | `step_up_required` | Use the existing step-up UI |
| Account email is unverified | 403 | `auth.email_not_verified` | Verify the account email |
| Password login disabled | 403 | `auth.password_login_disabled` | Explain policy; do not retry |
| Password already exists/race lost | 409 | `auth.password_already_set` | Refetch methods and switch to change mode |
| Password policy rejection | 400 | existing password-policy errors | Show the specific policy message |
| Policy unavailable | 503 | `auth.policy_unavailable` | Retry later |
| Initial-password capability unavailable | 503 | `auth.unavailable` | Retry/contact operator |

No submitted-credential verdict is returned as a codeless 401. The endpoint
does not verify a submitted current credential; its 401s belong to middleware
proof/session state.

### D10. Frontend modes

`PasswordTab` stops treating an absent auth-method response as
`hasPassword=true`. While the auth-method or policy query is loading, it shows
a non-authoritative loading state and disables credential submission. A query
failure shows an error and no form whose mode would be a guess.

Once both reads resolve, the pane has three states:

1. **No password, method enabled:** render "Add email and password sign-in,"
   the existing account email read-only from current-user state, new password,
   confirmation, and an "Add password" action. Do not render a current-
   password field. Submit through a new RTK Query mutation for D1.
2. **No password, method disabled:** render the policy explanation and no
   credential-creation form.
3. **Password exists:** preserve the current change-password form, including
   the current password field. If the method is disabled, preserve the current
   "stored password retained" notice and allow credential maintenance under
   the existing backend contract.

On successful enrollment, the mutation invalidates `SelfAuthMethods`, clears
form secrets, shows a localized success toast, and lets the refetch switch the
pane into change-password mode. A `409 auth.password_already_set` also
invalidates/refetches before displaying a non-destructive message, covering a
second tab or concurrent request.

The existing global handling of `step_up_required` and
`reauthentication_required` is reused. The return target remains
`/user/security?tab=password`; no credential or email is placed in the URL or
browser storage. User-visible strings are added to both English and Italian
locales.

### D11. Existing email and fallback semantics

The password login identifier is the account's existing normalized email. The
frontend displays it but does not submit it, and the backend never accepts an
email override on this route. Adding or changing a login email is a separate
verified-email-change feature outside this design.

Forgot/reset password and the administrator's send-password-reset action stay
available when password login is enabled. They remain the fallback when the
user prefers mailbox proof or cannot complete an authenticated enrollment.

### D12. API and documentation surfaces

The Huma operation updates the generated OpenAPI document. The implementation
also updates:

- `backend/internal/core/auth/AGENTS.md` with the route, gate, audit, and
  credential-change rules;
- `docs/site/architecture/authentication-flow.mdx` with the enrollment flow;
- `docs/site/modules/core/auth.mdx` with the endpoint and policy behavior; and
- frontend English/Italian copy.

No ADR is required because the operation uses existing module, middleware,
storage, policy, and UI seams without changing a platform-wide architectural
decision.

## 5. End-to-end flow

1. An OAuth-only operator opens `/user/security?tab=password`.
2. The console reads policy, current user, and self authentication methods.
3. It renders the initial-password form only when the password method is
   enabled and `hasPasswordSet` is false.
4. The user submits new password plus client-side confirmation.
5. Middleware validates the operator bearer, global identity, and enrollment
   proof. A stale no-factor OAuth session is sent through reauthentication and
   returns to the same URL.
6. The service verifies policy and account eligibility, validates and hashes
   the password, and invokes the create-only setter.
7. Mongo atomically changes the password from absent to present. A concurrent
   loser receives a conflict.
8. The service keeps the caller's session, tears down every other session and
   device-trust grant best-effort, and emits the audit/security event.
9. The console invalidates `SelfAuthMethods`; the refetched state changes the
   pane to ordinary password-change mode.
10. A later login accepts either the linked OAuth provider or the account
    email plus the new password, subject to the live surface policy.

## 6. Edge cases and failure behavior

1. **Password added in another tab:** atomic compare-and-set returns 409; the
   console refetches and switches to change mode.
2. **Policy disabled after the form renders:** the backend's live strict read
   returns 403 and stores nothing.
3. **Password policy edited while the form is open:** submission is judged by
   the current backend policy; frontend hints are advisory.
4. **OAuth session older than five minutes:** no write occurs; the existing
   reauthentication flow returns to the password tab.
5. **Caller enrolled MFA after page load:** the proof gate uses current factor
   state and requires fresh MFA.
6. **Unverified local email with a linked provider:** the password is not set;
   the user must verify the local account email.
7. **Service principal:** refused defensively even if a crafted token reaches
   the service.
8. **User deleted/deactivated between middleware and service:** no write; the
   request fails closed through the existing user eligibility mapping.
9. **Repository race:** only one update can match an empty hash.
10. **Revocation store outage after persistence:** the password remains set;
    bounded failure telemetry/audit is emitted and existing access exposure is
    limited by access-token lifetime, matching current credential-change
    failure semantics.
11. **Missing additive setter in a fork:** route returns 503 and never falls
    back to the replacement-style `UpdatePasswordHash` method.
12. **Break-glass active while password policy is off:** enrollment remains
    forbidden; break-glass cannot create a credential.

## 7. Test strategy

### Backend service and repository

- OAuth-only, verified, active human successfully sets a password.
- The resulting hash verifies and `passwordUpdatedAt` is set.
- Existing password returns the stable already-set sentinel without rewrite.
- Two concurrent setters produce exactly one success and one conflict.
- Disabled/unavailable policy writes nothing.
- Unverified email, inactive user, deleted user, and service principal write
  nothing.
- Every live password-policy sentinel propagates through the existing mapping.
- Other sessions and refresh tokens are revoked, access-token session IDs are
  denylisted, device trust is removed, and the caller's session survives.
- A teardown failure does not roll back the password and is observable.
- Audit/security events contain the intended bounded metadata and no secret.

### Handler, middleware, and route wiring

- The route exists on the operator surface and not the client surface.
- The route requires a bearer, `RequireGlobal`, and enrollment proof.
- Fresh OAuth `auth_time` without MFA succeeds; stale proof returns
  `reauthentication_required`; an enrolled caller receives `step_up_required`
  until they prove the factor. A no-factor caller whose role requires MFA has
  the same enrollment-proof behavior as any other no-factor caller: recent
  interactive authentication succeeds, otherwise reauthentication is
  required.
- `auth.password_already_set`, `auth.password_login_disabled`,
  `auth.email_not_verified`, password-policy, and outage mappings have the
  specified statuses/codes.
- The OpenAPI operation contains no email/current-password fields.

### Frontend

- Loading/error state renders no guessed form.
- No-password + enabled renders new/confirm fields and no current field.
- No-password + disabled renders policy guidance and no form.
- Existing password renders the current change form.
- Enrollment calls the new mutation and never sends the account email.
- Success and already-set conflict invalidate `SelfAuthMethods` and clear
  secret fields.
- Step-up and reauthentication preserve `?tab=password`.
- English/Italian locale parity passes.

### Verification commands

The implementation plan will use focused package/component tests first, then:

```bash
make ci-backend
make ci-frontend-admin
```

It will regenerate and verify `backend/openapi/enterprise.json` through the
repository's existing OpenAPI target.

## 8. Acceptance criteria

1. A newly OAuth-created Tier-1 user can add a policy-compliant password from
   the Security page and subsequently log in with the same account email and
   that password.
2. The same user can still log in through every active linked OAuth provider.
3. A stale or insufficiently proven session cannot create the password.
4. An existing password cannot be overwritten through the enrollment route,
   including under concurrent requests.
5. A disabled password method cannot acquire new credentials through this
   route or break-glass.
6. The current session survives success; other sessions and device-trust
   grants are revoked best-effort and observably.
7. Tier-2 routes and UI are unchanged.
8. Focused tests, backend CI, frontend-admin CI, and OpenAPI drift checks pass.
