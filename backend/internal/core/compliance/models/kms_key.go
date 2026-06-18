package models

import "time"

// KMSKeysCollection holds per-tenant Data Encryption Keys (DEKs)
// wrapped with a platform master key. One row per tenant.
const KMSKeysCollection = "compliance_kms_keys"

// KMSKey states. Matches AWS KMS's coarse vocabulary so a future
// migration to AWS KMS can map cleanly.
const (
	KMSStateActive          = "active"
	KMSStatePendingDeletion = "pending_deletion"
)

// KMSKey persists a tenant's wrapped Data Encryption Key. The
// EncryptedDEK field holds the DEK under AES-256-GCM with the master
// key (env ORKESTRA_KMS_MASTER_KEY). Shredding amounts to clearing
// EncryptedDEK and flipping State to pending_deletion: the row stays
// for audit (auditors may need proof the key existed + when it was
// shredded) but the key material is gone.
type KMSKey struct {
	UUID         string     `bson:"uuid" json:"uuid"`
	TenantUUID   string     `bson:"tenantUuid" json:"tenantUuid"`
	Alias        string     `bson:"alias" json:"alias"`
	EncryptedDEK []byte     `bson:"encryptedDek,omitempty" json:"-"`
	Nonce        []byte     `bson:"nonce,omitempty" json:"-"`
	State        string     `bson:"state" json:"state"`
	CreatedAt    time.Time  `bson:"createdAt" json:"createdAt"`
	ShreddedAt   *time.Time `bson:"shreddedAt,omitempty" json:"shreddedAt,omitempty"`
}
