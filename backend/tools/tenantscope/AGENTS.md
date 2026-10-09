# Tool: tenantscope

_Path: `/backend/tools/tenantscope`_
_Parent: [../../AGENTS.md](../../AGENTS.md)_

## What it does

Static analyzer that enforces **invariant #1** from ADR-0001: every MongoDB
read/write in an Orkestra backend package must derive its filter (or
aggregation pipeline) from `shared/tenantrepo.Scope`, `MustScope`, or
`ScopeAggregate`. Insert operations must go through `StampInsert` /
`StampInsertM`.

Skipping the helper creates a cross-tenant data leak: without
`tenantId` on the filter, a request acting in tenant A can observe or
mutate rows that belong to tenant B. The helper panics in dev when the
tenant context is missing, but that only catches code paths that run —
the analyzer catches the rest at compile time.

## Scope

Phase 5.4 extended the package filter from `/internal/addons/` to
cover every module under `/internal/`:

- `/internal/addons/` — feature modules. All queries must be tenant-scoped.
- `/internal/core/` — kernel modules (user, auth, tenant, notification,
  authz, navigation). Many of these queries are platform-global by
  design (the user collection is cross-tenant on purpose, as are
  sessions, modules, tenants themselves). Those sites carry an
  `//tenantscope:allow admin-view: ...` comment or live in the
  baseline until Phase 5.x tightens them up.
- `/internal/shared/` — infrastructure packages. Same rule as core.

The analyzer skips `/tools/` (including itself).

## Suppression

Two mechanisms, chosen by audience:

### 1. Inline allow-comment

Put the comment on the line *immediately above* the flagged call:

```go
//tenantscope:allow admin-view: operator-wide lookup for the admin tenant list
coll.Find(ctx, bson.M{"status": "active"})
```

The reason is required (≥ 5 chars). Canonical prefixes the reviewer
should look for:

| Prefix | When to use |
|---|---|
| `admin-view:` | Operator-side panel that must see every tenant (admin tenants list, audit event reader, compliance evidence) |
| `webhook:` | Third-party callback handler that identifies the affected row via an external ID (Stripe event, SDI notification) rather than a tenant |
| `system:` | Platform-global state that is not tenant-owned (module config, user catalog, session store) |

### 2. Expiring allow-until clause (Phase 5.4)

For temporary workarounds — e.g. you shipped a fix that needs a
follow-up migration next quarter — use the `allow-until` form:

```go
//tenantscope:allow-until=2026-10-01 system: tenant filter added in #9482 once migration lands
coll.Find(ctx, bson.M{"ownerUUID": ownerUUID})
```

On and after `2026-10-01` the analyzer fails the build on that call
site, forcing the fix to land. Use this instead of a bare `allow`
whenever there's a realistic plan to remediate — it prevents the
baseline from growing unchecked.

### 3. Baseline file (mass historical drift)

`baseline.txt` at the package root holds one-line entries for every
violation present when the Phase 5.4 scope expansion shipped. As code
is migrated off raw filters onto `tenantrepo.Scope`, the corresponding
lines are deleted from the baseline. CI fails if the analyzer finds
any violation NOT listed — so new drift is always flagged.

Do not add lines to the baseline by hand. If you need to suppress a
diagnostic, use an allow-comment with a reason. The baseline exists
only to carry the existing codebase's backlog.

## Running locally

```bash
cd backend
# Run analyzer against the whole tree; same invocation CI uses.
go run ./tools/tenantscope/cmd/tenantscope \
    -baseline=tools/tenantscope/baseline.txt \
    ./internal/...

# Regenerate the baseline after a legitimate refactor:
go run ./tools/tenantscope/cmd/tenantscope ./internal/... \
    | awk -F': tenantscope: ' 'NF==2 { split($1, a, ":"); p=a[1]; l=a[2]; \
        sub(/^\/app\//, "", p); n=split($2, w, " "); m=w[1]; print p":"l":"m }' \
    | sort -u > tools/tenantscope/baseline.txt
# Then edit the file to preserve the header block at the top.

# Unit tests:
go test ./tools/tenantscope/...
```

## CI wiring

`.github/workflows/backend.yml` has a dedicated `tenantscope` job:

```yaml
- name: Enforce tenant-scoping invariant across internal/
  run: go run ./tools/tenantscope/cmd/tenantscope \
        -baseline=tools/tenantscope/baseline.txt ./internal/...
```

The job fails on any diagnostic not present in the baseline and not
covered by an allow-comment. Artifacts: none (the diagnostics are
printed to stderr and visible in the job log).

## Files

| File | Purpose |
|---|---|
| `analyzer.go` | Core analyzer logic (targetMethods, scopeFuncs, allow-comment parser, allow-until expiry) |
| `analyzer_test.go` | Unit tests against inline Go source fixtures |
| `baseline.txt` | Accepted historical drift; shrinks over time |
| `cmd/tenantscope/main.go` | singlechecker CLI wrapper |
| `AGENTS.md` | This file |

## Related

- [ADR-0001 — Unified Tenant model](../../../docs/adr/0001-unified-tenant-model.md)
- [ADR-0002 — Metrics label schema](../../../docs/adr/0002-metrics-label-schema.md)
- [`shared/tenantrepo/scope.go`](../../pkg/sdk/tenantrepo/scope.go) — the helpers the analyzer accepts as scope sources
- [`shared/middleware/auth.go`](../../internal/shared/middleware/auth.go) — populates the tenant context the helpers read
- [`authz/AGENTS.md`](../../internal/core/authz/AGENTS.md) — the 9-invariant checklist this analyzer enforces #1 of
