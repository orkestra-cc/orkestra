# Orkestra OpenAPI auth — shared minter for openapi.it bearers

_Path: `/backend/internal/shared/openapiauth`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

## Module home

This is an in-tree package of the single backend Go module, imported as
`github.com/orkestra/backend/internal/shared/openapiauth`. (ADR-0006 D2
folded the SDK split back into one module; the brief stint as a separate
`github.com/orkestra-cc/orkestra-openapi-auth` module is reverted.)

It is the OpenAPI.it OAuth-token minter the removed `billing` and
`company` addons used. **No module in the core base consumes it** — it is
kept as a ready-made helper for a fork that re-adds an OpenAPI.it-backed
vertical (copy from the archived `orkestra-cc/orkestra-addon-billing` /
`-company` snapshots).

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
