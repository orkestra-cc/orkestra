package idempotency

import (
	"context"
	"time"
)

// Record states.
const (
	StateInProgress = "in_progress"
	StateDone       = "done"
)

// Record is one idempotency entry, unique per (Scope, Key).
type Record struct {
	Scope       string    `bson:"scope"`
	Key         string    `bson:"key"`
	Fingerprint string    `bson:"fingerprint"`
	State       string    `bson:"state"` // StateInProgress | StateDone
	Status      int       `bson:"status,omitempty"`
	ContentType string    `bson:"contentType,omitempty"`
	Location    string    `bson:"location,omitempty"`
	Body        []byte    `bson:"body,omitempty"`
	BodyStored  bool      `bson:"bodyStored"`
	LockedUntil time.Time `bson:"lockedUntil"`
	ExpiresAt   time.Time `bson:"expiresAt"`
	CreatedAt   time.Time `bson:"createdAt"`
}

// Store persists idempotency records.
type Store interface {
	// Begin inserts an in_progress record, or takes over one whose
	// LockedUntil < now. It returns (nil, nil) when the caller owns the key
	// now; otherwise the existing record.
	Begin(ctx context.Context, scope, key, fingerprint string, now, lockedUntil, expiresAt time.Time) (*Record, error)
	// Finish marks the record done with the captured response.
	Finish(ctx context.Context, scope, key string, status int, contentType, location string, body []byte, bodyStored bool) error
	// Abandon deletes the record (5xx, panic, empty response).
	Abandon(ctx context.Context, scope, key string) error
}
