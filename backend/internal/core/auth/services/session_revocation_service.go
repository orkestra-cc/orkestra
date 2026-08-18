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

// SessionRevocationService tracks revoked JWT session IDs (the `sid` claim)
// in Redis so a stolen access token can be invalidated mid-session without
// waiting for the access-token TTL to elapse.
//
// Entries auto-expire after the access-token TTL plus a small clock-skew
// buffer: a token older than that is already rejected by signature
// validation, so keeping the revocation row longer only wastes memory.
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
// accessTokenTTL should match the TTL used by the JWT service; a one-minute
// buffer is added on top to swallow clock skew between issuer and verifier.
func NewSessionRevocationService(client RedisClient, accessTokenTTL time.Duration, log *slog.Logger) SessionRevocationService {
	return newSessionRevocationService(client, accessTokenTTL, log, metrics.Default())
}

func newSessionRevocationService(client RedisClient, accessTokenTTL time.Duration, log *slog.Logger, recorder sessionRevocationStoreFailureRecorder) SessionRevocationService {
	if log == nil {
		log = slog.Default()
	}
	if accessTokenTTL <= 0 {
		accessTokenTTL = 15 * time.Minute
	}
	if recorder == nil {
		recorder = metrics.Default()
	}
	return &redisSessionRevocationService{
		client:  client,
		ttl:     accessTokenTTL + time.Minute,
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
