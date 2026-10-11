package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

const (
	siwcStateTTL    = 10 * time.Minute
	siwcStatePrefix = "llm:siwc:state:"
	siwcStateMaxLen = 256
	siwcStateField  = "siwc_state"
)

// ErrSIWCStateUnavailable: the one-shot store cannot be trusted right now
// (no Redis, or a Redis command failed). It fails closed, unlike the rate
// limiter: a state that cannot be consumed exactly once must not be honored.
var ErrSIWCStateUnavailable = errors.New("llm siwc: state store unavailable")

// SIWCAttempt is everything an authorization attempt is bound to. It is
// sealed at rest: the verifier and nonce never sit readable in Redis.
type SIWCAttempt struct {
	UserUUID    string    `json:"u"`
	TenantID    string    `json:"t"`
	Label       string    `json:"l,omitempty"`
	HostID      string    `json:"h"`
	RedirectURI string    `json:"r"`
	Verifier    string    `json:"v"`
	Nonce       string    `json:"n"`
	Mode        string    `json:"m"`
	ClientID    string    `json:"c"` // dynamic_agent_client, or the issued client id on reauth
	AccountUUID string    `json:"a,omitempty"`
	CreatedAt   time.Time `json:"at"`
}

// siwcStateRecord is what Redis holds: the tenant in clear (the vault
// context needs it to open the envelope; it is also bound in the AAD, so
// tampering fails the open) and the sealed attempt. models.Envelope carries
// json:"-" tags by design, hence the explicit wire form.
type siwcStateRecord struct {
	Tenant string       `json:"t"`
	Env    siwcStateEnv `json:"env"`
}

type siwcStateEnv struct {
	Alg           string `json:"alg"`
	KeyID         string `json:"kid,omitempty"`
	KeyVersion    int    `json:"kv"`
	SchemaVersion int    `json:"sv"`
	Ciphertext    string `json:"ct"`
}

// SIWCStateStore keeps authorization attempts for ten minutes. Take is
// one-shot: the first caller to increment the marker wins; everyone else,
// including a replay of the same callback, sees ErrSIWCStateInvalid.
type SIWCStateStore struct {
	redis module.RedisClient
	vault *Vault
	now   func() time.Time
}

func NewSIWCStateStore(redis module.RedisClient, vault *Vault) *SIWCStateStore {
	return &SIWCStateStore{redis: redis, vault: vault, now: time.Now}
}

func siwcStateDigest(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

func (s *SIWCStateStore) ready() error {
	if s == nil || s.redis == nil || s.vault == nil {
		return ErrSIWCStateUnavailable
	}
	return nil
}

// Put seals the attempt under (attempt.TenantID, sha256(state), "siwc_state")
// and stores it for ten minutes. Vault errors (notably ErrSecretKeyMissing)
// are returned wrapped, so errors.Is still matches.
func (s *SIWCStateStore) Put(ctx context.Context, state string, a SIWCAttempt) error {
	if err := s.ready(); err != nil {
		return err
	}
	if state == "" || len(state) > siwcStateMaxLen || a.TenantID == "" {
		return ErrSIWCStateInvalid
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.now().UTC()
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("llm siwc: encode attempt: %w", err)
	}
	digest := siwcStateDigest(state)
	env, err := s.vault.Seal(ctx, a.TenantID, digest, siwcStateField, string(raw))
	if err != nil {
		return fmt.Errorf("llm siwc: seal attempt: %w", err)
	}
	blob, err := json.Marshal(siwcStateRecord{Tenant: a.TenantID, Env: siwcStateEnv{
		Alg: env.Alg, KeyID: env.KeyID, KeyVersion: env.KeyVersion,
		SchemaVersion: env.SchemaVersion, Ciphertext: env.Ciphertext,
	}})
	if err != nil {
		return fmt.Errorf("llm siwc: encode record: %w", err)
	}
	if err := s.redis.Set(ctx, siwcStatePrefix+digest, string(blob), siwcStateTTL); err != nil {
		return fmt.Errorf("%w: store attempt", ErrSIWCStateUnavailable)
	}
	return nil
}

// Take consumes the attempt: the first caller wins, every other (and every
// unknown, expired or tampered state) gets ErrSIWCStateInvalid. A store
// failure is ErrSIWCStateUnavailable.
func (s *SIWCStateStore) Take(ctx context.Context, state string) (*SIWCAttempt, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if state == "" || len(state) > siwcStateMaxLen {
		return nil, ErrSIWCStateInvalid
	}
	digest := siwcStateDigest(state)
	key := siwcStatePrefix + digest
	n, err := incrCounter(ctx, s.redis, key+":used", siwcStateTTL)
	if err != nil {
		return nil, fmt.Errorf("%w: consume marker", ErrSIWCStateUnavailable)
	}
	if n != 1 {
		return nil, ErrSIWCStateInvalid
	}
	blob, err := s.redis.Get(ctx, key)
	if err != nil {
		return nil, ErrSIWCStateInvalid // expired or never issued
	}
	_ = s.redis.Del(ctx, key)
	var rec siwcStateRecord
	if json.Unmarshal([]byte(blob), &rec) != nil || rec.Tenant == "" {
		return nil, ErrSIWCStateInvalid
	}
	raw, err := s.vault.Open(ctx, rec.Tenant, digest, siwcStateField, models.Envelope{
		Alg: rec.Env.Alg, KeyID: rec.Env.KeyID, KeyVersion: rec.Env.KeyVersion,
		SchemaVersion: rec.Env.SchemaVersion, Ciphertext: rec.Env.Ciphertext,
	})
	if err != nil {
		if errors.Is(err, ErrSecretKeyMissing) {
			return nil, fmt.Errorf("llm siwc: open attempt: %w", err)
		}
		return nil, ErrSIWCStateInvalid
	}
	var a SIWCAttempt
	if json.Unmarshal([]byte(raw), &a) != nil || a.TenantID != rec.Tenant {
		return nil, ErrSIWCStateInvalid
	}
	if s.now().Sub(a.CreatedAt) > siwcStateTTL {
		return nil, ErrSIWCStateInvalid
	}
	return &a, nil
}
