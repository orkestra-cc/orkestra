// backend/internal/core/llm/services/vault_test.go
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type fakeKMS struct{ keys map[string]bool }

func (f *fakeKMS) CreateKey(_ context.Context, tenantUUID string) (string, error) {
	f.keys["k-"+tenantUUID] = true
	return "k-" + tenantUUID, nil
}
func (f *fakeKMS) Encrypt(_ context.Context, keyID string, p []byte) ([]byte, error) {
	if !f.keys[keyID] {
		return nil, iface.ErrKMSKeyNotFound
	}
	return append([]byte(keyID+":"), p...), nil
}
func (f *fakeKMS) Decrypt(_ context.Context, keyID string, c []byte) ([]byte, error) {
	if !f.keys[keyID] {
		return nil, iface.ErrKMSKeyDeleted
	}
	return []byte(strings.TrimPrefix(string(c), keyID+":")), nil
}
func (f *fakeKMS) DeleteKey(_ context.Context, keyID string) error { delete(f.keys, keyID); return nil }

func randomKeyHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func TestVault_LocalRoundTrip(t *testing.T) {
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	env, err := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if env.Alg != models.EnvelopeAlgLocal || env.KeyVersion != 1 || env.SchemaVersion != models.EnvelopeSchemaVersion {
		t.Fatalf("envelope = %+v", env)
	}
	got, err := v.Open(context.Background(), "t1", "c1", "secret", env)
	if err != nil || got != "sk-live-abcdef" {
		t.Fatalf("Open = %q, %v", got, err)
	}
}

func TestVault_AADBindsTenantResourceAndField(t *testing.T) {
	v, _ := NewVault(randomKeyHex(t), slog.Default())
	env, _ := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
	for _, tc := range [][3]string{{"t2", "c1", "secret"}, {"t1", "c2", "secret"}, {"t1", "c1", "refreshToken"}} {
		if _, err := v.Open(context.Background(), tc[0], tc[1], tc[2], env); !errors.Is(err, ErrEnvelopeCorrupt) {
			t.Fatalf("Open with AAD %v: err = %v, want ErrEnvelopeCorrupt", tc, err)
		}
	}
}

func TestVault_PrefersKMSAndFallsBackToLocal(t *testing.T) {
	v, _ := NewVault(randomKeyHex(t), slog.Default())
	kms := &fakeKMS{keys: map[string]bool{}}
	v.SetKMSProvider(kms)
	env, err := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
	if err != nil || env.Alg != models.EnvelopeAlgKMS || env.KeyID != "k-t1" {
		t.Fatalf("envelope = %+v, %v", env, err)
	}
	got, err := v.Open(context.Background(), "t1", "c1", "secret", env)
	if err != nil || got != "sk-live-abcdef" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	// Shredded key: the error must stay classifiable.
	_ = kms.DeleteKey(context.Background(), "k-t1")
	if _, err := v.Open(context.Background(), "t1", "c1", "secret", env); !errors.Is(err, iface.ErrKMSKeyDeleted) {
		t.Fatalf("after shred err = %v", err)
	}
}

func TestVault_NoKeyNoKMS(t *testing.T) {
	v, err := NewVault("", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if v.Available() {
		t.Fatal("Available() must be false without key and KMS")
	}
	if _, err := v.Seal(context.Background(), "t1", "c1", "secret", "x"); !errors.Is(err, ErrSecretKeyMissing) {
		t.Fatalf("Seal err = %v", err)
	}
}

func TestNewVault_RejectsBadKey(t *testing.T) {
	if _, err := NewVault("abc", slog.Default()); err == nil {
		t.Fatal("short hex key accepted")
	}
	if _, err := NewVault(strings.Repeat("zz", 32), slog.Default()); err == nil {
		t.Fatal("non-hex key accepted")
	}
}

func TestVault_KMSAADBindsTenantResourceAndField(t *testing.T) {
	v, _ := NewVault("", slog.Default())
	kms := &fakeKMS{keys: map[string]bool{}}
	v.SetKMSProvider(kms)
	env, err := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range [][3]string{{"t2", "c1", "secret"}, {"t1", "c2", "secret"}, {"t1", "c1", "refreshToken"}} {
		if _, err := v.Open(context.Background(), tc[0], tc[1], tc[2], env); !errors.Is(err, ErrEnvelopeCorrupt) {
			t.Fatalf("KMS Open with AAD %v: err = %v, want ErrEnvelopeCorrupt", tc, err)
		}
	}
}

func TestVault_SetKMSProviderNilKeepsCurrent(t *testing.T) {
	v, _ := NewVault("", slog.Default())
	v.SetKMSProvider(&fakeKMS{keys: map[string]bool{}})
	v.SetKMSProvider(nil)
	if !v.Available() {
		t.Fatal("nil provider must not clear the existing KMS")
	}
}

func TestVault_ErrorsDoNotLeakSecretOrKey(t *testing.T) {
	key := randomKeyHex(t)
	v, _ := NewVault(key, slog.Default())
	env, _ := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
	_, err := v.Open(context.Background(), "t2", "c1", "secret", env)
	if err == nil || strings.Contains(err.Error(), "sk-live-abcdef") || strings.Contains(err.Error(), key) {
		t.Fatalf("error leaks material: %v", err)
	}
	if _, err := NewVault(key[:63]+"z", slog.Default()); err == nil || strings.Contains(err.Error(), key[:63]) {
		t.Fatalf("NewVault error leaks key: %v", err)
	}
}

// scriptedKMS returns decryptErr from Decrypt; everything else round-trips.
type scriptedKMS struct {
	fakeKMS
	decryptErr error
}

func (s *scriptedKMS) Decrypt(ctx context.Context, keyID string, c []byte) ([]byte, error) {
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	return s.fakeKMS.Decrypt(ctx, keyID, c)
}

// T4.a: a KMS ciphertext that does not open is a corrupt envelope, like on
// the local path; a shredded key and an infrastructure failure keep their
// own identity (crypto-shred and retries depend on it).
func TestVault_KMSDecryptFailuresAreClassified(t *testing.T) {
	kms := &scriptedKMS{fakeKMS: fakeKMS{keys: map[string]bool{}}}
	v, _ := NewVault("", slog.Default())
	v.SetKMSProvider(kms)
	env, err := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
	if err != nil {
		t.Fatal(err)
	}
	transient := errors.New("kms repo: connection reset")
	for _, tc := range []struct {
		name        string
		decryptErr  error
		wantCorrupt bool
		wantIs      error
	}{
		{"authentication failed", fmt.Errorf("compliance: open: %w", iface.ErrKMSCiphertextInvalid), true, nil},
		{"too short", fmt.Errorf("compliance: ciphertext too short: %w", iface.ErrKMSCiphertextInvalid), true, nil},
		{"shredded key", iface.ErrKMSKeyDeleted, false, iface.ErrKMSKeyDeleted},
		{"transient", transient, false, transient},
	} {
		kms.decryptErr = tc.decryptErr
		_, err := v.Open(context.Background(), "t1", "c1", "secret", env)
		if errors.Is(err, ErrEnvelopeCorrupt) != tc.wantCorrupt {
			t.Errorf("%s: err = %v, corrupt = %v, want %v", tc.name, err, errors.Is(err, ErrEnvelopeCorrupt), tc.wantCorrupt)
		}
		if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
			t.Errorf("%s: err = %v, want it to stay %v", tc.name, err, tc.wantIs)
		}
	}
}

// T4.b: the AAD is tenant|resource|field|schema; an empty part or one that
// contains the separator could make two contexts collide, so Seal and Open
// refuse them before touching any key.
func TestVault_RejectsAmbiguousContext(t *testing.T) {
	for _, withKMS := range []bool{false, true} {
		v, _ := NewVault(randomKeyHex(t), slog.Default())
		if withKMS {
			v.SetKMSProvider(&fakeKMS{keys: map[string]bool{}})
		}
		env, err := v.Seal(context.Background(), "t1", "c1", "secret", "sk-live-abcdef")
		if err != nil {
			t.Fatal(err)
		}
		for _, parts := range [][3]string{
			{"", "c1", "secret"}, {"t1", "", "secret"}, {"t1", "c1", ""},
			{"t1|c1", "secret", "x"}, {"t1", "c1|secret", "x"}, {"t1", "c1", "secret|1"},
		} {
			if _, err := v.Seal(context.Background(), parts[0], parts[1], parts[2], "x"); !errors.Is(err, ErrInvalidEnvelopeContext) {
				t.Errorf("kms=%v Seal%v err = %v", withKMS, parts, err)
			}
			if _, err := v.Open(context.Background(), parts[0], parts[1], parts[2], env); !errors.Is(err, ErrInvalidEnvelopeContext) {
				t.Errorf("kms=%v Open%v err = %v", withKMS, parts, err)
			}
		}
	}
}
