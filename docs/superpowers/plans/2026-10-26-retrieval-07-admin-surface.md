# Retrieval PR 7 — Administrative Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** give operators the D15 surface of ADR-0023: four permissions with their Cedar policy and a pinned seeding matrix, five operator-host routes (corpora, diagnostics, orphan drop, tenant indexing state, search log), and the `/admin/retrieval` console page.

**Architecture:** a new `AdminService` in `retrieval/services` reads the module's Mongo state through a new `repository.AdminStore` and the engine through narrow accessors on the existing `*services.Service`. Diagnostics re-use the PR 5 pipeline with `engine.Query.Diagnostics` switched on, under the admin's own principals. Huma handlers in `retrieval/handlers` are mounted from `module.go` with the llm `group()` gating pattern. The console page follows the `/admin/llm` page: URL-synced tabs, one RTK Query slice, en/it strings.

**Tech Stack:** Go 1.26.10, Huma v2, chi, mongo-driver v1.17.7, Cedar (cedar-go), React 19 + TypeScript 5.9, RTK Query, React Bootstrap, Vitest + MSW.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D1 table, D5, D10, D12 "Search audit", D13, D15) and the roadmap `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (row 7, "Shared contract", "Error codes, permissions, routes").

## Global Constraints

Every task includes the roadmap's **Global constraints** section verbatim (trailers, no new method on an existing `iface` interface, no cross-module `services/`/`repository/` imports, tenantrepo + `//tenantscope:allow` annotations, `RequireInternalTenant` on every entry point, errcode + errquality, `Retrieval` prefix on every HTTP-visible type, docs move with code, `make ci-backend` + `make ci-frontend-admin` before the PR, Mongo tests gated by `MONGO_TEST_URI`). PR-specific additions:

- Commit trailers on every commit, after a blank line: `Prop: upstream` then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Commit steps below show only the subject.
- Depends on PRs 5 and 6 being merged. Where this plan needs something the roadmap contract does not name (document fields of PR 4/5 collections, the pipeline function of PR 5, registry accessors of PR 3), the step says exactly what to read and what to add; it never renames or reshapes PR 3–6 code beyond the seam it names.
- Permission keys, exactly: `retrieval.corpus.read` (System), `retrieval.corpus.admin` (System), `retrieval.tenant.read` (org), `retrieval.searchlog.admin` (org). `retrieval.tenant.admin` is **not** declared (no route; policycoverage).
- Step-up freshness: `stepUpMaxAge = 5 * time.Minute` (same value as `backend/internal/core/llm/module.go:38`).
- No route returns indexed content or scores except diagnostics; corpus and tenant reports carry states and counts only (D15).
- Diagnostics have no bypass: same pipeline, same principals, same corpus permission, same search-log row as a consumer search (D10, D12).
- UI tasks: the implementer invokes the `orkestra-frontend-admin` skill **before** writing any `frontend-admin/` file and posts its pre-flight block. Production precedent: `frontend-admin/src/pages/admin/llm/index.tsx` (tabs, permission hook, no-access alert), `frontend-admin/src/pages/admin/compliance/AuditEventsTab.tsx` (paged, filtered log). References: `src/reference/components/tables/Tables.tsx`, `src/reference/components/ui/Modals.tsx`, `src/reference/components/forms/FormValidation.tsx`, `src/reference/components/forms/DatePicker.tsx`, `src/reference/components/ui/StatCards.tsx`.

## Review Focus

1. **A platform administrator diagnoses a query whose best match is shared with nobody they belong to**: the item is absent from the diagnostic hits exactly as from a consumer search (pinned in Task 5 by `TestDiagnose_NoBypassForPlatformAdmin`).
2. **A consumer search running after a diagnostic one**: `Search` never sets `engine.Query.Diagnostics` and its result carries no per-arm score (pinned in Task 5 by `TestSearch_NeverRequestsDiagnostics`).
3. **Dropping a corpus that is still registered, or while Qdrant is down**: 409 and nothing touched; on an engine outage mid-drop the state row survives so a retry finishes the drop (pinned in Task 6 by `TestDropOrphan_RefusesRegistered` and `TestDropOrphan_EngineOutageKeepsState`).
4. **Reading the search log of tenant A, or an empty page**: no row of tenant B is ever returned, and every read — an empty page included — emits exactly one `retrieval.searchlog.read` audit event (pinned in Task 4 by `TestAdminStore_SearchLogIsTenantScoped` and in Task 7 by `TestSearchLog_EmitsOneAuditEventEvenWhenEmpty`).
5. **A later change to authz seeding suffix rules**: `manager`, `operator`, `guest`, `org_member`, `org_viewer` and `org_billing` never hold `retrieval.searchlog.admin`, and production `developer` never holds an `.admin` key (pinned in Task 1 by `TestSeedSystemRoles_RetrievalMatrix`).

---

## File map

| File | Responsibility | Task |
|---|---|---|
| `backend/internal/core/retrieval/module.go` (modify) | `Permissions()`, `NavItems()`, admin wiring in `Init`, `RegisterRoutes` | 1, 8 |
| `backend/internal/core/retrieval/module_test.go` (modify) | permission list pin, route guards | 1, 8 |
| `backend/internal/core/authz/cedar/policies/retrieval.cedar` (create) | explicit Cedar coverage of the four keys | 1 |
| `backend/internal/core/authz/cedar/retrieval_policy_test.go` (create) | Cedar verdicts for the four keys | 1 |
| `backend/internal/core/authz/services/seeding_retrieval_test.go` (create) | D15 seeding matrix pinned against `SeedSystemRoles` | 1 |
| `backend/internal/shared/errcode/codes.go`, `codes_test.go` (modify) | `retrieval.*` codes + golden rows | 2 |
| `backend/internal/core/retrieval/handlers/errors.go` (create) + test | sentinel → errcode mapping | 2 |
| `backend/internal/core/retrieval/models/admin.go` (create) | HTTP DTOs (`Retrieval*`) and internal read views | 3 |
| `backend/internal/core/retrieval/repository/admin_store.go` (create) + test | Mongo reads/deletes for the admin surface | 4 |
| `backend/internal/core/retrieval/services/admin_runtime.go` (create) | accessors on `*Service` the admin needs | 3 |
| `backend/internal/core/retrieval/services/admin.go` (create) + test | `AdminService`: reports, drop, search log, audit | 3, 6, 7 |
| `backend/internal/core/retrieval/services/diagnostics.go` (create) + test | `(*Service).Diagnose` over the PR 5 pipeline | 5 |
| `backend/internal/core/retrieval/handlers/admin.go` (create) + test | Huma handlers | 8 |
| `backend/internal/core/retrieval/routes.go` (create) | `huma.Register` functions | 8 |
| `backend/openapi/enterprise.json` (regenerate) | `make openapi-dump` | 8 |
| `frontend-admin/src/store/api/retrievalApi.ts` (create) + test; `baseApi.ts` (modify) | RTK slice, tags | 9 |
| `frontend-admin/src/pages/admin/retrieval/*` (create) | page, tabs, modals, permission hook, error helper, tests | 9, 10 |
| `frontend-admin/src/routes/coreRoutes.tsx` (modify) | `/admin/retrieval` route | 10 |
| `frontend-admin/src/locales/en.json`, `it.json` (modify) | `adminRetrieval.*`, `errors.retrieval.*` | 9, 10 |
| `backend/internal/core/retrieval/AGENTS.md`, `docs/site/modules/core/retrieval.mdx` (modify) | routes, RBAC, admin surface | 11 |

---

### Task 1: Permissions, Cedar policy and the D15 seeding matrix

**Files:**
- Modify: `backend/internal/core/retrieval/module.go` (add `Permissions()`)
- Modify: `backend/internal/core/retrieval/module_test.go`
- Create: `backend/internal/core/authz/cedar/policies/retrieval.cedar`
- Create: `backend/internal/core/authz/cedar/retrieval_policy_test.go`
- Create: `backend/internal/core/authz/services/seeding_retrieval_test.go`

**Interfaces:**
- Consumes: `iface.PermissionSpec{Key, Module, Description, System}`; authz test helpers `newTier1Service`, `staticRoleLookup` (`services/tier1_test.go:28-43`), `fakeRepo.roleByName` (`services/repo_fake_test.go`); cedar test helpers `newTestEngine`, `decidedBy` (`cedar/engine_test.go:11-39`).
- Produces: `func (m *Module) Permissions() []iface.PermissionSpec` returning exactly the four keys below; Task 8's routes use these literals.

- [ ] **Step 1: Write the failing module test**

In `module_test.go`:

```go
func TestModule_Permissions(t *testing.T) {
	want := map[string]bool{ // key -> System
		"retrieval.corpus.read":     true,
		"retrieval.corpus.admin":    true,
		"retrieval.tenant.read":     false,
		"retrieval.searchlog.admin": false,
	}
	got := map[string]bool{}
	for _, p := range NewModule().Permissions() {
		if p.Module != "retrieval" || p.Description == "" {
			t.Errorf("%s: module %q, description %q", p.Key, p.Module, p.Description)
		}
		got[p.Key] = p.System
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("permissions = %v, want %v (retrieval.tenant.admin arrives with reconciliation, step 2)", got, want)
	}
}
```

(`NewModule` is PR 3's constructor; call it the way PR 3's own `module_test.go` does.)

- [ ] **Step 2: Write the failing seeding-matrix test**

`authz/services/seeding_retrieval_test.go` (package `services`). It registers the four keys exactly as `TestModule_Permissions` pins them, plus one unrelated key per suffix so the catalog is not degenerate, seeds, and compares holders per key:

```go
var retrievalSpecs = []iface.PermissionSpec{
	{Key: "retrieval.corpus.read", Module: "retrieval", System: true},
	{Key: "retrieval.corpus.admin", Module: "retrieval", System: true},
	{Key: "retrieval.tenant.read", Module: "retrieval"},
	{Key: "retrieval.searchlog.admin", Module: "retrieval"},
	{Key: "tenant.read", Module: "tenant"},
	{Key: "system.modules.admin", Module: "system", System: true},
}

var allSeededRoles = []string{"super_admin", "administrator", "developer", "manager", "operator", "guest",
	"org_owner", "org_admin", "org_member", "org_billing", "org_viewer"}

// holders returns the seeded roles whose permission set contains key or "*", sorted.
func holders(repo *fakeRepo, key string) []string

func TestSeedSystemRoles_RetrievalMatrix(t *testing.T) {
	cases := []struct {
		production bool
		want       map[string][]string
	}{
		{false, map[string][]string{
			"retrieval.corpus.read":     {"administrator", "developer", "guest", "manager", "operator", "super_admin"},
			"retrieval.corpus.admin":    {"administrator", "developer", "super_admin"},
			"retrieval.tenant.read":     {"administrator", "developer", "guest", "manager", "operator", "org_admin", "org_member", "org_owner", "org_viewer", "super_admin"},
			"retrieval.searchlog.admin": {"administrator", "developer", "org_admin", "org_owner", "super_admin"},
		}},
		{true, map[string][]string{
			"retrieval.corpus.read":     {"administrator", "developer", "guest", "manager", "operator", "super_admin"},
			"retrieval.corpus.admin":    {"administrator", "super_admin"},
			"retrieval.tenant.read":     {"administrator", "developer", "guest", "manager", "operator", "org_admin", "org_member", "org_owner", "org_viewer", "super_admin"},
			"retrieval.searchlog.admin": {"administrator", "org_admin", "org_owner", "super_admin"},
		}},
	}
	// for each case: svc, repo := newTier1Service(t, staticRoleLookup("")); svc.production = c.production;
	// RegisterPermissions(retrievalSpecs); SeedSystemRoles; every one of allSeededRoles exists;
	// reflect.DeepEqual(holders(repo, key), want[key]) for each key, failing with the env and key.
}
```

These lists were derived from `SeedSystemRoles` (`authz/services/service.go:901-1007`): operator `.read`/`.self`; guest `.read`; manager all minus `.delete`/`.admin`; developer all, production `.read`/`.view`/`.self`; administrator all; super_admin `*`; org roles exclude System keys, org_admin minus `.delete`, org_member `.read`/`.view`/`.self`/`.own`, org_viewer `.read`/`.view`, org_billing finance prefixes only.

- [ ] **Step 3: Write the failing Cedar test**

`authz/cedar/retrieval_policy_test.go`:

```go
func TestRetrievalPolicies(t *testing.T) {
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}
	mfa := func(p Principal) Principal { p.MFAEnrolled = true; return p } // engine.go:91
	cases := []struct {
		name   string
		env    string
		p      Principal
		action string
		allow  bool
		policy string // must be among d.Reasons when allow; "" = do not check
	}{
		{"administrator reads corpora", "production", mfa(Principal{UserUUID: "u", SystemRole: "administrator"}), "retrieval.corpus.read", true, "retrieval.corpus_read.platform_roles"},
		{"administrator administers corpora", "production", mfa(Principal{UserUUID: "u", SystemRole: "administrator"}), "retrieval.corpus.admin", true, "retrieval.corpus_admin.platform_admins"},
		{"administrator without MFA is refused", "production", Principal{UserUUID: "u", SystemRole: "administrator"}, "retrieval.corpus.admin", false, ""},
		{"prod developer cannot administer", "production", Principal{UserUUID: "u", SystemRole: "developer"}, "retrieval.corpus.admin", false, ""},
		{"org_admin reads the search log", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_admin"}}, "retrieval.searchlog.admin", true, "retrieval.searchlog_admin.org_admins"},
		{"org_member cannot read the search log", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_member"}}, "retrieval.searchlog.admin", false, ""},
		{"org_viewer cannot read the search log", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_viewer"}}, "retrieval.searchlog.admin", false, ""},
		{"org_viewer reads tenant indexing", "production", Principal{UserUUID: "u", TenantRoles: []string{"org_viewer"}}, "retrieval.tenant.read", true, ""},
	}
	// for each: d := newTestEngine(t, c.env).IsAuthorized(c.p, c.action, r); assert d.Allowed == c.allow,
	// len(d.Errors) == 0, and decidedBy(d, c.policy) when c.policy != "".
}
```

- [ ] **Step 4: Run the three tests to verify they fail**

Run: `cd backend && go test ./internal/core/retrieval/ -run TestModule_Permissions && go test ./internal/core/authz/... -run 'TestSeedSystemRoles_RetrievalMatrix|TestRetrievalPolicies'`
Expected: FAIL — `Permissions` returns nothing; the matrix test fails only if the seeding code disagrees (it should PASS once it compiles — it pins current behaviour, which is the point); the Cedar test FAILS on the missing policy ids.

- [ ] **Step 5: Implement `Permissions()` and `retrieval.cedar`**

`Permissions()` returns the four specs with `Module: "retrieval"` and these descriptions: corpus.read "Read corpus states, generations, engine and inference health and counters (never content or scores)"; corpus.admin "Run per-stage score diagnostics and drop orphan corpora"; tenant.read "Read the organization's indexing state and backlog counts"; searchlog.admin "Read the organization's search log (who searched what, and whom the results described)". Comment above it: only keys with a route are declared (policycoverage); `retrieval.tenant.admin` arrives in step 2.

`retrieval.cedar`, header comment modelled on `llm.cedar` (why explicit permits exist, `.admin` suffix ⇒ abac MFA forbid, `.read` suffix-covered for tenant roles). Three policies:
- `@id("retrieval.corpus_read.platform_roles")` permit `Action::"retrieval.corpus.read"` when `system_role` ∈ {super_admin, administrator, developer};
- `@id("retrieval.corpus_admin.platform_admins")` permit `Action::"retrieval.corpus.admin"` when `system_role` ∈ {super_admin, administrator};
- `@id("retrieval.searchlog_admin.org_admins")` permit `Action::"retrieval.searchlog.admin"` when `principal has tenant_roles && (principal.tenant_roles.contains("org_owner") || principal.tenant_roles.contains("org_admin")) && resource is Orkestra::Tenant`.
`retrieval.tenant.read` is named in a comment only (covered by the `action_suffix == "read"` permits of `tenant_roles.cedar` and `platform.cedar`).

- [ ] **Step 6: Run the tests to verify they pass**

Run: same as Step 4.
Expected: PASS. Note: `make backend-policycoverage` reports `permission.declared.unused` for the four keys until Task 8 mounts the routes; do not baseline them.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/core/retrieval/module.go backend/internal/core/retrieval/module_test.go \
  backend/internal/core/authz/cedar/policies/retrieval.cedar backend/internal/core/authz/cedar/retrieval_policy_test.go \
  backend/internal/core/authz/services/seeding_retrieval_test.go
git commit -m "feat(retrieval): declare D15 permissions, Cedar policy and pin the seeding matrix"
```

---

### Task 2: Error codes and the handler error mapping

**Files:**
- Modify: `backend/internal/shared/errcode/codes.go` (new `// --- retrieval ---` block after the llm block, ~L470)
- Modify: `backend/internal/shared/errcode/codes_test.go` (`goldenCodes`)
- Create: `backend/internal/core/retrieval/handlers/errors.go`, `handlers/errors_test.go`

**Interfaces:**
- Consumes: `iface.ErrRetrieval*` sentinels (roadmap, PR 3); `services.ErrCorpusRegistered`, `services.ErrCorpusNotFound` (declared here in Task 2 in `services/admin.go` so the mapping compiles; Task 6 uses them).
- Produces: `func mapError(err error) error` in package `handlers`; the codes below.

| Const | Value | Status (builder) | Raised for |
|---|---|---|---|
| `RetrievalNotConfigured` | `retrieval.not_configured` | 503 `FeatureNotConfigured` | `iface.ErrRetrievalNotConfigured` |
| `RetrievalUnavailable` | `retrieval.unavailable` | 503 `ServiceUnavailable` | `iface.ErrRetrievalUnavailable` |
| `RetrievalCorpusNotFound` | `retrieval.corpus_not_found` | 404 | `iface.ErrRetrievalUnknownCorpus`, `services.ErrCorpusNotFound` |
| `RetrievalCorpusRegistered` | `retrieval.corpus_registered` | 409 | `services.ErrCorpusRegistered` |
| `RetrievalCorpusUnavailable` | `retrieval.corpus_unavailable` | 503 `ServiceUnavailable` | `iface.ErrRetrievalCorpusUnavailable` |
| `RetrievalForbidden` | `retrieval.forbidden` | 403 | `iface.ErrRetrievalForbidden` |
| `RetrievalInvalidFilter` | `retrieval.invalid_filter` | 422 | `iface.ErrRetrievalInvalidFilter` |
| `RetrievalInvalidRequest` | `retrieval.invalid_request` | 422 | `iface.ErrRetrievalInvalidRequest` |
| `RetrievalLocalityViolation` | `retrieval.locality_violation` | 409 | `iface.ErrRetrievalLocality` |
| `RetrievalVisibilityCheckFailed` | `retrieval.visibility_check_failed` | 503 `ServiceUnavailable` | `iface.ErrRetrievalVisibilityCheck` |
| `RetrievalInternal` | `retrieval.internal` | 500 `Internal` | anything else (errquality R3) |

- [ ] **Step 1: Write the failing tests**

Add the eleven rows to `goldenCodes`. In `handlers/errors_test.go`:

```go
func TestMapError(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{iface.ErrRetrievalNotConfigured, 503, errcode.RetrievalNotConfigured},
		{fmt.Errorf("wrap: %w", iface.ErrRetrievalUnavailable), 503, errcode.RetrievalUnavailable},
		{iface.ErrRetrievalUnknownCorpus, 404, errcode.RetrievalCorpusNotFound},
		{services.ErrCorpusNotFound, 404, errcode.RetrievalCorpusNotFound},
		{services.ErrCorpusRegistered, 409, errcode.RetrievalCorpusRegistered},
		{iface.ErrRetrievalCorpusUnavailable, 503, errcode.RetrievalCorpusUnavailable},
		{iface.ErrRetrievalForbidden, 403, errcode.RetrievalForbidden},
		{iface.ErrRetrievalInvalidFilter, 422, errcode.RetrievalInvalidFilter},
		{iface.ErrRetrievalInvalidRequest, 422, errcode.RetrievalInvalidRequest},
		{iface.ErrRetrievalLocality, 409, errcode.RetrievalLocalityViolation},
		{iface.ErrRetrievalVisibilityCheck, 503, errcode.RetrievalVisibilityCheckFailed},
		{errors.New("mongo: dial tcp 10.0.0.3:27017"), 500, errcode.RetrievalInternal},
	}
	// each: e := mapError(c.err).(*errcode.Error); e.Status == c.status, e.Code == c.code,
	// and !strings.Contains(e.Detail, c.err.Error()) (errquality R1 — no wrapped text in details).
}

func TestMapError_NotConfiguredIsExpected(t *testing.T) {
	// mapError(iface.ErrRetrievalNotConfigured).(*errcode.Error).ExpectedUnavailable() == true
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/shared/errcode/ ./internal/core/retrieval/handlers/`
Expected: FAIL — undefined consts / `mapError`.

- [ ] **Step 3: Implement**

Declare the eleven consts with one-line comments (status + meaning) in `codes.go`. In `services/admin.go` declare `var ErrCorpusRegistered = errors.New("retrieval: corpus is still registered")` and `var ErrCorpusNotFound = errors.New("retrieval: no state for this corpus")`. `mapError` is an `errors.Is` switch in the table's order with fixed English details (e.g. "The vector engine is not configured on this installation."); the `default` branch logs nothing and returns `errcode.Internal(errcode.RetrievalInternal, "The retrieval request could not be completed.")`. Handlers log the cause themselves (Task 8).

- [ ] **Step 4: Run to verify pass**

Run: same as Step 2. Expected: PASS (both golden tests included).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/shared/errcode/ backend/internal/core/retrieval/handlers/errors.go \
  backend/internal/core/retrieval/handlers/errors_test.go backend/internal/core/retrieval/services/admin.go
git commit -m "feat(retrieval): add retrieval.* error codes and handler mapping"
```

---

### Task 3: Admin DTOs, read views and runtime accessors

**Files:**
- Create: `backend/internal/core/retrieval/models/admin.go`
- Create: `backend/internal/core/retrieval/services/admin_runtime.go`, `services/admin_runtime_test.go`

**Interfaces:**
- Consumes (PR 3/5 internals — read `services/registry.go`, `services/search.go` and `module.go` first): the corpus registry's stored specs, the "owning module started" check of D5 step 1, the engine handle built from `QDRANT_URL`, the collection-name rule `<prefix>_<corpus>_g<generation>`.
- Produces (used by Tasks 4–8):

```go
// models/admin.go — HTTP-visible (package-less Huma names, Retrieval prefix)
type RetrievalComponentStatus string // "ok" | "not_configured" | "unavailable" | "mismatch"

type RetrievalEngineStatus struct {
	Status RetrievalComponentStatus `json:"status" enum:"ok,not_configured,unavailable"`
}
type RetrievalInferenceStatus struct {
	Endpoint string                   `json:"endpoint"`
	Kind     string                   `json:"kind" enum:"embed,rerank"`
	Status   RetrievalComponentStatus `json:"status" enum:"ok,not_configured,unavailable,mismatch"`
	Profile  string                   `json:"profile,omitempty"` // InferenceProfile.ID when ok
}
type RetrievalBacklog struct {
	Pending  int64      `json:"pending"`
	OldestAt *time.Time `json:"oldestAt,omitempty"`
}
type RetrievalCorpusStatus struct {
	Name               string                  `json:"name"`
	Owner              string                  `json:"owner,omitempty"`
	OwnerRunning       bool                    `json:"ownerRunning"`
	Registered         bool                    `json:"registered"` // false = orphan: state row, no registration
	Locality           iface.RetrievalLocality `json:"locality,omitempty"`
	EmbedEndpoint      string                  `json:"embedEndpoint,omitempty"`
	RerankEnabled      bool                    `json:"rerankEnabled"`
	State              string                  `json:"state" enum:"uninitialized,building,active,profile_changed"`
	ActiveGeneration   int                     `json:"activeGeneration"`
	BuildingGeneration int                     `json:"buildingGeneration,omitempty"`
	IngestionProfile   string                  `json:"ingestionProfile,omitempty"` // hash only
	Outbox             RetrievalBacklog        `json:"outbox"`
	TenantsDone        int64                   `json:"tenantsDone"`
	TenantsBuilding    int64                   `json:"tenantsBuilding"`
}
type RetrievalErasureCounts struct {
	Pending int64 `json:"pending"` // status pending
	Fencing int64 `json:"fencing"` // first pass done, waiting for the fencing window
}
type RetrievalCorporaReport struct {
	Engine    RetrievalEngineStatus      `json:"engine"`
	Inference []RetrievalInferenceStatus `json:"inference"`
	Corpora   []RetrievalCorpusStatus    `json:"corpora"` // registered first (by name), then orphans (by name)
	Erasures  RetrievalErasureCounts     `json:"erasures"`
}
type RetrievalTenantCorpus struct {
	Corpus       string           `json:"corpus"`
	Owner        string           `json:"owner"`
	OwnerRunning bool             `json:"ownerRunning"`
	InitialBuild string           `json:"initialBuild" enum:"pending,building,done"`
	Points       *int64           `json:"points,omitempty"` // active generation, this tenant; absent unless engine ok
	Outbox       RetrievalBacklog `json:"outbox"`
}
type RetrievalTenantIndexingReport struct {
	TenantID        string                  `json:"tenantId"`
	Engine          RetrievalEngineStatus   `json:"engine"`
	Corpora         []RetrievalTenantCorpus `json:"corpora"` // registered corpora only, by name
	ErasuresPending int64                   `json:"erasuresPending"` // tenant-scope tombstones not done
}
type RetrievalDiagnosticsRequest struct {
	Query  string                 `json:"query" minLength:"1" maxLength:"8000"` // rune bound 2000 enforced by the pipeline
	Filter *iface.RetrievalFilter `json:"filter,omitempty"`
	Mode   iface.RetrievalMode    `json:"mode,omitempty" enum:"hybrid,dense,keyword"`
	Limit  int                    `json:"limit,omitempty" minimum:"1" maximum:"100" default:"10"`
}
type RetrievalDiagnosticHit struct {
	SourceID     string   `json:"sourceId"`
	ChunkID      string   `json:"chunkId"`
	Rank         int      `json:"rank"`
	Score        float32  `json:"score"`  // final, as a consumer would see it
	Fused        float32  `json:"fused"`  // RRF score before rerank
	Dense        *float32 `json:"dense,omitempty"`
	Keyword      *float32 `json:"keyword,omitempty"`
	Rerank       *float32 `json:"rerank,omitempty"`
	Snippet      string   `json:"snippet"`
}
type RetrievalDiagnosticsReport struct {
	Corpus     string                   `json:"corpus"`
	Collection string                   `json:"collection"`
	Profile    string                   `json:"profile"` // embedding profile pinned for the request
	Principals []string                 `json:"principals"` // the caller's own, as applied
	Degraded   bool                     `json:"degraded"`
	Hits       []RetrievalDiagnosticHit `json:"hits"`
}
type RetrievalSearchLogEntry struct {
	ID                string                 `json:"id"`
	CreatedAt         time.Time              `json:"createdAt"`
	UserUUID          string                 `json:"userUuid"`
	Corpus            string                 `json:"corpus"`
	Query             string                 `json:"query"`
	Filter            *iface.RetrievalFilter `json:"filter,omitempty"`
	Principals        []string               `json:"principals"`
	ReturnedSourceIDs []string               `json:"returnedSourceIds"`
	Subjects          []string               `json:"subjects"`
	Degraded          bool                   `json:"degraded"`
	DurationMs        int64                  `json:"durationMs"`
}
type RetrievalSearchLogPage struct {
	Items  []RetrievalSearchLogEntry `json:"items"`
	Total  int64                     `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

// Internal (no JSON tags needed beyond bson mapping done in repository)
type CorpusStateView struct {
	Corpus             string
	ActiveGeneration   int
	BuildingGeneration int
	ProfileChanged     bool
	IngestionProfile   string
}
type SearchLogQuery struct {
	UserUUID      string
	Since, Until  *time.Time
	Limit, Offset int
}
type ProgressCounts struct{ Done, Building int64 }
```

```go
// services/admin_runtime.go — accessors on PR 3/5's *Service
func (s *Service) RegisteredCorpora() []iface.RetrievalCorpusSpec       // sorted by Name; copies
func (s *Service) OwnerRunning(owner string) bool                       // same check as D5 step 1
func (s *Service) Engine() (engine.Engine, error)                       // iface.ErrRetrievalNotConfigured when QDRANT_URL is empty
func (s *Service) CollectionName(corpus string, generation int) string  // "<prefix>_<corpus>_g<generation>"

// AdminRuntime is what AdminService needs from *Service (Task 5 adds Diagnose).
type AdminRuntime interface {
	RegisteredCorpora() []iface.RetrievalCorpusSpec
	OwnerRunning(owner string) bool
	Engine() (engine.Engine, error)
	CollectionName(corpus string, generation int) string
	Diagnose(ctx context.Context, req iface.RetrievalSearchRequest) (*DiagnosticResult, error)
}
```

If PR 3/5 already export an accessor with the same meaning, the new method delegates to it (one line); do not duplicate the logic.

- [ ] **Step 1: Write the failing test**

```go
func TestAdminRuntime_Accessors(t *testing.T) {
	// Build a *Service the way PR 3's registry tests do, with QDRANT_URL unset and
	// QDRANT_COLLECTION_PREFIX "orkestra"; register corpora "zeta" and "alpha".
	// RegisteredCorpora() names == []string{"alpha", "zeta"};
	// mutating the returned slice does not change a second call;
	// CollectionName("alpha", 3) == "orkestra_alpha_g3";
	// _, err := Engine(); errors.Is(err, iface.ErrRetrievalNotConfigured).
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/services/ -run TestAdminRuntime_Accessors`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Implement the accessors and `models/admin.go`**

`Diagnose` and `DiagnosticResult` are declared in Task 5; until then keep `AdminRuntime` without `Diagnose` and add the method to the interface in Task 5.

- [ ] **Step 4: Run to verify pass**

Run: same as Step 2. Expected: PASS. Also `go vet ./internal/core/retrieval/...`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/models/admin.go backend/internal/core/retrieval/services/admin_runtime*.go
git commit -m "feat(retrieval): admin DTOs and runtime accessors"
```

---

### Task 4: `AdminStore` — Mongo reads and corpus-row deletes

**Files:**
- Create: `backend/internal/core/retrieval/repository/admin_store.go`, `repository/admin_store_test.go`
- Modify: `backend/internal/core/retrieval/module.go` `Collections()` (search-log indexes)

**Interfaces:**
- Consumes: the PR 4/5 documents of `retrieval_corpus_state`, `retrieval_tenant_progress`, `retrieval_outbox`, `retrieval_erasures` (roadmap `Erasure` + PR 6 `Status`), `retrieval_search_log`. Read their `models/*.go` before writing the queries: field names come from those structs, never from this plan. `tenantrepo.Scope`, `tenantrepo.CurrentTenantID`. The repository package's `newTestDB` helper (pattern `backend/internal/core/llm/repository/testdb_test.go`; reuse PR 3's).
- Produces:

```go
type AdminStore struct{ /* *mongo.Database */ }
func NewAdminStore(db *mongo.Database) *AdminStore
var ErrNotFound // reuse the package's existing not-found sentinel if PR 3 declared one

// Installation-wide (each query carries //tenantscope:allow system: admin counters across tenants)
func (r *AdminStore) CorpusStates(ctx context.Context) ([]models.CorpusStateView, error)
func (r *AdminStore) CorpusState(ctx context.Context, corpus string) (*models.CorpusStateView, error) // ErrNotFound
func (r *AdminStore) OutboxBacklogAll(ctx context.Context) (map[string]models.RetrievalBacklog, error) // by corpus
func (r *AdminStore) ProgressCounts(ctx context.Context) (map[string]models.ProgressCounts, error)    // by corpus
func (r *AdminStore) ErasureCounts(ctx context.Context) (models.RetrievalErasureCounts, error)
func (r *AdminStore) DeleteCorpusRows(ctx context.Context, corpus string) error   // outbox + tenant progress of the corpus, all tenants
func (r *AdminStore) DeleteCorpusState(ctx context.Context, corpus string) error

// Tenant in ctx (tenantrepo.Scope)
func (r *AdminStore) OutboxBacklogTenant(ctx context.Context) (map[string]models.RetrievalBacklog, error)
func (r *AdminStore) TenantInitialBuild(ctx context.Context) (map[string]string, error) // corpus -> "pending"|"building"|"done"
func (r *AdminStore) TenantErasuresPending(ctx context.Context) (int64, error)        // scope "tenant", status != "done"
func (r *AdminStore) SearchLog(ctx context.Context, q models.SearchLogQuery) ([]models.RetrievalSearchLogEntry, int64, error)
```

"Pending" outbox rows are the rows PR 4's worker has not yet applied (its own pending predicate; read `services/outbox.go`). `OldestAt` is the minimum enqueue time among them. `SearchLog` sorts by `createdAt` descending, then `_id` descending; `Since`/`Until` are inclusive bounds on `createdAt`.

`Collections()` gains, on `retrieval_search_log`, the indexes `{tenantId:1, createdAt:-1}` and `{tenantId:1, userUuid:1, createdAt:-1}` (use PR 5's field names; skip either if PR 5 already declares the same keys).

- [ ] **Step 1: Write the failing tests** (gated: `newTestDB` skips without `MONGO_TEST_URI`)

```go
func TestAdminStore_SearchLogIsTenantScoped(t *testing.T) {
	// insert 3 rows for tenant "tA" (users u1,u1,u2; createdAt day1,day2,day3) and 2 for "tB";
	// ctx scoped to tA:
	//   SearchLog(ctx, {Limit:50}) -> 3 items, total 3, newest first, none with tenant tB;
	//   SearchLog(ctx, {UserUUID:"u1", Limit:50}) -> 2;
	//   SearchLog(ctx, {Since:&day2, Until:&day2, Limit:50}) -> 1 (inclusive);
	//   SearchLog(ctx, {Limit:1, Offset:1}) -> 1 item (the day2 row), total 3.
	// ctx with no tenant -> error (tenantrepo refuses), never all rows.
}

func TestAdminStore_CountsAndBacklog(t *testing.T) {
	// outbox: corpus "crm" 2 pending in tA (enqueued t0, t1) + 1 pending in tB (t2) + 1 applied in tA;
	// OutboxBacklogAll -> crm {Pending:3, OldestAt:t0}; OutboxBacklogTenant(tA) -> crm {2, t0}.
	// progress: crm tA done, tB building -> ProgressCounts["crm"] == {1,1}; TenantInitialBuild(tA)["crm"] == "done".
	// erasures: global user pending, tenant tA item fencing, tenant tA subject done
	//   -> ErasureCounts == {Pending:1, Fencing:1}; TenantErasuresPending(tA) == 1.
}

func TestAdminStore_DeleteCorpusRows(t *testing.T) {
	// rows for corpora "old" and "crm" in tA and tB; DeleteCorpusRows("old") removes every
	// "old" outbox/progress row in both tenants and none of "crm"; DeleteCorpusState("old")
	// then CorpusState("old") -> ErrNotFound; erasure rows are untouched.
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && MONGO_TEST_URI=<replica-set uri> go test ./internal/core/retrieval/repository/ -run TestAdminStore`
Expected: FAIL — undefined `AdminStore`.

- [ ] **Step 3: Implement `AdminStore`**

Counts use `CountDocuments` / a `$group` aggregate; tenant-scoped reads go through `tenantrepo.Scope` (aggregates through `tenantrepo.ScopeAggregate`).

- [ ] **Step 4: Run to verify pass**

Run: same as Step 2, then `make backend-tenantscope` from the repository root.
Expected: PASS; tenantscope clean.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/repository/admin_store*.go backend/internal/core/retrieval/module.go
git commit -m "feat(retrieval): admin store over corpus state, outbox, progress, erasures and search log"
```

---

### Task 5: Diagnostics over the PR 5 pipeline

**Files:**
- Create: `backend/internal/core/retrieval/services/diagnostics.go`, `services/diagnostics_test.go`
- Modify: the PR 5 pipeline file (`services/search.go` or wherever `(*Service).Search` lives)
- Modify: `backend/internal/core/retrieval/services/admin_runtime.go` (add `Diagnose` to `AdminRuntime`)

**Interfaces:**
- Consumes: `(*Service).Search` and its pipeline (PR 5); `engine.Query.Diagnostics`, `engine.Candidate.DenseScore`/`KeywordScore` (roadmap engine contract); PR 5's search test harness (fake engine, fake source, fake principal resolver) in `services/*_test.go`.
- Produces:

```go
type StageScores struct {
	ChunkID string
	Fused   float32
	Dense   *float32
	Keyword *float32
	Rerank  *float32
}
type DiagnosticResult struct {
	Result     iface.RetrievalSearchResult
	Collection string
	Profile    string   // embedding profile ID pinned at D5 step 2
	Principals []string // as applied at D5 step 3
	Scores     []StageScores // one per Result.Hits[i], same order
}
// Diagnose runs exactly the D5 pipeline Search runs — same gate, same principals,
// same erasure veto and Visible check, same search-log row — with
// engine.Query.Diagnostics set, and returns the per-arm scores.
func (s *Service) Diagnose(ctx context.Context, req iface.RetrievalSearchRequest) (*DiagnosticResult, error)
```

The only change to PR 5's code: its pipeline function takes a `diagnostics bool` (or an options struct) that sets `engine.Query.Diagnostics` and keeps, per surviving candidate, `Score` (fused), `DenseScore`, `KeywordScore` and the rerank score through steps 6–8. `Search` passes `false` and returns what it returned before.

- [ ] **Step 1: Write the failing tests**

```go
func TestDiagnose_ReturnsArmScores(t *testing.T) {
	// harness: corpus "crm", items c1 (visibleTo tenant:*) and c2 (tenant:*), caller with tenant:*;
	// fake engine returns candidates with DenseScore/KeywordScore set when q.Diagnostics.
	// res := svc.Diagnose(ctx, {Corpus:"crm", Query:"sales lead"});
	// len(res.Scores) == len(res.Result.Hits); Scores[i].ChunkID == Hits[i].ChunkID;
	// Scores[0].Dense != nil && Scores[0].Keyword != nil; res.Collection == "orkestra_crm_g1";
	// res.Principals contains "user:<caller>" and "tenant:*"; fake engine saw Diagnostics == true.
}

func TestDiagnose_NoBypassForPlatformAdmin(t *testing.T) {
	// caller ctx: system role "administrator" (testkit identity), principals {user:admin, tenant:*};
	// item secret visibleTo ["user:someone-else"] scores highest in the fake engine.
	// Diagnose(..) hits contain no sourceId "secret"; the fake engine received
	// Principals without "user:someone-else"; and Visible() was called for every candidate.
}

func TestDiagnose_WritesSearchLogRow(t *testing.T) {
	// after Diagnose, the harness's search-log writer recorded exactly one row with the
	// caller's user, the corpus and the returned source ids — same as after Search.
}

func TestSearch_NeverRequestsDiagnostics(t *testing.T) {
	// Search(ctx, req) with the same harness: every engine.Query the fake received has
	// Diagnostics == false; the iface.RetrievalSearchResult JSON has no "dense"/"keyword" keys.
}

func TestDiagnose_PropagatesGateErrors(t *testing.T) {
	// caller without the corpus permission -> errors.Is(err, iface.ErrRetrievalForbidden);
	// unknown corpus -> iface.ErrRetrievalUnknownCorpus; QDRANT_URL unset -> iface.ErrRetrievalNotConfigured.
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/services/ -run 'TestDiagnose|TestSearch_NeverRequestsDiagnostics'`
Expected: FAIL — `Diagnose` undefined.

- [ ] **Step 3: Implement `Diagnose` and thread the flag through the pipeline**

- [ ] **Step 4: Run the whole services package**

Run: `cd backend && go test ./internal/core/retrieval/services/`
Expected: PASS — including every PR 5 search test, unchanged.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services/
git commit -m "feat(retrieval): per-stage score diagnostics over the normal pipeline"
```

---

### Task 6: `AdminService` reports and orphan drop

**Files:**
- Modify: `backend/internal/core/retrieval/services/admin.go`
- Create: `backend/internal/core/retrieval/services/admin_test.go`

**Interfaces:**
- Consumes: `AdminRuntime` (Tasks 3, 5); `AdminStore` methods (Task 4) through the interface below; `iface.LLMInferenceProfiles` (PR 1; may be nil); `iface.AuditSink`.
- Produces:

```go
type AdminStoreAPI interface { /* the eleven AdminStore methods of Task 4, same signatures */ }

type AdminDeps struct {
	Runtime  AdminRuntime
	Store    AdminStoreAPI
	Profiles iface.LLMInferenceProfiles // nil: every inference row reports not_configured
	Audit    func() iface.AuditSink     // resolved per call; nil sink = no audit
	Logger   *slog.Logger
}
func NewAdminService(d AdminDeps) *AdminService

func (a *AdminService) Corpora(ctx context.Context) (*models.RetrievalCorporaReport, error)
func (a *AdminService) TenantIndexing(ctx context.Context) (*models.RetrievalTenantIndexingReport, error)
func (a *AdminService) Diagnose(ctx context.Context, corpus string, in models.RetrievalDiagnosticsRequest) (*models.RetrievalDiagnosticsReport, error)
func (a *AdminService) DropOrphanCorpus(ctx context.Context, corpus string) error
// SearchLog: Task 7
```

Rules the tests pin:
- `Corpora`: engine status from `Runtime.Engine()` (`ErrRetrievalNotConfigured` → `not_configured`, `Health` error → `unavailable`); one inference row per distinct endpoint named by a registered corpus (`Ingestion.EmbedEndpoint`, and `Rerank.Endpoint` when `Rerank.Enabled`), status from `Profiles.InferenceProfile`: nil → `ok` + profile ID, `ErrLLMNotConfigured` → `not_configured`, `ErrLLMProfileMismatch` → `mismatch`, `ErrLLMProviderUnavailable` or any other error → `unavailable`. Corpora: every registered spec, then every state row whose corpus is not registered (`Registered: false`). State: no row → `uninitialized`; `ActiveGeneration == 0 && BuildingGeneration > 0` → `building`; `ProfileChanged` → `profile_changed`; else `active`. A Mongo error fails the call; an engine or inference failure never does (D1).
- `TenantIndexing`: tenant from `tenantrepo.CurrentTenantID`; registered corpora only; `Points` set only when the engine is `ok` and the corpus has an active generation, from `Engine.Count(ctx, CollectionName(c, active), engine.Selector{TenantID: tenant})`; a `Count` error leaves `Points` nil and sets `Engine.Status` to `unavailable`.
- `Diagnose`: builds `iface.RetrievalSearchRequest{Corpus: corpus, Query, Filter, Mode, Limit}` and calls `Runtime.Diagnose`; maps into `RetrievalDiagnosticsReport` (hit i gets `Scores[i]`); emits audit `retrieval.diagnostics.run`, resource type `retrieval_corpus`, resource id = corpus, outcome `success` or `failure`, metadata `{mode, limit, queryRunes, hits, degraded}` — never the query text or the filter values.
- `DropOrphanCorpus` order: (1) corpus registered → `ErrCorpusRegistered`; (2) `Store.CorpusState` not found → `ErrCorpusNotFound`; (3) `Runtime.Engine()` error → returned as is; (4) `DropCollection(CollectionName(corpus, g))` for `g` in `1..max(Active, Building)`, `engine.ErrNotFound` ignored, any other error → `fmt.Errorf("%w", iface.ErrRetrievalUnavailable)` and stop; (5) `Store.DeleteCorpusRows`; (6) `Store.DeleteCorpusState` last. Audit `retrieval.corpus.dropped`, resource `retrieval_corpus`/corpus, metadata `{generations: []int, collections: []string}`, outcome `failure` (with `reason` = the errcode it maps to) when steps 3–6 fail. Steps 1–2 emit nothing (no state changed).

Audit event fields follow `llm/services/catalog.go:110-129` (`TenantID`/`TenantKind` from tenantrepo, actor from `ctxauth.GetUserUUID`/`GetUserEmail`, IP from `ctxauth.GetClientIP`, `ActorType: "user"`).

- [ ] **Step 1: Write the failing tests** (fakes for `AdminRuntime`, `AdminStoreAPI`, `engine.Engine`, `LLMInferenceProfiles`, and a recording `AuditSink`)

```go
func TestCorpora_NotConfiguredStillListsCorpora(t *testing.T)   // Engine() not configured -> engine "not_configured", 1 registered corpus listed, no error
func TestCorpora_OrphansAndStates(t *testing.T)                 // registered "crm" active g1 + state row "old" -> corpora[0]=crm active, corpora[1]=old Registered=false
func TestCorpora_InferenceStatuses(t *testing.T)                // embed ok(profile "ip_x"), rerank mismatch; rerank disabled corpus adds no rerank row
func TestCorpora_EngineDownIsReportedNotFailed(t *testing.T)    // Health error -> engine "unavailable", err == nil
func TestTenantIndexing_PointsOnlyWhenEngineOK(t *testing.T)    // ok -> Points=&n with Selector{TenantID:"tA"}; not configured -> Points nil
func TestDropOrphan_RefusesRegistered(t *testing.T)             // ErrCorpusRegistered; fake engine and store saw no call; no audit event
func TestDropOrphan_UnknownCorpus(t *testing.T)                 // ErrCorpusNotFound
func TestDropOrphan_DropsEveryGeneration(t *testing.T)          // Active 2, Building 3 -> DropCollection g1,g2,g3 (g1 ErrNotFound ignored), rows then state deleted in that order; one audit "retrieval.corpus.dropped" success with collections [p_old_g1,p_old_g2,p_old_g3]
func TestDropOrphan_EngineOutageKeepsState(t *testing.T)        // g2 DropCollection ErrUnavailable -> errors.Is(err, iface.ErrRetrievalUnavailable); DeleteCorpusRows/State not called; audit outcome "failure"
func TestDiagnose_AuditCarriesNoQueryText(t *testing.T)         // audit metadata keys == {mode,limit,queryRunes,hits,degraded}; no value contains the query string
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/services/ -run 'TestCorpora|TestTenantIndexing|TestDropOrphan|TestDiagnose_Audit'`
Expected: FAIL — `NewAdminService` undefined.

- [ ] **Step 3: Implement the four methods**

- [ ] **Step 4: Run to verify pass**

Run: same as Step 2. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services/admin*.go
git commit -m "feat(retrieval): admin service reports, diagnostics audit and orphan corpus drop"
```

---

### Task 7: Search-log read with audit

**Files:**
- Modify: `backend/internal/core/retrieval/services/admin.go`, `services/admin_test.go`

**Interfaces:**
- Consumes: `AdminStoreAPI.SearchLog` (Task 4).
- Produces: `func (a *AdminService) SearchLog(ctx context.Context, q models.SearchLogQuery) (*models.RetrievalSearchLogPage, error)`

Rules: `Limit` 1–200 (0 → 50), `Offset` 0–10000, `Since` after `Until` → `iface.ErrRetrievalInvalidRequest` (wrapped) before any read. Every call that reaches the store emits exactly one audit event `retrieval.searchlog.read`, resource type `retrieval_search_log`, resource id = tenant id, metadata `{userUuid, since, until, limit, offset, returned}` (filter values and count only, never row content); outcome `failure` when the store errors.

- [ ] **Step 1: Write the failing tests**

```go
func TestSearchLog_EmitsOneAuditEventEvenWhenEmpty(t *testing.T) // store returns 0 rows -> page {Items:[], Total:0}; exactly one audit event, metadata["returned"] == 0
func TestSearchLog_Validation(t *testing.T)                      // Since > Until -> ErrRetrievalInvalidRequest, store not called, no audit; Limit 0 -> store got 50; Limit 500 -> invalid
func TestSearchLog_AuditHasNoRowContent(t *testing.T)            // store returns a row with Query "mario rossi"; no audit metadata value contains it
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/services/ -run TestSearchLog`
Expected: FAIL — `SearchLog` undefined.

- [ ] **Step 3: Implement `SearchLog`**

- [ ] **Step 4: Run to verify pass**

Run: same. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services/admin*.go
git commit -m "feat(retrieval): audited search-log read"
```

---

### Task 8: Handlers, routes, module wiring, navigation and OpenAPI

**Files:**
- Create: `backend/internal/core/retrieval/handlers/admin.go`, `handlers/admin_test.go`
- Create: `backend/internal/core/retrieval/routes.go`
- Modify: `backend/internal/core/retrieval/module.go` (`Init` wiring, `RegisterRoutes`, `NavItems`, `stepUpMaxAge`)
- Modify: `backend/internal/core/retrieval/module_test.go` (route guards)
- Regenerate: `backend/openapi/enterprise.json`

**Interfaces:**
- Consumes: `*services.AdminService` (Tasks 6–7) through `handlers.AdminAPI`; `mapError` (Task 2); `module.RouteInfo`, `op.AuthMW.Require*` (pattern `backend/internal/core/llm/module.go:599-659`).
- Produces:

```go
// handlers/admin.go
type AdminAPI interface {
	Corpora(ctx context.Context) (*models.RetrievalCorporaReport, error)
	TenantIndexing(ctx context.Context) (*models.RetrievalTenantIndexingReport, error)
	Diagnose(ctx context.Context, corpus string, in models.RetrievalDiagnosticsRequest) (*models.RetrievalDiagnosticsReport, error)
	DropOrphanCorpus(ctx context.Context, corpus string) error
	SearchLog(ctx context.Context, q models.SearchLogQuery) (*models.RetrievalSearchLogPage, error)
}
func NewAdminHandler(svc AdminAPI, logger *slog.Logger) *AdminHandler
// one method per route; errors go through mapError, and a 5xx cause is logged with slog.ErrorContext (never sent)

// routes.go
func RegisterCorpusReadRoutes(api huma.API, h *handlers.AdminHandler)
func RegisterCorpusAdminRoutes(api huma.API, h *handlers.AdminHandler)
func RegisterTenantReadRoutes(api huma.API, h *handlers.AdminHandler)
func RegisterSearchLogRoutes(api huma.API, h *handlers.AdminHandler)
```

Routes (tags `["Retrieval Admin"]`, `Security` scopes as in `llm/routes.go:13-17`):

| OperationID | Method + path | Input | 2xx body | Gates (in order) |
|---|---|---|---|---|
| `admin-retrieval-corpora-list` | `GET /v1/admin/retrieval/corpora` | — | 200 `RetrievalCorporaReport` | `RequireInternalTenant`, `RequireSystemPermission("retrieval.corpus.read")` |
| `admin-retrieval-corpora-diagnostics` | `POST /v1/admin/retrieval/corpora/{corpus}/diagnostics` | path `corpus` (`pattern:"^[a-z][a-z0-9_]{0,31}$"`), body `RetrievalDiagnosticsRequest` | 200 `RetrievalDiagnosticsReport` | internal, `RequireSystemPermission("retrieval.corpus.admin")`, `RequireMFA()`, `RequireStepUp(stepUpMaxAge)` |
| `admin-retrieval-corpora-drop` | `DELETE /v1/admin/retrieval/corpora/{corpus}` | path `corpus` | 204, no body | same four as diagnostics |
| `admin-retrieval-tenant-indexing-get` | `GET /v1/admin/retrieval/tenant/indexing` | — | 200 `RetrievalTenantIndexingReport` | internal, `RequirePermission("retrieval.tenant.read")` |
| `admin-retrieval-search-log-list` | `GET /v1/admin/retrieval/search-log` | query `userUuid` (format uuid), `since`, `until` (RFC 3339 `time.Time`), `limit` (default 50, 1–200), `offset` (default 0, 0–10000) | 200 `RetrievalSearchLogPage` | internal, `RequirePermission("retrieval.searchlog.admin")`, `RequireMFA()`, `RequireStepUp(stepUpMaxAge)` |

Errors per route: diagnostics 403 `retrieval.forbidden`, 404 `retrieval.corpus_not_found`, 409 `retrieval.locality_violation`, 422 `retrieval.invalid_filter`/`retrieval.invalid_request`, 503 `retrieval.not_configured`/`retrieval.unavailable`/`retrieval.corpus_unavailable`/`retrieval.visibility_check_failed`; drop 404 `retrieval.corpus_not_found`, 409 `retrieval.corpus_registered`, 503 `retrieval.not_configured`/`retrieval.unavailable`; search log 422 `retrieval.invalid_request`; reads 500 `retrieval.internal` only on a Mongo failure.

`Init` builds `repository.NewAdminStore(deps.DB)` and `services.NewAdminService` with `Runtime` = the module's `*services.Service`, `Profiles` = `module.GetTyped[iface.LLMGateway](deps.Services, module.ServiceLLMGateway)` asserted to `iface.LLMInferenceProfiles` (nil when either step fails, WARN once), `Audit` = `func() iface.AuditSink { s, _ := module.GetTyped[iface.AuditSink](deps.Services, module.ServiceAuditSink); return s }` (compliance registers it after this module's Init, so it is resolved per call). `Init` contacts neither Qdrant nor inference (D1).

`NavItems()`: `{Realm: "platform", Tier: "internal", Name: "Retrieval", Icon: "search", Path: "/admin/retrieval", MinRole: "administrator", Active: true}` (`faSearch` is registered in `frontend-admin/src/helpers/initFA.ts`).

- [ ] **Step 1: Write the failing tests**

`module_test.go`: copy the `recordingMW`/`guardsKey`/`mark` harness and `docsless()` from `backend/internal/core/llm/module_test.go:197-237,367-372` and assert:

```go
func TestModule_RouteGuards(t *testing.T) {
	stepUp := "stepup:" + (5 * time.Minute).String()
	cases := []struct{ method, path string; want []string }{
		{http.MethodGet, "/v1/admin/retrieval/corpora", []string{"internal", "sys:retrieval.corpus.read"}},
		{http.MethodPost, "/v1/admin/retrieval/corpora/crm/diagnostics", []string{"internal", "sys:retrieval.corpus.admin", "mfa", stepUp}},
		{http.MethodDelete, "/v1/admin/retrieval/corpora/crm", []string{"internal", "sys:retrieval.corpus.admin", "mfa", stepUp}},
		{http.MethodGet, "/v1/admin/retrieval/tenant/indexing", []string{"internal", "perm:retrieval.tenant.read"}},
		{http.MethodGet, "/v1/admin/retrieval/search-log", []string{"internal", "perm:retrieval.searchlog.admin", "mfa", stepUp}},
	}
	// as in llm: every case's recorded gates == want; chi.Walk count == len(cases);
	// every key of NewModule().Permissions() gates at least one route.
}

func TestModule_NavItem(t *testing.T) // exactly one item, Path "/admin/retrieval", MinRole "administrator", Tier "internal"
```

`handlers/admin_test.go` (fake `AdminAPI`, real Huma via `humatest`):

```go
func TestHandlers_ReportsCarryNoContent(t *testing.T)   // GET corpora and tenant/indexing JSON bodies contain none of the keys "query","snippet","score","text"
func TestHandlers_DropReturns204(t *testing.T)           // DELETE -> 204, empty body; fake saw corpus "crm"
func TestHandlers_DropRegisteredIs409(t *testing.T)      // fake returns services.ErrCorpusRegistered -> 409 code "retrieval.corpus_registered"
func TestHandlers_BadCorpusNameIs422(t *testing.T)       // POST /v1/admin/retrieval/corpora/Bad-Name/diagnostics -> 422, fake not called
func TestHandlers_SearchLogQueryParams(t *testing.T)     // ?userUuid=<uuid>&since=2026-10-01T00:00:00Z&limit=20&offset=40 -> fake got SearchLogQuery{UserUUID, Since, Limit:20, Offset:40}
func TestHandlers_InternalErrorHidesCause(t *testing.T)  // fake returns errors.New("mongo: dial 10.0.0.3") -> 500 "retrieval.internal", body lacks "10.0.0.3"
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/ ./internal/core/retrieval/handlers/`
Expected: FAIL — no routes mounted / undefined handler.

- [ ] **Step 3: Implement handlers, `routes.go`, `RegisterRoutes`, `NavItems`, `Init` wiring**

`RegisterRoutes` uses the llm `group()` helper shape: one `ProtectedRouter.Group` per row of the table, `RequireInternalTenant()` first, then the gates in table order. Permission strings are literals at the call site (policycoverage reads them).

- [ ] **Step 4: Run tests and the static gates**

Run: `cd backend && go test ./internal/core/retrieval/... && cd .. && make backend-policycoverage backend-errquality backend-tenantscope`
Expected: PASS; policycoverage reports no `permission.declared.unused`, `permission.used.undeclared` or `permission.cedar.unreferenced` for `retrieval.*`.

- [ ] **Step 5: Regenerate the OpenAPI document**

Run: `(cd docker && docker compose -f docker-compose.infra.yml --env-file .env up -d mongodb redis) && cd backend && make openapi-dump && git diff --stat openapi/enterprise.json`
Expected: five new operations under tag "Retrieval Admin"; every new schema name starts with `Retrieval`. Then `make openapi-check` → PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/retrieval/ backend/openapi/enterprise.json
git commit -m "feat(retrieval): mount the D15 admin routes on the operator host"
```

---

### Task 9: Console API slice, permissions hook and strings

> Invoke the `orkestra-frontend-admin` skill first and post its pre-flight block (precedent `src/pages/admin/llm/index.tsx`, `src/store/api/llmApi.ts`; reference `src/reference/components/tables/Tables.tsx`).

**Files:**
- Create: `frontend-admin/src/store/api/retrievalApi.ts`, `retrievalApi.test.ts`
- Modify: `frontend-admin/src/store/api/baseApi.ts` (`tagTypes` ~L809-873, `TENANT_SCOPED_TAGS` ~L916)
- Create: `frontend-admin/src/pages/admin/retrieval/retrievalPermissions.ts`
- Create: `frontend-admin/src/pages/admin/retrieval/retrievalErrors.ts`, `retrievalErrors.test.ts`
- Modify: `frontend-admin/src/locales/en.json`, `it.json` (`errors.retrieval.*`, `adminRetrieval.*`)

**Interfaces:**
- Consumes: the five routes and DTOs of Task 8 (TypeScript interfaces mirror `models/admin.go` field by field: `RetrievalCorporaReport`, `RetrievalTenantIndexingReport`, `RetrievalDiagnosticsRequest`, `RetrievalDiagnosticsReport`, `RetrievalSearchLogPage`, `RetrievalSearchLogEntry`).
- Produces:

```ts
// retrievalApi.ts — endpoint names are global (endpointNames.test.ts); all start with a retrieval-specific word
getRetrievalCorpora: build.query<RetrievalCorporaReport, void>            // GET /v1/admin/retrieval/corpora, providesTags ['RetrievalCorpora']
runRetrievalDiagnostics: build.mutation<RetrievalDiagnosticsReport, { corpus: string } & RetrievalDiagnosticsRequest> // POST .../{corpus}/diagnostics, no invalidation
dropRetrievalCorpus: build.mutation<void, { corpus: string }>             // DELETE .../{corpus}, invalidatesTags ['RetrievalCorpora']
getRetrievalTenantIndexing: build.query<RetrievalTenantIndexingReport, void> // providesTags ['RetrievalTenantIndexing']
listRetrievalSearchLog: build.query<RetrievalSearchLogPage, RetrievalSearchLogQuery> // GET .../search-log?…, providesTags ['RetrievalSearchLog'], keepUnusedDataFor: 0
export interface RetrievalSearchLogQuery { userUuid?: string; since?: string; until?: string; limit: number; offset: number }

// retrievalPermissions.ts
export const RETRIEVAL_PERMISSIONS = {
  corpusRead: 'retrieval.corpus.read',
  corpusAdmin: 'retrieval.corpus.admin',
  tenantRead: 'retrieval.tenant.read',
  searchLog: 'retrieval.searchlog.admin'
} as const;
export const useRetrievalPermissions: () => {
  canReadCorpora: boolean; canAdminCorpora: boolean; canReadTenant: boolean; canReadSearchLog: boolean;
};

// retrievalErrors.ts — same contract as pages/admin/llm/llmErrors.ts
export const isReauthCancelled: (err: unknown) => boolean;   // re-export or copy of the llm helper
export const retrievalErrorMessage: (t: (k: string) => string, err: unknown) => string; // errors.retrieval.<situation>, then detail, then generic
```

Tags: add `'RetrievalCorpora'`, `'RetrievalTenantIndexing'`, `'RetrievalSearchLog'` to `tagTypes`; add the last two to `TENANT_SCOPED_TAGS` (the corpus report is installation-wide). `keepUnusedDataFor: 0` on the search log: every view is a fresh, audited read.

- [ ] **Step 1: Write the failing tests**

```ts
// retrievalApi.test.ts
it('tenant-scoped retrieval caches are dropped on an org switch, the corpus report is not', ...)
// getTenantScopedTags() contains 'RetrievalTenantIndexing' and 'RetrievalSearchLog', not 'RetrievalCorpora'
it('listRetrievalSearchLog sends only the filters that are set', ...)
// MSW captures the URL: {limit:50, offset:0} -> '?limit=50&offset=0'; {userUuid:'u-1', since:'2026-10-01T00:00:00Z', limit:20, offset:40}
// -> userUuid=u-1&since=2026-10-01T00%3A00%3A00Z&limit=20&offset=40 (no until)
it('dropRetrievalCorpus deletes the corpus route and refetches the report', ...)

// retrievalErrors.test.ts
it('maps a retrieval code to its translation', ...)  // {data:{code:'retrieval.corpus_registered'}} -> t('errors.retrieval.corpus_registered')
it('falls back to the backend detail, then to the generic line', ...)
```

Locale parity is enforced by `src/locales/parity.test.ts`.

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend-admin && npx vitest run src/store/api/retrievalApi.test.ts src/pages/admin/retrieval/retrievalErrors.test.ts src/store/api/endpointNames.test.ts src/locales/parity.test.ts`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement the slice, tags, hook, error helper and strings**

`errors.retrieval` gets one key per code of Task 2 (`not_configured`, `unavailable`, `corpus_not_found`, `corpus_registered`, `corpus_unavailable`, `forbidden`, `invalid_filter`, `invalid_request`, `locality_violation`, `visibility_check_failed`, `internal`) in en and it.

- [ ] **Step 4: Run to verify pass**

Run: same as Step 2. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend-admin/src/store/api/ frontend-admin/src/pages/admin/retrieval/ frontend-admin/src/locales/
git commit -m "feat(frontend-admin): retrieval API slice, permissions and strings"
```

---

### Task 10: `/admin/retrieval` page — Corpora, Indexing, Search log

> Invoke the `orkestra-frontend-admin` skill first; pre-flight must list `src/pages/admin/llm/index.tsx`, `src/pages/admin/llm/ConfirmDeleteModal.tsx`, `src/pages/admin/compliance/AuditEventsTab.tsx`, `src/reference/components/tables/Tables.tsx`, `src/reference/components/ui/Modals.tsx`, `src/reference/components/forms/FormValidation.tsx`, `src/reference/components/ui/StatCards.tsx`, all opened this session.

**Files:**
- Create: `frontend-admin/src/pages/admin/retrieval/index.tsx`, `index.test.tsx`
- Create: `frontend-admin/src/pages/admin/retrieval/CorporaTab.tsx`, `CorporaTab.test.tsx`
- Create: `frontend-admin/src/pages/admin/retrieval/DiagnosticsModal.tsx`
- Create: `frontend-admin/src/pages/admin/retrieval/IndexingTab.tsx`
- Create: `frontend-admin/src/pages/admin/retrieval/SearchLogTab.tsx`, `SearchLogTab.test.tsx`
- Modify: `frontend-admin/src/routes/coreRoutes.tsx` (lazy import next to `LlmAdminPage` ~L39; route object after `llm` ~L208)
- Modify: `frontend-admin/src/locales/en.json`, `it.json` (`adminRetrieval.*` page strings)

**Interfaces:**
- Consumes: Task 9's hooks (`useGetRetrievalCorporaQuery`, `useRunRetrievalDiagnosticsMutation`, `useDropRetrievalCorpusMutation`, `useGetRetrievalTenantIndexingQuery`, `useListRetrievalSearchLogQuery`), `useRetrievalPermissions`, `retrievalErrorMessage`, `isReauthCancelled`; `ConfirmDeleteModal` from `pages/admin/llm/ConfirmDeleteModal` (reuse, do not copy).
- Produces: default export `RetrievalAdminPage`; route `admin/retrieval` with `ProtectedRoute requiredPermissions={[['retrieval.corpus.read', 'retrieval.tenant.read', 'retrieval.searchlog.admin', 'super_admin', 'administrator', 'developer']]}`.

Behaviour:
- Tabs `corpora` (needs `canReadCorpora`), `indexing` (`canReadTenant`), `search-log` (`canReadSearchLog`); `?tab=` synced with `useSearchParams` exactly as `llm/index.tsx`; unknown or forbidden value → first visible tab; no visible tab → `Alert` `adminRetrieval.noAccess`. Each pane `mountOnEnter unmountOnExit`.
- Corpora: status row of `StatCard`s (engine, inference rows, erasures pending/fencing); `AdvanceTable` of corpora (name, owner + running badge, state badge, generation, outbox pending + oldest, tenants done/building). `engine.status === 'not_configured'` shows an info `Alert` `adminRetrieval.corpora.notConfigured` above the table. With `canAdminCorpora`: a "Diagnose" action on registered rows opens `DiagnosticsModal`; a "Drop" action **only on rows with `registered === false`** opens `ConfirmDeleteModal` naming the corpus and its generations.
- `DiagnosticsModal`: react-hook-form + yup (`query` required, 1–2000 chars; `mode` select hybrid/dense/keyword; `limit` 1–100, default 10). Result: principals as badges, `degraded` warning, table rank / sourceId / chunkId / score / fused / dense / keyword / rerank / snippet; a dismissed step-up (`isReauthCancelled`) shows nothing.
- Indexing: table of the tenant's corpora (corpus, owner running, initial build badge, points or "—", outbox pending + oldest) and the erasures-pending count.
- Search log: filters user UUID (text), since/until (`CustomDateInput`), applied on submit; table createdAt / user / corpus / query / returned count / subjects count / duration / degraded; row expansion shows principals, returned ids, subjects, filter JSON; pagination by `offset` with page size 50 (`AdvanceTableFooter` driven by `total`). An info line `adminRetrieval.searchLog.audited` states that each view is recorded in the audit trail.

- [ ] **Step 1: Write the failing tests** (mock `useAuth` like `llm/index.test.tsx:19-23`; MSW handlers per route)

```tsx
// index.test.tsx
it('shows only the tabs the caller may open', ...)
// perms [tenant.read] -> tabs: Indexing only; [corpus.read, searchlog.admin] -> Corpora + Search log
it('falls back to the first visible tab for a forbidden ?tab=', ...)   // ?tab=search-log without searchlog.admin -> corpora active
it('shows the no-access alert without any retrieval permission', ...)

// CorporaTab.test.tsx
it('offers Drop only on orphan rows and only with corpus.admin', ...)
// report with crm (registered) + old (orphan): with corpusAdmin -> one Drop button, in the "old" row; without -> none, and no Diagnose buttons
it('drops an orphan after confirmation and refetches', ...)          // DELETE /v1/admin/retrieval/corpora/old called once; GET corpora called twice
it('shows the not-configured notice and still lists corpora', ...)
it('renders diagnostic arm scores', ...)                               // POST returns hits with dense 0.8, keyword null -> row shows 0.8 and "—"

// SearchLogTab.test.tsx
it('applies the user and date filters to the request', ...)           // URL has userUuid and since; no until
it('pages by offset', ...)                                            // total 120 -> next page requests offset=50
```

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend-admin && npx vitest run src/pages/admin/retrieval`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement the page, tabs, modal, route and strings**

- [ ] **Step 4: Run the frontend gate**

Run: `make ci-frontend-admin`
Expected: PASS (typecheck, eslint, tests incl. parity and endpoint names, audit, build).

- [ ] **Step 5: Check it in the running console**

With the dev stack up and `ENV=development`: open `/admin/retrieval` as an administrator through `integrated-browser-mcp` (`browser_navigate`, `browser_snapshot`); the sidebar shows "Retrieval"; Corpora shows the engine status (not configured without the PR 8 overlay); Search log asks for step-up before loading.

- [ ] **Step 6: Commit**

```bash
git add frontend-admin/src/pages/admin/retrieval/ frontend-admin/src/routes/coreRoutes.tsx frontend-admin/src/locales/
git commit -m "feat(frontend-admin): /admin/retrieval page with corpora, indexing and search log"
```

---

### Task 11: Docs and the full gate

**Files:**
- Modify: `backend/internal/core/retrieval/AGENTS.md` (created by PR 3)
- Modify: `docs/site/modules/core/retrieval.mdx` (created by PR 3)

- [ ] **Step 1: Update `retrieval/AGENTS.md`**

Add sections: **Routes** (the Task 8 table: path, permission, MFA/step-up, error codes); **RBAC** (the four keys with scope; the seeding matrix of Task 1 as a table, production and non-production; `retrieval.tenant.admin` arrives in step 2; the test names that pin matrix, Cedar and route gates); **Admin invariants** (no route returns content or scores except diagnostics; diagnostics run the normal pipeline with no bypass and write a search-log row; every search-log read is audited; orphan drop order and why the state row goes last; drop only after every replica runs a binary without the corpus registration — the registered check is per process; audit sink resolved per call from `module.ServiceAuditSink`).

- [ ] **Step 2: Update `docs/site/modules/core/retrieval.mdx`**

Add "Administration" (console page and tabs, who sees what — the seeding matrix in prose plus table, the step-up prompts, that search-log views are audited, the orphan drop) and "HTTP API" (the five routes with permission and error codes; link to the generated API reference).

- [ ] **Step 3: Run the full gates**

Run: `MONGO_TEST_URI=<replica-set uri> QDRANT_TEST_URL=<url> QDRANT_TEST_API_KEY=<key> make ci-backend && make ci-frontend-admin && make agents-check`
Expected: `Backend CI: OK`, `Frontend-admin CI: OK`, agents check OK.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/core/retrieval/AGENTS.md docs/site/modules/core/retrieval.mdx
git commit -m "docs(retrieval): admin routes, RBAC matrix and console page"
```
