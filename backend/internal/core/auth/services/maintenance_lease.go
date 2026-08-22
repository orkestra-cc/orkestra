package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

// Lease timings. The TTL is generous relative to the renew interval so a
// brief Redis blip does not hand the scheduler to a second replica; the
// follower retry is long because losing a sweep cycle costs nothing and
// a tight retry loop against a healthy leader costs Redis traffic.
const (
	LeaseTTL           = 2 * time.Minute
	LeaseRenewInterval = 30 * time.Second
	LeaseRetryInterval = 5 * time.Minute
)

// LeaseRedisClient is the narrow extension the maintenance lease needs.
// Defined here rather than widening module.RedisClient, which every fork
// implementation would have to grow. Mirrors AtomicTakeRedisClient.
type LeaseRedisClient interface {
	RedisClient
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// Compare-and-expire / compare-and-delete. Both run server-side so a
// replica can only act on a lease whose owner token it holds — the
// read-then-act version has a window in which the lease expires, another
// replica acquires it, and the first replica then renews or deletes a
// lease it no longer owns.
const (
	leaseRenewScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
  return 0
end`
	leaseReleaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
else
  return 0
end`
)

// MaintenanceLease elects one scheduler leader across backend replicas.
//
// It guards the SCHEDULER, not one pass: the leader retains the lease
// across both the drain wait and the six-hour idle wait. Guarding only
// the pass would let every follower wake on its own retry timer and
// re-enter drain mode against an idle cluster, and would turn the
// per-cycle batch bound into a per-replica multiplier.
//
// Redis unavailability is never leadership. A failed acquire or a failed
// renew skips maintenance — it must never affect authentication.
type MaintenanceLease struct {
	client LeaseRedisClient
	key    string
	log    *slog.Logger

	mu    sync.Mutex
	token string
}

func NewMaintenanceLease(client LeaseRedisClient, key string, log *slog.Logger) *MaintenanceLease {
	if log == nil {
		log = slog.Default()
	}
	return &MaintenanceLease{client: client, key: key, log: log}
}

// Acquire attempts leadership. Returns (false, nil) when another replica
// holds it and (false, err) when Redis could not answer — the caller must
// treat both as "not the leader" but only log the second.
func (l *MaintenanceLease) Acquire(ctx context.Context) (bool, error) {
	if l == nil || l.client == nil {
		return false, nil
	}
	token, err := randomLeaseToken()
	if err != nil {
		return false, err
	}
	ok, err := l.client.SetNX(ctx, l.key, token, LeaseTTL)
	if err != nil || !ok {
		return false, err
	}
	l.mu.Lock()
	l.token = token
	l.mu.Unlock()
	return true, nil
}

// Renew extends the lease only while this replica still owns it.
// A false return means leadership was lost: the caller must cancel the
// in-flight database context and the scheduler loop rather than carry on.
func (l *MaintenanceLease) Renew(ctx context.Context) (bool, error) {
	if l == nil || l.client == nil {
		return false, nil
	}
	l.mu.Lock()
	token := l.token
	l.mu.Unlock()
	if token == "" {
		return false, nil
	}
	res, err := l.client.Eval(ctx, leaseRenewScript, []string{l.key}, token, LeaseTTL.Milliseconds())
	if err != nil {
		return false, err
	}
	return leaseScriptSucceeded(res), nil
}

// Release drops the lease if this replica owns it. A non-owner Release is
// a silent no-op so a shutdown race cannot hand the next leader's lease
// away.
func (l *MaintenanceLease) Release(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	l.mu.Lock()
	token := l.token
	l.token = ""
	l.mu.Unlock()
	if token == "" {
		return nil
	}
	_, err := l.client.Eval(ctx, leaseReleaseScript, []string{l.key}, token)
	return err
}

func leaseScriptSucceeded(res interface{}) bool {
	switch v := res.(type) {
	case int64:
		return v == 1
	case int:
		return v == 1
	default:
		return false
	}
}

func randomLeaseToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
