package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/redis/go-redis/v9"
)

// fakeRedisClient is a minimal in-memory RedisClient for unit tests.
// Mirrors the contract of *database.RedisClientAdapter — returns redis.Nil
// for missing keys so the service's "not found → not revoked" branch can
// be exercised without a live Redis.
type fakeRedisClient struct {
	mu       sync.Mutex
	data     map[string]string
	ttls     map[string]time.Duration
	counters map[string]int64
	getErr   error
	setErr   error
}

func newFakeRedisClient() *fakeRedisClient {
	return &fakeRedisClient{data: map[string]string{}, ttls: map[string]time.Duration{}, counters: map[string]int64{}}
}

func (f *fakeRedisClient) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	switch v := value.(type) {
	case string:
		f.data[key] = v
	case []byte:
		f.data[key] = string(v)
	default:
		f.data[key] = ""
	}
	f.ttls[key] = ttl
	return nil
}

// ttlFor returns the TTL most recently passed to Set for key. Lets tests
// assert on the stored expiry without a live Redis.
func (f *fakeRedisClient) ttlFor(key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ttls[key]
}

func (f *fakeRedisClient) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.data[key]
	if !ok {
		return "", redis.Nil
	}
	return v, nil
}

func (f *fakeRedisClient) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}

func (f *fakeRedisClient) Keys(_ context.Context, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.data))
	for k := range f.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestSessionRevocation_RevokeThenIsRevoked(t *testing.T) {
	svc, _ := newTestSessionRevocationService(t, newFakeRedisClient(), 15*time.Minute, nil)
	ctx := context.Background()

	if err := svc.Revoke(ctx, "sid-1", "logout"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err := svc.IsRevoked(ctx, "sid-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !revoked {
		t.Fatal("sid-1 should be revoked")
	}
}

func TestSessionRevocation_UnknownSidNotRevoked(t *testing.T) {
	svc, _ := newTestSessionRevocationService(t, newFakeRedisClient(), 15*time.Minute, nil)

	revoked, err := svc.IsRevoked(context.Background(), "never-seen")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("untouched sid should not be revoked")
	}
}

func TestSessionRevocation_EmptySidNoOps(t *testing.T) {
	svc, _ := newTestSessionRevocationService(t, newFakeRedisClient(), 15*time.Minute, nil)
	ctx := context.Background()

	// Revoking empty sid must be a harmless no-op — older JWTs may lack one.
	if err := svc.Revoke(ctx, "", "logout"); err != nil {
		t.Fatalf("Revoke(empty): %v", err)
	}
	revoked, err := svc.IsRevoked(ctx, "")
	if err != nil {
		t.Fatalf("IsRevoked(empty): %v", err)
	}
	if revoked {
		t.Fatal("empty sid must not be reported as revoked")
	}
}

func TestSessionRevocation_FailsOpenOnRedisError(t *testing.T) {
	// Redis returning a non-Nil error must not lock users out — the service
	// fails open and returns revoked=false.
	fake := newFakeRedisClient()
	fake.getErr = errors.New("dial timeout")
	svc, _ := newTestSessionRevocationService(t, fake, 15*time.Minute, nil)

	revoked, err := svc.IsRevoked(context.Background(), "sid-x")
	if err != nil {
		t.Fatalf("IsRevoked must swallow Redis errors, got %v", err)
	}
	if revoked {
		t.Fatal("Redis outage must fail open (revoked=false)")
	}
}

func TestSessionRevocation_LookupFailureIsObservableAndSanitized(t *testing.T) {
	storeErr := errors.New("redis transport error containing credentials")
	fake := newFakeRedisClient()
	fake.getErr = storeErr
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, collector := newTestSessionRevocationService(t, fake, 15*time.Minute, logger)

	before := sessionRevocationFailureCount(t, collector, "lookup")
	revoked, err := svc.IsRevoked(context.Background(), "sensitive-session-id")
	if err != nil {
		t.Fatalf("IsRevoked must fail open, got %v", err)
	}
	if revoked {
		t.Fatal("lookup failure must not report the session as revoked")
	}
	if got := sessionRevocationFailureCount(t, collector, "lookup"); got != before+1 {
		t.Errorf("lookup failure metric = %d, want %d", got, before+1)
	}
	if output := logs.String(); strings.Contains(output, "sensitive-session-id") || strings.Contains(output, storeErr.Error()) {
		t.Errorf("lookup failure log leaked sensitive data: %s", output)
	}
}

func TestSessionRevocation_LookupFailureWarningIsRateLimited(t *testing.T) {
	fake := newFakeRedisClient()
	fake.getErr = errors.New("redis transport error")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, _ := newTestSessionRevocationService(t, fake, 15*time.Minute, logger)

	for range 3 {
		if _, err := svc.IsRevoked(context.Background(), "sid"); err != nil {
			t.Fatalf("IsRevoked must fail open, got %v", err)
		}
	}
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Errorf("warning count = %d, want 1 within the rate-limit window", got)
	}
}

func TestSessionRevocation_WriteFailureIsObservableAndReturned(t *testing.T) {
	storeErr := errors.New("redis write transport error")
	fake := newFakeRedisClient()
	fake.setErr = storeErr
	svc, collector := newTestSessionRevocationService(t, fake, 15*time.Minute, nil)

	before := sessionRevocationFailureCount(t, collector, "write")
	err := svc.Revoke(context.Background(), "sid-write", "logout")
	if !errors.Is(err, storeErr) {
		t.Fatalf("Revoke error = %v, want original error %v", err, storeErr)
	}
	if got := sessionRevocationFailureCount(t, collector, "write"); got != before+1 {
		t.Errorf("write failure metric = %d, want %d", got, before+1)
	}
}

// The denylist entry must outlive every access token the policy is
// permitted to mint, in BOTH directions of a policy change. Deriving the
// entry TTL from the live policy value fails the decrease case: a token
// minted at 4h, then a policy lowered to 15m, then a logout, would store
// a 16-minute entry while the 4-hour token is still valid. ADR-0017 D5.
//
// This also covers what used to be a dedicated
// TestSessionRevocation_DefaultTTLFallback case (constructorArg=0 below):
// there is no more "default" to fall back to now that the entry TTL is a
// fixed value rather than derived from the constructor argument.
func TestSessionRevocationTTL_UsesPolicyMaximum(t *testing.T) {
	for _, constructorArg := range []time.Duration{0, time.Minute, 15 * time.Minute, 4 * time.Hour, 48 * time.Hour} {
		fake := newFakeRedisClient()
		svc, _ := newTestSessionRevocationService(t, fake, constructorArg, slog.New(slog.DiscardHandler))
		if err := svc.Revoke(context.Background(), "sid-1", "logout"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		want := MaxAccessTokenTTL + time.Minute
		if got := fake.ttlFor("auth:revoked:session:sid-1"); got != want {
			t.Errorf("constructor arg %v produced entry TTL %v, want the fixed %v", constructorArg, got, want)
		}
	}
}

func TestRevocationTTL_OutlivesTokensMintedBeforePolicyDecrease(t *testing.T) {
	// Mint at 4h, lower the policy to 15m, then revoke. The entry must
	// still cover the 4h token that is out there.
	fake := newFakeRedisClient()
	svc, _ := newTestSessionRevocationService(t, fake, 4*time.Hour, slog.New(slog.DiscardHandler))
	// (Policy lowered to 15m — the service must not consult it at all.)
	if err := svc.Revoke(context.Background(), "sid-2", "logout"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if got := fake.ttlFor("auth:revoked:session:sid-2"); got < 4*time.Hour {
		t.Fatalf("entry TTL %v does not cover the already-minted 4h token", got)
	}
}

// Incr / Expire round out the RedisClient contract. This fake is only
// used by revocation tests, which never touch the counter primitive.
func (f *fakeRedisClient) Incr(_ context.Context, key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.counters[key] + 1
	f.counters[key] = n
	return n, nil
}

func (f *fakeRedisClient) Expire(context.Context, string, time.Duration) error { return nil }

func TestSessionRevocation_ConcurrentLookupFailuresFailOpenAndRateLimitWarnings(t *testing.T) {
	const workers = 32
	storeErr := errors.New("redis transport error from concurrent lookup")
	fake := newFakeRedisClient()
	fake.getErr = storeErr
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, collector := newTestSessionRevocationService(t, fake, 15*time.Minute, logger)

	start := make(chan struct{})
	results := make(chan struct {
		revoked bool
		err     error
	}, workers)
	var ready sync.WaitGroup
	var calls sync.WaitGroup
	ready.Add(workers)
	calls.Add(workers)
	for range workers {
		go func() {
			defer calls.Done()
			ready.Done()
			<-start
			revoked, err := svc.IsRevoked(context.Background(), "sensitive-concurrent-session-id")
			results <- struct {
				revoked bool
				err     error
			}{revoked: revoked, err: err}
		}()
	}
	ready.Wait()
	close(start)
	calls.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			t.Errorf("IsRevoked must fail open, got %v", result.err)
		}
		if result.revoked {
			t.Error("lookup failure must not report the session as revoked")
		}
	}
	if got := sessionRevocationFailureCount(t, collector, "lookup"); got != workers {
		t.Errorf("lookup failure metric = %d, want %d", got, workers)
	}
	output := logs.String()
	if got := strings.Count(output, "level=WARN"); got > 1 {
		t.Errorf("warning count = %d, want at most 1 within the rate-limit window", got)
	}
	if strings.Contains(output, "sensitive-concurrent-session-id") || strings.Contains(output, storeErr.Error()) {
		t.Errorf("lookup failure log leaked sensitive data: %s", output)
	}
}

func newTestSessionRevocationService(t *testing.T, client RedisClient, accessTokenTTL time.Duration, log *slog.Logger) (SessionRevocationService, *metrics.Collector) {
	t.Helper()
	collector := metrics.NewCollector()
	if err := collector.Register(); err != nil {
		t.Fatalf("register metrics collector: %v", err)
	}
	return newSessionRevocationService(client, accessTokenTTL, log, collector), collector
}

func sessionRevocationFailureCount(t *testing.T, collector *metrics.Collector, operation string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	collector.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics returned %d", rec.Code)
	}
	pattern := regexp.MustCompile(`(?m)^orkestra_auth_session_revocation_store_failures_total\{operation="` + regexp.QuoteMeta(operation) + `"\} ([0-9]+)$`)
	match := pattern.FindStringSubmatch(rec.Body.String())
	if match == nil {
		return 0
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("parse metric count: %v", err)
	}
	return count
}
