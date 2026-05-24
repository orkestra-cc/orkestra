# Module: User — Global accounts, profiles, system role

_Path: `/backend/internal/core/user`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Core](../CLAUDE.md) | [☰ Backend](../../../CLAUDE.md) | [Root](../../../../CLAUDE.md)

## Purpose

Owns the per-tier user collections (`operator_users` and `client_users` after the ADR-0003 PR-D D-8 cutover): account identity, profile fields, the global **system role**, OAuth link bookkeeping, and driver-document expiry tracking. Exposes three providers via the registry — `ServiceOperatorUserProvider`, `ServiceClientUserProvider`, and the canonical `ServiceUserService` (which points at the operator-tier provider) — so auth, authz, tenant and anything else that needs to look up a user depends on the interface rather than this package.

Does not touch passwords, sessions, JWTs, or org memberships — those belong to auth and tenant respectively.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration, collection spec, permissions catalog, service registration |
| `routes.go` | Huma route registration (13 endpoints) |
| `handlers/user_handler.go` | HTTP handler implementations |
| `services/user_service.go` | User CRUD business logic, role queries, document expiry helpers |
| `repository/user_repository.go` | MongoDB persistence — upsert, find, filter, soft-delete |
| `models/user.go` | `User` struct + `CreateUserInput`, `UpdateUserInput`, `UserFilters`, `UserManagementResponse` |

## MongoDB collections

| Collection | Indexes | TTL |
|---|---|---|
| `operator_users` | `uuid` unique, `email` unique, `tier` | — |
| `client_users` | `uuid` unique, `email` unique, `tier` | — |

Declared in `module.go::Collections()`. Email uniqueness is scoped per collection — the same address may legitimately exist as both an operator and a client account (one human running an internal staff role and an external client account). The repository stamps `tier="operator"` / `tier="client"` on every insert so a tier-guard test can assert each collection only holds rows of its own tier.

## Dependencies

- **Modules**: none (this is a leaf).
- **Required services**: none.
- **Optional services**: none.
- **Provides**: `ServiceUserService` (canonical, operator-tier) + `ServiceOperatorUserProvider` + `ServiceClientUserProvider` → `iface.UserProvider`.
- **Permissions contributed** (`module.go:48-55`):

| Key | Purpose |
|---|---|
| `user.read` | List users |
| `user.update` | Update user profiles |
| `user.delete` | Delete users |
| `user.self` | Edit your own profile |
| `user.avatar.self` | Manage your own avatar (upload, pick from linked OAuth provider, reset to initials) |

These permissions gate the module's own HTTP endpoints. Note that the current `RegisterRoutes` actually gates every admin route behind **`system.users.admin`** (a system permission contributed by authz), so `user.*` permissions are currently granted to managers/operators via system roles but not directly enforceable on most HTTP surfaces. The avatar endpoints under `/v1/me/avatar/*` are the exception — they're gated by `RequireGlobal()` (any authenticated user) because owner-self is asserted by the handler from the JWT user UUID. Future work: wire per-route `RequirePermission("user.read")` etc. and let the authz role matrix do the rest.

## Lifecycle

- **Init**: constructs both per-tier user repositories and matching OAuth provider repositories (operator + client) from the auth package, wires the per-tier `UserService` instances, and registers each under `ServiceOperatorUserProvider` / `ServiceClientUserProvider`. The operator-tier provider is also registered under the canonical `ServiceUserService` key — that's what unaware consumers (setup wizard, dev token generator) get by default; audience-aware consumers (onboarding) request the per-tier key directly.
- **Start / Stop / HealthCheck**: inherit the no-op from `BaseModule`.
- **Seeding**: none. Users are created by the auth module's registration flows or the setup wizard.

## HTTP endpoints

All routes are behind `RequireSystemPermission("system.users.admin")` (`module.go:70-74`) — in practice this means only `super_admin`, `administrator`, or `developer` system roles can hit them today.

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/users` | Create a new user |
| GET | `/v1/users` | Paginated list with optional filters (role, email verified, search, expired-docs) |
| GET | `/v1/users/{id}` | Get user by UUID |
| PUT | `/v1/users/{id}` | Update user profile |
| DELETE | `/v1/users/{id}` | Soft-delete user |
| GET | `/v1/users/count` | Count users with optional filters |
| GET | `/v1/users/by-email?email=` | Look up user by email |
| GET | `/v1/users/role/{role}` | Users with a specific system role |
| GET | `/v1/admin/client-users` | List Tier-2 client users with tenant memberships joined (powers `/admin/clients`) |
| GET | `/v1/admin/client-users/{id}` | Single Tier-2 client user with memberships + OAuth providers |
| POST | `/v1/admin/client-users` | Admin-direct create of a client_users row, password hashed against the live policy, EmailVerified=true |
| POST | `/v1/admin/client-users/invite` | Invite-flow create: row with no password, 7-day `admin_invite` email-token sent. Recipient redeems via `/v1/auth/client/accept-invite` |
| POST | `/v1/admin/client-users/{id}/invite/resend` | Re-emit the invite email (invalidates prior unused invite token) |
| POST | `/v1/admin/client-users/{id}/resend-verification` | Admin-trigger variant of resend verification — surfaces real errors instead of the public flow's silent return |
| POST | `/v1/admin/client-users/{id}/send-password-reset` | Admin-trigger variant of forgot-password — same enumeration-safe primitive but signals 404 / 503 directly to the operator |
| PATCH | `/v1/admin/client-users/{id}` | Update name / username / email / phone / role / isActive on a client user |
| DELETE | `/v1/admin/client-users/{id}` | Soft-delete + email alias on a client user (reuses `SoftDeleteAndAliasEmail`) |

The self-service avatar surface lives outside the admin gate — mounted on **both** operator and client protected routers under `RequireGlobal()`:

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/me/avatar/presign-upload` | Mint a short-lived presigned PUT URL for the SPA to upload directly to S3-compatible storage (RustFS / MinIO / AWS S3). Cap 2 MiB; MIME ∈ {png, jpeg, webp}. Backend chooses the object key `avatars/{tier}/{userUUID}/{uuidv7}.{ext}` |
| POST | `/v1/me/avatar/commit` | HEAD the freshly-uploaded blob, set `User.AvatarSource=uploaded`, GC the previously-stored object key |
| PATCH | `/v1/me/avatar/source` | Switch source to `initials` or to a linked OAuth provider's picture (`oauth_google/apple/github/discord`). Validates the OAuth link is active — returns 422 `oauth_provider_not_linked` otherwise |

Backed by `handlers/AvatarHandler` (one instance per tier, bound to that tier's `UserService` and a shared `blob.Store`). The pipeline is three-step so image bytes go directly to storage without proxying through Go — see `internal/shared/blob/CLAUDE-ish.go` (package doc) for the S3-compat contract.

Full admin registration in `routes.go`. The `/v1/admin/client-users[/{id}]` family is implemented by `handlers/admin_client_handler.go` (the `AdminClientUserHandler`). It binds to the **client-tier** `UserService` directly, looks up `iface.TenantProvider` lazily from the registry to join memberships, looks up `iface.PasswordHasher` lazily on create so it can hash the supplied password without importing auth's package, and looks up `iface.AdminAuthInviter` (satisfied by the client-tier `*services.PasswordAuthService`) for the invite / resend-verification / send-password-reset endpoints.

The companion tier-aware MFA reset is mounted by the auth module at `POST /v1/admin/client-users/{userId}/mfa/reset` — see [`../auth/CLAUDE.md`](../auth/CLAUDE.md).

## Service contract

`iface.UserProvider` (`shared/iface/interfaces.go:28-53`) is the boundary. Consumers must go through this, never the concrete `services.UserService`.

Key method groups:

- **Identity / lookup** — `GetUserByID`, `GetUserByEmail`, `GetUserForAuth` (includes password hash + lockout fields; auth-only), `GetUserCount`
- **Creation** — `CreateUserWithPassword` (called by password signup), `CreateUserFromOAuth` (called by OAuth flows)
- **Auth-side mutations** — `UpdatePasswordHash`, `MarkEmailVerified`, `RecordFailedLogin` (optional `lockUntil`), `ClearFailedLogins`, `UpdateUserLastLogin`, `StartMFAGraceIfUnset` (idempotent — preserves an existing clock), `ResetMFAGrace` (unconditionally restarts — used by admin MFA reset), `ClearMFAGrace` (wipe on successful enrollment)
- **OAuth link management** — `GetUserOAuthLinks`, `AddOAuthLinkToUser`, `RemoveOAuthLinkFromUser`, `SetPrimaryOAuthLink`
- **General mutation** — `UpdateUser`, `DeleteUser`

`GetUserForAuth` returns the full `*User` including the password hash. Every other read path returns `*UserManagementResponse` which strips sensitive fields — use the right one.

## Key invariants

- **System role is global, not org-scoped.** `User.Role` is one of `super_admin`, `administrator`, `developer`, `manager`, `operator`, `guest` — the same value the JWT carries as the `srole` claim. Org-scoped roles live in the authz module as role bindings and have nothing to do with this field.
- **`NewUser()` defaults the role to `operator`** (`models/user.go:289`). The first user created on a fresh install is bumped to `super_admin` by the auth module's first-user heuristic — this module is agnostic.
- **Validator `oneof`** in `user.go:77,120,143,193` enforces the six role names on every create/update/filter DTO. If you rename a role, update all four lines in lock-step.
- **Email uniqueness** is enforced at the DB level by the unique index plus at the service level by a pre-insert existence check. Concurrent creates with the same email will have one succeed and one error.
- **Soft delete only** — `DeleteUser` sets `DeletedAt` on the document. The unique email index still matches soft-deleted rows, so reactivating a soft-deleted account requires either a hard delete or a permanent email alias. `SoftDeleteAndAliasEmail` is the alias path: it stamps `deletedAt` AND atomically rewrites `email` to `<original>+deleted-<unixNano>@orphan.local` (preserving the original on `originalEmail` for audit) so the unique index frees up. Used by the tenant cascade hook for orphaned external (Tier-2) owners — internal users are intentionally never aliased because operator humans outlive single workspaces.
- **`User.MFAGraceStartedAt` is stamped by the auth module**, cleared by the auth module (on successful enrollment), and read by both auth (to decide login grace vs 403) and the admin MFA reset flow (which calls `ResetMFAGrace` to restart the countdown). The field is non-serialized (`json:"-"`) — it's internal bookkeeping, not part of the public user surface.
- **`OAuthLinks` is embedded in the user document**, not a separate collection. The auth module has its *own* `auth_oauth_providers` collection for provider-side metadata (IDs, tokens). The two are synced but serve different roles: `User.OAuthLinks` is the "connected accounts" list surfaced to the user; `auth_oauth_providers` is the provider-lookup index used during OAuth callback.

## What this module does NOT do

- Password hashing, verification, sessions, refresh tokens, JWT issuance, MFA → **auth** module
- Org membership, "which orgs does this user belong to" → **tenant** module
- Org-scoped role assignment, permission checks → **authz** module
- Email delivery (verification, reset, notifications) → **notification** module
- Object storage of avatar blobs → `internal/shared/blob/` (`blob.Store` interface + S3-compatible impl). This module consumes the store via `module.ServiceBlobStore` and uses `blob.ResolveAvatarURL` for every read path (`enrichWithOAuthProviders`); a missing store leaves uploaded avatars unrendered but OAuth-source + initials keep working.

## Rules

- **Never import another module's `services/` or `repository/` package.** If you need something from auth or tenant, it should come through a `shared/iface` interface or be inverted so that module calls you.
- **Never hardcode a role string** outside of the validator tags. Use the seeded role names as constants if you add new helpers — future role renames must be a single-grep operation.
- **Use `GetUserForAuth` only from auth flows.** It returns password hashes — every other path must use `GetUserByID` / `GetUserByEmail` which return the scrubbed response DTO.
- **Don't extend the HTTP surface for self-service flows** (password change, email verify). Those live on the auth module so the rate limiter and notification dependency are in scope.
- **Every new field on `User` must be reflected in `UserManagementResponse`** (or deliberately scrubbed). Forgetting breaks the admin UI without a test failure.

## Related

- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — consumes `UserProvider` for every flow
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — reads `User.Role` via the same provider to honor the super_admin/administrator/developer shortcut in permission evaluation
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — depends on `user` to verify that invited userUUIDs exist before creating memberships
- [`../../shared/iface/interfaces.go:28-53`](../../shared/iface/interfaces.go) — `UserProvider` interface definition
- [Authentication flow](../../../../docs/Authentication_flow.md) — how `User.Role` threads through JWT claims and middleware
