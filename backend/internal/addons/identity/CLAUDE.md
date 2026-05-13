# Module: Identity (BYO OIDC + SCIM 2.0)

_Path: `/backend/internal/addons/identity`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

[← Backend](../../../CLAUDE.md) | [☰ Module Map](../../../../../CLAUDE.md#module-map)

## Module home

This directory is a **separate Go module**
(`github.com/orkestra-cc/orkestra-addon-identity`) since Phase 5k of
the SDK split. Source lives in-tree at this path for monorepo
development; the same tree is mirrored to
[github.com/orkestra-cc/orkestra-addon-identity](https://github.com/orkestra-cc/orkestra-addon-identity)
and tagged starting from `v0.1.0`. Backend's `go.mod` carries a
`replace` directive pointing at this path so changes here take effect
without a tag bump during cross-cutting work; CI and external
consumers fetch the published version through the Go module proxy.

### Boundary lifts that unblocked this extraction

Two iface lifts landed in the SDK at v0.4.0 to break identity's
concrete-type dependencies on core/auth:

- **`iface.LoginTokens`** — the SDK-side shape of a freshly minted
  Orkestra session (AccessToken / RefreshToken / TokenType / ExpiresIn
  / User). Subset of `auth/models.TokenResponse` — only the fields an
  external flow needs back.
- **`iface.LoginTokenIssuer`** — `IssueLoginTokensExternal(ctx, user, deviceID, platform, ip, amr) (*LoginTokens, error)`.
  `core/auth.PasswordAuthService` gained a thin wrapper method that
  returns the iface shape; the existing `IssueLoginTokens` keeps
  returning `*authModels.TokenResponse` for internal callers
  (webauthn / mfa handlers).

Identity probes `module.GetTyped[iface.LoginTokenIssuer]` and calls
`IssueLoginTokensExternal` from the OIDC callback path. No auth
package import.

### Inlined utility helpers

The handful of `internal/shared/utils` helpers identity used
(EncryptOAuthToken / DecryptOAuthToken / SecureRandomString /
GenerateState / GenerateNonce / GetClientIP) were inlined into the
addon's private `internal/cryptoutil` subpackage with **byte-for-byte
identical algorithms** — ciphertext written by either implementation
is interchangeable. The package lives under `internal/` (Go's
internal-package rule) so future addons that extract can't reach
across boundaries the same way; each adds its own copy or its own
iface contract.

The `userModels.{User,UserManagementResponse,CreateUserInput}` and
`authModels.TokenResponse` references became `iface.*` directly —
`core/user/models` is already a re-export shim around iface so the
swap was mechanical (same playbook as the Phase 5i dev extraction).

## What it does

The platform's BYO-identity-provider plane:

1. **Per-tenant OIDC** — each tenant configures their own IdP (Okta,
   Entra, Auth0, …) at `/admin/identity/idp`. Users sign in via
   `/v1/identity/oidc/{tenantSlug}/start`, the IdP issues an ID
   token, the callback verifies it (signature + audience + nonce via
   `coreos/go-oidc`), the user is found-or-created, and an Orkestra
   session is minted via `iface.LoginTokenIssuer`.
2. **SCIM 2.0** — `/scim/v2/*` accepts provisioning calls from the
   tenant's IdP. v1 ships the full metadata surface (ResourceTypes,
   Schemas, ServiceProviderConfig) plus stubbed CRUD; persistence is
   a follow-up.

## Tenant-scoped, not platform-wide

Every protected route operates on the caller's current tenant
context (`X-Tenant-ID`). Admin CRUD is gated by `tenant.update` so
only operators who can already manage the tenant can edit its IdP /
SCIM token. SCIM tokens are bound to a single tenant — the bearer
middleware resolves the tenant directly from the token (no JWT
auth).

## Collections

| Collection | Notes |
|---|---|
| `identity_idp_configs` | One row per (tenant, protocol). `tenantId + protocol` unique so a tenant can configure at most one OIDC + one SAML (future). |
| `identity_scim_tokens` | One active row per tenant — service-layer rotate-revokes-previous keeps the invariant. `tokenHash` non-unique so revoked rows survive for audit. |

## Endpoints

### Public (no auth required)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/identity/oidc/{tenantSlug}/start` | Begin an OIDC flow against the tenant's IdP |
| `GET` | `/v1/identity/oidc/callback` | Validate state, exchange code, mint session |

### Tenant-admin (gated by `tenant.update`)

| Method | Path | Purpose |
|---|---|---|
| `GET / PUT / DELETE` | `/v1/identity/idp` | Tenant's OIDC config (one per tenant) |
| `GET / POST / DELETE` | `/v1/identity/scim/tokens[/{id}]` | SCIM bearer token management |

### SCIM 2.0 (gated by bearer token only)

| Method | Path | Purpose |
|---|---|---|
| All standard SCIM verbs | `/scim/v2/{Users,Groups,ResourceTypes,Schemas,ServiceProviderConfig}` | RFC 7644 / 7643 surface |

## Service surface (published into ServiceRegistry)

| Key | Type | Purpose |
|---|---|---|
| `ServiceIdentityOIDCService` | `*services.Service` | The OIDC orchestrator — exposes `SetAuditSink` for compliance |
| `ServiceIdentityAdminHandler` | `*handlers.AdminHandler` | Tenant-admin CRUD — exposes `SetAuditSink` |
| `ServiceIdentityScimAdminHandler` | `*handlers.ScimAdminHandler` | SCIM token rotation — exposes `SetAuditSink` |

All three are receivers of compliance's `iface.AuditSinkSetter` probe
(see Phase 5j compliance CLAUDE.md). Identity does not import
compliance; the wiring direction is push-down from compliance.

## OIDC state model

State + nonce are HS256-signed JWTs persisted in Redis for 10 minutes
(matches the auth module's OAuth state TTL). Callback validates: (1)
state signature, (2) state freshness via Redis lookup, (3) ID token
signature against the IdP's JWKS, (4) audience, (5) nonce. Only on
all-pass does the user get found-or-created and the session minted.

## Not in v1

- SAML — protocol slot reserved in `IdPConfig.Protocol`; no handler.
- SCIM CRUD persistence — `Users` / `Groups` write paths return 501
  until the next phase wires them to the user / tenant providers.
- Per-tenant JWT signing — tokens are minted with the platform's
  shared key, scoped by the user's tenant memberships.
