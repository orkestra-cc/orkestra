package services

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/compliance/models"
)

// inMemoryKMSRepo is a minimal in-memory stand-in for the Mongo repo
// — used to exercise the Shred lifecycle and the ErrKMSKeyNotFound
// contract without booting MongoDB. The tenant service relies on the
// not-found error to log retries on transient crypto-shred failures.
type inMemoryKMSRepo struct {
	byUUID   map[string]*models.KMSKey
	byTenant map[string]string
}

func newInMemoryKMSRepo() *inMemoryKMSRepo {
	return &inMemoryKMSRepo{
		byUUID:   map[string]*models.KMSKey{},
		byTenant: map[string]string{},
	}
}
func (r *inMemoryKMSRepo) Insert(_ context.Context, k *models.KMSKey) error {
	r.byUUID[k.UUID] = k
	r.byTenant[k.TenantUUID] = k.UUID
	return nil
}
func (r *inMemoryKMSRepo) GetByUUID(_ context.Context, uuid string) (*models.KMSKey, error) {
	k, ok := r.byUUID[uuid]
	if !ok {
		return nil, iface.ErrKMSKeyNotFound
	}
	return k, nil
}
func (r *inMemoryKMSRepo) Shred(_ context.Context, uuid string) error {
	k, ok := r.byUUID[uuid]
	if !ok {
		return iface.ErrKMSKeyNotFound
	}
	k.EncryptedDEK = nil
	k.Nonce = nil
	k.State = models.KMSStatePendingDeletion
	now := time.Now()
	k.ShreddedAt = &now
	return nil
}

// TestAEADRoundTrip pins the AES-GCM envelope primitive the KMS uses:
// plaintext sealed with a given key + nonce + associated data is
// recovered exactly. Exercises the same `newGCM` helper LocalKMS uses
// so a regression in the AEAD wiring is caught here.
func TestAEADRoundTrip(t *testing.T) {
	t.Parallel()
	aead, err := newGCM(bytes32(0))
	if err != nil {
		t.Fatalf("newGCM: %v", err)
	}
	nonce := bytes12(1)
	plaintext := []byte("hello compliance")
	sealed := aead.Seal(nil, nonce, plaintext, []byte("tenant-42"))
	got, err := aead.Open(nil, nonce, sealed, []byte("tenant-42"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

// TestAEADRejectsWrongAssociatedData ensures ciphertext sealed under
// one keyID cannot be opened under another. LocalKMS.Encrypt passes
// keyID as additional authenticated data so a mixed-up keyID fails
// loudly rather than returning garbage plaintext.
func TestAEADRejectsWrongAssociatedData(t *testing.T) {
	t.Parallel()
	aead, err := newGCM(bytes32(5))
	if err != nil {
		t.Fatalf("newGCM: %v", err)
	}
	nonce := bytes12(7)
	sealed := aead.Seal(nil, nonce, []byte("msg"), []byte("key-A"))
	if _, err := aead.Open(nil, nonce, sealed, []byte("key-B")); err == nil {
		t.Fatal("Open should fail with mismatched associated data")
	}
}

// TestHexDecodeValidation pins that the master-key parser rejects
// malformed input so typos in ORKESTRA_KMS_MASTER_KEY don't silently
// fall back to a zero or partial key — a silent fallback would
// catastrophically weaken every wrapped DEK.
func TestHexDecodeValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", true},
		{"too short", "deadbeef", true},
		{"non-hex char", string(make([]byte, 64)), true},
		{"valid 32 bytes", "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeHex32(tc.in)
			if tc.wantErr != (err != nil) {
				t.Fatalf("decodeHex32(%q) error=%v, wantErr=%v", tc.in, err, tc.wantErr)
			}
		})
	}
}

// TestShredMarksKeyDeleted exercises the crypto-shred primitive:
// after Shred, the key metadata survives with state=pending_deletion
// but EncryptedDEK is gone. This is the row a compliance auditor
// inspects to confirm the DEK has actually been destroyed.
func TestShredMarksKeyDeleted(t *testing.T) {
	t.Parallel()
	repo := newInMemoryKMSRepo()
	ctx := context.Background()
	key := &models.KMSKey{
		UUID:         uuid.NewString(),
		TenantUUID:   "t-1",
		Alias:        "tenant/t-1",
		EncryptedDEK: []byte{1, 2, 3},
		Nonce:        []byte{4, 5, 6},
		State:        models.KMSStateActive,
		CreatedAt:    time.Now(),
	}
	if err := repo.Insert(ctx, key); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := repo.Shred(ctx, key.UUID); err != nil {
		t.Fatalf("shred: %v", err)
	}
	got, err := repo.GetByUUID(ctx, key.UUID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != models.KMSStatePendingDeletion {
		t.Fatalf("state = %q; want pending_deletion", got.State)
	}
	if len(got.EncryptedDEK) != 0 {
		t.Fatalf("EncryptedDEK not cleared: %v", got.EncryptedDEK)
	}
	if got.ShreddedAt == nil {
		t.Fatal("ShreddedAt not stamped")
	}
}

// TestShredMissingKeyIsNotFound validates the idempotency contract:
// attempting to shred a non-existent keyID returns ErrKMSKeyNotFound
// rather than silently succeeding. The tenant service relies on this
// to log retries on transient failures.
func TestShredMissingKeyIsNotFound(t *testing.T) {
	t.Parallel()
	repo := newInMemoryKMSRepo()
	err := repo.Shred(context.Background(), "nonexistent")
	if !stderrors.Is(err, iface.ErrKMSKeyNotFound) {
		t.Fatalf("expected ErrKMSKeyNotFound, got %v", err)
	}
}

// --- test helpers ---

func bytes32(seed byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = seed
	}
	return out
}

func bytes12(seed byte) []byte {
	out := make([]byte, 12)
	for i := range out {
		out[i] = seed
	}
	return out
}
