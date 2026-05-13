# Orkestra OpenAPI auth — shared minter for openapi.it bearers

_Path: `/backend/internal/shared/openapiauth`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

## Module home

This directory is a **separate Go module**
(`github.com/orkestra-cc/orkestra-openapi-auth`) since Phase 5c of the
SDK split. Source lives in-tree at this path for monorepo development;
the same tree is mirrored to
[github.com/orkestra-cc/orkestra-openapi-auth](https://github.com/orkestra-cc/orkestra-openapi-auth)
and tagged starting from `v0.1.0`. Backend's `go.mod` carries a
`replace` directive pointing at this path so changes here take effect
without a tag bump during cross-cutting work; CI and external
consumers fetch the published version through the Go module proxy.

The package was previously `github.com/orkestra/backend/internal/shared/openapiauth`.
Phase 5c carved it out because the `company` addon (extracted in the
same phase) needs to import it, and Go's `internal/` rule made that
impossible across the new module boundary. The `billing` addon (still
in-tree at the time of writing) also imports the new public path so
its eventual extraction is unblocked.

## What it does

Exchanges (account email, API key) HTTP Basic credentials for a
short-lived JWT bearer at `oauth.openapi.it/token`, with a
caller-specified scope list. The minted JWT is what every downstream
`Authorization: Bearer …` request to `*.openapi.com` expects.

- **Per-product Minters** — callers construct one `Minter` per OpenAPI
  product (currently `company`, `billing`), each with the scope list
  relevant to that product.
- **Two-tier cache** — minted tokens are cached in-process for the
  hot path, and (when a `Cache` is provided) in Redis so replicas
  reuse the same JWT until shortly before it expires.
- **401/403 invalidation** — an upstream auth-rejection invalidates
  the cached JWT so the next attempt mints fresh, which matters when
  the operator rotates the API key in `/admin/modules`.
- **Stdlib-only** — zero non-stdlib imports keep the module trivially
  embeddable and audit-friendly.

## Configuration

`Config` fields:

| Field | Purpose | Default |
|---|---|---|
| `AccountEmail` | OpenAPI.com account email | _(required)_ |
| `APIKey` | Long-lived API key from console.openapi.com | _(required)_ |
| `OAuthBaseURL` | OAuth host | `https://oauth.openapi.it` |
| `Scopes` | List passed to `/token` (e.g. `GET:company.openapi.com/IT-start/*`) | _(required)_ |
| `TTL` | Lifetime requested when minting, in seconds | 31536000 (1 year) |
| `Tag` | Disambiguates the cache key when multiple products share a Redis | _(required)_ |

The Redis cache and HTTP client are passed as separate parameters to
`NewMinter`, not on `Config`. Pass `nil` for the cache to fall back to
in-process caching only; pass `nil` for the HTTP client to get a
sensible default (15s timeout).

## Errors

| Sentinel | Meaning |
|---|---|
| `ErrMissingCredentials` | `AccountEmail` or `APIKey` empty |
| `ErrUpstreamAuth` | 401/403 from `/token` — operator must rotate the key |
| `ErrUpstreamUnreachable` | Network failure / non-2xx with no body |
| `ErrUpstreamMalformed` | 2xx with malformed JSON or missing token field |

## Tests

```bash
cd backend/internal/shared/openapiauth
go test ./...
```

The test suite mocks `oauth.openapi.it` with `httptest.Server` and
exercises happy-path mint, in-process cache hit, Redis cache hit,
401 invalidation, and concurrent mint coalescing.
