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
| `services/auth_service.go` | OAuth orchestration, provider linking, token pair issuance |
| `services/password_auth_service.go` | Password register/login/verify/reset/change, rate-limited |
| `services/password_service.go` | Argon2id hashing + policy validation |
| `services/jwt_service.go` | RS256 JWT signing, validation, membership embedding |
| `services/oauth_provider_factory.go` | Factory for Google / Apple / Discord / GitHub providers |
| `services/oauth_state_service.go` | Redis-backed OAuth state/nonce with 10-minute TTL |
| `services/risk_assessment_service.go` | Device-fingerprint + IP risk scoring |
| `repository/auth_repository.go` | Legacy shared repository, mainly for account/link lookups |
| `repository/auth_session_repository.go` | Device session documents |
| `repository/refresh_token_repository.go` | Hashed refresh tokens + rotation lineage |
| `repository/oauth_provider_repository.go` | `auth_oauth_providers` — provider-side lookup (provider + providerID → user) |
| `repository/email_token_repository.go` | Single-use verification + reset tokens |
| `models/*.go` | `OAuthProvider`, `RefreshToken`, `AuthSession`, `EmailToken`, `SecurityEvent`, collection-name constants |
| `utils/pkce.go`, `utils/redirect_validation.go` | PKCE helpers + redirect-URL allowlist check |

## MongoDB collections

Declared in `module.go:53-74`. Collection name constants live in `models/collections.go`.

| Collection | Indexes | TTL |
|---|---|---|
| `auth_oauth_providers` | compound `(userUuid, provider)` unique | — |
| `auth_refresh_tokens` | `uuid` unique, `userUuid` | — (rotation is explicit) |
| `auth_sessions` | `uuid` unique | — |
| `auth_security_events` | (none declared) | — |
| `auth_email_tokens` | `uuid` unique, `tokenHash` unique, `userUuid`, `expiresAt` **TTL 24h** | Yes (`module.go:71`) |

Only email tokens currently have a TTL — refresh tokens and sessions are rotated/invalidated explicitly in the service layer.

## Dependencies

- **Modules** (`module.go:31`): `user`, `notification`, `tenant`, `authz`. All four are listed so the topological sort boots them first.
- **Required services** (`module.go:32-34`): `ServiceUserService`, `ServiceTenantProvider`. Panics if missing — both are core.
- **Optional services** (`module.go:35-37`): `ServiceNotificationSender`. Graceful degradation: signup and password-reset mail endpoints still mount, but when `RequireEmailVerification=true` signup returns 503 unless the notifier is configured.
- **Provides** (`module.go:38-45`): `ServiceAuthService`, `ServiceJWTService`, `ServicePasswordService`, `ServicePasswordAuthService`.
- **Permissions contributed** (`module.go:47-51`): only `auth.self` — "edit your own password and sessions".

## Lifecycle

`Init` (`module.go:76-210`) is where every moving part gets wired:

1. **Repositories**: auth, OAuth provider, refresh token, auth session, email token.
2. **OAuth provider factory**: reads `cfg.Auth.{Google,Apple,GitHub,Discord}` and builds a per-provider config. Apple can load its private key either inline (`AUTH_APPLE_PRIVATE_KEY`) or from a file path (`AUTH_APPLE_PRIVATE_KEY_PATH`).
3. **JWT service**: loaded with the `AUTH_JWT_PRIVATE_KEY` / `AUTH_JWT_PUBLIC_KEY` pair, then has `SetTenantProvider(...)` called on it so every future `GenerateAccessToken` embeds the caller's current org memberships.
4. **OAuth state service**: Redis-backed state/nonce store, 10-minute TTL.
5. **Auth service**: the orchestrator for OAuth flows.
6. **Password service**: argon2id hasher with HIBP policy validation (`services/password_service.go`).
7. **Password auth service**: register/login/verify/reset/change flows, wired to the optional notification sender and a shared `RateLimiter`.
8. **Handlers**: OAuth and password handlers, both given access to the cookie config (`cfg.Auth.Cookie.Name`, `Domain`, `Secure`).
9. **Register services** under `ServiceAuthService`, `ServiceJWTService`, `ServicePasswordService`, `ServicePasswordAuthService`.

`Start / Stop / HealthCheck` inherit from `BaseModule`.

No seeding — there are no default accounts or default tokens. The first user is created by whichever external flow gets there first (setup wizard, OAuth signup, password register).

## Runtime configuration

Config today lives in env vars via `cfg *config.Config`, not in `ConfigSchema`. This module does not expose a `/admin/modules` config surface. Relevant env vars:

| Env var | Purpose | Default |
|---|---|---|
| `AUTH_JWT_PRIVATE_KEY` / `AUTH_JWT_PUBLIC_KEY` | RS256 key pair (paths or PEM) | — (required) |
| `AUTH_REQUIRE_EMAIL_VERIFICATION` | Gate signup on successful verification | `true` in prod, `false` otherwise |
| `AUTH_GOOGLE_CLIENT_ID` / `SECRET` | Google Web OAuth | — |
| `AUTH_GOOGLE_ANDROID_CLIENT_ID` / `AUTH_GOOGLE_IOS_CLIENT_ID` | Mobile native Google sign-in | — |
| `AUTH_APPLE_CLIENT_ID` / `TEAM_ID` / `KEY_ID` | Apple Sign-In | — |
| `AUTH_APPLE_PRIVATE_KEY` / `AUTH_APPLE_PRIVATE_KEY_PATH` | `.p8` key, inline PEM or file path | — |
| `AUTH_APPLE_REDIRECT_URL` | Apple OAuth callback | — |
| `AUTH_GITHUB_CLIENT_ID` / `SECRET` / `REDIRECT_URL` | GitHub OAuth | — |
| `AUTH_DISCORD_CLIENT_ID` / `SECRET` / `REDIRECT_URL` | Discord OAuth | — |
| `COOKIE_NAME` / `COOKIE_DOMAIN` / `COOKIE_SECURE` | Refresh-token cookie attributes | set in `cfg.Auth.Cookie` |
| `APP_NAME` / `SUPPORT_EMAIL` | Rendered into verification/reset email templates | `Orkestra` / empty |

## HTTP endpoints

Registered from two handlers — `auth_handler.go` for OAuth/session/refresh, `password_handler.go` for password flows.

### Public (no auth required)

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/auth/oauth/login` | Start an OAuth flow, return the provider URL + state token |
| POST | `/v1/auth/google/mobile` | Exchange a Google ID token from a mobile app for an Orkestra session |
| POST | `/v1/auth/apple/mobile` | Exchange an Apple ID token from a mobile app for an Orkestra session |
| GET | `/v1/auth/oauth/google/callback` | Web OAuth callback (raw HTTP, not Huma) |
| GET | `/v1/auth/oauth/discord/callback` | Web OAuth callback (raw HTTP) |
| POST | `/v1/auth/oauth/apple/callback` | Apple returns form-post, not a redirect (raw HTTP) |
| GET | `/v1/auth/oauth/github/callback` | GitHub web OAuth callback (Huma-registered) |
| POST | `/v1/auth/register` | Email+password signup |
| POST | `/v1/auth/login` | Email+password login |
| POST | `/v1/auth/verify-email` | Consume a verification token |
| POST | `/v1/auth/verify-email/resend` | Request a new verification email |
| POST | `/v1/auth/forgot-password` | Send a password reset email |
| POST | `/v1/auth/reset-password` | Consume a reset token and set a new password |
| POST | `/v1/auth/refresh` | Refresh using a header-supplied refresh token |
| POST | `/v1/auth/refresh-cookie` | Refresh using the `Cookie:` header |
| GET | `/v1/auth/session` | Poll for session after OAuth redirect finishes |
| POST | `/v1/auth/logout` | Revoke refresh cookie, invalidate session |

### Protected (bearer access token required)

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/v1/auth/me` | bearer | Return the current authenticated user |
| POST | `/v1/auth/change-password` | `RequireGlobal()` | Self-service password change |

`change-password` is deliberately global (no org context) because it's a user-level self-service flow (`module.go:237-241`).

## Service contract

No single interface is exposed from this module — its concrete services are consumed from the registry by type. The one published interface is:

- **`iface.JWTProvider`** (`shared/iface/interfaces.go:56-62`) — just `GenerateAccessToken(user *User) (string, error)`. Consumed by the dev module to mint test tokens.

Everything else (`services.AuthService`, `services.JWTService`, `services.PasswordService`, `services.PasswordAuthService`) is fetched with `MustGetTyped[*services.X]` by `cmd/server/main.go` or by middleware. This is intentional — the surface is too broad to pin as an interface today.

## Key invariants

- **JWT payload shape.** Access tokens carry: `sub`, `email`, `srole` (the global system role), `memberships` (an array of `{orgId, orgName, orgSlug, roles[]}` fetched via `TenantProvider.ListUserMemberships` at issue time). **Permissions are not embedded** — they are resolved per-request by middleware calling `authz.HasPermission`. This is the most important thing to remember about the authentication architecture: roles are coarse-grained and cached in the JWT, permissions are fine-grained and resolved fresh.
- **First-user heuristic.** `password_auth_service.go::Register` (`:116-121`), `RegisterInitialAdmin` (`:177`), and `auth_service.go::OAuth register` all check `GetUserCount(ctx, nil) == 0` and assign `super_admin` to the first account created on a fresh install. The setup wizard's `POST /v1/setup/admin` uses `RegisterInitialAdmin` which also bypasses email verification.
- **Email verification is gated by `AUTH_REQUIRE_EMAIL_VERIFICATION`.** `true` in production, `false` elsewhere. When true, signup returns 503 with `ErrNotificationDown` if the notification sender is missing or reports `IsConfigured() == false`. `RegisterInitialAdmin` (setup wizard path) bypasses verification entirely because the wizard runs before SMTP is configured.
- **Refresh tokens rotate on every use.** Stored as a hash in `auth_refresh_tokens`. On refresh, the old token is marked revoked and a new pair is issued. Reuse of a revoked token is a reuse-attack signal — the service-level behavior is to revoke the whole session (see `refresh_token_repository.go`).
- **Session per device.** `AuthSession` binds a session to a `deviceId` + fingerprint. Refresh tokens link back to their session — revoking a session cascades to every token issued from it.
- **Email token TTL is 24 hours.** Enforced by the `auth_email_tokens.expiresAt` TTL index (`module.go:71`). The service also compares expiry on read in case the TTL sweeper is behind.
- **OAuth state is 10 minutes in Redis.** Validated before code exchange in every provider's callback handler.
- **Rate limiting** lives in `shared/errors.RateLimiter` and is shared across `Register`, `Login`, `ForgotPassword`, `VerifyEmailResend`. Current defaults are hardcoded — when you need to tune them, do it in `password_auth_service.go` and not in the handler.
- **Notification idempotency.** Verification and reset emails always carry an idempotency key like `verify:<userUUID>:<tokenUUID>` and `reset:<userUUID>:<tokenUUID>` so retries don't dispatch duplicates.
- **Password policy.** Minimum 10 characters, HIBP breach check via the password service. The service rejects `"password has appeared in a known data breach"` — observed in dev when the initial admin used a common test string.

## What this module does NOT do

- User profile CRUD or the system-role field → **user** module
- Org membership, invite lifecycle, plan entitlements → **tenant** module
- Permission evaluation, role bindings, system role seeding → **authz** module
- Rendering and sending emails → **notification** module (auth just passes `TemplatedNotificationRequest`)
- MFA / TOTP / WebAuthn — reserved in the JWT claim set, not implemented in v1
- OAuth token refresh against the provider — only the user's Orkestra session is refreshed; provider access tokens are not persisted long-term

## Rules

- **Never store a plaintext refresh or email token.** Always hash-and-compare. Tokens are returned to the caller exactly once per issue.
- **Never embed permissions in the JWT.** If you find yourself wanting to, you need a faster `HasPermission` — not a fatter token. Revocation must be instant.
- **Never call `notification.EmailSender.Send` directly.** Every auth-triggered email must go through `SendTemplated` with a `TemplateID` that exists in `notification/services/default_templates.go`.
- **Never read `cfg.Auth.JWT.PrivateKey` outside the JWT service.** Key material stays inside one package.
- **Never bypass the rate limiter on login / forgot-password endpoints.** The limiter is the only protection against credential stuffing and reset-flood.
- **When you add a new OAuth provider**, wire it through the factory in `module.go:94-132`, not directly in the handler. Factory patterns mean the callback handler stays generic.
- **Every new auth-adjacent collection needs a deliberate TTL decision.** Email tokens have TTLs because they're user-initiated. Sessions do not because they're invalidated explicitly. Don't copy-paste one into the other.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — consumed via `UserProvider` for every flow
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — consumed via `TenantProvider` for membership embedding in JWTs
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — consumed via `AuthzProvider` for permission checks in middleware
- [`../notification/CLAUDE.md`](../notification/CLAUDE.md) — optional dependency for verification + reset emails
- [`../../shared/middleware/auth.go`](../../shared/middleware/auth.go) — JWT validation, `RequirePermission`, `RequireGlobal`
- [`../../../../docs/Authentication_flow.md`](../../../../docs/Authentication_flow.md) — high-level walkthrough of the flows
