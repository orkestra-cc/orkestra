package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MFAChallengePurpose categorises the flow a challenge belongs to so the
// wrong purpose can't be consumed where it shouldn't (e.g. an enroll
// challenge satisfying a login step).
type MFAChallengePurpose string

const (
	MFAPurposeEnroll MFAChallengePurpose = "enroll"
	MFAPurposeLogin  MFAChallengePurpose = "login"   // consumed in Block B
	MFAPurposeStepUp MFAChallengePurpose = "step_up" // consumed in Block D
)

// MFAChallengeTTL is the Redis TTL applied to newly issued challenges.
// Five minutes leaves room for the user to open an authenticator app and
// type a code without being so long that a leaked challengeId stays useful.
const MFAChallengeTTL = 5 * time.Minute

// MFAMaxAttempts caps the number of guesses per challenge. Exceeding this
// causes the challenge to be deleted — the client must start a fresh flow.
const MFAMaxAttempts = 5

// ErrMFAChallengeNotFound is returned when Get/Consume/IncrementAttempts is
// called with a challenge ID that does not exist (expired, consumed, or
// exhausted). Callers treat it as "unauthenticated".
var ErrMFAChallengeNotFound = errors.New("mfa challenge not found")

// MFAChallenge is the Redis-stored payload. PendingSecret is only populated
// for enrollment challenges; session, device, and source-AMR fields are
// populated for login challenges so the verify endpoint can mint a token pair
// without round-tripping the user's password or changing session identity.
type MFAChallenge struct {
	ID            string              `json:"id"`
	UserUUID      string              `json:"userUuid"`
	Purpose       MFAChallengePurpose `json:"purpose"`
	PendingSecret string              `json:"pendingSecret,omitempty"`
	Attempts      int                 `json:"attempts"`
	CreatedAt     time.Time           `json:"createdAt"`
	ExpiresAt     time.Time           `json:"expiresAt"`

	// Login-continuation fields — populated only for MFAPurposeLogin.
	// SourceAMR records the factors already completed at login time
	// (typically ["pwd"] or ["oauth"]); "otp" is appended on successful
	// verify to form the final token's amr claim.
	DeviceID    string   `json:"deviceId,omitempty"`
	DeviceType  string   `json:"deviceType,omitempty"`
	SessionID   string   `json:"sessionId,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	IPAddress   string   `json:"ipAddress,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	UserAgent   string   `json:"userAgent,omitempty"`
	LoginMethod string   `json:"loginMethod,omitempty"`
	RiskScore   float64  `json:"riskScore,omitempty"`
	RiskFactors []string `json:"riskFactors,omitempty"`
	TrustLevel  string   `json:"trustLevel,omitempty"`
	SourceAMR   []string `json:"sourceAmr,omitempty"`
}

// LoginChallengeInput bundles the device + network context the login flow
// needs to stash alongside a challenge. Keeping this as a struct rather
// than a grab-bag of parameters keeps the call sites readable and lets
// future fields (e.g. risk score) slot in without rippling through the
// function signature.
type LoginChallengeInput struct {
	UserUUID    string
	SourceAMR   []string
	DeviceID    string
	DeviceType  string
	SessionID   string
	Platform    string
	IPAddress   string
	Fingerprint string
	UserAgent   string
	LoginMethod string
	RiskScore   float64
	RiskFactors []string
	TrustLevel  string
}

// MFAChallengeService issues, looks up, and consumes short-lived challenges
// in Redis. Kept small on purpose: the TOTP verification itself lives in
// the MFA service; this layer is just secure state for a single flow.
type MFAChallengeService interface {
	Begin(ctx context.Context, userUUID string, purpose MFAChallengePurpose, pendingSecret string) (*MFAChallenge, error)
	// BeginLogin mints a login-continuation challenge carrying enough
	// device/network context to mint a TokenPair after verify. Purpose is
	// always MFAPurposeLogin; pendingSecret is never set.
	BeginLogin(ctx context.Context, in LoginChallengeInput) (*MFAChallenge, error)
	Peek(ctx context.Context, id string) (*MFAChallenge, error)
	Consume(ctx context.Context, id string) (*MFAChallenge, error)
	IncrementAttempts(ctx context.Context, id string) (int, error)
}

type mfaChallengeService struct {
	store OAuthStateStore // reused: same Set/Get/Delete surface we already have
}

// NewMFAChallengeService constructs the service on top of any storage that
// satisfies OAuthStateStore — notably RedisOAuthStateStore. Sharing the
// store type avoids a second Redis adapter for a nearly-identical pattern.
func NewMFAChallengeService(store OAuthStateStore) MFAChallengeService {
	return &mfaChallengeService{store: store}
}

func (s *mfaChallengeService) Begin(ctx context.Context, userUUID string, purpose MFAChallengePurpose, pendingSecret string) (*MFAChallenge, error) {
	if userUUID == "" {
		return nil, fmt.Errorf("userUUID is required")
	}
	now := time.Now()
	ch := &MFAChallenge{
		ID:            uuid.NewString(),
		UserUUID:      userUUID,
		Purpose:       purpose,
		PendingSecret: pendingSecret,
		Attempts:      0,
		CreatedAt:     now,
		ExpiresAt:     now.Add(MFAChallengeTTL),
	}

	payload, err := json.Marshal(ch)
	if err != nil {
		return nil, fmt.Errorf("marshal mfa challenge: %w", err)
	}
	if err := s.store.Set(ctx, buildMFAChallengeKey(ch.ID), payload, MFAChallengeTTL); err != nil {
		return nil, fmt.Errorf("store mfa challenge: %w", err)
	}
	return ch, nil
}

func (s *mfaChallengeService) BeginLogin(ctx context.Context, in LoginChallengeInput) (*MFAChallenge, error) {
	if in.UserUUID == "" {
		return nil, fmt.Errorf("userUUID is required")
	}
	now := time.Now()
	ch := &MFAChallenge{
		ID:          uuid.NewString(),
		UserUUID:    in.UserUUID,
		Purpose:     MFAPurposeLogin,
		Attempts:    0,
		CreatedAt:   now,
		ExpiresAt:   now.Add(MFAChallengeTTL),
		DeviceID:    in.DeviceID,
		DeviceType:  in.DeviceType,
		SessionID:   in.SessionID,
		Platform:    in.Platform,
		IPAddress:   in.IPAddress,
		Fingerprint: in.Fingerprint,
		UserAgent:   in.UserAgent,
		LoginMethod: in.LoginMethod,
		RiskScore:   in.RiskScore,
		RiskFactors: append([]string(nil), in.RiskFactors...),
		TrustLevel:  in.TrustLevel,
		SourceAMR:   append([]string(nil), in.SourceAMR...),
	}
	payload, err := json.Marshal(ch)
	if err != nil {
		return nil, fmt.Errorf("marshal mfa login challenge: %w", err)
	}
	if err := s.store.Set(ctx, buildMFAChallengeKey(ch.ID), payload, MFAChallengeTTL); err != nil {
		return nil, fmt.Errorf("store mfa login challenge: %w", err)
	}
	return ch, nil
}

func (s *mfaChallengeService) Peek(ctx context.Context, id string) (*MFAChallenge, error) {
	if id == "" {
		return nil, ErrMFAChallengeNotFound
	}
	raw, err := s.store.Get(ctx, buildMFAChallengeKey(id))
	if err != nil {
		return nil, ErrMFAChallengeNotFound
	}
	var ch MFAChallenge
	if err := json.Unmarshal(raw, &ch); err != nil {
		return nil, fmt.Errorf("unmarshal mfa challenge: %w", err)
	}
	if time.Now().After(ch.ExpiresAt) {
		s.destroy(ctx, id)
		return nil, ErrMFAChallengeNotFound
	}
	ch.Attempts = s.attemptsFor(ctx, id)
	return &ch, nil
}

func (s *mfaChallengeService) Consume(ctx context.Context, id string) (*MFAChallenge, error) {
	if id == "" {
		return nil, ErrMFAChallengeNotFound
	}
	raw, err := s.store.Take(ctx, buildMFAChallengeKey(id))
	if err != nil {
		return nil, ErrMFAChallengeNotFound
	}
	var ch MFAChallenge
	if err := json.Unmarshal(raw, &ch); err != nil {
		return nil, fmt.Errorf("unmarshal mfa challenge: %w", err)
	}
	if time.Now().After(ch.ExpiresAt) {
		_ = s.store.Delete(ctx, buildMFAAttemptsKey(id))
		return nil, ErrMFAChallengeNotFound
	}
	ch.Attempts = s.attemptsFor(ctx, id)
	// The challenge is already gone atomically. The separate attempt key
	// contains no proof material and is safe to clean up best-effort.
	_ = s.store.Delete(ctx, buildMFAAttemptsKey(id))
	return &ch, nil
}

// IncrementAttempts bumps the counter and returns the new value. When the
// counter reaches MFAMaxAttempts the challenge is deleted, forcing the
// client to start over — the cheapest rate limiter for a 6-digit code,
// and the ONLY one on the login-verify path.
//
// The counter lives in its own key and moves via an atomic INCR rather
// than riding the challenge JSON. Read-modify-write on the JSON (Peek →
// ++ → Set) meant concurrent verifies all read the same value and all
// wrote back the same value: N parallel guesses cost one attempt. That
// turned "5 tries per challenge" into "5 tries for a serial attacker,
// effectively unbounded for a parallel one" — against a 10^6 keyspace
// that is the difference between a useless and a useful attack.
func (s *mfaChallengeService) IncrementAttempts(ctx context.Context, id string) (int, error) {
	ch, err := s.Peek(ctx, id)
	if err != nil {
		return 0, err
	}
	remaining := time.Until(ch.ExpiresAt)
	if remaining <= 0 {
		s.destroy(ctx, id)
		return ch.Attempts, ErrMFAChallengeNotFound
	}

	n, err := s.store.Incr(ctx, buildMFAAttemptsKey(id), remaining)
	if err != nil {
		// Fail closed: a counter we cannot advance is a counter that
		// cannot cap anything, so drop the challenge rather than serve
		// unlimited guesses against it.
		s.destroy(ctx, id)
		return ch.Attempts, fmt.Errorf("increment mfa attempts: %w", err)
	}
	if int(n) >= MFAMaxAttempts {
		s.destroy(ctx, id)
	}
	return int(n), nil
}

// destroy removes a challenge and its attempt counter together — leaving
// the counter behind would let a recycled id inherit a used-up budget.
func (s *mfaChallengeService) destroy(ctx context.Context, id string) {
	_ = s.store.Delete(ctx, buildMFAChallengeKey(id))
	_ = s.store.Delete(ctx, buildMFAAttemptsKey(id))
}

// attemptsFor reads the atomic counter so Peek can report a live value
// rather than the zero frozen into the challenge JSON at Begin time.
func (s *mfaChallengeService) attemptsFor(ctx context.Context, id string) int {
	raw, err := s.store.Get(ctx, buildMFAAttemptsKey(id))
	if err != nil || len(raw) == 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	return n
}

func buildMFAChallengeKey(id string) string {
	return fmt.Sprintf("mfa:challenge:%s", id)
}

// buildMFAAttemptsKey names the atomic attempt counter for a challenge.
func buildMFAAttemptsKey(id string) string {
	return fmt.Sprintf("mfa:challenge:%s:attempts", id)
}
