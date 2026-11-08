// Package syncdelta provides pure helpers for offline delta sync: an opaque
// cursor over (updatedAt, _id), a stable read horizon, tombstone-aware pages,
// the 409 conflict error and client-time validation.
//
// A module that serves a delta endpoint fetches limit+1 rows with
// Filter(cursor, now) sorted by Sort(), then calls BuildPage. The horizon
// (StableHorizon, 2 s) keeps the cursor behind in-flight writes whose
// updatedAt could still land before it. Soft-deleted rows (tombstones) are
// returned in Page.Deleted and must be kept at least DefaultTombstoneRetention;
// a cursor older than the retention yields ErrCursorExpired (410) and the
// client must do a full resync.
//
// Documents must store updatedAt as a BSON date and use a comparable _id
// (string) so the (updatedAt, _id) order is total.
//
// Error envelope: {"status","title","detail","code"} with the Code*
// constants below; CursorExpiredError, InvalidCursorError,
// ClientTimeOutOfRangeError and *ConflictError are huma.StatusError values a
// handler can return directly.
package syncdelta
