package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/compliance/models"
	"github.com/orkestra/backend/internal/addons/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// LocalKMS is the in-process KMS provider used in dev and simple prod
// deployments. Per-tenant DEKs are wrapped with a single master key
// (env ORKESTRA_KMS_MASTER_KEY, hex-encoded 32 bytes) and persisted in
// Mongo. A future AWS KMS provider will satisfy the same interface;
// the tenant + envelope cipher consumers are agnostic to the backend.
//
// Security model:
//   - Master key lives in an env var for v1. Rotation is not in scope
//     for 4.3 — the plan calls for a "wrap KEK with a KEK-of-KEKs"
//     rotation story in a later phase. For now, treat the master key
//     as the crown jewel: rotating it invalidates every wrapped DEK.
//   - Each tenant gets one 32-byte DEK, encrypted under the master key
//     with AES-256-GCM. The wrapped form is what's stored.
//   - Encrypt/Decrypt use the unwrapped DEK on a per-call basis. The
//     DEK is never cached in memory across calls — the master key
//     unwrap step is tiny so the overhead is negligible compared to
//     a Mongo round trip.
//   - Shred = remove the wrapped DEK from Mongo. The DEK cannot be
//     re-derived from anything else, so every ciphertext wrapped
//     with it is cryptographically unrecoverable.
type LocalKMS struct {
	repo      *repository.KMSKeyRepository
	masterKey []byte // 32 bytes
}

// NewLocalKMS reads the master key from env and builds a provider.
// Returns an error if the env var is missing or malformed — operators
// must decide what key to use; a zero-filled fallback would silently
// compromise every tenant's data.
func NewLocalKMS(repo *repository.KMSKeyRepository) (*LocalKMS, error) {
	raw := os.Getenv("ORKESTRA_KMS_MASTER_KEY")
	if raw == "" {
		return nil, fmt.Errorf("compliance: ORKESTRA_KMS_MASTER_KEY env is required (hex-encoded 32 bytes)")
	}
	key, err := decodeHex32(raw)
	if err != nil {
		return nil, fmt.Errorf("compliance: ORKESTRA_KMS_MASTER_KEY: %w", err)
	}
	return &LocalKMS{repo: repo, masterKey: key}, nil
}

// CreateKey mints a fresh DEK for tenantUUID, wraps it, and persists
// the wrapped form. Idempotent: if the tenant already has a key,
// returns the existing UUID rather than minting a second one.
func (k *LocalKMS) CreateKey(ctx context.Context, tenantUUID string) (string, error) {
	if existing, err := k.repo.GetByTenant(ctx, tenantUUID); err == nil && existing != nil {
		return existing.UUID, nil
	} else if err != nil && !stderrors.Is(err, iface.ErrKMSKeyNotFound) {
		return "", err
	}

	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return "", fmt.Errorf("compliance: generate DEK: %w", err)
	}
	nonce, wrapped, err := k.wrapDEK(dek)
	if err != nil {
		return "", err
	}
	row := &models.KMSKey{
		UUID:         uuid.NewString(),
		TenantUUID:   tenantUUID,
		Alias:        "tenant/" + tenantUUID,
		EncryptedDEK: wrapped,
		Nonce:        nonce,
		State:        models.KMSStateActive,
		CreatedAt:    time.Now().UTC(),
	}
	if err := k.repo.Insert(ctx, row); err != nil {
		return "", fmt.Errorf("compliance: insert KMS key: %w", err)
	}
	return row.UUID, nil
}

// Encrypt seals plaintext under the tenant's DEK. The ciphertext
// layout is:
//
//	[12-byte nonce][GCM-sealed (ciphertext + 16-byte auth tag)]
//
// — callers store the whole blob as a single opaque value.
func (k *LocalKMS) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	dek, err := k.unwrapByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, []byte(keyID))
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// Decrypt opens ciphertext produced by Encrypt. Returns
// ErrKMSKeyDeleted once the key has been shredded — the signal
// crypto-shred relies on to make callers notice the data is gone.
func (k *LocalKMS) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	dek, err := k.unwrapByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(ciphertext) < ns+aead.Overhead() {
		return nil, fmt.Errorf("compliance: ciphertext too short")
	}
	nonce, sealed := ciphertext[:ns], ciphertext[ns:]
	return aead.Open(nil, nonce, sealed, []byte(keyID))
}

// DeleteKey shreds the wrapped DEK. The metadata row survives with
// state=pending_deletion so auditors can prove the key existed + when
// it was shredded.
func (k *LocalKMS) DeleteKey(ctx context.Context, keyID string) error {
	return k.repo.Shred(ctx, keyID)
}

// --- internal helpers ---

// wrapDEK encrypts the raw DEK under the master key with AES-256-GCM.
// Returns (nonce, ciphertext) so the repository stores both — the
// nonce is specific to this wrap and must be preserved verbatim.
func (k *LocalKMS) wrapDEK(dek []byte) (nonce, ciphertext []byte, err error) {
	aead, err := newGCM(k.masterKey)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = aead.Seal(nil, nonce, dek, []byte("orkestra-kms-dek"))
	return nonce, ciphertext, nil
}

// unwrapByID fetches the wrapped DEK for keyID and decrypts it with
// the master key. Returns ErrKMSKeyDeleted when the row survives but
// the wrapped DEK is gone (shredded).
func (k *LocalKMS) unwrapByID(ctx context.Context, keyID string) ([]byte, error) {
	row, err := k.repo.GetByUUID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if len(row.EncryptedDEK) == 0 || len(row.Nonce) == 0 || row.State == models.KMSStatePendingDeletion {
		return nil, iface.ErrKMSKeyDeleted
	}
	aead, err := newGCM(k.masterKey)
	if err != nil {
		return nil, err
	}
	dek, err := aead.Open(nil, row.Nonce, row.EncryptedDEK, []byte("orkestra-kms-dek"))
	if err != nil {
		return nil, fmt.Errorf("compliance: unwrap DEK: %w", err)
	}
	return dek, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// decodeHex32 decodes a 64-char hex string into 32 bytes. Surfaces a
// clean error if the input is malformed so the operator can spot the
// env-var typo on boot.
func decodeHex32(s string) ([]byte, error) {
	if len(s) != 64 {
		return nil, fmt.Errorf("must be 64 hex chars (32 bytes), got %d", len(s))
	}
	out := make([]byte, 32)
	for i := 0; i < 32; i++ {
		hi, err := hexNibble(s[2*i])
		if err != nil {
			return nil, err
		}
		lo, err := hexNibble(s[2*i+1])
		if err != nil {
			return nil, err
		}
		out[i] = (hi << 4) | lo
	}
	return out, nil
}

func hexNibble(b byte) (byte, error) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', nil
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, nil
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex char %q", b)
}

// compile-time assertion: LocalKMS satisfies iface.KMSProvider.
var _ iface.KMSProvider = (*LocalKMS)(nil)
