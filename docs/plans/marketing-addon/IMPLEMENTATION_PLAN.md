---
type: implementation-plan
author: claude
date: 2026-05-18
domain: orkestra
component: marketing-addon
status: ready-to-execute
phase: 1 (Fondazione anagrafica MVP)
branch: feature/marketing-addon
tags: [orkestra, marketing, plan]
---

# Marketing Addon — Phase 1 Implementation Plan

Concrete plan to land **Phase 1 (Fondazione anagrafica MVP)** from
[`Orkestra_marketing_addon.md`](Orkestra_marketing_addon.md) §9 into the
orkestra repo. Phases 2–4 are explicitly **out of scope** for this plan
and will be planned separately once Phase 1 is in `dev`.

Decisions resolved with the user before this plan was written:

- **Scope**: Phase 1 only.
- **Module layout**: separate Go module from day one (matches
  sales/subscriptions/etc.).
- **Branch**: feature branch `feature/marketing-addon` off `dev`.
- **SKU profile**: rely on `enterprise = ["*"]` auto-inclusion; no new
  profile, no Makefile/compose churn.

## Phase 1 deliverables (from §9)

What ships at the end of this plan:

- 5 MongoDB collections: `marketing_organizations`, `marketing_persons`,
  `marketing_memberships`, `marketing_tags`,
  `marketing_custom_field_schemas`.
- Huma CRUD APIs for all 5, gated by Cedar permissions.
- Importer **`csv`** with column-mapping UI; dedup on
  `primary_email` (Person) and `vat`/`tax_code` (Organization);
  auto-merge of non-conflicting fields; `sources[]` provenance.
- Admin UI at `/marketing/*` (contacts list/detail, tags, custom-field
  schemas, import wizard).
- Enabled out of the box on the `enterprise` SKU; disabled by default
  on every other SKU.

Explicitly **deferred** to Phase 2+ (do not implement now): activity
log + scoring (Phase 2), excel + odoo importers + conflict-review
queue (Phase 3), card types + cards (Phase 4), campaigns/segments
(Phase 5). Schemas for those collections exist in `schemas/` already
— they stay as design docs, not code.

---

## 1. Branch + workflow

```bash
git checkout dev
git pull
git checkout -b feature/marketing-addon
```

Sub-PR strategy onto `feature/marketing-addon` (each one independently
green on CI; merge to branch, not yet to `dev`):

1. **PR-1**: scaffolding (go.mod, module.go, catalog file, empty
   handlers/routes/services/repository/models, CLAUDE.md, root
   CLAUDE.md table entry). Goal: builds clean on every profile, addon
   visible as disabled at `/admin/modules`.
2. **PR-2**: data layer — 5 collections, models, repositories,
   tenant-scoped, indexes declared via `Collections()`. Tests for
   repositories.
3. **PR-3**: API layer — handlers + routes + Cedar permissions + Cedar
   policy entries + policycoverage reconciliation. Tests for handlers.
4. **PR-4**: CSV importer + dedup + auto-merge + `sources[]`. Background
   job for long-running imports. Tests for importer pipeline.
5. **PR-5**: frontend manifest + pages (contacts list/detail, tags,
   custom-field schemas, import wizard).
6. **PR-6**: docs, OpenAPI dump regen, CLAUDE.md updates, project-root
   table entry. Final merge of `feature/marketing-addon → dev`.

Each PR opens against `feature/marketing-addon`; the final squash to
`dev` happens after PR-6.

---

## 2. Module layout (PR-1)

New tree, mirroring the pattern set by `internal/addons/subscriptions`
and `internal/addons/sales`:

```
backend/internal/addons/marketing/
├── go.mod                       # module github.com/orkestra-cc/orkestra-addon-marketing
├── go.sum
├── CLAUDE.md                    # module-level doc (responsibilities, deps, conventions)
├── README.md                    # external-facing intro for the public mirror
├── LICENSE                      # MIT (matches other extracted addons)
├── module.go                    # MarketingModule, Module/HasCollections/HasPermissions/HasNavItems/HasConfigSchema/Routable
├── routes.go                    # RegisterRoutes — wires handler buckets to perm-gated subgroups
├── models/
│   ├── organization.go
│   ├── person.go
│   ├── membership.go
│   ├── tag.go
│   ├── custom_field_schema.go
│   ├── source.go                # shared Source / EmailEntry / PhoneEntry / Consent structs
│   └── collections.go           # const Names — single source of truth for Mongo collection names
├── repository/
│   ├── organization_repo.go
│   ├── person_repo.go
│   ├── membership_repo.go
│   ├── tag_repo.go
│   ├── custom_field_schema_repo.go
│   └── *_test.go                # one *_test.go per repo using a mongo testkit
├── services/
│   ├── contact_service.go       # Person + Organization + Membership CRUD orchestration
│   ├── tag_service.go
│   ├── custom_field_service.go  # validation of bag against schema
│   ├── dedup.go                 # email/vat/tax_code matchers (used by importer + create)
│   ├── source_service.go        # append-to-sources[] helper used by every write path
│   └── *_test.go
├── handlers/
│   ├── organizations_handler.go
│   ├── persons_handler.go
│   ├── memberships_handler.go
│   ├── tags_handler.go
│   ├── custom_field_schemas_handler.go
│   ├── imports_handler.go       # POST /v1/marketing/imports — kicks a csv import job
│   └── *_test.go
└── importers/
    ├── importer.go              # Importer interface (Describe/ValidateConfig/DryRun/Run)
    ├── pipeline.go              # generic extract→normalize→map→dedup→commit pipeline
    ├── csv/
    │   ├── adapter.go
    │   ├── mapping.go           # column→field mapping definitions
    │   └── adapter_test.go
    └── job.go                   # background runner (one-shot, per design D18)
```

**`go.mod` contents** (new file):

```go
module github.com/orkestra-cc/orkestra-addon-marketing

go 1.25.10

require (
    github.com/danielgtaylor/huma/v2 v2.34.1
    github.com/go-chi/chi/v5 v5.2.5
    github.com/google/uuid v1.6.0
    github.com/orkestra-cc/orkestra-sdk v0.4.0
    go.mongodb.org/mongo-driver v1.17.6
)
```

Pin SDK to the same version every other extracted addon currently uses
(`v0.4.0` per `MEMORY.md`); align if newer when PR-1 lands.

**`backend/go.mod` change** — add the replace directive next to the
others:

```go
// in-tree, mirrored to orkestra-cc/orkestra-addon-marketing and tagged
// from v0.1.0 once Phase 1 stabilizes.
replace github.com/orkestra-cc/orkestra-addon-marketing => ./internal/addons/marketing
```

Plus a transitive `require ... // indirect` line (resolved by `go mod
tidy` after PR-1).

**`backend/cmd/server/catalog_marketing.go`** (new file):

```go
//go:build !no_addons || addon_marketing

package main

import (
    "github.com/orkestra-cc/orkestra-addon-marketing"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
    optionalModules["marketing"] = func() module.Module { return marketing.NewModule() }
}
```

No `allOptionalModuleNames` change is needed — it iterates the map.

**Enterprise auto-enable**: `profileAddons["enterprise"] = ["*"]` in
`pkg/sdk/module/config_service.go` already expands to every non-core
addon. Marketing inherits this for free; no edit required. Verify in
the seeder test (`pkg/sdk/module/config_service_test.go::TestSeed…` —
no new test case needed, the existing enterprise case asserts the `*`
expansion is non-empty, not specific names).

---

## 3. Data layer (PR-2)

### 3.1 Collection const + indexes

`models/collections.go` is the single source of truth — every repo
imports from here, never hardcodes a string. Following
[`mongo-collection-naming`](skill) rules every name carries the
`marketing_` prefix.

```go
package models

const (
    OrganizationsCollection      = "marketing_organizations"
    PersonsCollection            = "marketing_persons"
    MembershipsCollection        = "marketing_memberships"
    TagsCollection               = "marketing_tags"
    CustomFieldSchemasCollection = "marketing_custom_field_schemas"
)
```

`module.go::Collections()` declares all 5 with the index sets pulled
verbatim from the per-collection schema docs in
[`schemas/`](schemas/). Specifically:

| Collection | Critical indexes (from schemas/*.md) |
|---|---|
| `marketing_organizations` | `(tenant_id, vat)` unique-sparse, `(tenant_id, tax_code)` unique-sparse, `(tenant_id, tags)` |
| `marketing_persons` | `(tenant_id, emails.address)` unique-sparse on `primary=true`, `(tenant_id, tags)`, `(tenant_id, active_card_ids)` (denormalized — populated later, declare now) |
| `marketing_memberships` | `(tenant_id, person_id, org_id)` unique, `(tenant_id, person_id, primary)` |
| `marketing_tags` | `(tenant_id, key)` unique, `(tenant_id, parent_id)` |
| `marketing_custom_field_schemas` | `(tenant_id, target_collection, key)` unique |

The exact field names + index specs must match
`schemas/marketing_*.md` field-by-field. If a schema doc and this plan
disagree, the schema doc wins — it's the canonical design artifact.

### 3.2 Tenant scoping (mandatory CI gate)

Every repository query must go through
`github.com/orkestra-cc/orkestra-sdk/tenantrepo`:

- Reads/lookups: `tenantrepo.Scope(ctx, filter)` — fails closed on
  missing tenant.
- Inserts: `tenantrepo.StampInsert(ctx, doc)` /
  `tenantrepo.StampInsertM(ctx, docs)` — stamps `tenant_id` from
  context.
- Aggregations: `tenantrepo.ScopeAggregate(ctx, pipeline)`.

The CI `tenantscope` analyzer fails the build on any direct
`coll.Find(ctx, filter)` without going through these helpers. **Do
not** add baseline entries for new marketing code — the baseline is
for legacy violations only; new code must be clean.

### 3.3 Models

Map the schema docs 1:1 to Go structs with BSON tags. Examples (full
specs in `schemas/marketing_persons.md` etc.):

```go
// models/person.go
type Person struct {
    ID             primitive.ObjectID `bson:"_id,omitempty"            json:"id"`
    TenantID       string             `bson:"tenant_id"                json:"tenant_id"`
    FirstName      string             `bson:"first_name,omitempty"     json:"first_name,omitempty"`
    LastName       string             `bson:"last_name,omitempty"      json:"last_name,omitempty"`
    Emails         []EmailEntry       `bson:"emails,omitempty"         json:"emails,omitempty"`
    Phones         []PhoneEntry       `bson:"phones,omitempty"         json:"phones,omitempty"`
    Language       string             `bson:"language,omitempty"       json:"language,omitempty"`
    Tags           []primitive.ObjectID `bson:"tags,omitempty"          json:"tags,omitempty"`
    CustomFields   map[string]any     `bson:"custom_fields,omitempty"  json:"custom_fields,omitempty"`
    Consent        *Consent           `bson:"consent,omitempty"        json:"consent,omitempty"`
    ActiveCardIDs  []primitive.ObjectID `bson:"active_card_ids,omitempty" json:"active_card_ids,omitempty"`
    Sources        []Source           `bson:"sources,omitempty"        json:"sources,omitempty"`
    Notes          string             `bson:"notes,omitempty"          json:"notes,omitempty"`
    CreatedAt      time.Time          `bson:"created_at"               json:"created_at"`
    UpdatedAt      time.Time          `bson:"updated_at"               json:"updated_at"`
    CreatedBy      string             `bson:"created_by,omitempty"     json:"created_by,omitempty"`
    UpdatedBy      string             `bson:"updated_by,omitempty"     json:"updated_by,omitempty"`
}
```

`active_card_ids` stays in the struct from day one even though cards
arrive in Phase 4 — avoids a future migration. The index on it is also
declared from Phase 1.

### 3.4 Custom-field validation (write-time, per D05)

`custom_field_service.go` exposes:

```go
func (s *CustomFieldService) Validate(ctx context.Context, target string, fields map[string]any) error
```

`target` is `"persons"` or `"organizations"`. The service loads the
tenant's schema bag from `marketing_custom_field_schemas`, validates
each `fields[key]` against its declared type
(`text`/`number`/`enum`/`multi_enum`/`date`/`bool`/`json`), and rejects
unknown keys with `400 unknown_custom_field`. Every Person/Organization
create+update path calls this before persisting.

---

## 4. API layer (PR-3)

### 4.1 Routes (Huma)

`routes.go` follows the per-permission-bucket pattern from
`internal/addons/subscriptions/routes.go`. Each `Register*` function is
wired into a chi subgroup with `RequirePermission` middleware.

Endpoint matrix (all under `/v1/marketing/`):

| Path | Methods | Permission | Notes |
|---|---|---|---|
| `/v1/marketing/organizations` | GET, POST | `marketing.contact.read` / `.write` | List + create |
| `/v1/marketing/organizations/{id}` | GET, PATCH, DELETE | `read` / `write` / `delete` | |
| `/v1/marketing/persons` | GET, POST | `read` / `write` | List + create |
| `/v1/marketing/persons/{id}` | GET, PATCH, DELETE | `read` / `write` / `delete` | |
| `/v1/marketing/persons/{id}/memberships` | GET, POST, DELETE | `read` / `write` | Inline membership management |
| `/v1/marketing/memberships/{id}` | PATCH, DELETE | `write` / `delete` | For direct membership mutations |
| `/v1/marketing/tags` | GET, POST | `read` / `write` | |
| `/v1/marketing/tags/{id}` | PATCH, DELETE | `write` / `delete` | |
| `/v1/marketing/custom-field-schemas` | GET, POST | `read` / `write` | |
| `/v1/marketing/custom-field-schemas/{id}` | PATCH, DELETE | `write` / `delete` | |
| `/v1/marketing/imports` | POST, GET | `marketing.import.run` / `read` | Kick + list import jobs |
| `/v1/marketing/imports/{id}` | GET | `read` | Job status + summary |
| `/v1/marketing/imports/{id}/preview` | POST | `import.run` | Dry-run preview (no DB writes) |

Query support on list endpoints: `tag`, `tag[]`, `has_email`,
`source`, `created_after`, `q` (substring across name/email),
`limit`/`cursor` pagination. Implement with Mongo `$in` / `$text` (no
text index in Phase 1 — fall back to `$regex` on indexed prefix).

All routes register with `huma.Register` so they appear in
`/openapi.json` automatically. The `imports/*` endpoints accept
`multipart/form-data` for the CSV upload; Huma supports this via
`huma.MultipartFormFiles` (see `auth` module's photo-upload routes
for reference if needed).

### 4.2 Cedar permissions

`module.go::Permissions()` declares the catalog (consumed by both the
authz module's gate registration and the `policycoverage` analyzer):

```go
func (m *MarketingModule) Permissions() []iface.PermissionSpec {
    return []iface.PermissionSpec{
        {Key: "marketing.contact.read",   Module: "marketing", Description: "View persons and organizations"},
        {Key: "marketing.contact.write",  Module: "marketing", Description: "Create and update persons and organizations"},
        {Key: "marketing.contact.delete", Module: "marketing", Description: "Hard-delete persons and organizations (GDPR right-to-be-forgotten)"},
        {Key: "marketing.tag.write",      Module: "marketing", Description: "Manage marketing tags"},
        {Key: "marketing.import.run",     Module: "marketing", Description: "Trigger and preview CSV import jobs"},
    }
}
```

Permissions deferred (Phase 2+): `marketing.activity.*`,
`marketing.score_profile.*`, `marketing.card_type.*`,
`marketing.card.*`, `marketing.conflict.resolve`.

### 4.3 Cedar policy coverage

The `policycoverage` analyzer fails CI when a permission key has no
Cedar coverage. Two paths:

1. **Preferred**: add a `permit (...)` rule with explicit
   `Action::"marketing.contact.read"` literals in a new file
   `backend/internal/core/authz/cedar/policies/marketing.cedar`. This
   tightens the future enforce-mode blast radius.
2. **Fallback**: if a permission is a clean fit for an existing
   `context.action_suffix == "..."` clause (e.g. `*.read` ABAC rules
   in `abac.cedar`), no new policy is needed.

Plan: write `policies/marketing.cedar` listing each of the 5 keys with
the same role-based pattern other module-scoped actions use. Verify
locally with `make policycoverage` before opening PR-3.

---

## 5. CSV importer (PR-4)

### 5.1 Architecture

Implements design §5. The pipeline lives in
`importers/pipeline.go` and is **shared** with future adapters
(excel/odoo). Only `csv/adapter.go` is new in Phase 1.

```
backend/internal/addons/marketing/importers/
├── importer.go      # interface, types (Descriptor, Config, Source, Job, Result, PreviewReport)
├── pipeline.go      # extract → normalize → map → dedup → commit (generic)
├── job.go           # one-shot runner, persists state in marketing_import_jobs
└── csv/
    ├── adapter.go   # implements Importer for files
    └── mapping.go   # column → canonical field
```

> Note: `marketing_import_jobs` is a Phase 3 deliverable in the
> design, but **we need it from day one** to surface import progress
> in the UI. Declared as a "Phase-1 minimal" version: only
> `_id, tenant_id, importer, status (queued|running|done|failed),
> stats {rows_in, persons_created, persons_merged, orgs_created,
> orgs_merged}, error, created_at, completed_at, created_by`. Full
> schema (with `paused_for_review`, conflict refs) lands in Phase 3.

### 5.2 Dedup (design §5.5, Phase 1 subset)

Person:

1. Normalize incoming `primary_email` to lowercase.
2. Lookup `marketing_persons` where any
   `emails.address == lower(incoming_email)` AND
   `tenant_id == ctx`. Found → match.
3. No match: insert new Person.

Organization:

1. Normalize incoming `vat` (uppercase, strip whitespace).
2. Lookup by `vat`. No match → lookup by `tax_code`. No match → insert.

Soft-match logic (`first_name+last_name+phone` for Person,
`legal_name` for Organization) is **deferred to Phase 3** when the
conflict-review queue exists. Phase 1 does **not** surface soft
matches.

### 5.3 Auto-merge (design §5.6, Phase 1 subset)

For matched records, apply only the auto-merge half (no review queue):

- Fields where existing is empty/null and incoming is non-empty →
  write incoming.
- Additive fields (`tags[]`, `emails[]`, `phones[]`, `sources[]`) →
  set-union merge, dedup by canonical value
  (lowercase email, normalized phone).
- Conflicting non-empty values on overwrite-fields (`primary_email`,
  `vat`, `tax_code`) → **skip the field**, record the conflict in the
  job's `stats.conflicts_skipped` counter, and emit a per-row WARN
  log. The full conflict review workflow lands in Phase 3.

This is a deliberate, narrow Phase-1 behavior: import is conservative,
never destructive, surfaces problems in the job summary without
blocking the rest of the file.

### 5.4 Idempotence

Phase 1 has no Activity log, so `dedup_key` (design D21) does **not**
apply yet. Person/Org dedup by email/VAT covers re-imports of the same
CSV — each row matches an existing record and goes through the
auto-merge path, producing zero net mutation when the data hasn't
changed.

### 5.5 Job execution

`POST /v1/marketing/imports` accepts the CSV + mapping JSON, creates a
`marketing_import_jobs` doc with `status=queued`, returns 202 with the
job ID. A goroutine started by `MarketingModule.Start()` polls the
collection for `queued` jobs every 5s (configurable later) and runs
them serially per-tenant. On `Stop()` the goroutine context is
cancelled. This is the same pattern `subscriptions/jobs/renewal.go`
uses.

### 5.6 ConfigSchema for the importer (subset of §3.5)

Phase 1 surfaces only the importer-relevant config:

```go
func (m *MarketingModule) ConfigSchema() []module.ConfigField {
    return []module.ConfigField{
        {Key: "importPersonDedupKeys",   Label: "Person dedup keys",       Type: module.FieldString, Default: "primary_email",       EnvVar: "MARKETING_IMPORT_PERSON_KEYS"},
        {Key: "importOrgDedupKeys",      Label: "Organization dedup keys", Type: module.FieldString, Default: "vat,tax_code",        EnvVar: "MARKETING_IMPORT_ORG_KEYS"},
        {Key: "importJobPollInterval",   Label: "Import job poll interval", Type: module.FieldDuration, Default: "5s",                EnvVar: "MARKETING_IMPORT_POLL_INTERVAL"},
    }
}
```

Phase-2/4 config (score cron, card expiration cron,
conflict-review-required-fields) is added in the matching phase, not now.

---

## 6. Frontend (PR-5)

### 6.1 Module manifest

`frontend-admin/src/modules/marketing.tsx` — pattern from
`subscriptions.tsx`:

```tsx
import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const ContactsListPage   = lazy(() => import('pages/marketing/contacts/list'));
const ContactDetailPage  = lazy(() => import('pages/marketing/contacts/detail'));
const TagsPage           = lazy(() => import('pages/marketing/tags'));
const CustomFieldsPage   = lazy(() => import('pages/marketing/custom-fields'));
const ImportsPage        = lazy(() => import('pages/marketing/imports'));
const ImportWizardPage   = lazy(() => import('pages/marketing/imports/wizard'));

const perms: [string[]] = [['super_admin', 'administrator', 'manager']];

const wrap = (node: React.ReactNode, key: string) => (
  <ModuleGate module="marketing">
    <ProtectedRoute requiredPermissions={perms}>
      <Suspense key={key} fallback={<OrkestraLoader />}>{node}</Suspense>
    </ProtectedRoute>
  </ModuleGate>
);

export const marketingManifest: ModuleManifest = {
  name: 'marketing',
  routes: () => [
    { path: 'marketing/contacts',           element: wrap(<ContactsListPage   />, 'marketing-contacts') },
    { path: 'marketing/contacts/:id',       element: wrap(<ContactDetailPage  />, 'marketing-contact-detail') },
    { path: 'marketing/tags',               element: wrap(<TagsPage           />, 'marketing-tags') },
    { path: 'marketing/custom-fields',      element: wrap(<CustomFieldsPage   />, 'marketing-custom-fields') },
    { path: 'marketing/imports',            element: wrap(<ImportsPage        />, 'marketing-imports') },
    { path: 'marketing/imports/new',        element: wrap(<ImportWizardPage   />, 'marketing-import-wizard') },
  ],
  injectApi: () => import('store/api/marketingApi'),
};
```

Register the manifest in `frontend-admin/src/modules/index.ts`.

### 6.2 Pages

| Page | Purpose |
|---|---|
| `contacts/list` | TanStack Table of persons; filter by tag, source, has-email; bulk-tag action |
| `contacts/detail` | Person view: identity, emails/phones, memberships, tags, custom fields, sources timeline |
| `tags` | CRUD on tags with hierarchy (parent picker) |
| `custom-fields` | Schema editor — target collection (persons/orgs), field type, label, options for enum types |
| `imports` | List of import jobs with status + stats |
| `imports/wizard` | 3-step: upload CSV → preview rows → map columns → run |

URL-synced tabs are mandatory wherever tabs appear (see
[`url-tabs`](skill) skill). The contact detail page has tabs:
overview, memberships, custom fields, sources/audit. Persist active
tab in `?tab=` search param.

### 6.3 RTK Query API slice

`frontend-admin/src/store/api/marketingApi.ts` — endpoints typed off
the OpenAPI dump (auto-generation via `openapi-typescript` if the
project uses that; otherwise hand-typed off the schema). One
`createApi` slice, lazy-injected by the manifest.

---

## 7. Docs + CI hygiene (PR-6)

### 7.1 OpenAPI dump

After PR-3 (routes land) regenerate the canonical spec:

```bash
cd backend
(cd ../docker && docker compose -f docker-compose.infra.yml up -d)
make openapi-dump
git add openapi/enterprise.json
```

`make openapi-check` in `ci-backend` fails the build if the committed
JSON drifts from a fresh dump — same gate that caught the Phase F log
levels drift (commit `b2e24b8`).

### 7.2 CLAUDE.md updates

- **Project root `CLAUDE.md`** — add a row to the "Optional" module
  table in §"Module Map":
  ```
  | **marketing**  | Contact base, importer pipeline, scoring, cards — [docs](backend/internal/addons/marketing/CLAUDE.md) | — |
  ```
  Update the "Optional (toggled at `/admin/modules`; all instantiated
  at boot):" sentence's module count if it includes one — currently
  reads "13 optional", becomes "14 optional".
- **`backend/CLAUDE.md`** — bump "7 core modules + 13 optional addons"
  → "+ 14 optional addons". Add `marketing/` to the addon tree in §"Project Structure".
- **`backend/internal/addons/marketing/CLAUDE.md`** (new) — follow the
  template used by subscriptions/sales: separate-Go-module preamble,
  responsibility split, data model summary referencing
  `docs/plans/marketing-addon/schemas/`, dependency declarations.

### 7.3 Memory updates (auto-memory)

After the merge to `dev` lands, add an entry to `MEMORY.md` under
"Active work" / "Released" — short pointer to a new
`project_marketing_addon_phase1.md` memory file with phase-by-phase
status and follow-ups.

### 7.4 CI gates checklist

Before merging `feature/marketing-addon → dev`:

- [ ] `make ci-backend` green — covers lint, tenantscope, policycoverage, tests, openapi-check, enterprise build.
- [ ] `make ci-frontend-admin` green — typecheck, eslint, tests, build.
- [ ] Profile matrix in `.github/workflows/backend.yml` builds all 6 SKUs (`starter|minimal|billing|ai|saas|enterprise`). Without the `addon_marketing` build tag the addon must not be linked — the catalog file's `//go:build` guard handles this.
- [ ] `make openapi-dump` produces no diff vs `openapi/enterprise.json`.
- [ ] Manual smoke: `docker-compose.enterprise.yml` boots, `/admin/modules` shows marketing as enabled, `/marketing/contacts` renders, a CSV import end-to-end completes.

---

## 8. Test plan

Repository tests (per module test conventions):

- `repository/*_test.go` — Mongo round-trip + index assertions per
  collection. Use `internal/testkit` or follow the in-tree pattern
  whichever applies for an extracted module (per memory
  [project_sdk_split_extractions], some extracted addons still use
  inline test helpers because testkit is backend-private).

Service tests:

- `services/dedup_test.go` — every dedup case (no match, email match,
  vat match, tax_code fallback, normalization edge cases).
- `services/custom_field_service_test.go` — validation matrix per
  field type, unknown-key rejection.

Importer tests:

- `importers/csv/adapter_test.go` — happy path, malformed rows, BOM
  handling, optional column mapping, large file (>1k rows).
- `importers/pipeline_test.go` — auto-merge of additive fields,
  conflict-skip behavior, sources[] append correctness.

Handler tests:

- `handlers/*_test.go` — auth/permission rejection, happy path, 4xx
  on validation errors. Use the same inline `authedCtx` pattern
  subscriptions adopted in Phase 5f (see
  `internal/addons/subscriptions/handlers/me_handler_test.go`).

Coverage target: ≥70% on services + importers (matches the recent
core unit-test push in `a39da63`).

---

## 9. Risks + open questions

- **Custom-field schema enforcement on import.** Phase 1 plan applies
  validation on direct API writes. The CSV importer must apply the
  *same* validator, otherwise import becomes a bypass. Resolution:
  the importer's `map` step calls
  `CustomFieldService.Validate` per row; rows that fail validation
  go to a `failed_rows` list on the job, the rest proceed. **Confirm
  this behavior with the user during PR-4 review.**
- **`active_card_ids` index on persons before cards ship.** Index is
  on an array field that is always empty in Phase 1. Mongo handles
  this fine (sparse-effective for empty arrays). Declared now to
  avoid a write-blocking index build in Phase 4.
- **Soft-match deferral.** Design §5.5 specifies `first+last+phone`
  soft-match for persons feeding the review queue. Phase 1 omits
  this; importers might surface duplicates that a Phase-3 reviewer
  would have caught. Acceptable trade-off — Phase 1 dedup is strict
  on `primary_email`, which is the dominant key in practice.
- **Public mirror tag**. Per the SDK split pattern, the addon
  publishes to `github.com/orkestra-cc/orkestra-addon-marketing` from
  `v0.1.0` once Phase 1 stabilizes. The first tag bump is
  out-of-scope for this plan — it lands as a separate release PR
  after `feature/marketing-addon` is in `dev` and has a `ci-all`
  green cycle. Update
  [`project_sdk_split_extractions`](../../memory/project_sdk_split_extractions.md)
  memory accordingly when that happens.
- **Italian-language naming**. The design doc is in Italian; the
  schema docs use Italian field labels in some places. All Go
  identifiers, BSON tags, JSON field names, and route paths in this
  plan are English to match the rest of the codebase. UI labels can
  follow either convention — Orkestra's existing admin UI uses
  Italian strings via i18n (see
  [`subscriptions.tsx::DisplayName`](skill) returning
  `"Sottoscrizioni"`); marketing should do the same:
  `DisplayName() = "Marketing"` (Italian-friendly cognate) but
  `Description()` Italian for parity.

---

## 10. Stepwise execution checklist

To track during implementation. Each item maps to one of the 6 sub-PRs
in §1.

**PR-1 — scaffolding**

- [ ] `git checkout -b feature/marketing-addon`
- [ ] Create `backend/internal/addons/marketing/` tree (go.mod, LICENSE, empty module.go that compiles, README.md).
- [ ] Add `replace` directive in `backend/go.mod` + `go mod tidy` from `backend/`.
- [ ] Create `backend/cmd/server/catalog_marketing.go`.
- [ ] Add to `Makefile` if any addon-specific target is needed (likely none — `addon_marketing` falls under enterprise auto-build).
- [ ] Verify `make build` (enterprise) + `make build-starter` (no addons) both pass.
- [ ] Boot enterprise stack locally, confirm `/admin/modules` lists marketing as disabled.

**PR-2 — data layer**

- [ ] Models + `collections.go`.
- [ ] Repositories with `tenantrepo.Scope` / `StampInsertM`.
- [ ] `Collections()` declares all 5 with indexes.
- [ ] Repo tests pass.
- [ ] `make backend-tenantscope` clean (no new baseline entries).

**PR-3 — API layer**

- [ ] Handlers + routes (per matrix in §4.1).
- [ ] `Permissions()` declared.
- [ ] `policies/marketing.cedar` written + `make policycoverage` clean.
- [ ] Handler tests pass.
- [ ] `make openapi-dump` produces a diff; commit it.

**PR-4 — importer**

- [ ] Importer interface + pipeline + CSV adapter.
- [ ] `Startable.Start()` launches the job poller; `Stop()` cancels.
- [ ] `marketing_import_jobs` (minimal schema) added to `Collections()`.
- [ ] Importer + pipeline tests pass.

**PR-5 — frontend**

- [ ] Manifest + RTK Query slice.
- [ ] 6 pages from §6.2 (each behind `Suspense + ProtectedRoute + ModuleGate`).
- [ ] URL-synced tabs on contact detail.
- [ ] `make ci-frontend-admin` green.

**PR-6 — docs + merge**

- [ ] Project-root CLAUDE.md table updated.
- [ ] `backend/CLAUDE.md` updated.
- [ ] `backend/internal/addons/marketing/CLAUDE.md` written.
- [ ] Memory pointer in `MEMORY.md` + new memory file.
- [ ] Final `openapi-dump` regen (catches drift from PR-3 onwards).
- [ ] PR: `feature/marketing-addon → dev` with a rollup description.

---

*Plan owner: claude. Track Phase 1 status here; spawn separate plans for Phase 2 (activity log + scoring), Phase 3 (excel/odoo + conflict review), Phase 4 (cards), Phase 5 (campaigns) once Phase 1 is in `dev`.*
