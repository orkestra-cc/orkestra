package blob

// CachedStore's presigned-GET cache is keyed on the object key alone, so
// two callers presigning the SAME key at different TTLs share one entry.
// Before the TTL was recorded in the value, whichever call ran first
// decided the other's lifetime: a caller asking for a short-lived URL
// could be handed a long-lived one, silently extending the revocation
// window it was relying on. These tests pin the rule that fixes it —
// never serve a cached URL signed for longer than the caller asked —
// and pin that fixing it did NOT cost the single-DEL invalidation the
// cache depends on (enumerating keys to invalidate is the mistake
// authz's generation-keyed cache exists to avoid).

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// recordingStore is an inner Store that mints a distinguishable URL per
// presign and counts how many times it was asked.
type recordingStore struct {
	presignGets int
	lastTTL     time.Duration
	deleted     []string
	put         []string
	presignErr  error
}

func (r *recordingStore) PresignPut(context.Context, string, string, int64, time.Duration) (*PresignedPut, error) {
	return &PresignedPut{}, nil
}
func (r *recordingStore) Put(_ context.Context, key, _ string, _ io.Reader) error {
	r.put = append(r.put, key)
	return nil
}
func (r *recordingStore) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	if r.presignErr != nil {
		return "", r.presignErr
	}
	r.presignGets++
	r.lastTTL = ttl
	// The signature number makes each mint distinguishable, and the ttl
	// makes the served lifetime readable straight off the URL.
	return "https://signed/" + key + "?n=" + strconv.Itoa(r.presignGets) + "&ttl=" + ttl.String(), nil
}
func (r *recordingStore) Delete(_ context.Context, key string) error {
	r.deleted = append(r.deleted, key)
	return nil
}
func (r *recordingStore) Exists(context.Context, string) (bool, error) { return true, nil }

// newTestCache wires a CachedStore over miniredis. SignedGetTTL is 1h so
// the default path matches production's avatar store.
func newTestCache(t *testing.T) (*CachedStore, *recordingStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	inner := &recordingStore{}
	store := NewCached(inner, rdb, CachedConfig{SignedGetTTL: time.Hour, CacheBuffer: 10 * time.Minute, KeyPrefix: "blob:url:test:"})
	cached, ok := store.(*CachedStore)
	if !ok {
		t.Fatalf("NewCached returned %T, want *CachedStore", store)
	}
	return cached, inner, mr
}

func TestPresignGetReusesACachedURLForTheSameTTL(t *testing.T) {
	c, inner, _ := newTestCache(t)
	ctx := context.Background()

	first, err := c.PresignGet(ctx, "k", time.Hour)
	if err != nil {
		t.Fatalf("first presign: %v", err)
	}
	second, err := c.PresignGet(ctx, "k", time.Hour)
	if err != nil {
		t.Fatalf("second presign: %v", err)
	}
	if first != second {
		t.Fatalf("same TTL must reuse the cached URL: %q then %q", first, second)
	}
	if inner.presignGets != 1 {
		t.Fatalf("inner presigns = %d, want 1 — the cache did not serve the second call", inner.presignGets)
	}
}

// The regression this file exists for: a 15-minute caller must never be
// handed the 1-hour URL an operator preview left in the cache.
func TestPresignGetNeverServesAURLSignedForLongerThanRequested(t *testing.T) {
	c, inner, _ := newTestCache(t)
	ctx := context.Background()

	long, err := c.PresignGet(ctx, "k", time.Hour)
	if err != nil {
		t.Fatalf("long presign: %v", err)
	}
	short, err := c.PresignGet(ctx, "k", 15*time.Minute)
	if err != nil {
		t.Fatalf("short presign: %v", err)
	}
	if short == long {
		t.Fatal("a 15m caller was served the cached 1h URL — the revocation window is silently extended")
	}
	if inner.presignGets != 2 {
		t.Fatalf("inner presigns = %d, want 2 — the short caller must re-presign", inner.presignGets)
	}
	if inner.lastTTL != 15*time.Minute {
		t.Fatalf("inner was asked for %s, want 15m", inner.lastTTL)
	}
}

// The safe direction: a shorter cached URL satisfies a longer request.
// The caller just re-presigns sooner than it strictly had to.
func TestPresignGetReusesAShorterCachedURLForALongerRequest(t *testing.T) {
	c, inner, _ := newTestCache(t)
	ctx := context.Background()

	short, err := c.PresignGet(ctx, "k", 15*time.Minute)
	if err != nil {
		t.Fatalf("short presign: %v", err)
	}
	long, err := c.PresignGet(ctx, "k", time.Hour)
	if err != nil {
		t.Fatalf("long presign: %v", err)
	}
	if long != short {
		t.Fatalf("a shorter cached URL is safe to reuse: %q then %q", short, long)
	}
	if inner.presignGets != 1 {
		t.Fatalf("inner presigns = %d, want 1", inner.presignGets)
	}
}

// After the short caller re-presigns, the cache holds the SHORT entry —
// it converges on the most conservative TTL in use rather than flipping
// back and forth between the two callers.
func TestPresignGetCacheConvergesOnTheShortestTTL(t *testing.T) {
	c, inner, _ := newTestCache(t)
	ctx := context.Background()

	if _, err := c.PresignGet(ctx, "k", time.Hour); err != nil {
		t.Fatalf("long presign: %v", err)
	}
	short, err := c.PresignGet(ctx, "k", 15*time.Minute)
	if err != nil {
		t.Fatalf("short presign: %v", err)
	}
	// Both callers now reuse the 15m entry: no further minting either way.
	for i, ttl := range []time.Duration{time.Hour, 15 * time.Minute, time.Hour} {
		got, err := c.PresignGet(ctx, "k", ttl)
		if err != nil {
			t.Fatalf("presign %d: %v", i, err)
		}
		if got != short {
			t.Fatalf("presign %d (%s) = %q, want the converged %q", i, ttl, got, short)
		}
	}
	if inner.presignGets != 2 {
		t.Fatalf("inner presigns = %d, want 2 (one per distinct TTL, then steady state)", inner.presignGets)
	}
}

// An entry written by a build that stored a bare URL must not be served
// as if its lifetime were known. It is re-minted and overwritten, so the
// cache self-heals without an operator flushing Redis.
func TestPresignGetIgnoresALegacyBareURLEntry(t *testing.T) {
	c, inner, mr := newTestCache(t)
	ctx := context.Background()

	if err := mr.Set("blob:url:test:k", "https://legacy/url"); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}
	got, err := c.PresignGet(ctx, "k", time.Hour)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if got == "https://legacy/url" {
		t.Fatal("a legacy entry carries no TTL and must not be served")
	}
	if inner.presignGets != 1 {
		t.Fatalf("inner presigns = %d, want 1", inner.presignGets)
	}
	if raw, _ := mr.Get("blob:url:test:k"); raw == "https://legacy/url" {
		t.Fatal("the legacy entry must be overwritten so the cache self-heals")
	}
}

// The whole point of keeping the TTL in the VALUE: one object key still
// maps to one entry, so every invalidation path stays a single DEL and
// none of them can leave a variant behind.
func TestInvalidationPathsStillDropTheSingleEntry(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(c *CachedStore) error{
		"Delete":        func(c *CachedStore) error { return c.Delete(ctx, "k") },
		"Put":           func(c *CachedStore) error { return c.Put(ctx, "k", "image/png", nil) },
		"InvalidateGet": func(c *CachedStore) error { return c.InvalidateGet(ctx, "k") },
	}
	for name, invalidate := range cases {
		t.Run(name, func(t *testing.T) {
			c, inner, mr := newTestCache(t)
			// Two different TTLs, so if the fix had introduced per-TTL
			// cache keys this would leave one behind.
			if _, err := c.PresignGet(ctx, "k", time.Hour); err != nil {
				t.Fatalf("presign 1h: %v", err)
			}
			if _, err := c.PresignGet(ctx, "k", 15*time.Minute); err != nil {
				t.Fatalf("presign 15m: %v", err)
			}
			if err := invalidate(c); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if keys := mr.Keys(); len(keys) != 0 {
				t.Fatalf("%s left %v cached, want none", name, keys)
			}
			before := inner.presignGets
			if _, err := c.PresignGet(ctx, "k", 15*time.Minute); err != nil {
				t.Fatalf("presign after %s: %v", name, err)
			}
			if inner.presignGets != before+1 {
				t.Fatalf("%s: the next presign must hit the origin", name)
			}
		})
	}
}

// A presign failure must not poison the cache with an empty entry.
func TestPresignGetDoesNotCacheAFailure(t *testing.T) {
	c, inner, mr := newTestCache(t)
	ctx := context.Background()
	inner.presignErr = errors.New("boom")

	if _, err := c.PresignGet(ctx, "k", time.Hour); err == nil {
		t.Fatal("expected the inner error to propagate")
	}
	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("cached %v after a failed presign, want nothing", keys)
	}
}

func TestCachedGetEntryRoundTrips(t *testing.T) {
	raw := encodeCachedGet(15*time.Minute, "https://signed/x?a=b|c")
	ttl, url, ok := decodeCachedGet(raw)
	if !ok || ttl != 15*time.Minute || url != "https://signed/x?a=b|c" {
		t.Fatalf("round trip = (%s, %q, %v)", ttl, url, ok)
	}
	for _, bad := range []string{"", "https://no-ttl", "|https://empty-ttl", "abc|https://nan", "-5|https://negative"} {
		if _, _, ok := decodeCachedGet(bad); ok {
			t.Fatalf("decodeCachedGet(%q) = ok, want refused", bad)
		}
	}
}

// CachedConfig.CacheBuffer was validated by NewCached and then dropped on
// the floor — PresignGet subtracted a hardcoded 10 minutes, so configuring
// any other buffer silently did nothing. Both production call sites happen
// to pass 10m, which is why nothing had noticed.
func TestCacheEntryTTLHonoursTheConfiguredBuffer(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	store := NewCached(&recordingStore{}, rdb, CachedConfig{
		SignedGetTTL: time.Hour,
		CacheBuffer:  45 * time.Minute, // deliberately not the old hardcoded 10m
		KeyPrefix:    "blob:url:test:",
	})
	if _, err := store.PresignGet(context.Background(), "k", time.Hour); err != nil {
		t.Fatalf("presign: %v", err)
	}
	if got := mr.TTL("blob:url:test:k"); got != 15*time.Minute {
		t.Fatalf("cache entry TTL = %s, want 15m (1h signed − 45m buffer)", got)
	}
}
