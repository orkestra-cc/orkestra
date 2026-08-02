# Backend — Go Modular Server

Single Go binary, single Go module. 8 core modules (always loaded) and an **empty optional-module catalog** ([ADR-0006](../docs/adr/0006-collapse-to-core-only-base.md): core-only base — a fork adds its own optional modules in-tree). Slim `cmd/server/main.go` that wires infrastructure and delegates everything else to the module registry. Port 3000 inside the container.

## Stack

Go 1.25.12 | Huma v2 (OpenAPI-first) | MongoDB 8.0 | Redis 8.2 | Chi router | AIR hot-reload (Docker)

## Module System

Every module implements the `Module` interface from the Orkestra SDK
(`pkg/sdk/module/module.go` — see [`pkg/sdk/CLAUDE.md`](pkg/sdk/CLAUDE.md)
for the SDK boundary rules and [`../docs/onboarding/orkestra-sdk.md`](../docs/onboarding/orkestra-sdk.md)
for the new-developer walkthrough):

```
Name, DisplayName, Description, Category
ConfigSchema, Collections, NavItems, Dependencies
ProvidedServices, RequiredServices, OptionalServices
Enabled, Init, RegisterRoutes, Start, Stop, HealthCheck
```

**Registration** (`cmd/server/catalog.go` + `catalog_<name>.go`): core modules (user → notification → tenant → authz → auth → navigation → logging) are always loaded — they live in `catalog.go`. The `optionalModules` map ships empty; a fork's optional module lives in its own `cmd/server/catalog_<name>.go` file and registers itself via `init()`. Every registered module compiles into the binary; runtime enable/disable is owned by the `module_configs` collection edited at `/admin/modules`. Optional modules are instantiated, initialized, and routed at boot — only enabled ones have `Start()` called. The admin API can enable/disable modules at runtime via `StartModule()`/`StopModule()` without restart. The registry topologically sorts by `Dependencies()` so producers init before consumers, auto-creates MongoDB collections with their declared indexes, seeds configs, collects nav items, and gates routes for disabled modules via `ModuleGate` middleware.

**First-boot seeding**: on a fresh install, `ConfigService.SeedFromModules` creates the `module_configs` document from each module's `ConfigSchema().EnvVar` and `EnabledByDefault`. Subsequent boots ignore env defaults; admin-set values in `module_configs` are authoritative. (ADR-0006 removed the `ORKESTRA_PROFILE` minimal/full seeding — with an empty catalog there is nothing to pre-enable.) Documents for modules **no longer compiled into the binary** (addons a fork removed, or anything left over from the ADR-0006 core-only collapse on an upgraded environment) are treated as **orphans**: `GetAllConfigs` / `ModuleStatusJSON` filter them out of the admin listing so the `/admin/modules` UI only ever shows registered modules. The orphan documents are *not* deleted — they stay in the collection (recoverable, secrets intact); they are simply not served.

Container builds: `Dockerfile` produces a single image. CI builds once per push and publishes `ghcr.io/<repo>/backend:latest` and `ghcr.io/<repo>/backend:<sha>` — no profile matrix, no build tags.

**Cross-module communication**: modules discover each other through the `ServiceRegistry` (typed key-value store). Consumer modules import interfaces from `pkg/sdk/iface/` — never import another module's `services/` or `repository/` package.

**Runtime config**: `ModuleConfigService` stores module state in MongoDB (`module_configs` collection), cached in Redis (30s TTL). Secrets encrypted with AES-256-GCM. Each module supports named config environments (production/sandbox) stored as nested maps in the same document. Admin API at `GET/PATCH /v1/admin/modules`, with per-environment endpoints at `/v1/admin/modules/{name}/environments/{env}` and `PUT /v1/admin/modules/{name}/active-environment`.

**Module infrastructure containers** (extension seam, unused by the core base): a module that needs an external service can declare it via `InfraContainers() []InfraContainerSpec` on the `Module` interface, and `shared/container.Manager` will create/start it (via the Docker Go SDK over the host socket) before `Start()` and stop it after `Stop()`. No core module declares any, so ADR-0006 removed the `/var/run/docker.sock` mount + `CONTAINER_CONTROL_ENABLED` from the compose files. A fork that adds a module needing managed infra (the removed `agents`→`orkestra-hindsight` and `graph`→`orkestra-memgraph` worked this way) re-adds the socket mount and sets `CONTAINER_CONTROL_ENABLED=true`.

**Startup reliability**: `NewMongoConnection` and `NewRedisConnection` (in `internal/shared/database/`) retry with exponential backoff (up to 20 attempts, 500ms → 5s) to wait out first-boot auth races — container servers start accepting TCP before SCRAM user / `--requirepass` provisioning completes. The Mongo readiness probe uses `ListDatabaseNames`, not `Ping`, because `Ping` bypasses the auth path and can pass prematurely. `ensureCollection` in the registry also retries transient Mongo errors (pool cleared, `AuthenticationFailed` code 18) because the driver's background monitoring connections re-authenticate for several seconds after the main client succeeds. If you see `Transient mongo error, retrying` at debug level during startup, that's this mechanism working as intended.

## Project Structure

```
backend/
├── cmd/
│   ├── server/                     # The binary
│   │   ├── main.go                 # Boot, register modules, start
│   │   ├── catalog.go              # Core module catalog + empty optionalModules
│   │   └── catalog_<name>.go       # (none in the base) one per fork-added module
│   └── migrations/                 # One-shot data migrations (0002, 0003)
├── internal/
│   ├── core/                       # Always loaded (init order: user → notification → tenant → authz → auth → navigation → logging)
│   │   ├── user/                   # User CRUD, roles, documents
│   │   ├── notification/           # Email delivery, templates, preferences, unsubscribe
│   │   ├── tenant/                 # Orgs + memberships (two-tier tenancy)
│   │   ├── authz/                  # Permissions, roles, Cedar policy engine
│   │   ├── auth/                   # Email/password + OAuth 2.1, JWT, sessions, RBAC
│   │   ├── navigation/             # Dynamic menu from module NavItems
│   │   └── logging/                # Runtime log-level admin (ADR-0005 Phase F)
│   │   # internal/addons/ does not exist in the base — a fork adds it
│   ├── shared/                     # Backend-internal infrastructure
│   │   ├── config/                 # App configuration
│   │   ├── database/               # MongoDB, Redis connections
│   │   ├── middleware/             # Auth, JWT validator, rate limiting
│   │   ├── blob/                   # S3-compatible object storage (avatars)
│   │   ├── openapiauth/            # OpenAPI.com OAuth-token minter (in-tree helper)
│   │   ├── container/              # Docker SDK manager for module InfraContainers()
│   │   ├── setup/                  # First-install wizard endpoints (/v1/setup/*)
│   │   ├── systeminit/             # Atomic first-admin sentinel (system_init collection)
│   │   ├── ownerrepo/              # Polymorphic-owner scope helpers
│   │   ├── telemetry/ geoip/ errcode/ errors/ types/ utils/
│   └── testkit/                    # Test helpers for auth identity + context
├── pkg/
│   └── sdk/                        # In-tree SDK package (Module, registry, iface, …)
├── tools/
│   ├── tenantscope/                # Static analyzer: enforces tenantrepo use (CI gate)
│   ├── policycoverage/             # Static analyzer: permission ↔ route ↔ Cedar coverage (CI gate)
│   └── piiscan/                    # Static analyzer: subject-PII collection ⇒ PIIProducer (CI gate, ADR-0009)
├── openapi/enterprise.json         # Committed OpenAPI spec (make openapi-dump)
├── Dockerfile                      # Multi-stage: dev (AIR) / production
└── go.mod                          # Single module — pkg/sdk + everything else
```

Each module follows: `module.go` → `handlers/` → `services/` → `repository/` → `models/`

## Adding a New Module

1. Create `internal/addons/yourmodule/module.go` implementing the `Module` interface
2. Create `cmd/server/catalog_yourmodule.go` with a single `init()` that registers `optionalModules["yourmodule"] = func() module.Module { return yourmodule.NewModule() }` (the registry auto-sorts by `Dependencies()`)
3. Declare `Collections()` for auto-created MongoDB collections + indexes
4. Declare `NavItems()` for sidebar entries (group, icon, path, minRole)
5. Declare `ConfigSchema()` for admin-configurable fields
5b. Optionally declare `ConfigGroups()` to give the admin settings page a
    sectioned rail instead of one flat form. Omit it and the form stays flat —
    that is a supported end state, not a gap.
6. Declare `Dependencies()` if your module needs other modules to init first
7. Use `shared/iface` interfaces for cross-module deps — add new interfaces there if needed
8. Use `deps.Services.Register(key, impl)` to expose services to other modules

Users enable the module via the admin UI at `/admin/modules` (takes effect immediately, no restart needed). For first boot of a fresh install, the module's `ConfigSchema().EnvVar` fields seed the initial `module_configs` document from the host environment, and its `EnabledByDefault` decides the initial enabled state — see [docker/CLAUDE.md](../docker/CLAUDE.md) for the env-var-vs-admin-UI split.

## API Endpoints

- **`/docs`** — Interactive API documentation (Scalar)
- **`/openapi.json`** — Auto-generated OpenAPI 3.1 spec
- **`/v1/admin/modules`** — Module management (administrator only)
- **`/v1/admin/modules/{name}/environments/{env}`** — Per-environment config CRUD
- **`/v1/admin/modules/{name}/active-environment`** — Switch active environment
- **`/v1/admin/modules/health`** — Per-module health checks

OpenAPI specs are auto-generated by Huma v2 — add endpoints with `huma.Register()` and they appear in `/docs` after restart.

### Canonical spec for docs.orkestra.cc

`backend/openapi/enterprise.json` is the **canonical OpenAPI document** consumed by [docs.orkestra.cc](https://docs.orkestra.cc) (rendered under `/api` via `docusaurus-plugin-openapi-docs`). It is **committed**, **regenerated by `make openapi-dump`**, and **gated by `make openapi-check`** in `ci-backend`.

When you change routes, regenerate before committing:

```bash
# from backend/
(cd ../docker && docker compose -f docker-compose.infra.yml up -d)   # if not already running
make openapi-dump                                                     # writes openapi/enterprise.json
git add openapi/enterprise.json
```

The dump runs the full enterprise build (`cmd/server` with default tags) with `OPENAPI_DUMP=1` set, which serializes `huma.API.OpenAPI()` to disk and exits before binding any listener. Module Init runs against an isolated Mongo namespace (`orkestra_openapi_dump`) and Redis DB index `15`, so dev/staging data is never touched. Both `operatorAPI` and `clientAPI` share a single in-memory OpenAPI document (the audience split lives at the mux/host level today), so one file covers both surfaces.

## Error-code contract

Admin-facing handlers return a stable `code` field alongside the human-fallback `detail` so frontends can localize the user-visible string without coupling to the handler's English text. Codes follow `<module>.<situation>` in **snake_case** — the module owns its namespace, the situation names the failure semantically (not the HTTP status). Codes live in `internal/shared/errcode/codes.go` as `const` strings and are covered by a golden-file contract test (`codes_test.go`) that snapshots every const name + value against an AST-parsed inventory of the file — a silent rename or value drift fails CI loudly.

Return a code-bearing failure with the typed builders:

```go
return nil, errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")
// → 409 {"status":409,"title":"Conflict","detail":"Email already in use","code":"auth.email_in_use"}
```

Builders cover the common statuses (`BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `UnprocessableEntity`); `errcode.New(status, code, detail)` exists for one-offs. Handlers not yet migrated keep returning `huma.ErrorXxx` text-only — the frontend falls back to `detail` when `code` is missing.

Examples:

- `auth.email_in_use` — `POST /v1/users` rejected because the email is already registered (409). Worked example, in `user_handler.go`.
- `billing.invoice_not_found` — SDI invoice UUID lookup miss (404). Future.
- `authz.permission_denied` — Cedar policy refused the action (403). Future.

See [`../docs/archive/frontend-admin-i18n.md`](../docs/archive/frontend-admin-i18n.md) (Phase 2) for the rollout. Until a handler is migrated, do not invent a code for it from the frontend — read it off the response or fall back to `detail`.

## Dev Tokens (LOCAL DEVELOPMENT ONLY)

```bash
./scripts/devtoken.sh developer                       # Generate operator-aud token
./scripts/devtoken.sh admin --quiet                   # Token only (for piping)
./scripts/devtoken.sh operator --curl                 # Ready-to-use curl command
./scripts/devtoken.sh administrator --audience client # ADR-0003 PR-D — mint aud=client for api.*
```

Roles (highest to lowest): `super_admin` > `administrator` > `developer` > `manager` > `operator` > `guest`.

Audiences (ADR-0003 PR-D D-10): `operator` (default, hits `console.*`) or `client` (hits `api.*`). Both surfaces' `RequireAudience` gates reject cross-audience tokens with `401 audience_mismatch`.

Creates synthetic users (no DB writes).

**`POST /dev/token` is an unauthenticated endpoint that mints a signed `super_admin` JWT to anyone who can reach it.** It is therefore gated on `IsProductionLike()` — it does not exist in **production *or* staging**, only in development. (It used to be gated on `IsProduction()` alone, which left it live and anonymous on internet-reachable staging hosts.) The gate is enforced twice: `cmd/server/main.go` skips the wiring, and `devtoken.Handler.RegisterRoutes` refuses to register regardless of what the caller passes.

Token lifetime is **not** caller-controllable: the `JWTProvider` seam mints with the deployment's access-token TTL (`JWT_ACCESS_TOKEN_EXPIRY`, or the admin-managed `accessTokenTTL`). Sending an `expiry` field returns 400 — previously it was accepted, range-checked, echoed back in `expiresAt`/`expiresIn`, and then ignored, so callers were told a lifetime the token did not have. `scripts/devtoken.sh` no longer has `--expiry`.

## Browser-issued cookies

Orkestra sets three cookies, all HttpOnly. None is readable by script.

| Cookie | Set by | Lifetime | Purpose |
|---|---|---|---|
| `orkestra_cookie` (name from `COOKIE_NAME_REFRESH`) | login / refresh | 7d | The refresh token. The only credential cookie; the SPA holds the access token in memory. |
| `orkestra_did` | `shared/middleware.DeviceMiddleware` on first sight of a browser | 1y | Server-minted random device identifier. **Not a credential** — it identifies a device, it does not authenticate one. Replaces the old MD5-of-headers id, which any caller could reproduce. Native apps send `X-Device-ID` instead. |
| `orkestra_oauth_state` | OAuth start endpoints | 10m | Per-flow CSRF nonce that binds an OAuth callback to the browser that started it. Cleared on callback. |

## Client IP resolution (trusted proxies)

`X-Forwarded-For` and friends are ordinary request headers — anyone can send them. Header interpretation therefore happens in exactly one place, `shared/middleware.RealIP`, which applies the deployment's trusted-proxy policy, rewrites `r.RemoteAddr` with the result, and deletes the forwarding headers so nothing downstream can re-derive a spoofed value. `utils.GetClientIP(r)` reads `RemoteAddr` and nothing else.

**Do not reintroduce header parsing anywhere else.** The single chokepoint is what makes the policy enforceable; four controls depend on it: the operator IP allow/blocklist (`shared/middleware/ip_gate.go`), the login geo-block (`geoBlockCountries`), the per-IP login rate limiter / lockout bucket, and the IP stamped on every audit + security event.

| Env var | Meaning |
|---|---|
| `TRUSTED_PROXY_CIDRS` | Networks our own reverse proxies live in. **Preferred** — survives a topology change without a recount. Hops are skipped right-to-left while they fall inside these networks. |
| `TRUSTED_PROXY_COUNT` | How many proxy hops sit in front of the backend. Used when `TRUSTED_PROXY_CIDRS` is empty. |

Both unset = **trust nothing**: forwarding headers are ignored and every request is attributed to its direct peer. That is the correct default for local dev and a deliberately fail-closed one elsewhere — but it is *wrong* behind a load balancer or CDN, where it collapses every caller onto the proxy's address and into a single rate-limit bucket. A production-like boot with no policy configured logs a startup warning. A malformed policy is fatal at boot rather than silently degrading to "trust nothing".

Chi's `middleware.RealIP` must **not** be used — it trusts the leftmost XFF entry unconditionally.

## Development

All services run in Docker. Never start the server manually. Two workflows depending on what you need:

**Full dev stack (Chainguard hardened images, AIR hot reload):**
```bash
cd docker
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml up -d
docker compose logs -f backend
```

The dev backend builds `docker/Dockerfile.dev-backend` (golang:alpine, AIR pre-baked; a fork with a Chainguard subscription overrides the base via the `GO_BASE` build-arg). One infra base + one app file per environment (`docker-compose.{dev,staging,prod}.yml`) + an opt-in `docker-compose.observability.yml` — ADR-0006 removed the `minimal`/`full` runtime-profile compose files.

**WSL2 caveat**: AIR doesn't detect file changes on Windows mounts. Rebuild manually (container names are stack-namespaced — `${APP_NAME}-<svc>-${ENV}`; example below is this checkout's dev stack, see [docker/CLAUDE.md](../docker/CLAUDE.md#multi-stack-model)):
```bash
docker exec orkestra-commons-backend-development go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-commons-backend-development
```

**Log level**: controlled by `LOG_LEVEL` env var — `debug` (dev), `info` (staging), `warn` (prod).

**Structured request logger** (ADR-0005 Phase A): every HTTP request produces one JSON line via `shared/middleware.RequestLogger` (mounted outermost on each audience mux after `RequestID` + `RealIP`). The payload is **allowlist-only** — never log bodies, headers, or raw query strings; module code uses `slog.InfoContext(ctx, "msg", slog.String(...))` so `trace_id` / `span_id` correlate to the same request automatically via `shared/utils.TraceContextHandler`. Tunables: `LOG_HTTP_SKIP_PATHS` (default `/health,/ready,/metrics,/openapi.json`), `LOG_HTTP_SLOW_THRESHOLD_MS` (default `1000`).

**HTTP latency histogram** (ADR-0005 Phase B): the same middleware observes `orkestra_http_request_duration_seconds` on `metrics.Default()` after each request, labelled `{audience, method, route, status_class}` with the Chi route template (never raw path) and `trace_id` as a Prometheus exemplar. Streaming endpoints (paths ending `/stream`) are intentionally excluded so SSE lifetime doesn't pollute API p99. The histogram is wired by the `setupMiddleware` call sites — modules don't need to register anything.

**Per-module log levels** (ADR-0005 Phase C): every line emitted via `deps.Logger` is auto-stamped with `module=<name>` by the module registry (`pkg/sdk/module/registry.go::depsFor`). Set `LOG_LEVEL_<MODULE>=debug` (e.g. `LOG_LEVEL_RAG=debug`) to widen one module's level without affecting the global `LOG_LEVEL` — the `shared/utils.PerModuleLevelHandler` reads these at boot and gates `Enabled` accordingly. Bare `slog.Info(...)` outside the module pipeline still uses the global threshold.

**OTLP logs fanout** (ADR-0005 Phase E): set `OTEL_LOGS_ENABLED=true` + `OTEL_EXPORTER_OTLP_ENDPOINT=…` to fan every log record out to an OTLP backend (collector → Loki/Tempo, or a vendor like Honeycomb/Datadog/Grafana Cloud/Axiom). `telemetry.InitLogs` builds the exporter + `LoggerProvider`; `shared/utils.FanoutHandler` tees stdout + OTLP so stdout stays the source of truth.

**Runtime log-level admin** (ADR-0005 Phase F): the `logging` core module (`internal/core/logging/`) owns the `log_levels` Mongo collection and exposes `/v1/admin/observability/log-levels` for the global level + per-module overrides. The slog handler's `LevelResolver` is hot-swapped at boot from the env-driven static snapshot to the DB-backed live one — admin edits via the UI at `/admin/observability/log-levels` take effect instantly via the shared `resolverBox` atomic.Pointer that every existing module logger shares. Env vars (`LOG_LEVEL`, `LOG_LEVEL_<MODULE>`) still seed boot defaults; the DB doc overrides them once present.

## Rules

- **Read the module's own CLAUDE.md** before modifying it — each core module (`notification`, `auth`, `authz`, `tenant`, `user`, `navigation`, `logging`) has one under `internal/core/<name>/`
- **Use the module system** — don't add routes or init logic directly to main.go
- **Use `shared/iface`** for cross-module deps — never import another module's services package from module.go
- **Validate all inputs**, implement RBAC on every endpoint, never expose secrets in responses
- **MongoDB indexes** — declare them in `Collections()`, don't create them manually
- **Vulnerability allowlist** — `backend/.vulncheck-allowlist.txt` lists upstream-unfixed reachable CVEs accepted by `make backend-vulncheck` (and the Backend CI workflow). Each entry must be re-evaluated when the relevant dependency is bumped.
