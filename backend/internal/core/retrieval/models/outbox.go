// Package models holds the retrieval module's MongoDB document shapes.
package models

import "time"

// AppliedState is what the index worker last applied for a key.
type AppliedState struct {
	Generation  int    `bson:"generation"`
	Revision    int64  `bson:"revision"`
	ProfileHash string `bson:"profileHash"`
	IndexedHash string `bson:"indexedHash"`
	Chunks      int    `bson:"chunks"`
}

// OutboxEntry is one (tenant, corpus, source) key of retrieval_outbox.
type OutboxEntry struct {
	TenantID      string        `bson:"tenantId"`
	Corpus        string        `bson:"corpus"`
	SourceID      string        `bson:"sourceId"`
	Revision      int64         `bson:"revision"` // highest revision enqueued ($max)
	Dirty         bool          `bson:"dirty"`    // set by Enqueue/Requeue/Reschedule, cleared by Claim
	Epoch         int64         `bson:"epoch"`    // +1 on every Claim
	LeaseOwner    string        `bson:"leaseOwner,omitempty"`
	LeaseUntil    *time.Time    `bson:"leaseUntil,omitempty"`
	Attempts      int           `bson:"attempts"`
	NextAttemptAt time.Time     `bson:"nextAttemptAt"`
	LastError     string        `bson:"lastError,omitempty"` // closed code set
	Applied       *AppliedState `bson:"applied,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	UpdatedAt     time.Time     `bson:"updatedAt"`
}
