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
	// PR 6: Status ("pending"|"fencing"|"done"), FirstPassAt, FenceUntil,
	// DoneAt, Attempts, NextAttemptAt.
}
