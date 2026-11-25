// backend/internal/core/llm/services/vault.go
package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const localKeyVersion = 1

// Vault seals and opens module secrets. KMS (per-tenant DEK, crypto-shred
// on purge) wins when compliance injected one; otherwise AES-256-GCM with
// the dedicated local key. The AAD binds every ciphertext to
// tenant|resource|field|schema so an envelope copied elsewhere fails to
// open instead of leaking. The display-only last four characters are the
// caller's job (models.Last4); the envelope never carries them.
type Vault struct {
	mu       sync.RWMutex
	kms      iface.KMSProvider
	localKey []byte // nil when LLM_SECRET_ENCRYPTION_KEY is unset
	logger   *slog.Logger
}

func NewVault(localKeyHex string, logger *slog.Logger) (*Vault, error) {
	if logger == nil {
		logger = slog.Default()
	}
	v := &Vault{logger: logger}
	if localKeyHex != "" {
		key, err := hex.DecodeString(localKeyHex)
		if err != nil || len(key) != 32 {
			return nil, errors.New("llm: LLM_SECRET_ENCRYPTION_KEY must be 64 hex characters (32 bytes)")
		}
		v.localKey = key
	}
	return v, nil
}

// SetKMSProvider satisfies iface.KMSProviderSetter; compliance calls it
// after its own Init. nil leaves the current provider untouched.
func (v *Vault) SetKMSProvider(k iface.KMSProvider) {
	if k == nil {
		return
	}
	v.mu.Lock()
	v.kms = k
	v.mu.Unlock()
}

func (v *Vault) current() (iface.KMSProvider, []byte) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.kms, v.localKey
}

func (v *Vault) Available() bool {
	k, local := v.current()
	return k != nil || local != nil
}

func aad(tenantID, resourceUUID, field string, schema int) []byte {
	return []byte(tenantID + "|" + resourceUUID + "|" + field + "|" + strconv.Itoa(schema))
}

// checkContext refuses an AAD part that is empty or contains the '|'
// separator: either could make two different contexts produce the same
// AAD. All parts are server-generated today; this fails closed if a caller
// ever passes something else.
func checkContext(parts ...string) error {
	for _, p := range parts {
		if p == "" || strings.Contains(p, "|") {
			return ErrInvalidEnvelopeContext
		}
	}
	return nil
}

func (v *Vault) Seal(ctx context.Context, tenantID, resourceUUID, field, plaintext string) (models.Envelope, error) {
	if err := checkContext(tenantID, resourceUUID, field); err != nil {
		return models.Envelope{}, err
	}
	kms, local := v.current()
	env := models.Envelope{SchemaVersion: models.EnvelopeSchemaVersion}
	ad := aad(tenantID, resourceUUID, field, env.SchemaVersion)
	switch {
	case kms != nil:
		keyID, err := kms.CreateKey(ctx, tenantID)
		if err != nil {
			return models.Envelope{}, fmt.Errorf("llm: kms create key: %w", err)
		}
		// KMS ciphertext is self-contained; the AAD is prepended in clear
		// and checked on Open so cross-record swaps still fail.
		ct, err := kms.Encrypt(ctx, keyID, append(append([]byte{}, ad...), append([]byte{0}, []byte(plaintext)...)...))
		if err != nil {
			return models.Envelope{}, fmt.Errorf("llm: kms encrypt: %w", err)
		}
		env.Alg, env.KeyID, env.KeyVersion = models.EnvelopeAlgKMS, keyID, 0
		env.Ciphertext = base64.StdEncoding.EncodeToString(ct)
		return env, nil
	case local != nil:
		block, err := aes.NewCipher(local)
		if err != nil {
			return models.Envelope{}, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return models.Envelope{}, err
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return models.Envelope{}, err
		}
		ct := gcm.Seal(nonce, nonce, []byte(plaintext), ad)
		env.Alg, env.KeyVersion = models.EnvelopeAlgLocal, localKeyVersion
		env.Ciphertext = base64.StdEncoding.EncodeToString(ct)
		return env, nil
	default:
		return models.Envelope{}, ErrSecretKeyMissing
	}
}

func (v *Vault) Open(ctx context.Context, tenantID, resourceUUID, field string, env models.Envelope) (string, error) {
	if err := checkContext(tenantID, resourceUUID, field); err != nil {
		return "", err
	}
	if env.IsZero() {
		return "", ErrEnvelopeCorrupt
	}
	raw, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return "", ErrEnvelopeCorrupt
	}
	ad := aad(tenantID, resourceUUID, field, env.SchemaVersion)
	kms, local := v.current()
	switch env.Alg {
	case models.EnvelopeAlgKMS:
		if kms == nil {
			return "", ErrSecretKeyMissing
		}
		pt, err := kms.Decrypt(ctx, env.KeyID, raw)
		if errors.Is(err, iface.ErrKMSCiphertextInvalid) {
			return "", ErrEnvelopeCorrupt
		}
		if err != nil {
			// iface.ErrKMSKeyDeleted (crypto-shred) and infrastructure
			// failures stay classifiable as themselves.
			return "", err
		}
		sep := len(ad)
		if len(pt) < sep+1 || string(pt[:sep]) != string(ad) || pt[sep] != 0 {
			return "", ErrEnvelopeCorrupt
		}
		return string(pt[sep+1:]), nil
	case models.EnvelopeAlgLocal:
		if local == nil {
			return "", ErrSecretKeyMissing
		}
		block, err := aes.NewCipher(local)
		if err != nil {
			return "", err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return "", err
		}
		if len(raw) < gcm.NonceSize() {
			return "", ErrEnvelopeCorrupt
		}
		pt, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], ad)
		if err != nil {
			return "", ErrEnvelopeCorrupt
		}
		return string(pt), nil
	default:
		return "", ErrEnvelopeCorrupt
	}
}
