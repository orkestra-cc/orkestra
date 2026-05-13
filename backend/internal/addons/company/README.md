<div align="center">

# Orkestra Company addon

**Italian business-registry lookup for the [Orkestra](https://github.com/orkestra-cc/orkestra) modular monolith. Resolves codice-fiscale / partita-IVA holders against the OpenAPI Company API (`company.openapi.com`), persists each result in MongoDB with a 24h Redis cache in front, and exposes enrichment endpoints for advanced / marketing / stakeholders / AML data sets.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-addon-company.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-addon-company)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--addon--company-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-company)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-addon-company?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-company/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[SDK](https://github.com/orkestra-cc/orkestra-sdk) · [OpenAPI auth](https://github.com/orkestra-cc/orkestra-openapi-auth) · [Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## What this is

A self-contained Orkestra addon implementing the `Module` interface from [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk). It owns the `company_lookups` MongoDB collection, exposes `/v1/company/*` lookup + enrichment endpoints, and gates every route on the `company.lookup.read` permission. The addon depends on [`orkestra-cc/orkestra-openapi-auth`](https://github.com/orkestra-cc/orkestra-openapi-auth) for the OAuth-token minter that exchanges (account email, API key) Basic credentials for short-lived JWT bearers at `oauth.openapi.it/token`.

**Endpoints** (mounted under `/v1/company` on the operator surface):

| Method | Path | Description |
|---|---|---|
| GET | `/lookup/{taxCode}` | Look up a company by tax code (Mongo → Redis → external API) |
| GET | `/lookup/{taxCode}/enrich/{type}` | Fetch enrichment (`advanced` / `marketing` / `stakeholders` / `aml` / `full`) |
| GET | `/lookups` | Paginated list of previously looked-up companies |
| GET | `/lookups/search` | Search stored lookups by name / tax code / VAT code |
| GET | `/lookups/{id}` | Get a stored lookup by UUID |

## How it ships

The same source tree lives in two places:

- **In-tree** at [`backend/internal/addons/company/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/internal/addons/company) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo, where cross-module development happens.
- **Standalone** at this repository, tagged from `v0.1.0`, consumed via the Go module proxy by anything outside the monorepo.

The upstream backend's `go.mod` looks roughly like this:

```go
require github.com/orkestra-cc/orkestra-addon-company v0.1.0
replace github.com/orkestra-cc/orkestra-addon-company => ./internal/addons/company
```

The `replace` will retire once cross-cutting addon churn settles.

## Install

```bash
go get github.com/orkestra-cc/orkestra-addon-company@latest
```

Requires Go 1.25.10 or newer.

```go
import company "github.com/orkestra-cc/orkestra-addon-company"
```

## Boot in a host

The addon plugs into any host that implements the Orkestra SDK kernel. Register it alongside the other catalog entries:

```go
import (
    company "github.com/orkestra-cc/orkestra-addon-company"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

reg := module.NewModuleRegistry(logger) // your kernel's *slog.Logger
reg.Register(company.NewModule())
```

The registry takes care of init order (company has no hard dependencies), Mongo collection creation, route mounting on the operator surface, RBAC enforcement (`company.lookup.read`), and module-gate middleware for runtime enable/disable via `/admin/modules`.

## Configuration

The addon activates when either OAuth credentials OR the legacy static bearer is set; the OAuth path is preferred so tokens rotate automatically.

| Env var (seeds first-install) / admin field | Purpose |
|---|---|
| `OPENAPI_COMPANY_ACCOUNT_EMAIL` | Account email for OAuth `/token` Basic auth |
| `OPENAPI_COMPANY_API_KEY` | Long-lived API key from console.openapi.com |
| `OPENAPI_OAUTH_BASE_URL` | OAuth host (default `https://oauth.openapi.it`; sandbox `https://test.oauth.openapi.it`) |
| `OPENAPI_COMPANY_BEARER_TOKEN` | Legacy static JWT used only when API Key is empty |
| `OPENAPI_COMPANY_BASE_URL` | Override `https://company.openapi.com` for sandbox |
| `OPENAPI_COMPANY_TIMEOUT` | HTTP request timeout (default 15s) |
| `OPENAPI_COMPANY_CACHE_TTL` | Redis cache TTL for lookup results (default 24h) |

All fields are also editable at runtime under `/admin/modules/company`, with AES-256-GCM at-rest encryption for the API-key and bearer-token fields. The admin UI's edit form is rendered from this module's `ConfigSchema()` declaration.

## Resilience

- **Circuit breaker** — 5 consecutive failures → 30 s open → half-open probe.
- **Exponential retry** — 1s/2s/4s on 5xx and network errors; 4xx errors short-circuit.
- **Redis cache** — 24h TTL on the `company:lookup:{taxCode}` key blunts API spend (free tier is 30 req/month).
- **Mongo persistence** — every successful lookup is stored, so historical reads never round-trip the upstream API again.

See [`CLAUDE.md`](CLAUDE.md) for the per-package map, enrichment endpoint contracts, MongoDB indexes, and the full external-API flow.

## Versioning

Standard Go semver. `v0.x` allows breaking changes (rare); `v1.x` will freeze the public surface alongside the rest of the Orkestra addon ecosystem.

## Contributing

Most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where this addon lives alongside the kernel and its consumers — so a change can be implemented, tested, and reviewed against real callers in one diff. PRs against this standalone repo are welcome for addon-only changes (enrichment additions, bug fixes scoped to this tree).

See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
