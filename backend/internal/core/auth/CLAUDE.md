# Module: Auth — Email/password + OAuth 2.1, JWT, sessions

_Path: `/backend/internal/core/auth`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Core](../CLAUDE.md) | [☰ Backend](../../../CLAUDE.md) | [Root](../../../../CLAUDE.md)

## Purpose

Owns every flow that turns an external credential (email+password, OAuth code, Apple/Google ID token, refresh cookie) into a signed access token plus a tracked session. Manages refresh-token rotation, device-bound sessions, email verification tokens, password reset tokens, and the OAuth state machine.

Does not own user profile data (delegates to `iface.UserProvider`), org membership (delegates to `iface.TenantProvider`), permission evaluation (delegates to `iface.AuthzProvider`), or email delivery (delegates to `iface.NotificationSender`).

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module wiring — repos, providers, JWT, OAuth factory, password service, handlers |
| `maintenance.go` | `Start`/`Stop` plus the elected, adaptive-cadence refresh-token retention sweep (ADR-0017 D7) |
| `handlers/auth_handler.go` | OAuth initiate/callback endpoints, mobile ID-token routes, logout, refresh |
| `handlers/oauth_callback_flow.go` | The ONE web-callback implementation (`completeOAuthCallback`): trust before destination, strict provider resolution from one config read, per-provider `oauthExchange` closures, inline completion for operator/legacy flows, one-shot relay for client-tier flows; `HandleOAuthRelayCompleteHTTP` (client host); the four `Handle*CallbackHTTP` wrappers are thin |
| `handlers/oauth_callback_redirect.go` | The closed SPA callback contract — the ONLY file that may build `/auth/callback` or `/user/security` URLs; per-tier `spaURL()`, allowlisted codes, MFA in the fragment, relay destination, `Referrer-Policy: no-referrer`; policed by `TestCallbackURLBuilders_StructuralScan` |
| `services/oauth_provider_usability.go` | `ProviderStructurallyConfigured` (pure), `OAuthWebProviderUsable` / `UsableWebProviders` (strict, one read), `OAuthResolver` interface |
| `handlers/password_handler.go` | Register, login, verify email, forgot/reset/change password |
| `handlers/admin_user_auth_handler.go` | Operator-side admin endpoints under `/v1/admin/users/{id}/...` — auth-methods aggregator, send-password-reset, resend-verification, oauth unlink. Inline error mapping translates the typed service errors to 404 / 409 with body codes |
| `handlers/self_user_auth_handler.go` | Self-service endpoints under `/v1/auth/{tier}/me/...` — auth-methods aggregator, session list/revoke, OAuth self-unlink. Drives the operator-tier `/user/security` page; mirrors the admin handler's structure with self-action allowed |
| `services/auth_service.go` | OAuth orchestration, provider linking, token pair issuance, auth-methods aggregator (`GetUserAuthMethods`), admin OAuth unlink (`AdminUnlinkOAuth`) + self-service unlink (`SelfUnlinkOAuth`) sharing a `wouldLockOutOAuthUnlink` lockout helper that counts **usable** credentials (`usableProvidersForLinks` + the `SetProviderUsability` seam), session list / revoke methods with three-step revocation (refresh tokens → session doc → Redis sid) |
| `services/password_auth_service.go` | Password register/login/verify/reset/change, rate-limited |
| `services/password_service.go` | Argon2id hashing + policy validation |
| `services/jwt_service.go` | RS256 JWT signing, validation, membership embedding |
| `services/oauth_provider_factory.go` | Factory for Google / Apple / Discord / GitHub providers |
| `services/oauth_config_resolver.go` | Reads live provider configs from `ModuleConfigService` on every OAuth request |
| `services/oauth_state_service.go` | Redis-backed OAuth state/nonce with 10-minute TTL |
| `services/risk_assessment_service.go` | Device-fingerprint + IP risk scoring |
| `repository/auth_repository.go` | Legacy shared repository, mainly for account/link lookups |
| `repository/auth_session_repository.go` | Device session documents |
| `repository/refresh_token_repository.go` | Hashed refresh tokens + rotation lineage |
| `repository/oauth_provider_repository.go` | `operator_oauth_providers` / `client_oauth_providers` — provider-side lookup (provider + providerID → user); per-tier constructors only after D-8 |
| `repository/email_token_repository.go` | Single-use verification + reset tokens |
| `models/*.go` | `OAuthProvider`, `RefreshToken`, `AuthSession`, `EmailToken`, `SecurityEvent`, collection-name constants |
| `utils/pkce.go`, `utils/redirect_validation.go` | PKCE helpers + redirect-URL allowlist check |

## MongoDB collections

Declared in `module.go::Collections()`. Collection name constants live in `models/collections.go` (and `models/email_token.go` for email tokens). After ADR-0003 PR-D D-8 every PII-bearing auth collection is split per audience tier — operator-tier rows live in `operator_*`, client-tier rows in `client_*`. The legacy single-tier `auth_*` collections were removed by D-8.

| Collection | Indexes | TTL |
|---|---|---|
| `operator_oauth_providers` / `client_oauth_providers` | compound `(userUuid, provider)` unique | — |
| `operator_refresh_tokens` / `client_refresh_tokens` | `uuid` unique, `userUuid`, `familyId` | — (application sweep, not a TTL index: bounded per-cycle progress and backlog telemetry are required for the first cleanup of an upgraded install, and a TTL index provides neither) |
| `operator_refresh_token_families` / `client_refresh_token_families` | `familyId` unique, `expiresAt` | Yes — absolute expiry is the latest token expiry in the family (with a 24h minimum fallback), so the non-PII replay fence survives every refresh token it protects |
| `operator_sessions` / `client_sessions` | `uuid` unique, `expiresAt` (TTL via ExpireAt) | Yes — `expiresAt` is the 90-day retention deadline (`models.AuthSessionRetention`) |
| `auth_security_events` | (none declared) | — — single non-tier-split (audit log keyed on userUUID alone) |
| `operator_email_tokens` / `client_email_tokens` | `uuid` unique, `tokenHash` unique, `userUuid`, `expiresAt` **TTL 24h** | Yes |
| `operator_mfa_factors` / `client_mfa_factors` | `uuid` unique, compound `(userUuid, type)` unique | — — one row per (user, factor type). The `webauthn` row carries an embedded `webauthnCredentials[]` array (zero-or-many passkeys per user) |
| `auth_device_trust` | `uuid` unique, `(userUuid, deviceId)`, `trustedUntil` (TTL via ExpireAt) | Yes — single non-tier-split (grant follows the user record) |
| `service_account_credentials` | `uuid` unique, `clientId` unique, `userUuid` | — single non-tier-split (service accounts are an operator-surface-only concept, not a per-audience one — see "Service accounts" below) |

Email tokens, device-trust grants, refresh-family replay fences, and sessions have TTL indexes. Refresh-token rows and MFA factor rows are rotated/invalidated explicitly in the service layer.

> **Zero-`expiresAt` rows are excluded structurally, not by runbook.** A session document with a zero `expiresAt` serialises as a year-1 BSON date, so a bare TTL index would delete it **immediately** — an irreversible delete of a session that has not expired at all. The write path has always set the field, but "should not happen" is not sufficient warrant for that. Both session TTL indexes therefore carry a partial filter (`module.go`'s `sessionRetentionPartialFilter`):
>
> ```
> {expiresAt: {$gt: ISODate("2000-01-01")}}
> ```
>
> A row below that floor is simply not in the index, so Mongo's TTL monitor never considers it. Any deadline this code writes is `now + AuthSessionRetention`, decades above the floor, so no legitimate row is excluded. Pinned by `TestSessionTTLIndexExcludesZeroExpiry` — including that the spec never asks for `Sparse` and `PartialFilter` together, which Mongo rejects outright.
>
> Counting such rows before a deploy is still a **recommended sanity check** — a non-zero count means something upstream is writing sessions without a deadline, and those rows will now accumulate forever rather than being reaped:
>
> ```
> db.operator_sessions.countDocuments({expiresAt: {$lt: ISODate("2000-01-01")}})
> db.client_sessions.countDocuments({expiresAt: {$lt: ISODate("2000-01-01")}})
> ```
>
> It is **no longer a deploy blocker**: a non-zero count is a bug to chase, not a reason to hold the release, because the index can no longer delete those rows.
>
> **Upgrading an environment that already built the un-filtered index:** adding `partialFilterExpression` to an existing index is not an in-place change — Mongo keys the index by name, so `createIndex` with different options on the same keys fails rather than rebuilding. Observed verbatim (both collections):
>
> ```
> Failed to ensure collection ... module=auth collection=operator_sessions
> error="create indexes for \"operator_sessions\": (IndexKeySpecsConflict) An
> existing index has the same name as the requested index. ...
> Requested index: { ... partialFilterExpression: { expiresAt: { $gt: new Date(946684800000) } } },
> existing index: { v: 2, key: { expiresAt: 1 }, name: \"expiresAt_1\", expireAfterSeconds: 0 }"
> ```
>
> `ensureCollections` logs at WARN and continues (index creation is deliberately non-fatal), so boot succeeds with the **old, unfiltered index still live** — i.e. still able to delete a zero-`expiresAt` row. Drop it once and let the next boot rebuild it:
>
> ```
> db.operator_sessions.dropIndex("expiresAt_1")
> db.client_sessions.dropIndex("expiresAt_1")
> ```
>
> Until that is done the pre-flight count above is still the only guard, so run it first. Grep boot logs for `IndexKeySpecsConflict` to tell the two states apart.

## Dependencies

- **Modules** (`module.go:31`): `user`, `notification`, `tenant`, `authz`. All four are listed so the topological sort boots them first.
- **Required services** (`module.go:32-34`): `ServiceUserService`, `ServiceTenantProvider`. Panics if missing — both are core.
- **Optional services** (`module.go:35-37`): `ServiceNotificationSender`. Graceful degradation: signup and password-reset mail endpoints still mount, but when `RequireEmailVerification=true` signup returns 503 unless the notifier is configured.
- **Provides** (`module.go:38-45`): `ServiceAuthService`, `ServiceJWTService`, `ServicePasswordService`, `ServicePasswordAuthService`.
- **Permissions contributed**: `auth.self` (edit your own password/sessions), `auth.mfa.self` (manage your own MFA factors), `system.users.mfa_reset` (admin reset of another user's MFA), `system.users.password_reset` (admin: send a password-reset email to another user), `system.users.email_verify_resend` (admin: resend the email-verification message), `system.users.oauth_unlink` (admin: unlink an OAuth identity from another user). The four `system.users.*` keys back the operator-side admin user-auth surface (`/v1/admin/users/{userId}/...`); each gates exactly one route so the audit trail and any future RBAC tweaks stay per-action.

## Lifecycle

`Init` is where every moving part gets wired:

1. **Repositories**: auth, OAuth provider, refresh token, auth session, email token.
2. **OAuth provider factory**: constructed with an **empty** config map. Provider configs are resolved per-request by `OAuthConfigResolver` from the live `module_configs` document, then passed to `factory.CreateProvider(p, cfg)` through the override parameter. No provider state is pinned at boot — rotating a secret at `/admin/modules` takes effect on the next OAuth request.
3. **OAuth config resolver**: `NewOAuthConfigResolver(deps.ConfigService)`. It reads the current `module_configs` document on each OAuth request, so edits made in `/admin/modules` take effect without a service restart.
4. **JWT service**: loaded with the `AUTH_JWT_PRIVATE_KEY` / `AUTH_JWT_PUBLIC_KEY` pair, then has `SetTenantProvider(...)` called on it so every future `GenerateAccessToken` embeds the caller's current org memberships. The **operator-audience** JWT service additionally gets `SetDefaultTenantProvider(...)` called on it, wiring `iface.DefaultTenantProvider` (the tenant module's platform-default Tier-1 pointer, PR 3) from `module.ServiceDefaultTenantProvider` — optional (`module.GetTyped`, not `MustGetTyped`), since a missing key must not block auth boot. The **service-audience** and **client-audience** JWT services deliberately do NOT get this call — see the "Tenant fallback selection" invariant below.
5. **OAuth state service**: Redis-backed state/nonce store, 10-minute TTL.
6. **Auth service**: the orchestrator for OAuth flows.
7. **Password service**: argon2id hasher with HIBP policy validation (`services/password_service.go`).
8. **Password auth service**: register/login/verify/reset/change flows, wired to the optional notification sender and a shared `RateLimiter`.
9. **Handlers**: OAuth, password, MFA, and WebAuthn handlers, each constructed twice (operator + client) and stamped with the matching tier's cookie domain at construction time (`cfg.Auth.Cookie.OperatorDomain` / `ClientDomain`; an empty value mints the cookie without a `Domain` attribute, scoped to the minting host). The shared `Cookie.Name` + `Cookie.Secure` are still process-scoped.
10. **Register services** under `ServiceAuthService`, `ServiceJWTService`, `ServicePasswordService`, `ServicePasswordAuthService`, plus the per-tier keys (`ServiceOperator{AuthService,PasswordAuthService,JWTService}` / `ServiceClient{...}`) that audience-aware consumers (dev token generator, future tier-specific addons) request directly.

`Start` / `Stop` are implemented in `maintenance.go` — they own the refresh-token retention sweep (see "Refresh-token retention is an elected, self-draining sweep" under Key invariants). `Start` **never returns an error**: `auth` is a core module, so `ModuleRegistry.StartAll` would hand that error to `main.go`'s `log.Fatalf` and a degraded Redis would refuse to boot the platform. Every recoverable condition — no lease, no tiers, Redis unreachable — returns nil and skips maintenance; leadership is acquired inside the goroutine, after `Start` has returned. `HealthCheck` still inherits from `BaseModule`.

No seeding — there are no default accounts or default tokens. The first user is created by whichever external flow gets there first (setup wizard, OAuth signup, password register).

## Runtime configuration

OAuth provider settings are admin-managed through `ConfigSchema()` — stored in `module_configs`, secrets encrypted at rest with AES-256-GCM, editable at `/admin/modules`, and resolved live per OAuth request. Env vars are the **seed source** only: on first boot the registry populates the document from the `EnvVar` field on each schema entry, and after that the document is authoritative. Non-OAuth settings (JWT keys, cookies, feature toggles) still live in `cfg *config.Config` because they're process-scoped and must not rotate at runtime.

### Configuration groups (`ConfigGroups()`)

`auth` declares an 11-key group tree via `ConfigGroups()` — 7 top-level groups
(`registration`, `login`, `password`, `mfa`, `oauth`, `antiabuse`, `sessions`) plus
`oauth.google` / `oauth.apple` / `oauth.github` / `oauth.discord` nested under `oauth`
(`Parent: "oauth"`). It is the largest configuration surface in the base — 65
`ConfigField` entries — and is the first (and so far only) module to actually render
the settings page's sectioned rail rather than the plain-form degradation path. This is
the shape a contributor adding a field to `ConfigSchema()` must keep valid:

- Every `ConfigField.Group` must name one of the 11 declared keys. `Group` used to be a
  human-readable display label; it is now a `ConfigGroup.Key` — the panel heading and
  description are pulled from the matching `ConfigGroup.Label`/`Description` (and, once
  resolved through i18n, from `moduleConfig.auth.groups.<key>.label`/`.desc` in the
  locale bundles).
- A provider credential field (one of the 19 fields under `oauth.google` / `oauth.apple` /
  `oauth.github` / `oauth.discord`) needs `DependsOnMatch: "any"` plus a `DependsOn` pair
  naming that **same** provider's own two toggles — `{provider}EnabledAdmin` and
  `{provider}EnabledClient` — each with `In: ["true"]`. It must be `"any"`, not the
  default `"all"`: the field has to appear as soon as *either* audience surface enables
  the provider, and an AND gate would stay hidden until both toggles are on — wrong the
  moment an operator enables a provider on only one surface.
- The 8 provider-enable toggles themselves (`{provider}Enabled{Admin,Client}`) are
  **never** gated by a `DependsOn` — they live directly in the `oauth` parent group with
  no conditions. Gating a toggle on anything, including itself, would make that provider
  permanently unrecoverable through the admin UI: there would be no visible control left
  to switch it back on.
- `cmd/server/config_declarations_test.go` runs `module.ValidateConfigDeclarations` over
  every module compiled into the binary, `auth` included. A `Group` that doesn't resolve
  to a declared key, a `DependsOn.Key` that doesn't resolve to a field of the same
  module, an unknown `DependsOnMatch` value, or a `DependsOnMatch` set with no
  `DependsOn` to combine — all of these **fail the build**, rather than silently
  rendering a phantom rail entry.
- `auth`'s own `config_groups_test.go` goes further than "does it validate": it pins the
  exact field count per group (`TestConfigGroups_FieldCountPerGroup`), the exact
  `DependsOn` condition set and `DependsOnMatch` on every one of the 19 provider
  credentials (`TestProviderCredentials_HiddenUntilEitherSurfaceEnabled`), and that all 8
  toggles stay ungated (`TestProviderToggles_NeverGated`). Moving a field to a different
  group, or changing its gating, will fail one of these tests by design — update the
  count map / gating assertions **deliberately** alongside the change, not by
  reflexively editing numbers until the suite goes green.

### Admin-managed (ConfigSchema, per-provider)

Schema keys below are what handlers and the resolver look up. The `EnvVar` column is the one-time seed source — once the document exists, editing the env var has no effect without a wipe.

#### Auth Policy tabs (added 2026-05-07)

The `auth` module config document also carries five admin-managed
policy tabs that drive runtime behaviour without a restart. All values
are read through `services.AuthPolicyService` (nil-tolerant — accessors
fall back to the legacy hardcoded defaults when the service or its
ConfigService is missing).

| Group | Keys | Effect |
|---|---|---|
| Registration | `registrationEnabledAdmin/Client`, `defaultRoleClient`, `allowedEmailDomainsAdmin/Client` | **`registrationEnabledAdmin/Client` both default to `false`** — a fresh install accepts no self-service signups until the super_admin opens them. `Register` returns 403 `auth.registration_disabled` / `auth.email_domain_not_allowed` per surface, and the OAuth callback's new-user branch returns `ErrOAuthSignupDisabled` (mapped to the `error=oauth_signup_disabled` callback redirect) when `registrationEnabledAdmin/Client=false` — both paths share the same umbrella kill switch. `defaultRoleClient` overrides the role assigned to a new Tier-2 signup (consulted by both the password and OAuth paths). Non-first operator-tier signups default to `guest` (lowest system role) on both paths so a fresh callback can't grant elevated privileges; the first-admin sentinel still upgrades the very first account to `super_admin`. The very first user on a fresh install bypasses the password Register's kill switch so a misconfigured flag can't lock everyone out — the OAuth path has no first-user bypass; operators bootstrap via password. |
| Login & Sessions | `loginEnabledAdmin/Client`, `passwordLoginEnabledAdmin/Client`, `accountLockoutThreshold`, `accountLockoutDuration`, `sessionAbsoluteTTL` | `Login` returns 403 `auth.login_disabled` per surface; OAuth start endpoints (`InitiateOAuthLogin`, `HandleMobile{Google,Apple}Auth`) honour the same gate. The lockout pair is plumbed into `RateLimiter.SetAuthFailedConfig` on every login attempt — admin edits take effect on the next try. `accountLockoutDuration` (like `accessTokenTTL` / `passwordResetTokenTTL`) is parsed by `utils.ParseDuration`, so `30d` typed into the admin UI now works the same as it does in an env var. `sessionAbsoluteTTL` (ADR-0017 D1) caps total session age from login, independent of the refresh TTL's idle-timeout behaviour; resolved via `AuthPolicyService.SessionAbsoluteTTL` — see the dedicated section below. `passwordLoginEnabledAdmin/Client` (both default `true`, the password-login toggle) is read strictly via `services.StrictBool` on every password sign-in, signup and reset-request check on `/v1/auth/{tier}/*` (absent key → `true`; malformed → the read fails closed rather than silently defaulting), refusing with 403 `auth.password_login_disabled` when off, and 503 `auth.policy_unavailable` when the policy cannot be established. Which routes are gated, which stay open, and with what status, is owned by the "Password-login gate verdicts" section below — read the table there rather than a copy here. The boot-time `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` override rescues the **operator `Login` path and the MFA/WebAuthn completion of a challenge that login created** — never registration, never a reset, never the client surface — and a rescue emits exactly one `auth.policy.break_glass_used` audit event, from the direct full-token login OR from the winning completion, carrying audience, user UUID, session id and source IP (never a password, token or full email). A failed credential attempt under the override claims nothing. Turning either off is additionally guarded at config-write time — see the login-method invariant below — so it can never be flipped into a state with no usable sign-in method. One field pair per audience tier, unlike `sessionAbsoluteTTL` which is shared. |
| Password Policy | `passwordMinLength`, `passwordMaxLength`, `passwordRequireUpper/Lower/Digit/Symbol`, `breachedPasswordCheck` | `passwordService.ValidatePolicy` reads the live policy on every signup / change-password / reset. Defaults match the legacy hardcoded values (10..128 chars, no complexity, HIBP on). New errors: `ErrPasswordMissing{Upper,Lower,Digit,Symbol}`. An inverted min/max range is swapped on read so a misedit can't reject every password. |
| OAuth Providers | `{google,apple,github,discord}Enabled{Admin,Client}`, `oauthAllowSignup{Admin,Client}`, `oauthAutoLinkByEmail` | **All eight `{provider}Enabled{Admin,Client}` toggles default to `false`** — a fresh install exposes no social-login button until the super_admin both configures the provider's credentials AND flips its surface toggle on (a provider with no client ID is already filtered out regardless; the toggle is the explicit second gate). The **web** path (`/providers`, OAuth start, callback) reads the toggles strictly through `OAuthConfigResolver.OAuthWebProviderUsable` — absent → `false` (the schema default), malformed → that provider alone is unusable with a WARN naming the key — while the **mobile** ID-token endpoints keep the permissive `OAuthProviderEnabled` (absent → `true`) until the native flow gets its own decision. A disabled or incomplete provider answers 403 `auth.oauth_provider_disabled` on start; an unreadable auth document answers 503 `auth.policy_unavailable`. Credentials still live one-set-per-provider in the existing tabs. Phase 9: `oauthAllowSignup{Admin,Client}` (default true) is the OAuth-specific signup gate — checked **in addition to** `registrationEnabledAdmin/Client` on the Registration tab (both must allow). When either is off, the OAuth callback returns `ErrOAuthSignupDisabled` and redirects to `/auth/callback?success=false&error=oauth_signup_disabled` instead of creating the user. Phase 10: `oauthAutoLinkByEmail` (default true) gates auto-attaching a provider to an existing account whose email matches the IdP's **verified** address; read strictly by `OAuthAutoLinkByEmailEnabled` (absent → true; malformed/unreadable → 503 `auth.policy_unavailable` before any lookup). When off, the callback returns `ErrOAuthLinkDisabled` (`error=oauth_link_disabled`) and the user must initiate linking from authenticated settings. |
| MFA | `mfaEnabled`, `mfaEnrollmentGraceDays`, `mfaRequiredForRoles`, `recoveryCodesCount` | `mfaEnabled` **defaults to `false`** — a fresh install's first account is `super_admin` (privileged), so seeding it `true` would block that operator from the config writes (e.g. SMTP) needed to finish setup with an MFA prompt for a factor they never enrolled. Operators turn it on **after** enrolling a second factor; otherwise privileged users hit the enrollment grace window on their next login. `mfaEnabled=false` short-circuits `MFARequired` to false (existing enrollments are not deleted; voluntary verification still works). `mfaEnrollmentGraceDays` overrides the legacy 7-day `MFAEnrollmentGraceWindow` constant — new value takes effect on the next login. Phase 9: `mfaRequiredForRoles` (stringList, lowercased on read) replaces the built-in privileged-role list when set. Empty falls back to the built-in (super_admin, administrator, org_owner, org_admin). The kill switch wins over both the built-in and the configured list. Phase 10: `recoveryCodesCount` overrides the legacy `BackupCodeCount` constant when in the safe range 1..50; out-of-range falls back to the legacy default 10. Read at enrollment-confirm time so admin edits take effect on the next user's enrollment. |
| Anti-abuse & Notifications | `notifyUserOnNewDeviceLogin`, `notifyAdminOnSuspiciousLogin`, `suspiciousLoginRecipients`, `ipAllowlistAdmin`, `ipBlocklistAdmin`, `geoBlockCountries`, `inactiveAccountAutoDisableDays` | New-device user email fires from `PasswordAuthService.notifyNewDeviceLogin` when no prior session exists for the (deviceId, userUUID) pair (template `auth.new_device_login`). Admin half of the suspicious-login fan-out reads `notifyAdminOnSuspiciousLogin` + `suspiciousLoginRecipients` live on every `OnLogin` (template `auth.admin_suspicious_login`, distinct idempotency key per recipient). `ipAllowlistAdmin` / `ipBlocklistAdmin` drive a chi middleware (`shared/middleware/ip_gate.go`) mounted on the operator host mux only — empty allowlist = open, blocklist always wins. `geoBlockCountries` resolves the request IP via `geoip.Resolver` and rejects login with 403 `country_blocked` (fails open when geoip is disabled or unable to resolve). `inactiveAccountAutoDisableDays` = N>0 flips `user.IsActive=false` on next login when `lastLogin` is older than N days; 0 disables the check. |
| Sessions & Account | `revokeSessionsOnPasswordChange`, `selfServiceAccountDeletionClient` | Phase 8 trivial toggles. `revokeSessionsOnPasswordChange` (default true) gates both the device-trust revocation in `PasswordAuthService.ChangePassword` and the handler-side session-id revocation — when off, password change leaves existing sessions alive (migration / staged-rollout escape hatch; not recommended in steady state). `selfServiceAccountDeletionClient` (default false) controls whether `POST /v1/me/dsr/erase` is mounted on the client surface; export stays unconditional. The compliance module pulls `*AuthPolicyService` from `ServiceAuthPolicy` at Init and reads the toggle live on every erase request. Operator-side erasure is unaffected by either toggle. `audiencePinning` was originally on this list but is structural (host-mux `RequireAudience`) — exposing it as a flippable toggle would be a security-regression vector, so it stays as a non-toggleable invariant. |

The privileged-role list itself (`super_admin`, `administrator`,
`org_owner`, `org_admin`) is still hardcoded in
`services/mfa_policy.go`. Making it admin-managed is a deliberate
follow-up — the change is security-sensitive and worth a PR diff.

#### Duration bounds on credential-governing config (ADR-0017 D6)

`accessTokenTTL` and `passwordResetTokenTTL` govern credentials that may
already be in a user's hands, so an absurd value is exploitable, not just
inconvenient — a multi-week access token would outlive its own Redis
revocation entry. Both are bounded, and the bound is enforced **twice**
on purpose: once at the PATCH boundary so the stored value can never
disagree with the effective value, and again at read time as a second
line of defence for legacy or out-of-band data. Read-time clamping alone
was explicitly rejected because it leaves the two disagreeing — the
admin UI would show one value while a different one governed logins.

| Input | `accessTokenTTL` | `passwordResetTokenTTL` |
|---|---|---|
| empty | unset **at the policy** — but `ModuleConfigService.GetValue` re-supplies the schema default `15m` for an empty key, so the env fallback below is not reached in practice (see the env-var table) | 30m default |
| malformed or out-of-range PATCH | 422, not persisted | 422, not persisted |
| malformed value already in DB | warn, fall through to env | warn, use 30m |
| out of range already in DB | warn and clamp | warn and clamp |
| env / direct constructor above 24h | warn and clamp to 24h | n/a |
| env / direct constructor below 1m | warn and clamp to 1m — below a minute the SPA's proactive refresh (inside a 30s skew of expiry) would rotate the token on every request (ADR-0020 D3, #317) | n/a |

Write-time enforcement is `(*AuthModule).ValidateConfigSnapshot` in
`config_validation.go` — the module's implementation of the optional
`module.HasConfigSnapshotValidator` seam (ADR-0017 D6; PR 1's snapshot
contract). It judges the complete target snapshot on **all three**
mutation surfaces — active-profile PATCH, named-environment PATCH, and
environment activation — running two rules in sequence: these duration
bounds, then the password-login toggle's anti-lockout invariant (see
"Login method invariant" below). It rejects a non-empty duration value
outside `[services.MinAccessTokenTTL, services.MaxAccessTokenTTL]`
(1m–24h) or `[services.MinPasswordResetTokenTTL,
services.MaxPasswordResetTokenTTL]` (5m–24h) with a
`*module.ConfigValidationError`, which the admin API maps to 422 before
the value ever reaches `UpdateConfig`. An empty value is always accepted —
emptiness is a decision with field-specific meaning, not an omission to
reject. Generic `UpdateConfig` still validates nothing on its own; the
seam is opt-in per module, and `auth` is currently its only implementer.
`auth` no longer implements the legacy value-only `HasConfigValidator` —
the snapshot seam replaced it so the login-method invariant can see
secret *presence*, which the legacy value-only hook never could.
`AuthModule.HotReloadConfig()` returns `true`: `AuthPolicyService` and the
OAuth resolver have always read config live at request time, so a
successful write now persists `needsRestart=false` in the same atomic
update instead of leaving a stale restart banner in the admin UI.
Read-time enforcement is the pre-existing `clampPersistedDuration` in
`services/auth_duration_bounds.go`, which stays as-is.

#### Login method invariant (password-login toggle, spec §4.4)

`passwordLoginEnabledAdmin` / `passwordLoginEnabledClient` are the only
config keys that can leave a surface with **no way to sign in at all**, so
turning either off is refused at write time unless OAuth can carry the
surface on its own. `validateLoginMethodInvariant`
(`config_validation.go`) is the second rule `ValidateConfigSnapshot` runs,
after the duration bounds, and therefore judges the target snapshot on
**all three** mutation surfaces — active-profile PATCH,
named-environment PATCH, and environment activation.

Per surface `S ∈ {Admin, Client}`:

```
passwordOn(S)   := strictBool(Values["passwordLoginEnabled"+S],  default true)
providerOn(p,S) := strictBool(Values[p+"Enabled"+S],             default false)
structural(p)   := EffectiveValues[p+"ClientId"]    ≠ "" ∧
                   EffectiveValues[p+"RedirectURL"] ≠ "" ∧ secret(p)
    where secret(google|github|discord) := SecretPresent[p+"ClientSecret"]
          secret(apple)                 := EffectiveValues["appleTeamId"] ≠ "" ∧
                                           EffectiveValues["appleKeyId"]  ≠ "" ∧
                                           (SecretPresent["applePrivateKey"] ∨
                                            readable non-empty applePrivateKeyPath)
oauthOn(S)      := ∃ p ∈ services.WebProviderOrder : providerOn(p,S) ∧ structural(p)
autoLink        := strictBool(Values["oauthAutoLinkByEmail"],    default true)

valid(S)        := passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)
```

- **Only the target snapshot is consulted** — never the active profile,
  never a secret *value*. Booleans come from `snap.Values` (raw, so an
  absent key takes the schema default), the structural strings from
  `snap.EffectiveValues` (EnvVar/`Default` fallback applied), and secrets
  from `snap.SecretPresent`. `providerStructuralFromSnapshot` assembles the
  same `services.ProviderStructuralFields` the runtime's `usableFromView`
  assembles, field-for-field, and hands them to the **same** exported
  predicate the OAuth flow uses, `services.ProviderStructurallyConfigured`,
  so the validator and `OAuthWebProviderUsable` can never disagree about
  what "configured" means. Apple's file leg is probed through `services.ReadableNonEmptyFile`
  (the `KeyFileProbe` seam), so a path naming a missing or empty file is
  not a present secret.
- **`autoLink` is the PR 2 half of the invariant.** An SSO-only surface is
  only survivable if an existing account can still be reached by its
  provider-**verified** email; `oauthAutoLinkByEmail` is the switch that
  allows it (documented on the OAuth Providers row above), so turning it
  off while a surface is password-off is the same lockout and gets the
  same refusal.
- **Malformed booleans are rejected up front.** All eleven participating
  keys — `oauthAutoLinkByEmail` plus, per surface, `passwordLoginEnabled<S>`
  and the four `{provider}Enabled<S>` toggles — are parsed with
  `services.StrictBool` *before* any surface is judged, so an out-of-band
  `"treu"` is refused on **every** write rather than only when its surface
  happens to be the one being switched off. That failure is a
  `*module.ConfigValidationError` naming **that key** ("must be exactly
  true or false") with **no** `Code` — it is a field-shape error, not the
  invariant.
- **Only the cross-field lockout carries a code.** It names
  `passwordLoginEnabled<S>` and sets `errcode.AuthLoginMethodLockout`
  (`auth.login_method_lockout`); the admin API maps the whole envelope to
  **422** before anything reaches `UpdateConfig`, so config *and* enabled
  state are unchanged and the audit row records `outcome=failure` with key
  names only. The message names both ways out: keep the password method
  on, or leave one fully configured, enabled OAuth provider on this
  surface **together with** auto-link.
- **This is why `auth` moved to the snapshot seam.** The legacy value-only
  `HasConfigValidator` sees one key's new value; `structural(p)` needs
  secret *presence* across the whole target profile, which only
  `ConfigValidationSnapshot` carries. `auth` no longer implements the
  legacy hook.

Pinned by `config_validation_test.go`. The staging drill for the refusal
(and for the concurrent-PATCH race that ends in `409
module.config_revision_stale` then this 422 on retry) is spec §7 steps 2–3
and 5.

#### Password-login gate verdicts (spec §4.3)

The rule: **a route is gated iff it would authenticate a NEW session — or
mint a credential-setting token — with the password method.** Routes that
manage or verify the credential *inside an existing flow* stay open:
change-password, reset-password, accept-invite, verify-email,
resend-verification. (Change-password verifies the current password, so
"authenticates with the password" alone would wrongly gate it.) The
credential legitimately continues to exist for the other surface and for a
later re-enable, and deleting it is a different decision this toggle does
not make.

| Route | Verdict | Accessor / notes |
|---|---|---|
| `POST /v1/auth/{tier}/login` | **403** `auth.password_login_disabled` | `PasswordLoginDecision`. Sits after the `loginEnabledAdmin/Client` kill switch and **before** `GetUserForAuth`, so lockout counters, the rate limiter and the audit trail see nothing and every email — known or unknown — gets the identical answer. Only the operator surface can be rescued |
| `POST /v1/auth/{tier}/register` | **403** | Strict `PasswordLoginEnabled` — break-glass never opens registration. The **operator-only** first-user branch and `RegisterInitialAdmin` (setup wizard) are the two bootstrap exceptions: they are evaluated before the gate and read no policy at all. The client tier has no first-user bypass |
| `POST /v1/auth/{tier}/forgot-password` | **403** | Strict `PasswordLoginEnabled`, evaluated **before** the user lookup, so no reset token is minted and the outcome cannot depend on account state. `ErrPasswordLoginDisabled` and `ErrAuthPolicyUnavailable` are the ONLY errors this service method propagates — every account-specific outcome stays swallowed behind the generic success body, so it is not an enumeration oracle. `PasswordAuthHandler.ForgotPassword` enforces the same contract independently: it maps those two sentinels and lets **any** other error fall through to the generic success body (logged at warn, never with the address), so a future service-side error cannot answer differently for some addresses |
| `POST /v1/auth/{tier}/mfa/login/verify`, `POST /v1/auth/{tier}/mfa/webauthn/login/finish` | **403** when the challenge was password-sourced | `PasswordLoginDecision`, re-evaluated **before** the factor is verified. See "Completion re-check" under HTTP endpoints for the four outcomes (untouched / 403+consume / 503+retain / 401 on an empty audience) |
| `POST /v1/admin/users/{userId}/send-password-reset` **and** `POST /v1/admin/client-users/{id}/send-password-reset` | **409** `auth.password_login_disabled` | Strict `PasswordLoginEnabled` on the target's tier, inside `AdminTriggerPasswordReset`. A reset link for a refused method would also revoke the target's sessions and leave them an unusable password. Break-glass never opens it. Both handlers match `iface.ErrPasswordLoginDisabled` with `errors.Is` |
| `POST /v1/auth/{tier}/me/password-confirm` | **409** `auth.password_confirm_unavailable` | Same 409 branch as "no password hash": a credential the surface refuses cannot prove presence either (§4.6) |
| `POST /v1/auth/{tier}/reset-password` | **open** | Redeems a token only the gated routes can mint; closing it would strand a reset in flight |
| `POST /v1/auth/{tier}/accept-invite` | **open** | Sets the password **and** marks the email verified atomically; auto-link is what lets that account sign in with OAuth later |
| `verify-email`, `verify-email/resend`, `change-password` | **open** | Not a credential, or authenticated credential management |

Every gated path answers **503** `auth.policy_unavailable` instead when
the policy cannot be established — a nil policy service is an outage, never
a legacy allow.

**Decision vs accessor — two methods, and the split is the security
boundary.** `AuthPolicyService.PasswordLoginEnabled(ctx, audience)` is the
strict accessor: absent key → `true` (compatibility applies to the key, not
to the document), present canonical boolean → its value, and a missing
document / repository failure / malformed value / unknown audience / unwired
service → an error wrapping `ErrAuthPolicyUnavailable`. It **never** sees
the break-glass. `PasswordLoginDecision(ctx, audience)` wraps it and is used
by exactly two callers — `Login` and the completion of a challenge `Login`
created: on the **operator** audience only, and only when
`AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` is set, it converts a stored
`false` *or* a failed read into `{Allowed: true, BreakGlassUsed: true}`. So
an outage never silently fails open, while the recovery switch still works
when ConfigService itself is the outage. Everything else — registration,
forgot/reset, admin reset, password-confirm, `PasswordReauthAllowed`, the
unlink guard, `AuthMethodsView` — calls the accessor and is blind to the
override by construction.

A rescued authentication emits exactly one `auth.policy.break_glass_used`
audit row (`EmitBreakGlassUsed`, best-effort through the nil-guarded sink):
from `Login` on a direct full-token success, or from the **winning**
MFA/WebAuthn completion when either that completion's own decision or the
challenge's stamped `BreakGlassUsed` was a rescue. The row carries the
audience, the user UUID, the session id and the source IP — never a
password, a token or a full email. A failed credential attempt under the
override claims nothing.

#### Absolute session cap (ADR-0017 D1)

The refresh TTL is an **idle** timeout — rotation writes a fresh
`now + refreshTTL` on every use, so an active user is never asked to
re-authenticate no matter how long the session has run. `sessionAbsoluteTTL`
(`services/session_cap.go`) bounds total session age from `session.StartedAt`
instead: default `720h` (30 days), range `[services.MinSessionAbsoluteTTL,
services.MaxSessionAbsoluteTTL]` (1h–89d), enforced at the PATCH boundary via
`authDurationBounds` in `config_validation.go` exactly like `accessTokenTTL`
and `passwordResetTokenTTL`. `MaxSessionAbsoluteTTL` leaves
`services.SessionRetentionSafetyMargin` (24h) below `models.AuthSessionRetention`
(90d) — equality is unsafe, because at exactly the retention boundary
Mongo's TTL monitor can delete the session document before the refresh path
evaluates the cap, presenting an expired session as a missing anchor.
`services/session_cap_test.go`'s `TestSessionAbsoluteTTLLeavesRetentionMargin`
pins the strict inequality so changing either constant breaks the build.

Clearing the field is the supported way for a fork to disable the cap
without patching code — `(*AuthPolicyService).SessionAbsoluteTTL` then
returns `0`. This is the one duration in the module whose *empty* value
means something other than "fall back to a default", so it cannot be
resolved through `ModuleConfigService.GetValue`: that accessor's `ok &&
v != ""` guard makes an absent key and an operator-cleared key
indistinguishable, both falling through to the schema `Default` (`"720h"`).
`ModuleConfigService.GetRawValue(ctx, moduleName, key) (string, bool, error)`
(`pkg/sdk/module/config_service.go`) is the narrow accessor added for this:
it reports the active environment's stored value plus whether the key is
actually present, so "present and empty" (disable) and "absent" (never
configured, use the default) are distinguishable. `configValueReader` in
`services/auth_policy_service.go` was extended with `GetRawValue` to expose
it to `SessionAbsoluteTTL`; `GetValue` itself is unchanged, so no existing
caller's behaviour shifts.

**The third return value is load-bearing, and the reason `SessionAbsoluteTTL`
returns `(time.Duration, error)`.** A failed `module_configs` read is not an
absence — it says *nothing* about the key. Collapsing it into `("", false)`
(which the accessor originally did) made a transient read failure take the
"absent" branch and answer with the 30-day default, so a deployment that had
deliberately cleared `sessionAbsoluteTTL` got the cap **back** for the
duration of the outage — irreversibly signing out every session older than 30
days that refreshed in that window. Every other failure on this path fails
closed to 503; that one silently substituted a different policy. Both layers
now propagate: `GetRawValue` returns the error, `SessionAbsoluteTTL` maps it
to `ErrSessionEnforcementUnavailable`, and `sessionWithinAbsoluteCap`
propagates it to the 503 the handler already emits. Pinned by
`TestSessionAbsoluteTTL_ReadErrorDoesNotApplyTheDefault`,
`TestSessionWithinAbsoluteCap_PolicyReadErrorFailsClosed`, and (at the SDK
layer, hermetically, against an unreachable Mongo)
`TestGetRawValue_ReadFailureIsNotAbsence`. **A `nil` document is still an
absence, not an error** — a module with no config document has genuinely said
nothing.

Enforcement is `(*authService).sessionWithinAbsoluteCap`, called by **both**
`RefreshTokensWithRiskAssessment` and `MintAccessTokenFromRefresh` — see the
"A session has a maximum age" invariant below for why the non-rotating path
is not optional. Reaching the cap runs `expireSessionForMaxAge`: revoke the
session's refresh rows with `models.RevokeReasonSessionMaxAge`, CAS the
session document inactive via `AuthSessionRepository.ExpireSessionForMaxAge`,
push the sid onto the Redis denylist. Only the caller that **wins** the CAS
emits `orkestra_auth_session_cap_expiries_total` and the
`session_max_age_reached` security event, so concurrent refreshes on one
session count once. A repository error at any of those steps returns
`ErrSessionEnforcementUnavailable` (fail closed, never a cap expiry); a
denylist failure *after* durable revocation returns
`SessionRevocationDegradedError`, because the logout did happen and the
caller must still clear the cookie. A clean not-found — or a row with
neither `StartedAt` nor `CreatedAt` — fails **open** under a measured
compatibility window, counting
`orkestra_auth_session_anchor_anomalies_total{kind="missing"|"zero_timestamp"}`;
that rule is to be tightened to fail-closed in the first minor release after
30 consecutive production days at zero, tracked in
[#277](https://github.com/orkestra-cc/orkestra/issues/277). A row with `StartedAt` zero but a
usable `CreatedAt` is **not** an anomaly — it has a perfectly good anchor,
and counting it would poison the observation window.

**The cap outcomes above, and the refresh-path outcomes beside them, surface
as six distinct HTTP responses**
(`writeRefreshErr`, called from all three refresh-flow handlers —
`RefreshTokensWithHeaderHTTP`, `GetSessionHTTP`, `RefreshTokensHTTP`):
`ErrSessionEnforcementUnavailable` is **503** `session_enforcement_unavailable`
— never a 401, because reporting a storage outage as an authentication
failure would train clients to discard a session that is still perfectly
valid, and the caller may retry once storage recovers.
`ErrSessionMaxAgeReached` is **401** `session_max_age_reached` — distinct
from `refresh_token_replay` because "revoked" is inaccurate for a session
that simply aged out.
`ErrRefreshRotationRaced` is **409** `refresh_rotation_raced` — a sibling
tab won the CAS inside `RefreshRotationGrace` against a healthy family, so
the browser already holds the successor cookie and one retry lands. It is
kept off the 401 path precisely because every client reads a 401 there as
a sign-out; the grace-window rule itself is under **Refresh tokens** below.

`ErrRefreshLookupUnavailable` is **503** `refresh_lookup_unavailable` — the
refresh path could not be *completed* because infrastructure failed: the token
store or user store was unreachable, signing failed, the rotating write
failed, **the picker in front of the rotation could not classify a cookie
candidate** (`PeekRefreshToken`'s lookup), **the rotation-race classifier
could not read the family state** (`benignRotationRetry`'s `FamilyRevoked`, or
the post-CAS re-read), **or the read-only mint behind the picker failed on one
of its own three reads** (`MintAccessTokenFromRefresh`'s `GetByTokenAny`, its
`GetUserByID`, its `GenerateAccessTokenForSessionWithAMR`). The race-classifier
pair is the destructive case: both used to fold
"could not read" into "revoked" and run `handleRefreshReplay`, so a store
hiccup during a legitimate multi-tab race revoked the family the winner had
just renewed and signed every tab out. `benignRotationRetry` now returns
`(benign, err)` — three states, not two — and on the error both callers answer
503 **without** revoking. A replay verdict that *was* reached still answers 401
even if its `RevokeFamily` fails: fail closed denies the current request, it
does not invent a verdict and persist it.
Before this, EIGHT of those ten sites were wrapped
generically and answered as a codeless 401, so a Mongo blip during a refresh
reached the SPA as the same answer a dead refresh token produces and no
client-side rule could separate them. The picker case is the one that
actually answers a browser: the cookie handlers never reach the rotation
until every candidate is classified, so `pickRefreshCandidate` now returns a
lookup error as a third value, and on that input the handlers answer 503
**and suppress the rotated-token replay fallback** — a candidate the store
could not read may have been the valid successor, and revoking that family
is the PR-D D-9 regression in a new shape. A user who is genuinely **gone**
still answers 401 (`errors.Is(err, iface.ErrUserNotFound)`) — the
distinction is what stops this becoming a blanket 503 that never lets a dead
session end. Every emitting site wraps the sentinel *and* the underlying
error (`fmt.Errorf("…: %w: %w", ErrRefreshLookupUnavailable, err)`) so the
cause survives into the log while `errors.Is` still classifies. The cookie
is never expired on this outcome (`clearRefreshCookieOnTerminalRefreshErr`
is an allowlist), and `refreshFailureOutcome` logs it as
`lookup_unavailable`, not `invalid_token`.

**Scope, precisely:** the ten sites are the four infrastructure wraps inside
`RefreshTokensWithRiskAssessment` (token lookup, user lookup, mint, rotating
write), `PeekRefreshToken`'s lookup, the two race-classifier reads —
`benignRotationRetry`'s `FamilyRevoked` and the post-CAS re-read in the
`ErrTokenAlreadyRotated` branch — and the three inside
`MintAccessTokenFromRefresh` (its own `GetByTokenAny`, its `GetUserByID`, its
`GenerateAccessTokenForSessionWithAMR`). So `POST /v1/auth/{tier}/refresh-cookie`,
`POST /v1/auth/{tier}/refresh` **and** `GET /v1/auth/session` are each covered
end to end. The rule the race-classifier pair
encodes: **an unreadable family state answers 503 and revokes nothing;
replay may fire only on a family state that was actually read.** A readable
state behaves exactly as before — inside the grace window against a healthy
family it is `ErrRefreshRotationRaced` (409), outside it or against a revoked
family it is `handleRefreshReplay` + 401.

The mint's three were the residual, closed by spec v20 (follow-up 9). They are
reachable precisely because Peek and Mint issue **separate** reads: a blip
opening between them gave Peek-OK → Mint-fail → a codeless 401 on session
bootstrap, which is the one status a client reads as the end of the session.
Its user lookup carries the same not-found-first split as the rotation's, so a
genuinely deleted account still ends the session with a 401 — a blanket 503
there would strand it in a retry loop that never resolves. The absolute-cap
return between them stays **unwrapped**: it propagates
`ErrSessionMaxAgeReached` / `ErrSessionEnforcementUnavailable` verbatim and
wrapping it would break `writeRefreshErr`'s branch order (ADR-0017 owns it).
**No handler change was needed:** `GetSessionHTTP` already routes the mint's
error through `writeRefreshErr`, `refreshFailureOutcome` already logs it as
`lookup_unavailable`, and the cookie-clear allowlist already excludes the
sentinel.

> **The same code is also emitted by `shared/middleware.AuthMiddleware`,
> and that is the path that actually reaches a user.** The three refresh
> handlers above are read by machinery, not people: `frontend-admin`'s
> `performRefresh` discards the body on `!res.ok`, and `/v1/auth/session`
> is classified as an auth check whose toast is suppressed. What a
> capped-out user actually hits is their still-live access token meeting
> the denylist on the next protected request — which used to answer the
> generic `session_revoked`. `Revoke` stores the reason as the Redis
> **value**, and `IsRevoked` was already issuing the `GET` and discarding
> it, so the middleware now reads it through the optional
> `services.SessionRevocationReasonReader` extension and maps
> `models.RevokeReasonSessionMaxAge` to `session_max_age_reached`. The
> extension is discovered by type assertion, not added to
> `SessionRevocationService`, so a fork's own implementation keeps
> compiling and simply gets the generic wording. No extra round-trip.
> Covered by `require_auth_test.go`'s `TestRequireAuth_SessionMaxAge_*`
> trio, including the fallback for a reason-blind service.
>
> **The middleware's 401 codes, and the one a client may act on.**
> `RequireAuth`'s credential-rejection path emits exactly three
> top-level codes, all from `shared/middleware/auth.go`:
> `session_revoked` and `session_max_age_reached` (above), and
> **`access_token_expired`** — a well-formed, correctly signed token
> that simply aged out (`sendAccessTokenExpired`, plus
> `WWW-Authenticate: Bearer error="access_token_expired"`). (A fourth,
> `step_up_required`, can come out of `RequireAuth` too, but from
> *after* the credential was accepted: `setUserContext`'s
> impersonation-bypass branch, `sendMFARequired`.) Every other
> rejection on the credential path — missing bearer, malformed, forged
> signature — keeps the **codeless** generic 401 it has
> always had: `sendErrorResponse`
> puts the `AppError`'s code in `errors[0].value`, and for an
> `AuthenticationError` that is `INVALID_CREDENTIALS`, the same value a
> wrong password produces, so it discriminates nothing. That is
> deliberate — the codes are a small, closed set, not a per-branch
> taxonomy. A **wrong-audience** token is not in that list: on every
> mounted router `RequireAudience` runs in FRONT of `RequireAuth` and
> answers it first with `401 audience_mismatch`
> (`shared/middleware/audience.go:102-108`), so `RequireAuth`'s codeless
> 401 is only what a wrong audience would get on a router that mounted
> `RequireAuth` alone.
>
> `access_token_expired` is the **only non-terminal one of the three**:
> the other two say the session is gone and the client must clear and
> re-authenticate, while this one says the credential aged out and the
> sanctioned recovery (`401` → `POST /v1/auth/{tier}/refresh-cookie` →
> retry, ADR-0020) will work — so it is the only code on this path a
> client should answer by refreshing. It is also the client's proof that the
> retry is *safe*: the middleware rejects before dispatch, so a request
> answered with it provably never reached its handler and cannot have
> consumed anything (the alternative — the client inferring expiry from
> its own reckoning of the token's lifetime — cannot cover a token that
> expired in flight, and a loose inference replays a rejected request
> such as a wrong-current-password `change-password`). Two properties
> keep it honest and must stay true: it has **exactly one emitter** in
> `backend/`, and expiry is decided by `jwt.Parse` **before**
> `validateTokenEnhanced`'s own token-type, issuer and audience checks
> (`jwt_service.go`), so a wrong audience or a wrong token type can
> never acquire it. Adding a second emitter, or moving the expiry
> decision after those checks, silently breaks a client that refreshes
> on it. Nothing about what is *accepted* changed when it shipped —
> only what a rejection says about itself — and `RequireAuth` stays
> bearer-only: it rotates nothing and sets no cookie. Covered by
> `require_auth_test.go`'s `TestRequireAuth_ExpiredBearer_*`,
> `TestRequireAuth_NonExpiredRejections_CarryNoExpiredCode` (the bound)
> and `TestRequireAuth_RevokedSession_StillReportsRevokedNotExpired`
> (the terminal code is not shadowed).

> **Both SPAs treat the 503 as "retry later", never as a sign-out.**
> `frontend-admin`'s `performRefresh` returns `{ok:false, retry:true}` and
> `frontend-client`'s `refreshAccessToken` returns
> `{status:'unavailable'}`; neither clears the access token, and the
> client also keeps its `localStorage` session marker (clearing it would
> make the sign-out sticky across the next cold load). Both collapsed the
> 503 into the 401 path before, logging the user out for the exact reason
> the 503 exists to prevent. `SessionRevocationDegradedError` is **401 with no
code**: a generic logout, since a partially degraded cap logout must not
claim a completely recorded cap expiry. The HttpOnly refresh cookie is
expired (`clearRefreshCookieOnTerminalRefreshErr`, called immediately
before `writeRefreshErr` at each of the three call sites) on exactly the
two outcomes where the session is durably gone — cap expiry and the
degraded logout — and deliberately left alone on
`ErrSessionEnforcementUnavailable`, where durable logout is not known to
have completed. Redux state cleanup on the frontend is not a substitute:
without the expiring `Set-Cookie`, the browser keeps presenting a cookie
for a session that is durably dead.

`accountLockoutDuration` and `accountLockoutThreshold` are **deliberately
excluded** from this validator: neither governs an already-issued
credential, and an absurd value there is self-punishing (an operator who
sets a year-long lockout locks out real users, not an attacker) rather
than exploitable.

### OAuth provider credentials (admin-managed)

| Provider | Key | Type | Seed env var |
|---|---|---|---|
| Google | `googleClientId` | string | `OAUTH_GOOGLE_CLIENT_ID` |
| Google | `googleClientSecret` | secret | `OAUTH_GOOGLE_CLIENT_SECRET` |
| Google | `googleRedirectURL` | string | `OAUTH_GOOGLE_REDIRECT_URL` |
| Google | `googleAndroidClientId` | string | `OAUTH_GOOGLE_ANDROID_CLIENT_ID` |
| Google | `googleIOSClientId` | string | `OAUTH_GOOGLE_IOS_CLIENT_ID` |
| Apple | `appleClientId` | string | `OAUTH_APPLE_CLIENT_ID` |
| Apple | `appleTeamId` / `appleKeyId` | string | `OAUTH_APPLE_TEAM_ID` / `OAUTH_APPLE_KEY_ID` |
| Apple | `applePrivateKey` | secret | `OAUTH_APPLE_PRIVATE_KEY` (inline PEM) |
| Apple | `applePrivateKeyPath` | string | `OAUTH_APPLE_PRIVATE_KEY_PATH` (file fallback) |
| Apple | `appleRedirectURL` | string | `OAUTH_APPLE_REDIRECT_URL` |
| Apple | `appleIOSClientId` / `appleAndroidClientId` | string | `OAUTH_APPLE_IOS_CLIENT_ID` / `OAUTH_APPLE_ANDROID_CLIENT_ID` |
| GitHub | `githubClientId` / `githubClientSecret` / `githubRedirectURL` | string / secret / string | `OAUTH_GITHUB_*` |
| Discord | `discordClientId` / `discordClientSecret` / `discordRedirectURL` | string / secret / string | `OAUTH_DISCORD_*` |

### Process-scoped (env vars only)

Every duration value below — env var or admin-managed — is read by the
same parser, `internal/shared/utils.ParseDuration` (ADR-0017): anything
`time.ParseDuration` accepts, plus a bare `<number>d` day suffix
(`30d`, `0.5d`). Before ADR-0017 the env path (`config.parseDuration`)
accepted `d` and the admin/module path (`AuthPolicyService`,
`module.go`'s `parseDurationEnv`) did not — the same string meant two
different things depending on where it was typed, and
`AUTH_DEVICE_TRUST_DURATION=30d` silently fell back to its default.

| Env var | Purpose | Default |
|---|---|---|
| `AUTH_JWT_PRIVATE_KEY` / `AUTH_JWT_PUBLIC_KEY` | RS256 key pair (paths or PEM) | — (required) |
| `AUTH_REQUIRE_EMAIL_VERIFICATION` | Gate signup on successful verification | `true` in prod, `false` otherwise |
| `AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS` | **Emergency only.** Read once at boot into `cfg.Auth.OperatorPasswordLoginBreakGlass` and handed to `AuthPolicyService.SetOperatorBreakGlass` by `module.go::Init`, which also logs a WARN on **every** boot while it is set. Rescues `POST /v1/auth/operator/login` (and the MFA/WebAuthn completion of a challenge that login created) when `passwordLoginEnabledAdmin` is `false` **or** unreadable — see "Login method invariant" and "Password-login gate verdicts" above. It opens nothing else: not registration, not forgot/reset, not password-confirm, not the unlink guard, not `AuthMethodsView`, not the client surface, and it never bypasses `loginEnabledAdmin` or the MFA / low-risk / RBAC gates on the config repair that follows. There is no unset path short of a restart. `docker/.env.example` carries the five-step drill. | `false` |
| `JWT_ACCESS_TOKEN_EXPIRY` | Access-token TTL — **level 2 of `admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY → 15m`, and on a seeded install it is not the level that governs.** `accessTokenTTL` is declared in `ConfigSchema` with `Default: "15m"` and **no `EnvVar`**, so `buildInitialConfig` seeds it on first boot, `backfillSchemaKeys` back-fills it into older documents, and `GetValue`'s `schemaFallback` returns the same default even for a key an operator has cleared. `AuthPolicyService.AccessTokenTTL` therefore answers with a positive duration on any readable, parsable document — not the `0` that would let `accessTokenLifetime` fall through — and this variable is consulted only when the `module_configs` document is missing, unreadable, **or holding a value the parser rejects** — a malformed persisted value makes `clampPersistedDuration` return its `0` fallback, which is the "warn, fall through to env" row of the ADR-0017 D6 table above, and it needs an out-of-band write because the PATCH validator refuses a malformed value. **Change the TTL at `/admin/modules/auth` (or `PATCH /v1/admin/modules/auth`), not here** — a `60s` set here alone was observed to change nothing until the admin key was PATCHed to `1m`. ADR-0017 D5 repaired this chain at the policy layer, where the policy's own 15m default used to swallow "unset"; the schema default re-forms it one layer down. Documented, not changed: moving the default would alter the effective TTL of every seeded deployment. A value outside `[MinAccessTokenTTL, MaxAccessTokenTTL]` (1m–24h) is **refused with a 422** at the PATCH boundary (`validateAuthDurations`) and **clamped with a warning** at the two levels that cannot surface one — `clampPersistedDuration` on read, and `NewJWTService` for this variable — because below 1m the SPA's proactive refresh would rotate the token on every request (ADR-0020 D3, #317). | `15m` |
| `JWT_REFRESH_TOKEN_EXPIRY` | Refresh-token TTL — and, because rotation writes `now + this` on every use, the **idle** timeout: this many days without a refresh ends the session. The absolute cap is the separate `sessionAbsoluteTTL`. The `refreshTTL <= 0 → 720h` guard in `NewJWTService` is unreachable through configuration. | `7d` |
| `AUTH_DEVICE_TRUST_DURATION` | "Remember this device" trust-grant lifetime (`models.DeviceTrustDuration`), read via `parseDurationEnv` in `module.go`. Accepts the `d` suffix (`30d`); malformed or unset logs a warning and falls back to the default. | `30d` (720h) |
| `COOKIE_NAME_REFRESH` / `COOKIE_SECURE` / `COOKIE_SAME_SITE` / `COOKIE_HTTP_ONLY` | Refresh-token cookie attributes shared across audiences. `COOKIE_NAME_REFRESH` names the cookie (the **only** cookie Orkestra sets — the SPA holds the access token in memory); defaults to `orkestra_cookie`. | set in `cfg.Auth.Cookie` |
| `OPERATOR_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for tokens minted on the operator host (`console.*`). ADR-0003 PR-D D-9 — keep this distinct from `CLIENT_COOKIE_DOMAIN` so a session minted on one surface can't be replayed on the other. An empty value mints the cookie without a `Domain` attribute (scoped to the minting host). | `console.localhost` (dev) / empty (prod, operator-set) |
| `CLIENT_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for tokens minted on the client host (`api.*`). | `api.localhost` (dev) / empty (prod, operator-set) |
| `FRONTEND_URL` | Legacy single-host SPA origin. Used to build `verify-email` / `reset-password` links in transactional email and as the fallback when the per-tier values below are empty. | `http://localhost:8080` |
| `OPERATOR_FRONTEND_URL` | Operator-tier SPA origin (`console.*`). Verification + reset links minted by the operator-tier `PasswordAuthService` use this host. Empty falls back to `FRONTEND_URL`. | empty |
| `CLIENT_FRONTEND_URL` | Client-tier SPA origin (`app.*`). Set this so signups landing on the client API host get verify links pointing at the client SPA, not the operator console. Empty falls back to `FRONTEND_URL`. | empty |
| `APP_NAME` / `SUPPORT_EMAIL` | Rendered into verification/reset email templates | `Orkestra` / empty |

### Resolver API

`OAuthConfigResolver` is the single entry point handlers use to read OAuth settings. Never read `cfg.Auth.Google/Apple/GitHub/Discord` directly — those fields are still populated from env vars for the Load path but are effectively dead code for the OAuth handlers.

| Method | Returns |
|---|---|
| `OAuthWebProviderUsable(ctx, audience, provider)` | `(*OAuthProviderConfig, bool, error)` — **the web path.** ONE `ActiveConfigRequiredModule` read; strict toggle (`{provider}Enabled{Admin,Client}`: absent → schema default `false`, malformed → unusable + WARN naming the key) then `ProviderStructurallyConfigured` over the same view, then the config the provider is built from — no check-then-reread. `(nil, false, nil)` is a per-provider defect; a non-nil error (missing document, repository error, undecryptable stored secret) is document-level and maps to **503 `auth.policy_unavailable`** |
| `UsableWebProviders(ctx, audience)` | `([]models.OAuthProvider, error)` — the above over `WebProviderOrder` (google, apple, github, discord) from one read; served by `GET /v1/auth/{tier}/providers` |
| `Get(ctx, provider)` | `(*OAuthProviderConfig, bool)` — **legacy per-key path, mobile only**; `false` means client ID is empty. Every key is a separate `module_configs` read and a decrypt failure silently falls back to the env var — which is why the web flow does not use it |
| `RedirectURL(ctx, provider)` | Web callback URL, or `""` (legacy) |
| `MobileAudience(ctx, provider, platform)` | Platform-specific client ID for mobile ID-token validation; falls back to the web client ID when `platform` is unknown |
| `ConfiguredProviders(ctx)` | Legacy "has a client ID" list; no longer serves `/providers` |

`services.OAuthResolver` is the interface `AuthHandler` holds (the concrete resolver satisfies it; tests inject a fake). `ProviderStructurallyConfigured(p, fields, probe)` is the single exported pure predicate — `clientId ≠ "" ∧ redirectURL ≠ "" ∧ secret present` (apple: `teamId ∧ keyId ∧ (inline key ∨ readable non-empty key file)`) — and receives secret **presence** only; the PR 3 validator reuses it against the target snapshot.

### OAuth state-encoded tier dispatch (ADR-0003 PR-D D-6)

The OAuth `state` parameter is a **signed HS256 JWT** carrying the audience tier the flow was started for:

```json
{ "tier": "operator" | "client" | "", "csrf": "<32-byte-base64url>", "exp": <now + 10min>, "iat": <now> }
```

- **HMAC secret** is derived deterministically from `cfg.Auth.JWT.PrivateKey` (`SHA-256("orkestra-oauth-state-secret-v1\x00" || PKCS8(privateKey))`). Every replica reaches the same secret without an env var; rotation is implicit when JWT keys rotate.
- **CSRF nonce doubles as the Redis key** that holds the per-flow side data (provider, redirectUri, deviceInfo, securityContext). The Redis row also stores `tier`; the callback cross-checks `state.tier == redis.tier` to defeat any tamper that touches only one half.
- **Per-audience start endpoints** mount under `/v1/auth/{operator,client}/{providers,oauth/login,google/mobile,apple/mobile}` via `RegisterOAuthStartRoutes(api, mount)`. Each tier-bound `AuthHandler` instance has `tier` set so its start endpoints stamp the matching value into the JWT. Legacy `/v1/auth/...` start endpoints stamp `tier=""` so callbacks self-handle on the legacy `authService` (preserves any in-flight pre-cutover flows).
- **Single shared callback** stays at `/v1/auth/oauth/{provider}/callback` (one redirect URI per provider, no IdP-side duplication). Mounted exclusively on the operator host mux by the legacy `AuthHandler`. On every callback `dispatchTarget(state.tier)` returns either the legacy handler itself (empty/unknown tier) or the matching tier-bound `AuthHandler` from the `tierDispatch` map; that target's `authService` mints the tokens and that target's `config.Auth.Cookie` controls the refresh-token cookie. Tier-aware mobile ID-token endpoints follow the same mount pattern but bypass state — they invoke their handler instance's `authService` directly.

Wiring (in `module.go::Init`):
- `m.authHandler.SetStateSecret(secret)` + `SetTierDispatch(map[string]*AuthHandler{operator: m.operatorAuthHandler, client: m.clientAuthHandler})` — the dispatcher.
- `m.operatorAuthHandler.SetTier("operator")` + `SetStateSecret(secret)` — operator-tier start endpoints.
- `m.clientAuthHandler.SetTier("client")` + `SetStateSecret(secret)` — client-tier start endpoints (also wired to the client-audience JWT service so minted tokens carry `aud=client`).

### OAuth callback contract (spec §4.10)

Every provider callback runs `completeOAuthCallback` (`handlers/oauth_callback_flow.go`), whose order is **trust before destination**: `resolveStateForCallback(ctx, raw, provider)` checks signature/expiry → browser binding → atomic one-shot take → `tier` → `provider` → link-mode pair, and any failure is a terminal generic **400 with no redirect** — no trusted tier exists yet, so there is no SPA to send anyone to. Only then does the handler dispatch to the tier-bound instance, clear the `orkestra_oauth_state` cookie (if it lives on this host), interpret the IdP's `error`, require the code, resolve the provider **strictly from one config read** (`OAuthWebProviderUsable`, so a provider disabled mid-flow is refused and the provider is built from the value that answered the check) and exchange. Every failure from that point redirects to the **configured tier SPA** (`spaURL()` — `OPERATOR_FRONTEND_URL` / `CLIENT_FRONTEND_URL` → `FRONTEND_URL`, handed to the handler by `module.go` via `SetSPAURL`; the `Origin` header is never read) with a coarse allowlisted code; raw IdP/error text stays in sanitized log fields.

**Where a flow completes depends on who owns the cookie.** Every provider callback is mounted on the operator host, and a response from `console.*` cannot set a cookie for `api.*` (RFC 6265 §5.3; the cross-tier isolation model has no shared parent domain). So an operator/legacy login completes inline (`finishOAuthCompletion`: application half, refresh cookie with the target tier's domain and TTL, redirect), while a **client-tier login is relayed — every terminal outcome of it**: the callback stores an encrypted one-shot `OAuthRelayRecord` (tier, provider, the state's CSRF nonce, then either the user-info map + provider tokens + security context + device info, or the allowlisted `FailureCode` of a flow that already failed; `OAuthRelayTTL` 60 s) and redirects to `{CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`. `HandleOAuthRelayCompleteHTTP`, on the client host mux, takes the record atomically, **requires** the state cookie its own host set at start to equal the nonce (`verifyRelayBinding`) — the browser binding the operator host had to defer (`verifyOAuthStateBinding` decides on `StartHost` first, so an unrelated operator nonce on `console.*` neither blocks nor binds a client flow) — refuses a missing/foreign cookie, a replay, a link-mode or wrong-tier record with 400 and no redirect, clears the cookie, then either redirects with the recorded failure code or runs the same `finishOAuthCompletion` and sets the client refresh cookie on its own host. A client-tier state with no client surface configured, or whose relay record cannot be stored, is a terminal 400 on the operator host — safe (no token, state consumed) but the only two shapes that cannot clear the start-host cookie, which then expires on its own ten-minute `Max-Age`. A login-CSRF attempt reaches the relay without the attacker's nonce and is refused before any token exists. The relay id is a single-use, browser-bound handle like the IdP code, never a credential.

The wire shape is **closed** and lives in `handlers/oauth_callback_redirect.go`, the only file allowed to build these URLs:

- success: `{spa}/auth/callback?success=true&provider=<google|apple|github|discord>` — the refresh cookie is the only credential; the operator/legacy SPA bootstraps through `GET /v1/auth/session` (operator mux only), while a client-tier SPA bootstraps through `POST /v1/auth/client/refresh-cookie` — mounted per tier by `RegisterTierMountableRoutes` — since `/v1/auth/session` does not exist on the client host;
- failure: `{spa}/auth/callback?success=false&error=<oauth_access_denied | oauth_signup_disabled | oauth_link_disabled | auth.oauth_email_unverified | oauth_provider_unavailable | oauth_login_failed>` — anything else collapses to `oauth_login_failed`; account status and lookup results are never encoded; config uncertainty on this surface is `oauth_provider_unavailable`;
- MFA continuation: `{spa}/auth/callback#requiresMfa=true&mfaToken=<one-shot id>&webauthnAvailable=<bool>` — in the **fragment**, so the five-minute challenge id never reaches a server log, a proxy or a Referer; **no cookie is written** on a partial;
- link mode: `{spa}/user/security?tab=oauth&link=success|failed&provider=<p>[&code=already_linked|duplicate_provider|invalid_userinfo|access_denied|provider_unavailable|internal]` — its own builder, never the login state machine, operator-only.

Every callback and relay response — redirects **and** terminal 400s, whose request URLs carry `state`, `code` or `relay` — sets `Referrer-Policy: no-referrer` and `Cache-Control: no-store` (`writeCallbackRejection` is the only 400 writer). **No callback URL may carry `access_token`, `refresh_token`, `email` or `user_id`** — `TestCallbackURLBuilders_StructuralScan` fails the build on a literal outside the builder file or a forbidden `url.Values` key inside it, and `oauth_callback_flow_test.go` checks every `Location`'s exact parameter names and values. The legacy `success=true&user_id=…&email=…` and the unregistered Huma Apple callback's `access_token=…` are gone.

## HTTP endpoints

Registered from two handlers — `auth_handler.go` for OAuth/session/refresh, `password_handler.go` for password flows.

After the ADR-0003 PR-D D-8 hard cutover every auth route is mounted under one of two audience prefixes — `/v1/auth/operator/...` (operator host mux) or `/v1/auth/client/...` (client host mux). The legacy `/v1/auth/...` paths no longer exist. Use `{tier}` below as a stand-in for `operator` or `client`; both prefixes mount the same routes with audience-correct token issuance and cookie domains.

The OAuth provider callbacks (`/v1/auth/oauth/{google,apple,discord,github}/callback`) and the OAuth-side session poll (`/v1/auth/session`) stay un-prefixed — the IdP has a single registered redirect URI per provider, and the operator AuthHandler dispatches the resulting flow to the matching tier's authService via the signed-state JWT's `tier` claim.

### Public (no auth required)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/auth/{tier}/providers` | List OAuth providers **usable** on this audience — from one strict config read (`OAuthConfigResolver.UsableWebProviders`): the per-surface toggle parsed strictly (absent → `false`, malformed → omitted + WARN naming the key) AND `ProviderStructurallyConfigured` (client ID, redirect URL, secret present; Apple team/key + inline key or readable key file). A document-level failure (missing auth document, repository error, undecryptable stored secret) is **503 `auth.policy_unavailable`**, never an empty list. The unauthenticated login pages drive their social-login buttons off this endpoint; edits resolve on the next request. |
| GET | `/v1/auth/{tier}/policy` | Public slice of admin-managed auth policy: `{registrationEnabled, loginEnabled, passwordMinLength, ...}`. Read by the SPA login + signup pages so kill switches hide the CTA instead of surfacing as a 403 on submit. The pre-existing fields keep their permissive nil-safe reads (deviation 12) — a policy-read failure never affects them. **§4.9 pair:** `passwordLoginEnabled` is the strict, non-null persisted per-surface value on an OK read; it is `null` **only** in the operator emergency case — the policy store is unreadable AND the operator break-glass override is active. `passwordLoginBreakGlassEffective` is **operator-endpoint only** (always `false` on the client tier) and is `true` exactly when the override is configured AND the persisted value is `false` or unreadable, telling the console to render the labelled emergency login form. A read failure **without** the override answers **503** `auth.policy_unavailable` for the whole response — the strict pair must never mask a failure the permissive pair is allowed to shrug off |
| POST | `/v1/auth/{tier}/oauth/login` | Start an OAuth flow. The signed-state JWT carries `tier` so the shared callback dispatches to the matching authService. Refuses with 403 `auth.login_disabled` (kill switch), 403 `auth.oauth_provider_disabled` (per-provider defect) or 503 `auth.policy_unavailable` (document-level), and builds the provider from the same resolved config the check answered with. The stored `RedirectURI` is the configured tier SPA + `/auth/callback`, never the `Origin` header. |
| POST | `/v1/auth/{tier}/google/mobile` | Exchange a Google ID token from a mobile app for an Orkestra session; mints tokens with `aud=tier` |
| POST | `/v1/auth/{tier}/apple/mobile` | Exchange an Apple ID token from a mobile app for an Orkestra session; mints tokens with `aud=tier` |
| GET | `/v1/auth/oauth/google/callback` | Web OAuth callback (raw HTTP). Single shared callback per provider — dispatches to operator or client via `state.tier`; operator/legacy flows complete here, client-tier flows are **relayed** to the client API host under the closed contract below |
| GET | `/v1/auth/oauth/discord/callback` | Web OAuth callback (raw HTTP) |
| POST | `/v1/auth/oauth/apple/callback` | Apple returns form-post, not a redirect (raw HTTP). No dev-only "missing state" fallback: a missing state is a terminal 400 everywhere |
| GET | `/v1/auth/oauth/github/callback` | GitHub web OAuth callback (raw HTTP since PR 2 — the former Huma operation never set the refresh cookie) |
| GET | `/v1/auth/client/oauth/complete` | **Client host mux only.** The relay endpoint that completes a client-tier web login: takes the one-shot relay record, requires the state cookie this host set at start, sets the client refresh cookie, redirects to the client SPA. Not in OpenAPI |
| GET | `/v1/auth/session` | Poll for session after OAuth redirect finishes |
| POST | `/v1/auth/{tier}/register` | Email+password signup. Refuses with 403 `auth.password_login_disabled` when `passwordLoginEnabled{Admin,Client}` is off for **this** surface (strict read; the operator break-glass is invisible here — it never opens registration), and 503 `auth.policy_unavailable` when the policy cannot be established. The very first operator account and `RegisterInitialAdmin` (setup wizard) are the two bootstrap exceptions and bypass the method gate entirely, with no policy read at all |
| POST | `/v1/auth/{tier}/login` | Email+password login. Refuses with 403 `auth.password_login_disabled` per surface, and 503 `auth.policy_unavailable` on an unestablishable policy (a nil policy is an outage, never a legacy allow). The gate sits **after** the `loginEnabledAdmin/Client` kill switch and **before** `GetUserForAuth`, so lockout counters, the rate limiter and the audit trail see nothing and every email — known or unknown — gets the identical answer. Operator-surface only, the boot-time break-glass converts a stored `false` or a failed read into a rescued login; a direct full-token success then emits the `auth.policy.break_glass_used` audit event (audience, user UUID, session id, source IP — never an email) |
| POST | `/v1/auth/{tier}/verify-email` | Consume a verification token |
| POST | `/v1/auth/{tier}/verify-email/resend` | Request a new verification email |
| POST | `/v1/auth/{tier}/forgot-password` | Send a password reset email. Gated the same strict way (403 `auth.password_login_disabled` / 503 `auth.policy_unavailable`), evaluated **before** the user lookup so the outcome cannot depend on account state, and no reset token is minted. Those two policy errors are the ONLY ones the service returns — every account-specific outcome stays swallowed behind the unchanged generic success body, so propagating them is not an enumeration oracle |
| POST | `/v1/auth/{tier}/reset-password` | Consume a reset token and set a new password. **Not** gated by the password-login toggle — redeeming an already-issued token stays open so a reset in flight when the method is turned off can still complete |
| POST | `/v1/auth/{tier}/accept-invite` | Consume an `admin_invite` token: set the user's password **and** mark email verified atomically. Issued by the operator-side admin invite flow (see user CLAUDE.md). Redemption, like `reset-password` and `verify-email`, is **not** gated by the password-login toggle. |
| POST | `/v1/auth/{tier}/refresh` | Refresh using a header-supplied refresh token |
| POST | `/v1/auth/{tier}/refresh-cookie` | Refresh using the `Cookie:` header |
| POST | `/v1/auth/{tier}/logout` | Revoke refresh cookie, invalidate session. Public route — identity comes from `resolveLogoutIdentity`, which requires a **signature-verified** refresh cookie whenever the request context is anonymous (see Key invariants) |
| POST | `/v1/auth/token` | OAuth2 client-credentials grant for service accounts (machine principals). Un-prefixed — operator-tier only, no client-tier equivalent. `{grantType: client_credentials, clientId, clientSecret}` → `{accessToken, tokenType: Bearer, expiresIn}`, no refresh token. Rate-limited like login. See "Service accounts" below |

### Protected (bearer access token required)

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/v1/auth/{tier}/me` | bearer | Return the current authenticated user. The response `avatar` field is resolved server-side via `blob.ResolveAvatarURL` from `User.AvatarSource`: a fresh presigned GET for `uploaded`, the matching `OAuthLinks[i].OAuthData["picture"]` for `oauth_*`, empty for `initials`. The same resolution runs on every other response builder (login, refresh-cookie session-poll, MFA partial responses) so the SPA sees a stable shape regardless of code path |
| PATCH | `/v1/auth/{tier}/me` | bearer | Self-service preference patch. Strictly allowlisted: `language` (BCP-47, oneof=en/it) and `fullName` (1..100 chars). Response mirrors GET /me so the SPA can replace its cached user document without an extra round-trip. Adding a new mutable preference requires extending `UpdateCurrentUserInput` AND honoring it in `UpdateCurrentUser` — the underlying SDK `UpdateUserInput` shape is wider but NOT pass-through |
| POST | `/v1/auth/{tier}/change-password` | `RequireGlobal()` | Self-service password change |
| POST | `/v1/auth/{tier}/mfa/enroll/begin` | `RequireGlobal()` | Start TOTP enrollment — returns `{challengeId, secret, provisioningUri}` |
| POST | `/v1/auth/{tier}/mfa/enroll/confirm` | `RequireGlobal()` | Confirm enrollment with a TOTP code, receive 10 one-shot backup codes |
| GET | `/v1/auth/{tier}/me/mfa` | `RequireGlobal()` | Return `{status, type, backupCodesRemaining}` |
| POST | `/v1/auth/{tier}/me/mfa/remove` | `RequireGlobal()` + `RequireStepUp(5m)` | Remove own factor — step-up middleware demands a <5min MFA proof; request body is empty |
| POST | `/v1/auth/{tier}/mfa/verify` | `RequireGlobal()` | Verify TOTP or backup code; mint a stepped-up access token with `amr:["pwd","otp"]` + `last_otp_at=now` |
| POST | `/v1/admin/users/{userId}/mfa/reset` | `RequireSystemPermission("system.users.mfa_reset")` + `RequireStepUp(5m)` | Admin: delete an **operator** user's MFA factor and restart their enrollment grace. Mounted on the operator host; targets `operator_mfa_factors` |
| POST | `/v1/admin/client-users/{userId}/mfa/reset` | same gates | Tier-aware companion of the above. Same operator-host mount, but routed through `clientMFAHandler` so the reset operates against `client_users` + `client_mfa_factors` |
| POST | `/v1/auth/{tier}/mfa/webauthn/register/begin` | `RequireGlobal()` | Begin enrolling a passkey — returns `{challengeId, publicKey}` (W3C `PublicKeyCredentialCreationOptions`) |
| POST | `/v1/auth/{tier}/mfa/webauthn/register/finish` | `RequireGlobal()` | Finish enrolling a passkey — body `{challengeId, name, attestationResponse}`, returns the public credential metadata |
| GET | `/v1/auth/{tier}/me/mfa/webauthn/credentials` | `RequireGlobal()` | List the user's enrolled passkeys (id, name, transports, createdAt, lastUsedAt) |
| DELETE | `/v1/auth/{tier}/me/mfa/webauthn/credentials/{credentialId}` | `RequireGlobal()` + `RequireStepUp(5m)` | Remove one passkey by base64url-encoded credential id |
| POST | `/v1/auth/{tier}/mfa/webauthn/verify/begin` | `RequireGlobal()` | Begin a step-up assertion using a passkey |
| POST | `/v1/auth/{tier}/mfa/webauthn/verify/finish` | `RequireGlobal()` | Finish a step-up assertion; mints a stepped-up access token with `amr:[..., "otp", "webauthn"]` + `last_otp_at=now` |
| GET | `/v1/auth/{tier}/me/auth-methods` | `RequireGlobal()` | Self-service: aggregate password / MFA / OAuth state of the calling user. Same `models.AuthMethodsView` shape the admin route returns. Drives the `/user/security` page header |
| GET | `/v1/auth/{tier}/me/sessions` | `RequireGlobal()` | Self-service: list active sessions for the caller. `IsCurrent` flag stamped from JWT `sid` |
| DELETE | `/v1/auth/{tier}/me/oauth/{provider}` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: unlink one of the caller's OAuth identities. Service-layer last-credential safeguard rejects with 409 `last_credential` when removing would leave the user with no **usable** login — usable links and a usable password only; 503 `auth.policy_unavailable` when usability cannot be established |
| POST | `/v1/auth/{tier}/me/oauth/link/{provider}` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: start the OAuth flow that adds a new sign-in provider to the caller's account. Returns `{authUrl, state}` — the SPA navigates to `authUrl`; the shared callback redirects back to `/user/security?tab=oauth&link=success\|failed&provider=<x>[&code=already_linked\|duplicate_provider\|invalid_userinfo\|access_denied\|provider_unavailable\|internal]`. The signed-state JWT carries `mode=link` + `linkUserUUID` so the callback binds the new identity to the authenticated user without trusting any query-string parameter. Provider usability is checked the same strict way as the login start. Operator-only: link mode never relays. |
| DELETE | `/v1/auth/{tier}/me/sessions/{sessionId}` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: revoke one session by UUID. Returns 409 `cannot_revoke_current` when the target sid matches the caller's JWT — logout is the right tool for that |
| DELETE | `/v1/auth/{tier}/me/sessions` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: revoke every active session except the calling one. Returns `{revoked: int}` |
| POST | `/v1/auth/{tier}/me/mfa/backup-codes/regenerate` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: replace the user's TOTP backup-code list with a fresh set. Old codes stop working immediately. Returns `{codes: string[]}` exactly once |
| POST | `/v1/auth/{tier}/me/password-confirm` | `RequireGlobal()` | Self-service: reconfirm password to satisfy `RequireStepUp` when no MFA factor is enrolled. Returns a fresh access token with `amr += "reauth"` + `last_otp_at = now`. Refuses with 409 `auth.password_confirm_unavailable` for users with any MFA factor (must use the MFA path), no password (pure-OAuth account), or a surface whose password method is disabled; 503 `auth.policy_unavailable` when that policy can't be read |
| GET | `/v1/admin/users/{userId}/auth-methods` | `RequireSystemPermission("system.users.admin")` | Admin: aggregate password / MFA / OAuth state of an operator user. Drives the Authentication Methods card on `/admin/user/profile/:userId`. Read-only |
| POST | `/v1/admin/users/{userId}/send-password-reset` | `RequireSystemPermission("system.users.password_reset")` | Admin: trigger the standard password-reset email for an operator user. Operator-side companion of the existing client-user route. Answers **409** `auth.password_login_disabled` when the target's surface rejects the method — minting a reset link there would revoke the target's sessions and leave an unusable password — and 503 `auth.policy_unavailable` on an unestablishable policy. Break-glass never opens it |
| POST | `/v1/admin/users/{userId}/resend-verification` | `RequireSystemPermission("system.users.email_verify_resend")` | Admin: re-emit the email-verification message. Idempotent — already-verified users return 200 with no action |
| DELETE | `/v1/admin/users/{userId}/oauth/{provider}` | `RequireSystemPermission("system.users.oauth_unlink")` + `RequireStepUp(5m)` | Admin: unlink a Google/Apple/GitHub/Discord identity. Service-layer safeguards reject self-action (409 `self_action`) and last-**usable**-credential lockout — sole usable link with no usable password returns 409 `last_credential`; uncertainty returns 503 `auth.policy_unavailable` |
| GET | `/v1/admin/service-accounts` | `RequireSystemPermission("auth.service_accounts.read")` | List service accounts with live active-credential counts. Never returns secrets |
| GET | `/v1/admin/service-accounts/{id}` | `RequireSystemPermission("auth.service_accounts.read")` | Get one service account plus its full credential history (active and revoked) |
| POST | `/v1/admin/service-accounts` | `RequireSystemPermission("auth.service_accounts.manage")` + `RequireStepUp(5m)` | Create a service account (`{name}`) — mints the `kind=service` user row plus its first credential. Response carries `clientId` + `clientSecret` exactly once. `201` |
| PATCH | `/v1/admin/service-accounts/{id}` | `RequireSystemPermission("auth.service_accounts.manage")` + `RequireStepUp(5m)` | Rename and/or enable/disable a service account. Only non-nil fields are applied |
| POST | `/v1/admin/service-accounts/{id}/credentials` | `RequireSystemPermission("auth.service_accounts.manage")` + `RequireStepUp(5m)` | Issue a rotation credential. Enforces the max-two-active cap (409 on a third). Response carries the plaintext secret exactly once. `201` |
| DELETE | `/v1/admin/service-accounts/{id}/credentials/{credentialId}` | `RequireSystemPermission("auth.service_accounts.manage")` + `RequireStepUp(5m)` | Revoke a credential. Not idempotent — revoking an already-revoked credential surfaces the same not-found outcome as an unknown id. `204` |

And a public endpoint that completes a login after a partial response:

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/v1/auth/{tier}/mfa/login/verify` | none (uses `challengeId`) | Complete a login by validating TOTP/backup; mints full token pair with `amr:[source,otp]`. Re-checks the password policy first — see the completion re-check below |
| POST | `/v1/auth/{tier}/mfa/webauthn/login/begin` | none (uses `loginChallengeId`) | Begin a passkey assertion to satisfy a paused login |
| POST | `/v1/auth/{tier}/mfa/webauthn/login/finish` | none (uses both challenge ids) | Finish a passkey assertion; mints full token pair with `amr:[source, otp, webauthn]`. Same completion re-check |

**Completion re-check (both endpoints).** A login paused for MFA must still be an *allowed* login when its second factor lands — otherwise turning `passwordLoginEnabled{Admin,Client}` off leaves every in-flight password login able to complete for the next five minutes. `MFAChallenge` therefore carries `Audience` (`"operator"`/`"client"`, stamped by both producers) and `BreakGlassUsed` (whether the *initial* check was rescued by the operator break-glass — audit context only, never a standing permission), and `recheckPasswordChallenge` (`handlers/mfa_handler.go`, shared by `LoginVerify` and `LoginFinish`) re-evaluates `PasswordLoginDecision` **before** the factor is verified — so a disabled login can neither burn attempt budget nor probe factor state:

- a challenge that is not password-sourced (`LoginMethod == "password"` ∨ `SourceAMR ∋ "pwd"`) is **untouched** — OAuth-sourced logins complete regardless of the password toggle;
- **disabled** → the challenge is atomically consumed, **403** `auth.password_login_disabled`;
- **policy outage** (read failure, or a nil policy service — missing wiring is an outage, never a pass) → **503** `auth.policy_unavailable` with the challenge **retained**, so a transient failure is retryable inside the original TTL;
- **empty or unknown `Audience`** → a pre-toggle in-flight challenge: consumed, **401** with the generic "invalid or expired challenge" wording. Rollout therefore waits one 5-minute challenge TTL after deploy before exposing the switch;
- **allowed** → the flow proceeds unchanged; the one-winner `Consume` stays where it was. The completion that wins it emits exactly one `auth.policy.break_glass_used` when either this decision or the challenge's stamped `BreakGlassUsed` was a rescue.

`change-password` and the self-service MFA routes are deliberately global (no org context) because they're user-level flows.

### MFA implementation notes

- **Privilege policy** lives in `services/mfa_policy.go`. `RoleRequiresMFA(user, memberships)` returns true for `super_admin`, `administrator`, and any org membership carrying `org_owner`/`org_admin`. `developer` is intentionally excluded — its prod downgrade to read-only covers the risk.
- **Grace period defaults to 7 days** (legacy `MFAEnrollmentGraceWindow` constant; runtime value comes from `mfaEnrollmentGraceDays` on the MFA policy tab). A privileged user logging in without a factor has `User.MFAGraceStartedAt` stamped on that login (idempotent via `UserProvider.StartMFAGraceIfUnset`). Past the window, login returns 403 `mfa_enrollment_required`. Granting a privileged role via authz `CreateBinding` also eagerly starts the clock so the configured window begins at promotion, not next login. The master `mfaEnabled` flag short-circuits the requirement entirely without deleting existing enrollments.
- **Login state machine** (`PasswordAuthService.completeLogin`; OAuth mirrors via `AuthService.evaluateMFAForOAuth`): (a) non-privileged → full token with `amr:["pwd"]`/`["oauth"]`; (b) privileged with factor → partial 200 response `{requiresMfa: true, mfaToken: <challengeId>}` and no access token — client must call `/v1/auth/mfa/login/verify`; (c) privileged without factor within grace → full token + `mfaEnrollmentRequired:true` + `mfaGraceExpiresAt`; (d) privileged without factor past grace → `ErrMFAEnrollmentRequired` → 403.
- Factor secrets are AES-256-GCM encrypted with `MFA_SECRET_ENCRYPTION_KEY` (falls back to `OAUTH_TOKEN_ENCRYPTION_KEY` for single-key dev setups). Backup codes are argon2id hashed via the existing `PasswordService`.
- Challenge state lives in Redis under `mfa:challenge:<uuid>` with a 5-minute TTL; after 5 failed verifications the challenge is deleted. Login challenges additionally carry `DeviceID`/`Platform`/`IPAddress`/`Fingerprint`/`SourceAMR` so the public login-verify endpoint can mint a token pair without re-posting the user's password, plus `Audience` + `BreakGlassUsed` for the completion re-check above.
- **TOTP replay guard** — `MFAFactorDoc.LastUsedStep` advances via an atomic `AdvanceLastUsedStep` CAS in the repo (`$or: lastUsedStep < step OR $exists:false`). A captured code cannot be used twice within its 30-second window, whether by the same caller or a concurrent one.
- `JWTClaims.AMR` (RFC 8176) and `JWTClaims.LastOTPAt` are emitted `omitempty` so pre-Block-A tokens still validate. Password login sets `amr:["pwd"]`, OAuth `amr:["oauth"]`, MFA verify sets `amr:[source,"otp"]` + `last_otp_at=now`, and the password-confirm bypass sets `amr:[source,"reauth"]` + `last_otp_at=now`. The local `"reauth"` marker is accepted by `amrSatisfiesMFA` so `RequireStepUp` treats it as a satisfied proof — `ConfirmPassword` is the gatekeeper: it refuses to mint a `reauth` token for users with any enrolled factor, so the marker can never bypass MFA-required scenarios.
- `RoleMiddleware.RequireMFA()` is applied to the routes whose abuse MFA exists to prevent: authz role + binding mutations (create/update/delete-role, create/delete-binding), tenant scoped mutations (update/delete-org, update-plan, remove-member, create-invite), and module config writes (`update-module`, `update-module-environment`, `set-active-environment`). Read paths stay open. **The gate honours the `mfaEnabled` master switch**: when MFA is globally off (`AuthPolicyService.MFAEnabled` → false) `RequireMFA()` passes through, mirroring `MFARequired`. Without this a never-enrolled operator on an MFA-off install is deadlocked — their password-only token (`amr=["pwd"]`) can never satisfy the gate, so they can't perform the very module/config writes (e.g. enabling an addon, configuring SMTP) needed to run the platform. The switch is read via the wired `StepUpPolicy` (nil-tolerant: when no policy is wired, the gate keeps its legacy unconditional behaviour). `RequireStepUp` is **not** relaxed this way — it keeps demanding a fresh proof and instead offers the `password_confirm_required` reauth fallback for no-factor users.
- `RoleMiddleware.RequireStepUp(maxAge)` is a stricter variant applied to catastrophic / irreversible actions (currently `POST /v1/auth/me/mfa/remove`, `POST /v1/admin/users/{id}/mfa/reset`, the self-service OAuth link/unlink + session revoke/revoke-all, and backup-code regeneration). It checks both that `amr` contains an MFA marker AND that `last_otp_at` is within `maxAge` of now — a session-long MFA proof is not enough. The middleware emits **four** distinct envelopes so the frontend can pick the right modal without a second round-trip:
  - **`code="step_up_required"`** (401) — user has an MFA factor; ask for a fresh OTP / passkey. The global `StepUpModal` drives the user through `/mfa/verify` (or WebAuthn assertion) and replays.
  - **`code="password_confirm_required"`** (401) — user has **no** MFA factor enrolled, the policy doesn't require them to, AND the password is still an accepted credential for the token's audience. The `PasswordConfirmModal` posts the password to `/v1/auth/{tier}/me/password-confirm`; the response mints a fresh access token with `amr += "reauth"` + `last_otp_at = now`, which the middleware then accepts via `amrSatisfiesMFA` (the `"reauth"` marker is treated as a satisfied proof).
  - **`code="mfa_enrollment_required"`** (403) — the user's role obligates MFA but they haven't enrolled, **or** the per-surface password method is disabled (`passwordLoginEnabled{Admin,Client}` = false) so no reconfirm can be offered. No bypass — the SPA nudges them to enroll a factor.
  - **`code="auth.policy_unavailable"`** (503) — the password-method policy could not be evaluated on the no-factor branch: no `StepUpPolicy` is wired, or `PasswordReauthAllowed` returned an error. Retryable, and deliberately **not** a step-up prompt — an outage must never be dressed up as a user obligation (`mfa_enrollment_required`) or as a modal the user cannot satisfy.

  **The `service` audience reaches this 503, and that is a real behaviour change — not an unreachable case.** The operator mux is mounted for **both** `operator` and `service` audiences (`cmd/server/main.go`), the enrollment lookup falls back to the operator `mfa_factors` repo for any audience that isn't `client` and so answers "no factor" for a service principal (`auth/module.go`), service-account tokens are minted by `GenerateAccessToken` with **no** `amr` and **no** `last_otp_at` (`services/service_account_service.go`) so `amrSatisfiesMFA` is always false and the gate always dispatches, and the service user's role is `guest` so `roleRequiresMFA` is false. Control therefore reaches `PasswordReauthAllowed(ctx, "service")`, which the adapter refuses as an unknown audience → 503. A service account hitting a `RequireStepUp` route (`POST`/`PATCH /v1/admin/service-accounts`, credential issue/revoke, `/v1/admin/users/{id}/mfa/reset`, …) used to get **401 `password_confirm_required`** and now gets **503 `auth.policy_unavailable`**. Neither outcome ever succeeded — service accounts carry no password hash, so the reconfirm endpoint would refuse them anyway — so this is not a capability or security regression, but it *is* reachable and the status code did change. There is **no** service-kind short-circuit in `internal/shared/middleware/`: giving non-`operator`/`client` audiences a definitive 4xx ("step-up is not available to this principal") is a contract decision the spec has not made, tracked as a follow-up for the spec owner rather than invented here.

  The branching is fed by `SetMFAEnrollmentLookup` (per-tier `MFAFactorRepository.FindByUserAndType` for TOTP + WebAuthn), `SetStepUpPolicy` (the live `*AuthPolicyService`), and `SetUserProvider` (so a stale role on the JWT doesn't shadow a fresh policy check). Wired in `cmd/server/main.go` post-InitAll. Any **enrollment-lookup** error fails closed to the legacy `step_up_required` path — a degraded Mongo must never silently weaken the gate; a **policy** error fails closed to the 503 above. `middleware.StepUpPolicy.PasswordReauthAllowed(ctx, audience string)` is the seam (`*AuthPolicyService.PasswordReauthAllowed` adapts strict `PasswordLoginEnabled` onto the middleware's string audience); its doc comment on the interface is the contract — there is no `middleware/CLAUDE.md`. The operator break-glass is **invisible** to it: a temporary override is not a durable login method, so it must not keep the password modal on offer.

- **Password reconfirm endpoint** — `POST /v1/auth/{tier}/me/password-confirm` lives on `PasswordAuthHandler` (`handlers/password_handler.go`) and is mounted under `RequireGlobal()` only (no step-up gate; it's the bypass). The service-layer `PasswordAuthService.ConfirmPassword` refuses with `ErrPasswordConfirmUnavailable` → 409 `auth.password_confirm_unavailable` when the user has no password (pure-OAuth account), when the per-surface password method is disabled (`passwordLoginEnabled{Admin,Client}` = false — a credential the surface refuses cannot prove presence either, so it takes the *same* 409 branch as "no password hash"), or when the user has any MFA factor enrolled (defensive — a crafted direct call must not be able to downgrade an MFA-required user). The method-policy read is **strict**: the operator break-glass is invisible here, and a policy that cannot be read returns `ErrAuthPolicyUnavailable` → 503 `auth.policy_unavailable` via `mapPasswordError` rather than guessing. A `PasswordAuthService` built without `Policy`+`Audience` therefore 503s this endpoint — production wiring always supplies both, and a consumer-package test can use `services.NewAuthPolicyServiceForTest`. Audit: emits `auth.password.reconfirmed` on success.
- **Session revocation list** — Redis-backed set at `auth:revoked:session:<sid>` checked on every authenticated request by both `AuthMiddleware` and the lightweight public-key-only `JWTValidator` (both satisfy `module.RoleMiddleware`). Populated on logout + change-password; payload is the reason string for operator debugging. Entries auto-expire after a **fixed** 24h + 1min — the maximum access-token lifetime the platform permits, plus clock skew — never a value derived from the live `accessTokenTTL`. Sizing the entry from the current policy value strands tokens on both sides of a policy change: raising the TTL leaves long tokens uncovered, lowering it expires the entry while tokens minted under the old value are still valid. `NewJWTService` clamps every effective access-token lifetime to 24h so the window is always sufficient. ADR-0017 D5. Fails open on Redis errors — a degraded Redis must not lock every user out. Logout invalidates the current sid only; `allDevices=true` still relies on refresh-token revocation (per-user-generation counter is a follow-up).
- **Grace countdown on `/v1/auth/me/mfa`** — response now carries `requiresMfa` + `graceExpiresAt` computed from the user record + JWT memberships, so the frontend banner/countdown can render without relying on the one-shot login response.
- **WebAuthn / passkeys** — second-factor enrollment under `services/webauthn_service.go` + `handlers/webauthn_handler.go`. Library: `github.com/go-webauthn/webauthn`. Configuration: `WEBAUTHN_RP_ID` (eTLD+1 host, no scheme/port) + `WEBAUTHN_RP_ORIGINS` (comma-separated full URLs). Both env vars are optional — if either is missing the module derives them from `FRONTEND_URL` (eg. `http://localhost:8080` → `rpId=localhost`, `origins=[http://localhost:8080]`); if neither resolves, WebAuthn is disabled and the endpoints don't mount. Credentials live as an embedded `webauthnCredentials[]` array on the same `*_mfa_factors` row (one row per user with `type=webauthn`); the (userUuid,type) unique index naturally allows a user to enroll both TOTP and passkeys. Login/step-up via passkey sets `amr=[..., "otp", "webauthn"]` so existing step-up middleware accepts the proof. The partial login response carries `webauthnAvailable: bool` so the verify page can offer the passkey button alongside the code field.

### Self-service security surface

`handlers/self_user_auth_handler.go` hosts six routes under
`/v1/auth/{tier}/me/...` that power the **`/user/security`** page on
`frontend-admin`. Reads (`auth-methods`, `sessions`) are gated by
`RequireGlobal()` only; destructive endpoints (OAuth unlink, session
revoke, revoke-all, backup-codes regenerate) are gated by
`RequireGlobal()` + `RequireStepUp(5m)` so a fresh MFA proof is
required for credential / session removal. Backup-codes regeneration
lives on `MFAHandler` for cohesion with the rest of the MFA surface.

The service layer reuses the lockout helper extracted from
`AdminUnlinkOAuth` — same usable-credential arithmetic, same
`SetProviderUsability` prelude, same 503 on uncertainty — so a
self-unlink that would leave the user with no usable login method is
rejected with 409 `last_credential`. The
companion **link** endpoint (`POST /me/oauth/link/{provider}`) mints a
signed-state JWT with `Mode=link` + `LinkUserUUID=caller` so the
single shared OAuth callback can bind the returning identity to the
authenticated user without minting fresh tokens. The callback rejects
identities already claimed by another account (`ErrOAuthLinkClaimedByOther`)
or duplicates of an existing provider on the same user
(`ErrOAuthLinkAlreadyExists`); both surface as
`/user/security?tab=oauth&link=failed&code=<reason>` redirects.
Session revocation is the same three-step coordinated op the admin
paths use:
`refreshTokenRepo.RevokeTokensBySession` → flip
`AuthSession.IsActive=false` → push the sid into the Redis revocation
set (`auth:revoked:session:<sid>`) so middleware kills in-flight
access tokens on the next request. The "revoke all except current"
endpoint exists so the caller's response can complete without a
mid-flight 401. Each successful action emits
`slog.Info("self_auth_action", event=..., userUUID=...)`. Persistent
audit rows are tracked under the same `RecordSecurityEvent` follow-up
as the admin paths.

### Admin user-auth surface

`handlers/admin_user_auth_handler.go` hosts four operator-tier admin endpoints under `/v1/admin/users/{userId}/...` that power the **Authentication Methods** card on `/admin/user/profile/:userId`. Each route is in its own router group with its own permission gate; only the unlink route adds `RequireStepUp(5m)`.

- `GET .../auth-methods` — aggregates `User.PasswordHash` presence + `PasswordUpdatedAt`, MFA factor rows from `operator_mfa_factors`, OAuth identities from `User.OAuthLinks`, and email-verification + last-login state into one `models.AuthMethodsView`. Backed by `AuthService.GetUserAuthMethods`. Read-only — gated by `system.users.admin` rather than a new permission since reading is incidental to user administration. **The view splits the password concept:** `hasPasswordSet` is hash presence only, `passwordUsableForLogin` is `hasPasswordSet ∧` the strict per-surface `passwordLoginEnabled{Admin,Client}` policy for this user's tier. "Set / change / last updated" UI reads `hasPasswordSet`; unlink and reset decisions read `passwordUsableForLogin`. `hasUsablePassword` survives one release as a **deprecated alias of `hasPasswordSet`** (not of usability) for wire compatibility. The policy read is strict and fails the whole call with 503 `auth.policy_unavailable` — an outage must not render an unlock the backend would refuse.
- `POST .../send-password-reset` — proxies to `iface.AdminAuthInviter.AdminTriggerPasswordReset` on the operator-tier `*PasswordAuthService`. No step-up — the action emits a notification, it does not read or mutate a credential. `mapAdminInviterError` maps the per-surface method gate to **409** `auth.password_login_disabled` and an unestablishable policy to **503** `auth.policy_unavailable`. The client-user twin (`POST /v1/admin/client-users/{id}/send-password-reset`, user module `mapInviteErr`) answers identically, matching the **`iface`** sentinels (`iface.ErrPasswordLoginDisabled` / `iface.ErrAuthPolicyUnavailable`) with `errors.Is` — by identity, never by message, and the user module must not import `auth/services`. The auth package's `services.Err*` names are aliases of those same vars, so both halves are one identity.
- `POST .../resend-verification` — same pattern via `AdminResendVerification`. Idempotent (200 with no action when already verified).
- `DELETE .../oauth/{provider}` — backed by `AuthService.AdminUnlinkOAuth`. Service-layer safeguards: rejects `actorUUID == targetUUID` (`ErrAdminSelfAction` → 409 `self_action`) and rejects the operation when it would leave the user with no **usable** login method (`ErrLastCredentialRemoval` → 409 `last_credential`). The guard counts usable credentials, not active rows: `locked := targetUsable ∧ (¬passwordUsable ∨ PasswordHash == "") ∧ remainingUsable == 0`, where `targetUsable` is the target link being active **and** its provider usable, `remainingUsable` counts the other active links whose provider is usable, and `passwordUsable` is the strict per-surface `passwordLoginEnabled{Admin,Client}` read (break-glass invisible). Consequences: a link whose provider is disabled or structurally unconfigured is **removable** and never satisfies the guard on its own; a password hash the surface refuses does not rescue a sole usable link. Both `AdminUnlinkOAuth` and `SelfUnlinkOAuth` precompute usability for every active link through `usableProvidersForLinks` **before** mutating — it calls the `SetProviderUsability` seam (wired in `module.go` over `OAuthConfigResolver.OAuthWebProviderUsable`, one closure shared by both tier bundles). Missing wiring or any policy/provider-config uncertainty refuses with `ErrAuthPolicyUnavailable` → 503 `auth.policy_unavailable` rather than counting a link it cannot vouch for. Step-up gated because the action removes a credential.

Each successful action emits three audit lanes in parallel: (1) `slog.Info("auth_security_event", event=…)` for log shipping; (2) one row into `auth_security_events` via `securityEventRepo.Insert` (the auth-private audit collection — Phase 2.1); (3) one row into `compliance_audit_events` via the compliance `iface.AuditSink` when the compliance addon is enabled (Phase 6 of the /admin/users hardening). The compliance lane is driven by `authEventComplianceAction` (`services/auth_service.go`), which maps the internal event-type strings (`admin_password_reset_sent`, `admin_verification_resent`, `admin_oauth_unlink`, `admin_mfa_reset`, plus `self_oauth_unlink` / `self_oauth_link` / `self_session_revoke[_all]`) onto the dotted compliance vocabulary (`auth.password.reset_requested` / `auth.email.verify_resend` / `auth.oauth.unlinked` / `auth.mfa.reset` / `auth.oauth.unlinked.self` / …). Unmapped event-types still hit slog + `auth_security_events` but don't get duplicated to compliance — adding an event to the SOC2 view is a deliberate opt-in via the mapping function.

## Service accounts

Machine principals (CI jobs, integrations, and other automated callers) are
Tier-1 operator user rows with `Kind: iface.UserKindService` — see
[ADR-0014](../../../../docs/adr/0014-service-accounts-client-credentials.md)
for the design rationale. `services/service_account_service.go` owns the
account + credential lifecycle and the client-credentials grant;
`handlers/service_account_admin_handler.go` (the six admin routes) and
`handlers/service_token_handler.go` (`POST /v1/auth/token`) are thin HTTP
bindings over it.

Invariants:

- **Interactive flows reject `Kind == "service"`.** Password login
  (`services/password_auth_service.go`), every OAuth flow
  (`services/auth_service.go::GenerateEnhancedTokenPair`), and all three
  refresh read paths (`RefreshTokensWithRiskAssessment`,
  `PeekRefreshToken`, `MintAccessTokenFromRefresh`, all in
  `services/auth_service.go`) fail closed on a service principal. The
  `POST /v1/auth/token` client-credentials grant is the **only** path that
  mints a token for a service account.
- **Privileged system roles are unassignable to service accounts.** The
  `user` module's `serviceAccountRoleAllowed` guard
  (`internal/core/user/handlers/user_handler.go`) refuses
  `super_admin`/`administrator` for any `Kind == "service"` user, on both
  create and update, and fails closed — refuses the assignment — even when
  the pre-read needed to classify the target account is unavailable.
- **Secrets are argon2id-hashed via the existing `PasswordService`, shown
  exactly once, capped at two active credentials per account.** The
  plaintext client secret (`sas_`-prefixed) is returned only in the
  create/issue response body and is never persisted or logged. A third
  `IssueCredential` call against an account already holding two active
  credentials returns 409 — the count-then-insert cap is documented
  best-effort (not atomic), not a security boundary.
- **Tokens carry `aud: "service"`; the operator mux accepts
  `{operator, service}`.** `RequireAudience` is variadic set-membership
  (`shared/middleware/audience.go`); the client host mux is unchanged
  (`{client}` only) — service accounts act on the Tier-1 operator surface
  exclusively.
- **Disabling an account stops new grants instantly; permissions still
  resolve per request.** `Grant` refuses a disabled account's credentials
  immediately. A token already minted before the disable remains valid for
  up to its access-token TTL (default 15m — see "JWT payload shape"
  above), but because permissions are never embedded in the token,
  unbinding or disabling a service account takes effect on its
  authorization immediately regardless of the bearer token's remaining
  lifetime.

## Service contract

No single interface is exposed from this module — its concrete services are consumed from the registry by type. The one published interface is:

- **`iface.JWTProvider`** (`pkg/sdk/iface/interfaces.go:56-62`) — just `GenerateAccessToken(user *User) (string, error)`. Consumed by the dev module to mint test tokens.

Everything else (`services.AuthService`, `services.JWTService`, `services.PasswordService`, `services.PasswordAuthService`) is fetched with `MustGetTyped[*services.X]` by `cmd/server/main.go` or by middleware. This is intentional — the surface is too broad to pin as an interface today.

## Key invariants

- **JWT payload shape.** Access tokens carry: `sub`, `email`, `srole` (the global system role), `memberships` (an array of `{orgId, orgName, orgSlug, roles[]}` fetched via `TenantProvider.ListUserMemberships` at issue time). **Permissions are not embedded** — they are resolved per-request by middleware calling `authz.HasPermission`. This is the most important thing to remember about the authentication architecture: roles are coarse-grained and cached in the JWT, permissions are fine-grained and resolved fresh.
- **Tenant fallback selection.** `jwt_service.go::loadMemberships` picks `TenantFallbackID`/`ActingTenantID`/`ActingTenantKind` — the token's fallback tenant, stamped into both claims together — in this order:
  1. The **operational platform default** (`iface.DefaultTenantProvider`, the tenant module's `tenant_defaults` pointer — see [tenant/CLAUDE.md](../tenant/CLAUDE.md#default-tenant-invariants)), when `s.defaultTenants` is wired AND it names one of the memberships just loaded. The default grants nothing by itself — it is only ever considered among memberships already validated for this user; a non-member never receives it. Membership sourcing and the operator `X-Tenant-ID` override still apply downstream in middleware exactly as before.
  2. Otherwise, the first **owned** membership (`IsOwner`), in the deterministic order `tenant/repository.go::ListMembershipsByUser` returns (`joinedAt` ascending, then the membership's tenant identifier — persisted field `tenantId` — ascending as a stable tie-break; this fixed the pre-6.2 bug where the fallback depended on MongoDB's undefined natural order).
  3. Otherwise, the first membership in that same deterministic order.
  The embedded `memberships` claim (`mbr` on the wire) uses that identical repo order and is never re-sorted in `loadMemberships` — selection and the embedded list can never disagree about ordering. **Failure never blocks issuance**: a nil provider, a "no default assigned" `(nil, nil)` response, or a provider error all fall straight through to rule 2 — a transient platform-default read must never stop a user from logging in. Only the **operator-audience** JWT service is wired with the provider (`auth/module.go`, right after `operatorJWT.SetTenantProvider(...)`) — the service-audience and client-audience services never consult it, so a Tier-2 client-portal token can never be influenced by the internal platform default. **The JWT is a snapshot**: transferring the platform default does not revoke or rewrite already-issued tokens — they keep whatever fallback was selected at issuance until refresh, reauthentication, or expiry; only a newly minted or refreshed token sees the new default. **Naming**: the Go field is `models.JWTClaims.TenantFallbackID` (legacy name: `DefaultTenantID`, renamed to disambiguate the per-token fallback from the global platform-default pointer), but the wire claim key stays `dtid` for backward compatibility with already-issued tokens and existing clients — `claimsToMap`/`mapToClaims` (`jwt_service.go`) are the only two places that translate between them.
- **First-user heuristic.** `password_auth_service.go::Register` (`:116-121`), `RegisterInitialAdmin` (`:177`), and `auth_service.go::OAuth register` all check `GetUserCount(ctx, nil) == 0` and assign `super_admin` to the first account created on a fresh install. The setup wizard's `POST /v1/setup/admin` uses `RegisterInitialAdmin` which also bypasses email verification. The setup wizard's `POST /v1/setup/admin` creates the admin only — it no longer bootstraps an internal tenant (ADR-note: zero-tenant installs are supported).
- **Email verification is gated by `AUTH_REQUIRE_EMAIL_VERIFICATION`.** `true` in production, `false` elsewhere. When true, signup returns 503 with `ErrNotificationDown` if the notification sender is missing or reports `iface.IsConfiguredForCategory(ctx, notifier, "auth.verify_email") == false` — every auth pre-flight asks for the category it is about to send (ADR-0019 D7); a fork's sender without the companion interface falls back to `IsConfigured`. `RegisterInitialAdmin` (setup wizard path) bypasses verification entirely because the wizard runs before SMTP is configured. Verification and reset sends carry `auth.verify_email` / `auth.reset_password` as their category (aligned in ADR-0019 PR 2; before that they carried the bare token-purpose strings).
- **A provider-verified email is mandatory before any email lookup.** Every provider populates `OAuthUserInfo.EmailVerified` from its own signal (Google `email_verified`; Apple `email_verified`, which Apple types as **String or Boolean** — read by `getBoolOrStringClaimFromMap`, which accepts a JSON bool *and* `"true"`/`"false"`, since reading the string shape as false would lock every unlinked Apple identity out of signup and linking; Discord `verified`; GitHub **only** from `/user/emails` — primary verified first, then any verified). A failing `/user/emails` — transport error, non-200 (401 on a revoked token, 403/429 on rate limits), unreadable or non-JSON body — is a **provider error** (`ProviderError` operation `user_emails`, carrying the HTTP status where there is one), never a silent downgrade: only a 200 that carries no verified address falls back to the public-profile `email`, which is a free-text field and is therefore never marked verified by assumption. The handlers forward it as `email_verified` in the `userInfoMap`. `HandleOAuthCallbackWithLinking` decides three things **before** `GetUserByEmail` for an identity with no existing `(provider, providerID)` link, in this order: the bit must be `true` (else `ErrOAuthEmailUnverified` → 403 `auth.oauth_email_unverified` / web `error=auth.oauth_email_unverified`, identically whether or not a local account exists — it must not become an account-existence oracle); the auto-link policy must be establishable (strict `OAuthAutoLinkByEmailEnabled`: read failure, missing document or malformed value → `ErrAuthPolicyUnavailable` → 503 before lookup, link or token issuance; a nil policy is the same outage, never the legacy "always link"); only then the lookup, after which a found account auto-links only when the policy is on, and a new account lands `EmailVerified=true` without re-asking. An existing provider-ID link logs in as today regardless of the bit (`oauth_inactive_user_test.go` pins it). Regression tests: `gates_test.go` `TestOAuthCallback_NewUser_RequiresVerifiedEmail`, `_UnverifiedEmail_SameAnswerForKnownAndUnknownAccount`, `_AutoLinkPolicyUnavailable_FailsClosedBeforeLookup`, `github_oauth_service_test.go`.
- **Refresh tokens rotate on every use with family detection.** Each login mints a fresh `FamilyID`; every subsequent rotation preserves it via `RotateWithFamily` (atomic CAS on `{isRevoked:false}`). Old rows are marked `revokedReason="rotated"` with `succeededBy` pointing at the successor so the chain is walkable. Reuse of a rotated token — or CAS-loss on concurrent rotation — triggers `RevokeFamily`: every active row in the lineage is revoked with `revokedReason="replay_detected"`, a structured `slog.Warn` fires, and callers get `ErrRefreshTokenReplay` → 401 with body `{code:"refresh_token_replay"}`. **Except inside `RefreshRotationGrace` (10s).** Several tabs of one app share a login, so their access tokens expire at the same instant and each posts the same cookie; exactly one wins the CAS. Answering every loser with replay revoked the family — the winner's fresh successor included — and forced a full re-login about once per access-token lifetime. So when the presented row is `rotated`, was revoked within the grace window, **and the family carries no revocation fence** (`FamilyRevoked`), the refresh returns `ErrRefreshRotationRaced` → **409** `{code:"refresh_rotation_raced"}` and touches nothing: no family revocation, no credentials. The family fence is the discriminator — a racing sibling runs against a healthy family, a replay that already tripped detection does not. The trade is deliberate: an attacker replaying inside the window gets a retry hint instead of tripping the kill, but gains nothing, since progress still needs the successor cookie only the legitimate client holds. Outside the window, or once the fence exists, detection is unchanged. Clients retry **once** on 409 — `frontend-admin` additionally serialises rotation across tabs with a Web Lock (`orkestra:auth-refresh`), which prevents most races from reaching the backend at all. Pre-Block-C rows have empty `FamilyID`; `RevokeFamily("")` is a no-op guard so a stray pre-Block-C replay doesn't wipe unrelated sessions. No refresh row may be deleted while its token could still pass temporal validation, regardless of revocation state — an unexpired rotated row is exactly what replay detection matches against. Once `expiresAt` is past, replaying it cannot mint credentials and the row may be swept. `CleanupRevokedTokens` was deleted in ADR-0017 D7: revocation age alone is never a safe deletion criterion, and it was wrong across a `JWT_REFRESH_TOKEN_EXPIRY` change between restarts.
- **Refresh-token retention is an elected, self-draining sweep.** `AuthModule.Start` runs one loop covering both tiers. A Redis lease (`auth:maintenance:token-sweep`, 2m TTL, renewed every 30s with Lua compare-and-expire) elects **one scheduler across replicas** — held across the idle wait too, so 5,000 rows/tier/cycle is a cluster-wide bound, not a per-replica multiplier. The cadence adapts to the `hasMore` bit the previous batch reported: 5 minutes while draining, 6 hours once dry. Watch `orkestra_auth_token_sweep_backlog_estimate{tier}` reach zero; no manual intervention or interval change is expected. **Every lease failure is a step-down, never an exit.** A failed acquire, a failed renew, and a renew that reports someone else owns the key all land in the same follower state: log one bounded warning, sweep nothing, re-contend in five minutes. Exiting the loop on any of them would end retention for the life of the process — nothing calls `Start` again — and the most ordinary instance of that is a Redis restart, which comes back *healthy* and answers the next renew with `not_owner` rather than an error. Authentication is never affected on any of these paths. The next pass is scheduled as an **absolute deadline**, not a duration recomputed each time the loop wakes: the renew ticker wakes it every 30s, so rebuilding the timer from the full interval on each wake would starve the sweep forever while every log line still looked healthy.
- **Refresh-family replay fencing is durable.** The tier-scoped `*_refresh_token_families` row records a family revocation independently of the token rows. It closes the standalone-Mongo race where replay revocation lands after a rotation CAS but before its successor insert: the late successor is fenced and cannot remain active. Do not replace this with process-local coordination.
- **A session has one canonical SID.** Token issuance generates one random session UUID *before* either JWT is signed. The access JWT `sid`, refresh JWT `sid`, `RefreshTokenDoc.SessionUUID`, `AuthSessionDoc.UUID`, and returned `TokenResponse.SessionID` must all equal it. Refresh rotation and MFA/WebAuthn login completion preserve that SID; never derive it from time or device data, and never mint a second SID while completing a paused login.
- **A session has a maximum age, and both refresh paths enforce it.** `sessionAbsoluteTTL` (default 30d, empty disables) bounds total session age from `session.StartedAt`, independently of activity — the refresh TTL is the *idle* timeout, not the cap. `RefreshTokensWithRiskAssessment` **and** `MintAccessTokenFromRefresh` both call `sessionWithinAbsoluteCap`: `/session` mints without rotating, so enforcing on the rotation endpoint alone would let a bootstrap-only client hold a session open forever. Reaching the cap is a **logout** — refresh tokens revoked, session inactive, sid denylisted — not a denial, because a denial would leave the in-flight access token valid until its natural expiry. Repository failures fail closed to 503 `session_enforcement_unavailable`; only a clean not-found fails open, under a measured compatibility window counted by `orkestra_auth_session_anchor_anomalies_total`.
- **An inactive user receives no credentials.** Password login, web OAuth, mobile OAuth, MFA completion, refresh rotation, and read-only session bootstrap all validate token eligibility / `IsActive` before returning credentials. Disabling an account therefore blocks every issuance path; session termination additionally invalidates active refresh/session state and Redis revocation makes access tokens fail on their next request when Redis is available.
- **MFA and WebAuthn challenges have exactly one winner.** A successful TOTP/backup-code or WebAuthn assertion atomically consumes its short-lived challenge. Invalid WebAuthn assertions do not burn the challenge, but concurrent or replayed valid assertions cannot both complete or mint credentials. TOTP timestep and backup-code consumption are likewise atomic.
- **Redis revocation degradation is deliberately bounded.** Failed Redis lookup/write operations increment `orkestra_auth_session_revocation_store_failures_total` with `operation="lookup"` or `operation="write"`. Lookup failures fail open: persisted refresh/session revocation still blocks reauthentication, while an already-issued access token may remain usable only until its configured access-token expiry (rather than causing a platform-wide lockout).
- **Legacy SID migration.** Deployments upgrading from builds affected by non-canonical session IDs must revoke all active refresh-token rows, then allow already-issued access tokens to expire within the configured access-token TTL. No refresh row may be deleted while its token could still pass temporal validation, regardless of revocation state — replay detection depends on the rotated/revoked rows surviving until `expiresAt`, not on a fixed retention window past revocation.
- **Cookie iteration is picker-first, never first-error-wins.** Refresh handlers (`RefreshTokensHTTP`, `RefreshTokensWithHeaderHTTP`, `GetSessionHTTP`) call `pickRefreshCandidate` over every `orkestra_cookie` value the browser sent before any mutating call. The picker uses `AuthService.PeekRefreshToken` (pure read, no rotation, no replay handling) to classify each candidate, then returns either a valid candidate to refresh on OR — only if every candidate is rotated/expired/unknown — the first rotated one to surface a genuine replay. This shape exists because the browser sends EVERY cookie sharing the name on every request (RFC 6265): a stale parent-domain cookie left over from before the PR-D D-9 cookie-domain split (e.g. `.orkestra.cc` value frozen at a long-rotated token) used to be processed first by the old first-error-wins loop and would nuke the family behind the current `.staging-api.orkestra.cc` cookie. The picker isolates that stale sibling so genuine replay detection still fires when there is no valid candidate, but a leftover cookie next to a healthy one no longer logs the user out every 15 minutes. On successful refresh with `len(candidates) > 1`, the handlers also emit `Set-Cookie ...; Max-Age=0` for each meaningful parent of the current cookie domain (via `clearStaleParentDomainCookies`) so the browser evicts the leftover.
- **Session bootstrap is read-only — rotation lives only in the explicit refresh endpoints (`/refresh-cookie`, `/refresh`).** `GET /v1/auth/session` (handler: `GetSessionHTTP`) MUST NOT rotate the refresh row. It calls `AuthService.MintAccessTokenFromRefresh` which validates the row (non-expired, non-revoked — any reason including `rotated` disqualifies) and mints an access token without touching the refresh row's state. The TokenResponse it returns carries an empty `RefreshToken`; the caller's existing cookie stays authoritative. The split exists because the SPA had TWO independent refresh paths (`useGetSessionQuery` → `/session` AND `baseQueryWithRetry`'s 401-handler → `/refresh-cookie`) and both used to rotate. When they fired concurrently (typical on app boot / tab focus / 401 race) one would win the rotation and the other would land with a now-rotated cookie, tripping replay detection on the legitimate session holder. Keeping rotation confined to the explicit refresh endpoints — and out of `/session` — means any number of `/session` calls can coexist idempotently. The `pickRefreshCandidate` helper is still useful here for the multi-cookie-from-domain-split case, but the rotated-fallback path is intentionally unused in `GetSessionHTTP` — read-only mint never fires replay. ⚠️ "Read-only" means **does not rotate**, which is the whole anti-replay claim — it does **not** mean side-effect-free. `MintAccessTokenFromRefresh` also enforces the absolute session cap (see "A session has a maximum age" below), and reaching the cap is a logout: a `/session` call can revoke the session's refresh rows, flip the session document inactive and denylist the sid. That is deliberate — a bootstrap-only client must not be able to hold a session open past the cap — and it is still not a rotation, so no `/session` call can ever invalidate a cookie another tab is about to present.
- **`RequireAuth` is bearer-only — it never reads the refresh cookie, and rotation happens only through the explicit refresh endpoints (ADR-0020, #317).** `shared/middleware/auth.go` used to carry an *implicit* third refresh path: on any protected request whose bearer was missing/expired/invalid it called `RefreshTokensWithRiskAssessment`, wrote the successor cookie and returned the minted token in `X-New-Access-Token` / `X-Token-Refreshed`. It was not serialised, no client consumed those headers on ordinary responses (`frontend-admin` withholds an expired bearer, so *every* request after the access TTL rotated again), and a parallel burst had one winner and N−1 generic 401s whose `/refresh-cookie` retries could meet a superseded cookie — operators were signed out hours into a session. The branch, `SetAuthService` and `NewAuthMiddlewareWithConfig` are gone; the middleware answers a missing/expired/invalid bearer with a plain 401 and clients recover through `401 → refresh-cookie → retry`. `X-Token-Refreshed` left the contract; `X-New-Access-Token` survives **only** as `POST /v1/auth/{tier}/refresh`'s response channel (`RefreshTokensWithHeaderHTTP`) and stays CORS-exposed for it. `require_auth_test.go`'s three `*_NeverRotates` tests (cookie-only, expired bearer + cookie, tampered bearer + cookie) pin the *observable* contract — no `Set-Cookie`, no `X-New-Access-Token` — for a request built through the bearer-only middleware; they cannot by themselves catch the branch coming back, since a test built via `NewAuthMiddleware` never has a seam to wire in the first place. Two structural tests in the same file cover what the behavioural tests cannot, and each covers a different reintroduction shape — neither alone is the whole guard. `TestAuthMiddleware_Fields_CannotReintroduceCookieRotation` reflects over `AuthMiddleware`'s field names and fails the moment one is added or removed, so a resurrected `authService`/`cookieName`/`config` field trips it before any behaviour is written against it — but it says nothing about a rewrite that adds no field. `TestAuthGo_ContainsNoCookieRead` closes that specific gap: it parses `auth.go` with `go/ast` and fails if the file contains any `.Cookie(`/`.Cookies()` call, catching a **mint-only** rewrite — reading the refresh cookie and minting from the already-injected `jwtService` needs no new struct field, so the field-diff test alone would stay green while it happened. It is deliberately scoped to `auth.go`, not the whole package, because `device.go` legitimately reads the device-id cookie; the residual gap is a helper placed in a *different* file of the package, which neither test would catch — reviewers still carry that. Do **not** reintroduce a cookie branch here, not even a mint-only one: it keeps cookie-only auth on mutating routes and hides a refresh-row + user read behind every request from a client that ignores the header.
- **Logout identity must be signature-verified.** `POST /v1/auth/{tier}/logout` is mounted on the **public** router (`module.go` → `ri.Router`), so no auth middleware ever populates `ctx["userUUID"]` and the refresh cookie is the only identity source for the overwhelming majority of calls. `handlers/auth_handler.go::resolveLogoutIdentity` is the single place that resolves it, and it uses `jwtService.ValidateRefreshToken` — **never** `ParseUnverifiedClaims`. The unverified parse is legitimate in the audience gate (cheap routing ahead of a real verifier) but catastrophic here: there is no verifier downstream, so an unverified claim would let an anonymous caller hand-roll a JWT naming any `userUUID` and drive `TerminateAllSessionsByUUID` against it (`allDevices=true`) — an unauthenticated forced-logout of any user whose UUID is known. The `deviceId` used for single-session termination comes from the same verified claims; the request body's `refreshToken` field is not consulted (no client populates it, and a body-supplied token carries no proof of ownership). An unresolvable request still clears the cookie and returns 200 — logout is idempotent and must not become an account-existence oracle. Regression tests: `handlers/logout_identity_test.go`.
- **Client IP is never read from a request header in this module.** `utils.GetClientIP(r)` returns `r.RemoteAddr`, which `shared/middleware.RealIP` has already resolved under the deployment's trusted-proxy policy. This matters here because the login flow uses the IP for the rate-limit / lockout bucket (`"ip:"+in.IP`), the geo-block (`CountryBlocked`), the risk score, and every audit row — all of which were caller-controlled while `GetClientIP` trusted `X-Forwarded-For`. See [backend/CLAUDE.md](../../../CLAUDE.md#client-ip-resolution-trusted-proxies) for the policy and its env vars.
- **A deactivated account cannot refresh.** `RefreshTokensWithRiskAssessment` and `MintAccessTokenFromRefresh` both reject `user.IsActive == false` with `ErrInvalidRefreshToken`. `Login` has always checked this, but login is the one path an already-signed-in attacker never revisits — without the refresh-side check, disabling an account (offboarding, compromise response, the `inactiveAccountAutoDisableDays` sweep) had no effect until the refresh row expired, up to 7 days later. The user module additionally calls `iface.SessionTerminator` on deactivate/delete so existing sessions die immediately rather than after one access-token TTL; auth satisfies that interface via `AuthService.TerminateAllSessionsByUUID` under `ServiceAuthService` / `ServiceClientAuthService`.
- **A credential change must close all four pathways.** `revokeSessionsAfterCredentialChange` (`password_auth_service.go`) is the single implementation behind both password flows. It revokes **refresh tokens**, terminates **session docs**, pushes every **sid into the Redis revocation set** (so access tokens already in flight die on their next request), and drops **device-trust grants** (so the holder of the old password stops skipping the MFA prompt). Closing only some of those is close to worthless: before this, `ResetPassword` — the recovery flow for a *compromised* account — revoked refresh tokens alone, leaving the attacker's access token, session doc, and MFA-skip grant intact. `ChangePassword` passes `CurrentSID` so the caller's own session survives (they just proved the current password; evicting them achieves nothing) and every other device is signed out; `ResetPassword` passes `""` so nothing is spared and a by-user refresh sweep runs on top. Still gated by `revokeSessionsOnPasswordChange` for the change path. ⚠️ The pre-fix behaviour was the exact inverse — the handler revoked the **caller's own** sid and left every other device running. Regression tests: `services/password_credential_revocation_test.go`.
- **Device identity is server-minted, never header-derived.** `deviceId` used to be MD5(User-Agent | IP | Accept-* headers) for every browser — all caller-chosen inputs, so anyone replaying a victim's header signature *was* the victim's device as far as this system was concerned. It is now 32 random bytes issued in an HttpOnly `orkestra_did` cookie (`shared/middleware/device.go`), with `X-Device-ID` still honoured for native apps and the query-string source removed. The header-derived value survives only as `Fingerprint` — a **risk signal**, now SHA-256, never an identity and never the thing a trust decision is keyed on. ⚠️ **Device trust is currently unreachable end-to-end**: grants are only created from an OAuth-originated login challenge (`MarkTrusted` refuses an empty deviceId), grants are only consumed in `PasswordAuthService.completeLogin`, and the password handler never populates `LoginInput.DeviceID`. Wiring a device id into the password login path is the missing piece — do **not** add it without confirming the id is the cookie value, or the MFA-skip becomes reachable with a guessable key.
- **Session per device.** `AuthSession` binds a session to a `deviceId` + fingerprint. Refresh tokens link back to their session — revoking a session cascades to every token issued from it.
- **Email token TTL is 24 hours** by default — verification tokens use 24h, password reset tokens 30min, **admin-invite tokens 7 days**. Enforced by the `expiresAt` TTL index on each tier's `*_email_tokens` collection. The service also compares expiry on read in case the TTL sweeper is behind. Token purposes (`EmailTokenPurpose*` in `models/email_token.go`): `verify_email`, `reset_password`, `admin_invite` (the last sets password AND marks email verified on redemption — admin vouched for the inbox by sending the invite).
- **OAuth state is 10 minutes in Redis and ONE-SHOT.** `ValidateOAuthState` consumes the row with the store's atomic `Take` (Redis `GETDEL`); a `Get` followed by a deferred delete — the shape it had until v4.3 — let two concurrent callbacks both read one state. Exactly one presentation proceeds; a replay is a generic 400. `oauth_state_service_test.go` pins the one-winner race on the in-memory store.
- **OAuth state is bound to the endpoint's provider, the tier, the link-mode pair and the browser, in that order after the signature.** `resolveStateForCallback(ctx, raw, provider)` runs signature/expiry → browser binding → atomic take → `tier` → `provider` (`stateInfo.Provider` must equal the callback's provider — a Google state presented to the Discord callback is a 400) → `mode`/`linkUserUUID`, every mismatch the same generic error, all before the IdP `error`, the code or any profile is read.
- **The browser binding is verified where the cookie lives.** The signed state + one-shot row prove a callback belongs to a flow *we* started, not that it belongs to *this browser*: without a binding, login CSRF (an attacker-started flow finished by a victim's browser lands the victim in the attacker's session; in `mode=link` it attaches the victim's IdP identity to the attacker's account) is open. Both start endpoints therefore also drop the CSRF nonce into an HttpOnly `orkestra_oauth_state` cookie (SameSite=Lax — Strict would suppress the top-level redirect back from the IdP) and the callback requires the two to match (`handlers/oauth_state_binding.go`). `StartHost` is checked first: a cross-host callback is **deferred** whatever cookie the operator host holds (an unrelated operator flow's nonce must not block a client login); on the starting host the cookie is required and a mismatched or absent one, like a state with no `shost` claim, is rejected. The ADR-0003 tier split puts client-tier starts on `api.*` while every provider callback lands on `console.*`, so that cookie cannot reach the callback — such a callback is deferred, never accepted: the operator host does the IdP half only and hands the flow to `GET /v1/auth/client/oauth/complete` on the client API host through a one-shot relay record; the relay endpoint **requires** the cookie (`verifyRelayBinding`) and fails closed without it. The SPA must call the start endpoint with `credentials: 'include'` or the cookie is never stored — `frontend-admin`'s `socialAuthUtils.ts` and RTK `baseApi` both do.
- **OAuth link reuse refreshes the cached `picture` URL.** Every successful OAuth callback updates two places on link reuse (`auth_service.go` ≈ line 1612): the provider doc's `metadata.picture` (drives `UserManagementResponse.Providers[].Avatar`) AND the embedded `User.OAuthLinks[i].OAuthData["picture"]` (drives `blob.ResolveAvatarURL` for `AvatarSource=oauth_*`) via the additive `iface.OAuthLinkDataUpdater` sub-interface. Without this, users who linked Google before the embedded-picture field existed would never see their avatar populate until they manually re-linked.
- **Token lifetimes come from config, never from literals.** `JWTService.AccessTokenTTL(ctx)` resolves `admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY → 15m`, with the effective value clamped into 1m–24h so it can never outlive its revocation-denylist entry. **In practice the first level always answers**, and the chain is documented rather than exercised: `accessTokenTTL` is a schema field with `Default: "15m"` and no `EnvVar`, so it is seeded on first boot and `GetValue` re-supplies that default even when the stored value is empty — the policy returns a positive duration and nothing falls through, so `JWT_ACCESS_TOKEN_EXPIRY` is reached only when the `module_configs` document is missing, unreadable, **or holding a value the parser rejects** (`clampPersistedDuration`'s `0` fallback — the D6 table's "malformed value already in DB" row, which takes an out-of-band write since the PATCH validator refuses one). Change the TTL at `/admin/modules/auth`; the env-var table above has the detail and the live observation. `JWTService.RefreshTokenTTL()` resolves `JWT_REFRESH_TOKEN_EXPIRY → 7d` (the unreachable 720h zero-guard in `NewJWTService` is not a configured default). They drive every `expiresIn` in a response, the `expiresAt` on each persisted refresh row, and the `Max-Age` on every refresh cookie. Because rotation rewrites the refresh row's expiry on every use, the refresh TTL is the session's **idle** timeout, not its total lifetime; the total is bounded separately by `sessionAbsoluteTTL` (ADR-0017 D1). The lifetime deliberately kept separate from all three is `models.AuthSessionRetention` (90d): the session **document** is audit and device history that the risk scorer reads, and nothing authenticates off it.
- **The MFA attempt cap is an atomic counter.** `IncrementAttempts` moves a dedicated Redis key via `INCR` (`OAuthStateStore.Incr`), not a read-modify-write over the challenge JSON. With RMW, concurrent verifies all read the same value and wrote back the same value, so N parallel guesses cost one attempt — "5 tries" held only against a serial attacker. `Peek` reports the live counter; `Consume`/exhaustion/expiry delete challenge and counter together so a recycled id cannot inherit a spent budget. A counter that cannot be advanced fails closed (the challenge is destroyed).
- **Account lockout reads the admin policy.** The branch that stamps `User.LockedUntil` uses `AuthPolicyService.LockoutThreshold`/`LockoutDuration`, the same values plumbed into the rate limiter. It previously compared against a hardcoded `5` with a hardcoded 15-minute window, so tightening `accountLockoutThreshold` moved the in-memory bucket but not the persisted lock.
- **Password character classes are Unicode-aware.** `checkCharacterClasses` classifies with `unicode.IsUpper/IsLower/IsDigit/IsPunct/IsSymbol`. The old ASCII-range switch put *everything* non-`[A-Za-z0-9]` in the symbol bucket: a plain space satisfied `requireSymbol`, and `ПАРОЛЬ` / `passwörd` satisfied `requireSymbol` while satisfying neither `requireUpper` nor `requireLower`. Whitespace now counts as no class at all.
- **`aud` must name an audience the platform issues.** `validateTokenEnhanced` rejects anything outside `{operator, client, service}` — it used to accept any non-empty string. It deliberately does **not** pin `aud` to the minting audience: one `AuthMiddleware` with one JWT service guards both muxes, so equality would lock out a whole tier. Pinning a request to its surface is `RequireAudience`'s job at the mux.
- **Rate limiting** lives in `shared/errors.RateLimiter` and is shared across `Register`, `Login`, `ForgotPassword`, `VerifyEmailResend`. Current defaults are hardcoded — when you need to tune them, do it in `password_auth_service.go` and not in the handler.
- **Notification idempotency.** Verification and reset emails always carry an idempotency key like `verify:<userUUID>:<tokenUUID>` and `reset:<userUUID>:<tokenUUID>` so retries don't dispatch duplicates.
- **Password policy.** Length bounds, complexity requirements, and the HIBP toggle are admin-managed via the Password Policy tab; defaults match the legacy hardcoded values (10..128 chars, no complexity, HIBP on). The service still rejects `"password has appeared in a known data breach"` — observed in dev when the initial admin used a common test string.

## What this module does NOT do

- User profile CRUD or the system-role field → **user** module
- Self-service avatar (upload / OAuth picker / reset to initials) → **user** module's `/v1/me/avatar/*` surface
- Org membership, invite lifecycle, plan entitlements → **tenant** module
- Permission evaluation, role bindings, system role seeding → **authz** module
- Rendering and sending emails → **notification** module (auth just passes `TemplatedNotificationRequest`)
- WebAuthn passwordless (discoverable / usernameless) login — the current flow requires password login first, then offers passkey as the second factor. Full passwordless would need a discoverable credential entry point and a `BeginDiscoverableLogin` wiring; not built yet.
- OAuth token refresh against the provider — only the user's Orkestra session is refreshed; provider access tokens are not persisted long-term.

## Rules

- **Never store a plaintext refresh or email token.** Always hash-and-compare. Tokens are returned to the caller exactly once per issue.
- **Never embed permissions in the JWT.** If you find yourself wanting to, you need a faster `HasPermission` — not a fatter token. Revocation must be instant.
- **Never call `notification.EmailSender.Send` directly.** Every auth-triggered email must go through `SendTemplated` with a `TemplateID` that exists in `notification/services/default_templates.go`.
- **Never read `cfg.Auth.JWT.PrivateKey` outside the JWT service.** Key material stays inside one package.
- **Never bypass the rate limiter on login / forgot-password endpoints.** The limiter is the only protection against credential stuffing and reset-flood.
- **When you add a new OAuth provider**, add its fields to `ConfigSchema()`, extend the switch in `oauth_config_resolver.go`, and wire the factory case in `services/oauth_provider_factory.go`. Never hardcode provider config inside a handler — everything flows through the resolver so admin edits are live.
- **Never read `cfg.Auth.{Google,Apple,GitHub,Discord}` from handlers.** Those struct fields still load from env vars for backward compatibility, but OAuth config is owned by the resolver. Handlers must call `h.oauthResolver.Get/RedirectURL/MobileAudience` so the admin panel stays authoritative.
- **Never build a `/auth/callback` or `/user/security` URL outside `handlers/oauth_callback_redirect.go`**, never put a token, an email or a user id in one, and never set a client-tier cookie from the operator host — relay instead. The structural scan and `oauth_callback_flow_test.go` are the guards.
- **Every new auth-adjacent collection needs a deliberate TTL decision.** Email tokens have TTLs because they're user-initiated. Sessions have one because `expiresAt` *is* the retention deadline (`models.AuthSessionRetention`) — a TTL index expresses that intent exactly. Refresh-token rows do not: a row may be deleted only once its own `expiresAt` is past, and the first cleanup of an upgraded install needs bounded per-cycle progress plus backlog telemetry that Mongo's TTL monitor cannot provide — they're swept by the elected scheduler in `maintenance.go` instead. Don't copy-paste one pattern into the other.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — consumed via `UserProvider` for every flow
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — consumed via `TenantProvider` for membership embedding in JWTs
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — consumed via `AuthzProvider` for permission checks in middleware
- [`../notification/CLAUDE.md`](../notification/CLAUDE.md) — optional dependency for verification + reset emails
- [`../../shared/middleware/auth.go`](../../shared/middleware/auth.go) — JWT validation, `RequirePermission`, `RequireGlobal`
- [`../../../../docs/site/architecture/authentication-flow.mdx`](../../../../docs/site/architecture/authentication-flow.mdx) — high-level walkthrough of the flows
