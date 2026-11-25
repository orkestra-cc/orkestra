package syncdelta

import "time"

// Page is the delta response body.
type Page[T any] struct {
	Items   []T      `json:"items"`
	Deleted []string `json:"deleted"`
	Cursor  string   `json:"cursor"`
	HasMore bool     `json:"hasMore"`
}

// Entry is a row of a delta query. SyncCursor().ID and SyncID() are the
// value of the tie-breaker field passed to Filter and Sort.
type Entry interface {
	SyncCursor() Cursor
	IsDeleted() bool
	SyncID() string
}

// BuildPage turns rows fetched with limit+1 into a page: at most limit rows
// are consumed, tombstones go to Deleted (ids), the cursor is the last
// consumed row's, or prev's position when there are none. The returned
// cursor is stamped IssuedAt = Horizon(now), also when the page is empty, so
// a quiet collection keeps renewing the client's cursor. HasMore is true when an extra
// row was fetched.
func BuildPage[T Entry](rows []T, limit int, prev *Cursor, now time.Time) Page[T] {
	p := Page[T]{Items: []T{}, Deleted: []string{}}
	if limit < 0 {
		limit = 0
	}
	if len(rows) > limit {
		p.HasMore = true
		rows = rows[:limit]
	}
	for _, r := range rows {
		if r.IsDeleted() {
			p.Deleted = append(p.Deleted, r.SyncID())
		} else {
			p.Items = append(p.Items, r)
		}
	}
	var pos *Cursor
	switch {
	case len(rows) > 0:
		c := rows[len(rows)-1].SyncCursor()
		pos = &c
	case prev != nil:
		c := *prev
		pos = &c
	}
	if pos != nil {
		pos.IssuedAt = Horizon(now)
		p.Cursor = EncodeCursor(*pos)
	}
	return p
}
