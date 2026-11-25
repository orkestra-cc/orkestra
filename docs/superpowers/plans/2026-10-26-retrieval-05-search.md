# Retrieval PR 5 — Search pipeline, principals and search log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** answer `iface.RetrievalSearch.Search` with the ADR's 8-step hybrid pipeline: tenant + principal filtering, erasure veto, source visibility check, optional rerank, degraded mode, and a search-log row per search.

**Architecture:** `authz` gains the narrow `iface.AuthzRoleLister`. In `retrieval`:
- `filter.Compile` turns a validated `iface.RetrievalFilter` into an `engine.Filter`.
- `services.PrincipalResolver` computes the caller's principals.
- `services.Searcher` runs the pipeline over narrow interfaces (corpus lookup, corpus state, engine, tombstones, authz, llm companions, owner-module gate, search-log writer). The module's `*services.Service` delegates `Search`, `FilterSchema`, `Principals` and a stubbed `DeleteBySubject` to it.

Only the wiring task (Task 9) touches PR 3/PR 4 internals, so everything else is tested with in-package fakes.

**Tech Stack:** Go 1.26.10, mongo-driver v1.17.7 (replica set), Qdrant ≥ 1.20 through PR 3's `engine.Engine`, `iface.LLMInferenceProfiles` / `LLMPinnedEmbedder` / `LLMReranker` from PR 1.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D5, D7 filter part, D10, D12 search-audit part, D13) and `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (shared contract; row 5). Executors read both. PRs 1, 3 and 4 are merged before this one starts; this plan relies only on what the roadmap names for them, plus the accessors listed under "Consumed from PR 3/PR 4" below.

## Global Constraints

Everything in the roadmap's **Global constraints** section applies. In addition, for this PR:

- No HTTP route and no `errcode` constant: ADR D15 gives search no generic route. PR 7 owns codes. This PR returns the roadmap's `iface.ErrRetrieval*` sentinels only.
- `slog` never receives query text, snippets, chunk text, principals or subjects. Those go only into `retrieval_search_log`. Log attributes are `slog.String`/`slog.Int` (logscope).
- Pinned values. Each constant lives in the task that defines it:

| Value | Setting |
|---|---|
| Limit | default 10, range 1–100 |
| Query | 1–2000 runes, not blank |
| Fusion defaults | PR 3's `services.DefaultFusion` (`DenseWeight 1`, `KeywordWeight 1`, `Candidates 50`), applied at registration |
| Rerank K | PR 3's `services.DefaultRerankTopK` (20), cap 200 |
| Snippet | 300 runes |
| `Visible` timeout | 2 s |
| Principal cap | config key `principal_cap`: default 256, range 16–4096, env `RETRIEVAL_PRINCIPAL_CAP` |
| Search-log retention | config key `search_log_retention_days`: default 90, range 1–3650, env `RETRIEVAL_SEARCH_LOG_RETENTION_DAYS` |

- Platform roles get no bypass: `super_admin` resolves principals exactly like anyone else (ADR D10).
- Commit trailers on every commit: `Prop: upstream` and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Use Conventional Commits.

### Decisions this plan takes (argued from the ADR)

1. **Rerank failure keeps the fused order and sets `degraded: true`.** ADR D5 sets `degraded` when "inference is unavailable". A reranker is an inference endpoint. A corpus enables rerank only after its benchmark shows a gain (D14), so a page without rerank is below the corpus's contract, and a consumer running a corrective loop needs to know that. Failing the search instead would turn an optional quality stage into an outage.
2. **A search-log write failure fails the search closed:** no hits are returned and the error wraps `iface.ErrRetrievalUnavailable`. D12 says "the operator must be able to answer who looked up whom". A result served without its row is a lookup nobody can answer for. The row is written before the result leaves the module. Searches that already failed after the gate are logged best-effort, because they return no data.
3. **Never fewer than the permitted results without a flag (D5).**
   - The first engine call fetches `min(PerArm, max(4×Limit, RerankK))` candidates.
   - If step 6 drops enough to leave fewer than `Limit` survivors, and the engine returned a full page, the pipeline refills **once** with `Limit = PerArm`. Only the new candidates are checked.
   - If the page is still short and that last call returned a full page, `degraded: true` marks a possibly incomplete result.
   - The roadmap has no separate flag (see Contract issues).
4. **A query-time locality violation drops that stage and does not fail the search.**
   - An embed endpoint whose boundary breaks `local_only` means keyword-only with `degraded: true`.
   - A rerank endpoint that breaks it means no rerank, with `degraded: true`.
   - The text never reaches the endpoint either way. Keyword search needs no inference, so failing it would be an outage with no privacy gain.
   - `ErrRetrievalLocality` stays the registration-time and build-time error (PRs 3 and 4).
5. **Membership source for `tenant:*`:** `iface.TenantProvider.IsMember`, registered under `module.ServiceTenantProvider`.
   - It is a point read of the membership row, and the tenant module has no inactive-membership state: removal deletes the row (`internal/core/tenant/services/service.go:344-353`).
   - `TenantDirectoryReader.ListTenantMembers` would scan the whole tenant on every search.
6. **A principal-resolution error fails the search.** This covers membership, role lister, a contributor, an invalid contributor principal, or the cap being exceeded. Dropping a principal instead would silently return fewer results without a flag.
7. **Every result score is on one scale.** When rerank runs, `RerankK = max(TopK, Limit)`, so every returned hit was reranked and carries the reranker's score. Without rerank, `Score` is the engine's fused score.

### Consumed from PR 3 / PR 4 (roadmap names marked †, others are the smallest accessor this plan assumes; see Contract issues)

- † `engine.Engine`, `engine.Query`, `engine.Filter`, `engine.Range`, `engine.Candidate`, `engine.Runtime`, `engine.ErrUnavailable`, `engine.ErrNotFound`, `engine.CollectionSpec`, `engine.Point`, `engine.PointID` (`retrieval/engine/engine.go`).
- † `services.Probe{SourceID string; Subjects []string}` and `(*services.Tombstones).Covered(ctx, tenantID, corpus string, probes []Probe) (map[string]bool, error)` (`retrieval/services/tombstones.go`).
- † `iface.RetrievalCorpusSpec`, `RetrievalSource`, `RetrievalPrincipalContributor`, `RetrievalSearchRequest/Result/Hit/Provenance`, `RetrievalFilter`, `RetrievalFusion`, `RetrievalRerank`, `ErrRetrieval*` (`pkg/sdk/iface/retrieval.go`), and † `iface.LLMInferenceProfiles`, `LLMPinnedEmbedder`, `LLMReranker`, `InferenceProfile`, `ErrLLMProfileMismatch` (`pkg/sdk/iface/llm_installation.go`).
- PR 3 filter validation: this plan calls it `filter.Validate(f *iface.RetrievalFilter, schema []iface.RetrievalFilterField) error`. It enforces depth ≤ 4, ≤ 64 nodes, ≤ 100 values, declared fields only, and op/type compatibility, and returns errors wrapping `iface.ErrRetrievalInvalidFilter`.
- PR 3 registry reads: a corpus spec by name, and the registered principal contributors.
- PR 4 corpus-state read: the active generation of a corpus, its collection name, and the embedding `InferenceProfile.ID` it was built with.

Task 9 is the only place these unnamed accessors are used.

## Review Focus

1. **A record made private between indexing and query** (roadmap RF 2).
   - Expected: `Visible` refuses it, so its text is neither reranked nor returned.
   - Expected: a `Visible` error or timeout fails the whole search with `ErrRetrievalVisibilityCheck`.
   - Tests: Task 7 `TestSearch_VisibleRefusedDropped`, `TestSearch_VisibleTimeoutFailsClosed`; Task 8 `TestSearch_RerankNeverSeesRefusedText`; Task 10 `TestIntegrationSearch_PrivateRecordDropped`.
2. **A user erased while their points still sit in the engine** (roadmap RF 3).
   - Expected: the tombstone veto drops them before the owner's `Visible` is even asked, even when `Visible` would say yes, and the search log does not list them.
   - Tests: Task 7 `TestSearch_TombstoneVetoDropsBeforeVisible`; Task 10 `TestIntegrationSearch_ErasedUserNeverReturned`.
3. **The embed endpoint now serves another model** (roadmap RF 5).
   - Expected: the profile differs from the pinned generation profile, so no query is embedded, the keyword arm alone answers, and `degraded: true`.
   - Tests: Task 6 `TestSearch_ProfileMismatchFallsBackToKeyword`; Task 10 `TestIntegrationSearch_ProfileMismatchKeywordOnly`.
4. **A heavily restricted caller whose first candidate page is mostly refused.**
   - Expected: one refill fills the page. If it still cannot, the result says so.
   - Tests: Task 7 `TestSearch_RefillsOnceWhenDropsShortenThePage`, `TestSearch_ShortPageAfterRefillIsFlagged`.
5. **A principal contributor that errors, emits `tenant:*`/`user:`/`role:`, or pushes the caller over the cap.**
   - Expected: the search fails closed before the engine is queried.
   - Tests: Task 3 `TestPrincipals_ContributorReservedPrefixRefused`, `TestPrincipals_CapOverflowFailsClosed`; Task 6 `TestSearch_PrincipalResolutionErrorFailsClosed`.

---

### Task 1: `iface.AuthzRoleLister` on the authz service

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go`. Insert after `AuthzCacheInvalidator` (ends at L982), with a header comment in the same style: consumed by retrieval; resolve with `module.GetTyped` against `ServiceAuthzProvider`.
- Create: `backend/internal/core/authz/services/role_lister.go`
- Create: `backend/internal/core/authz/services/role_lister_test.go`
- Modify: `backend/internal/core/authz/services/repo_fake_test.go`:
  - L21-28: add error-injection fields.
  - L151-160: `GetRoleByUUID` returns `repository.ErrNotFound` wrapped.
  - L297-316: `ListActiveBindingsForUser` honours the injected error.
- Modify: `backend/internal/core/authz/AGENTS.md`, under "Service contract" after the `AuthzCacheInvalidator` paragraph (and its Consumers list).
- Modify: `backend/pkg/sdk/AGENTS.md`, L49 list of narrow sub-interfaces.
- Modify: `docs/site/sdk/shared-iface.mdx`. Add a table row after `AuthzCacheInvalidator`: `AuthzRoleLister | authz | retrieval (role principals)`.
- Modify: `docs/site/modules/core/authz.mdx`, one paragraph after "How a decision is made".

**Interfaces:**
- Produces:

```go
// pkg/sdk/iface/interfaces.go — verbatim from the roadmap
type AuthzRoleLister interface {
	// ActiveRoleIDs lists the UUIDs of active roles bound to the user in the
	// tenant through non-expired bindings. tenantID "" is refused.
	ActiveRoleIDs(ctx context.Context, userUUID, tenantID string) ([]string, error)
}

// internal/core/authz/services/role_lister.go
var ErrRoleListTenantRequired = errors.New("authz: listing a user's roles requires a tenant")
func (s *Service) ActiveRoleIDs(ctx context.Context, userUUID, tenantID string) ([]string, error)
```

- [ ] **Step 1: Extend the fake repo.** Add the fields `listBindingsErr error` and `getRoleErr error` to `searchRepo`. `ListActiveBindingsForUser` returns `listBindingsErr` when it is set. `GetRoleByUUID` returns `getRoleErr` when it is set. A missing uuid returns `fmt.Errorf("searchRepo: role %q: %w", uuid, repository.ErrNotFound)`; no existing test matches on the old message (`grep -rn "role not found by uuid" internal/` finds only the definition).

- [ ] **Step 2: Write the failing tests** in `role_lister_test.go`. Use `newTier1Service`, `seedRole` and `seedBinding` from `tier1_test.go` / `repo_fake_test.go`. Set expiry by editing `repo.bindings[id]`.

```go
func TestService_ImplementsAuthzRoleLister(t *testing.T) { var _ iface.AuthzRoleLister = (*Service)(nil) }

func TestAuthzRoleLister_ResolvesFromServiceAuthzProvider(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("operator"))
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(svc))
	_, ok := module.GetTyped[iface.AuthzRoleLister](reg, module.ServiceAuthzProvider)
	// want ok == true
}

func TestActiveRoleIDs_ListsActiveRolesOfLiveBindingsInTheTenant(t *testing.T) {
	// seedRole("r-sys", "org_member", true, nil, "")       bound in tenant-X  -> listed (system org role, tenantId "")
	// seedRole("r-a",   "sales",      false, nil, "tenant-X") bound in tenant-X -> listed
	// seedRole("r-off", "old",        false, nil, "tenant-X") bound, then IsActive=false -> not listed
	// seedRole("r-exp", "trial",      false, nil, "tenant-X") bound with ExpiresAt = now-1h -> not listed
	// seedRole("r-y",   "ops",        false, nil, "tenant-Y") bound in tenant-Y -> not listed for tenant-X
	// seedRole("r-g",   "auditor",    false, nil, "")         bound globally (tenant "") -> not listed
	got, err := svc.ActiveRoleIDs(ctx, "u-1", "tenant-X")
	// want err == nil, got == []string{"r-a", "r-sys"} (sorted, deduplicated)
}

func TestActiveRoleIDs_RefusesEmptyTenant(t *testing.T)            // errors.Is(err, ErrRoleListTenantRequired); repo never called
func TestActiveRoleIDs_RefusesEmptyUser(t *testing.T)              // err != nil
func TestActiveRoleIDs_NoBindingsIsEmptyNotNil(t *testing.T)       // got != nil && len(got) == 0
func TestActiveRoleIDs_DanglingBindingIsSkipped(t *testing.T)      // binding to "r-gone" (no role): got == []string{}, err == nil
func TestActiveRoleIDs_CustomRoleOfAnotherTenantIsSkipped(t *testing.T) // role r-y (tenantId tenant-Y) bound in tenant-X: not listed
func TestActiveRoleIDs_RepositoryErrorsFailClosed(t *testing.T)    // listBindingsErr -> err; getRoleErr (not ErrNotFound) -> err, got == nil
```

- [ ] **Step 3: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/authz/services/ -run 'ActiveRoleIDs|AuthzRoleLister' -v`
  - Expected: FAIL at build time, "svc.ActiveRoleIDs undefined" and "undefined: iface.AuthzRoleLister".

- [ ] **Step 4: Implement.** Add the interface to `iface`, and `ActiveRoleIDs` in `role_lister.go`:
  - `tenantID == ""` returns `ErrRoleListTenantRequired`. `userUUID == ""` returns `errors.New("authz: userUUID required")`.
  - Read `s.repo.ListActiveBindingsForUser(ctx, userUUID, tenantID)`. Then, per binding, call `s.repo.GetRoleByUUID`:
    - `errors.Is(err, repository.ErrNotFound)`: skip the binding.
    - Any other error: return it. Unlike `GetEffectivePermissions` L763-766, this does not skip on error, because a silently missing role principal hides records without a flag.
    - Also skip a role that is `!IsActive`, or whose `TenantID` is neither `""` nor `tenantID`.
  - Return the IDs deduplicated and sorted, as an empty non-nil slice when there are none.
  - No cache: one bindings query plus one role read per binding.

- [ ] **Step 5: Run the tests and check they pass.**
  - Run: `cd backend && go test ./internal/core/authz/... -v -run 'ActiveRoleIDs|AuthzRoleLister'`, then `cd backend && go test ./internal/core/authz/... ./pkg/sdk/iface/...`
  - Expected: PASS.

- [ ] **Step 6: Update the docs.**
  - authz `AGENTS.md`: one paragraph with the interface, the resolution, and the rules (non-expired, active, own-tenant or system role, `""` refused, a role read error fails rather than skips). Add "the retrieval module, to compute `role:<id>` principals (ADR-0023 D10)" to Consumers.
  - SDK `AGENTS.md` L49: add ``AuthzRoleLister` (`ActiveRoleIDs` — active role UUIDs of a user's non-expired bindings in one tenant; consumed by retrieval, resolved via `module.GetTyped` against `ServiceAuthzProvider`)``.
  - Add the docs-site row and paragraph listed under Files.

- [ ] **Step 7: Commit.**

```bash
git add backend/pkg/sdk/iface/interfaces.go backend/internal/core/authz backend/pkg/sdk/AGENTS.md docs/site/sdk/shared-iface.mdx docs/site/modules/core/authz.mdx
git commit -m "feat(authz): add iface.AuthzRoleLister for tenant role principals" -m "Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 2: Filter compilation

**Files:**
- Create: `backend/internal/core/retrieval/filter/compile.go`
- Create: `backend/internal/core/retrieval/filter/compile_test.go`

**Interfaces:**
- Consumes: PR 3 `filter.Validate` (see Consumed); `engine.Filter`, `engine.Range`.
- Produces:

```go
// Compile validates f against schema (PR 3's Validate first) and returns the
// engine form. nil f returns (nil, nil). Every error wraps
// iface.ErrRetrievalInvalidFilter. engine.Filter.Field holds the bare declared
// name; the driver adds the "f." namespace, so a compiled filter can never
// address "_rt".
func Compile(f *iface.RetrievalFilter, schema []iface.RetrievalFilterField) (*engine.Filter, error)
```

Mapping. A node is a group *or* a leaf. Numbers arrive as `float64` (JSON), `json.Number`, `int`, `int32` or `int64`. Datetimes arrive as RFC 3339 strings and compile to Unix **milliseconds** UTC as `float64` (`float64(t.UnixMilli())`), the representation PR 3 fixes for `engine.Range` and PR 4 writes into `Point.Fields` (`int64` ms). The op/type table is PR 3's `filter.Validate` table, which `Compile` calls first: `float` and `datetime` accept only `lt`, `lte`, `gt`, `gte`, `exists`; `bool` only `eq`, `exists`.

**Groups:**

| Input | Compiles to |
|---|---|
| `and` | `Must` |
| `or` | `Should` |
| `not` | `MustNot: [child]` |

**`eq` and `in` (`in` reads `Values`):**

| Field type | `eq` | `in` |
|---|---|---|
| `keyword` | `Match: [string]` | `Match` |
| `keyword[]` | `Match: [string]` (array contains) | `Match` |
| `bool` | `Match: [bool]` | `Match` |
| `integer` | `Match: [int64]`; a non-integral or out-of-int64 number is refused | `Match` |

**Comparisons:** `lt`, `lte`, `gt` and `gte` compile to `Range` on `integer`, `float` and `datetime` only.

**`exists`:**

| Input | Compiles to |
|---|---|
| `Value` nil or `true` | `{Field, Exists: true}` |
| `Value` `false` | `{MustNot: [{Field, Exists: true}]}` |
| any other `Value` | refused |

NaN and ±Inf are refused.

- [ ] **Step 1: Write the failing tests.** Use schema `team keyword`, `tags keyword[]`, `seniority integer`, `score float`, `active bool`, `since datetime`.

```go
func TestCompile_NilFilterIsNil(t *testing.T) // got == nil, err == nil

func TestCompile_Comparisons(t *testing.T) { // table: input node -> want *engine.Filter (reflect.DeepEqual)
	// {team eq "sales"}            -> {Field:"team", Match:[]any{"sales"}}
	// {team in ["a","b"]}          -> {Field:"team", Match:[]any{"a","b"}}
	// {tags eq "vip"}              -> {Field:"tags", Match:[]any{"vip"}}
	// {seniority eq 3.0}           -> {Field:"seniority", Match:[]any{int64(3)}}
	// {seniority gte 2}            -> {Field:"seniority", Range:&engine.Range{Gte: ptr(2)}}
	// {score lt 1.5}               -> {Field:"score", Range:&engine.Range{Lt: ptr(1.5)}}
	// {active eq true}             -> {Field:"active", Match:[]any{true}}
	// {since gt "2026-01-02T03:04:05+02:00"} -> {Field:"since", Range:{Gt: ptr(1767315845000)}}   // 2026-01-02T01:04:05Z in ms
	// {since lte "2026-01-02T01:04:05.5Z"}   -> Range{Lte: ptr(1767315845500)}
	// {team exists}                -> {Field:"team", Exists:true}
	// {team exists false}          -> {MustNot: []engine.Filter{{Field:"team", Exists:true}}}
}

func TestCompile_Groups(t *testing.T) // and/or/not nest: {and:[{or:[a,b]},{not:c}]} -> {Must:[{Should:[A,B]},{MustNot:[C]}]}

func TestCompile_Rejects(t *testing.T) { // every case: errors.Is(err, iface.ErrRetrievalInvalidFilter), got == nil
	// undeclared field "salary"; field "_rt.tenantId"; {team lt "x"}; {active gt true};
	// {team eq 3}; {seniority eq 3.5}; {seniority eq 1e19}; {score lt math.NaN()}; {score eq 2.5}; {since eq "2026-01-02T01:04:05Z"};
	// {team in []}; {team op in with Value instead of Values}; {since gt "yesterday"};
	// node with both And and Field; depth 5; 65 nodes; 101 Values; {team exists "yes"}
}

func TestCompile_FieldNamesAreBare(t *testing.T) // walk the compiled tree: no Field contains "." 
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/filter/ -run TestCompile -v`
  - Expected: FAIL, "undefined: Compile".

- [ ] **Step 3: Implement `Compile`** in `compile.go`:
  - Call `Validate(f, schema)` first.
  - Then compile recursively with a field→type map.
  - Value conversion lives in one helper per type, which returns `fmt.Errorf("%w: field %q: …", iface.ErrRetrievalInvalidFilter, name)`.

- [ ] **Step 4: Run the tests and check they pass.**
  - Run: `cd backend && go test ./internal/core/retrieval/filter/ -v`
  - Expected: PASS, and the PR 3 validation tests stay green.

- [ ] **Step 5: Commit.** Message: `feat(retrieval): compile search filters to the engine form`, with the same trailers as Task 1.

### Task 3: Principal resolver

**Files:**
- Create: `backend/internal/core/retrieval/services/principals.go`
- Create: `backend/internal/core/retrieval/services/principals_test.go`

**Interfaces:**
- Consumes: `iface.AuthzRoleLister` (Task 1), `iface.RetrievalPrincipalContributor`, `iface.RetrievalPrincipalContext`, `ctxauth.GetUserUUID` and `ctxauth.GetTenantID`.
- Produces:

```go
type MembershipChecker interface { // satisfied by iface.TenantProvider
	IsMember(ctx context.Context, userUUID, tenantUUID string) (bool, error)
}

const DefaultPrincipalCap = 256

var (
	ErrPrincipalCapExceeded = errors.New("retrieval: the caller's principals exceed the cap")
	ErrContributorPrincipal = errors.New("retrieval: a principal contributor returned an invalid principal")
)

type PrincipalResolver struct {
	Members      MembershipChecker
	Roles        iface.AuthzRoleLister // nil: every call fails closed
	Contributors func() []iface.RetrievalPrincipalContributor // nil: none
	Cap          func() int // nil or <= 0: DefaultPrincipalCap
}

// Principals implements iface.RetrievalPrincipalResolver for the user and
// tenant in ctx. Union of user:<uuid>; tenant:* when IsMember; role:<id> per
// ActiveRoleIDs; every contributor's output. Deduplicated, sorted ascending.
func (r *PrincipalResolver) Principals(ctx context.Context) (iface.RetrievalPrincipalContext, error)
```

Contributor output rules:
- Each string must match `^[a-z][a-z0-9_]{0,31}:\S{1,200}$`.
- Its prefix (before the first `:`) must not be `user`, `tenant` or `role`; those three belong to the resolver.
- Anything else is `ErrContributorPrincipal`, wrapped with the contributor's `Name()`.

The cap applies after deduplication: `len > cap` returns `ErrPrincipalCapExceeded`. Missing user or tenant in ctx returns `iface.ErrRetrievalForbidden`. Every error returns a zero `RetrievalPrincipalContext`.

- [ ] **Step 1: Write the failing tests.** Fakes are declared in the test file: `searchMembers{member bool; err error}`, `searchRoles{ids []string; err error}`, `searchContributor{name string; out []string; err error}`. Build the ctx with `testkit.NewIdentity("u-1", "u1@example.test", "operator").WithTenant("t-1", []string{"org_member"}, true).ContextFor(context.Background(), "t-1")`.

```go
func TestPrincipals_MemberWithRolesAndContributor(t *testing.T) // roles {"r-2","r-1"}, contributor {"team:t-9"} -> Principals == []string{"role:r-1","role:r-2","team:t-9","tenant:*","user:u-1"}, TenantID "t-1", UserUUID "u-1"
func TestPrincipals_NonMemberGetsNoTenantWildcard(t *testing.T) // super_admin identity, member=false, no roles -> []string{"user:u-1"}
func TestPrincipals_MembershipErrorFailsClosed(t *testing.T)    // err != nil, Principals == nil
func TestPrincipals_RoleListerErrorFailsClosed(t *testing.T)    // err != nil
func TestPrincipals_MissingRoleListerFailsClosed(t *testing.T)  // Roles == nil -> err != nil
func TestPrincipals_ContributorErrorFailsClosed(t *testing.T)   // err != nil and message names the contributor
func TestPrincipals_ContributorReservedPrefixRefused(t *testing.T) // each of "tenant:*","user:u-2","role:r-9","","team","Team:x","team:a b" -> errors.Is(err, ErrContributorPrincipal)
func TestPrincipals_CapOverflowFailsClosed(t *testing.T)        // Cap()=4, member, no roles, contributor {"team:a","team:b","team:c"} -> 5 principals -> errors.Is(err, ErrPrincipalCapExceeded); with Cap()=5 -> ok
func TestPrincipals_DuplicatesCountOnce(t *testing.T)           // two contributors both return "team:a", Cap()=3, member -> ok, len == 3
func TestPrincipals_MissingIdentityIsForbidden(t *testing.T)    // context.Background() -> errors.Is(err, iface.ErrRetrievalForbidden)
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -run TestPrincipals -v`
  - Expected: FAIL, "undefined: PrincipalResolver".

- [ ] **Step 3: Implement `PrincipalResolver.Principals`** as specified above. Errors wrap their cause as `fmt.Errorf("retrieval: principals: …: %w", err)`.

- [ ] **Step 4: Run the tests and check they pass.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -run TestPrincipals -v`
  - Expected: PASS.

- [ ] **Step 5: Commit.** Message: `feat(retrieval): resolve caller principals with a fail-closed cap`, with the trailers.

### Task 4: Search-log model, repository and retention purge

**Files:**
- Create: `backend/internal/core/retrieval/models/search_log.go`
- Create: `backend/internal/core/retrieval/repository/search_log.go`
- Create: `backend/internal/core/retrieval/repository/search_log_test.go` (Mongo). Use the package's `newTestDB` if PR 3/4 added one. Otherwise create `repository/testdb_test.go` copying `internal/core/llm/repository/testdb_test.go` with db prefix `retrieval_test_`.
- Create: `backend/internal/core/retrieval/services/search_log_retention.go`
- Create: `backend/internal/core/retrieval/services/search_log_retention_test.go`
- Modify: `backend/tools/piiscan/baseline.txt`. Append a commented entry `pii.subject_collection.no_producer:retrieval`. The comment says: search log carries `userUuid`; the retrieval PIIProducer arrives in PR 6, which deletes this line.

**Interfaces:**
- Produces:

```go
// models
type SearchTimings struct {
	TotalMs   int64 `bson:"totalMs"`
	ResolveMs int64 `bson:"resolveMs"` // steps 1-3
	EmbedMs   int64 `bson:"embedMs"`
	EngineMs  int64 `bson:"engineMs"`  // every engine.Query call
	CheckMs   int64 `bson:"checkMs"`   // tombstone veto + Visible
	RerankMs  int64 `bson:"rerankMs"`
}

const (
	SearchOutcomeOK       = "ok"
	SearchOutcomeDegraded = "degraded"
	SearchOutcomeError    = "error"
)

type SearchLog struct {
	UUID       string                 `bson:"uuid"`
	TenantID   string                 `bson:"tenantId"`
	UserUUID   string                 `bson:"userUuid"`   // the searcher
	Corpus     string                 `bson:"corpus"`
	Query      string                 `bson:"query"`
	Filter     *iface.RetrievalFilter `bson:"filter,omitempty"` // as validated
	Mode       string                 `bson:"mode"`       // effective: hybrid|dense|keyword
	Principals []string               `bson:"principals"`
	SourceIDs  []string               `bson:"sourceIds"`  // returned, rank order, unique
	Subjects   []string               `bson:"subjects"`   // union over returned points, sorted unique
	HitCount   int                    `bson:"hitCount"`
	Degraded   bool                   `bson:"degraded"`
	Outcome    string                 `bson:"outcome"`
	ErrorKind  string                 `bson:"errorKind,omitempty"` // not_configured|unavailable|principals|visibility_check
	Timings    SearchTimings          `bson:"timings"`
	CreatedAt  time.Time              `bson:"createdAt"`
}

// repository
const CollSearchLog = "retrieval_search_log"
type SearchLog struct{ coll *mongo.Collection }
func NewSearchLog(db *mongo.Database) *SearchLog
func (r *SearchLog) Insert(ctx context.Context, row *models.SearchLog) error            // tenantrepo.StampInsert overrides row.TenantID
func (r *SearchLog) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)  // system sweep

// services
type SearchLogPurger interface { PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) }
const DefaultSearchLogRetentionDays = 90
// PurgeSearchLog deletes rows created before now - days. days < 1 is refused
// (an error, nothing deleted).
func PurgeSearchLog(ctx context.Context, p SearchLogPurger, days int, now time.Time) (int64, error)
```

- [ ] **Step 1: Write the failing tests.**

```go
// repository/search_log_test.go (skips without MONGO_TEST_URI)
func TestSearchLog_InsertStampsTenantFromContext(t *testing.T) // ctx tenant "t-1", row.TenantID "t-other" -> stored doc tenantId == "t-1"
func TestSearchLog_InsertWithoutTenantFails(t *testing.T)      // t.Setenv("ENV","production"); context.Background() -> errors.Is(err, tenantrepo.ErrTenantScopeMissing); count == 0
func TestSearchLog_PurgeBeforeSweepsEveryTenant(t *testing.T)  // rows: t-1 @ now-100d, t-2 @ now-100d, t-1 @ now-1d; PurgeBefore(now-90d) -> 2; remaining == 1 (the recent t-1 row)

// services/search_log_retention_test.go
func TestPurgeSearchLog_CutoffIsDaysBeforeNow(t *testing.T) // fake purger records cutoff; now 2026-10-26T00:00:00Z, days 90 -> cutoff 2026-07-28T00:00:00Z
func TestPurgeSearchLog_RefusesNonPositiveDays(t *testing.T) // days 0 -> err != nil, purger not called
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/repository/ ./internal/core/retrieval/services/ -run 'SearchLog' -v` (with `MONGO_TEST_URI` set for the repository tests).
  - Expected: FAIL, undefined symbols.

- [ ] **Step 3: Implement** the model, the repository and `PurgeSearchLog`. `PurgeBefore` carries the annotation `//tenantscope:allow system: search-log retention runs for every tenant on the daily ticker; it deletes only rows older than search_log_retention_days` directly above its `DeleteMany`. Add the piiscan baseline entry.

- [ ] **Step 4: Run the tests and check they pass.**
  - Run: the same command, plus `make -C .. backend-tenantscope backend-piiscan` from `backend/`.
  - Expected: PASS, and both analyzers green.

- [ ] **Step 5: Commit.** Message: `feat(retrieval): add the search log collection and retention purge`, with the trailers.

### Task 5: Request planning and snippets (pure functions)

**Files:**
- Create: `backend/internal/core/retrieval/services/search_params.go`
- Create: `backend/internal/core/retrieval/services/search_params_test.go`
- Create: `backend/internal/core/retrieval/services/snippet.go`
- Create: `backend/internal/core/retrieval/services/snippet_test.go`

**Interfaces:**
- Produces:

```go
const (
	DefaultSearchLimit   = 10
	MaxSearchLimit       = 100
	MaxQueryRunes        = 2000
	MaxRerankTopK        = 200
	// Fusion and rerank defaults are PR 3's DefaultFusion and DefaultRerankTopK
	// (same package): never redeclare them here.
	SnippetMaxRunes      = 300
)

type searchPlan struct {
	Limit   int                   // 1..100
	Mode    iface.RetrievalMode   // never ""
	Fusion  iface.RetrievalFusion // effective; Candidates >= Limit
	RerankK int                   // 0 when the corpus has rerank disabled
	Fetch   int                   // first engine Query.Limit
}

// planSearch validates the request shape (not the filter) and derives the
// effective parameters. Errors wrap iface.ErrRetrievalInvalidRequest.
func planSearch(req iface.RetrievalSearchRequest, spec iface.RetrievalCorpusSpec) (searchPlan, error)

// Snippet returns at most max runes of text for display.
func Snippet(text, query string, max int) string
```

`planSearch` rules:

| Input | Rule |
|---|---|
| Corpus | `""` is refused |
| Query | refused when `strings.TrimSpace` gives `""` or it has more than 2000 runes |
| Mode | `""` means `hybrid`; anything outside `{hybrid, dense, keyword}` is refused |
| Limit | `0` means 10; outside 1–100 is refused |

Fusion is resolved field by field: request (a non-zero value overrides) → corpus spec (non-zero) → `DefaultFusion`. Bounds:
- Weights must satisfy `0 < w ≤ 10`, finite.
- `Candidates` must be within 1–500.
- An override outside these bounds is refused.
- `Candidates = max(Candidates, Limit)`.

Derived values:
- `RerankK = min(MaxRerankTopK, max(TopK or DefaultRerankTopK, Limit))` (PR 3's constant, 20) when `spec.Rerank.Enabled`, else 0.
- `Fetch = min(Candidates, max(4*Limit, RerankK))`.

`Snippet` rule:
- Collapse whitespace runs to one space and trim.
- If the result fits in `max` runes, return it whole.
- Otherwise split the query on runes that are neither letters nor digits, and keep terms of at least 2 runes.
- Find the earliest case-insensitive match (`unicode.ToLower` on runes) of any term.
- The window of `max` runes starts at the last space at or before `match − max/4`, or at 0 when there is no match.
- Each cut side gets `…`, and the ellipses count toward `max`.

- [ ] **Step 1: Write the failing tests.**

```go
func TestPlanSearch_Defaults(t *testing.T) // {Corpus:"c",Query:"q"}, zero spec -> Limit 10, Mode hybrid, Fusion{1,1,50}, RerankK 0, Fetch 40
func TestPlanSearch_RejectsBadShapes(t *testing.T) // each errors.Is(err, iface.ErrRetrievalInvalidRequest):
	// Corpus ""; Query "   "; Query of 2001 runes ("à"×2001); Mode "semantic"; Limit -1; Limit 101;
	// Fusion{DenseWeight: 11}; Fusion{KeywordWeight: -1}; Fusion{DenseWeight: math.Inf(1)}; Fusion{Candidates: 501}
func TestPlanSearch_FusionOverrideFieldByField(t *testing.T) // spec Fusion{2,3,50}, req Fusion{DenseWeight:4} -> Fusion{4,3,50}
func TestPlanSearch_CandidatesNeverBelowLimit(t *testing.T)  // spec Candidates 5, Limit 20 -> Candidates 20, Fetch 20
func TestPlanSearch_RerankK(t *testing.T) // Enabled TopK 0, Limit 10 -> RerankK 20, Fetch 40; TopK 20, Limit 30 -> RerankK 30, Fetch 50; disabled -> 0

func TestSnippet_ShortTextCollapsed(t *testing.T)   // Snippet("a  b\n c", "x", 300) == "a b c"
func TestSnippet_WindowAroundFirstMatch(t *testing.T) // 1000-rune text with "Responsabile" at rune 600; result contains "Responsabile", starts and ends with "…", utf8.RuneCountInString <= 300
func TestSnippet_MatchNearStartHasNoLeadingEllipsis(t *testing.T)
func TestSnippet_NoMatchTakesTheHead(t *testing.T)  // starts with the text's first word, ends with "…"
func TestSnippet_CaseInsensitiveMultibyte(t *testing.T) // query "città", text contains "CITTÀ" past rune 400 -> result contains "CITTÀ"; utf8.ValidString(result)
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -run 'PlanSearch|Snippet' -v`
  - Expected: FAIL, undefined symbols.

- [ ] **Step 3: Implement** both functions per the rules above.

- [ ] **Step 4: Run the tests and check they pass.** Same command; expected PASS.

- [ ] **Step 5: Commit.** Message: `feat(retrieval): plan search parameters and build snippets`, with the trailers.

### Task 6: Searcher — gate, pin, principals, arms, fusion, results (steps 1–5, 8)

**Files:**
- Create: `backend/internal/core/retrieval/services/search.go`
- Create: `backend/internal/core/retrieval/services/search_fakes_test.go`
- Create: `backend/internal/core/retrieval/services/search_test.go`

**Interfaces:**
- Consumes: Task 2 `filter.Compile`, Task 3 (through `iface.RetrievalPrincipalResolver`), Task 5 `planSearch` and `Snippet`, `engine.*`, PR 4's `module.ModuleStatusReader` (`IsStarted`, resolved from `module.ServiceModuleStatus`), `tenantrepo.RequireInternalTenant`, `ctxauth`.
- Produces:

```go
type CorpusLookup interface {
	Corpus(name string) (iface.RetrievalCorpusSpec, bool)
}

// PinnedGeneration is the pair search pins for the whole request (ADR D5 step 2).
type PinnedGeneration struct {
	Collection     string // <prefix>_<corpus>_g<generation>
	Generation     int
	EmbedProfileID string // InferenceProfile.ID the generation was built with
}

var ErrNoActiveGeneration = errors.New("retrieval: the corpus has no active generation yet")

type CorpusStateReader interface {
	ActiveGeneration(ctx context.Context, corpus string) (PinnedGeneration, error)
}

type TombstoneChecker interface { // satisfied by PR 4's *Tombstones
	Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error)
}

type SearchLogWriter interface {
	Insert(ctx context.Context, row *models.SearchLog) error
}

const DefaultVisibleTimeout = 2 * time.Second

type SearchDeps struct {
	Corpora        CorpusLookup
	State          CorpusStateReader
	Engine         engine.Engine // nil: QDRANT_URL unset -> ErrRetrievalNotConfigured
	Tombstones     TombstoneChecker
	Authz          iface.AuthzProvider
	Principals     iface.RetrievalPrincipalResolver
	Profiles       iface.LLMInferenceProfiles // nil: dense arm and rerank unavailable
	Embedder       iface.LLMPinnedEmbedder    // nil: dense arm unavailable
	Reranker       iface.LLMReranker          // nil: rerank unavailable
	Modules        module.ModuleStatusReader
	Log            SearchLogWriter
	Now            func() time.Time // nil: time.Now
	Logger         *slog.Logger
	VisibleTimeout time.Duration    // 0: DefaultVisibleTimeout
}

type Searcher struct{ /* deps */ }
func NewSearcher(d SearchDeps) *Searcher
func (s *Searcher) Search(ctx context.Context, req iface.RetrievalSearchRequest) (*iface.RetrievalSearchResult, error)
func (s *Searcher) FilterSchema(corpus string) ([]iface.RetrievalFilterField, error) // copy; unknown -> ErrRetrievalUnknownCorpus
```

#### Pipeline, in this exact order

**Step 1 — Gate:**
1. `tenantrepo.RequireInternalTenant(ctx)`. Failure returns `fmt.Errorf("%w: %w", iface.ErrRetrievalForbidden, err)`.
2. User and tenant from `ctxauth`. If either is missing: `ErrRetrievalForbidden`.
3. `planSearch`.
4. Corpus lookup. Unknown: `ErrRetrievalUnknownCorpus`.
5. `Authz.HasPermission(user, tenant, spec.Permission)`:
   - `false` returns `ErrRetrievalForbidden`.
   - An error is returned wrapped, never as forbidden.
6. `filter.Compile(req.Filter, spec.Filters)`.
7. `Modules.IsStarted(spec.Owner)`. False returns `ErrRetrievalCorpusUnavailable` (the same check PR 4's worker gate uses, so search and indexing agree on "started").

**Step 2 — Resolve and pin:**
- `Engine == nil` returns `ErrRetrievalNotConfigured`.
- Call `State.ActiveGeneration` **once**.
  - `ErrNoActiveGeneration` returns `{Hits: []RetrievalHit{}, Degraded: true}` with no engine call.
  - Any other error wraps `ErrRetrievalUnavailable`.

**Step 3 — Principals:** `Principals.Principals(ctx)`. An error is returned wrapped, and the engine is not called.

**Step 4 — Arms.** The dense arm runs only when `Mode != keyword`, and it needs all of the following:
- `Profiles` and `Embedder` are non-nil.
- `Profiles.InferenceProfile(ctx, spec.Ingestion.EmbedEndpoint)` succeeds.
- `p.ID == pinned.EmbedProfileID`.
- The locality allows `p.Boundary`. `local_only`, including `""`, requires `InferenceBoundaryLocal`.
- `EmbedWithProfile(ctx, PinnedEmbedRequest{Caller: "retrieval", OnBehalfOf: tenant, Profile: pinned.EmbedProfileID, Kind: EmbedInputQuery, Inputs: []string{req.Query}})` succeeds.
- It returns exactly one non-empty vector, of length `p.Dimensions` when `p.Dimensions > 0`.

Any failure, including a locality refusal, makes the search keyword-only with degraded set, and WARNs with endpoint and reason (no text). An embed error while `ctx.Err() != nil` returns `ctx.Err()` instead. The keyword arm (`Query.Keyword = req.Query`) runs unless `Mode == dense` and the dense vector was obtained.

**Step 5 — Fusion:** one `Engine.Query(ctx, pinned.Collection, engine.Query{TenantID: tenant, Principals: pc.Principals, Filter: compiled, Dense: vec, Keyword: kw, Weights: {plan.Fusion.DenseWeight, plan.Fusion.KeywordWeight}, PerArm: plan.Fusion.Candidates, Limit: plan.Fetch, Diagnostics: false})`. Any engine error wraps `ErrRetrievalUnavailable`; this covers `engine.ErrUnavailable`, `engine.ErrNotFound` and anything else.

**Step 8 — Result.** Take the first `Limit` candidates and build each `RetrievalHit`:
- `Rank` is 1-based.
- `Score` is the candidate's score.
- `Snippet` is `Snippet(c.Text, req.Query, SnippetMaxRunes)`.
- `Provenance` comes from `c.Runtime`: SourceID, ChunkID, Ordinal, Page, CharStart, CharEnd.
- `Locality` is the corpus locality, with `""` returned as `local_only`.

`Hits` is never nil.

Fakes in `search_fakes_test.go`, each recording its calls. PR 4's `services/fakes_test.go` is in the same package and declares `fakeEngine`, `fakeEmbedder`, `fakeSource`, `fakeProfiles`, `fakeModules` (and more), so every fake of this PR is prefixed `search` instead of `fake`:

| Fake | Behaviour |
|---|---|
| `searchEngine` | `queries []engine.Query`, `collections []string`, scripted `results [][]engine.Candidate`, `err error`; other `Engine` methods panic |
| `searchState` | returns a sequence of `PinnedGeneration`s (to prove the pin is read once) |
| `searchProfiles` | map endpoint → `*InferenceProfile` / error |
| `searchEmbedder` | `reqs`, `vec`, `err` |
| `searchReranker` | `reqs`, `res`, `err` |
| `searchSource` | `RetrievalSource`: `visible map[string]iface.RetrievalVisibility` (default: visible at the candidate's revision), `err`, `block bool`, `calls [][]string`; `Load`/`Enumerate` panic |
| `searchTombstones` | `covered map[string]bool`, `err`, `probes [][]Probe` |
| `searchAuthz` | `allow bool`, `err`, `asked []string` |
| `searchPrincipals` | |
| `searchModules` | |
| `searchCorpora` | |
| `searchLog` | `rows []*models.SearchLog`, `err`, `ctxTenant []string` |

Helpers:
- `cand(source string, rev int64, ordinal int, text string, subjects ...string) engine.Candidate`: TenantID `"t-1"`, ChunkID `source+"#"+ordinal`, Score `1/float32(ordinal+1)`.
- `newSearchFixture(t)`:
  - Corpus spec: corpus `"contacts"`, owner `"crm"`, permission `"crm.contact.read"`, filters `team keyword` and `seniority integer`, embed endpoint `"embed"`, rerank endpoint `"rerank"` (disabled), locality `""`.
  - Pin: `{"orkestra_contacts_g1", 1, "ip_embed_a"}`.
  - Profiles:
    - `"embed"`: `{ID: "ip_embed_a", Kind: embed, Boundary: local, Dimensions: 4}`.
    - `"rerank"`: `{ID: "ip_rerank_a", Kind: rerank, Boundary: local}`.
  - Embedder vector: `[0.1, 0.2, 0.3, 0.4]`.
  - Principals: `{t-1, u-1, ["tenant:*", "user:u-1"]}`.
  - Engine result: `s1`, `s2`, `s3` at revision 1. Subjects: `s1` = `user:alice`, `s2` = `crm:c-2`, `s3` = `user:bob`.
  - `ctx` is an internal tenant (`ctxauth.WithTenantKind(..., "internal")`, as in `internal/core/llm/services/ctx_test.go`).
- `fx.search(req)` calls `NewSearcher(fx.deps()).Search(fx.ctx, req)`.

- [ ] **Step 1: Write the failing tests** in `search_test.go`:

```go
func TestSearch_RefusesExternalTenant(t *testing.T)                // ErrRetrievalForbidden; len(engine.queries)==0
func TestSearch_RefusesCallerWithoutCorpusPermission(t *testing.T) // ErrRetrievalForbidden; authz.asked == []string{"u-1|t-1|crm.contact.read"}
func TestSearch_PermissionCheckErrorFailsClosed(t *testing.T)      // err != nil && !errors.Is(err, iface.ErrRetrievalForbidden); no engine call
func TestSearch_UnknownCorpus(t *testing.T)                        // ErrRetrievalUnknownCorpus
func TestSearch_InvalidRequest(t *testing.T)                       // Limit 101 -> ErrRetrievalInvalidRequest
func TestSearch_InvalidFilter(t *testing.T)                        // {salary eq 1} -> ErrRetrievalInvalidFilter; no engine call
func TestSearch_OwnerModuleDisabled(t *testing.T)                  // modules["crm"]=false -> ErrRetrievalCorpusUnavailable; no engine call
func TestSearch_GateOrder(t *testing.T) // external tenant + unknown corpus -> Forbidden; no permission + invalid filter -> Forbidden; disabled owner + invalid filter -> InvalidFilter
func TestSearch_NotConfigured(t *testing.T)                        // Engine nil -> ErrRetrievalNotConfigured
func TestSearch_NoActiveGenerationIsEmptyAndDegraded(t *testing.T) // Hits len 0 (not nil), Degraded true, no engine call
func TestSearch_PinsCollectionAndProfileOnce(t *testing.T)         // state sequence g1 then g2: state reads == 1; engine collection "orkestra_contacts_g1"; embed Profile "ip_embed_a"
func TestSearch_InjectsTenantAndPrincipals(t *testing.T)           // q.TenantID "t-1"; q.Principals == []string{"tenant:*","user:u-1"}; q.Diagnostics false
func TestSearch_PrincipalResolutionErrorFailsClosed(t *testing.T)  // principals err -> err returned (errors.Is the fake's error); no engine call
func TestSearch_HybridSendsBothArms(t *testing.T) // q.Dense == fake vector; q.Keyword == "head of sales"; q.Weights == engine.Weights{1,1}; q.PerArm 100; q.Limit 40;
	// embed req == PinnedEmbedRequest{Caller:"retrieval", OnBehalfOf:"t-1", Profile:"ip_embed_a", Kind: iface.EmbedInputQuery, Inputs: []string{"head of sales"}}; Degraded false
func TestSearch_ModeKeywordSkipsInference(t *testing.T)            // no embed, no profile call; q.Dense nil; Degraded false
func TestSearch_ModeDenseSkipsKeyword(t *testing.T)                // q.Keyword == ""; q.Dense set
func TestSearch_ProfileMismatchFallsBackToKeyword(t *testing.T) { // Review Focus 3
	// subcases: profile ID "ip_embed_b"; InferenceProfile err iface.ErrLLMProfileMismatch;
	// EmbedWithProfile err iface.ErrLLMProfileMismatch; EmbedWithProfile err iface.ErrLLMProviderUnavailable;
	// vector of length 3 for Dimensions 4
	// each: err nil; Degraded true; q.Dense nil; q.Keyword == query; first two subcases: len(embedder.reqs)==0
}
func TestSearch_LocalOnlyRefusesHostedEmbedEndpoint(t *testing.T) // boundary hosted, locality "" -> no embed call, Degraded true; locality hosted_allowed -> embed called, Degraded false
func TestSearch_ModeDenseWithInferenceDownIsKeywordDegraded(t *testing.T) // q.Keyword == query, Degraded true
func TestSearch_FusionOverrideWithinBounds(t *testing.T)           // req.Fusion{DenseWeight:3} -> q.Weights == engine.Weights{3,1}
func TestSearch_EngineErrorsAreUnavailable(t *testing.T)           // engine.ErrUnavailable, engine.ErrNotFound, errors.New("x") -> each errors.Is(err, iface.ErrRetrievalUnavailable)
func TestSearch_HitsCarrySnippetProvenanceLocality(t *testing.T)   // hits[0] == RetrievalHit{SourceID "s1", ChunkID "s1#0", Rank 1, Score 1, Snippet "<text>", Provenance{...}, Locality iface.RetrievalLocalOnly}; ranks 1..3
func TestFilterSchema(t *testing.T)                                // known corpus -> copy (mutating it does not change the next call); unknown -> ErrRetrievalUnknownCorpus
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -run 'TestSearch_|TestFilterSchema' -v`
  - Expected: FAIL, "undefined: NewSearcher".

- [ ] **Step 3: Implement `Searcher`** with steps 1–5 and 8 as specified. Keep each step a private method so Tasks 7 and 8 insert theirs between step 5 and step 8:
  - `gate(ctx, req) (gated, error)`
  - `pin(ctx, corpus) (PinnedGeneration, error)`
  - `denseVector(ctx, spec, pinned, query) ([]float32, bool)`
  - `query(ctx, …) ([]engine.Candidate, error)`
  - `toHits(spec, query, cands) []iface.RetrievalHit`

- [ ] **Step 4: Run the tests and check they pass.** Same command; expected PASS.

- [ ] **Step 5: Commit.** Message: `feat(retrieval): search gate, pinned generation and hybrid arms`, with the trailers.

### Task 7: Step 6 — erasure veto, source check, over-fetch refill

**Files:**
- Modify: `backend/internal/core/retrieval/services/search.go`
- Create: `backend/internal/core/retrieval/services/search_visibility_test.go`

**Interfaces:**
- Consumes: `TombstoneChecker`, `Probe` (PR 4), `iface.RetrievalSource.Visible`, `SearchDeps.VisibleTimeout`.
- Produces:

```go
// checkCandidates applies ADR D5 step 6 to cands in order and returns the
// survivors in the same order. Any error wraps iface.ErrRetrievalVisibilityCheck.
func (s *Searcher) checkCandidates(ctx context.Context, tenantID, corpus string, src iface.RetrievalSource,
	pc iface.RetrievalPrincipalContext, cands []engine.Candidate) ([]engine.Candidate, error)
```

**`checkCandidates` order:**
1. Drop any candidate whose `Runtime.TenantID != tenantID`, and WARN with corpus only.
2. Build one `Probe` per distinct SourceID. Its subjects are the union across that source's chunks, sorted.
3. Call `Tombstones.Covered(ctx, tenantID, corpus, probes)` and drop covered sources.
4. Call `src.Visible` under `context.WithTimeout(ctx, VisibleTimeout)` with the distinct remaining SourceIDs, in first-seen order. Drop any id missing from the map, any with `!Visible`, and any whose `Revision != c.Runtime.SourceRevision`.
5. Any error, or a deadline from steps 3–4, returns `fmt.Errorf("%w: …", iface.ErrRetrievalVisibilityCheck)`, and no candidate survives.

If every candidate was dropped before step 4, `Visible` is not called.

**Refill (decision 3):** after the first query, `survivors := checkCandidates(first)`. If `len(survivors) < plan.Limit && len(first) == plan.Fetch && plan.Fetch < plan.Fusion.Candidates`:
- Query again with `Limit = plan.Fusion.Candidates`.
- Check only the candidates whose `PointID` was not in `first`.
- Append the new survivors, keeping the second call's order.

`shortfall = len(survivors) < plan.Limit && len(lastCall) == lastCall's Query.Limit`. A shortfall sets `Degraded`. There is never a third engine call.

- [ ] **Step 1: Write the failing tests.**

```go
func TestSearch_TombstoneVetoDropsBeforeVisible(t *testing.T) { // Review Focus 2
	// covered["s3"]=true; source would say visible
	// want: hit SourceIDs == ["s1","s2"]; source.calls == [][]string{{"s1","s2"}} (s3 never asked)
}
func TestSearch_TombstoneProbeCarriesSubjects(t *testing.T)  // probes[0] == []Probe{{"s1",["user:alice"]},{"s2",["crm:c-2"]},{"s3",["user:bob"]}}; two chunks of s1 with subjects a,b -> one probe {"s1",["user:a","user:b"]}
func TestSearch_TombstoneErrorFailsClosed(t *testing.T)      // ErrRetrievalVisibilityCheck; result nil
func TestSearch_ForeignTenantCandidateDropped(t *testing.T)  // a candidate with Runtime.TenantID "t-2" never reaches Visible nor the hits
func TestSearch_VisibleRefusedDropped(t *testing.T)          // Review Focus 1: visible["s2"]={false,1} -> hits ["s1","s3"]
func TestSearch_VisibleMissingIDIsNotVisible(t *testing.T)   // map lacks "s2" -> dropped
func TestSearch_StaleRevisionDropped(t *testing.T)           // visible["s1"]={true,2}, candidate rev 1 -> dropped
func TestSearch_VisibleErrorFailsClosed(t *testing.T)        // ErrRetrievalVisibilityCheck; result nil
func TestSearch_VisibleTimeoutFailsClosed(t *testing.T)      // source.block=true, VisibleTimeout 20ms -> ErrRetrievalVisibilityCheck, returns in < 1s
func TestSearch_VisibleGetsCallerPrincipalContext(t *testing.T) // pc == {TenantID "t-1", UserUUID "u-1", Principals ["tenant:*","user:u-1"]}
func TestSearch_RefillsOnceWhenDropsShortenThePage(t *testing.T) { // Review Focus 4
	// Limit 2 -> Fetch 8; call 1 returns 8 candidates a1..a8, Visible refuses a1..a7;
	// call 2 (Limit 100) returns a1..a8 + b1..b4; Visible call 2 ids == ["b1","b2","b3","b4"]
	// want: len(engine.queries)==2; queries[1].Limit==100; hits ["a8","b1"]; Degraded false
}
func TestSearch_ShortPageAfterRefillIsFlagged(t *testing.T)  // Limit 2; call 1 as above (a8 survives); call 2 returns exactly 100 (a1..a8 + 92 new), every new one refused -> hits ["a8"], Degraded true, len(engine.queries)==2
func TestSearch_ShortPageWithExhaustedIndexIsNotFlagged(t *testing.T) // call 1 returns 3 (< Fetch 40), one refused -> 2 hits, 1 engine call, Degraded false
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -run 'TestSearch_(Tombstone|Visible|Foreign|StaleRevision|Refill|ShortPage)' -v`
  - Expected: FAIL. Hits still include vetoed sources, and there is no second query.

- [ ] **Step 3: Implement** `checkCandidates` and the refill between step 5 and step 8.

- [ ] **Step 4: Run the tests and check they pass.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -v`
  - Expected: PASS (Task 6 tests included).

- [ ] **Step 5: Commit.** Message: `feat(retrieval): erasure veto, source visibility check and refill`, with the trailers.

### Task 8: Step 7 — optional rerank

**Files:**
- Modify: `backend/internal/core/retrieval/services/search.go`
- Create: `backend/internal/core/retrieval/services/search_rerank_test.go`

**Interfaces:**
- Consumes: `iface.LLMReranker`, `iface.LLMInferenceProfiles`, `searchPlan.RerankK`.
- Produces: `func (s *Searcher) rerank(ctx context.Context, tenantID string, spec iface.RetrievalCorpusSpec, query string, survivors []engine.Candidate, k int) (ordered []engine.Candidate, scores []float32, ok bool)`

**Rules:**
- Rerank only when `plan.RerankK > 0`, with `n = min(RerankK, len(survivors))`, `n ≥ 1`.
- Resolve the profile with `Profiles.InferenceProfile(ctx, spec.Rerank.Endpoint)`. The locality check is the same as the embed check.
- Request: `RerankRequest{Caller: "retrieval", OnBehalfOf: tenantID, Profile: p.ID, Query: query, Documents: texts of survivors[:n] in fused order, TopN: n}`.
- Valid response: exactly `n` results, indexes in `[0, n)` and unique. The head is reordered by result order, and each hit's `Score` is the rerank score.
- Any failure keeps the fused order and fused scores and sets `Degraded` (decision 1). Failures are: nil companions, a profile error, a locality refusal, a call error, or a malformed result. The WARN carries the reason.
- Survivors beyond `n` follow the head in fused order. With `RerankK ≥ Limit` they are never returned.

- [ ] **Step 1: Write the failing tests.**

```go
func TestSearch_RerankDisabledByDefault(t *testing.T)    // len(reranker.reqs)==0
func TestSearch_RerankReordersHead(t *testing.T)         // Enabled, TopK 50, Limit 2; survivors s1,s2,s3; res [{2,0.9},{0,0.5},{1,0.1}]
	// want: req == RerankRequest{Caller:"retrieval", OnBehalfOf:"t-1", Profile:"ip_rerank_a", Query:q, Documents:[text1,text2,text3], TopN:3}
	// hits ["s3","s1"], scores [0.9,0.5], Degraded false
func TestSearch_RerankNeverSeesRefusedText(t *testing.T) // Review Focus 1: Visible refuses s2, tombstone covers s3 -> Documents == [text1]
func TestSearch_RerankFailureKeepsFusedOrderDegraded(t *testing.T)    // reranker err -> hits ["s1","s2"], fused scores, Degraded true
func TestSearch_RerankHostedEndpointRefusedForLocalOnly(t *testing.T) // rerank profile Boundary hosted -> no reranker call, Degraded true
func TestSearch_RerankMalformedResultIsFailure(t *testing.T)          // results [{0,..},{0,..},{1,..}] (duplicate) and [{5,..}] (range) -> fused order, Degraded true
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -run TestSearch_Rerank -v`
  - Expected: FAIL. The reranker is never called.

- [ ] **Step 3: Implement** `rerank` and call it between the refill and step 8.

- [ ] **Step 4: Run the tests and check they pass.**
  - Run: `cd backend && go test ./internal/core/retrieval/services/ -v`
  - Expected: PASS.

- [ ] **Step 5: Commit.** Message: `feat(retrieval): optional rerank with degraded fallback`, with the trailers.

### Task 9: Search log writes and module wiring

**Files:**
- Modify: `backend/internal/core/retrieval/services/search.go` (search-log write)
- Create: `backend/internal/core/retrieval/services/search_log_test.go`
- Create: `backend/internal/core/retrieval/services/search_state.go`, the adapter from PR 4's corpus-state read to `CorpusStateReader`.
- Modify: `backend/internal/core/retrieval/services/service.go`. Add methods. If PR 3 left stubs for `Search`, `FilterSchema`, `DeleteBySubject` or `Principals`, replace them.
- Create: `backend/internal/core/retrieval/services/service_search_test.go`
- Modify: `backend/internal/core/retrieval/module.go`: `Collections()`, `ConfigSchema()`, `Init`, `Start`/`Stop`.
- Modify: `backend/internal/core/retrieval/module_test.go`
- Modify: `backend/internal/core/retrieval/AGENTS.md`
- Modify: `docs/site/modules/core/retrieval.mdx`

**Interfaces:**
- Consumes: Tasks 3–8. From PR 3: corpus lookup, contributor list, the `*services.Service` type and its constructor, and the engine built from `QDRANT_URL` (nil when unset). From PR 4: `*Tombstones` and the corpus-state read.
- Produces:

```go
// services/search.go
var ErrSearchLogWrite = errors.New("retrieval: search log write failed")

// services/service.go
var ErrDeleteBySubjectNotImplemented = fmt.Errorf("retrieval: DeleteBySubject arrives with ADR-0023 PR 6: %w", errors.ErrUnsupported)
func (s *Service) SetSearch(searcher *Searcher, principals *PrincipalResolver) // called once from module Init
func (s *Service) Search(ctx context.Context, req iface.RetrievalSearchRequest) (*iface.RetrievalSearchResult, error) // nil searcher -> ErrRetrievalNotConfigured
func (s *Service) FilterSchema(corpus string) ([]iface.RetrievalFilterField, error)
func (s *Service) DeleteBySubject(ctx context.Context, tenantID, subject string) error // returns ErrDeleteBySubjectNotImplemented, does nothing else; PR 6 replaces the body
func (s *Service) Principals(ctx context.Context) (iface.RetrievalPrincipalContext, error) // nil resolver -> fail closed with ErrRetrievalForbidden

// module.go
type searchSettings struct{ principalCap, retentionDays int }
func clampSearchSettings(s searchSettings) searchSettings // principalCap 16..4096 (0 -> 256), retentionDays 1..3650 (0 -> 90)
func readSearchSettings(deps *module.Dependencies) searchSettings // deps.GetConfigInt("retrieval", "principal_cap", 256), ("retrieval","search_log_retention_days", 90), then clamp; read on every use (hot reload)
type searchServices struct {
	authz    iface.AuthzProvider
	roles    iface.AuthzRoleLister
	members  iface.TenantProvider
	profiles iface.LLMInferenceProfiles
	embedder iface.LLMPinnedEmbedder
	reranker iface.LLMReranker
}
func resolveSearchServices(reg *module.ServiceRegistry) searchServices // GetTyped on ServiceAuthzProvider (AuthzProvider and AuthzRoleLister), ServiceTenantProvider; type assertions on the ServiceLLMGateway value
```

**Search-log write** (decision 2):
- Every search that passed step 1 builds one `models.SearchLog`:
  - `UUID` is `uuid.NewString()`. `UserUUID`, `Corpus`, `Query`, `Filter` (`req.Filter`), the effective `Mode`, and `Principals` come from the request and its resolution.
  - `SourceIDs` and `Subjects` come from the returned hits only.
  - `HitCount` and `Degraded` describe the result. `Outcome` is `ok`, `degraded` or `error`, and `ErrorKind` is set on errors.
  - `Timings` are measured with `Now`. `CreatedAt` is `Now().UTC()`.
- Success path: the row is inserted **before** the result is returned. An insert error returns `fmt.Errorf("%w: %w", iface.ErrRetrievalUnavailable, ErrSearchLogWrite)` and no result.
- Error path after the gate: the insert is best-effort, a failure only WARNs, and the original error is returned.
- Gate refusals write no row.

**Module wiring.** `Collections()` adds `repository.CollSearchLog` with these indexes:
- `{tenantId:1, createdAt:-1}`
- `{tenantId:1, userUuid:1}`
- `{subjects:1}`, used by PR 6 for DSR, which crosses tenants
- `{createdAt:1}`, used by the retention sweep

`ConfigSchema()` adds two `module.FieldInt` keys, with `Min`/`Max` pointers as in `internal/core/llm/module.go:228`. If the module declares `ConfigGroups()`, put them in a new group `{Key: "search", Label: "Search & audit"}`.

| Key | Default | Range | Env |
|---|---|---|---|
| `principal_cap` | `"256"` | 16–4096 | `RETRIEVAL_PRINCIPAL_CAP` |
| `search_log_retention_days` | `"90"` | 1–3650 | `RETRIEVAL_SEARCH_LOG_RETENTION_DAYS` |

Description copy:
- `principal_cap`: "Maximum number of principals (user, tenant, roles, contributors) a search may carry; a caller above it gets no results until the cap is raised. Fails closed."
- `search_log_retention_days`: "Search-log rows older than this are deleted daily. The log records who searched for what and which people the results named (ADR-0023 D12)."

`Init`:
- Build `PrincipalResolver{Members: svcs.members, Roles: svcs.roles, Contributors: <PR 3 contributor list>, Cap: func() int { return readSearchSettings(deps).principalCap }}`.
- Build `NewSearcher(SearchDeps{…})`, with `Modules` = `module.GetTyped[module.ModuleStatusReader](deps.Services, module.ServiceModuleStatus)` (PR 4). A started module passes; an optional owner disabled at runtime is stopped by the registry, so it fails the check.
- Call `svc.SetSearch(...)`.
- Missing authz, role lister or tenant provider: WARN and leave the field nil, which fails closed per search. Missing LLM companions: WARN, and searches degrade. Init never returns an error for any of these, and never contacts Qdrant or an endpoint (ADR D1).

`Start`/`Stop`: add `searchLogRetentionLoop(ctx)`, which mirrors `internal/core/llm/module.go:540-558` (first run after 1 min, then every 24 h, test-seam fields). Each tick calls `services.PurgeSearchLog(ctx, repo, readSearchSettings(deps).retentionDays, time.Now())` and logs the deleted count at INFO, or a failure at WARN. It runs under the same cancel and wait group as PR 4's worker.

**Corpus-state adapter.** `search_state.go` defines `corpusStateReader` implementing `ActiveGeneration` over PR 4's corpus-state read:
- `Generation` is the active generation number.
- `EmbedProfileID` is the embedding `InferenceProfile.ID` recorded for that generation.
- `Collection` is the collection name built by PR 3/4's naming helper, `<prefix>_<corpus>_g<generation>`.
- A corpus without an active generation returns `ErrNoActiveGeneration`.

- [ ] **Step 1: Write the failing tests.**

```go
// services/search_log_test.go
func TestSearch_WritesSearchLogRow(t *testing.T) // Visible refuses s2; want one row:
	// UserUUID "u-1", Corpus "contacts", Query q, Filter == req.Filter, Mode "hybrid", Principals ["tenant:*","user:u-1"],
	// SourceIDs ["s1","s3"], Subjects ["user:alice","user:bob"], HitCount 2, Degraded false, Outcome "ok", ErrorKind "",
	// CreatedAt == fixed now (UTC); searchLog.ctxTenant == ["t-1"]
func TestSearch_LogWriteFailureFailsClosed(t *testing.T)      // log err -> result nil; errors.Is(err, iface.ErrRetrievalUnavailable) && errors.Is(err, ErrSearchLogWrite)
func TestSearch_FailedSearchAfterGateIsLogged(t *testing.T)   // Visible err -> row Outcome "error", ErrorKind "visibility_check", HitCount 0, SourceIDs empty; with log err as well the returned error is still ErrRetrievalVisibilityCheck
func TestSearch_GateRefusalIsNotLogged(t *testing.T)          // forbidden -> len(searchLog.rows)==0
func TestSearch_DegradedOutcomeLogged(t *testing.T)           // keyword fallback -> Outcome "degraded", Degraded true, Mode "hybrid"

// services/service_search_test.go
func TestService_ImplementsSearchContracts(t *testing.T) { var _ iface.RetrievalSearch = (*Service)(nil); var _ iface.RetrievalPrincipalResolver = (*Service)(nil) }
func TestService_SearchWithoutSearcherIsNotConfigured(t *testing.T) // ErrRetrievalNotConfigured
func TestService_DeleteBySubjectNotImplemented(t *testing.T)        // errors.Is(err, ErrDeleteBySubjectNotImplemented) && errors.Is(err, errors.ErrUnsupported)

// module_test.go
func TestClampSearchSettings(t *testing.T) // {0,0}->{256,90}; {5,0}->{16,90}; {100000,5000}->{4096,3650}; {300,30}->{300,30}
func TestCollections_DeclareSearchLog(t *testing.T) // a spec named "retrieval_search_log" with the four index key lists above
func TestConfigSchema_DeclaresSearchKeys(t *testing.T) // keys principal_cap (Default "256", Min 16, Max 4096, EnvVar RETRIEVAL_PRINCIPAL_CAP) and search_log_retention_days (Default "90", Min 1, Max 3650, EnvVar RETRIEVAL_SEARCH_LOG_RETENTION_DAYS)
func TestResolveSearchServices_AssertsCompanions(t *testing.T)
	// registry with a gateway fake implementing LLMGateway+LLMInferenceProfiles+LLMPinnedEmbedder+LLMReranker -> all three non-nil;
	// with a plain LLMGateway fake -> all three nil; an authz fake implementing AuthzProvider+AuthzRoleLister under ServiceAuthzProvider -> roles non-nil
```

- [ ] **Step 2: Run the tests and check they fail.**
  - Run: `cd backend && go test ./internal/core/retrieval/... -run 'SearchLog|FailedSearch|GateRefusal|DegradedOutcome|TestService_|ClampSearch|DeclareSearchLog|SearchKeys|ResolveSearchServices' -v`
  - Expected: FAIL, undefined symbols.

- [ ] **Step 3: Implement** the log write, the service methods, the corpus-state adapter and the module wiring as specified.

- [ ] **Step 4: Run the tests and check they pass.**
  - Run: `cd backend && go test ./internal/core/retrieval/... ./cmd/server/...`
  - Expected: PASS, including `TestCoreModulesLoadOrder`.

- [ ] **Step 5: Update the docs.**
  - `retrieval/AGENTS.md` gains a "Search" section:
    - the 8 steps with the files that own them
    - the degraded and fail-closed table (decisions 1–4, 6)
    - principals (sources, contributor rules, cap key)
    - the search log (fields, write-before-return policy, retention key, the `system:` sweep, indexes)
    - `DeleteBySubject` returning `ErrDeleteBySubjectNotImplemented` (wraps `errors.ErrUnsupported`) until PR 6
    - the temporary piiscan baseline line PR 6 removes
  - `docs/site/modules/core/retrieval.mdx` gains a "Searching a corpus" section for fork authors:
    - a `module.GetTyped[iface.RetrievalSearch](reg, module.ServiceRetrieval)` example
    - request bounds and filter ops per type
    - what `degraded` means
    - that platform administrators get no bypass
    - that every search is logged

- [ ] **Step 6: Commit.** Message: `feat(retrieval): write the search log and wire search into the module`, with the trailers.

### Task 10: Integration test against real Qdrant and MongoDB, then CI

**Files:**
- Create: `backend/internal/core/retrieval/services/search_integration_test.go`

**Interfaces:**
- Consumes: `NewSearcher`, `PrincipalResolver`, `repository.NewSearchLog`, PR 4's `*Tombstones` over the test database, PR 3's Qdrant driver built from `QDRANT_TEST_URL`/`QDRANT_TEST_API_KEY` (as PR 3's integration tests build it), `engine.PointID`, `engine.CollectionSpec`, `engine.Point`.

`newIntegrationEnv(t)`:
- Skips unless `MONGO_TEST_URI`, `QDRANT_TEST_URL` and `QDRANT_TEST_API_KEY` are all set.
- Creates a Mongo test database, and a Qdrant collection `itest_<unixnano>_contacts_g1`: `Dimensions 4`, field `team keyword`, default analyzer. The collection is dropped in `t.Cleanup`.
- Upserts three points for tenant `t-1`, revision 1, `VisibleTo ["tenant:*"]`:

| Source | Text | Subjects | Dense |
|---|---|---|---|
| `s1` | `"Maria Rossi responsabile commerciale Milano"` | `user:alice` | `[1,0,0,0]` |
| `s2` | `"Luca Bianchi responsabile commerciale Torino"` | `crm:c-2` | `[0.9,0.1,0,0]` |
| `s3` | `"Anna Verdi responsabile commerciale Roma"` | `user:bob` | `[0.8,0.2,0,0]` |

- Wires a `Searcher` with:
  - fake corpora (pinned `EmbedProfileID "ip_a"`)
  - fake profiles (`"embed"` → `ID "ip_a"`, local, 4 dims)
  - a fake embedder returning `[1,0,0,0]`
  - fake authz allow
  - a real `PrincipalResolver` over fake members and roles
  - a `searchSource`, real `Tombstones`, the real `SearchLog` repository, and the real engine

- [ ] **Step 1: Write the tests.**

```go
func TestIntegrationSearch_PrivateRecordDropped(t *testing.T) { // Review Focus 1
	// query "responsabile commerciale": baseline hits contain s1,s2,s3
	// source.visible["s2"]={false,1} -> hits ⊆ {s1,s3}, s2 absent
	// source.err = errors.New("down") -> errors.Is(err, iface.ErrRetrievalVisibilityCheck), result nil
	// source.block = true, VisibleTimeout 50ms -> ErrRetrievalVisibilityCheck
}
func TestIntegrationSearch_ErasedUserNeverReturned(t *testing.T) { // Review Focus 2
	// write a global user tombstone for "user:bob" through PR 4's tombstone write path
	// (shape: roadmap Erasure{Scope:"global", Kind:"user", Subject:"user:bob"})
	// want: s3 absent from hits; engine.Count(collection, Selector{TenantID:"t-1", SourceIDs:["s3"]}) == 1 (point still there);
	// the search_log row for this search has Subjects without "user:bob" and SourceIDs without "s3"
}
func TestIntegrationSearch_ProfileMismatchKeywordOnly(t *testing.T) { // Review Focus 3
	// fake profiles now return ID "ip_b" for "embed"
	// query "Rossi": Degraded true; embedder calls == 0; hits[0].SourceID == "s1" (keyword arm alone)
}
```

- [ ] **Step 2: Run the tests with the infrastructure.**
  - Run: `cd backend && MONGO_TEST_URI=... QDRANT_TEST_URL=... QDRANT_TEST_API_KEY=... go test ./internal/core/retrieval/services/ -run TestIntegrationSearch -v`
  - Expected: PASS. Without the variables, expected SKIP. A skip proves nothing about these paths (roadmap Global constraints).

- [ ] **Step 3: Run the full backend gate.**
  - Run: `make ci-backend` from the repo root, with the CI services that PR 3 added for Qdrant running locally.
  - Expected: exit 0. That covers lint, tenantscope, errquality, policycoverage, piiscan (with the baseline line from Task 4), logscope, tests with `-race`, build, and openapi-check (no route change, so no diff in `openapi/enterprise.json`).

- [ ] **Step 4: Commit.** Message: `test(retrieval): integration coverage for visibility, erasure veto and profile mismatch`, with the trailers.
