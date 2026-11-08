# SDK offline-sync primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give modules three reusable primitives for clients that retry and sync offline: an `Idempotency-Key` HTTP middleware with a per-module Mongo store, a delta cursor with a stable horizon and tombstones, and a 409 conflict contract.

**Architecture:** Two new SDK packages, no module and no kernel change. `backend/pkg/sdk/idempotency` is a chi middleware a module mounts on its own route group (after the auth middleware) with a `Store` (Mongo implementation over a module-owned collection). `backend/pkg/sdk/syncdelta` is pure helpers (cursor codec, Mongo filter/sort, page shape, conflict error, client-time validation).

**Tech Stack:** Go 1.26, chi v5, Huma v2, mongo-driver v1 (all already in `backend/go.mod`).

**Spec:** the offline-sync design, decisions S1–S12; its client contract is published as `docs/site/sdk/offline-sync.mdx` (Task 3).

## Global Constraints

- No new third-party dependency; `pkg/sdk/**` imports nothing from `backend/internal/` (so no `internal/shared/errcode`: the middleware writes the error envelope itself).
- Error envelope (same wire shape as `internal/shared/errcode.Error`): `Content-Type: application/problem+json`, body `{"status":<int>,"title":<http status text>,"detail":<text>,"code":<code>}`.
- Codes (spec, verbatim): `idempotency.key_required` 400, `idempotency.invalid_key` 400, `idempotency.unauthenticated` 400, `idempotency.body_too_large` 413, `idempotency.in_progress` 409 (+ `Retry-After: 1`), `idempotency.key_reused` 422, `idempotency.replay_unavailable` 409, `sync.conflict` 409, `sync.cursor_expired` 410, `sync.client_time_out_of_range` 422, `sync.invalid_cursor` 400.
- Values: key 8–128 chars `[A-Za-z0-9._:-]`; `MaxBody` default 1 MiB; stored response body ≤ 256 KiB; `LockTTL` default 2 min; `Retention` default 24 h; replay header `Idempotent-Replayed: true`; stable horizon 2 s; page limit 1–500 default 200; `DefaultTombstoneRetention` 30 days; client time ≤ 5 min in the future and ≤ 30 days in the past.
- Commits: conventional subject, trailers `Prop: upstream` and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Gate only through `ci-gate make ci` (VM rules in `/home/tore/AGENTS.md`); never `make ci` bare.

## Review Focus

1. Two concurrent requests with the same key: exactly one handler execution; the other gets 409 `in_progress` (unique index + insert race). Test in Task 1 (Mongo integration, goroutines).
2. Handler panics or the process dies after the `in_progress` insert: a later retry after `LockTTL` re-executes instead of being stuck at 409 forever. Test in Task 1 (expired lock taken over atomically).
3. Same key from two different users or tenants: independent records, no replay across scopes. Test in Task 1.
4. A 5xx response, or a handler that writes nothing: record removed; retry re-executes. Test in Task 1.
5. Cursor at the horizon: records updated within the last 2 s are not returned and the returned cursor never passes `now−2s`; equal `updatedAt` values page by `_id` without loss or duplication. Test in Task 2 (Mongo integration).

---

### Task 1: `pkg/sdk/idempotency`

**Files:**
- Create: `backend/pkg/sdk/idempotency/{doc.go,middleware.go,store.go,mongo_store.go,problem.go}`
- Test: `backend/pkg/sdk/idempotency/middleware_test.go` (in-memory fake Store), `backend/pkg/sdk/idempotency/mongo_store_integration_test.go` (skip without `MONGO_TEST_URI`; copy the harness of `backend/internal/core/compliance/repository/repository_integration_test.go`)

**Interfaces — Produces:**
```go
type Record struct {
    Scope, Key, Fingerprint string
    State string // "in_progress" | "done"
    Status int; ContentType, Location string; Body []byte; BodyStored bool
    LockedUntil, ExpiresAt, CreatedAt time.Time
}
type Store interface {
    // Begin inserts an in_progress record, or takes over one whose LockedUntil < now.
    // Returns (nil, nil) when this caller owns the key now; otherwise the existing record.
    Begin(ctx context.Context, scope, key, fingerprint string, now, lockedUntil, expiresAt time.Time) (*Record, error)
    Finish(ctx context.Context, scope, key string, status int, contentType, location string, body []byte, bodyStored bool) error
    Abandon(ctx context.Context, scope, key string) error // delete (5xx, panic, empty response)
}
func NewMongoStore(coll *mongo.Collection) Store
func CollectionSpec(name string, retention time.Duration) module.CollectionSpec // unique {scope:1,key:1}; TTL on expiresAt (TTL 0s with expiresAt as the deadline, or retention — follow how module.IndexSpec expresses TTL)

type Options struct {
    Scope    string        // e.g. "operator" / "client"; prefixed to the stored scope
    Required bool
    MaxBody  int64         // default 1<<20
    LockTTL  time.Duration // default 2m
    Retention time.Duration // default 24h
    Now      func() time.Time
    Logger   *slog.Logger
}
func Middleware(store Store, opt Options) func(http.Handler) http.Handler
const HeaderKey = "Idempotency-Key"; const HeaderReplayed = "Idempotent-Replayed"
```
Behaviour: methods GET/HEAD/OPTIONS pass through untouched. Stored scope = `opt.Scope + "|" + tenantID + "|" + userUUID` from `ctxauth.GetTenantID` / `ctxauth.GetUserUUID` (`pkg/sdk/ctxauth`); no user UUID + key present → 400 unauthenticated. Fingerprint = hex SHA-256 of `method + "\n" + URL.RequestURI() + "\n" + body`; body read with `http.MaxBytesReader`-style limit then restored on `r.Body`. Response captured with a wrapper (status, headers `Content-Type`/`Location`, body up to 256 KiB + flag); after the handler: status ≥ 500, panic (recover, abandon, re-panic) or nothing written → `Abandon`; else `Finish`. Replay writes the stored status, `Content-Type`, `Location`, body and `Idempotent-Replayed: true`. Store errors → 503 with code `idempotency.unavailable` (add to the code list; fail closed: the request is not executed).

- [ ] **Step 1: Failing tests** — `TestNoKeyPassesThrough`, `TestRequiredKeyMissing` (400 key_required), `TestInvalidKey` (`"short"`, 129 chars, `"bad key!"` → 400 invalid_key), `TestFirstRequestExecutesAndStores`, `TestReplaySameFingerprint` (handler count 1, second response identical + `Idempotent-Replayed: true`), `TestReusedKeyDifferentBody` (422 key_reused), `TestInProgress` (fake store returns in_progress record → 409 + `Retry-After: 1`), `TestServerErrorAbandons` (500 → Abandon called; retry executes again), `TestPanicAbandons`, `TestBodyTooLarge` (413), `TestLargeResponseNotReplayed` (300 KiB body → second call 409 replay_unavailable), `TestScopeIsolation` (same key, two users → two executions), `TestStoreErrorFailsClosed` (503, handler not called), `TestUnauthenticatedWithKey` (400). Integration: `TestMongoBeginRace` (20 goroutines Begin same scope/key → exactly one nil), `TestMongoTakeOverExpiredLock`, `TestMongoFinishThenBeginReturnsDone`, `TestCollectionSpecIndexes` (unique + TTL present after ensuring the spec through the same path the registry uses, or by creating them directly from the spec).
- [ ] **Step 2:** `cd backend && go test ./pkg/sdk/idempotency/ -race` → FAIL (undefined).
- [ ] **Step 3:** Implement. Begin's atomic form: `FindOneAndUpdate` upsert with filter `{scope, key, $or:[{state:"in_progress", lockedUntil:{$lt:now}}]}` will not upsert correctly on a duplicate — use: `InsertOne`; on duplicate-key error, `FindOneAndUpdate({scope,key,state:"in_progress",lockedUntil:{$lt:now}}, {$set: new lock+fingerprint})` (taken over → nil), else `FindOne` and return the existing record.
- [ ] **Step 4:** Same command (+ `MONGO_TEST_URI='mongodb://127.0.0.1:27317/?directConnection=true'`) → PASS; `go vet`, `gofmt -l` clean.
- [ ] **Step 5: Commit** `feat(sdk/idempotency): Idempotency-Key middleware with a per-module store`.

### Task 2: `pkg/sdk/syncdelta`

**Files:**
- Create: `backend/pkg/sdk/syncdelta/{doc.go,cursor.go,page.go,conflict.go,clienttime.go}`
- Test: `backend/pkg/sdk/syncdelta/*_test.go`, `backend/pkg/sdk/syncdelta/filter_integration_test.go` (Mongo)

**Interfaces — Produces:**
```go
type Cursor struct{ UpdatedAt time.Time; ID string }
func EncodeCursor(c Cursor) string                 // base64url(no padding) of JSON {"t": RFC3339Nano, "id"}
func DecodeCursor(s string) (*Cursor, error)       // "" → nil, nil; garbage → ErrInvalidCursor
const StableHorizon = 2 * time.Second
const DefaultTombstoneRetention = 30 * 24 * time.Hour
const DefaultLimit, MaxLimit = 200, 500
func Horizon(now time.Time) time.Time               // now - StableHorizon
func Filter(cur *Cursor, now time.Time) bson.M     // updatedAt ≤ horizon AND (cur == nil OR updatedAt > t OR (updatedAt == t AND _id > id))
func Sort() bson.D                                  // {updatedAt:1, _id:1}
func ClampLimit(n int) int
func CheckExpired(cur *Cursor, now time.Time, retention time.Duration) error // cursor older than retention → ErrCursorExpired
type Page[T any] struct{ Items []T `json:"items"`; Deleted []string `json:"deleted"`; Cursor string `json:"cursor"`; HasMore bool `json:"hasMore"` }
type Entry interface{ SyncCursor() Cursor; IsDeleted() bool; SyncID() string }
func BuildPage[T Entry](rows []T, limit int, prev *Cursor) Page[T] // rows fetched with limit+1; splits deleted → Deleted ids; cursor = last row's, else prev (encoded); HasMore when len(rows) > limit
type ConflictError struct{ CurrentVersion int64; Current any }
func (e *ConflictError) Error() string
func (e *ConflictError) GetStatus() int            // 409, so Huma treats it as a StatusError
func (e *ConflictError) MarshalJSON() ([]byte, error) // {"status":409,"title":"Conflict","detail":"…","code":"sync.conflict","currentVersion":n,"current":{…}}
func ParseIfMatch(h string) (int64, bool)          // `"12"` or `12` → 12
var ErrInvalidCursor, ErrCursorExpired, ErrClientTimeOutOfRange error
func ValidateClientTime(t, now time.Time) error    // > now+5m or < now-30d → ErrClientTimeOutOfRange
```
Huma integration: check how Huma v2.39 renders an error returned from a handler that implements `huma.StatusError` (`GetStatus() int` + `Error()`) — the JSON body is the error value itself, so `MarshalJSON` controls the envelope. Write a test registering a tiny Huma operation (humatest) that returns `&ConflictError{…}` and asserts status 409, `code`, `currentVersion`, `current`.

- [ ] **Step 1: Failing tests** — `TestCursorRoundTrip`, `TestDecodeCursorInvalid`, `TestFilterShape` (bson equality for nil and non-nil cursor), `TestBuildPage` (limit 2 with 3 rows → HasMore, cursor of row 2; deleted rows go to `Deleted`, not `Items`), `TestCheckExpired`, `TestValidateClientTime` (now+4m ok, now+6m err, now−29d ok, now−31d err), `TestParseIfMatch`, `TestConflictErrorHuma` (humatest); integration `TestFilterPagesWithoutLossAtEqualTimestamps` (10 docs same `updatedAt`, limit 3 → 4 pages, all 10 ids once, in `_id` order) and `TestHorizonExcludesFreshWrites` (doc with `updatedAt = now−1s` not returned; cursor never after horizon).
- [ ] **Step 2:** `go test ./pkg/sdk/syncdelta/ -race` → FAIL. Implement. → PASS (with `MONGO_TEST_URI` for integration).
- [ ] **Step 3: Commit** `feat(sdk/syncdelta): delta cursor, tombstones and conflict contract`.

### Task 3: Documentation

**Files:**
- Modify: `backend/pkg/sdk/AGENTS.md` (package map rows for `idempotency/` and `syncdelta/`, and the "HTTP middleware addons need to wrap their own routes with" example).
- Create: `docs/site/sdk/offline-sync.mdx` (client contract: header, codes table, replay semantics, cursor/horizon/tombstones/410, conflict 409 body, client time; module-author section: mounting `idempotency.Middleware` on a route group after auth with `CollectionSpec("<module>_idempotency_keys", 24*time.Hour)` in `Collections()`, a `GET …/sync?cursor=` handler with `Filter`/`Sort`/`BuildPage`; mapping table of a typical mobile offline operation queue — task status update, completion, revert, confirmation, photo upload — to these primitives). Add it to the docs-site sidebar/index the other `docs/site/sdk/*.mdx` pages use.
- [ ] Commit `docs(sdk): offline-sync primitives reference`.
