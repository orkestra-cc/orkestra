<div align="center">

# Orkestra Identity addon

**Per-tenant Bring-Your-Own-OIDC login + SCIM 2.0 provisioning for the [Orkestra](https://github.com/orkestra-cc/orkestra) modular monolith. Each tenant configures their own IdP (Okta, Entra, Auth0, …); users sign in via `/v1/identity/oidc/{tenantSlug}/start` and the addon mints an Orkestra session through the SDK's `iface.LoginTokenIssuer` contract. SCIM 2.0 surface mounted at `/scim/v2/` with bearer-token-resolved tenant scope.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-addon-identity.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-addon-identity)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--addon--identity-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-identity)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-addon-identity?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-identity/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[SDK](https://github.com/orkestra-cc/orkestra-sdk) · [Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## What this is

A self-contained Orkestra addon implementing the `Module` interface from [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk). Owns two MongoDB collections (`identity_idp_configs`, `identity_scim_tokens`) and three route surfaces:

1. **Public OIDC flow** — `/v1/identity/oidc/{tenantSlug}/start` + `/v1/identity/oidc/callback`. Built on `coreos/go-oidc` so Orkestra never hand-rolls OIDC crypto.
2. **Tenant-admin** — `/v1/identity/idp` (OIDC config CRUD) + `/v1/identity/scim/tokens` (SCIM bearer rotation), gated by `tenant.update`.
3. **SCIM 2.0 protocol surface** — `/scim/v2/{Users,Groups,…}` mounted on the root router behind a bearer middleware that resolves the tenant from the token (no JWT auth).

The OIDC bridge into the platform uses **`iface.LoginTokenIssuer`** (added to the SDK at v0.4.0 alongside this extraction). Core/auth's `PasswordAuthService` exposes `IssueLoginTokensExternal` to satisfy the contract; identity never imports its concrete type.

## How it ships

The same source tree lives in two places:

- **In-tree** at [`backend/internal/addons/identity/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/internal/addons/identity) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo, where cross-module development happens.
- **Standalone** at this repository, tagged from `v0.1.0`, consumed via the Go module proxy by anything outside the monorepo.

```go
require github.com/orkestra-cc/orkestra-addon-identity v0.1.0
replace github.com/orkestra-cc/orkestra-addon-identity => ./internal/addons/identity
```

The `replace` will retire once cross-cutting addon churn settles.

## Install

```bash
go get github.com/orkestra-cc/orkestra-addon-identity@latest
```

Requires Go 1.25.10 or newer and `orkestra-cc/orkestra-sdk v0.4.0+` (for `iface.LoginTokenIssuer` / `iface.LoginTokens`).

```go
import identity "github.com/orkestra-cc/orkestra-addon-identity"
```

## Boot in a host

```go
import (
    identity "github.com/orkestra-cc/orkestra-addon-identity"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

reg := module.NewModuleRegistry(logger) // your kernel's *slog.Logger
reg.Register(identity.NewModule())
```

Identity declares `Dependencies()` of `["auth", "tenant"]` so the topo sort runs auth (which publishes `iface.LoginTokenIssuer` under `module.ServicePasswordAuthService`) and tenant (which publishes `iface.TenantProvider`) first. Identity's `Init()` `MustGetTyped`s both — boot will panic if either is missing, by design.

## What this addon expects from the host

| Service key | Required interface | Why |
|---|---|---|
| `ServicePasswordAuthService` | `iface.LoginTokenIssuer` | Mints the Orkestra session at the tail of an OIDC callback |
| `ServiceTenantProvider` | `iface.TenantProvider` | Resolves `{tenantSlug}` → `tenantUUID` for IdP config lookup |
| `ServiceUserService` | `iface.UserProvider` | Find-or-create user from the OIDC identity claims |

All three are satisfied by Orkestra's core modules out of the box.

## Configuration (per tenant)

OIDC config lives in `identity_idp_configs`, one row per (tenant, protocol). Edited via the admin surface; `clientSecret` is AES-256-GCM encrypted at rest with `OAUTH_TOKEN_ENCRYPTION_KEY`.

| Field | Notes |
|---|---|
| `displayName` | Human label for the admin UI |
| `issuerURL` | Discovery URL (no trailing slash) |
| `clientID` / `clientSecret` | OAuth client registered at the IdP |
| `redirectURL` | Absolute callback URL (typically `https://api.example.com/v1/identity/oidc/callback`) |
| `scopes` | Defaults to `openid email profile` |
| `subClaim` / `emailClaim` / `nameClaim` | Override for non-standard IdPs |
| `enabled` | When false, `/start` returns 404 — no info leak |

## SCIM tokens

Rotation-based: `POST /v1/identity/scim/tokens` mints a new bearer and revokes any previous active row for the tenant. The plaintext is returned exactly once. Service-layer enforces "one active row per tenant" — admin UI lists historical revoked rows for audit.

See [`CLAUDE.md`](CLAUDE.md) for the boundary-lift rationale, the OIDC state model, the cryptoutil inline-helper note, and the v1 scope boundary (SAML + SCIM persistence are follow-ups).

## Versioning

Standard Go semver. `v0.x` allows breaking changes (rare); `v1.x` will freeze the public surface alongside the rest of the Orkestra addon ecosystem.

## Contributing

Most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where this addon lives alongside the kernel and its consumers. PRs against this standalone repo are welcome for addon-only changes (new IdP claim mappings, SCIM resource types, etc.).

See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
