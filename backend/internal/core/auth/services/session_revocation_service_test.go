package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeRedisClient is a minimal in-memory RedisClient for unit tests.
// Mirrors the contract of *database.RedisClientAdapter — returns redis.Nil
// for missing keys so the service's "not found → not revoked" branch can
// be exercised without a live Redis.
type fakeRedisClient struct {
	mu     sync.Mutex
	data   map[string]string
	getErr error
}

func newFakeRedisClient() *fakeRedisClient {
	return &fakeRedisClient{data: map[string]string{}}
}

func (f *fakeRedisClient) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch v := value.(type) {
	case string:
		f.data[key] = v
	case []byte:
		f.data[key] = string(v)
	default:
		f.data[key] = ""
	}
	return nil
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
	svc := NewSessionRevocationService(newFakeRedisClient(), 15*time.Minute, nil)
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
	svc := NewSessionRevocationService(newFakeRedisClient(), 15*time.Minute, nil)

	revoked, err := svc.IsRevoked(context.Background(), "never-seen")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("untouched sid should not be revoked")
	}
}

func TestSessionRevocation_EmptySidNoOps(t *testing.T) {
	svc := NewSessionRevocationService(newFakeRedisClient(), 15*time.Minute, nil)
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
	svc := NewSessionRevocationService(fake, 15*time.Minute, nil)

	revoked, err := svc.IsRevoked(context.Background(), "sid-x")
	if err != nil {
		t.Fatalf("IsRevoked must swallow Redis errors, got %v", err)
	}
	if revoked {
		t.Fatal("Redis outage must fail open (revoked=false)")
	}
}

func TestSessionRevocation_DefaultTTLFallback(t *testing.T) {
	// Zero TTL defaults to 15m so callers that forget the config don't end
	// up with an instantly-expiring revocation.
	svc := NewSessionRevocationService(newFakeRedisClient(), 0, nil)
	ctx := context.Background()

	if err := svc.Revoke(ctx, "sid-ttl", "admin_kill"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, _ := svc.IsRevoked(ctx, "sid-ttl")
	if !revoked {
		t.Fatal("revocation with default TTL must still be effective")
	}
}
