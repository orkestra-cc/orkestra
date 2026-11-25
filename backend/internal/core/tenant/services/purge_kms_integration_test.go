package services

import (
	"context"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// recordingPurgeKMS is an iface.KMSProvider fake whose CreateKey is
// idempotent per tenant (the LocalKMS contract: an existing key is returned,
// never a second one) and which records every DeleteKey call, so a test can
// prove which key a purge crypto-shredded.
type recordingPurgeKMS struct {
	mu      sync.Mutex
	keys    map[string]string // tenantUUID -> keyID
	created []string          // tenantUUIDs passed to CreateKey
	deleted []string          // keyIDs passed to DeleteKey
}

var _ iface.KMSProvider = (*recordingPurgeKMS)(nil)

func newRecordingPurgeKMS() *recordingPurgeKMS {
	return &recordingPurgeKMS{keys: map[string]string{}}
}

func (f *recordingPurgeKMS) CreateKey(_ context.Context, tenantUUID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, tenantUUID)
	if id, ok := f.keys[tenantUUID]; ok {
		return id, nil
	}
	id := "kms-" + tenantUUID
	f.keys[tenantUUID] = id
	return id, nil
}

func (f *recordingPurgeKMS) Encrypt(_ context.Context, _ string, plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

func (f *recordingPurgeKMS) Decrypt(_ context.Context, _ string, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

func (f *recordingPurgeKMS) DeleteKey(_ context.Context, keyID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, keyID)
	return nil
}

func (f *recordingPurgeKMS) snapshot() (created, deleted []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.created...), append([]string(nil), f.deleted...)
}

// TestPurgeTenant_ShredsLazilyMintedKey covers a tenant created before KMS
// was wired: its row has no KMSKeyID, but a consumer (the llm vault) later
// minted the tenant's key through the idempotent CreateKey and kept the key
// ID only in its own envelopes. The purge must still find and shred that
// key, or the sealed data stays recoverable.
func TestPurgeTenant_ShredsLazilyMintedKey(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	kms := newRecordingPurgeKMS()
	svc.SetKMSProvider(kms)
	ctx := context.Background()

	tn := seedDefaultsTenant(t, repo, nil) // no KMSKeyID stamped
	lazyKey, err := kms.CreateKey(ctx, tn.UUID)
	if err != nil {
		t.Fatalf("lazy CreateKey: %v", err)
	}

	if err := svc.PurgeTenant(ctx, tn.UUID); err != nil {
		t.Fatalf("PurgeTenant: %v", err)
	}
	_, deleted := kms.snapshot()
	if len(deleted) != 1 || deleted[0] != lazyKey {
		t.Fatalf("DeleteKey calls = %v, want exactly [%s] (the lazily minted key)", deleted, lazyKey)
	}
	row, err := repo.GetTenantByUUIDIncludingDeleted(ctx, tn.UUID)
	if err != nil {
		t.Fatalf("GetTenantByUUIDIncludingDeleted: %v", err)
	}
	if row.Status != models.TenantStatusPurged {
		t.Fatalf("status = %q, want purged", row.Status)
	}
}

// TestPurgeTenant_ShredsStampedKey pins the unchanged path: a tenant whose
// row names its key has exactly that key shredded, without resolving it
// again through CreateKey.
func TestPurgeTenant_ShredsStampedKey(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	kms := newRecordingPurgeKMS()
	svc.SetKMSProvider(kms)
	ctx := context.Background()

	stamped := "kms-stamped-key"
	tn := seedDefaultsTenant(t, repo, func(tn *models.Tenant) { tn.KMSKeyID = &stamped })

	if err := svc.PurgeTenant(ctx, tn.UUID); err != nil {
		t.Fatalf("PurgeTenant: %v", err)
	}
	created, deleted := kms.snapshot()
	if len(created) != 0 {
		t.Fatalf("CreateKey calls = %v, want none for a tenant with a stamped key", created)
	}
	if len(deleted) != 1 || deleted[0] != stamped {
		t.Fatalf("DeleteKey calls = %v, want exactly [%s]", deleted, stamped)
	}
}
