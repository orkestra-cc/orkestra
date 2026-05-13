# Module: Company Lookup - OpenAPI Company API

_Path: `/backend/internal/addons/company`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

<!-- Navigation -->

[← Backend](../../../CLAUDE.md) | [☰ Module Map](../../../../../CLAUDE.md#module-map)

<!-- /Navigation -->

## Module home

This directory is a **separate Go module**
(`github.com/orkestra-cc/orkestra-addon-company`) since Phase 5c of
the SDK split. Source lives in-tree at this path for monorepo
development; the same tree is mirrored to
[github.com/orkestra-cc/orkestra-addon-company](https://github.com/orkestra-cc/orkestra-addon-company)
and tagged starting from `v0.1.0`. Backend's `go.mod` carries a
`replace` directive pointing at this path so changes here take effect
without a tag bump during cross-cutting work; CI and external
consumers fetch the published version through the Go module proxy.

The addon depends on
[`orkestra-cc/orkestra-openapi-auth`](https://github.com/orkestra-cc/orkestra-openapi-auth)
for the OAuth-token minter that exchanges (account email, API key)
HTTP Basic credentials for short-lived JWT bearers at
`oauth.openapi.it/token`. That helper was previously imported from
`backend/internal/shared/openapiauth` and was carved out into its own
module in the same Phase 5c commit so this addon could cross the new
module boundary.

## Module Purpose

The company module provides **Italian company data lookup** by tax code (Codice Fiscale) or VAT number (Partita IVA) through integration with the OpenAPI Company API (`company.openapi.com`).

- **Primary Role**: Look up external company data for auto-filling customer/supplier details and company verification
- **External Integration**: OpenAPI Company API for real-time company information
- **Conditional Activation**: Module activates when either (a) `OPENAPI_COMPANY_ACCOUNT_EMAIL` + `OPENAPI_COMPANY_API_KEY` are configured (preferred — module mints + rotates JWTs), or (b) the legacy static `OPENAPI_COMPANY_BEARER_TOKEN` is set
- **Scope**: Italy only (`/IT-start/{taxCode}` endpoint)

## Module Structure

```
company/
├── config/
│   └── config.go                  # CompanyAPIConfig (BaseURL, token, timeouts)
├── models/
│   ├── company.go                 # CompanyLookup domain model (MongoDB document)
│   ├── dto.go                     # API response types + Huma request/response DTOs
│   └── enrichments.go             # Typed enrichment structs (Advanced, Marketing, etc.)
├── repository/
│   └── company_repository.go      # Interface + MongoDB impl (company_lookups collection)
├── services/
│   ├── company_service.go         # Interface + business logic (lookup + enrichment)
│   └── openapi_client.go          # External HTTP client (company.openapi.com)
├── handlers/
│   └── company_handler.go         # Huma HTTP handlers
├── routes.go                      # Route registration
└── CLAUDE.md                      # This file
```

## API Endpoints

| Method | Endpoint | Operation ID | Description |
|--------|----------|-------------|-------------|
| GET | `/v1/company/lookup/{taxCode}` | `lookup-company` | Look up company by tax code (checks MongoDB first, then external API) |
| GET | `/v1/company/lookups` | `list-company-lookups` | List previously looked-up companies (paginated) |
| GET | `/v1/company/lookups/search` | `search-company-lookups` | Search stored lookups by name/tax code/VAT code |
| GET | `/v1/company/lookups/{id}` | `get-company-lookup` | Get a specific stored lookup by UUID |
| GET | `/v1/company/lookup/{taxCode}/enrich/{type}` | `enrich-company-lookup` | Fetch enrichment data and store on existing lookup |

### Enrichment Types

| Type | API Endpoint | Description |
|------|-------------|-------------|
| `advanced` | `IT-advanced` | ATECO, PEC, REA, CCIAA, balance sheets, shareholders, VAT group |
| `marketing` | `IT-marketing` | Contacts (phone, email, website), social media, employee count |
| `stakeholders` | `IT-stakeholders` | Company officers and representatives |
| `aml` | `IT-aml` | Anti-money laundering checks |
| `full` | `IT-full` | All of the above in one call |

## Lookup Flow

```
Request → MongoDB Check → Redis Cache Check → External API Call → MongoDB Insert → Redis Cache Set → Response
              ↓ (hit)          ↓ (hit)
          Return stored    Return cached
```

1. **MongoDB** checked first for existing lookup by `taxCode`
2. **Redis cache** checked on DB miss (key: `company:lookup:{taxCode}`, TTL: 24h default)
3. **External API** called on cache miss (`GET /IT-start/{taxCode}`)
4. **MongoDB** inserted with new UUID
5. **Redis** cached for future lookups
6. **Response** returned to client

### Enrichment Flow

```
Request → Lookup by taxCode (must exist) → External API ({type} endpoint) → Update MongoDB document → Response
```

Enrichment data is stored as nested fields on the existing `CompanyLookup` document. The `fetchedTypes` map tracks which enrichment types have been fetched (key = type, value = timestamp).

## External API

- **Production**: `https://company.openapi.com`
- **Sandbox**: `https://test.company.openapi.com`
- **OAuth host**: `https://oauth.openapi.it` (sandbox: `https://test.oauth.openapi.it`)
- **Authentication**: account email + API key minted into a short-lived JWT bearer (preferred), or legacy static `OPENAPI_COMPANY_BEARER_TOKEN` fallback
- **Endpoint used**: `GET /IT-start/{vatCode_taxCode_or_id}`
- **Free tier**: 30 requests/month

### Authentication flow

The module uses the shared [`orkestra-openapi-auth`](../../shared/openapiauth) minter (in-tree at `backend/internal/shared/openapiauth/`, published as `github.com/orkestra-cc/orkestra-openapi-auth`). On the first request after start (or after the operator rotates credentials), the client POSTs to `<OAuthBaseURL>/token` with HTTP Basic auth (`accountEmail:apiKey`) and the scope set declared in `companyOAuthScopes`. The returned JWT is cached two-tier (in-process + Redis under `openapiauth:company:<digest>`) for `responseTTL − 60s`. An upstream 401/403 invalidates the cached JWT so the next attempt mints fresh — useful when the operator rotates the API key mid-flight.

When `accountEmail` or `apiKey` is empty, the client falls back to the static `bearerToken` field for back-compat with installs that minted JWTs manually before this flow landed. Configure either path at `/admin/modules/company`.

## Environment Variables

These env vars seed the initial `module_configs` document on first install only — once the document exists, `/admin/modules/company` is authoritative.

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENAPI_COMPANY_BASE_URL` | Override base URL | Derived from `OPENAPI_SANDBOX_MODE` |
| `OPENAPI_COMPANY_ACCOUNT_EMAIL` | OpenAPI.com account email (paired with API key for OAuth /token) | _(empty)_ |
| `OPENAPI_COMPANY_API_KEY` | Long-lived API key from console.openapi.com | _(empty)_ |
| `OPENAPI_OAUTH_BASE_URL` | OAuth host (shared with billing) | `https://oauth.openapi.it` (sandbox: `https://test.oauth.openapi.it`) |
| `OPENAPI_COMPANY_BEARER_TOKEN` | Legacy static JWT fallback (used only when API key is empty) | _(empty)_ |
| `OPENAPI_COMPANY_TIMEOUT` | HTTP request timeout | `15s` |
| `OPENAPI_COMPANY_RETRY_ATTEMPTS` | Retry count on failure | `3` |
| `OPENAPI_COMPANY_CACHE_TTL` | Redis cache TTL for company lookups | `24h` |
| `OPENAPI_SANDBOX_MODE` | Use sandbox environment (shared) | `true` |

## MongoDB Collection

| Collection | Description |
|------------|-------------|
| `company_lookups` | Company lookup results |

### Indexes

- `uuid` (unique)
- `taxCode` (unique)
- `vatCode`
- `companyName` (text)
- `createdAt` (descending)

## Resilience

- **Circuit breaker**: 5 failures → 30s open → half-open test
- **Retry with backoff**: Exponential backoff (1s, 2s, 4s...), skips 4xx errors
- **Redis caching**: 24h TTL prevents unnecessary API calls
- **MongoDB persistence**: Previously looked-up companies are always available

## Access Control

- **Role requirement**: `manager` and above (same as billing)
- **Authentication**: JWT Bearer token via auth middleware

---

### Related Guides

- [Billing Module](../billing/CLAUDE.md) - Shares the same OpenAPI.com bearer token
- [Backend Module](../../../CLAUDE.md) - Backend architecture and development workflow
- [Project Overview](../../../../../CLAUDE.md) - System architecture
