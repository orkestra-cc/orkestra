package models

import "time"

const (
	ProgressBuilding = "building"
	ProgressDone     = "done"
)

// TenantProgress tracks the resumable initial build of one (tenant, corpus,
// generation).
type TenantProgress struct {
	TenantID   string     `bson:"tenantId"`
	Corpus     string     `bson:"corpus"`
	Generation int        `bson:"generation"`
	Status     string     `bson:"status"`
	Cursor     string     `bson:"cursor"`
	Enqueued   int64      `bson:"enqueued"`
	Epoch      int64      `bson:"epoch"`
	LeaseOwner string     `bson:"leaseOwner,omitempty"`
	LeaseUntil *time.Time `bson:"leaseUntil,omitempty"`
	StartedAt  time.Time  `bson:"startedAt"`
	UpdatedAt  time.Time  `bson:"updatedAt"`
	DoneAt     *time.Time `bson:"doneAt,omitempty"`
}
