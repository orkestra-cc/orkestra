package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func newStateStore(t *testing.T, r *memRedis) *SIWCStateStore {
	t.Helper()
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return NewSIWCStateStore(r, v)
}

func testAttempt() SIWCAttempt {
	return SIWCAttempt{
		UserUUID: "u1", TenantID: "t1", Label: "Work", HostID: "host-1",
		RedirectURI: "http://127.0.0.1:14555/v1/llm/accounts/openai/callback",
		Verifier:    "verifier-secret-value", Nonce: "nonce-secret-value",
		Mode: SIWCModeLoopbackLocal, ClientID: SIWCDynamicClientID, AccountUUID: "acc-1",
	}
}

func TestSIWCState_OneShotRoundTrip(t *testing.T) {
	store := newStateStore(t, newMemRedis())
	ctx := context.Background()
	if err := store.Put(ctx, "state-1", testAttempt()); err != nil {
		t.Fatal(err)
	}
	got, err := store.Take(ctx, "state-1")
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	want := testAttempt()
	got.CreatedAt = time.Time{}
	if *got != want {
		t.Fatalf("attempt did not round-trip every bound field")
	}
	if _, err := store.Take(ctx, "state-1"); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("second take err = %v", err)
	}
	if _, err := store.Take(ctx, "never-put"); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("unknown take err = %v", err)
	}
	if _, err := store.Take(ctx, ""); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("empty take err = %v", err)
	}
}

func TestSIWCState_ConcurrentTakesHaveOneWinner(t *testing.T) {
	for name, mk := range map[string]func() module.RedisClient{
		"incr-expire": func() module.RedisClient { return newMemRedis() },
		"script":      func() module.RedisClient { return &evalRedis{memRedis: newMemRedis()} },
	} {
		t.Run(name, func(t *testing.T) {
			v, _ := NewVault(randomKeyHex(t), slog.Default())
			store := NewSIWCStateStore(mk(), v)
			if err := store.Put(context.Background(), "s", testAttempt()); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			wins := make(chan struct{}, 10)
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := store.Take(context.Background(), "s"); err == nil {
						wins <- struct{}{}
					}
				}()
			}
			wg.Wait()
			close(wins)
			if n := len(wins); n != 1 {
				t.Fatalf("winners = %d", n)
			}
		})
	}
}

func TestSIWCState_NothingSecretReadableInRedis(t *testing.T) {
	// Long, distinctive sentinels: a short marker can occur by chance in
	// base64 ciphertext. The tenant is stored in clear by design and is
	// not searched for.
	const (
		sState    = "sentinel-state-7f3a9c1e5b2d4086a1c3e5f7"
		sUser     = "sentinel-user-0b8d6f4a2c9e1357b3d5f7a9"
		sLabel    = "sentinel-label-4e6a8c0d2f1b3957a5c7e9b1"
		sHost     = "sentinel-host-9d7b5f3a1c8e6024b4d6f8a0"
		sVerifier = "sentinel-verifier-2a4c6e8f0b1d3597c5e7a9b1d3f5"
		sNonce    = "sentinel-nonce-6c8e0a2d4f1b3795d7f9b1c3e5a7"
		sAccount  = "sentinel-account-8b0d2f4a6c1e3579e1a3c5d7f9b0"
	)
	r := newMemRedis()
	store := newStateStore(t, r)
	a := testAttempt()
	a.UserUUID, a.Label, a.HostID, a.Verifier, a.Nonce, a.AccountUUID = sUser, sLabel, sHost, sVerifier, sNonce, sAccount
	if err := store.Put(context.Background(), sState, a); err != nil {
		t.Fatal(err)
	}
	if len(r.vals) != 1 {
		t.Fatalf("redis entries = %d, want 1", len(r.vals))
	}
	for k, v := range r.vals {
		for _, secret := range []string{sState, sUser, sLabel, sHost, sVerifier, sNonce, sAccount} {
			if strings.Contains(k+v, secret) {
				t.Fatal("a redis entry exposes a bound value")
			}
		}
		if r.ttl[k] != siwcStateTTL {
			t.Fatalf("%s ttl = %v, want %v", k, r.ttl[k], siwcStateTTL)
		}
	}
}

func TestSIWCState_SealedUnderTheAttemptTenantNotAFakeOne(t *testing.T) {
	v, _ := NewVault(randomKeyHex(t), slog.Default())
	kms := &fakeKMS{keys: map[string]bool{}}
	v.SetKMSProvider(kms)
	store := NewSIWCStateStore(newMemRedis(), v)
	if err := store.Put(context.Background(), "s", testAttempt()); err != nil {
		t.Fatal(err)
	}
	if len(kms.keys) != 1 || !kms.keys["k-t1"] {
		t.Fatalf("kms keys = %v, want only the attempt tenant's", kms.keys)
	}
	if _, err := store.Take(context.Background(), "s"); err != nil {
		t.Fatalf("take under kms: %v", err)
	}
}

func TestSIWCState_SecretKeyMissingIsDistinguishable(t *testing.T) {
	v, _ := NewVault("", slog.Default())
	store := NewSIWCStateStore(newMemRedis(), v)
	if err := store.Put(context.Background(), "s", testAttempt()); !errors.Is(err, ErrSecretKeyMissing) {
		t.Fatalf("put err = %v, want ErrSecretKeyMissing", err)
	}
	// An attempt sealed while a key existed, taken after it is gone: the
	// key problem must stay visible, not become "state invalid".
	r := newMemRedis()
	v2, _ := NewVault(randomKeyHex(t), slog.Default())
	if err := NewSIWCStateStore(r, v2).Put(context.Background(), "s", testAttempt()); err != nil {
		t.Fatal(err)
	}
	v3, _ := NewVault("", slog.Default())
	if _, err := NewSIWCStateStore(r, v3).Take(context.Background(), "s"); !errors.Is(err, ErrSecretKeyMissing) {
		t.Fatalf("take err = %v, want ErrSecretKeyMissing", err)
	}
}

func TestSIWCState_FailsClosedWithoutRedisOrOnStoreError(t *testing.T) {
	v, _ := NewVault(randomKeyHex(t), slog.Default())
	none := NewSIWCStateStore(nil, v)
	if err := none.Put(context.Background(), "s", testAttempt()); !errors.Is(err, ErrSIWCStateUnavailable) {
		t.Fatalf("put without redis = %v", err)
	}
	if _, err := none.Take(context.Background(), "s"); !errors.Is(err, ErrSIWCStateUnavailable) {
		t.Fatalf("take without redis = %v", err)
	}
	var nilStore *SIWCStateStore
	if _, err := nilStore.Take(context.Background(), "s"); !errors.Is(err, ErrSIWCStateUnavailable) {
		t.Fatalf("nil store = %v", err)
	}

	r := newMemRedis()
	store := NewSIWCStateStore(r, v)
	if err := store.Put(context.Background(), "s", testAttempt()); err != nil {
		t.Fatal(err)
	}
	r.incrErr = errors.New("down")
	if _, err := store.Take(context.Background(), "s"); !errors.Is(err, ErrSIWCStateUnavailable) {
		t.Fatalf("take with failing marker = %v", err)
	}
	r.incrErr = nil
	r.expireErr = errors.New("down")
	if _, err := store.Take(context.Background(), "s"); !errors.Is(err, ErrSIWCStateUnavailable) {
		t.Fatalf("take with failing expiry = %v", err)
	}
	if left := r.keysWithoutTTL(); len(left) != 0 {
		t.Fatalf("keys without TTL: %v", left)
	}
	// The marker was never consumed, so the state is still takeable.
	r.expireErr = nil
	if _, err := store.Take(context.Background(), "s"); err != nil {
		t.Fatalf("take after recovery: %v", err)
	}
}

func TestSIWCState_ExpiredTamperedAndMarkerTTL(t *testing.T) {
	r := newMemRedis()
	store := newStateStore(t, r)
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }
	ctx := context.Background()
	if err := store.Put(ctx, "old", testAttempt()); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(siwcStateTTL + time.Second) }
	if _, err := store.Take(ctx, "old"); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("expired take = %v", err)
	}
	if left := r.keysWithoutTTL(); len(left) != 0 {
		t.Fatalf("keys without TTL: %v", left)
	}

	// A record whose tenant was rewritten fails to open (the tenant is in the AAD).
	store.now = func() time.Time { return base }
	if err := store.Put(ctx, "swap", testAttempt()); err != nil {
		t.Fatal(err)
	}
	k := siwcStatePrefix + siwcStateDigest("swap")
	r.vals[k] = strings.Replace(r.vals[k], `"t":"t1"`, `"t":"t2"`, 1)
	if _, err := store.Take(ctx, "swap"); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("tampered take = %v", err)
	}

	// Garbage and oversize states are refused without touching the store.
	if _, err := store.Take(ctx, strings.Repeat("x", siwcStateMaxLen+1)); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("oversize take = %v", err)
	}
	if err := store.Put(ctx, "", testAttempt()); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("empty put = %v", err)
	}
	noTenant := testAttempt()
	noTenant.TenantID = ""
	if err := store.Put(ctx, "s", noTenant); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("tenantless put = %v", err)
	}
}

func TestSIWCLimits_PerKeyBudgetsAndRetryAfter(t *testing.T) {
	r := newMemRedis()
	l, _ := newTestLimiter(r)
	base := time.Date(2026, 10, 11, 12, 0, 30, 0, time.UTC)
	l.now = func() time.Time { return base }
	ctx := context.Background()
	cases := []struct {
		name  string
		limit int
		check func(string) error
	}{
		{"link", SIWCLinkLimit, func(id string) error { return l.CheckSIWCLink(ctx, id) }},
		{"complete", SIWCCompleteLimit, func(id string) error { return l.CheckSIWCComplete(ctx, id) }},
		{"callback", SIWCCallbackLimit, func(id string) error { return l.CheckSIWCCallback(ctx, id) }},
	}
	wantLimits := map[string]int{"link": 5, "complete": 10, "callback": 20}
	for _, c := range cases {
		if c.limit != wantLimits[c.name] {
			t.Fatalf("%s limit = %d", c.name, c.limit)
		}
		for i := 0; i < c.limit; i++ {
			if err := c.check("id-1"); err != nil {
				t.Fatalf("%s call %d: %v", c.name, i+1, err)
			}
		}
		err := c.check("id-1")
		var rl *RateLimitedError
		if !errors.As(err, &rl) || !errors.Is(err, ErrRateLimited) || rl.RetryAfter <= 0 {
			t.Fatalf("%s over limit = %v", c.name, err)
		}
		if err := c.check("id-2"); err != nil {
			t.Fatalf("%s: another subject was refused: %v", c.name, err)
		}
	}
	if SIWCLimitWindow != 10*time.Minute {
		t.Fatalf("window = %v", SIWCLimitWindow)
	}
	if left := r.keysWithoutTTL(); len(left) != 0 {
		t.Fatalf("counters without TTL: %v", left)
	}
	if k := SIWCCallbackLimitKey(strings.Repeat("9", 500)); len(k) > 100 {
		t.Fatalf("callback key not bounded: %d", len(k))
	}
	// A nil limiter and a missing Redis fail open, like Allow.
	var nl *RateLimiter
	if err := nl.CheckSIWCLink(ctx, "u"); err != nil {
		t.Fatalf("nil limiter: %v", err)
	}
}
