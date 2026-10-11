package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/shared/database"
)

func newTestLimiter(r *memRedis) (*RateLimiter, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	l := NewRateLimiter(r, slog.New(slog.NewTextHandler(logs, nil)))
	return l, logs
}

func TestRateLimiter_AllowsUpToLimitThenRefuses(t *testing.T) {
	r := newMemRedis()
	l, _ := newTestLimiter(r)
	// A fixed clock in the middle of a window, so the sixth call cannot
	// land in the next one.
	base := time.Date(2026, 10, 10, 12, 0, 30, 0, time.UTC)
	l.now = func() time.Time { return base }
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if ok, wait := l.Allow(ctx, "k", 5, time.Minute); !ok || wait != 0 {
			t.Fatalf("call %d: ok=%v wait=%v", i+1, ok, wait)
		}
	}
	ok, wait := l.Allow(ctx, "k", 5, time.Minute)
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("sixth: ok=%v wait=%v", ok, wait)
	}
	if wait != 30*time.Second {
		t.Fatalf("retry after = %v, want the rest of the window (30s)", wait)
	}
	// Another key has its own budget.
	if ok, _ := l.Allow(ctx, "other", 5, time.Minute); !ok {
		t.Fatal("an independent key was refused")
	}
	// Every counter carries a TTL: none can pin a permanent 429.
	if left := r.keysWithoutTTL(); len(left) != 0 {
		t.Fatalf("counters without TTL: %v", left)
	}
	// The next window starts afresh.
	l.now = func() time.Time { return base.Add(time.Minute) }
	if ok, _ := l.Allow(ctx, "k", 5, time.Minute); !ok {
		t.Fatal("a new window must admit again")
	}
}

func TestRateLimiter_StoreDownFailsOpenWithWarn(t *testing.T) {
	r := newMemRedis()
	r.incrErr = errors.New("connection refused")
	l, logs := newTestLimiter(r)
	for i := 0; i < 20; i++ {
		if ok, _ := l.Allow(context.Background(), "k", 1, time.Minute); !ok {
			t.Fatal("a Redis outage must allow (fail-open)")
		}
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no WARN logged: %s", logs.String())
	}
}

// Ruling 5.10: an Expire failure must not leave a counter without expiry.
func TestRateLimiter_ExpireFailureLeavesNoImmortalKey(t *testing.T) {
	r := newMemRedis()
	r.expireErr = errors.New("timeout")
	l, _ := newTestLimiter(r)
	if ok, _ := l.Allow(context.Background(), "k", 1, time.Minute); !ok {
		t.Fatal("the first call must pass")
	}
	if left := r.keysWithoutTTL(); len(left) != 0 {
		t.Fatalf("counter left without TTL after an Expire failure: %v", left)
	}
}

func TestRateLimiter_NilStoreAllows(t *testing.T) {
	l := NewRateLimiter(nil, nil)
	if ok, _ := l.Allow(context.Background(), "k", 1, time.Minute); !ok {
		t.Fatal("no Redis configured must allow")
	}
}

func TestRateLimiter_CheckCarriesRetryAfter(t *testing.T) {
	l, _ := newTestLimiter(newMemRedis())
	ctx := context.Background()
	if err := l.Check(ctx, "k", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	err := l.Check(ctx, "k", 1, time.Minute)
	var rl *RateLimitedError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("err = %v (%+v)", err, rl)
	}
}

// With a store that runs scripts, increment and expiry are one atomic
// step: a counter left without TTL (a crash between INCR and EXPIRE under
// an older path) gets one on the next call, and the plain INCR/EXPIRE pair
// is never used.
func TestRateLimiter_AtomicScriptPath(t *testing.T) {
	r := &evalRedis{memRedis: newMemRedis()}
	l := NewRateLimiter(r, nil)
	base := time.Date(2026, 10, 10, 12, 0, 30, 0, time.UTC)
	l.now = func() time.Time { return base }
	slot := base.UnixNano() / int64(time.Minute)
	immortal := "llm:rl:k:" + strconv.FormatInt(slot, 10)
	r.vals[immortal] = "2" // pre-existing counter without TTL
	r.expireErr = errors.New("EXPIRE must not be used on the script path")
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(context.Background(), "k", 5, time.Minute); !ok {
			t.Fatalf("call %d refused", i+1)
		}
	}
	if ok, _ := l.Allow(context.Background(), "k", 5, time.Minute); ok {
		t.Fatal("the counter carried over must count: 2 + 4 > 5")
	}
	if left := r.keysWithoutTTL(); len(left) != 0 {
		t.Fatalf("counters without TTL: %v", left)
	}
	if r.evals != 4 {
		t.Fatalf("evals = %d, want 4", r.evals)
	}
	r.incrErr = errors.New("down")
	if ok, _ := l.Allow(context.Background(), "k", 5, time.Minute); !ok {
		t.Fatal("a failing script must fail open")
	}
}

// The backend adapter is the store the module gets: it must take the
// atomic path.
var _ scriptRunner = (*database.RedisClientAdapter)(nil)
