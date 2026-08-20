# Logging Module Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the standalone log-level page with a specialized `/admin/modules/logging` workspace for atomic permanent configuration, expiring diagnostics, and a safe Loki preview.

**Architecture:** Extend the logging module's immutable persisted snapshot with diagnostic overrides and expose atomic application operations through its existing administrator-gated API. Add a constrained Loki client behind a local interface, then render the returned state in a logging-specific React module-detail surface while retaining the old route as a redirect.

**Tech Stack:** Go 1.25.13, Huma v2, MongoDB 8, `atomic.Pointer`, React 19, TypeScript 5.9, RTK Query, React Bootstrap, TanStack Table through `AdvanceTable`, Vitest/MSW, react-i18next.

**Spec:** `docs/superpowers/specs/2026-08-20-logging-module-operations-design.md`

## Global Constraints

- All backend operations serve Tier 1 and require `system.modules.admin` on the operator router.
- Preserve persist-before-publish and immutable snapshot reads; never mutate a published map.
- The effective order is active diagnostic, permanent module override, then permanent global level.
- Loki is optional and its failure must not block configuration or stopping a diagnostic.
- The browser may request only registered modules, 5/15/60-minute windows, valid levels, bounded text, and at most 100 events; it never sends LogQL.
- Log content is never persisted, re-logged, sent to analytics, or returned without recursive redaction and an allowlisted response shape.
- Use RTK Query, `AdvanceTable`, URL search parameters, existing Orkestra primitives, and EN/IT translations; no new frontend dependency.
- Keep legacy mutation endpoints during this change and redirect the legacy page route.
- Every commit in this commons branch carries exactly one `Prop: upstream` trailer.

---

### Task 1: Persist diagnostics and resolve expiry safely

**Files:**
- Modify: `backend/internal/core/logging/models/log_level.go`
- Modify: `backend/internal/core/logging/services/log_level_service.go`
- Modify: `backend/internal/core/logging/services/log_level_service_test.go`

**Interfaces:**
- Produces: `models.DiagnosticOverride`, `models.AdminDiagnosticEntry`, and diagnostics on `models.LogLevelDoc`/`models.AdminView`.
- Produces: `(*LogLevelService).StartDiagnostic(ctx, module, level, expiresAt, actor) error`, `StopDiagnostic(ctx, module, actor) error`, and `CleanupExpired(ctx) error`.
- Preserves: `Global() slog.Level` and `LevelFor(module string) (slog.Level, bool)`.

- [ ] **Step 1: Add failing model and service tests**

Add table-driven tests proving precedence, expiry, persistence, restart load, no-expiry behavior, stop idempotency, and cleanup. Use a fixed injectable clock on the service:

```go
now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
svc.now = func() time.Time { return now }
require.NoError(t, svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, ptr(now.Add(time.Hour)), "operator-1"))
level, explicit := svc.LevelFor("auth")
assert.Equal(t, slog.LevelDebug, level)
assert.True(t, explicit)
now = now.Add(2 * time.Hour)
level, explicit = svc.LevelFor("auth")
assert.Equal(t, slog.LevelWarn, level) // permanent override resumes
assert.True(t, explicit)
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `cd backend && go test ./internal/core/logging/services -run 'TestLogLevelService_(Diagnostic|Cleanup)' -count=1`

Expected: compilation fails because diagnostic types and methods do not exist.

- [ ] **Step 3: Implement immutable diagnostic snapshots**

Add the persisted value and admin projection:

```go
type DiagnosticOverride struct {
    Level     LogLevel  `bson:"level" json:"level"`
    StartedAt time.Time `bson:"startedAt" json:"startedAt"`
    StartedBy string    `bson:"startedBy" json:"startedBy"`
    ExpiresAt *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}
```

Add `diagnostics map[string]models.DiagnosticOverride` and `now func() time.Time` to service state. Clone diagnostics in every snapshot copy. In `LevelFor`, use a diagnostic only when `ExpiresAt == nil || now.Before(*ExpiresAt)`; otherwise resolve the permanent map. Validate module membership before starting a diagnostic. Persist diagnostics in the same `log_levels` document.

- [ ] **Step 4: Run tests including the race detector**

Run: `cd backend && go test -race ./internal/core/logging/services -count=1`

Expected: PASS with no race reports.

- [ ] **Step 5: Commit the diagnostic domain**

```bash
git add backend/internal/core/logging/models/log_level.go backend/internal/core/logging/services/log_level_service.go backend/internal/core/logging/services/log_level_service_test.go
git commit -m "feat(logging): add expiring diagnostic overrides" -m "Prop: upstream"
```

### Task 2: Add atomic permanent-configuration replacement

**Files:**
- Modify: `backend/internal/core/logging/models/log_level.go`
- Modify: `backend/internal/core/logging/services/log_level_service.go`
- Modify: `backend/internal/core/logging/services/log_level_service_test.go`

**Interfaces:**
- Consumes: diagnostics-aware snapshots from Task 1.
- Produces: `models.PermanentConfigInput { Global LogLevel; PerModule map[string]LogLevel; ExpectedUpdatedAt time.Time }`.
- Produces: sentinel `services.ErrConfigConflict` and `ApplyPermanent(ctx, input, actor) error`.

- [ ] **Step 1: Write failing atomicity and conflict tests**

Cover a valid replacement, unknown module, invalid level, stale `ExpectedUpdatedAt`, persistence failure, and preservation of active diagnostics. Assert the repository is not called for validation/conflict errors and the in-memory view is unchanged after every failure.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `cd backend && go test ./internal/core/logging/services -run TestLogLevelService_ApplyPermanent -count=1`

Expected: compilation fails because `ApplyPermanent` is undefined.

- [ ] **Step 3: Implement validate-then-persist batch replacement**

Under the existing write mutex: compare timestamps exactly, validate `Global`, validate every module against the catalog, build fresh maps, preserve diagnostics, set actor/time using `now`, persist once, and only then publish. Do not implement the batch operation by looping over `SetGlobal`/`SetModule`.

- [ ] **Step 4: Run service tests**

Run: `cd backend && go test -race ./internal/core/logging/services -count=1`

Expected: PASS.

- [ ] **Step 5: Commit atomic configuration**

```bash
git add backend/internal/core/logging/models/log_level.go backend/internal/core/logging/services/log_level_service.go backend/internal/core/logging/services/log_level_service_test.go
git commit -m "feat(logging): apply permanent levels atomically" -m "Prop: upstream"
```

### Task 3: Expose batch and diagnostic APIs with lifecycle cleanup

**Files:**
- Modify: `backend/internal/core/logging/handlers/log_level_handler.go`
- Modify: `backend/internal/core/logging/handlers/log_level_handler_test.go`
- Modify: `backend/internal/core/logging/routes.go`
- Modify: `backend/internal/core/logging/module.go`
- Modify: `backend/internal/core/logging/module_test.go` if present; otherwise create it

**Interfaces:**
- Consumes: `ApplyPermanent`, `StartDiagnostic`, `StopDiagnostic`, and `CleanupExpired`.
- Produces: `PUT /v1/admin/observability/log-levels`, `PUT /v1/admin/observability/log-levels/{module}/diagnostic`, and `DELETE /v1/admin/observability/log-levels/{module}/diagnostic`.

- [ ] **Step 1: Write failing handler tests**

Test request decoding, 400 for invalid duration/level/module, 409 for stale configuration, 500 for persistence failure, actor capture, and fresh `AdminView` responses. Use `durationMinutes` values `15`, `60`, `240`, or omit `expiresAt` explicitly for no expiry; reject arbitrary durations.

- [ ] **Step 2: Run handler tests and verify failure**

Run: `cd backend && go test ./internal/core/logging/handlers -run 'TestLogLevelHandler_(Apply|StartDiagnostic|StopDiagnostic)' -count=1`

Expected: compilation fails because the new request handlers are undefined.

- [ ] **Step 3: Register the Huma operations**

Map `ErrConfigConflict` to `huma.Error409Conflict`, validation failures to 400, and repository failures to 500. Keep every operation inside the existing `RequireSystemPermission("system.modules.admin")` route group.

- [ ] **Step 4: Add cleanup lifecycle**

Give `LoggingModule` a cancellable cleanup loop started from `Start` and stopped from `Stop`. Tick once per minute, call `CleanupExpired`, and log only the cleanup error—not diagnostic or log content. Ensure repeated Start/Stop calls are safe.

- [ ] **Step 5: Run module and handler tests**

Run: `cd backend && go test -race ./internal/core/logging/... -count=1`

Expected: PASS.

- [ ] **Step 6: Commit API and lifecycle changes**

```bash
git add backend/internal/core/logging
git commit -m "feat(logging): expose diagnostic operations" -m "Prop: upstream"
```

### Task 4: Implement the constrained Loki preview client

**Files:**
- Create: `backend/internal/core/logging/logquery/client.go`
- Create: `backend/internal/core/logging/logquery/client_test.go`
- Create: `backend/internal/core/logging/logquery/redact.go`
- Create: `backend/internal/core/logging/logquery/redact_test.go`
- Modify: `backend/internal/core/logging/models/log_level.go`
- Modify: `backend/internal/core/logging/handlers/log_level_handler.go`
- Modify: `backend/internal/core/logging/handlers/log_level_handler_test.go`
- Modify: `backend/internal/core/logging/routes.go`
- Modify: `backend/internal/core/logging/module.go`
- Modify: `docker/docker-compose.dev.yml`
- Modify: `docker/docker-compose.staging.yml`
- Modify: `docker/docker-compose.prod.yml`
- Modify: `docker/.env.example`

**Interfaces:**
- Produces: `logquery.Provider` with `Status(context.Context) Status` and `Query(context.Context, Query) ([]models.LogEvent, error)`.
- Produces: `GET /v1/admin/observability/log-levels/logs?module=&windowMinutes=&level=&q=&limit=`.
- Configuration: `LOKI_QUERY_URL` defaults empty (unavailable), `GRAFANA_URL` defaults empty; the observability overlay supplies internal `http://loki:3100` and external Grafana base URL explicitly.

- [ ] **Step 1: Write failing query-construction and parser tests**

Use `httptest.Server` and assert a fixed `/loki/api/v1/query_range` request with encoded parameters. Cover allowed windows, limit clamping to 100, unknown modules, invalid levels, search length above 200 characters, upstream non-2xx, timeout, oversized response, invalid Loki JSON, and chronological normalization.

- [ ] **Step 2: Write failing recursive-redaction tests**

Use nested objects and arrays containing keys matched case-insensitively by `password`, `secret`, `token`, `authorization`, `cookie`, `email`, `phone`, `address`, and `user_id`. Assert values become `"[REDACTED]"`, while `trace_id`, `span_id`, `request_id`, `route`, and numeric duration fields survive.

- [ ] **Step 3: Run logquery tests and verify failure**

Run: `cd backend && go test ./internal/core/logging/logquery -count=1`

Expected: compilation fails because the package implementation is absent.

- [ ] **Step 4: Implement the provider and safe response projection**

Construct LogQL only from validated values: `{service="orkestra-backend", module="<escaped>"} | json`, plus fixed level and text filter operators. Use a dedicated `http.Client` with a 3-second timeout, `io.LimitReader` capped at 1 MiB, and close every response body. Return only normalized `LogEvent` fields, with redacted structured attributes.

- [ ] **Step 5: Add API status and preview handlers**

Extend `AdminView` with provider availability and optional Grafana base URL. Return 503 with a stable error code when unavailable, 504 on provider timeout, 502 for other upstream failures, and 400 for request validation. Never include the upstream response body in errors.

- [ ] **Step 6: Wire environment values**

Add the two variables to backend service environments in each compose file and document them in `docker/.env.example`. The default app stack must remain valid with both empty.

- [ ] **Step 7: Run security-focused tests and build**

Run: `cd backend && go test -race ./internal/core/logging/... -count=1 && go build ./cmd/server`

Expected: PASS.

- [ ] **Step 8: Commit the Loki proxy**

```bash
git add backend/internal/core/logging docker/docker-compose.dev.yml docker/docker-compose.staging.yml docker/docker-compose.prod.yml docker/.env.example
git commit -m "feat(logging): add safe Loki log preview" -m "Prop: upstream"
```

### Task 5: Extend the frontend API contract

**Files:**
- Modify: `frontend-admin/src/types/observability.ts`
- Modify: `frontend-admin/src/store/api/observabilityApi.ts`
- Create: `frontend-admin/src/store/api/observabilityApi.test.ts`
- Modify: `frontend-admin/src/store/api/baseApi.ts` only if `LogLevels` is not already in `tagTypes`

**Interfaces:**
- Consumes: API responses from Tasks 3 and 4.
- Produces: `useApplyPermanentLogLevelsMutation`, `useStartDiagnosticMutation`, `useStopDiagnosticMutation`, and `useGetLogPreviewQuery`.

- [ ] **Step 1: Add failing MSW contract tests**

Assert exact methods, encoded module path segments, query parameters, request bodies, cache invalidation after mutations, and skipping the preview query when no module is selected.

- [ ] **Step 2: Run the API tests and verify failure**

Run: `cd frontend-admin && npm test -- --run src/store/api/observabilityApi.test.ts`

Expected: tests fail because the new hooks and types do not exist.

- [ ] **Step 3: Add strict TypeScript models and endpoints**

Model diagnostics, permanent override values, provider status, log events, batch conflict payload, and preview filters without `any`. Retain old exported hooks until the legacy page is replaced.

- [ ] **Step 4: Run tests and typecheck**

Run: `cd frontend-admin && npm test -- --run src/store/api/observabilityApi.test.ts && npm run typecheck`

Expected: PASS.

- [ ] **Step 5: Commit the frontend contract**

```bash
git add frontend-admin/src/types/observability.ts frontend-admin/src/store/api/observabilityApi.ts frontend-admin/src/store/api/observabilityApi.test.ts frontend-admin/src/store/api/baseApi.ts
git commit -m "feat(observability): add logging workspace API" -m "Prop: upstream"
```

### Task 6: Build the specialized logging module workspace

**Files:**
- Create: `frontend-admin/src/pages/admin/modules/logging/index.tsx`
- Create: `frontend-admin/src/pages/admin/modules/logging/LoggingOverview.tsx`
- Create: `frontend-admin/src/pages/admin/modules/logging/PermanentLevelsPanel.tsx`
- Create: `frontend-admin/src/pages/admin/modules/logging/DiagnosticsPanel.tsx`
- Create: `frontend-admin/src/pages/admin/modules/logging/LogPreviewPanel.tsx`
- Create: `frontend-admin/src/pages/admin/modules/logging/index.test.tsx`
- Modify: `frontend-admin/src/pages/admin/modules/detail/index.tsx`
- Modify: `frontend-admin/src/pages/admin/modules/detail/index.test.tsx`
- Modify: `frontend-admin/src/locales/en.json`
- Modify: `frontend-admin/src/locales/it.json`

**Interfaces:**
- Consumes: hooks and types from Task 5 plus existing `ModuleDetailHeader`, `ModuleOverviewPanel`, `ModuleDependencyCard`, `StatCard`, `SectionCard`, `AdvanceTable`, and `SubtleBadge`.
- Produces: `LoggingModulePage`, selected when `moduleName === "logging"`.

- [ ] **Step 1: Complete the mandatory frontend pre-flight**

Open and record these current sources before JSX changes:

```text
Production precedent: frontend-admin/src/pages/admin/modules/detail/index.tsx
Production precedent: frontend-admin/src/pages/admin/tenants/index.tsx
Reference read: frontend-admin/src/reference/components/ui/StatCards.tsx
Reference read: frontend-admin/src/reference/components/tables/Tables.tsx
Primitives: ModuleDetailHeader, ModuleOverviewPanel, ModuleDependencyCard, StatCard, SectionCard, AdvanceTable, SubtleBadge
```

Post the filled pre-flight block in the implementation update.

- [ ] **Step 2: Write failing workspace integration tests**

With MSW and `MemoryRouter`, cover: default `overview`; direct `?section=levels`; stale section normalization; dirty count; discard; one batch save; 409 reload prompt; diagnostic creation; countdown from server timestamps; no-expiry warning; stop; provider unavailable; preview error/empty/results; and five-second auto-refresh using fake timers.

- [ ] **Step 3: Run the workspace test and verify failure**

Run: `cd frontend-admin && npm test -- --run src/pages/admin/modules/logging/index.test.tsx`

Expected: compilation fails because `LoggingModulePage` does not exist.

- [ ] **Step 4: Implement the page shell and overview**

Use `useSearchParams` with the exact keys `overview`, `levels`, `diagnostics`, and `logs`; replace invalid values with `overview`. Preserve the standard module header/overview/dependencies context. Render summary cards for global level, permanent override count, active diagnostics, and provider availability.

- [ ] **Step 5: Implement permanent draft editing**

Keep one draft object derived from the last server snapshot. Show inherited and effective values separately. Do not mutate on select change. Render a bottom save bar only when dirty; `Discard` restores the server snapshot and `Apply changes` sends one batch mutation with `expectedUpdatedAt`.

- [ ] **Step 6: Implement diagnostics and log preview**

Use an explicit duration selector with `15`, `60`, `240`, and no-expiry. Apply starts/stops immediately and disable only the affected action. For preview, show filters, manual refresh, an off-by-default five-second auto-refresh, capped rows, expandable safe attributes, and a Grafana link only when supplied by the API.

- [ ] **Step 7: Add complete EN/IT copy and accessibility labels**

Place keys under `adminObservability.loggingWorkspace.*`. Translate level explanations, conflict recovery, diagnostic warnings, provider states, filters, buttons, countdown labels, and empty states. Give every module-specific select and icon action a unique accessible name.

- [ ] **Step 8: Run focused tests, parity, and typecheck**

Run: `cd frontend-admin && npm test -- --run src/pages/admin/modules/logging/index.test.tsx src/pages/admin/modules/detail/index.test.tsx && npm run typecheck`

Expected: PASS.

- [ ] **Step 9: Commit the workspace**

```bash
git add frontend-admin/src/pages/admin/modules frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(observability): add logging module workspace" -m "Prop: upstream"
```

### Task 7: Redirect the legacy route and remove duplicate navigation

**Files:**
- Modify: `frontend-admin/src/routes/coreRoutes.tsx`
- Delete: `frontend-admin/src/pages/admin/observability/log-levels/index.tsx`
- Create: `frontend-admin/src/pages/admin/observability/log-levels/redirect.test.tsx`
- Modify: `backend/internal/core/logging/module.go`
- Modify: `backend/internal/core/logging/module_test.go`
- Modify: `backend/internal/core/logging/CLAUDE.md`
- Modify: `docs/adr/0005-observability-logging-tracing-metrics.md`

**Interfaces:**
- Consumes: `LoggingModulePage` from Task 6.
- Produces: legacy navigation redirect to `/admin/modules/logging?section=levels` and no standalone logging nav item.

- [ ] **Step 1: Write failing redirect and navigation tests**

Assert the legacy URL replaces history with the new levels section and that `LoggingModule.NavItems()` contributes no duplicate “Log levels” item.

- [ ] **Step 2: Run tests and verify failure**

Run: `cd frontend-admin && npm test -- --run src/pages/admin/observability/log-levels/redirect.test.tsx; cd ../backend && go test ./internal/core/logging -run TestLoggingModule_NavItems -count=1`

Expected: both assertions fail against the current route and nav item.

- [ ] **Step 3: Implement redirect and navigation cleanup**

Replace the lazy legacy page element with `<Navigate to="/admin/modules/logging?section=levels" replace />`. Remove `NavItems()` from `LoggingModule`; do not hardcode a replacement sidebar item because the existing Modules entry is authoritative.

- [ ] **Step 4: Update module and ADR documentation**

Document the specialized module workspace, atomic permanent configuration, diagnostics lifecycle, Loki preview boundary, legacy redirect, new endpoints, and the fact that streaming/full querying remains Grafana's responsibility.

- [ ] **Step 5: Run focused tests**

Run: `cd frontend-admin && npm test -- --run src/pages/admin/observability/log-levels/redirect.test.tsx src/pages/admin/modules/logging/index.test.tsx; cd ../backend && go test ./internal/core/logging/... -count=1`

Expected: PASS.

- [ ] **Step 6: Commit route and documentation changes**

```bash
git add frontend-admin/src/routes/coreRoutes.tsx frontend-admin/src/pages/admin/observability/log-levels backend/internal/core/logging/module.go backend/internal/core/logging/module_test.go backend/internal/core/logging/CLAUDE.md docs/adr/0005-observability-logging-tracing-metrics.md
git commit -m "refactor(logging): move controls into module detail" -m "Prop: upstream"
```

### Task 8: Regenerate contracts and run final verification

**Files:**
- Modify: `backend/openapi/enterprise.json`
- Modify: generated or checked documentation only when `make openapi-dump` updates it

**Interfaces:**
- Consumes: completed backend and frontend implementation.
- Produces: checked-in OpenAPI matching the Huma routes and a verified release candidate.

- [ ] **Step 1: Regenerate OpenAPI through the sanctioned target**

Run: `make openapi-dump`

Expected: `backend/openapi/enterprise.json` contains the batch, diagnostic, and preview operations without unrelated drift.

- [ ] **Step 2: Run backend quality gates**

Run: `make ci-backend`

Expected: lint, tenant scope, policy coverage, PII scan, vulnerability checks, tests, build, and OpenAPI check all pass.

- [ ] **Step 3: Run frontend quality gates**

Run: `make ci-frontend-admin`

Expected: typecheck, ESLint, tests, audit, and build all pass.

- [ ] **Step 4: Inspect the final diff and trailers**

Run: `git diff dev...HEAD --check && git log --format='%h %(trailers:key=Prop,valueonly) %s' dev..HEAD`

Expected: no whitespace errors; every feature commit reports exactly `upstream`.

- [ ] **Step 5: Commit generated API changes if present**

```bash
git add backend/openapi/enterprise.json
git diff --cached --quiet || git commit -m "docs(api): refresh logging operations schema" -m "Prop: upstream"
```

- [ ] **Step 6: Record verification evidence**

Capture the exact passing commands, test counts, known pre-existing warnings, and final `git status --short --branch` for the handoff. Do not claim completion if either CI target is incomplete or failing.
