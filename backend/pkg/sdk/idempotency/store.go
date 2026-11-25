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
	Owner       string    `bson:"owner,omitempty"` // lock token of the request that executes
	LockedUntil time.Time `bson:"lockedUntil"`
	ExpiresAt   time.Time `bson:"expiresAt"`
	CreatedAt   time.Time `bson:"createdAt"`
}

// Store persists idempotency records. Every lock has an owner token: Finish,
// Abandon and Renew act only while the record still carries it, so a stale
// request whose lock was taken over cannot touch the new owner's record.
type Store interface {
	// Begin inserts an in_progress record, or takes over one whose
	// LockedUntil < now and whose fingerprint equals fingerprint. It returns
	// (owner, nil, nil) when the caller owns the key now; otherwise
	// ("", existing, nil).
	Begin(ctx context.Context, scope, key, fingerprint string, now, lockedUntil, expiresAt time.Time) (owner string, existing *Record, err error)
	// Renew extends the lock; false means the caller no longer owns it.
	Renew(ctx context.Context, scope, key, owner string, lockedUntil time.Time) (bool, error)
	// Finish marks the record done with the captured response; false means
	// the caller no longer owns the lock and nothing was written.
	Finish(ctx context.Context, scope, key, owner string, status int, contentType, location string, body []byte, bodyStored bool) (bool, error)
	// Abandon deletes the record (5xx, panic, empty response); false means
	// the caller no longer owns the lock and nothing was deleted.
	Abandon(ctx context.Context, scope, key, owner string) (bool, error)
}
