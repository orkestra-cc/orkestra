package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/metrics"
)

// Limit is "at most Threshold events per Window". Threshold < 1 means
// UNSET and never locks — a misedited policy must not become a
// deny-all, the same stance the limiter's SetAuthFailedConfig took.
type Limit struct {
	Threshold int
	Window    time.Duration
}

// Verdict is what the store said about a key. The zero Verdict is what
// a caller gets alongside a non-nil error, and it reads as "not locked"
// so a caller that ignores the error still fails open.
type Verdict struct {
	Count      int64
	Locked     bool
	RetryAfter time.Duration
}

// AttemptCounter is the single "N events per window" primitive of the
// auth module: login lockout, password-reset request caps,
// verification-resend caps, service-account grant lockout and (PR B) the
// authenticated MFA-verify cap all key into it.
//
// Every method returns its error. What an unavailable store MEANS is
// decided by each caller, and the decision is the same everywhere:
// FAIL OPEN and fall back to the durable rule. A Locked error reads as
// not locked; a RecordFailure error yields no verdict and the caller
// switches to User.FailedLoginCount for that attempt. A fail-closed
// counter would turn a Redis outage into a platform-wide login outage —
// the trade ADR-0017 D5 already rejected for session revocation.
type AttemptCounter interface {
	// Locked peeks. It never increments. A non-nil error means the
	// store could not answer.
	Locked(ctx context.Context, key string, limit Limit) (Verdict, error)
	// RecordFailure increments the key and reports the resulting state.
	RecordFailure(ctx context.Context, key string, limit Limit) (Verdict, error)
	// Reset deletes the key. A successful login clears the email scope.
	Reset(ctx context.Context, key string) error
}

// ScriptRedisClient is the narrow RedisClient extension the counter
// needs. database.RedisClientAdapter satisfies it (redis.go:166); the
// auth module refuses to boot without it, exactly as it does for the
// GETDEL contract (module.go:897-899).
type ScriptRedisClient interface {
	RedisClient
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// attemptScript is BOTH reads. Running peek and increment through one
// script is what makes count, TTL and orphan-healing a single atomic
// step; the two-command INCR-then-EXPIRE shape it replaces
// (oauth_state_service.go Incr) leaves a key with no TTL when it fails
// between the commands — for a lockout counter that is a permanent 429
// until someone runs DEL by hand.
//
// KEYS[1] counter key
// ARGV[1] window in milliseconds
// ARGV[2] "1" to increment, "0" to peek
// returns {count, ttlMillis}; {0, -2} when the key does not exist.
//
// The threshold is deliberately NOT in here: Verdict.Locked is computed
// Go-side against the LIVE limit, so lowering accountLockoutThreshold
// mid-window locks immediately and raising it unlocks (edge case 3),
// with no capacity frozen into the key.
const attemptScript = `
local n
if ARGV[2] == "1" then
  n = redis.call('INCR', KEYS[1])
else
  n = tonumber(redis.call('GET', KEYS[1]) or '0')
end
if n == 0 then return {0, -2} end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
`

// Scope names. These are also the bounded label values of
// orkestra_auth_lockouts_total — keep the two lists in step.
const (
	ScopeIP          = "ip"
	ScopeEmail       = "email"
	ScopeClient      = "client"
	ScopeResetEmail  = "reset-email"
	ScopeResetIP     = "reset-ip"
	ScopeVerifyEmail = "verify-email"
	ScopeVerifyIP    = "verify-ip"
	ScopeMFAVerify   = "mfa-verify"
	ScopeMFAEnroll   = "mfa-enroll"
)

const attemptKeyPrefix = "auth:attempts:"

// Request caps stay named constants rather than admin config: a request
// cap has no legitimate reason to be tuned per install today, and adding
// schema keys is a separate decision (spec §4.1 D2).
//
// The per-ADDRESS caps are 60 per window, not 3. An egress address is
// not an account: a corporate NAT or VPN carries hundreds of people, and
// three reset requests among them must not silence the endpoint for
// everyone behind it.
var (
	ResetRequestsPerEmail  = Limit{Threshold: 3, Window: 15 * time.Minute}
	ResetRequestsPerIP     = Limit{Threshold: 60, Window: 15 * time.Minute}
	VerifyRequestsPerEmail = Limit{Threshold: 3, Window: 15 * time.Minute}
	VerifyRequestsPerIP    = Limit{Threshold: 60, Window: 15 * time.Minute}
)

// MFAVerifyLimit is the authenticated MFA-verify cap (spec D20). Defined
// here in PR A; wired to the handlers in PR B.
var MFAVerifyLimit = Limit{Threshold: MFAMaxAttempts, Window: MFAChallengeTTL}

// MFAEnrollLimit bounds `/mfa/enroll/confirm`. Same threshold and same
// window as MFAVerifyLimit — five attempts per challenge lifetime is the
// same judgement in both places, and there is no reason for the numbers to
// differ — but deliberately a SEPARATE budget on a SEPARATE key.
//
// 🔴 Do not collapse the two onto AttemptKeyMFAVerify. If a failed
// ENROLMENT spent the STEP-UP budget, a user fumbling their enrolment
// codes would lock themselves out of step-up — and step-up is precisely
// what a user who already holds a factor must pass in order to re-enrol.
// That is a circular lockout, the third this branch would have had to fix.
// The budgets stay independent so exhausting one can never close the door
// the other opens.
var MFAEnrollLimit = Limit{Threshold: MFAMaxAttempts, Window: MFAChallengeTTL}

// normaliseEmail applies the SAME normalisation Login does
// (password_auth_service.go: strings.ToLower(strings.TrimSpace(...))),
// so a counter keyed at signup and a counter keyed at login are the
// same counter.
func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// AttemptKeyIP returns "" for an unresolvable address so the caller
// SKIPS the IP scope. Sharing one "ip:" key among every caller with no
// resolvable address — today's behaviour — lets one such client lock out
// all of them.
func AttemptKeyIP(ip string) string { return addressKey(ScopeIP, ip) }

// AttemptKeyResetIP / AttemptKeyVerifyIP have the same empty-skips rule.
func AttemptKeyResetIP(ip string) string  { return addressKey(ScopeResetIP, ip) }
func AttemptKeyVerifyIP(ip string) string { return addressKey(ScopeVerifyIP, ip) }

func addressKey(scope, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return attemptKeyPrefix + scope + ":" + ip
}

// The email scopes are per audience: an operator and a client account
// sharing an address lock independently (edge case 4).
func AttemptKeyEmail(aud PolicyAudience, email string) string {
	return emailKey(ScopeEmail, aud, email)
}
func AttemptKeyResetEmail(aud PolicyAudience, email string) string {
	return emailKey(ScopeResetEmail, aud, email)
}
func AttemptKeyVerifyEmail(aud PolicyAudience, email string) string {
	return emailKey(ScopeVerifyEmail, aud, email)
}

func emailKey(scope string, aud PolicyAudience, email string) string {
	email = normaliseEmail(email)
	if email == "" {
		return ""
	}
	return attemptKeyPrefix + scope + ":" + string(aud) + ":" + email
}

// AttemptKeyClient — a client ID IS an account, so it carries the
// account pair, not the address pair.
func AttemptKeyClient(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return ""
	}
	return attemptKeyPrefix + ScopeClient + ":" + clientID
}

// AttemptKeyMFAVerify is per audience and per user UUID (never per
// email: the caller is already authenticated).
func AttemptKeyMFAVerify(aud PolicyAudience, userUUID string) string {
	return mfaUserKey(ScopeMFAVerify, aud, userUUID)
}

// AttemptKeyMFAEnroll is the ENROLMENT sibling of AttemptKeyMFAVerify —
// same shape, different scope segment, so the two counters never touch.
// See MFAEnrollLimit for why that separation is load-bearing.
func AttemptKeyMFAEnroll(aud PolicyAudience, userUUID string) string {
	return mfaUserKey(ScopeMFAEnroll, aud, userUUID)
}

func mfaUserKey(scope string, aud PolicyAudience, userUUID string) string {
	userUUID = strings.TrimSpace(userUUID)
	if userUUID == "" {
		return ""
	}
	return attemptKeyPrefix + scope + ":" + string(aud) + ":" + userUUID
}

// ScopeOfKey recovers the scope label from a key so the metric does not
// need a second argument at every call site. Unknown shapes collapse to
// "unknown" in the recorder.
func ScopeOfKey(key string) string {
	rest := strings.TrimPrefix(key, attemptKeyPrefix)
	if i := strings.IndexByte(rest, ':'); i > 0 {
		return rest[:i]
	}
	return ""
}

// attemptWarningInterval throttles the store-unavailable WARN so a
// Redis outage produces one line a minute, not one per request — the
// sessionRevocationWarningInterval pattern
// (session_revocation_service.go:14, :172-181).
const attemptWarningInterval = time.Minute

type redisAttemptCounter struct {
	client ScriptRedisClient
	log    *slog.Logger

	mu          sync.Mutex
	lastWarning time.Time
}

// NewRedisAttemptCounter builds the production counter. client must
// support EVAL; the auth module verifies that at boot.
func NewRedisAttemptCounter(client ScriptRedisClient, log *slog.Logger) AttemptCounter {
	if log == nil {
		log = slog.Default()
	}
	return &redisAttemptCounter{client: client, log: log}
}

func (c *redisAttemptCounter) Locked(ctx context.Context, key string, limit Limit) (Verdict, error) {
	return c.eval(ctx, "peek", key, limit, false)
}

func (c *redisAttemptCounter) RecordFailure(ctx context.Context, key string, limit Limit) (Verdict, error) {
	v, err := c.eval(ctx, "record", key, limit, true)
	if err == nil && v.Locked {
		metrics.Default().RecordAuthLockout(ScopeOfKey(key))
	}
	return v, err
}

func (c *redisAttemptCounter) Reset(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if err := c.client.Del(ctx, key); err != nil {
		c.report(ctx, "reset", err)
		return fmt.Errorf("attempt counter reset: %w", err)
	}
	return nil
}

func (c *redisAttemptCounter) eval(ctx context.Context, op, key string, limit Limit, incr bool) (Verdict, error) {
	// An empty key means the caller resolved no identifier for this
	// scope (no client IP, no email). Skipping is the whole point: it
	// must not share a bucket with every other such caller.
	if key == "" {
		return Verdict{}, nil
	}
	window := limit.Window
	if window <= 0 {
		window = 15 * time.Minute
	}
	flag := "0"
	if incr {
		flag = "1"
	}

	raw, err := c.client.Eval(ctx, attemptScript, []string{key}, window.Milliseconds(), flag)
	if err != nil {
		c.report(ctx, op, err)
		return Verdict{}, fmt.Errorf("attempt counter %s: %w", op, err)
	}

	count, ttlMillis, err := parseAttemptResult(raw)
	if err != nil {
		c.report(ctx, op, err)
		return Verdict{}, fmt.Errorf("attempt counter %s: %w", op, err)
	}

	v := Verdict{Count: count}
	// Threshold < 1 is UNSET, never deny-all.
	if limit.Threshold >= 1 && count >= int64(limit.Threshold) {
		v.Locked = true
	}
	if ttlMillis > 0 {
		v.RetryAfter = time.Duration(ttlMillis) * time.Millisecond
	}
	return v, nil
}

// parseAttemptResult reads the {count, ttlMillis} pair. go-redis decodes
// a Lua table of integers as []interface{} of int64.
func parseAttemptResult(raw interface{}) (count, ttlMillis int64, err error) {
	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 2 {
		return 0, 0, fmt.Errorf("unexpected script result %T", raw)
	}
	for i, v := range arr {
		n, ok := v.(int64)
		if !ok {
			return 0, 0, fmt.Errorf("unexpected script result element %d: %T", i, v)
		}
		if i == 0 {
			count = n
		} else {
			ttlMillis = n
		}
	}
	return count, ttlMillis, nil
}

// report increments the failure metric on every error and logs at most
// one WARN per attemptWarningInterval. The error is always returned to
// the caller regardless — the metric is the alerting signal, the return
// is what lets D4 know to fall back to the durable rule.
func (c *redisAttemptCounter) report(ctx context.Context, op string, err error) {
	metrics.Default().RecordAuthAttemptStoreFailure(op)

	c.mu.Lock()
	throttled := !c.lastWarning.IsZero() && time.Since(c.lastWarning) < attemptWarningInterval
	if !throttled {
		c.lastWarning = time.Now()
	}
	c.mu.Unlock()
	if throttled {
		return
	}
	c.log.WarnContext(ctx, "auth attempt counter store unavailable",
		slog.String("operation", op),
		slog.String("error", err.Error()),
	)
}

// ===== in-memory implementation =====

// MemoryAttemptCounter is the test / no-Redis stand-in, atomic by
// construction under one mutex. It ships in a non-_test.go file for the
// same reason NewMemoryOAuthStateStore does: handler tests across
// several packages need it without each standing up a miniredis.
type MemoryAttemptCounter struct {
	mu      sync.Mutex
	counts  map[string]int64
	expires map[string]time.Time
	failErr error
	now     func() time.Time
}

func NewMemoryAttemptCounter() AttemptCounter {
	return &MemoryAttemptCounter{
		counts:  map[string]int64{},
		expires: map[string]time.Time{},
		now:     time.Now,
	}
}

// FailWith makes every subsequent call return err, so a test can drive
// the documented fail-open path without killing a real store.
func (m *MemoryAttemptCounter) FailWith(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failErr = err
}

func (m *MemoryAttemptCounter) Locked(_ context.Context, key string, limit Limit) (Verdict, error) {
	return m.touch(key, limit, false)
}

func (m *MemoryAttemptCounter) RecordFailure(_ context.Context, key string, limit Limit) (Verdict, error) {
	return m.touch(key, limit, true)
}

func (m *MemoryAttemptCounter) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	delete(m.counts, key)
	delete(m.expires, key)
	return nil
}

func (m *MemoryAttemptCounter) touch(key string, limit Limit, incr bool) (Verdict, error) {
	if key == "" {
		return Verdict{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return Verdict{}, m.failErr
	}
	now := m.now()
	if exp, ok := m.expires[key]; ok && !now.Before(exp) {
		delete(m.counts, key)
		delete(m.expires, key)
	}
	window := limit.Window
	if window <= 0 {
		window = 15 * time.Minute
	}
	if incr {
		m.counts[key]++
		if _, ok := m.expires[key]; !ok {
			m.expires[key] = now.Add(window)
		}
	}
	n := m.counts[key]
	if n == 0 {
		return Verdict{}, nil
	}
	if _, ok := m.expires[key]; !ok {
		m.expires[key] = now.Add(window) // heal the orphan, as the script does
	}
	v := Verdict{Count: n, RetryAfter: time.Until(m.expires[key])}
	if limit.Threshold >= 1 && n >= int64(limit.Threshold) {
		v.Locked = true
	}
	if v.RetryAfter < 0 {
		v.RetryAfter = 0
	}
	return v, nil
}
