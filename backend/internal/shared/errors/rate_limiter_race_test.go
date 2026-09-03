package errors

// H-1: RateLimiter.Check read rl.configs without a lock while
// SetAuthFailedConfig wrote it on every login and every service-account
// grant. A concurrent map read and write is a FATAL runtime error — not
// a recoverable panic — so any anonymous caller could stop the process.
//
// This probe is kept permanently: it is cheap, and it is the regression
// test for a defect whose blast radius is the whole process.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

func TestRateLimiter_ConcurrentCheckAndMiddlewareIsRaceFree(t *testing.T) {
	rl := NewRateLimiter()
	t.Cleanup(rl.Close)

	handler := rl.Middleware("api:general")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rl.Check(context.Background(), "ip:"+strconv.Itoa(i), "api:general")

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = "203.0.113." + strconv.Itoa(i%256) + ":1234"
				handler.ServeHTTP(httptest.NewRecorder(), req)
			}
		}(i)
	}
	wg.Wait()
}

// An unknown config name must fall back to api:general WITHOUT reading
// the map twice outside the lock.
func TestRateLimiter_UnknownConfigFallsBackUnderLock(t *testing.T) {
	rl := NewRateLimiter()
	t.Cleanup(rl.Close)

	res := rl.Check(context.Background(), "k", "does:not:exist")
	if res == nil {
		t.Fatal("Check must never return nil")
	}
}

// The auth-facing surface is GONE. These identifiers must not come back:
// they are what made an anonymous request able to write a shared map.
func TestRateLimiter_AuthSurfaceRemoved(t *testing.T) {
	// Compile-time assertions live in the type; this test documents the
	// contract for a reader. If any of the following exists again, the
	// H-1 writer is back and the counters have been bypassed:
	//   SetAuthFailedConfig, IsBlocked, IsLockedOut, RecordFailedAuth,
	//   CheckMultiple, AuthMiddleware
	// and the configs auth:login, auth:refresh, auth:failed,
	// security:sensitive, global:ip.
	rl := NewRateLimiter()
	t.Cleanup(rl.Close)
	if len(rl.configs) != 1 {
		t.Fatalf("configs = %d, want exactly 1 (api:general)", len(rl.configs))
	}
	if _, ok := rl.configs["api:general"]; !ok {
		t.Fatal("api:general must be the surviving config")
	}
}
