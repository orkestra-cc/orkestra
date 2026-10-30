package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PINEnrollmentCode is what an enrollment code store keeps for one user:
// never the code itself, only its SHA-256, plus who minted it.
type PINEnrollmentCode struct {
	Hash string `json:"h"`
	Via  string `json:"via"`
	By   string `json:"by"`
	// PIN fingerprints the PIN the code was issued for (pinFingerprint):
	// redemption refuses a code whose PIN has since been removed or
	// replaced, so an outstanding code never enrolls a device for a newer
	// PIN.
	PIN string `json:"p,omitempty"`
}

// PINEnrollmentCodeStore holds at most one live device enrollment code per
// (audience, user). Put replaces any previous code. Peek reads without
// consuming, so a wrong guess never destroys the real code (the attempt
// counter bounds guessing); TakeIf deletes the record only if it still
// holds the expected hash, so exactly one redemption wins.
type PINEnrollmentCodeStore interface {
	Put(ctx context.Context, aud PolicyAudience, userUUID string, code PINEnrollmentCode, ttl time.Duration) error
	Peek(ctx context.Context, aud PolicyAudience, userUUID string) (*PINEnrollmentCode, error)
	TakeIf(ctx context.Context, aud PolicyAudience, userUUID, hash string) (bool, error)
}

// ErrPINEnrollmentCodeNotFound reports no live code for the user.
var ErrPINEnrollmentCodeNotFound = errors.New("pin enrollment code not found")

const pinEnrollmentKeyPrefix = "auth:pin:enroll:"

func pinEnrollmentKey(aud PolicyAudience, userUUID string) string {
	return pinEnrollmentKeyPrefix + string(aud) + ":" + userUUID
}

// RedisPINEnrollmentCodeStore is the production store (GET + GETDEL).
type RedisPINEnrollmentCodeStore struct {
	redis AtomicTakeRedisClient
}

// NewRedisPINEnrollmentCodeStore wires the store onto the module's Redis
// adapter.
func NewRedisPINEnrollmentCodeStore(r AtomicTakeRedisClient) *RedisPINEnrollmentCodeStore {
	return &RedisPINEnrollmentCodeStore{redis: r}
}

func (s *RedisPINEnrollmentCodeStore) Put(ctx context.Context, aud PolicyAudience, userUUID string, code PINEnrollmentCode, ttl time.Duration) error {
	raw, err := json.Marshal(code)
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, pinEnrollmentKey(aud, userUUID), string(raw), ttl)
}

func (s *RedisPINEnrollmentCodeStore) Peek(ctx context.Context, aud PolicyAudience, userUUID string) (*PINEnrollmentCode, error) {
	raw, err := s.redis.Get(ctx, pinEnrollmentKey(aud, userUUID))
	if err != nil || raw == "" {
		// The adapter reports a missing key as an error; either way there
		// is no code to redeem, and the caller answers the same 401.
		return nil, ErrPINEnrollmentCodeNotFound
	}
	var c PINEnrollmentCode
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, ErrPINEnrollmentCodeNotFound
	}
	return &c, nil
}

func (s *RedisPINEnrollmentCodeStore) TakeIf(ctx context.Context, aud PolicyAudience, userUUID, hash string) (bool, error) {
	raw, err := s.redis.GetDel(ctx, pinEnrollmentKey(aud, userUUID))
	if err != nil || raw == "" {
		return false, nil
	}
	var c PINEnrollmentCode
	if json.Unmarshal([]byte(raw), &c) != nil {
		return false, nil
	}
	// A concurrent Put replaced the code between our Peek and this take:
	// the record we just deleted is the NEW code, which this caller never
	// proved. Losing it is the safe direction — the minter issues another.
	return subtle.ConstantTimeCompare([]byte(c.Hash), []byte(hash)) == 1, nil
}

// MemoryPINEnrollmentCodeStore is the no-Redis stand-in for tests. TTLs
// are honoured against the wall clock.
type MemoryPINEnrollmentCodeStore struct {
	mu   sync.Mutex
	rows map[string]memoryPINCode
}

type memoryPINCode struct {
	code    PINEnrollmentCode
	expires time.Time
}

func NewMemoryPINEnrollmentCodeStore() *MemoryPINEnrollmentCodeStore {
	return &MemoryPINEnrollmentCodeStore{rows: map[string]memoryPINCode{}}
}

func (s *MemoryPINEnrollmentCodeStore) Put(_ context.Context, aud PolicyAudience, userUUID string, code PINEnrollmentCode, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[pinEnrollmentKey(aud, userUUID)] = memoryPINCode{code: code, expires: time.Now().Add(ttl)}
	return nil
}

func (s *MemoryPINEnrollmentCodeStore) Peek(_ context.Context, aud PolicyAudience, userUUID string) (*PINEnrollmentCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[pinEnrollmentKey(aud, userUUID)]
	if !ok || time.Now().After(row.expires) {
		return nil, ErrPINEnrollmentCodeNotFound
	}
	c := row.code
	return &c, nil
}

func (s *MemoryPINEnrollmentCodeStore) TakeIf(_ context.Context, aud PolicyAudience, userUUID, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := pinEnrollmentKey(aud, userUUID)
	row, ok := s.rows[key]
	if !ok || time.Now().After(row.expires) {
		return false, nil
	}
	delete(s.rows, key)
	return subtle.ConstantTimeCompare([]byte(row.code.Hash), []byte(hash)) == 1, nil
}

// pinEnrollmentAlphabet drops 0/O, 1/I/L: the code is read off one screen
// and typed on another.
const pinEnrollmentAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newPINEnrollmentCode returns an 8-character code (≈39 bits) formatted
// XXXX-XXXX for display.
func newPINEnrollmentCode() (string, error) {
	// Rejection sampling: 256 is not a multiple of 31, so a plain modulo
	// would favour the first eight symbols.
	limit := byte(256 - 256%len(pinEnrollmentAlphabet))
	out := make([]byte, 0, 9)
	buf := make([]byte, 16)
	for len(out) < 9 {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("enrollment code entropy: %w", err)
		}
		for _, b := range buf {
			if len(out) == 9 {
				break
			}
			if b >= limit {
				continue
			}
			if len(out) == 4 {
				out = append(out, '-')
			}
			out = append(out, pinEnrollmentAlphabet[int(b)%len(pinEnrollmentAlphabet)])
		}
	}
	return string(out), nil
}

// normalisePINEnrollmentCode accepts the code as displayed or as typed:
// case-insensitive, dashes and spaces ignored.
func normalisePINEnrollmentCode(code string) string {
	code = strings.ToUpper(code)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, code)
}

func hashPINSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
