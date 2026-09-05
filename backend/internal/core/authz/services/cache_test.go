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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/internal/core/authz/models"
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

func TestInvalidateUserPermissions_NoOpWhenUserHasNoEntries(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	// A user with nothing cached still bumps cleanly — no scan means
	// there is no "nothing to delete" branch to get wrong.
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

func TestCache_ClientWithoutMGetBypassesTheCacheEntirely(t *testing.T) {
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
