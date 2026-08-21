package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/redis/go-redis/v9"
)

const sessionRevocationWarningInterval = time.Minute

// sessionRevocationTTL is how long a revoked sid stays on the denylist.
// It is FIXED at the maximum access-token lifetime the platform permits
// plus a clock-skew minute — deliberately not derived from the live
// policy value.
//
// Deriving it from the current value is unsafe in both directions. If an
// operator raises accessTokenTTL, an entry sized from the old value
// expires while the new longer tokens are still valid. If they lower it,
// an entry sized from the new value expires while tokens minted under the
// old one are still valid. Because NewJWTService clamps every effective
// lifetime to MaxAccessTokenTTL, no token can outlive this window.
// The alternative — tracking each session's newest access-token exp —
// costs a write per mint to save bounded Redis retention. ADR-0017 D5.
const sessionRevocationTTL = MaxAccessTokenTTL + time.Minute

// SessionRevocationService tracks revoked JWT session IDs (the `sid` claim)
// in Redis so a stolen access token can be invalidated mid-session without
// waiting for the access-token TTL to elapse.
//
// Entries auto-expire after `sessionRevocationTTL` — the maximum
// access-token lifetime the platform permits, plus a clock-skew minute.
// Sizing this from the live policy value would let a policy change strand
// tokens outside their own revocation entry.
//
// The IsRevoked lookup fails open on any Redis error. A degraded Redis
// must not lock every user out of the platform — the worst case on an
// outage is that the stolen-token window widens back to the access-token
// TTL, which is where it was before this feature existed. Failures are
// logged so operators can see the degradation.
type SessionRevocationService interface {
	// Revoke marks the given sid as revoked. Reason is persisted as the
	// Redis value for operator debugging (e.g. "logout", "password_change",
	// "admin_kill"). A zero sid is a no-op because older JWTs may lack it.
	Revoke(ctx context.Context, sid, reason string) error
	// IsRevoked returns true when the sid has been marked revoked. Returns
	// false on Redis errors — see the type comment for the fail-open
	// rationale.
	IsRevoked(ctx context.Context, sid string) (bool, error)
}

// SessionRevocationDegradedError means durable session/refresh state was
// revoked, but the short-lived Redis sid deny-list could not be updated.
// Callers can detect this partial degradation with errors.As without
// exposing the store error in ordinary logs or API responses.
type SessionRevocationDegradedError struct{ Cause error }

func (e *SessionRevocationDegradedError) Error() string { return "session revocation store degraded" }
func (e *SessionRevocationDegradedError) Unwrap() error { return e.Cause }

type sessionRevocationStoreFailureRecorder interface {
	RecordSessionRevocationStoreFailure(operation string)
}

type redisSessionRevocationService struct {
	client  RedisClient
	ttl     time.Duration
	log     *slog.Logger
	metrics sessionRevocationStoreFailureRecorder

	warningMu   sync.Mutex
	lastWarning time.Time
}

// NewSessionRevocationService builds a Redis-backed revocation store.
//
// Deprecated argument: accessTokenTTL is ignored. It is retained so forks
// calling this constructor directly keep compiling; the entry TTL is the
// fixed sessionRevocationTTL. Passing a shorter value cannot shorten the
// security window. ADR-0017 D5.
func NewSessionRevocationService(client RedisClient, accessTokenTTL time.Duration, log *slog.Logger) SessionRevocationService {
	_ = accessTokenTTL
	return newSessionRevocationService(client, accessTokenTTL, log, metrics.Default())
}

func newSessionRevocationService(client RedisClient, _ time.Duration, log *slog.Logger, recorder sessionRevocationStoreFailureRecorder) SessionRevocationService {
	if log == nil {
		log = slog.Default()
	}
	if recorder == nil {
		recorder = metrics.Default()
	}
	return &redisSessionRevocationService{
		client:  client,
		ttl:     sessionRevocationTTL,
		log:     log,
		metrics: recorder,
	}
}

func (s *redisSessionRevocationService) Revoke(ctx context.Context, sid, reason string) error {
	if sid == "" {
		return nil
	}
	if reason == "" {
		reason = "revoked"
	}
	if err := s.client.Set(ctx, revocationKey(sid), reason, s.ttl); err != nil {
		s.metrics.RecordSessionRevocationStoreFailure("write")
		return err
	}
	return nil
}

func (s *redisSessionRevocationService) IsRevoked(ctx context.Context, sid string) (bool, error) {
	if sid == "" {
		return false, nil
	}
	_, err := s.client.Get(ctx, revocationKey(sid))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	s.metrics.RecordSessionRevocationStoreFailure("lookup")
	s.warnStoreUnavailable(ctx)
	return false, nil
}

func (s *redisSessionRevocationService) warnStoreUnavailable(ctx context.Context) {
	s.warningMu.Lock()
	defer s.warningMu.Unlock()
	if !s.lastWarning.IsZero() && time.Since(s.lastWarning) < sessionRevocationWarningInterval {
		return
	}
	s.lastWarning = time.Now()
	s.log.WarnContext(ctx, "session revocation store unavailable",
		slog.String("operation", "lookup"),
	)
}

func revocationKey(sid string) string {
	return "auth:revoked:session:" + sid
}
