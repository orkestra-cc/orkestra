<div align="center">

# Orkestra OpenAPI auth

**Shared OAuth-token minter for [openapi.it](https://oauth.openapi.it) products. Exchanges (account email, API key) HTTP Basic credentials for a short-lived JWT bearer with a caller-specified scope list, cached two-tier (in-process + Redis) so replicas reuse the same JWT until shortly before it expires.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-openapi-auth.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-openapi-auth)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--openapi--auth-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-openapi-auth)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-openapi-auth?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-openapi-auth/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## What this is

A stdlib-only Go helper that turns long-lived OpenAPI.com credentials into short-lived JWT bearers. Used inside the Orkestra modular monolith by two addons:

- [`orkestra-cc/orkestra-addon-company`](https://github.com/orkestra-cc/orkestra-addon-company) — company-registry lookups against `company.openapi.com`
- [`orkestra-cc/orkestra-addon-billing`](https://github.com/orkestra-cc/orkestra-addon-billing) (future) — FatturaPA/SDI integration against `sdi.openapi.com`

Both addons construct one `Minter` per product, configured with the scope list the product accepts at `/token`. The minted JWT lives in an in-process cache plus (optionally) Redis, so concurrent and cross-replica requests reuse the same token until it nears expiry.

## How it ships

The same source tree lives in two places:

- **In-tree** at [`backend/internal/shared/openapiauth/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/internal/shared/openapiauth) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo, where cross-module development happens.
- **Standalone** at this repository, tagged from `v0.1.0`, consumed via the Go module proxy by anything outside the monorepo.

The upstream backend's `go.mod` looks roughly like this:

```go
require github.com/orkestra-cc/orkestra-openapi-auth v0.1.0
replace github.com/orkestra-cc/orkestra-openapi-auth => ./internal/shared/openapiauth
```

The `replace` will retire once cross-cutting addon churn settles.

## Install

```bash
go get github.com/orkestra-cc/orkestra-openapi-auth@latest
```

Requires Go 1.25.10 or newer. Zero non-stdlib dependencies.

```go
import "github.com/orkestra-cc/orkestra-openapi-auth"
```

## Usage

```go
import (
    "context"
    "log/slog"
    "net/http"
    "os"

    openapiauth "github.com/orkestra-cc/orkestra-openapi-auth"
)

cfg := openapiauth.Config{
    AccountEmail: os.Getenv("OPENAPI_COMPANY_ACCOUNT_EMAIL"),
    APIKey:       os.Getenv("OPENAPI_COMPANY_API_KEY"),
    OAuthBaseURL: "https://oauth.openapi.it",                  // or test.oauth.openapi.it
    Scopes:       []string{"GET:company.openapi.com/IT-start/*"},
    TTL:          31536000,                                     // seconds — defaults to 1 year if 0
    Tag:          "company",                                    // disambiguates the cache key
}

// 2nd arg: any Get/Set Redis-shaped Cache implementation (or nil for in-process only).
// 3rd arg: optional http.Client (nil → sensible default with 15s timeout).
// 4th arg: optional *slog.Logger (nil → slog.Default()).
minter := openapiauth.NewMinter(cfg, nil, nil, slog.Default())

token, err := minter.Token(ctx)
if err != nil {
    // ErrMissingCredentials / ErrUpstreamAuth / ErrUpstreamUnreachable / ErrUpstreamMalformed
}

req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://company.openapi.com/IT-start/12345", nil)
req.Header.Set("Authorization", "Bearer "+token)

// After an upstream 401/403 — invalidate so the next Token() mints fresh:
minter.Invalidate(ctx)
```

The `Cache` interface is intentionally tiny (`Get`/`Set` with TTL) so existing per-module Redis clients satisfy it directly. Each product (`Tag`) gets its own cache slot, so a `company` minter and a `billing` minter sharing the same Redis never cross-pollinate.

## Errors

| Sentinel | Meaning |
|---|---|
| `ErrMissingCredentials` | `AccountEmail` or `APIKey` empty |
| `ErrUpstreamAuth` | 401/403 from `/token` — operator must rotate the API key |
| `ErrUpstreamUnreachable` | Network failure / non-2xx with empty body |
| `ErrUpstreamMalformed` | 2xx with malformed JSON or missing token field |

Callers typically map `ErrUpstreamAuth` to an operator-visible alert and the rest to a transient 502.

## Versioning

Standard Go semver. `v0.x` allows breaking changes (rare); `v1.x` will freeze the public surface alongside the rest of the Orkestra addon ecosystem.

## Contributing

Most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where this helper lives alongside its consumers — so a change can be implemented, tested, and reviewed against real callers in one diff. PRs against this standalone repo are welcome for helper-only changes.

See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
