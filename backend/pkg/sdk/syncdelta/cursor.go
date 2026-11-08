package syncdelta

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	// StableHorizon is how far behind now reads stop, so that writes still in
	// flight cannot appear behind an already-issued cursor.
	StableHorizon = 2 * time.Second
	// DefaultTombstoneRetention is how long soft-deleted rows are kept.
	DefaultTombstoneRetention = 30 * 24 * time.Hour
	// DefaultLimit and MaxLimit bound the page size.
	DefaultLimit = 200
	MaxLimit     = 500
)

// ErrInvalidCursor is returned by DecodeCursor for malformed cursors.
var ErrInvalidCursor = errors.New("syncdelta: invalid cursor")

// ErrCursorExpired is returned when a cursor predates the tombstone retention.
var ErrCursorExpired = errors.New("syncdelta: cursor expired")

// Cursor is the position of the last row handed to the client.
type Cursor struct {
	UpdatedAt time.Time
	// ID is the value of the string tie-breaker field (see Filter).
	ID string
	// IssuedAt is when the cursor was handed out (the read horizon of that
	// response). CheckExpired uses it, so a quiet collection does not expire
	// a client that synced recently. Zero means unknown.
	IssuedAt time.Time
}

type wireCursor struct {
	T  string `json:"t"`
	ID string `json:"id"`
	I  string `json:"i,omitempty"`
}

// EncodeCursor returns the opaque base64url (no padding) form of c.
func EncodeCursor(c Cursor) string {
	w := wireCursor{T: c.UpdatedAt.UTC().Format(time.RFC3339Nano), ID: c.ID}
	if !c.IssuedAt.IsZero() {
		w.I = c.IssuedAt.UTC().Format(time.RFC3339Nano)
	}
	b, _ := json.Marshal(w)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses a cursor. The empty string means "from the start" and
// returns nil, nil; anything malformed returns ErrInvalidCursor.
func DecodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var w wireCursor
	if err := json.Unmarshal(raw, &w); err != nil || w.ID == "" {
		return nil, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, w.T)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	c := &Cursor{UpdatedAt: t.UTC(), ID: w.ID}
	if w.I != "" {
		it, err := time.Parse(time.RFC3339Nano, w.I)
		if err != nil {
			return nil, ErrInvalidCursor
		}
		c.IssuedAt = it.UTC()
	}
	return c, nil
}

// Horizon is the newest updatedAt a read may return at time now.
func Horizon(now time.Time) time.Time { return now.Add(-StableHorizon) }

// Filter builds the Mongo filter for the rows after cur and not newer than
// the horizon. Combine it with the module's own tenant scope. idField is the
// string field that breaks ties between equal updatedAt values: "_id" for
// string-keyed collections, the string "uuid" field for ObjectID-keyed ones.
func Filter(cur *Cursor, now time.Time, idField string) bson.M {
	h := bson.M{"updatedAt": bson.M{"$lte": Horizon(now)}}
	if cur == nil {
		return h
	}
	return bson.M{"$and": bson.A{
		h,
		bson.M{"$or": bson.A{
			bson.M{"updatedAt": bson.M{"$gt": cur.UpdatedAt}},
			bson.M{"updatedAt": cur.UpdatedAt, idField: bson.M{"$gt": cur.ID}},
		}},
	}}
}

// Sort is the order Filter pages in; idField must match Filter's.
func Sort(idField string) bson.D {
	return bson.D{{Key: "updatedAt", Value: 1}, {Key: idField, Value: 1}}
}

// ClampLimit applies the default and the maximum page size.
func ClampLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultLimit
	case n > MaxLimit:
		return MaxLimit
	}
	return n
}

// CheckExpired returns ErrCursorExpired when cur was issued longer ago than
// retention (DefaultTombstoneRetention when retention <= 0): tombstones the
// client needs may already be purged. A cursor without IssuedAt falls back
// to its UpdatedAt.
func CheckExpired(cur *Cursor, now time.Time, retention time.Duration) error {
	if cur == nil {
		return nil
	}
	if retention <= 0 {
		retention = DefaultTombstoneRetention
	}
	ref := cur.IssuedAt
	if ref.IsZero() {
		ref = cur.UpdatedAt
	}
	if ref.Before(now.Add(-retention)) {
		return ErrCursorExpired
	}
	return nil
}
