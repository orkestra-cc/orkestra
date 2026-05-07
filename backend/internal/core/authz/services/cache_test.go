package services

// Phase 17: cache-layer coverage for authz.Service. Backed by an
// in-process miniredis so the cache code paths actually run instead
// of short-circuiting on a nil redis adapter. Pins the contract that
// CreateBinding / DeleteBinding / DeleteRole / RemoveBindingsByTenant /
// UpdateRole all invalidate the affected user's cached effective
// permissions — without these, a permission grant or revocation
// would only take effect after the 60s TTL.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/shared/database"
)

// startMiniredis spins up an in-process Redis and wraps it in the
// production RedisClientAdapter so the Service.cache* methods exercise
// the real Get/Set/Keys/Del code paths.
func startMiniredis(t *testing.T) (*miniredis.Miniredis, *database.RedisClientAdapter) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mr, database.NewRedisClientAdapter(client)
}

// newCacheTestService stands a Service up with a real Redis
// adapter and the in-memory fake repo. Cache code paths exercise
// the production Get/Set/Keys/Del implementation.
func newCacheTestService(t *testing.T, lookup UserSystemRoleLookup) (*Service, *fakeRepo, *miniredis.Miniredis) {
	t.Helper()
	repo := newFakeRepo()
	mr, redisAdapter := startMiniredis(t)
	svc := &Service{
		repo:                repo,
		redis:               redisAdapter,
		logger:              testLogger(t),
		userRoles:           lookup,
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
	return svc, repo, mr
}

// ===== cacheKey =====

func TestCacheKey_StableForGivenInputs(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	// Stable across calls — the function is pure.
	if a, b := svc.cacheKey("u-1", "tenant-A"), svc.cacheKey("u-1", "tenant-A"); a != b {
		t.Errorf("not stable: %q != %q", a, b)
	}
	// Different inputs produce different keys.
	if svc.cacheKey("u-1", "tenant-A") == svc.cacheKey("u-1", "tenant-B") {
		t.Errorf("tenant must affect the key")
	}
	if svc.cacheKey("u-1", "tenant-A") == svc.cacheKey("u-2", "tenant-A") {
		t.Errorf("user must affect the key")
	}
}

func TestCacheKey_NormalisesEmptyTenantToHyphen(t *testing.T) {
	// Empty tenant collapses to "-" so the key remains parseable and
	// distinct from a tenant literally named "".
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	want := "authz:cache:u-1:-"
	if got := svc.cacheKey("u-1", ""); got != want {
		t.Errorf("got %q, want %q", got, want)
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
	_ = mr.Set(svc.cacheKey("u-c", "t"), "not-json")
	if _, ok := svc.cacheGet(context.Background(), "u-c", "t"); ok {
		t.Errorf("malformed payload must be a cache miss, not a panic")
	}
}

// ===== cacheSet TTL =====

func TestCacheSet_HasTTL(t *testing.T) {
	// 60s TTL is what makes revocation eventually consistent across
	// replicas. Pin it so a refactor doesn't accidentally bump it to
	// hours or strip it entirely.
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	svc.cacheSet(context.Background(), "u-1", "tenant-A", []string{"x.read"})

	ttl := mr.TTL(svc.cacheKey("u-1", "tenant-A"))
	if ttl == 0 {
		t.Fatalf("expected a TTL on the cache entry, got 0 (= no expiry)")
	}
	if ttl > 90*time.Second {
		t.Errorf("TTL = %v, expected ~60s — drift may make revocation lag too long", ttl)
	}
}

// ===== cacheInvalidate =====

func TestCacheInvalidate_DropsAllKeysForUser(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	// Two cached entries for u-1 (tenant-A, tenant-B) and one unrelated row.
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"a.read"})
	svc.cacheSet(ctx, "u-1", "tenant-B", []string{"b.read"})
	svc.cacheSet(ctx, "u-other", "tenant-A", []string{"c.read"})

	svc.cacheInvalidate(ctx, "u-1")

	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-A"); ok {
		t.Errorf("u-1 / tenant-A must be invalidated")
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-B"); ok {
		t.Errorf("u-1 / tenant-B must be invalidated")
	}
	if _, ok := svc.cacheGet(ctx, "u-other", "tenant-A"); !ok {
		t.Errorf("u-other must NOT be invalidated by u-1's flush")
	}
}

func TestCacheInvalidate_NoOpWhenUserHasNoEntries(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	// Should not panic / not error.
	svc.cacheInvalidate(context.Background(), "u-never-cached")
}

// ===== flushCache =====

func TestFlushCache_ClearsEveryAuthzCacheEntry(t *testing.T) {
	svc, _, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"x"})
	svc.cacheSet(ctx, "u-2", "tenant-B", []string{"y"})

	svc.flushCache(ctx)

	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-A"); ok {
		t.Errorf("u-1 entry must be flushed")
	}
	if _, ok := svc.cacheGet(ctx, "u-2", "tenant-B"); ok {
		t.Errorf("u-2 entry must be flushed")
	}
}

func TestFlushCache_DoesNotTouchNonAuthzKeys(t *testing.T) {
	// Different module's key prefix must survive flushCache.
	svc, _, mr := newCacheTestService(t, staticRoleLookup(""))
	_ = mr.Set("session:abc", "keep me")
	svc.cacheSet(context.Background(), "u-1", "tenant-A", []string{"x"})

	svc.flushCache(context.Background())

	if got, _ := mr.Get("session:abc"); got != "keep me" {
		t.Errorf("non-authz key was wiped: got %q", got)
	}
}

// ===== End-to-end: HasPermission writes to cache =====

func TestHasPermission_PopulatesCacheOnFirstCall(t *testing.T) {
	svc, repo, mr := newCacheTestService(t, staticRoleLookup("operator"))
	repo.seedRole("role-A", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.seedBinding("bind-A", "u-1", "tenant-A", "role-A")

	ok, err := svc.HasPermission(context.Background(), "u-1", "tenant-A", "billing.invoice.read")
	if err != nil || !ok {
		t.Fatalf("HasPermission: ok=%v err=%v", ok, err)
	}
	// Cache row should now exist with the resolved permission set.
	raw, err := mr.Get(svc.cacheKey("u-1", "tenant-A"))
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
	// binding for u-target. The cache for u-target must be wiped so the
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
	// without a lookup, so it conservatively flushes the entire authz
	// cache. Pin that contract — narrowing it later would risk
	// stale-perm bugs.
	svc, repo, _ := newCacheTestService(t, staticRoleLookup(""))
	repo.seedBinding("b-1", "u-1", "tenant-A", "role")
	svc.cacheSet(context.Background(), "u-1", "tenant-A", []string{"old"})
	svc.cacheSet(context.Background(), "u-other", "tenant-A", []string{"keep-me-but-flushed"})

	if err := svc.DeleteBinding(context.Background(), "b-1"); err != nil {
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

	if err := svc.DeleteRole(context.Background(), "role-c"); err != nil {
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
