package services

// Phase 17: cache-layer coverage for authz.Service. Backed by an
// in-process miniredis so the cache code paths actually run instead
// of short-circuiting on a nil redis adapter. Pins the contract that
// CreateBinding / DeleteBinding / DeleteRole / RemoveBindingsByTenant /
// UpdateRole all invalidate the affected user's cached effective
// permissions — without these, a permission grant or revocation
// would only take effect after the 60s TTL.
//
// D26 / L-11: the invalidation used to be `KEYS authz:cache:<user>:*`
// followed by `DEL`. It enumerated keys on the hot path, it could
// partially fail leaving some verdicts live, it raced a repopulation
// landing between the scan and the delete, and its glob was built from
// a request body. The cache is now generation-keyed, so invalidation is
// a single atomic INCR. The "no KEYS, exactly one INCR" assertions
// below are that contract, not an implementation detail.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// startMiniredis spins up an in-process Redis and wraps it in the
// production RedisClientAdapter so the Service.cache* methods exercise
// the real Get/Set/MGet/Incr code paths.
func startMiniredis(t *testing.T) (*miniredis.Miniredis, *database.RedisClientAdapter) {
	t.Helper()
	mr := miniredis.RunT(t)
	// MaxRetries -1 disables go-redis's retry backoff. Several tests
	// below close miniredis on purpose to exercise the degraded paths;
	// with retries on, each of those spends ~1.7s backing off against a
	// server that is never coming back.
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return mr, database.NewRedisClientAdapter(client)
}

// newCacheTestService stands a Service up with a real Redis
// adapter and the in-memory fake repo. Cache code paths exercise
// the production implementation, and the client is wired through
// setRedis — the same seam New uses — so the MGET type assertion is
// covered rather than bypassed by a struct literal.
func newCacheTestService(t *testing.T, lookup UserSystemRoleLookup) (*Service, *fakeRepo, *miniredis.Miniredis) {
	t.Helper()
	repo := newFakeRepo()
	mr, redisAdapter := startMiniredis(t)
	svc := &Service{
		repo:                repo,
		logger:              testLogger(t),
		userRoles:           lookup,
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
	svc.setRedis(redisAdapter, testLogger(t))
	return svc, repo, mr
}

// ===== command recorder =====
//
// miniredis v2.38.0 exposes no command log of its own. The pre-hook is
// the seam: it runs before every dispatched command, and returning false
// lets miniredis handle the command normally. A plain command *count*
// would be useless here — the whole assertion is that the command is an
// INCR and never a KEYS.

type cmdRecorder struct {
	mu   sync.Mutex
	cmds []string
}

func recordCommands(t *testing.T, mr *miniredis.Miniredis) *cmdRecorder {
	t.Helper()
	rec := &cmdRecorder{}
	mr.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		rec.mu.Lock()
		rec.cmds = append(rec.cmds, strings.ToUpper(cmd))
		rec.mu.Unlock()
		return false // not handled here: miniredis still runs the command
	})
	return rec
}

func (r *cmdRecorder) reset() {
	r.mu.Lock()
	r.cmds = nil
	r.mu.Unlock()
}

func (r *cmdRecorder) log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.cmds))
	copy(out, r.cmds)
	return out
}

func (r *cmdRecorder) count(name string) int {
	n := 0
	for _, c := range r.log() {
		if c == name {
			n++
		}
	}
	return n
}

// ===== cacheKey =====

func TestCacheKey_StableForGivenInputs(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	// Stable across calls while the generations do not move.
	if a, b := svc.cacheKey(ctx, "u-1", "tenant-A"), svc.cacheKey(ctx, "u-1", "tenant-A"); a != b {
		t.Errorf("not stable: %q != %q", a, b)
	}
	// Different inputs produce different keys.
	if svc.cacheKey(ctx, "u-1", "tenant-A") == svc.cacheKey(ctx, "u-1", "tenant-B") {
		t.Errorf("tenant must affect the key")
	}
	if svc.cacheKey(ctx, "u-1", "tenant-A") == svc.cacheKey(ctx, "u-2", "tenant-A") {
		t.Errorf("user must affect the key")
	}
}

func TestCacheKey_CarriesBothGenerations(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	if err := mr.Set(authzGlobalGenKey, "4"); err != nil {
		t.Fatalf("seed global generation: %v", err)
	}
	if err := mr.Set(authzUserGenPrefix+"u-1", "7"); err != nil {
		t.Fatalf("seed user generation: %v", err)
	}

	key := svc.cacheKey(context.Background(), "u-1", "tenant-1")
	if key != "authz:cache:4:u-1:7:tenant-1" {
		t.Fatalf("cacheKey = %q", key)
	}
}

func TestCacheKey_MissingGenerationsReadAsZero(t *testing.T) {
	// Also pins the empty-tenant normalisation: "" collapses to "-" so
	// the key stays parseable and distinct from a tenant literally
	// named "".
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	key := svc.cacheKey(context.Background(), "u-1", "")
	if key != "authz:cache:0:u-1:0:-" {
		t.Fatalf("cacheKey = %q, want zeros and the '-' tenant placeholder", key)
	}
}

// ===== cacheGet / cacheSet round-trip =====

func TestCacheSetGet_RoundTripsPermissions(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"billing.invoice.read", "tenant.update"})

	got, ok := svc.cacheGet(ctx, "u-1", "tenant-A")
	if !ok {
		t.Fatalf("cache miss right after set")
	}
	if len(got) != 2 || got[0] != "billing.invoice.read" || got[1] != "tenant.update" {
		t.Errorf("got %v, want [billing.invoice.read tenant.update]", got)
	}
}

func TestCacheGet_MissingKeyReturnsFalse(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	if _, ok := svc.cacheGet(context.Background(), "u-X", "tenant-X"); ok {
		t.Errorf("expected miss for unset key")
	}
}

func TestCacheGet_MalformedPayloadReturnsFalse(t *testing.T) {
	// A corrupt JSON value in Redis must fail safe — return false so
	// the caller falls back to recomputing rather than handing the
	// caller garbage. Guards the json.Unmarshal-error branch.
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	_ = mr.Set(svc.cacheKey(ctx, "u-c", "t"), "not-json")
	if _, ok := svc.cacheGet(ctx, "u-c", "t"); ok {
		t.Errorf("malformed payload must be a cache miss, not a panic")
	}
}

// A generation read that fails is a cache MISS — the evaluator goes to
// Mongo, which is the fresh answer. It must never be a stale hit and
// never an error to the caller.
func TestCacheGet_GenerationReadFailureIsAMiss(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	mr.Close()

	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Fatal("a generation read failure must read as a miss")
	}
}

// ===== cacheSet TTL =====

func TestCacheSet_HasTTL(t *testing.T) {
	// 60s TTL is what makes revocation eventually consistent across
	// replicas. Pin it so a refactor doesn't accidentally bump it to
	// hours or strip it entirely. It also bounds how long a retired
	// entry lingers in Redis after a generation bump.
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"x.read"})

	ttl := mr.TTL(svc.cacheKey(ctx, "u-1", "tenant-A"))
	if ttl == 0 {
		t.Fatalf("expected a TTL on the cache entry, got 0 (= no expiry)")
	}
	if ttl > 90*time.Second {
		t.Errorf("TTL = %v, expected ~60s — drift may make revocation lag too long", ttl)
	}
}

// ===== InvalidateUserPermissions =====

// One INCR, no KEYS. The command log is the assertion: a KEYS here is
// the defect, not an implementation detail.
func TestInvalidateUserPermissions_IsOneIncrAndNoScan(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	rec := recordCommands(t, mr)
	rec.reset()

	if err := svc.InvalidateUserPermissions(context.Background(), "u-1"); err != nil {
		t.Fatalf("InvalidateUserPermissions: %v", err)
	}
	if rec.count("KEYS") != 0 {
		t.Fatalf("a KEYS scan was issued: %v", rec.log())
	}
	if rec.count("INCR") != 1 {
		t.Fatalf("want exactly one INCR, got: %v", rec.log())
	}
}

// An entry written under the previous generation can never be READ
// again — that is what makes the invalidation total instead of
// best-effort.
func TestGenerationBump_RetiresTheOldEntry(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "tenant-1", []string{"tenant.read"})

	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-1"); !ok {
		t.Fatal("precondition: the entry must be readable")
	}
	if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-1"); ok {
		t.Fatal("an entry written under the previous generation must not be readable")
	}
}

func TestInvalidateUserPermissions_RetiresOnlyThatUser(t *testing.T) {
	// The per-user counter is what keeps one user's revocation from
	// costing every other user a cold cache.
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"a.read"})
	svc.cacheSet(ctx, "u-1", "tenant-B", []string{"b.read"})
	svc.cacheSet(ctx, "u-other", "tenant-A", []string{"c.read"})

	if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
		t.Fatalf("InvalidateUserPermissions: %v", err)
	}

	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-A"); ok {
		t.Errorf("u-1 / tenant-A must be invalidated")
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-B"); ok {
		t.Errorf("u-1 / tenant-B must be invalidated")
	}
	if _, ok := svc.cacheGet(ctx, "u-other", "tenant-A"); !ok {
		t.Errorf("u-other must NOT be invalidated by u-1's bump")
	}
}

func TestInvalidateUserPermissions_BumpsEvenWithNothingCached(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	// The bump is unconditional: with no scan there is no "nothing to
	// delete" branch to get wrong, and no cheap way to know there was
	// nothing cached in the first place. It must simply succeed.
	if err := svc.InvalidateUserPermissions(context.Background(), "u-never-cached"); err != nil {
		t.Errorf("InvalidateUserPermissions: %v", err)
	}
}

// An INCR failure is RETURNED, because the caller decides what it means
// (D27: a failed pre-invalidation refuses the change).
func TestInvalidateUserPermissions_ErrorIsReturned(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	mr.Close()
	if err := svc.InvalidateUserPermissions(context.Background(), "u-1"); err == nil {
		t.Fatal("an INCR failure must be returned, not swallowed")
	}
}

// Retired entries are not deleted; they expire on their own 60s TTL.
func TestRetiredEntries_ExpireOnTheirOwnTTL(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	oldKey := svc.cacheKey(ctx, "u-1", "t-1")

	if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if !mr.Exists(oldKey) {
		t.Fatal("the retired entry is not deleted — it is simply unreachable")
	}
	mr.FastForward(61 * time.Second)
	if mr.Exists(oldKey) {
		t.Fatal("the retired entry must expire on its own TTL")
	}
}

// ===== flushCache =====

// The global flush retires EVERY user's entries with one INCR.
func TestFlushCache_RetiresEveryUser(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	svc.cacheSet(ctx, "u-2", "t-1", []string{"b"})
	rec := recordCommands(t, mr)
	rec.reset()

	if err := svc.flushCache(ctx); err != nil {
		t.Fatalf("flushCache: %v", err)
	}
	if rec.count("KEYS") != 0 {
		t.Fatalf("the global flush must not scan either: %v", rec.log())
	}
	if rec.count("INCR") != 1 {
		t.Fatalf("want exactly one INCR, got: %v", rec.log())
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Error("u-1's entry survived the global flush")
	}
	if _, ok := svc.cacheGet(ctx, "u-2", "t-1"); ok {
		t.Error("u-2's entry survived the global flush")
	}
}

func TestFlushCache_DoesNotTouchNonAuthzKeys(t *testing.T) {
	// Another module's key prefix must survive flushCache. Trivially
	// true of a generation bump, and kept so a future "just FLUSHDB it"
	// shortcut fails loudly.
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	_ = mr.Set("session:abc", "keep me")
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"x"})

	if err := svc.flushCache(ctx); err != nil {
		t.Fatalf("flushCache: %v", err)
	}

	if got, _ := mr.Get("session:abc"); got != "keep me" {
		t.Errorf("non-authz key was wiped: got %q", got)
	}
}

func TestFlushCache_ErrorIsReturned(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	mr.Close()
	if err := svc.flushCache(context.Background()); err == nil {
		t.Fatal("an INCR failure must be returned, not swallowed")
	}
}

// ===== degraded modes =====

// A nil Redis means no cache at all: reads miss, invalidation is a
// no-op success. A test setup without Redis must not fail every role
// edit. "No cache configured" is not "cache unavailable".
func TestCache_NilRedisDegradesCleanly(t *testing.T) {
	svc := &Service{}
	if _, ok := svc.cacheGet(context.Background(), "u-1", "t-1"); ok {
		t.Error("a nil Redis must read as a miss")
	}
	// Must not panic either.
	svc.cacheSet(context.Background(), "u-1", "t-1", []string{"a"})
	if err := svc.InvalidateUserPermissions(context.Background(), "u-1"); err != nil {
		t.Errorf("a nil Redis must make invalidation a no-op success, got %v", err)
	}
	if err := svc.flushCache(context.Background()); err != nil {
		t.Errorf("a nil Redis must make the flush a no-op success, got %v", err)
	}
}

// noMGetRedis satisfies module.RedisClient and nothing more. It stands
// in for a fork's own client: module.RedisClient is an SDK contract
// forks implement, so MGET cannot be added to it — it is an optional
// narrow extension the service type-asserts for once, at construction.
type noMGetRedis struct{ inner *database.RedisClientAdapter }

func (n noMGetRedis) Get(ctx context.Context, key string) (string, error) {
	return n.inner.Get(ctx, key)
}

func (n noMGetRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return n.inner.Set(ctx, key, value, expiration)
}

func (n noMGetRedis) Del(ctx context.Context, keys ...string) error {
	return n.inner.Del(ctx, keys...)
}

func (n noMGetRedis) Keys(ctx context.Context, pattern string) ([]string, error) {
	return n.inner.Keys(ctx, pattern)
}

func (n noMGetRedis) Incr(ctx context.Context, key string) (int64, error) {
	return n.inner.Incr(ctx, key)
}

func (n noMGetRedis) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return n.inner.Expire(ctx, key, expiration)
}

func TestCache_ClientWithoutMGetSkipsReadsAndWritesButStillBumps(t *testing.T) {
	// Without MGET the two generations cannot be read in one round trip,
	// and two GETs could compose a key from two different moments. The
	// contract is to bypass the cache — no read, no write — so every
	// lookup resolves from Mongo. Slower, never stale.
	var _ module.RedisClient = noMGetRedis{}

	mr, adapter := startMiniredis(t)
	svc := &Service{
		repo:                newFakeRepo(),
		logger:              testLogger(t),
		userRoles:           staticRoleLookup(""),
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
	svc.setRedis(noMGetRedis{inner: adapter}, testLogger(t))
	ctx := context.Background()

	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	if keys := mr.Keys(); len(keys) != 0 {
		t.Errorf("cacheSet must write nothing without MGET, got %v", keys)
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Error("cacheGet must miss without MGET")
	}
	// Invalidation still bumps: the counter is the shared contract, and
	// a replica that DOES have MGET must see the bump.
	if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
		t.Fatalf("InvalidateUserPermissions: %v", err)
	}
	if got, err := mr.Get(authzUserGenPrefix + "u-1"); err != nil || got != "1" {
		t.Errorf("generation = %q (err %v), want \"1\"", got, err)
	}
}

func TestService_ImplementsAuthzCacheInvalidator(t *testing.T) {
	var _ iface.AuthzCacheInvalidator = (*Service)(nil)
}

// errMGetRedis is a client whose MGET fails while every other command
// still works: a transient timeout, a cluster MOVED, an ACL that covers
// GET but not MGET. Closing the whole server cannot express this — and
// a test that closes the server passes even if generations wrongly
// reported ok=true on an MGET error, because the follow-up GET fails
// too. This fake is what makes that mutation fail.
type errMGetRedis struct{ *database.RedisClientAdapter }

func (errMGetRedis) MGet(context.Context, ...string) ([]interface{}, error) {
	return nil, errors.New("MGET unavailable")
}

func TestCacheGet_UnreadableGenerationsAreAMissEvenWhenGetWorks(t *testing.T) {
	// The hazard: a user sitting at generation (0, N) whose MGET fails
	// while GET still works. If the failure read as generation (0, 0)
	// the lookup would resolve to the RETIRED gen-0 key — which is still
	// physically in Redis, because retired entries are never deleted —
	// and serve a revoked verdict. It has to be a miss.
	mr, adapter := startMiniredis(t)
	svc := &Service{
		repo:                newFakeRepo(),
		logger:              testLogger(t),
		userRoles:           staticRoleLookup(""),
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
	svc.setRedis(adapter, testLogger(t))
	ctx := context.Background()

	svc.cacheSet(ctx, "u-1", "t-1", []string{"revoked.perm"})
	retiredKey := svc.cacheKey(ctx, "u-1", "t-1")
	if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	// Fixture check: the retired entry is still physically present, so
	// a wrong generation really can reach it.
	if !mr.Exists(retiredKey) {
		t.Fatal("precondition: the retired entry must still be in Redis")
	}

	svc.setRedis(errMGetRedis{adapter}, testLogger(t))
	if got, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Fatalf("an unreadable generation resolved to the retired key and served %v", got)
	}
}

func TestCacheGet_IsOneMGetAndOneGet(t *testing.T) {
	// The single round trip is the premise of the whole
	// MultiGetRedisClient / setRedis apparatus: two GETs in place of the
	// MGET could read the global and the per-user counter at two
	// different moments and compose a key that never existed. Without
	// this assertion that substitution leaves the suite green.
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	rec := recordCommands(t, mr)
	rec.reset()

	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); !ok {
		t.Fatal("precondition: the entry must be readable")
	}
	if got := rec.count("MGET"); got != 1 {
		t.Errorf("MGET count = %d, want exactly 1: %v", got, rec.log())
	}
	if got := rec.count("GET"); got != 1 {
		t.Errorf("GET count = %d, want exactly 1 — two GETs would mean the counters were read separately: %v", got, rec.log())
	}
	if got := rec.count("KEYS"); got != 0 {
		t.Errorf("a KEYS scan was issued: %v", rec.log())
	}
}

// ===== the lost-invalidation race =====

// An INCR that lands while a reader is between its generation read and
// its write-back must not be republished by that reader. The reader
// computed its verdict BEFORE the bump; if it writes under the
// generation current at WRITE time, the pre-bump verdict becomes the
// live entry and is served for the full 60s TTL — the invalidation is
// lost even though the INCR succeeded.
//
// The fix is to compose the write key from the SAME generation pair the
// read used, so such an entry is born dead rather than born stale.
func TestGetEffectivePermissions_ConcurrentInvalidationIsNotRepublished(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	repo.seedRole("role-A", "reader", false, []string{"old.perm"}, "t-A")
	repo.seedBinding("bind-A", "u-1", "t-A", "role-A")

	// Fire the invalidation exactly once, on the reader's GET: that is
	// after it has read the generations and before it writes its verdict
	// back — the precise window the race lives in. The pre-hook runs
	// with no miniredis lock held, so calling back in is safe.
	var once sync.Once
	mr.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		if strings.ToUpper(cmd) == "GET" {
			once.Do(func() {
				if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
					t.Errorf("mid-flight invalidate: %v", err)
				}
			})
		}
		return false
	})

	perms, err := svc.GetEffectivePermissions(ctx, "u-1", "t-A")
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if len(perms) != 1 || perms[0] != "old.perm" {
		t.Fatalf("fixture: perms = %v, want [old.perm]", perms)
	}

	if got, ok := svc.cacheGet(ctx, "u-1", "t-A"); ok {
		t.Fatalf("LOST INVALIDATION: the pre-bump verdict %v is readable after the INCR and would be served for the full TTL", got)
	}
}

func TestGetEffectivePermissions_ReadsTheGenerationsOncePerCall(t *testing.T) {
	// A second MGET on the miss path is exactly the lost-invalidation
	// window above: it means the write key came from a later read of the
	// counters than the read key did. The miss path is MGET, GET, SET.
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	repo.seedRole("role-A", "reader", false, []string{"a.read"}, "t-A")
	repo.seedBinding("bind-A", "u-1", "t-A", "role-A")
	rec := recordCommands(t, mr)
	rec.reset()

	if _, err := svc.GetEffectivePermissions(ctx, "u-1", "t-A"); err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if got := rec.count("MGET"); got != 1 {
		t.Errorf("MGET count = %d on the miss path, want exactly 1: %v", got, rec.log())
	}
	if got := rec.count("SET"); got != 1 {
		t.Errorf("SET count = %d, want exactly 1: %v", got, rec.log())
	}

	// The hit path reads the counters once too, and writes nothing.
	rec.reset()
	if _, err := svc.GetEffectivePermissions(ctx, "u-1", "t-A"); err != nil {
		t.Fatalf("GetEffectivePermissions (hit): %v", err)
	}
	if got := rec.count("MGET"); got != 1 {
		t.Errorf("MGET count = %d on the hit path, want exactly 1: %v", got, rec.log())
	}
	if got := rec.count("SET"); got != 0 {
		t.Errorf("the hit path must not write: %v", rec.log())
	}
}

// ===== End-to-end: HasPermission writes to cache =====

func TestHasPermission_PopulatesCacheOnFirstCall(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup("operator"))
	ctx := context.Background()
	repo.seedRole("role-A", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.seedBinding("bind-A", "u-1", "tenant-A", "role-A")

	ok, err := svc.HasPermission(ctx, "u-1", "tenant-A", "billing.invoice.read")
	if err != nil || !ok {
		t.Fatalf("HasPermission: ok=%v err=%v", ok, err)
	}
	// Cache row should now exist with the resolved permission set.
	raw, err := mr.Get(svc.cacheKey(ctx, "u-1", "tenant-A"))
	if err != nil {
		t.Fatalf("expected cache row: %v", err)
	}
	var cached []string
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("cache row not JSON: %v", err)
	}
	if len(cached) == 0 {
		t.Errorf("expected non-empty cached perms, got %v", cached)
	}
}

// ===== Integration: CreateBinding invalidates the target's cache =====

func TestCreateBinding_InvalidatesTargetCache(t *testing.T) {
	// Pre-populate u-target's cache, then have a granter create a new
	// binding for u-target. The cache for u-target must be retired so the
	// next HasPermission call sees the freshly-granted permission.
	svc, repo, _ := newCacheTestService(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	// Stale cache entry from before the binding.
	svc.cacheSet(context.Background(), "u-target", "tenant-A", []string{"old-cache"})

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "u-target",
		RoleUUID: "role-X",
	})
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	if _, ok := svc.cacheGet(context.Background(), "u-target", "tenant-A"); ok {
		t.Errorf("u-target's stale cache entry must be invalidated after grant")
	}
}

// ===== Integration: DeleteBinding flushes cache =====

func TestDeleteBinding_FlushesEveryAuthzCache(t *testing.T) {
	// DeleteBinding can't tell which user the binding belonged to
	// without a lookup, so it conservatively retires the entire authz
	// cache. Pin that contract — narrowing it later would risk
	// stale-perm bugs.
	svc, repo, _ := newCacheTestService(t, staticRoleLookup(""))
	repo.seedBinding("b-1", "u-1", "tenant-A", "role")
	svc.cacheSet(context.Background(), "u-1", "tenant-A", []string{"old"})
	svc.cacheSet(context.Background(), "u-other", "tenant-A", []string{"keep-me-but-flushed"})

	if err := svc.DeleteBinding(context.Background(), "tenant-A", "b-1"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if _, ok := svc.cacheGet(context.Background(), "u-1", "tenant-A"); ok {
		t.Errorf("u-1 cache must be flushed")
	}
	// u-other's entry is also gone — wide-flush is the documented contract.
	if _, ok := svc.cacheGet(context.Background(), "u-other", "tenant-A"); ok {
		t.Errorf("DeleteBinding flushes the whole authz cache; u-other should be gone too")
	}
}

// ===== Integration: RemoveBindingsByTenant flushes only when bindings removed =====

func TestRemoveBindingsByTenant_NoMatch_DoesNotFlush(t *testing.T) {
	// When 0 bindings match (e.g. tenant never had any), the flush
	// should be skipped — no work to invalidate. This pins the
	// "if n > 0" guard.
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	svc.cacheSet(context.Background(), "u-1", "tenant-X", []string{"keep"})

	n, err := svc.RemoveBindingsByTenant(context.Background(), "tenant-NONEXISTENT")
	if err != nil {
		t.Fatalf("RemoveBindingsByTenant: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 removed, got %d", n)
	}
	if _, ok := svc.cacheGet(context.Background(), "u-1", "tenant-X"); !ok {
		t.Errorf("cache must NOT be flushed when no bindings were removed")
	}
}

func TestRemoveBindingsByTenant_FlushesWhenBindingsRemoved(t *testing.T) {
	svc, repo, _ := newCacheTestService(t, staticRoleLookup(""))
	repo.seedBinding("b-A", "u-1", "tenant-A", "role")
	svc.cacheSet(context.Background(), "u-1", "tenant-A", []string{"old"})

	n, err := svc.RemoveBindingsByTenant(context.Background(), "tenant-A")
	if err != nil {
		t.Fatalf("RemoveBindingsByTenant: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 removed, got %d", n)
	}
	if _, ok := svc.cacheGet(context.Background(), "u-1", "tenant-A"); ok {
		t.Errorf("cache must be flushed after bindings removed")
	}
}

// ===== Integration: DeleteRole flushes cache =====

func TestDeleteRole_FlushesCache(t *testing.T) {
	svc, repo, _ := newCacheTestService(t, staticRoleLookup(""))
	repo.seedRole("role-c", "x", false, []string{"x.read"}, "tenant-A")
	svc.cacheSet(context.Background(), "u-1", "tenant-A", []string{"old"})

	if err := svc.DeleteRole(context.Background(), "tenant-A", "role-c"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if _, ok := svc.cacheGet(context.Background(), "u-1", "tenant-A"); ok {
		t.Errorf("DeleteRole must flush the cache")
	}
}

// ===== Integration: HasPermission cache hit short-circuits the repo =====

func TestHasPermission_CacheHit_BypassesRepoLookup(t *testing.T) {
	// Pre-load the cache with a known perm; the repo has NO bindings
	// for this user. If the cache is consulted first the call returns
	// true; otherwise it falls back to the repo and returns false.
	// This pins the cache-first read order.
	svc, _, _ := newCacheTestService(t, staticRoleLookup("operator"))
	svc.cacheSet(context.Background(), "u-cache", "tenant-A", []string{"cached.perm"})

	ok, err := svc.HasPermission(context.Background(), "u-cache", "tenant-A", "cached.perm")
	if err != nil {
		t.Fatalf("HasPermission: %v", err)
	}
	if !ok {
		t.Errorf("cached perm must be honoured without consulting the repo")
	}
}

// ===== withGeneration (D27) =====
//
// withGeneration is the invalidation CONTRACT: pre-invalidate → write →
// post-invalidate. The pre step is a gate — a counter the store cannot
// bump means the change's effect cannot be guaranteed, so nothing is
// written. The post step retires a repopulation that landed during the
// write; its failure is logged, counted, and non-fatal.

// assertNoGenerationBump fails when either counter moved. A mutation
// that was REFUSED must retire nothing: bumping before a request that is
// about to be rejected turns a 403/404 into a remotely triggerable cache
// flush (ruling P9). Reads both the wire (no INCR crossed it) and the
// store (no counter key exists), because the two can only disagree if
// the recorder is wired wrong.
func assertNoGenerationBump(t *testing.T, mr *miniredis.Miniredis, rec *cmdRecorder) {
	t.Helper()
	if n := rec.count("INCR"); n != 0 {
		t.Errorf("a refused mutation issued %d INCR(s): %v", n, rec.log())
	}
	if mr.Exists(authzGlobalGenKey) {
		t.Error("the global generation was bumped by a refused mutation")
	}
	for _, k := range mr.Keys() {
		if strings.HasPrefix(k, authzUserGenPrefix) {
			t.Errorf("per-user generation %q was bumped by a refused mutation", k)
		}
	}
}

func TestWithGeneration_PreInvalidationFailureRefusesTheWrite(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	mr.Close()
	written := false

	err := svc.withGeneration(context.Background(), userScope("u-1"), func() error {
		written = true
		return nil
	})
	if err == nil {
		t.Fatal("a pre-invalidation failure must refuse the mutation")
	}
	if !errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Errorf("err = %v, want it to wrap ErrAuthzCacheUnavailable", err)
	}
	if written {
		t.Fatal("the mutation must not run when the pre-invalidation failed")
	}
}

// The post step retires an entry repopulated by a concurrent read
// between the pre-invalidation and the write — that read stored the OLD
// verdict under the NEW generation.
func TestWithGeneration_PostInvalidationRetiresARepopulation(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()

	err := svc.withGeneration(ctx, userScope("u-1"), func() error {
		// Stand in for the racing read that repopulates with the old answer.
		svc.cacheSet(ctx, "u-1", "t-1", []string{"stale"})
		return nil
	})
	if err != nil {
		t.Fatalf("withGeneration: %v", err)
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Fatal("the post-invalidation must retire an entry repopulated during the write")
	}
}

// A post-invalidation failure is NOT fatal: the write already landed, so
// refusing it after the fact would lie to the caller. The stale entry
// dies within its own 60s TTL.
func TestWithGeneration_PostInvalidationFailureIsNonFatal(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	written := false

	err := svc.withGeneration(context.Background(), globalScope(), func() error {
		written = true
		mr.Close() // the store dies between the write and the post step
		return nil
	})
	if err != nil {
		t.Fatalf("a post-invalidation failure must not fail the mutation, got %v", err)
	}
	if !written {
		t.Fatal("precondition: the mutation must have run")
	}
}

func TestWithGeneration_MutationErrorPropagates(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	sentinel := errors.New("write failed")
	err := svc.withGeneration(context.Background(), userScope("u-1"), func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the mutation's own error", err)
	}
	if errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Error("a mutation failure must not be reported as a cache failure")
	}
}

// "No cache configured" is not "cache unavailable": with no Redis the
// gate has nothing to guard, so the mutation must still run.
func TestWithGeneration_NilRedisRunsTheMutation(t *testing.T) {
	svc := &Service{}
	written := false
	if err := svc.withGeneration(context.Background(), globalScope(), func() error {
		written = true
		return nil
	}); err != nil {
		t.Fatalf("a nil Redis must not refuse the mutation, got %v", err)
	}
	if !written {
		t.Fatal("the mutation must run when no cache is configured")
	}
}

func TestWithGeneration_ScopeSelectsTheCounter(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()

	if err := svc.withGeneration(ctx, userScope("u-1"), func() error { return nil }); err != nil {
		t.Fatalf("withGeneration(user): %v", err)
	}
	if mr.Exists(authzGlobalGenKey) {
		t.Error("a user-scoped mutation must not bump the global counter")
	}
	if got, _ := mr.Get(authzUserGenPrefix + "u-1"); got != "2" {
		t.Errorf("user generation = %q, want \"2\" (pre + post)", got)
	}

	if err := svc.withGeneration(ctx, globalScope(), func() error { return nil }); err != nil {
		t.Fatalf("withGeneration(global): %v", err)
	}
	if got, _ := mr.Get(authzGlobalGenKey); got != "2" {
		t.Errorf("global generation = %q, want \"2\" (pre + post)", got)
	}
}

// ===== P9: a REFUSED mutation retires nothing =====

func TestUpdateRole_SystemRoleRefusalBumpsNothing(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"system.users.admin"}, "")
	rec := recordCommands(t, mr)

	_, err := svc.UpdateRole(context.Background(), "", "role-sys", granterSystem, models.UpdateRoleInput{
		Name: strPtr("renamed"),
	})
	if !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("err = %v, want ErrSystemRoleImmutable", err)
	}
	assertNoGenerationBump(t, mr, rec)
}

func TestUpdateRole_CrossTenantRefusalBumpsNothing(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	registerTestPermissions(t, svc, registered("billing.invoice.read"))
	repo.seedRole("role-b", "reader", false, []string{"billing.invoice.read"}, "tenant-B")
	rec := recordCommands(t, mr)

	// tenant-A asking to edit tenant-B's role: 404, and nothing retired.
	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-b", granterSystem, models.UpdateRoleInput{
		Permissions: []string{"billing.invoice.read"},
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want repository.ErrNotFound", err)
	}
	assertNoGenerationBump(t, mr, rec)
}

func TestUpdateRole_UnknownPermissionRefusalBumpsNothing(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	registerTestPermissions(t, svc, registered("billing.invoice.read"))
	repo.seedRole("role-c", "reader", false, []string{"billing.invoice.read"}, "tenant-A")
	rec := recordCommands(t, mr)

	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{
		Permissions: []string{"billing.invoice.nope"},
	})
	if !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("err = %v, want ErrUnknownPermission", err)
	}
	assertNoGenerationBump(t, mr, rec)
}

// A patch that changes no field never reaches the repo, so it must not
// retire anything either.
func TestUpdateRole_NoOpPatchBumpsNothing(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedRole("role-c", "reader", false, []string{"billing.invoice.read"}, "tenant-A")
	rec := recordCommands(t, mr)

	if _, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{}); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	assertNoGenerationBump(t, mr, rec)
}

func TestDeleteRole_SystemRoleRefusalBumpsNothing(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"system.users.admin"}, "")
	rec := recordCommands(t, mr)

	if err := svc.DeleteRole(context.Background(), "", "role-sys"); !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("err = %v, want ErrSystemRoleImmutable", err)
	}
	assertNoGenerationBump(t, mr, rec)
}

func TestCreateBinding_RefusedGrantBumpsNothing(t *testing.T) {
	// The cascade rule refuses the grant (403). Nothing is written, so
	// nothing may be retired.
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	rec := recordCommands(t, mr)

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter-with-nothing", models.CreateBindingInput{
		UserUUID: "u-target",
		RoleUUID: "role-X",
	})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("err = %v, want ErrInsufficientPermissionsToGrant", err)
	}
	assertNoGenerationBump(t, mr, rec)
}

// ===== the gate: a cache that cannot be bumped refuses the write =====

func TestUpdateRole_CacheUnavailableRefusesTheWrite(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	registerTestPermissions(t, svc, registered("billing.invoice.read", "billing.invoice.refund"))
	repo.seedRole("role-c", "reader", false, []string{"billing.invoice.read"}, "tenant-A")
	mr.Close()

	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{
		Permissions: []string{"billing.invoice.read", "billing.invoice.refund"},
	})
	if !errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Fatalf("err = %v, want ErrAuthzCacheUnavailable", err)
	}
	role, getErr := repo.GetRoleByUUID(context.Background(), "role-c")
	if getErr != nil {
		t.Fatalf("GetRoleByUUID: %v", getErr)
	}
	if len(role.Permissions) != 1 {
		t.Errorf("permissions = %v, want the write to have been refused", role.Permissions)
	}
}

func TestCreateBinding_CacheUnavailableRefusesTheGrant(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	mr.Close()

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "u-target",
		RoleUUID: "role-X",
	})
	if !errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Fatalf("err = %v, want ErrAuthzCacheUnavailable", err)
	}
	if len(repo.bindings) != 0 {
		t.Errorf("bindings = %v, want the grant to have been refused", repo.bindings)
	}
}

func TestDeleteRole_CacheUnavailableRefusesTheDelete(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedRole("role-c", "reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.seedBinding("b-1", "u-1", "tenant-A", "reader")
	mr.Close()

	err := svc.DeleteRole(context.Background(), "tenant-A", "role-c")
	if !errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Fatalf("err = %v, want ErrAuthzCacheUnavailable", err)
	}
	if _, ok := repo.roles["role-c"]; !ok {
		t.Error("the role must survive a refused delete")
	}
	if _, ok := repo.bindings["b-1"]; !ok {
		t.Error("the binding cascade must not run when the delete is refused")
	}
}

// The cascade hooks learn how many rows they removed only from the write
// itself, so the "if n > 0" guard (TestRemoveBindingsByTenant_NoMatch_
// DoesNotFlush) leaves them with the post-write half alone. Its failure
// is RETURNED — the tenant module's hooks propagate it — never logged
// and swallowed.
func TestRemoveBindingsByTenant_CacheUnavailableIsReported(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedBinding("b-A", "u-1", "tenant-A", "role")
	mr.Close()

	n, err := svc.RemoveBindingsByTenant(context.Background(), "tenant-A")
	if !errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Fatalf("err = %v, want ErrAuthzCacheUnavailable", err)
	}
	if n != 1 {
		t.Errorf("n = %d, want the removal count to still be reported", n)
	}
}

func TestRemoveBindingsByUserAndTenant_CacheUnavailableIsReported(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup(""))
	repo.seedBinding("b-A", "u-1", "tenant-A", "role")
	mr.Close()

	n, err := svc.RemoveBindingsByUserAndTenant(context.Background(), "u-1", "tenant-A")
	if !errors.Is(err, ErrAuthzCacheUnavailable) {
		t.Fatalf("err = %v, want ErrAuthzCacheUnavailable", err)
	}
	if n != 1 {
		t.Errorf("n = %d, want the removal count to still be reported", n)
	}
}

// DeleteBinding has no pre-write guard of its own — the repository
// discovers the miss — so the gate's pre-bump runs before the row is
// known to exist, and an unmatched delete retires the cache once. The
// residual is bounded: the caller already holds authz.binding.delete in
// the resolved tenant (assertTenantScope pins path == resolved), and the
// cost is a cold cache, never a wrong verdict. Pinned so a future reader
// sees it is deliberate, not an oversight.
func TestDeleteBinding_UnmatchedDeleteStillReturnsNotFound(t *testing.T) {
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	rec := recordCommands(t, mr)

	err := svc.DeleteBinding(context.Background(), "tenant-A", "b-nonexistent")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want repository.ErrNotFound", err)
	}
	if got := rec.count("INCR"); got != 1 {
		t.Errorf("INCR count = %d, want exactly the single pre-invalidation", got)
	}
}

func strPtr(s string) *string { return &s }
