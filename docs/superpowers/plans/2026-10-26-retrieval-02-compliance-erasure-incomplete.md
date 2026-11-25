# Erasure requests stay `incomplete` until every producer is done — Implementation Plan (retrieval PR 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** amend ADR-0009 as ADR-0023 D12 decides: a right-to-erasure run is recorded per producer instance as done, pending or failed, a request is `completed` only when every producer is done, and anything else stays `incomplete` with durable, leased, bounded retries — for every producer, on every erasure path (operator workflow, self-service, retention). Two defects found on the way are fixed too: the right-of-access export loses one of the two `auth` producers' data, and an operator has no way to close a request stuck on a producer that is no longer installed.

**Architecture:** `iface.PurgeResult` gains `Pending`; an optional `iface.PIIProducerKeyer` gives producers registered twice under one subject (auth: operator and client tier) distinct, stable keys. `DSRService.EraseRemaining` runs only the producers not yet done and returns merged per-producer progress. Every erasure path goes through `ErasureRequestService`, which writes a `compliance_erasure_requests` row first, runs under a lease, and settles the row as `completed` or `incomplete`; an `ErasureRetryWorker` started by compliance `Start` re-claims due `incomplete` rows (lease, exponential backoff, 20 attempts) and re-runs the remaining producers. The DSR export keys its bundle by the same producer key, so both auth tiers are exported. An operator closes an `incomplete` request as `closed_partial` only when every producer it still waits for is no longer registered in the binary, with a mandatory note and an audit event. The admin list shows `pending` and `incomplete` rows; the console labels, retries and (where allowed) closes them.

**Tech Stack:** Go 1.26.10, Huma v2, MongoDB 8.0 (`mongo-driver v1.17.7`), React 19 + RTK Query + Vitest/MSW console.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D12, bullet "Erasure requests complete only when every producer is done (amends ADR-0009)") and the roadmap `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (row 2, "SDK — `PurgeResult` (PR 2)").

## Global Constraints

Everything in the roadmap's **Global constraints** section applies. Specific to this PR:

- Branch `feat/retrieval-02-compliance-erasure-incomplete`, created from `docs/adr-0023-core-retrieval` before Task 1. The PR merges only after ADR-0023 is Accepted.
- Every commit ends with the two trailer lines `Prop: upstream` and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- `PurgeResult` and `models.ErasureRequestIncomplete` exactly as the roadmap's contract shows. No method is added to `iface.PIIProducer`; the per-instance key is the new optional interface `iface.PIIProducerKeyer`.
- Lifecycle values (exact strings): request status `pending | incomplete | completed | rejected | closed_partial`; producer outcome `done | pending | failed`; request source `request | self_service | retention`; run-level `lastError` `legal_hold_active | run_failed`.
- Retry values: run lease `5m` (BSON-truncated to the millisecond, as `llm/repository/outbox.go` `LeaseDue` does); a run's deadline is half the lease; worker tick `1m`, at most `50` requests per pass; backoff `1m << (attempts-1)` capped at `6h`; at most `20` attempts, after which `nextAttemptAt` is cleared and the row stays `incomplete` until an operator retries it. Never completed by exhaustion.
- A producer's error text is **logged** (WARN, with subject, key and userUuid as today) and returned in the run's `errors` map exactly as today; it is **never persisted** on the request row, which is retained compliance evidence and must not capture personal data a driver error might quote.
- Cross-tenant queries on `compliance_erasure_requests` carry `//tenantscope:allow system: <reason>` (worker claim) or `//tenantscope:allow dsr: <reason>` (per-subject lookups).
- Producer key rule, one implementation (`producerKeys`) for erase **and** export: `PIIProducerKeyer.ProducerKey()` when non-empty, else `Subject()`; the second and later producers with the same key get `#2`, `#3` in registration order. The export bundle is keyed by it (metadata `version` becomes `"2"`); `Subject()` stays the audit label.
- New HTTP errors use `internal/shared/errcode` (`compliance.erasure_request_busy`, `compliance.erasure_request_started`, `compliance.erasure_request_not_closable`, all 409; `compliance.erasure_close_note_required`, 422) with a row in `codes_test.go` `goldenCodes`; existing 404/409 `huma.Error*` responses stay as they are.
- `backend/openapi/enterprise.json` is regenerated with `cd backend && make openapi-dump` (needs Mongo + Redis from `docker/docker-compose.infra.yml`) whenever a Huma-visible type changes, and committed in the same commit.

## Review Focus

1. **Two producers registered under the same `Subject()`** (auth registers an operator-tier and a client-tier producer, both `"auth"`): each is tracked, retried and reported on its own, one failing is never hidden by the other succeeding, and both appear in the same export. Pinned in Task 2 (`TestEraseTracksProducersWithTheSameSubjectSeparately`, `TestExportIncludesBothAuthTiers`).
2. **A producer that fails half-way and returns a partial `PurgeResult` with its error** (the `llm` producer does): the partial counts are kept, the producer is `failed`, the retry calls it again and the counts accumulate. Pinned in Task 2 (`TestEraseKeepsPartialResultOfFailedProducer`).
3. **The process dies between starting an erasure and recording its outcome** (crash, deploy, Mongo blip at settle): the row is already `incomplete` under a lease; once the lease expires the worker re-runs only the producers not recorded done and completes it. Pinned in Task 4 (`TestRetryDueReclaimsExpiredLeaseAndRunsOnlyRemaining`).
4. **A producer that stays pending or failing past every retry**: after 20 attempts the row is still `incomplete`, has no `nextAttemptAt`, is listed to operators, and an operator retry starts a fresh attempt budget. Pinned in Task 4 (`TestRetryDueStopsAtMaxAttemptsAndStaysIncomplete`, `TestExecuteIncompleteResetsAttempts`).
5. **A legal hold placed after an erasure started**: no producer runs, the row stays `incomplete` with `lastError: legal_hold_active` and counts the attempt; it is never completed or rejected by the worker. Pinned in Task 4 (`TestRetryDueUnderLegalHoldStaysIncomplete`).

---

### Task 1: SDK — `PurgeResult.Pending`, the idempotency contract and `PIIProducerKeyer`; auth keys its two producers

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go:1370-1375` (`PersonalDataBundle` doc), `:1395-1425` (`PurgeResult`, `PIIProducer` doc comments including `Subject()` at L1408-1411; add `PIIProducerKeyer` after `PIIProducer`)
- Create: `backend/pkg/sdk/iface/pii_producer_test.go`
- Modify: `backend/internal/core/auth/services/pii_producer.go:21-67` (key field, `NewPIIProducer` first parameter, `ProducerKey`)
- Modify: `backend/internal/core/auth/module.go:1574-1591` (pass the keys)
- Create: `backend/internal/core/auth/services/pii_producer_key_test.go`
- Modify: `docs/site/sdk/shared-iface.mdx:29` (producer list gains `llm`; new paragraph below the table)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `iface.PurgeResult{RowsDeleted, RowsAnonymized int; Collections []string; Pending bool \`json:"pending,omitempty"\`}` — field and doc comment verbatim from the roadmap.
  - `type PIIProducerKeyer interface { ProducerKey() string }` in package `iface`.
  - auth: `const PIIProducerKeyOperator = "auth.operator"`, `const PIIProducerKeyClient = "auth.client"`; `func NewPIIProducer(key string, refresh …, mfaCollection string) iface.PIIProducer` (new first parameter, rest unchanged); the returned value implements `iface.PIIProducerKeyer`.

- [ ] **Step 1: Write the failing tests**

```go
// backend/pkg/sdk/iface/pii_producer_test.go
func TestPurgeResultPendingIsOmittedWhenFalse(t *testing.T) {
	b, _ := json.Marshal(PurgeResult{RowsDeleted: 1})
	if strings.Contains(string(b), "pending") { t.Fatalf("got %s; pending must be omitted when false", b) }
	b, _ = json.Marshal(PurgeResult{Pending: true})
	if !strings.Contains(string(b), `"pending":true`) { t.Fatalf("got %s", b) }
}

// backend/internal/core/auth/services/pii_producer_key_test.go
func TestPIIProducerDeclaresItsKey(t *testing.T) {
	p := NewPIIProducer(PIIProducerKeyClient, nil, nil, nil, nil, nil, nil, "", "", "", "")
	k, ok := p.(iface.PIIProducerKeyer)
	if !ok || k.ProducerKey() != "auth.client" { t.Fatalf("ProducerKey = %v, %v; want auth.client", k, ok) }
	if p.Subject() != "auth" { t.Fatalf("Subject must stay %q (audit rows and export bundles reference it)", "auth") }
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && go test ./pkg/sdk/iface/ -run TestPurgeResultPending && go test ./internal/core/auth/services/ -run TestPIIProducerDeclaresItsKey`
Expected: FAIL — `unknown field Pending`, `undefined: PIIProducerKeyClient`.

- [ ] **Step 3: Implement**

`iface`: add the field; extend the `PurgePersonalData` doc comment with the producer contract, in these words: *"Idempotent: compliance calls it again for the same user and mode until the producer reports its purge done, and a call after a completed purge must succeed and report what it found. Return Pending true with a nil error when the erasure is accepted but not finished (the request stays incomplete and the producer is called again). On failure return the error together with whatever was erased before it; compliance keeps those counts."* Add `PIIProducerKeyer` with this doc: *"Optional. A producer registered more than once under the same Subject (one per tier, say) implements it so compliance can track each instance's erasure on its own. The key must be unique in the registry and stable across restarts: incomplete erasure requests store it. Without it compliance keys the producer by Subject, suffixing later duplicates `#2`, `#3` in registration order. The key is also the producer's key in the export bundle."*
Rewrite the `Subject()` doc, which today says the subject is the bundle key and must never change, to: *"Subject names the kind of data the producer holds; DSR audit rows carry it by value, so it must not change after first use. It is not the export bundle key: compliance keys the bundle by the producer key (PIIProducerKeyer, else Subject), so two producers sharing a Subject are both exported — a right-of-access answer (GDPR Art. 15) must be complete."* `PersonalDataBundle`'s doc: keys are producer keys (`"user"`, `"auth.operator"`, `"auth.client"`, …) plus `"metadata"`.
auth: store `key` on `piiProducer`; `ProducerKey()` returns it; `module.go` passes `services.PIIProducerKeyOperator` to the operator-tier call (L1579) and `services.PIIProducerKeyClient` to the client-tier call (L1585).
`shared-iface.mdx`: add `llm` to the `PIIProducer` row; under the table, one paragraph stating the idempotency contract, `Pending`, partial results with an error, `PIIProducerKeyer`, and that export bundles are keyed by producer key (bundle `version` `"2"`).

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./pkg/sdk/iface/ ./internal/core/auth/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/sdk/iface backend/internal/core/auth docs/site/sdk/shared-iface.mdx
git commit -F - <<'EOF'
feat(sdk): PurgeResult.Pending, idempotent purge contract, PIIProducerKeyer

ADR-0023 D12. Auth keys its operator and client producers so compliance
can track each one.

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 2: Per-producer outcome in `DSRService` and the request model

**Files:**
- Modify: `backend/internal/core/compliance/models/erasure_request.go`
- Modify: `backend/internal/core/compliance/services/dsr.go:54-193` (Export and Erase)
- Test: `backend/internal/core/compliance/services/dsr_test.go` (add tests; the existing `res.Purged["user"]`/`["auth"]` and `res.Bundle["user"]`/`["auth"]` assertions keep passing because unkeyed unique subjects keep their subject as key)

Export-key consumers, checked by grep: the only readers of bundle keys are `services/dsr_test.go:85-148` and `handlers/me_handler_test.go:104` (unique subjects, unchanged). `frontend-client/src/pages/AccountDsrPage.tsx:18-27` downloads the bundle as an opaque JSON file and `frontend-client/src/api/dsr.ts:30` types it `unknown`; `frontend-admin` does not call the export; the OpenAPI `ExportOutputBody.bundle` is a free-form object, so the spec changes only in the export operation's description (Task 7). No other consumer needs a change.

**Interfaces:**
- Consumes: Task 1 `iface.PurgeResult.Pending`, `iface.PIIProducerKeyer`.
- Produces (models):

```go
const ErasureRequestIncomplete    = "incomplete"     // next to Pending/Completed/Rejected
const ErasureRequestClosedPartial = "closed_partial" // closed by an operator; Abandoned lists the producers never done

const (
	ErasureSourceRequest     = "request"      // lodged by the subject, executed by an operator (absent on legacy rows)
	ErasureSourceSelfService = "self_service" // POST /v1/me/dsr/erase
	ErasureSourceRetention   = "retention"    // retention auto-cleanup
)

const (
	ProducerOutcomeDone    = "done"
	ProducerOutcomePending = "pending"
	ProducerOutcomeFailed  = "failed"
)

const (
	ErasureLastErrorLegalHold = "legal_hold_active"
	ErasureLastErrorRun       = "run_failed"
)

// ErasureProducerProgress is one producer instance's erasure state on a request.
type ErasureProducerProgress struct {
	Key            string    `bson:"key" json:"key"`         // producer key (PIIProducerKeyer, else Subject, #n for duplicates)
	Subject        string    `bson:"subject" json:"subject"`
	Outcome        string    `bson:"outcome" json:"outcome" enum:"done,pending,failed"`
	RowsDeleted    int       `bson:"rowsDeleted" json:"rowsDeleted"`       // cumulative over attempts
	RowsAnonymized int       `bson:"rowsAnonymized" json:"rowsAnonymized"` // cumulative over attempts
	Attempts       int       `bson:"attempts" json:"attempts"`
	UpdatedAt      time.Time `bson:"updatedAt" json:"updatedAt"`
}

// added to ErasureRequest
	Source        string                    `bson:"source,omitempty" json:"source,omitempty"`
	Progress      []ErasureProducerProgress `bson:"progress,omitempty" json:"progress,omitempty"`
	Attempts      int                       `bson:"attempts,omitempty" json:"attempts,omitempty"`
	NextAttemptAt *time.Time                `bson:"nextAttemptAt,omitempty" json:"nextAttemptAt,omitempty"` // nil on an incomplete row: retries exhausted
	LeaseUntil    *time.Time                `bson:"leaseUntil,omitempty" json:"-"`
	LastError     string                    `bson:"lastError,omitempty" json:"lastError,omitempty"`
	Abandoned     []string                  `bson:"abandoned,omitempty" json:"abandoned,omitempty"` // closed_partial: producer keys never done
	Closable      bool                      `bson:"-" json:"closable,omitempty"`                   // response-only, filled by ListOpen
```

- Produces (services):
  - `func producerKeys(ps []iface.PIIProducer) []string` — key per producer, same order.
  - `type EraseResult struct { Purged map[string]iface.PurgeResult; Errors map[string]string; Progress []models.ErasureProducerProgress }` — `Purged`/`Errors` hold this run only, keyed by producer key, and `Purged` includes the partial result of a failed producer; `Progress` is merged with the prior progress: registered producers in registration order, then prior keys no longer registered.
  - `func (r *EraseResult) Complete() bool` — true iff every `Progress` entry is `done`.
  - `func (s *DSRService) CheckLegalHold(ctx context.Context, userUUID string) error` — `ErrLegalHoldActive`, the checker's error, or nil.
  - `func (s *DSRService) EraseRemaining(ctx context.Context, userUUID string, mode iface.EraseMode, prior []models.ErasureProducerProgress) (*EraseResult, error)`
  - `Erase(ctx, userUUID, mode)` keeps its signature and returns `EraseRemaining(ctx, userUUID, mode, nil)`.
  - `Export` keys `Bundle`, `Producers` and `Errors` by `producerKeys` (no signature change); bundle `metadata.version` becomes `"2"`.
  - `func (s *DSRService) RegisteredKeys() map[string]bool` — the producer keys of the current registry.
  - `func parseEraseMode(label string) iface.EraseMode` — inverse of `eraseModeLabel`; `"anonymize"` → `EraseAnonymize`, anything else → `EraseHardDelete`.

- [ ] **Step 1: Write the failing tests** (in `dsr_test.go`; extend `stubProducer` with an optional `key string` — when set the stub implements `ProducerKey()` through a wrapper type `keyedStub{*stubProducer}` — and a `purgeResults []iface.PurgeResult` / `purgeErrs []error` sequence per call)

```go
func TestEraseTracksProducersWithTheSameSubjectSeparately(t *testing.T) {
	op := &stubProducer{subject: "auth", purgeResult: iface.PurgeResult{RowsDeleted: 2}}
	cl := &stubProducer{subject: "auth", purgeResult: iface.PurgeResult{RowsDeleted: 1}, purgeErr: errors.New("mongo down")}
	svc, _ := newDSRService(op, cl)
	res, err := svc.Erase(ctx, "u-1", iface.EraseHardDelete)
	// err == nil
	// keys: res.Progress[0].Key == "auth", res.Progress[1].Key == "auth#2"
	// res.Progress[0].Outcome == "done", res.Progress[1].Outcome == "failed"
	// res.Purged["auth"].RowsDeleted == 2, res.Purged["auth#2"].RowsDeleted == 1, res.Errors["auth#2"] != "", res.Complete() == false
}

func TestEraseUsesProducerKeyWhenDeclared(t *testing.T) {
	// two keyed stubs, subject "auth", keys "auth.operator" and "auth.client"
	// res.Progress keys == ["auth.operator", "auth.client"]; both Subject == "auth"
}

func TestEraseKeepsPartialResultOfFailedProducer(t *testing.T) {
	llm := &stubProducer{subject: "llm",
		purgeResults: []iface.PurgeResult{{RowsDeleted: 3}, {RowsDeleted: 1}},
		purgeErrs:    []error{errors.New("usage anonymize failed"), nil}}
	user := &stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}}
	svc, _ := newDSRService(user, llm)
	first, _ := svc.Erase(ctx, "u-1", iface.EraseHardDelete)
	// first.Purged["llm"].RowsDeleted == 3; first.Progress[1] == {Key:"llm", Outcome:"failed", RowsDeleted:3, Attempts:1}
	second, _ := svc.EraseRemaining(ctx, "u-1", iface.EraseHardDelete, first.Progress)
	// user.purgeCalls == 1 (done producers are skipped); llm.purgeCalls == 2
	// second.Progress[1] == {Key:"llm", Outcome:"done", RowsDeleted:4, Attempts:2}; second.Complete() == true
}

func TestErasePendingProducerIsNotDone(t *testing.T) {
	// stub "retrieval" returns PurgeResult{Pending: true}, nil
	// res.Progress[0].Outcome == "pending"; res.Complete() == false; res.Errors is empty
}

func TestEraseRemainingReportsPriorKeyNoLongerRegistered(t *testing.T) {
	prior := []models.ErasureProducerProgress{{Key: "gone", Subject: "gone", Outcome: "failed", Attempts: 1}}
	// registry has only "user" (succeeds)
	// res.Progress == [user done, gone failed (Attempts unchanged: 1)]; res.Complete() == false
}

func TestEraseAuditRecordsOutcomes(t *testing.T) {
	// one done ("user"), one pending ("retrieval")
	// sink.events[0].Metadata["complete"] == false, ["pendingCount"] == 1, ["errorCount"] == 0
	// perSubject := Metadata["perSubject"].(map[string]any); perSubject["retrieval"].(map[string]any)["outcome"] == "pending"
}

func TestCheckLegalHold(t *testing.T) {
	// held → errors.Is(err, ErrLegalHoldActive); checker error → that error; no checker → nil
}

func TestExportIncludesBothAuthTiers(t *testing.T) {
	// keyed stubs, both Subject "auth": key "auth.operator" exports {"sessions": ["op-s1"]}, key "auth.client" exports {"sessions": ["cl-s1"]}
	// res.Bundle["auth.operator"] and res.Bundle["auth.client"] both present with their own data; res.Bundle["auth"] absent
	// res.Producers == ["auth.operator", "auth.client"]; res.Bundle["metadata"].(map[string]any)["version"] == "2"
}

func TestExportKeysUnkeyedDuplicatesWithSuffix(t *testing.T) {
	// two unkeyed stubs Subject "auth" with different data → Bundle["auth"] and Bundle["auth#2"]; an export error of the 2nd → Errors["auth#2"]
}

func TestRegisteredKeys(t *testing.T) {
	// user + keyed auth.operator + keyed auth.client → map{"user", "auth.operator", "auth.client"} all true, len 3
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/compliance/services/ -run 'TestErase|TestCheckLegalHold|TestExport|TestRegisteredKeys'`
Expected: FAIL — `undefined: models.ErasureProducerProgress`, `svc.EraseRemaining undefined`.

- [ ] **Step 3: Implement** the models and the services symbols above. `Export` uses the same `producerKeys` walk as `EraseRemaining`; the audit row `gdpr.dsr.exported` lists producer keys.

`EraseRemaining`: hold gate as today (`CheckLegalHold`); walk `registry.List()` with `producerKeys`; skip a key whose prior outcome is `done` (carry the prior entry unchanged); otherwise call `PurgePersonalData`, set the outcome (`err != nil` → `failed`, else `res.Pending` → `pending`, else `done`), add this call's counts to the prior counts, `Attempts = prior + 1`, `UpdatedAt = now UTC`; put `res` into `Purged` even when `err != nil`. Append prior entries whose key is not registered, unchanged. Audit `gdpr.dsr.erased` once per run: `perSubject` keyed by producer key with `subject`, `outcome`, `rowsDeleted`, `rowsAnonymized`, `collections`, `pending`; plus `totalRows`, `errorCount`, `pendingCount`, `complete` (`Complete()` of the merged progress) and `mode`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/core/compliance/...`
Expected: PASS (all existing DSR, retention and handler tests included).

- [ ] **Step 5: Commit** — `feat(compliance): per-producer erasure outcome and export keyed by producer` with the two trailers; the body names the fixed Art. 15 defect (client-tier auth export overwrote the operator-tier one).

---

### Task 3: Leased persistence for erasure runs

**Files:**
- Modify: `backend/internal/core/compliance/repository/erasure_request_repo.go`
- Modify: `backend/internal/core/compliance/module.go:210-216` (indexes)
- Create: `backend/internal/core/compliance/services/erasuretest/memrepo.go`, `memrepo_test.go`
- Test: `backend/internal/core/compliance/repository/repository_integration_test.go` (Mongo, gated by `MONGO_TEST_URI` through the existing `newTestDB`)

**Interfaces:**
- Consumes: Task 2 models.
- Produces (package `repository`):

```go
var ErrErasureRequestBusy = errors.New("compliance: an erasure run holds this request")
var ErrErasureLeaseLost  = errors.New("compliance: the erasure run lease was taken over")

// ErasureSettlement is the outcome of one leased run.
type ErasureSettlement struct {
	Status        string                           // models.ErasureRequestCompleted or models.ErasureRequestIncomplete
	Progress      []models.ErasureProducerProgress
	Attempts      int
	NextAttemptAt *time.Time // nil: no automatic retry
	LastError     string     // "" or models.ErasureLastError*
	ResolvedAt    *time.Time // set with Status completed
}

func (r *ErasureRequestRepository) ListByStatus(ctx context.Context, statuses ...string) ([]models.ErasureRequest, error) // newest requestedAt first; no statuses = all
func (r *ErasureRequestRepository) Claim(ctx context.Context, uuid, fromStatus, mode, actor string, now, leaseUntil time.Time) (*models.ErasureRequest, error)
func (r *ErasureRequestRepository) ClaimDue(ctx context.Context, now, leaseUntil time.Time) (*models.ErasureRequest, error) // nil, nil when nothing is due
func (r *ErasureRequestRepository) Settle(ctx context.Context, uuid string, leaseUntil time.Time, s ErasureSettlement) error
func (r *ErasureRequestRepository) HasIncomplete(ctx context.Context, userUUID string) (bool, error)
```

  `Claim` matches `{uuid, status: fromStatus, lease free}` (lease free = `leaseUntil` absent, null or `< now`) and sets `leaseUntil`, `attempts: 0`, `lastError: ""`; when `fromStatus` is `pending` it also sets `status: incomplete`, `mode`, `resolvedBy: actor` (for `incomplete` the stored mode and resolver stay). It returns the document after the update. On a miss it reads the row: same `fromStatus` → `ErrErasureRequestBusy`, otherwise `ErrErasureRequestNotFound`. `ClaimDue` matches `{status: incomplete, nextAttemptAt <= now, lease free}`, sorted by `nextAttemptAt` ascending. `Settle` matches `{uuid, leaseUntil: <exactly the claimed value>}`, `$set`s the settlement fields and unsets `leaseUntil`; a miss is `ErrErasureLeaseLost`. `Resolve` keeps its pending-only compare-and-set and is used by `Reject` only.
- Indexes (module.go `ErasureRequestsCollection`): add `{status: 1, nextAttemptAt: 1}` and `{userUuid: 1, status: 1}`.
- `erasuretest.MemRepo` (package `erasuretest`, "never used in production", like `policytest`): `func NewMemRepo() *MemRepo`; the six methods above plus `Insert`, `Get`, `Resolve` with the same semantics and sentinels; `func (m *MemRepo) Put(req models.ErasureRequest)`, `func (m *MemRepo) Row(uuid string) models.ErasureRequest`, `func (m *MemRepo) FailNextSettle(err error)`.

- [ ] **Step 1: Write the failing integration tests**

```go
func TestErasureListOpenNewestFirst(t *testing.T) {
	// insert pending r-old (requestedAt t0), incomplete r-new (t0+1h), completed r-done
	// ListByStatus(ctx, "pending", "incomplete") → UUIDs ["r-new", "r-old"]
}

func TestErasureClaimFromPendingSetsModeAndLease(t *testing.T) {
	// pending r-1 → Claim(ctx, "r-1", "pending", "anonymize", "admin-1", now, now+5m)
	// got.Status == "incomplete", got.Mode == "anonymize", got.ResolvedBy == "admin-1", *got.LeaseUntil == (now+5m).Truncate(time.Millisecond)
	// second Claim(ctx, "r-1", "incomplete", "", "admin-2", now, now+5m) → ErrErasureRequestBusy
	// Claim(ctx, "r-1", "pending", …) → ErrErasureRequestNotFound
}

func TestErasureClaimDueSkipsLiveLeaseAndFutureAttempts(t *testing.T) {
	// incomplete a: nextAttemptAt now-1m, no lease → claimed first
	// incomplete b: nextAttemptAt now-2m, leaseUntil now+1m → skipped
	// incomplete c: nextAttemptAt now+1m → skipped
	// incomplete d: nextAttemptAt nil (exhausted) → skipped
	// ClaimDue twice → "a", then nil
	// with now' = now+2m: ClaimDue → "b" (expired lease is reclaimable)
}

func TestErasureSettleIsFencedByLease(t *testing.T) {
	// claim at lease L1; ClaimDue after L1 expires → lease L2
	// Settle(ctx, uuid, L1, completed) → ErrErasureLeaseLost and the row is unchanged
	// Settle(ctx, uuid, L2, {Status: completed, ResolvedAt: &now}) → nil; row.Status == "completed"; row.LeaseUntil == nil
}

func TestErasureHasIncomplete(t *testing.T) {
	// u-1 has an incomplete row → true; u-2 only a completed row → false
}
```

  And in `memrepo_test.go` the same five scenarios run against `MemRepo` (one table-free test per scenario, same names with the `Mem` prefix), so the fake cannot drift from Mongo.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && MONGO_TEST_URI=mongodb://admin:<pw>@localhost:27017 go test ./internal/core/compliance/repository/ ./internal/core/compliance/services/erasuretest/ -run 'Erasure|Mem'`
Expected: FAIL — `r.Claim undefined`, `undefined: NewMemRepo`. (Without `MONGO_TEST_URI` the repository tests skip; a green run without it proves nothing about Mongo.)

- [ ] **Step 3: Implement** the repository methods, indexes and `MemRepo` as specified. Truncate every lease to the millisecond before writing it and before using it as a fence. Annotate `ClaimDue` `//tenantscope:allow system: erasure retry worker claims incomplete requests of every tenant`, `HasIncomplete` `//tenantscope:allow dsr: incomplete erasure lookup by data subject across tenants`, `Claim`/`Settle` like the existing `Resolve`.

- [ ] **Step 4: Run to verify they pass** (same command). Expected: PASS.

- [ ] **Step 5: Commit** — `feat(compliance): leased claim and settle for erasure requests` with the two trailers.

---

### Task 4: `ErasureRequestService` — one leased run for every erasure path

**Files:**
- Modify: `backend/internal/core/compliance/services/erasure_request.go`
- Modify (build only): `backend/internal/core/compliance/module.go:330`, `handlers/erasure_request_handler.go:59-100`, `handlers/erasure_request_handler_test.go:13-34`
- Test: `backend/internal/core/compliance/services/erasure_request_test.go` (replace `fakeErasureRepo` with `erasuretest.MemRepo`; existing tests keep their intent)

**Interfaces:**
- Consumes: Task 2 `EraseRemaining`, `CheckLegalHold`, `parseEraseMode`, `EraseResult.Complete`; Task 3 repository methods, `ErasureSettlement`, sentinels.
- Produces:

```go
// erasureRequestRepo gains Claim, ClaimDue, Settle, HasIncomplete; ListByStatus becomes variadic.

var ErrErasureRequestStarted = errors.New("compliance: the erasure has started; it can be retried, not rejected")

const ErasureRetryMaxAttempts = 20
const erasureRunLease = 5 * time.Minute
func erasureRetryBackoff(attempts int) time.Duration // 1m << (attempts-1), capped at 6h

type ErasureOutcome struct {
	Request *models.ErasureRequest // the row as settled
	Run     *EraseResult           // what this run did
}

func NewErasureRequestService(repo erasureRequestRepo, dsr *DSRService, sink iface.AuditSink, logger *slog.Logger) *ErasureRequestService // struct gains sink (nil-safe, used by Task 6), logger, now func() time.Time (default UTC now)
func (s *ErasureRequestService) ListOpen(ctx context.Context) ([]models.ErasureRequest, error) // pending + incomplete; replaces ListPending
func (s *ErasureRequestService) Execute(ctx context.Context, requestUUID string, mode iface.EraseMode, resolvedBy string) (*ErasureOutcome, error)
func (s *ErasureRequestService) Reject(ctx context.Context, requestUUID, resolvedBy, note string) error
func (s *ErasureRequestService) EraseNow(ctx context.Context, userUUID, tenantID, source, actor string, mode iface.EraseMode) (*ErasureOutcome, error)
func (s *ErasureRequestService) RetryDue(ctx context.Context) (bool, error) // false when nothing was due
func (s *ErasureRequestService) HasIncomplete(ctx context.Context, userUUID string) (bool, error)
```

Behaviour the tests pin:
- `Execute`: `Get`; `completed`/`rejected` → `repository.ErrErasureRequestNotFound` (404 as today); `CheckLegalHold` **before** claiming, so a held subject leaves the row untouched; `Claim(from = row.Status)` — a `pending` row gets the requested mode, an `incomplete` row keeps its stored mode and its attempt budget restarts; then the shared run (below). A run error is returned after settling.
- `Reject`: an `incomplete` row → `ErrErasureRequestStarted`; otherwise `Resolve` as today.
- `EraseNow`: `CheckLegalHold`; `Insert` a row `{Status: incomplete, Source: source, Mode: label, ResolvedBy: actor, TenantID, LeaseUntil: now+lease, NextAttemptAt: now+lease}` **before** any producer runs (an insert failure returns the error and erases nothing); then the shared run.
- `RetryDue`: `ClaimDue(now, now+lease)`; nil → `false, nil`; otherwise the shared run; run errors are recorded on the row and logged, not returned.
- Shared run (unexported): `EraseRemaining(context.WithTimeout(context.WithoutCancel(ctx), lease/2), row.UserUUID, parseEraseMode(row.Mode), row.Progress)`; `attempts = row.Attempts + 1`; settlement: complete → `completed`, `ResolvedAt = now`, no `NextAttemptAt`; not complete → `incomplete` with the merged progress; `ErrLegalHoldActive` → `incomplete`, progress unchanged, `LastError = legal_hold_active`; any other error → same with `run_failed`; an `incomplete` settlement gets `NextAttemptAt = now + erasureRetryBackoff(attempts)` unless `attempts >= ErasureRetryMaxAttempts` (then nil, and an ERROR log `"compliance: erasure request needs an operator"` with the request UUID and attempts). Settle under `context.WithoutCancel(ctx)`; `ErrErasureLeaseLost` → INFO log, the newer holder's state stands; any other settle error → WARN log (the lease expires and the worker re-runs the remaining producers). Neither settle error is returned to the caller.

- [ ] **Step 1: Write the failing tests**

```go
func TestErasureRetryBackoff(t *testing.T) {
	// 1→1m, 2→2m, 9→256m, 10→6h, 20→6h
}

func TestExecuteCompletesWhenEveryProducerDone(t *testing.T) {
	// pending r-1, producers user + auth done → out.Request.Status == "completed", Row("r-1").ResolvedAt != nil,
	// Row.ResolvedBy == "admin-1", Row.Mode == "hard_delete", len(Row.Progress) == 2, Row.LeaseUntil == nil
}

func TestExecutePendingProducerLeavesIncomplete(t *testing.T) {
	// user done, retrieval {Pending:true} → Status "incomplete", Attempts 1, *NextAttemptAt == t0+1m, LastError ""
}

func TestExecutePendingUnderLegalHoldStaysPending(t *testing.T) {
	// hold → errors.Is(err, ErrLegalHoldActive); Row.Status == "pending"; producer.purgeCalls == 0
}

func TestExecuteIncompleteRunsOnlyRemainingWithStoredMode(t *testing.T) {
	// incomplete r-1, Mode "anonymize", Progress [user done, auth failed]; Execute(…, EraseHardDelete, "admin-2")
	// user.purgeCalls == 0; auth.purgeCalls == 1 and auth.lastMode == EraseAnonymize; Status "completed"
}

func TestExecuteIncompleteResetsAttempts(t *testing.T) {
	// incomplete r-1, Attempts 20, NextAttemptAt nil, producer still failing → after Execute: Attempts 1, NextAttemptAt == t0+1m
}

func TestExecuteBusyWhileLeaseHeld(t *testing.T) {
	// incomplete r-1 with LeaseUntil t0+1m → errors.Is(err, repository.ErrErasureRequestBusy); no producer called
}

func TestRejectIncompleteIsRefused(t *testing.T) {
	// incomplete r-1 → errors.Is(err, ErrErasureRequestStarted); Row.Status == "incomplete"
}

func TestListOpenReturnsPendingAndIncomplete(t *testing.T) {
	// pending, incomplete, completed, rejected rows → 2 items
}

func TestEraseNowRecordsTheRunBeforeProducers(t *testing.T) {
	// a producer whose PurgePersonalData asserts that the repo already holds an incomplete row
	// for the user with Source "self_service" and a live lease; final Status "completed", Source "self_service", ResolvedBy "u-1"
}

func TestEraseNowUnderLegalHoldRecordsNothing(t *testing.T) {
	// hold → ErrLegalHoldActive; repo has no row
}

func TestEraseNowSettleFailureIsRecoveredByRetry(t *testing.T) {
	// repo.FailNextSettle(errors.New("mongo blip")); producer auth fails on 1st call, succeeds on 2nd
	// EraseNow returns no error and out.Run.Errors["auth"] != ""; Row.Status "incomplete" with LeaseUntil still set
	// RetryDue at t0 → false (lease live); RetryDue at t0+lease+1s → true; Status "completed"
}

func TestRetryDueReclaimsExpiredLeaseAndRunsOnlyRemaining(t *testing.T) {
	// incomplete r-1, LeaseUntil t0-1s, NextAttemptAt t0-1s, Progress [user done, auth.client failed]
	// RetryDue → true; user.purgeCalls == 0, client.purgeCalls == 1; Status "completed"
}

func TestRetryDueStopsAtMaxAttemptsAndStaysIncomplete(t *testing.T) {
	// incomplete r-1, Attempts 19, NextAttemptAt t0-1s, producer always Pending
	// RetryDue → true; Row.Status "incomplete", Attempts 20, NextAttemptAt nil, Completed never set
	// RetryDue again → false; ListOpen contains r-1
}

func TestRetryDueUnderLegalHoldStaysIncomplete(t *testing.T) {
	// incomplete r-1, Attempts 2, hold active → RetryDue true, no producer call,
	// Row.Status "incomplete", LastError "legal_hold_active", Attempts 3, *NextAttemptAt == t0+4m, Progress unchanged
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/compliance/services/ -run 'Execute|Reject|ListOpen|EraseNow|RetryDue|ErasureRetryBackoff|Lodge'`
Expected: FAIL — `svc.EraseNow undefined`, `undefined: ErasureRetryMaxAttempts`.

- [ ] **Step 3: Implement** the service as specified. `stubProducer` gains `lastMode iface.EraseMode` and an optional `onPurge func()` hook for `TestEraseNowRecordsTheRunBeforeProducers`. Keep the build green in this commit with the smallest caller changes: `module.go:330` passes `sink` and `deps.Logger`; `ErasureRequestHandler.ListPending` calls `ListOpen`; `ErasureRequestHandler.Execute` reads `out.Run.Purged`; `handlers/erasure_request_handler_test.go` drops `fakeErasureReqRepo` for `erasuretest.MemRepo` (seeding with `Put`). The new HTTP fields come in Task 7.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go build ./... && go test -race ./internal/core/compliance/...`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(compliance): erasure requests stay incomplete until every producer is done` with the two trailers.

---

### Task 5: Retry worker in `Start`/`Stop`; retention leaves a durable record

**Files:**
- Create: `backend/internal/core/compliance/services/erasure_retry.go`, `erasure_retry_test.go`
- Modify: `backend/internal/core/compliance/services/retention.go:25-76`
- Modify: `backend/internal/core/compliance/services/retention_test.go`, `services/integration_test.go:107-146`
- Modify: `backend/internal/core/compliance/module.go:36-51` (field), `:308-332` (wiring), `:384-408` (Start/Stop)

**Interfaces:**
- Consumes: Task 4 `RetryDue`, `EraseNow`, `HasIncomplete`.
- Produces:

```go
type ErasureRetryWorker struct { /* svc, logger, interval (1m), batch (50) */ }
func NewErasureRetryWorker(svc *ErasureRequestService, logger *slog.Logger) *ErasureRetryWorker
func (w *ErasureRetryWorker) RunOnce(ctx context.Context) (int, error) // RetryDue until false or 50; returns runs handled; a RetryDue error ends the pass
func (w *ErasureRetryWorker) Loop(ctx context.Context, stop <-chan struct{}) // ticks every interval until stop or ctx done, like RetentionService.Loop

func NewRetentionService(db *mongo.Database, erasures *ErasureRequestService, logger *slog.Logger, loadConfig func() RetentionConfig) *RetentionService
```

Retention `RunOnce`: per expired id, `HasIncomplete` true → skip (debug log; the worker owns it); else `EraseNow(ctx, id, "", models.ErasureSourceRetention, "system:retention", iface.EraseHardDelete)`; `ErrLegalHoldActive` or another error → WARN and continue as today; the returned count is the number of runs settled `completed`. Module: build `erasureReqSvc` (with `deps.Logger`) before `m.me` and the retention service, pass it to both; `m.erasureRetry = services.NewErasureRetryWorker(erasureReqSvc, deps.Logger)`; `Start` adds `go m.erasureRetry.Loop(ctx, m.stopCh)` when non-nil; `Stop` is unchanged (closing `stopCh` stops it); update the `Start`/`Stop` doc comments.

- [ ] **Step 1: Write the failing tests**

```go
func TestErasureRetryWorkerRunOnceDrainsDueRequests(t *testing.T) {
	// MemRepo with 3 due incomplete rows whose remaining producer succeeds, 1 not yet due
	// n, err := w.RunOnce(ctx) → n == 3, err == nil; the 3 rows completed, the 4th untouched
}

func TestErasureRetryWorkerLoopReturnsOnStop(t *testing.T) {
	// interval 10ms; close(stop) → Loop returns within 1s
}

func TestRetentionSkipsSubjectWithIncompleteErasure(t *testing.T)   // services/integration_test.go (Mongo): u-a has an incomplete row → no new row for u-a
func TestRetentionPreviewAndRunOnce(t *testing.T)                   // existing; now builds an ErasureRequestService over the real repo
	// and additionally asserts two compliance_erasure_requests rows with Source "retention", Status "completed"
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/compliance/services/ -run 'ErasureRetryWorker|Retention'` and with `MONGO_TEST_URI` set for the integration ones.
Expected: FAIL — `undefined: NewErasureRetryWorker`; `NewRetentionService` argument mismatch.

- [ ] **Step 3: Implement** as specified.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go build ./... && go test -race ./internal/core/compliance/...` (and the integration tests with `MONGO_TEST_URI`).
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(compliance): durable retry worker for incomplete erasures` with the two trailers.

---

### Task 6: Close an incomplete request whose remaining producers are gone

An `incomplete` request whose progress waits on a producer that this binary no longer registers (a fork removed the module) can never complete. An operator may close it as `closed_partial` — only then, never while a not-done producer is still registered (those are retried, not closed).

**Files:**
- Modify: `backend/internal/core/compliance/repository/erasure_request_repo.go`, `services/erasuretest/memrepo.go`, `memrepo_test.go`
- Modify: `backend/internal/core/compliance/services/erasure_request.go`
- Test: `backend/internal/core/compliance/repository/repository_integration_test.go`, `services/erasure_request_test.go`

**Interfaces:**
- Consumes: Task 2 `RegisteredKeys`, `models.ErasureRequestClosedPartial`, `Abandoned`, `Closable`; Task 4 service; `NormalizeReason` (`services/policy_validation.go:123`) and `maskReason` (`services/policy_admin.go:160`) for the note, as policy reasons do.
- Produces:

```go
// repository — CAS on {uuid, status: incomplete, lease free}; sets status closed_partial, abandoned,
// resolvedBy, resolvedAt (now), resolutionNote; unsets nextAttemptAt. Miss: ErrErasureRequestBusy when the row
// is incomplete under a live lease, else ErrErasureRequestNotFound.
func (r *ErasureRequestRepository) ClosePartial(ctx context.Context, uuid string, now time.Time, abandoned []string, closedBy, note string) error

// services
var ErrErasureRequestNotClosable  = errors.New("compliance: the erasure request still waits on a registered producer")
var ErrErasureCloseNoteRequired   = errors.New("compliance: a note of 1 to 500 characters is required to close an erasure request")

// abandonedKeys returns the not-done progress keys absent from registered; ok is true when there is at least one
// and no not-done key is registered.
func abandonedKeys(progress []models.ErasureProducerProgress, registered map[string]bool) (keys []string, ok bool)

func (s *ErasureRequestService) ClosePartial(ctx context.Context, requestUUID, closedBy, note string) (*models.ErasureRequest, error)
```

Behaviour: the note is normalized (`NormalizeReason`; invalid → `ErrErasureCloseNoteRequired`) and masked (`maskReason`) before it is stored. `Get`; `completed`, `rejected`, `closed_partial` or missing → `repository.ErrErasureRequestNotFound`; `pending` → `ErrErasureRequestNotClosable`; `incomplete` with `abandonedKeys(...).ok == false` → `ErrErasureRequestNotClosable`; otherwise `ClosePartial`, then one audit event `{ActorUserID: closedBy, ActorType: "user", Action: "gdpr.dsr.erasure_closed_partial", ResourceType: "erasure_request", ResourceID: requestUUID, Outcome: "success", Metadata: {"subjectUserUuid": row.UserUUID, "abandoned": keys, "mode": row.Mode}}` (the note stays on the row, not in the audit metadata), and the row as stored is returned. `ListOpen` sets `Closable` on each `incomplete` row from `abandonedKeys(row.Progress, dsr.RegisteredKeys())`. The legal hold does not gate closing: closing erases nothing.

- [ ] **Step 1: Write the failing tests**

```go
// repository_integration_test.go (Mongo)
func TestErasureClosePartialOnlyIncompleteAndLeaseFree(t *testing.T) {
	// incomplete r-1 no lease → ClosePartial(ctx, "r-1", now, []string{"gone"}, "admin-1", "module removed") == nil;
	//   row.Status == "closed_partial", row.Abandoned == ["gone"], row.ResolvedBy == "admin-1", row.ResolutionNote == "module removed", row.NextAttemptAt == nil
	// incomplete r-2 with LeaseUntil now+1m → ErrErasureRequestBusy; pending r-3 → ErrErasureRequestNotFound
}
// memrepo_test.go: TestMemErasureClosePartialOnlyIncompleteAndLeaseFree, same assertions.

// erasure_request_test.go
func TestAbandonedKeys(t *testing.T) {
	// progress [user done, gone failed, auth.client pending], registered {user, auth.client} → keys ["gone"], ok false
	// progress [user done, gone failed], registered {user} → ["gone"], true
	// progress [user done], registered {user} → [], false
}
func TestClosePartialRecordsAbandonedProducersAndAudits(t *testing.T) {
	// registry: user; incomplete r-1 progress [user done, gone failed]
	// ClosePartial(ctx, "r-1", "admin-1", "  module removed in v2  ") → row.Status "closed_partial", Abandoned ["gone"], ResolutionNote "module removed in v2"
	// sink.events has one "gdpr.dsr.erasure_closed_partial" with ResourceID "r-1", Metadata["abandoned"] == []string{"gone"}, no "note" key
}
func TestClosePartialRefusedWhileARegisteredProducerIsNotDone(t *testing.T) {
	// registry: user, auth.client; progress [user done, gone failed, auth.client failed] → errors.Is(err, ErrErasureRequestNotClosable); row unchanged; no audit
}
func TestClosePartialRefusesPendingAndResolved(t *testing.T) {
	// pending → ErrErasureRequestNotClosable; completed → repository.ErrErasureRequestNotFound
}
func TestClosePartialMasksAndRequiresNote(t *testing.T) {
	// note "" → ErrErasureCloseNoteRequired; 501 runes → ErrErasureCloseNoteRequired
	// note "asked by mario.rossi@example.com" → stored note does not contain "mario.rossi@example.com"
}
func TestListOpenMarksClosableRows(t *testing.T) {
	// incomplete with only an unregistered key not done → Closable true; incomplete with a registered key failed → false; pending → false
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/compliance/services/... -run 'Close|AbandonedKeys|ListOpenMarks'` (and the repository test with `MONGO_TEST_URI`)
Expected: FAIL — `svc.ClosePartial undefined`, `r.ClosePartial undefined`.

- [ ] **Step 3: Implement** as specified. Annotate the repository update like `Resolve` (`//tenantscope:allow` — resolves one erasure request by its own UUID).

- [ ] **Step 4: Run to verify they pass** (same commands). Expected: PASS.

- [ ] **Step 5: Commit** — `feat(compliance): close incomplete erasures stuck on unregistered producers` with the two trailers.

---

### Task 7: HTTP surface — self-service record, admin list, execute/reject/close on incomplete rows, export keys

**Files:**
- Modify: `backend/internal/core/compliance/handlers/me_handler.go:24-107`
- Modify: `backend/internal/core/compliance/handlers/erasure_request_handler.go`
- Modify: `backend/internal/core/compliance/module.go:314` (`NewMeHandler` call)
- Modify: `backend/internal/shared/errcode/codes.go` (after `CompliancePolicyPersistenceFailed`, ~L656), `codes_test.go` (`goldenCodes`)
- Modify: `backend/openapi/enterprise.json` (regenerated)
- Test: `handlers/me_handler_test.go` (`NewMeHandler` calls gain an `ErasureRequestService` over `erasuretest.MemRepo`), `handlers/erasure_request_handler_test.go`

**Interfaces:**
- Consumes: Task 4 service API, `ErrErasureRequestStarted`, `repository.ErrErasureRequestBusy`; Task 6 `ClosePartial`, `ErrErasureRequestNotClosable`, `ErrErasureCloseNoteRequired`.
- Produces:
  - `errcode.ComplianceErasureRequestBusy = "compliance.erasure_request_busy"` (409, "Another erasure run holds this request; try again in a few minutes."), `errcode.ComplianceErasureRequestStarted = "compliance.erasure_request_started"` (409, "This erasure has started; it can be retried, not rejected."), `errcode.ComplianceErasureRequestNotClosable = "compliance.erasure_request_not_closable"` (409, "Only an incomplete request whose remaining producers are no longer installed can be closed; retry it instead."), `errcode.ComplianceErasureCloseNoteRequired = "compliance.erasure_close_note_required"` (422, "A note of 1 to 500 characters is required to close an erasure request.").
  - Route `POST /v1/admin/compliance/erasure-requests/{id}/close`, operation ID `compliance-erasure-request-close`, registered in `RegisterErasureRequestAdminWriteRoutes` (so `system.compliance.dsr.manage` + `RequireStepUp(5 * time.Minute)` as execute/reject; no new permission, so policycoverage is unchanged). `CloseErasureRequestInput{ID string \`path:"id"\`; Body struct{ Note string \`json:"note" minLength:"1" maxLength:"500" doc:"Why the remaining erasure is abandoned"\` }}`, `CloseErasureRequestOutput{Body models.ErasureRequest}`. Errors: not found/resolved → 404 (existing text), busy → `ComplianceErasureRequestBusy`, not closable → `ComplianceErasureRequestNotClosable`, note → `ComplianceErasureCloseNoteRequired`.
  - The list items carry `closable` (Task 6).
  - The export operation's description (`me_handler.go` `RegisterMeRoutes`) states that bundle keys are producer keys (`auth.operator`, `auth.client`, …) and that bundle version `2` introduced them; `ExportOutput` is otherwise unchanged.
  - `func NewMeHandler(dsr *services.DSRService, erasures *services.ErasureRequestService) *MeHandler`
  - `EraseOutput.Body` gains `Status string \`json:"status" enum:"completed,incomplete"\`` and `RequestID string \`json:"requestId"\``; `Purged`/`Errors` now carry producer keys.
  - `ErasureRequestHandler.ListOpen` (replaces `ListPending`; operation ID unchanged, summary "List open (pending or incomplete) right-to-erasure requests").
  - `ExecuteErasureRequestOutput.Body` gains `Status string \`json:"status" enum:"completed,incomplete"\`` and `Progress []models.ErasureProducerProgress \`json:"progress"\``; the execute operation's description states that an incomplete request is retried with its stored mode and the body's `mode` is ignored.

- [ ] **Step 1: Write the failing tests**

```go
func TestMeEraseCompletedRecordsRequest(t *testing.T) {
	// producers user + auth done → out.Body.Status == "completed", out.Body.RequestID != "",
	// repo.Row(out.Body.RequestID).Source == "self_service", TotalRows unchanged semantics (existing TestMeEraseAggregatesTotalRows keeps passing)
}
func TestMeEraseReportsIncomplete(t *testing.T) {
	// producer retrieval {Pending:true} → out.Body.Status == "incomplete"; repo row Status "incomplete" with NextAttemptAt set
}
func TestErasureListOpenIncludesIncomplete(t *testing.T) {
	// pending + incomplete + completed rows → len(out.Body.Items) == 2
}
func TestErasureExecuteIncompleteReturnsProgress(t *testing.T) {
	// incomplete row, remaining producer done → out.Body.Status == "completed", len(out.Body.Progress) == 2
}
func TestErasureExecuteBusyIs409(t *testing.T) {
	assertCode(t, err, http.StatusConflict, errcode.ComplianceErasureRequestBusy)
}
func TestErasureRejectIncompleteIs409(t *testing.T) {
	assertCode(t, err, http.StatusConflict, errcode.ComplianceErasureRequestStarted)
}
func TestErasureCloseUnregisteredProducer(t *testing.T) {
	// registry user; incomplete r-1 progress [user done, gone failed]; note "module removed"
	// out.Body.Status == "closed_partial", out.Body.Abandoned == ["gone"], out.Body.ResolvedBy == "admin-1"
}
func TestErasureCloseWithRegisteredProducerIs409(t *testing.T) {
	assertCode(t, err, http.StatusConflict, errcode.ComplianceErasureRequestNotClosable)
}
func TestErasureCloseWithoutNoteIs422(t *testing.T) {
	assertCode(t, err, http.StatusUnprocessableEntity, errcode.ComplianceErasureCloseNoteRequired)
}
func TestMeExportBundleHasBothAuthTiers(t *testing.T) {
	// keyed fake producers auth.operator / auth.client (handlers fakeProducer gains an optional key) → out.Body.Bundle has both keys
}
```

Also add `TestErasureCloseRouteIsBehindDSRManageAndStepUp` next to the existing route tests (`handlers/policy_routes_test.go` shows how routes are asserted): the close path is registered by `RegisterErasureRequestAdminWriteRoutes` and not by the read registration.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/compliance/handlers/ ./internal/shared/errcode/`
Expected: FAIL — `undefined: errcode.ComplianceErasureRequestBusy`, `NewMeHandler` arity.

- [ ] **Step 3: Implement** the handlers, codes and golden rows. `MeHandler.Erase` calls `EraseNow(ctx, userUUID, tenantID, models.ErasureSourceSelfService, userUUID, iface.EraseHardDelete)` with `tenantID, _ := ctxauth.GetTenantID(ctx)`; `ErrLegalHoldActive` stays 409 and any other error 500 with the existing fixed detail. Update the `Erase` and erase-route descriptions: the record is kept and retried when a producer is not done. Then regenerate the spec: `cd backend && make openapi-dump`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/core/compliance/... ./internal/shared/errcode/ && git diff --stat -- openapi/`
Expected: PASS; the diff touches `PurgeResult` (`pending`), `ErasureRequest` (incl. `abandoned`, `closable`), `ErasureProducerProgress`, `EraseOutputBody`, `ExecuteErasureRequestOutputBody`, the new close operation with `CloseErasureRequestInputBody`, and the list/execute/export operation texts only.

- [ ] **Step 5: Commit** — `feat(compliance): incomplete erasures on the self-service and admin API` with the two trailers (include `backend/openapi/enterprise.json`).

---

### Task 8: Console — `incomplete` and `closed_partial` status, progress, retry and close

Invoke the **`orkestra-frontend-admin`** skill before writing any code in this task and post its pre-flight block. Production precedent: `frontend-admin/src/pages/admin/compliance/ErasureRequestsTab.tsx` and `LegalHoldsTab.tsx`; references: `src/reference/components/tables/Tables.tsx` (icon buttons in rows) and `src/reference/components/ui/StatCards.tsx` (the KPI tile); primitives `SubtleBadge`, `IconButton`, `SearchablePagedTable`, `EmptyState`; the close confirmation reuses `src/pages/admin/compliance/policies/ReasonModal.tsx` (react-hook-form + yup, mandatory reason ≤ 500) — read it and `src/reference/components/ui/Modals.tsx` before wiring it.

**Files:**
- Modify: `frontend-admin/src/store/api/complianceApi.ts:30-40` (types), `:262-274` (execute response type), new `closeErasureRequest` mutation after `rejectErasureRequest` (~L286) and its hook in the export list (~L504)
- Modify: `frontend-admin/src/pages/admin/compliance/ErasureRequestsTab.tsx`
- Modify: `frontend-admin/src/pages/admin/compliance/complianceFormat.ts:22-35`
- Modify: `frontend-admin/src/pages/admin/compliance/index.tsx:85-122` (KPI subtitle only)
- Modify: `frontend-admin/src/locales/en.json`, `it.json` (`adminCompliance.status`, `adminCompliance.erasure`, `adminCompliance.stats.pendingErasures.subtitle`)
- Create: `frontend-admin/src/pages/admin/compliance/complianceFormat.test.ts`
- Test: `frontend-admin/src/pages/admin/compliance/index.test.tsx`

**Interfaces:**
- Consumes: Task 7 JSON shapes, including `closable` and the close route.
- Produces (TypeScript):

```ts
export type ErasureRequestStatus = 'pending' | 'incomplete' | 'completed' | 'rejected' | 'closed_partial';
export type ErasureProducerOutcome = 'done' | 'pending' | 'failed';
export interface ErasureProducerProgress {
  key: string; subject: string; outcome: ErasureProducerOutcome;
  rowsDeleted: number; rowsAnonymized: number; attempts: number; updatedAt: string;
}
export interface PurgeResult { rowsDeleted: number; rowsAnonymized: number; collections?: string[]; pending?: boolean; }
// ErasureRequest: status: ErasureRequestStatus; adds source?: 'request' | 'self_service' | 'retention';
//   progress?: ErasureProducerProgress[]; attempts?: number; nextAttemptAt?: string; lastError?: string; resolutionNote?: string;
//   abandoned?: string[]; closable?: boolean
// closeErasureRequest: builder.mutation<ErasureRequest, { id: string; note: string }> — POST /v1/admin/compliance/erasure-requests/{id}/close
//   with body { note }, invalidates { type: 'ErasureRequest', id: 'LIST' } (same shape as rejectErasureRequest)
// executeErasureRequest result: { status: 'completed' | 'incomplete'; purged: Record<string, PurgeResult>; progress: ErasureProducerProgress[] }
```

UI rules: `erasureStatusColor('incomplete')` → `'danger'`. A `pending` row keeps Execute (mode `hard_delete`) and Reject. An `incomplete` row shows Retry (calls execute with `original.mode ?? 'hard_delete'`) and no Reject; under its status badge a muted line reads the progress (`{{done}} of {{total}} producers done`) and then either `Next retry {{date}}` (from `nextAttemptAt`, via `formatDateTime`) or `Automatic retries exhausted`. The subject cell shows the source label when `source` is `self_service` or `retention`. After execute/retry, `status === 'incomplete'` shows `toast.warning(executeIncomplete)`, otherwise the existing success toast. `erasureStatusColor('closed_partial')` → `'secondary'`. An `incomplete` row with `closable: true` also shows a Close button (`outline-secondary`); it opens `ReasonModal` (title `closeTitle`, body `closeBody` with the not-done unregistered keys, confirm `close`) and sends the reason as `note`; success → `toast.success(closeSuccess)`, failure → `toast.error(closeError)`. Rows without `closable` never show Close.

i18n (en / it):
- `adminCompliance.status.incomplete`: "Incomplete" / "Incompleta"
- `adminCompliance.status.closed_partial`: "Closed (partial)" / "Chiusa (parziale)"
- `adminCompliance.erasure.close`: "Close" / "Chiudi"
- `adminCompliance.erasure.closeTitle`: "Close a partial erasure" / "Chiudi una cancellazione parziale"
- `adminCompliance.erasure.closeBody`: "These producers are no longer installed and will never finish: {{keys}}. Closing records the erasure as partial; it cannot be undone." / "Questi produttori non sono più installati e non finiranno mai: {{keys}}. Chiudere registra la cancellazione come parziale; non si può annullare."
- `adminCompliance.erasure.closeSuccess`: "Erasure request closed as partial" / "Richiesta di cancellazione chiusa come parziale"
- `adminCompliance.erasure.closeError`: "Close failed" / "Chiusura non riuscita"
- `adminCompliance.erasure.retry`: "Retry" / "Riprova"
- `adminCompliance.erasure.executeIncomplete`: "Erasure started: some producers are not done yet and will be retried" / "Cancellazione avviata: alcuni produttori non hanno ancora finito e verranno ritentati"
- `adminCompliance.erasure.progress`: "{{done}} of {{total}} producers done" / "{{done}} produttori su {{total}} completati"
- `adminCompliance.erasure.nextRetry`: "Next retry {{date}}" / "Prossimo tentativo {{date}}"
- `adminCompliance.erasure.retriesExhausted`: "Automatic retries exhausted" / "Tentativi automatici esauriti"
- `adminCompliance.erasure.source.self_service`: "Self-service" / "Self-service"; `.retention`: "Retention" / "Conservazione"
- `adminCompliance.erasure.emptyMessage`: "No open erasure requests." / "Nessuna richiesta di cancellazione aperta."
- `adminCompliance.erasure.emptyHint`: "Erasure requests awaiting review or retry will appear here." / "Le richieste di cancellazione in attesa di revisione o di un nuovo tentativo appariranno qui."
- `adminCompliance.stats.pendingErasures.subtitle`: "Awaiting review or retry" / "In attesa di revisione o di un nuovo tentativo"

- [ ] **Step 1: Write the failing tests**

```ts
// complianceFormat.test.ts
it('colors an incomplete erasure as danger', () => {
  expect(erasureStatusColor('incomplete')).toBe('danger');
  expect(erasureStatusColor('closed_partial')).toBe('secondary');
  expect(erasureStatusColor('pending')).toBe('warning');
});

// index.test.tsx — fixture: an incomplete row er-2 { status: 'incomplete', mode: 'anonymize', source: 'self_service',
//   attempts: 20, progress: [{key:'user', outcome:'done', …}, {key:'auth.client', outcome:'failed', …}] } (no nextAttemptAt)
it('shows an incomplete request with its progress, a Retry action and no Reject', async () => {
  // findByText('Incomplete'); getByText('1 of 2 producers done'); getByText('Automatic retries exhausted');
  // within(row er-2): getByRole('button', {name: /^retry$/i}) present, queryByRole('button', {name: /^reject$/i}) null
});
it('retries an incomplete request with its stored mode', async () => {
  // click Retry → POST /v1/admin/compliance/erasure-requests/er-2/execute with body { mode: 'anonymize' }
});
it('closes a closable incomplete request with a mandatory note', async () => {
  // fixture er-3 { status: 'incomplete', closable: true, progress: [{key:'user', outcome:'done'}, {key:'gone', outcome:'failed'}] }
  // click Close in er-3's row → dialog shows 'gone'; confirming with an empty reason does not POST;
  // typing 'module removed' and confirming → POST /v1/admin/compliance/erasure-requests/er-3/close with body { note: 'module removed' }
});
it('offers Close only on closable rows', async () => {
  // er-1 (pending) and er-2 (incomplete, closable absent): queryByRole('button', {name: /^close$/i}) null within each row
});
it('warns when an execution leaves the request incomplete', async () => {
  // execute handler answers { status: 'incomplete', purged: {}, progress: [] } → findByText(/will be retried/i)
});
```

Also update the existing empty-state assertion (`/no pending erasure requests/i` → `/no open erasure requests/i`) and the existing execute test's mock response (`{ status: 'completed', purged: {}, progress: [] }`).

- [ ] **Step 2: Run to verify they fail**

Run: `cd frontend-admin && npx vitest run src/pages/admin/compliance src/locales`
Expected: FAIL — the new assertions (`Incomplete`, `Retry`) are not found.

- [ ] **Step 3: Implement** the types, tab, format and locale keys as specified.

- [ ] **Step 4: Run to verify they pass**

Run: `cd frontend-admin && npx vitest run src/pages/admin/compliance src/locales src/store/api && npm run typecheck && npm run lint`
Expected: PASS (locale parity included).

- [ ] **Step 5: Commit** — `feat(frontend-admin): incomplete erasure requests with progress, retry and close` with the two trailers.

---

### Task 9: Documentation, ADR-0009 amendment note, full CI

**Files:**
- Modify: `backend/internal/core/compliance/AGENTS.md` (Layout: `erasure_retry.go`, `erasuretest/`; Key contracts: the producer list becomes `user`, `auth` (two instances keyed `auth.operator` / `auth.client`), `tenant`, `authz`, `notification`, `llm`; a new bullet "Erasure lifecycle (ADR-0023 D12, amends ADR-0009)"; the Start/Stop line in Layout; Routes: list = pending + incomplete, execute on incomplete = retry with the stored mode, reject on incomplete = 409, new `POST …/compliance/erasure-requests/{id}/close` (dsr.manage + step-up); the DSR bullet states that export bundles are keyed by producer key, bundle version `2`)
- Modify: `docs/site/modules/core/compliance.mdx:15-27` (producer list adds `llm`; the erasure workflow paragraph describes `incomplete`, retries, the self-service/retention records and closing as `closed_partial`; the export bullet says bundle keys are producer keys — `auth.operator`, `auth.client` — since bundle version `2`; the admin-writes route line adds close)
- Modify: `docs/adr/0009-core-compliance-module.md:12` (Status cell gains "; amended by [ADR-0023](0023-core-retrieval-module.md) (D12: an erasure request completes only when every producer is done)", the way ADR-0006's status names its amendments)
- Modify: `docs/adr/README.md:17` (`Accepted (Phases 1–4 shipped; amended by 0023)`, the convention of the 0006 row)

**Interfaces:**
- Consumes: everything above. Produces: none.

The AGENTS.md lifecycle bullet states, in this order: every erasure path writes its `compliance_erasure_requests` row before any producer runs; producer instance keys and the `#n` fallback; outcomes `done | pending | failed` and that only all-`done` completes a request; partial counts of a failed producer are kept; the lease, deadline, backoff and 20-attempt values; exhaustion leaves the row `incomplete` with no `nextAttemptAt` until an operator retries; a hold placed after the start is recorded as `legal_hold_active` and counted as an attempt; producer error text is logged, never stored on the row; a progress key whose producer is no longer registered keeps the request `incomplete` until an operator closes it as `closed_partial` (allowed only when every not-done producer is unregistered; mandatory masked note, `abandoned` keys on the row, audit `gdpr.dsr.erasure_closed_partial`); `PurgePersonalData` must be idempotent.

- [ ] **Step 1: Write the docs** as listed.
- [ ] **Step 2: Verify the agent-instruction checks**

Run: `make agents-test`
Expected: PASS.

- [ ] **Step 3: Full CI**

Run: `make ci-backend` (with Mongo + Redis running and `MONGO_TEST_URI` exported) and `make ci-frontend-admin`.
Expected: `Backend CI: OK`, frontend CI green; `backend-openapi-check` reports no drift; `backend-piiscan` and `backend-tenantscope` pass without new baseline entries.

- [ ] **Step 4: Commit** — `docs(compliance): erasure requests stay incomplete until every producer is done` with the two trailers.
