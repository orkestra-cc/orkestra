// Package bench is the retrieval deployment benchmark: a JSONL fixture
// format, ranking metrics and (in later files) the runner that drives an
// inference endpoint over a fixture. It has no database or HTTP route access.
package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// FixtureItem is one searchable document. VisibleTo lists the principal
// strings allowed to see it; a principal set sees the item when the two
// lists intersect.
type FixtureItem struct {
	ID        string   `json:"id"`
	Lang      string   `json:"lang"`
	Title     string   `json:"title"`
	Text      string   `json:"text"`
	VisibleTo []string `json:"visibleTo"`
}

// FixtureQuery is one judged query. Expected holds relevant items visible to
// Principals; Hidden holds relevant items that are not visible to them and
// must therefore never be returned.
type FixtureQuery struct {
	ID         string   `json:"id"`
	Lang       string   `json:"lang"`
	Text       string   `json:"text"`
	Principals []string `json:"principals"`
	Expected   []string `json:"expected"`
	Hidden     []string `json:"hidden"`
}

// Fixture is a parsed benchmark fixture.
type Fixture struct {
	Items   []FixtureItem
	Queries []FixtureQuery
}

// maxLine bounds one JSONL line; item texts are short professional profiles.
const maxLine = 1 << 20

type queryLine struct {
	q    FixtureQuery
	line int
}

// LoadFixture reads a JSONL fixture: one JSON object per line, "kind"
// ("item" or "query") selecting the shape. Blank lines are skipped. Every
// error names the offending line.
func LoadFixture(r io.Reader) (*Fixture, error) {
	f := &Fixture{}
	items := map[string]int{}
	ids := map[string]struct{}{}
	var queries []queryLine

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	n := 0
	for sc.Scan() {
		n++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(raw), &head); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		switch head.Kind {
		case "item":
			var it FixtureItem
			if err := json.Unmarshal([]byte(raw), &it); err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			if err := checkNew(ids, it.ID, n); err != nil {
				return nil, err
			}
			if strings.TrimSpace(it.Text) == "" {
				return nil, fmt.Errorf("line %d: item %q has empty text", n, it.ID)
			}
			f.Items = append(f.Items, it)
			items[it.ID] = len(f.Items) - 1
		case "query":
			var q FixtureQuery
			if err := json.Unmarshal([]byte(raw), &q); err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			if err := checkNew(ids, q.ID, n); err != nil {
				return nil, err
			}
			if strings.TrimSpace(q.Text) == "" {
				return nil, fmt.Errorf("line %d: query %q has empty text", n, q.ID)
			}
			if len(q.Principals) == 0 {
				return nil, fmt.Errorf("line %d: query %q has no principals", n, q.ID)
			}
			queries = append(queries, queryLine{q: q, line: n})
		default:
			return nil, fmt.Errorf("line %d: unknown kind %q", n, head.Kind)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", n+1, err)
	}

	// Cross-references are checked last: a query may precede its items.
	for _, ql := range queries {
		q := ql.q
		for _, id := range q.Expected {
			idx, ok := items[id]
			if !ok {
				return nil, fmt.Errorf("line %d: query %q expects unknown item %q", ql.line, q.ID, id)
			}
			if !visible(&f.Items[idx], q.Principals) {
				return nil, fmt.Errorf("line %d: query %q expects item %q that is not visible to its principals", ql.line, q.ID, id)
			}
		}
		for _, id := range q.Hidden {
			idx, ok := items[id]
			if !ok {
				return nil, fmt.Errorf("line %d: query %q hides unknown item %q", ql.line, q.ID, id)
			}
			if visible(&f.Items[idx], q.Principals) {
				return nil, fmt.Errorf("line %d: query %q lists hidden item %q that is visible to its principals", ql.line, q.ID, id)
			}
		}
		f.Queries = append(f.Queries, q)
	}
	return f, nil
}

// VisibleTo reports whether the item is visible to the principals
// (visibleTo intersects principals). An unknown item is not visible.
func (f *Fixture) VisibleTo(itemID string, principals []string) bool {
	for i := range f.Items {
		if f.Items[i].ID == itemID {
			return visible(&f.Items[i], principals)
		}
	}
	return false
}

func visible(it *FixtureItem, principals []string) bool {
	return slices.ContainsFunc(it.VisibleTo, func(v string) bool {
		return slices.Contains(principals, v)
	})
}

func checkNew(ids map[string]struct{}, id string, line int) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("line %d: empty id", line)
	}
	if _, dup := ids[id]; dup {
		return fmt.Errorf("line %d: duplicate id %q", line, id)
	}
	ids[id] = struct{}{}
	return nil
}
