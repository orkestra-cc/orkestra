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
| `handlers/auth_handler.go` | OAuth initiate/callback endpoints, mobile ID-token routes, logout, refresh |
| `handlers/password_handler.go` | Register, login, verify email, forgot/reset/change password |
| `handlers/admin_user_auth_handler.go` | Operator-side admin endpoints under `/v1/admin/users/{id}/...` — auth-methods aggregator, send-password-reset, resend-verification, oauth unlink. Inline error mapping translates the typed service errors to 404 / 409 with body codes |
| `handlers/self_user_auth_handler.go` | Self-service endpoints under `/v1/auth/{tier}/me/...` — auth-methods aggregator, session list/revoke, OAuth self-unlink. Drives the operator-tier `/user/security` page; mirrors the admin handler's structure with self-action allowed |
| `services/auth_service.go` | OAuth orchestration, provider linking, token pair issuance, auth-methods aggregator (`GetUserAuthMethods`), admin OAuth unlink (`AdminUnlinkOAuth`) + self-service unlink (`SelfUnlinkOAuth`) sharing a `wouldLockOutOAuthUnlink` lockout helper, session list / revoke methods with three-step revocation (refresh tokens → session doc → Redis sid) |
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
| `operator_refresh_tokens` / `client_refresh_tokens` | `uuid` unique, `userUuid`, `familyId` | — (rotation is explicit; revoked rows retained ≥ refresh TTL so replay detection can see them) |
| `operator_sessions` / `client_sessions` | `uuid` unique | — |
| `auth_security_events` | (none declared) | — — single non-tier-split (audit log keyed on userUUID alone) |
| `operator_email_tokens` / `client_email_tokens` | `uuid` unique, `tokenHash` unique, `userUuid`, `expiresAt` **TTL 24h** | Yes |
| `operator_mfa_factors` / `client_mfa_factors` | `uuid` unique, compound `(userUuid, type)` unique | — — one row per (user, factor type). The `webauthn` row carries an embedded `webauthnCredentials[]` array (zero-or-many passkeys per user) |
| `auth_device_trust` | `uuid` unique, `(userUuid, deviceId)`, `trustedUntil` (TTL via ExpireAt) | Yes — single non-tier-split (grant follows the user record) |

Only email tokens and device-trust grants currently have a TTL — refresh tokens, sessions, and MFA factor rows are rotated/invalidated explicitly in the service layer.

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
3. **OAuth config resolver**: `NewOAuthConfigResolver(deps.ConfigService)`. Reads are served by `ModuleConfigService` (30s Redis cache in front of Mongo), so per-request resolution is sub-millisecond.
4. **JWT service**: loaded with the `AUTH_JWT_PRIVATE_KEY` / `AUTH_JWT_PUBLIC_KEY` pair, then has `SetTenantProvider(...)` called on it so every future `GenerateAccessToken` embeds the caller's current org memberships.
5. **OAuth state service**: Redis-backed state/nonce store, 10-minute TTL.
6. **Auth service**: the orchestrator for OAuth flows.
7. **Password service**: argon2id hasher with HIBP policy validation (`services/password_service.go`).
8. **Password auth service**: register/login/verify/reset/change flows, wired to the optional notification sender and a shared `RateLimiter`.
9. **Handlers**: OAuth, password, MFA, and WebAuthn handlers, each constructed twice (operator + client) and stamped with the matching tier's cookie domain at construction time (`cfg.Auth.Cookie.OperatorDomain` / `ClientDomain`, falling back to the legacy `Domain`). The shared `Cookie.Name` + `Cookie.Secure` are still process-scoped.
10. **Register services** under `ServiceAuthService`, `ServiceJWTService`, `ServicePasswordService`, `ServicePasswordAuthService`, plus the per-tier keys (`ServiceOperator{AuthService,PasswordAuthService,JWTService}` / `ServiceClient{...}`) that audience-aware consumers (dev token generator, future tier-specific addons) request directly.

`Start / Stop / HealthCheck` inherit from `BaseModule`.

No seeding — there are no default accounts or default tokens. The first user is created by whichever external flow gets there first (setup wizard, OAuth signup, password register).

## Runtime configuration

OAuth provider settings are admin-managed through `ConfigSchema()` — stored in `module_configs`, cached in Redis for 30s, secrets encrypted at rest with AES-256-GCM, editable at `/admin/modules`. Env vars are the **seed source** only: on first boot the registry populates the document from the `EnvVar` field on each schema entry, and after that the document is authoritative. Non-OAuth settings (JWT keys, cookies, feature toggles) still live in `cfg *config.Config` because they're process-scoped and must not rotate at runtime.

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
| Registration | `registrationEnabledAdmin/Client`, `defaultRoleClient`, `allowedEmailDomainsAdmin/Client` | `Register` returns 403 `registration_disabled` / `email_domain_not_allowed` per surface; `defaultRoleClient` overrides the role assigned to a new Tier-2 signup. The very first user on a fresh install bypasses the kill switch so a misconfigured flag can't lock everyone out. |
| Login & Sessions | `loginEnabledAdmin/Client`, `accountLockoutThreshold`, `accountLockoutDuration` | `Login` returns 403 `login_disabled` per surface; OAuth start endpoints (`InitiateOAuthLogin`, `HandleMobile{Google,Apple}Auth`) honour the same gate. The lockout pair is plumbed into `RateLimiter.SetAuthFailedConfig` on every login attempt — admin edits take effect on the next try. |
| Password Policy | `passwordMinLength`, `passwordMaxLength`, `passwordRequireUpper/Lower/Digit/Symbol`, `breachedPasswordCheck` | `passwordService.ValidatePolicy` reads the live policy on every signup / change-password / reset. Defaults match the legacy hardcoded values (10..128 chars, no complexity, HIBP on). New errors: `ErrPasswordMissing{Upper,Lower,Digit,Symbol}`. An inverted min/max range is swapped on read so a misedit can't reject every password. |
| OAuth Providers | `{google,apple,github,discord}Enabled{Admin,Client}`, `oauthAllowSignup{Admin,Client}`, `oauthAutoLinkByEmail` | `ListOAuthProviders` filters its return per audience; `InitiateOAuthLogin` + mobile handlers return 403 `oauth_provider_disabled` for a disabled surface. Credentials still live one-set-per-provider in the existing tabs. Phase 9: `oauthAllowSignup{Admin,Client}` (default true) gates whether an OAuth callback resolving to an unknown email may auto-provision a new account. When off, callbacks redirect to `/auth/callback?success=false&error=oauth_signup_disabled` instead of creating the user. Phase 10: `oauthAutoLinkByEmail` (default true) gates auto-attaching a provider to an existing email-matched account; when off, returns `ErrOAuthLinkDisabled` and the user must initiate linking from authenticated settings. |
| MFA | `mfaEnabled`, `mfaEnrollmentGraceDays`, `mfaRequiredForRoles`, `recoveryCodesCount` | `mfaEnabled=false` short-circuits `MFARequired` to false (existing enrollments are not deleted; voluntary verification still works). `mfaEnrollmentGraceDays` overrides the legacy 7-day `MFAEnrollmentGraceWindow` constant — new value takes effect on the next login. Phase 9: `mfaRequiredForRoles` (stringList, lowercased on read) replaces the built-in privileged-role list when set. Empty falls back to the built-in (super_admin, administrator, org_owner, org_admin). The kill switch wins over both the built-in and the configured list. Phase 10: `recoveryCodesCount` overrides the legacy `BackupCodeCount` constant when in the safe range 1..50; out-of-range falls back to the legacy default 10. Read at enrollment-confirm time so admin edits take effect on the next user's enrollment. |
| Anti-abuse & Notifications | `notifyUserOnNewDeviceLogin`, `notifyAdminOnSuspiciousLogin`, `suspiciousLoginRecipients`, `ipAllowlistAdmin`, `ipBlocklistAdmin`, `geoBlockCountries`, `inactiveAccountAutoDisableDays` | New-device user email fires from `PasswordAuthService.notifyNewDeviceLogin` when no prior session exists for the (deviceId, userUUID) pair (template `auth.new_device_login`). Admin half of the suspicious-login fan-out reads `notifyAdminOnSuspiciousLogin` + `suspiciousLoginRecipients` live on every `OnLogin` (template `auth.admin_suspicious_login`, distinct idempotency key per recipient). `ipAllowlistAdmin` / `ipBlocklistAdmin` drive a chi middleware (`shared/middleware/ip_gate.go`) mounted on the operator host mux only — empty allowlist = open, blocklist always wins. `geoBlockCountries` resolves the request IP via `geoip.Resolver` and rejects login with 403 `country_blocked` (fails open when geoip is disabled or unable to resolve). `inactiveAccountAutoDisableDays` = N>0 flips `user.IsActive=false` on next login when `lastLogin` is older than N days; 0 disables the check. |
| Sessions & Account | `revokeSessionsOnPasswordChange`, `selfServiceAccountDeletionClient` | Phase 8 trivial toggles. `revokeSessionsOnPasswordChange` (default true) gates both the device-trust revocation in `PasswordAuthService.ChangePassword` and the handler-side session-id revocation — when off, password change leaves existing sessions alive (migration / staged-rollout escape hatch; not recommended in steady state). `selfServiceAccountDeletionClient` (default false) controls whether `POST /v1/me/dsr/erase` is mounted on the client surface; export stays unconditional. The compliance module pulls `*AuthPolicyService` from `ServiceAuthPolicy` at Init and reads the toggle live on every erase request. Operator-side erasure is unaffected by either toggle. `audiencePinning` was originally on this list but is structural (host-mux `RequireAudience`) — exposing it as a flippable toggle would be a security-regression vector, so it stays as a non-toggleable invariant. |

The privileged-role list itself (`super_admin`, `administrator`,
`org_owner`, `org_admin`) is still hardcoded in
`services/mfa_policy.go`. Making it admin-managed is a deliberate
follow-up — the change is security-sensitive and worth a PR diff.

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

| Env var | Purpose | Default |
|---|---|---|
| `AUTH_JWT_PRIVATE_KEY` / `AUTH_JWT_PUBLIC_KEY` | RS256 key pair (paths or PEM) | — (required) |
| `AUTH_REQUIRE_EMAIL_VERIFICATION` | Gate signup on successful verification | `true` in prod, `false` otherwise |
| `JWT_ACCESS_TOKEN_EXPIRY` | Access-token TTL (Go `time.Duration`, e.g. `15m`, `1h`). Applied by `NewJWTService`; zero/unset falls back to `15m`. | `15m` |
| `JWT_REFRESH_TOKEN_EXPIRY` | Refresh-token TTL. Applied by `NewJWTService`; zero/unset falls back to `720h` (30d). | `7d` |
| `COOKIE_NAME` / `COOKIE_SECURE` / `COOKIE_SAME_SITE` / `COOKIE_HTTP_ONLY` / `COOKIE_MAX_AGE` | Refresh-token cookie attributes shared across audiences | set in `cfg.Auth.Cookie` |
| `COOKIE_DOMAIN` | Legacy single-host cookie domain. Used as the fallback when the per-audience values below are empty (single-host or transitional deployments). | empty |
| `OPERATOR_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for tokens minted on the operator host (`console.*`). ADR-0003 PR-D D-9 — keep this distinct from `CLIENT_COOKIE_DOMAIN` so a session minted on one surface can't be replayed on the other. | `console.localhost` (dev) / empty (prod, operator-set) |
| `CLIENT_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for tokens minted on the client host (`api.*`). | `api.localhost` (dev) / empty (prod, operator-set) |
| `FRONTEND_URL` | Legacy single-host SPA origin. Used to build `verify-email` / `reset-password` links in transactional email and as the fallback when the per-tier values below are empty. | `http://localhost:8080` |
| `OPERATOR_FRONTEND_URL` | Operator-tier SPA origin (`console.*`). Verification + reset links minted by the operator-tier `PasswordAuthService` use this host. Empty falls back to `FRONTEND_URL`. | empty |
| `CLIENT_FRONTEND_URL` | Client-tier SPA origin (`app.*`). Set this so signups landing on the client API host get verify links pointing at the client SPA, not the operator console. Empty falls back to `FRONTEND_URL`. | empty |
| `APP_NAME` / `SUPPORT_EMAIL` | Rendered into verification/reset email templates | `Orkestra` / empty |

### Resolver API

`OAuthConfigResolver` is the single entry point handlers use to read OAuth settings. Never read `cfg.Auth.Google/Apple/GitHub/Discord` directly — those fields are still populated from env vars for the Load path but are effectively dead code for the OAuth handlers.

| Method | Returns |
|---|---|
| `Get(ctx, provider)` | `(*OAuthProviderConfig, bool)` — builds the full config for `factory.CreateProvider(p, cfg)`; `false` means client ID is empty |
| `RedirectURL(ctx, provider)` | Web callback URL, or `""` |
| `MobileAudience(ctx, provider, platform)` | Platform-specific client ID for mobile ID-token validation; falls back to the web client ID when `platform` is unknown |
| `ConfiguredProviders(ctx)` | List of provider names that currently have a client ID — served by `GET /v1/auth/providers` to the login UI |

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

## HTTP endpoints

Registered from two handlers — `auth_handler.go` for OAuth/session/refresh, `password_handler.go` for password flows.

After the ADR-0003 PR-D D-8 hard cutover every auth route is mounted under one of two audience prefixes — `/v1/auth/operator/...` (operator host mux) or `/v1/auth/client/...` (client host mux). The legacy `/v1/auth/...` paths no longer exist. Use `{tier}` below as a stand-in for `operator` or `client`; both prefixes mount the same routes with audience-correct token issuance and cookie domains.

The OAuth provider callbacks (`/v1/auth/oauth/{google,apple,discord,github}/callback`) and the OAuth-side session poll (`/v1/auth/session`) stay un-prefixed — the IdP has a single registered redirect URI per provider, and the operator AuthHandler dispatches the resulting flow to the matching tier's authService via the signed-state JWT's `tier` claim.

### Public (no auth required)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/auth/{tier}/providers` | List OAuth providers currently configured for this audience |
| GET | `/v1/auth/{tier}/policy` | Public slice of admin-managed auth policy: `{registrationEnabled, loginEnabled, passwordMinLength}`. Read by the SPA login + signup pages so kill switches hide the CTA instead of surfacing as a 403 on submit |
| POST | `/v1/auth/{tier}/oauth/login` | Start an OAuth flow. The signed-state JWT carries `tier` so the shared callback dispatches to the matching authService |
| POST | `/v1/auth/{tier}/google/mobile` | Exchange a Google ID token from a mobile app for an Orkestra session; mints tokens with `aud=tier` |
| POST | `/v1/auth/{tier}/apple/mobile` | Exchange an Apple ID token from a mobile app for an Orkestra session; mints tokens with `aud=tier` |
| GET | `/v1/auth/oauth/google/callback` | Web OAuth callback (raw HTTP). Single shared callback per provider — dispatches to operator or client via state.tier |
| GET | `/v1/auth/oauth/discord/callback` | Web OAuth callback (raw HTTP) |
| POST | `/v1/auth/oauth/apple/callback` | Apple returns form-post, not a redirect (raw HTTP) |
| GET | `/v1/auth/oauth/github/callback` | GitHub web OAuth callback (Huma-registered) |
| GET | `/v1/auth/session` | Poll for session after OAuth redirect finishes |
| POST | `/v1/auth/{tier}/register` | Email+password signup |
| POST | `/v1/auth/{tier}/login` | Email+password login |
| POST | `/v1/auth/{tier}/verify-email` | Consume a verification token |
| POST | `/v1/auth/{tier}/verify-email/resend` | Request a new verification email |
| POST | `/v1/auth/{tier}/forgot-password` | Send a password reset email |
| POST | `/v1/auth/{tier}/reset-password` | Consume a reset token and set a new password |
| POST | `/v1/auth/{tier}/accept-invite` | Consume an `admin_invite` token: set the user's password **and** mark email verified atomically. Issued by the operator-side admin invite flow (see user CLAUDE.md). |
| POST | `/v1/auth/{tier}/refresh` | Refresh using a header-supplied refresh token |
| POST | `/v1/auth/{tier}/refresh-cookie` | Refresh using the `Cookie:` header |
| POST | `/v1/auth/{tier}/logout` | Revoke refresh cookie, invalidate session |

### Protected (bearer access token required)

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/v1/auth/{tier}/me` | bearer | Return the current authenticated user |
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
| DELETE | `/v1/auth/{tier}/me/oauth/{provider}` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: unlink one of the caller's OAuth identities. Service-layer last-credential safeguard rejects with 409 `last_credential` when removing would leave the user with no usable login |
| POST | `/v1/auth/{tier}/me/oauth/link/{provider}` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: start the OAuth flow that adds a new sign-in provider to the caller's account. Returns `{authUrl, state}` — the SPA navigates to `authUrl`; the shared callback redirects back to `/user/security?tab=oauth&link=success\|failed&provider=<x>&code=<reason>`. The signed-state JWT carries `mode=link` + `linkUserUUID` so the callback binds the new identity to the authenticated user without trusting any query-string parameter |
| DELETE | `/v1/auth/{tier}/me/sessions/{sessionId}` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: revoke one session by UUID. Returns 409 `cannot_revoke_current` when the target sid matches the caller's JWT — logout is the right tool for that |
| DELETE | `/v1/auth/{tier}/me/sessions` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: revoke every active session except the calling one. Returns `{revoked: int}` |
| POST | `/v1/auth/{tier}/me/mfa/backup-codes/regenerate` | `RequireGlobal()` + `RequireStepUp(5m)` | Self-service: replace the user's TOTP backup-code list with a fresh set. Old codes stop working immediately. Returns `{codes: string[]}` exactly once |
| GET | `/v1/admin/users/{userId}/auth-methods` | `RequireSystemPermission("system.users.admin")` | Admin: aggregate password / MFA / OAuth state of an operator user. Drives the Authentication Methods card on `/admin/user/profile/:userId`. Read-only |
| POST | `/v1/admin/users/{userId}/send-password-reset` | `RequireSystemPermission("system.users.password_reset")` | Admin: trigger the standard password-reset email for an operator user. Operator-side companion of the existing client-user route |
| POST | `/v1/admin/users/{userId}/resend-verification` | `RequireSystemPermission("system.users.email_verify_resend")` | Admin: re-emit the email-verification message. Idempotent — already-verified users return 200 with no action |
| DELETE | `/v1/admin/users/{userId}/oauth/{provider}` | `RequireSystemPermission("system.users.oauth_unlink")` + `RequireStepUp(5m)` | Admin: unlink a Google/Apple/GitHub/Discord identity. Service-layer safeguards reject self-action (409 `self_action`) and last-credential lockout — no password + sole OAuth link returns 409 `last_credential` |

And a public endpoint that completes a login after a partial response:

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/v1/auth/{tier}/mfa/login/verify` | none (uses `challengeId`) | Complete a login by validating TOTP/backup; mints full token pair with `amr:[source,otp]` |
| POST | `/v1/auth/{tier}/mfa/webauthn/login/begin` | none (uses `loginChallengeId`) | Begin a passkey assertion to satisfy a paused login |
| POST | `/v1/auth/{tier}/mfa/webauthn/login/finish` | none (uses both challenge ids) | Finish a passkey assertion; mints full token pair with `amr:[source, otp, webauthn]` |

`change-password` and the self-service MFA routes are deliberately global (no org context) because they're user-level flows.

### MFA implementation notes

- **Privilege policy** lives in `services/mfa_policy.go`. `RoleRequiresMFA(user, memberships)` returns true for `super_admin`, `administrator`, and any org membership carrying `org_owner`/`org_admin`. `developer` is intentionally excluded — its prod downgrade to read-only covers the risk.
- **Grace period defaults to 7 days** (legacy `MFAEnrollmentGraceWindow` constant; runtime value comes from `mfaEnrollmentGraceDays` on the MFA policy tab). A privileged user logging in without a factor has `User.MFAGraceStartedAt` stamped on that login (idempotent via `UserProvider.StartMFAGraceIfUnset`). Past the window, login returns 403 `mfa_enrollment_required`. Granting a privileged role via authz `CreateBinding` also eagerly starts the clock so the configured window begins at promotion, not next login. The master `mfaEnabled` flag short-circuits the requirement entirely without deleting existing enrollments.
- **Login state machine** (`PasswordAuthService.completeLogin`; OAuth mirrors via `AuthService.evaluateMFAForOAuth`): (a) non-privileged → full token with `amr:["pwd"]`/`["oauth"]`; (b) privileged with factor → partial 200 response `{requiresMfa: true, mfaToken: <challengeId>}` and no access token — client must call `/v1/auth/mfa/login/verify`; (c) privileged without factor within grace → full token + `mfaEnrollmentRequired:true` + `mfaGraceExpiresAt`; (d) privileged without factor past grace → `ErrMFAEnrollmentRequired` → 403.
- Factor secrets are AES-256-GCM encrypted with `MFA_SECRET_ENCRYPTION_KEY` (falls back to `OAUTH_TOKEN_ENCRYPTION_KEY` for single-key dev setups). Backup codes are argon2id hashed via the existing `PasswordService`.
- Challenge state lives in Redis under `mfa:challenge:<uuid>` with a 5-minute TTL; after 5 failed verifications the challenge is deleted. Login challenges additionally carry `DeviceID`/`Platform`/`IPAddress`/`Fingerprint`/`SourceAMR` so the public login-verify endpoint can mint a token pair without re-posting the user's password.
- **TOTP replay guard** — `MFAFactorDoc.LastUsedStep` advances via an atomic `AdvanceLastUsedStep` CAS in the repo (`$or: lastUsedStep < step OR $exists:false`). A captured code cannot be used twice within its 30-second window, whether by the same caller or a concurrent one.
- `JWTClaims.AMR` (RFC 8176) and `JWTClaims.LastOTPAt` are emitted `omitempty` so pre-Block-A tokens still validate. Password login sets `amr:["pwd"]`, OAuth `amr:["oauth"]`, MFA verify sets `amr:[source,"otp"]` + `last_otp_at=now`.
- `RoleMiddleware.RequireMFA()` is applied to the routes whose abuse MFA exists to prevent: authz role + binding mutations (create/update/delete-role, create/delete-binding), tenant scoped mutations (update/delete-org, update-plan, remove-member, create-invite), and module config writes (`update-module`, `update-module-environment`, `set-active-environment`). Read paths stay open.
- `RoleMiddleware.RequireStepUp(maxAge)` is a stricter variant applied to catastrophic / irreversible actions (currently `POST /v1/auth/me/mfa/remove` and `POST /v1/admin/users/{id}/mfa/reset`). It checks both that `amr` contains an MFA marker AND that `last_otp_at` is within `maxAge` of now — a session-long MFA proof is not enough. Returns 401 with `code="step_up_required"` + `maxAgeSeconds`; the web frontend's global `StepUpModal` pauses the request, drives the user through `/mfa/verify`, and replays.
- **Session revocation list** — Redis-backed set at `auth:revoked:session:<sid>` checked on every authenticated request by both `AuthMiddleware` (monolith) and `JWTValidator` (sidecar). Populated on logout + change-password; payload is the reason string for operator debugging. Entries auto-expire after the access-token TTL + 1min buffer. Fails open on Redis errors — a degraded Redis must not lock every user out. Logout invalidates the current sid only; `allDevices=true` still relies on refresh-token revocation (per-user-generation counter is a follow-up).
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
`AdminUnlinkOAuth` so a self-unlink that would leave the user with no
usable login method is rejected with 409 `last_credential`. The
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

- `GET .../auth-methods` — aggregates `User.PasswordHash` presence + `PasswordUpdatedAt`, MFA factor rows from `operator_mfa_factors`, OAuth identities from `User.OAuthLinks`, and email-verification + last-login state into one `models.AuthMethodsView`. Backed by `AuthService.GetUserAuthMethods`. Read-only — gated by `system.users.admin` rather than a new permission since reading is incidental to user administration.
- `POST .../send-password-reset` — proxies to `iface.AdminAuthInviter.AdminTriggerPasswordReset` on the operator-tier `*PasswordAuthService`. No step-up — the action emits a notification, it does not read or mutate a credential.
- `POST .../resend-verification` — same pattern via `AdminResendVerification`. Idempotent (200 with no action when already verified).
- `DELETE .../oauth/{provider}` — backed by `AuthService.AdminUnlinkOAuth`. Service-layer safeguards: rejects `actorUUID == targetUUID` (`ErrAdminSelfAction` → 409 `self_action`) and rejects the operation when it would leave the user with no usable login method, i.e. `PasswordHash == "" && len(activeOAuthLinks) == 1` (`ErrLastCredentialRemoval` → 409 `last_credential`). Step-up gated because the action removes a credential.

Each successful action emits `slog.Info("admin_auth_action", event=…, actorUUID=…, targetUUID=…)` so the operator log stream carries an audit record. The `event` is one of `admin_oauth_unlink`, `admin_password_reset_sent`, `admin_verification_resent`. **Persistent audit rows in `auth_security_events` are a follow-up** — `services.AuthService.RecordSecurityEvent` is currently a no-op; back-filling real persistence so the four admin paths land rows in the collection is tracked separately and is the right place to plumb actor/target/IP/UA at write time.

## Service contract

No single interface is exposed from this module — its concrete services are consumed from the registry by type. The one published interface is:

- **`iface.JWTProvider`** (`shared/iface/interfaces.go:56-62`) — just `GenerateAccessToken(user *User) (string, error)`. Consumed by the dev module to mint test tokens.

Everything else (`services.AuthService`, `services.JWTService`, `services.PasswordService`, `services.PasswordAuthService`) is fetched with `MustGetTyped[*services.X]` by `cmd/server/main.go` or by middleware. This is intentional — the surface is too broad to pin as an interface today.

## Key invariants

- **JWT payload shape.** Access tokens carry: `sub`, `email`, `srole` (the global system role), `memberships` (an array of `{orgId, orgName, orgSlug, roles[]}` fetched via `TenantProvider.ListUserMemberships` at issue time). **Permissions are not embedded** — they are resolved per-request by middleware calling `authz.HasPermission`. This is the most important thing to remember about the authentication architecture: roles are coarse-grained and cached in the JWT, permissions are fine-grained and resolved fresh.
- **First-user heuristic.** `password_auth_service.go::Register` (`:116-121`), `RegisterInitialAdmin` (`:177`), and `auth_service.go::OAuth register` all check `GetUserCount(ctx, nil) == 0` and assign `super_admin` to the first account created on a fresh install. The setup wizard's `POST /v1/setup/admin` uses `RegisterInitialAdmin` which also bypasses email verification.
- **Email verification is gated by `AUTH_REQUIRE_EMAIL_VERIFICATION`.** `true` in production, `false` elsewhere. When true, signup returns 503 with `ErrNotificationDown` if the notification sender is missing or reports `IsConfigured() == false`. `RegisterInitialAdmin` (setup wizard path) bypasses verification entirely because the wizard runs before SMTP is configured.
- **Refresh tokens rotate on every use with family detection.** Each login mints a fresh `FamilyID`; every subsequent rotation preserves it via `RotateWithFamily` (atomic CAS on `{isRevoked:false}`). Old rows are marked `revokedReason="rotated"` with `succeededBy` pointing at the successor so the chain is walkable. Reuse of a rotated token — or CAS-loss on concurrent rotation — triggers `RevokeFamily`: every active row in the lineage is revoked with `revokedReason="replay_detected"`, a structured `slog.Warn` fires, and callers get `ErrRefreshTokenReplay` → 401 with body `{code:"refresh_token_replay"}`. Pre-Block-C rows have empty `FamilyID`; `RevokeFamily("")` is a no-op guard so a stray pre-Block-C replay doesn't wipe unrelated sessions. Revoked rows must stay in the collection for at least the refresh TTL — do not shorten `CleanupRevokedTokens`'s `olderThan` below that.
- **Session per device.** `AuthSession` binds a session to a `deviceId` + fingerprint. Refresh tokens link back to their session — revoking a session cascades to every token issued from it.
- **Email token TTL is 24 hours** by default — verification tokens use 24h, password reset tokens 30min, **admin-invite tokens 7 days**. Enforced by the `expiresAt` TTL index on each tier's `*_email_tokens` collection. The service also compares expiry on read in case the TTL sweeper is behind. Token purposes (`EmailTokenPurpose*` in `models/email_token.go`): `verify_email`, `reset_password`, `admin_invite` (the last sets password AND marks email verified on redemption — admin vouched for the inbox by sending the invite).
- **OAuth state is 10 minutes in Redis.** Validated before code exchange in every provider's callback handler.
- **Rate limiting** lives in `shared/errors.RateLimiter` and is shared across `Register`, `Login`, `ForgotPassword`, `VerifyEmailResend`. Current defaults are hardcoded — when you need to tune them, do it in `password_auth_service.go` and not in the handler.
- **Notification idempotency.** Verification and reset emails always carry an idempotency key like `verify:<userUUID>:<tokenUUID>` and `reset:<userUUID>:<tokenUUID>` so retries don't dispatch duplicates.
- **Password policy.** Length bounds, complexity requirements, and the HIBP toggle are admin-managed via the Password Policy tab; defaults match the legacy hardcoded values (10..128 chars, no complexity, HIBP on). The service still rejects `"password has appeared in a known data breach"` — observed in dev when the initial admin used a common test string.

## What this module does NOT do

- User profile CRUD or the system-role field → **user** module
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
- **Every new auth-adjacent collection needs a deliberate TTL decision.** Email tokens have TTLs because they're user-initiated. Sessions do not because they're invalidated explicitly. Don't copy-paste one into the other.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — consumed via `UserProvider` for every flow
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — consumed via `TenantProvider` for membership embedding in JWTs
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — consumed via `AuthzProvider` for permission checks in middleware
- [`../notification/CLAUDE.md`](../notification/CLAUDE.md) — optional dependency for verification + reset emails
- [`../../shared/middleware/auth.go`](../../shared/middleware/auth.go) — JWT validation, `RequirePermission`, `RequireGlobal`
- [`../../../../docs/Authentication_flow.md`](../../../../docs/Authentication_flow.md) — high-level walkthrough of the flows
