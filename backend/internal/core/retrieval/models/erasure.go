package models

import "time"

const (
	ErasureScopeGlobal = "global"
	ErasureScopeTenant = "tenant"

	ErasureKindUser    = "user"
	ErasureKindSubject = "subject"
	ErasureKindItem    = "item"
	ErasureKindTenant  = "tenant"
)

// Erasure is a tombstone: the index worker refuses to write anything it covers.
type Erasure struct {
	UUID      string    `bson:"uuid"`
	Scope     string    `bson:"scope"`              // "global" | "tenant"
	TenantID  string    `bson:"tenantId,omitempty"` // absent for global
	Kind      string    `bson:"kind"`               // "user" | "subject" | "item" | "tenant"
	Corpus    string    `bson:"corpus,omitempty"`   // item
	SourceID  string    `bson:"sourceId,omitempty"` // item
	Subject   string    `bson:"subject,omitempty"`  // user ("user:<uuid>") and subject
	CreatedAt time.Time `bson:"createdAt"`

	// Finalization (ADR-0023 D12). Rows written before finalization existed
	// carry none of these: Status "" reads as pending.
	Status        string     `bson:"status,omitempty"` // "" is read as pending
	FirstPassAt   *time.Time `bson:"firstPassAt,omitempty"`
	FenceUntil    *time.Time `bson:"fenceUntil,omitempty"`
	DoneAt        *time.Time `bson:"doneAt,omitempty"`
	Attempts      int        `bson:"attempts"`
	NextAttemptAt *time.Time `bson:"nextAttemptAt,omitempty"`
}

// Finalization states of an Erasure.
const (
	ErasureStatusPending = "pending"
	ErasureStatusFencing = "fencing"
	ErasureStatusDone    = "done"
)

// EffectiveStatus is Status, or pending when the row predates finalization.
func (e *Erasure) EffectiveStatus() string {
	if e.Status == "" {
		return ErasureStatusPending
	}
	return e.Status
}
