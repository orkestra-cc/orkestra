package qdrant

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

// ackServer answers every request with an acknowledged update, or with
// countBody on the count endpoint.
func ackServer(t *testing.T, countBody string) (*Driver, *recorder) {
	t.Helper()
	srv, rec := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collections/c/points/count" {
			okJSON(w, countBody)
			return
		}
		okJSON(w, `{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`)
	})
	return mustNew(t, Config{URL: srv.URL}), rec
}

// jsonMap decodes a JSON object literal the way the recorder decodes bodies.
func jsonMap(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad expected JSON %s: %v", s, err)
	}
	return m
}

func assertJSON(t *testing.T, label string, got map[string]any, want string) {
	t.Helper()
	w := jsonMap(t, want)
	if !reflect.DeepEqual(got, w) {
		g, _ := json.Marshal(got)
		e, _ := json.Marshal(w)
		t.Errorf("%s body\n  got:  %s\n  want: %s", label, g, e)
	}
}

// notSentinel fails unless err is a refusal: non-nil and neither engine
// sentinel.
func notSentinel(t *testing.T, label string, err error) {
	t.Helper()
	switch {
	case err == nil:
		t.Errorf("%s = nil, want an error", label)
	case errors.Is(err, engine.ErrUnavailable), errors.Is(err, engine.ErrNotFound):
		t.Errorf("%s = %v, want a refusal, not an engine sentinel", label, err)
	}
}

func testPoint(source string, rev int64, ordinal int) engine.Point {
	return engine.Point{
		ID:    engine.PointID("t1", "c", source, ordinal),
		Dense: []float32{1, 0, 0.5, 0.25},
		Text:  "text of " + source,
		Runtime: engine.Runtime{
			TenantID:         "t1",
			SourceID:         source,
			SourceRevision:   rev,
			ChunkID:          fmt.Sprintf("%s#%d", source, ordinal),
			Ordinal:          ordinal,
			VisibleTo:        []string{"tenant:*"},
			Subjects:         []string{"user:x"},
			ContentHash:      "h",
			IngestionProfile: "p1",
			CharStart:        0,
			CharEnd:          10,
		},
	}
}

func pointsOf(r recorded) []any {
	pts, _ := r.Body["points"].([]any)
	return pts
}

func TestUpsert_EmptyIsNoop(t *testing.T) {
	d, rec := ackServer(t, "")
	if err := d.Upsert(t.Context(), "c", nil); err != nil {
		t.Fatalf("Upsert(nil) = %v", err)
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("Upsert(nil) sent %d requests, want 0", n)
	}
}

func TestUpsert_ConditionalOnRevisionPerGroup(t *testing.T) {
	d, rec := ackServer(t, "")
	pts := []engine.Point{testPoint("s1", 2, 0), testPoint("s2", 3, 0), testPoint("s1", 2, 1)}
	if err := d.Upsert(t.Context(), "c", pts); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	for i, wantRev := range []float64{2, 3} {
		r := reqs[i]
		if r.Method != http.MethodPut || r.Path != "/collections/c/points?wait=true" {
			t.Errorf("request %d = %s %s, want PUT /collections/c/points?wait=true", i, r.Method, r.Path)
		}
		uf, _ := r.Body["update_filter"].(map[string]any)
		must, _ := uf["must"].([]any)
		want := map[string]any{"key": "_rt.sourceRevision", "range": map[string]any{"lte": wantRev}}
		if len(must) != 1 || !reflect.DeepEqual(must[0], want) {
			t.Errorf("request %d update_filter = %v, want must [%v]", i, uf, want)
		}
	}
	if n := len(pointsOf(reqs[0])); n != 2 {
		t.Errorf("first request has %d points, want the 2 of revision 2", n)
	}
	if n := len(pointsOf(reqs[1])); n != 1 {
		t.Errorf("second request has %d points, want 1", n)
	}
}

func TestUpsert_PayloadShape(t *testing.T) {
	d, rec := ackServer(t, "")
	p := testPoint("s1", 1, 0)
	p.Runtime.Page = 3
	p.Fields = map[string]any{"status": "open"}
	q := testPoint("s2", 1, 0) // page 0, no fields
	if err := d.Upsert(t.Context(), "c", []engine.Point{p, q}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	pts := pointsOf(reqs[0])
	if len(pts) != 2 {
		t.Fatalf("got %d points, want 2", len(pts))
	}
	base := []string{"tenantId", "sourceId", "sourceRevision", "chunkId", "ordinal", "visibleTo", "subjects",
		"contentHash", "ingestionProfile", "charStart", "charEnd", "text"}
	for i, c := range []struct {
		p        engine.Point
		wantKeys []string
		wantF    map[string]any
	}{
		{p, append(append([]string{}, base...), "page"), map[string]any{"status": "open"}},
		{q, base, map[string]any{}},
	} {
		got := pts[i].(map[string]any)
		if got["id"] != c.p.ID {
			t.Errorf("point %d id = %v, want %s", i, got["id"], c.p.ID)
		}
		vector, _ := got["vector"].(map[string]any)
		if dense, _ := vector["dense"].([]any); len(dense) != 4 {
			t.Errorf("point %d vector = %v, want dense of length 4", i, got["vector"])
		}
		payload, _ := got["payload"].(map[string]any)
		if len(payload) != 2 {
			t.Errorf("point %d payload has keys %v, want only _rt and f", i, payload)
		}
		rt, _ := payload["_rt"].(map[string]any)
		keys := make([]string, 0, len(rt))
		for k := range rt {
			keys = append(keys, k)
		}
		want := append([]string{}, c.wantKeys...)
		sort.Strings(keys)
		sort.Strings(want)
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("point %d _rt keys = %v, want %v", i, keys, want)
		}
		if rt["text"] != c.p.Text {
			t.Errorf("point %d _rt.text = %v, want %q", i, rt["text"], c.p.Text)
		}
		if !reflect.DeepEqual(payload["f"], c.wantF) {
			t.Errorf("point %d f = %#v, want %#v", i, payload["f"], c.wantF)
		}
	}
}

func TestUpsert_RefusesInvalidPointsBeforeAnyRequest(t *testing.T) {
	cases := map[string]func(p *engine.Point){
		"Dense nil":        func(p *engine.Point) { p.Dense = nil },
		"ID empty":         func(p *engine.Point) { p.ID = "" },
		"TenantID empty":   func(p *engine.Point) { p.Runtime.TenantID = "" },
		"SourceID empty":   func(p *engine.Point) { p.Runtime.SourceID = "" },
		"Dense zero-sized": func(p *engine.Point) { p.Dense = []float32{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d, rec := ackServer(t, "")
			bad := testPoint("s2", 1, 0)
			mutate(&bad)
			// The invalid point comes after a valid one of another revision:
			// nothing may be written.
			notSentinel(t, "Upsert", d.Upsert(t.Context(), "c", []engine.Point{testPoint("s1", 1, 0), bad}))
			if n := len(rec.all()); n != 0 {
				t.Errorf("sent %d requests, want 0", n)
			}
		})
	}
}

func TestUpsert_SplitsAt256(t *testing.T) {
	d, rec := ackServer(t, "")
	pts := make([]engine.Point, 600)
	for i := range pts {
		pts[i] = testPoint("s1", 1, i)
	}
	if err := d.Upsert(t.Context(), "c", pts); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	var sizes []int
	for _, r := range rec.all() {
		sizes = append(sizes, len(pointsOf(r)))
	}
	if !reflect.DeepEqual(sizes, []int{256, 256, 88}) {
		t.Errorf("request sizes = %v, want [256 256 88]", sizes)
	}
}

func TestDeleteOtherRevisions_Body(t *testing.T) {
	d, rec := ackServer(t, "")
	if err := d.DeleteOtherRevisions(t.Context(), "c", "t1", "s1", 4); err != nil {
		t.Fatalf("DeleteOtherRevisions: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/collections/c/points/delete?wait=true" {
		t.Fatalf("requests = %+v, want one POST /collections/c/points/delete?wait=true", reqs)
	}
	assertJSON(t, "delete", reqs[0].Body, `{"filter":{
		"must":[{"key":"_rt.tenantId","match":{"value":"t1"}},{"key":"_rt.sourceId","match":{"value":"s1"}}],
		"must_not":[{"key":"_rt.sourceRevision","match":{"value":4}}]}}`)
}

func TestDeleteOtherRevisions_RefusesEmptyKeys(t *testing.T) {
	d, rec := ackServer(t, "")
	notSentinel(t, "DeleteOtherRevisions(tenant \"\")", d.DeleteOtherRevisions(t.Context(), "c", "", "s1", 1))
	notSentinel(t, "DeleteOtherRevisions(source \"\")", d.DeleteOtherRevisions(t.Context(), "c", "t1", "", 1))
	if n := len(rec.all()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}

func TestSelector_RefusedWhenZero(t *testing.T) {
	d, rec := ackServer(t, `{"result":{"count":1}}`)
	notSentinel(t, "DeleteByFilter(Selector{})", d.DeleteByFilter(t.Context(), "c", engine.Selector{}))
	_, err := d.Count(t.Context(), "c", engine.Selector{})
	notSentinel(t, "Count(Selector{})", err)
	if n := len(rec.all()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}

const selectorJSON = `{"must":[
	{"key":"_rt.tenantId","match":{"value":"A"}},
	{"key":"_rt.sourceId","match":{"any":["s1","s2"]}},
	{"key":"_rt.subjects","match":{"value":"user:x"}}]}`

func TestCount_Body(t *testing.T) {
	d, rec := ackServer(t, `{"result":{"count":7},"status":"ok"}`)
	n, err := d.Count(t.Context(), "c", engine.Selector{TenantID: "A", SourceIDs: []string{"s1", "s2"}, Subject: "user:x"})
	if err != nil || n != 7 {
		t.Fatalf("Count = %d, %v; want 7, nil", n, err)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/collections/c/points/count" {
		t.Fatalf("requests = %+v, want one POST /collections/c/points/count", reqs)
	}
	assertJSON(t, "count", reqs[0].Body, `{"filter":`+selectorJSON+`,"exact":true}`)
}

func TestCount_OnlySetSelectorFields(t *testing.T) {
	d, rec := ackServer(t, `{"result":{"count":0}}`)
	if _, err := d.Count(t.Context(), "c", engine.Selector{Subject: "user:x"}); err != nil {
		t.Fatal(err)
	}
	assertJSON(t, "count", rec.all()[0].Body,
		`{"filter":{"must":[{"key":"_rt.subjects","match":{"value":"user:x"}}]},"exact":true}`)
}

func TestDeleteByFilter_Body(t *testing.T) {
	d, rec := ackServer(t, "")
	if err := d.DeleteByFilter(t.Context(), "c", engine.Selector{TenantID: "A", SourceIDs: []string{"s1", "s2"}, Subject: "user:x"}); err != nil {
		t.Fatalf("DeleteByFilter: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/collections/c/points/delete?wait=true" {
		t.Fatalf("requests = %+v, want one POST /collections/c/points/delete?wait=true", reqs)
	}
	assertJSON(t, "delete", reqs[0].Body, `{"filter":`+selectorJSON+`}`)
}

func TestSelector_RefusesEmptySourceIDs(t *testing.T) {
	cases := map[string][]string{
		"empty list":          {},
		"empty id":            {""},
		"empty id among many": {"s1", ""},
	}
	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			d, rec := ackServer(t, `{"result":{"count":1}}`)
			sel := engine.Selector{TenantID: "A", SourceIDs: ids}
			notSentinel(t, "DeleteByFilter", d.DeleteByFilter(t.Context(), "c", sel))
			_, err := d.Count(t.Context(), "c", sel)
			notSentinel(t, "Count", err)
			if n := len(rec.all()); n != 0 {
				t.Errorf("sent %d requests, want 0", n)
			}
		})
	}
}
