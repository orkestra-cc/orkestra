package syncdelta

// Page is the delta response body.
type Page[T any] struct {
	Items   []T      `json:"items"`
	Deleted []string `json:"deleted"`
	Cursor  string   `json:"cursor"`
	HasMore bool     `json:"hasMore"`
}

// Entry is a row of a delta query.
type Entry interface {
	SyncCursor() Cursor
	IsDeleted() bool
	SyncID() string
}

// BuildPage turns rows fetched with limit+1 into a page: at most limit rows
// are consumed, tombstones go to Deleted (ids), the cursor is the last
// consumed row's, or prev when there are none. HasMore is true when an extra
// row was fetched.
func BuildPage[T Entry](rows []T, limit int, prev *Cursor) Page[T] {
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
	switch {
	case len(rows) > 0:
		p.Cursor = EncodeCursor(rows[len(rows)-1].SyncCursor())
	case prev != nil:
		p.Cursor = EncodeCursor(*prev)
	}
	return p
}
