package qdrant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

const emptyQueryResult = `{"result":{"points":[]},"status":"ok"}`

// queryServer answers /points/query with queryBody and /points/query/batch
// with batchBody.
func queryServer(t *testing.T, queryBody, batchBody string) (*Driver, *recorder) {
	t.Helper()
	srv, rec := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collections/c/points/query":
			okJSON(w, queryBody)
		case "/collections/c/points/query/batch":
			okJSON(w, batchBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return mustNew(t, Config{URL: srv.URL}), rec
}

func baseQuery() engine.Query {
	return engine.Query{
		TenantID:   "T",
		Principals: []string{"tenant:*", "user:u1"},
		Dense:      []float32{1, 0, 0.5, 0.25},
		Keyword:    "orchid supply",
		Weights:    engine.Weights{Dense: 2, Keyword: 0.5},
		PerArm:     20,
		Limit:      10,
	}
}

// onlyRequest runs q and returns the single request it sent.
func onlyRequest(t *testing.T, q engine.Query) recorded {
	t.Helper()
	d, rec := queryServer(t, emptyQueryResult, "")
	if _, err := d.Query(t.Context(), "c", q); err != nil {
		t.Fatalf("Query: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/collections/c/points/query" {
		t.Fatalf("requests = %+v, want one POST /collections/c/points/query", reqs)
	}
	return reqs[0]
}

const scopeJSON = `{"must":[
	{"key":"_rt.tenantId","match":{"value":"T"}},
	{"key":"_rt.visibleTo","match":{"any":["tenant:*","user:u1"]}}]}`

const denseArmJSON = `{"query":[1,0,0.5,0.25],"using":"dense","filter":` + scopeJSON + `,"limit":20}`

const keywordArmJSON = `{"query":{"text":"orchid supply"},"using":"_rt.text","filter":` + scopeJSON + `,
	"params":{"idf":{"corpus":{"must":[{"key":"_rt.tenantId","match":{"value":"T"}}]}}},"limit":20}`

const hybridJSON = `{"prefetch":[` + denseArmJSON + `,` + keywordArmJSON + `],
	"query":{"rrf":{"weights":[2,0.5]}},
	"filter":` + scopeJSON + `,"limit":10,"with_payload":["_rt","f"]}`

func TestQuery_RefusesUnscoped(t *testing.T) {
	cases := map[string]func(q *engine.Query){
		`TenantID ""`:                   func(q *engine.Query) { q.TenantID = "" },
		"Principals nil":                func(q *engine.Query) { q.Principals = nil },
		`Principals [""]`:               func(q *engine.Query) { q.Principals = []string{""} },
		`Principals ["tenant:*", ""]`:   func(q *engine.Query) { q.Principals = []string{"tenant:*", ""} },
		"no arm":                        func(q *engine.Query) { q.Dense, q.Keyword = nil, "" },
		"Limit 0":                       func(q *engine.Query) { q.Limit = 0 },
		"Limit 501":                     func(q *engine.Query) { q.Limit, q.PerArm = 501, 501 },
		"PerArm 501":                    func(q *engine.Query) { q.PerArm = 501 },
		"dense weight 0":                func(q *engine.Query) { q.Weights.Dense = 0 },
		"keyword weight 0":              func(q *engine.Query) { q.Weights.Keyword = 0 },
		"keyword weight -1, dense only": func(q *engine.Query) { q.Weights.Keyword = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d, rec := queryServer(t, emptyQueryResult, "")
			q := baseQuery()
			mutate(&q)
			_, err := d.Query(t.Context(), "c", q)
			notSentinel(t, "Query", err)
			if n := len(rec.all()); n != 0 {
				t.Errorf("sent %d requests, want 0", n)
			}
		})
	}
	// An absent arm's weight is not checked: it is not used.
	q := baseQuery()
	q.Keyword, q.Weights.Keyword = "", 0
	onlyRequest(t, q)
}

func TestQuery_HybridBody(t *testing.T) {
	assertJSON(t, "hybrid", onlyRequest(t, baseQuery()).Body, hybridJSON)
}

func TestQuery_SingleArm(t *testing.T) {
	q := baseQuery()
	q.Dense = nil
	assertJSON(t, "keyword only", onlyRequest(t, q).Body, `{"prefetch":[`+keywordArmJSON+`],
		"query":{"rrf":{"weights":[0.5]}},"filter":`+scopeJSON+`,"limit":10,"with_payload":["_rt","f"]}`)

	q = baseQuery()
	q.Keyword = ""
	assertJSON(t, "dense only", onlyRequest(t, q).Body, `{"prefetch":[`+denseArmJSON+`],
		"query":{"rrf":{"weights":[2]}},"filter":`+scopeJSON+`,"limit":10,"with_payload":["_rt","f"]}`)
}

func TestQuery_PerArmAtLeastLimit(t *testing.T) {
	q := baseQuery()
	q.PerArm, q.Limit = 5, 10
	body := onlyRequest(t, q).Body
	for i, p := range body["prefetch"].([]any) {
		if got := p.(map[string]any)["limit"]; got != float64(10) {
			t.Errorf("prefetch %d limit = %v, want 10", i, got)
		}
	}
}

func TestQuery_UserFilterIsNamespaced(t *testing.T) {
	q := baseQuery()
	q.Filter = &engine.Filter{Must: []engine.Filter{{Field: "_rt.tenantId", Match: []any{"x"}}}}
	body := onlyRequest(t, q).Body
	user := map[string]any{"must": []any{map[string]any{"key": "f._rt.tenantId", "match": map[string]any{"value": "x"}}}}
	filters := []any{body["filter"]}
	for _, p := range body["prefetch"].([]any) {
		filters = append(filters, p.(map[string]any)["filter"])
	}
	for i, f := range filters {
		must := f.(map[string]any)["must"].([]any)
		if len(must) != 3 || !reflect.DeepEqual(must[2], user) {
			t.Errorf("filter %d must = %v, want the user filter %v third", i, must, user)
		}
	}
}

func TestRenderFilter(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		in   engine.Filter
		want string
	}{
		{"match one", engine.Filter{Field: "status", Match: []any{"open"}},
			`{"key":"f.status","match":{"value":"open"}}`},
		{"match many", engine.Filter{Field: "status", Match: []any{"open", "closed"}},
			`{"key":"f.status","match":{"any":["open","closed"]}}`},
		{"match bool", engine.Filter{Field: "active", Match: []any{true}},
			`{"key":"f.active","match":{"value":true}}`},
		{"range gte only", engine.Filter{Field: "score", Range: &engine.Range{Gte: f64(10)}},
			`{"key":"f.score","range":{"gte":10}}`},
		{"range all bounds", engine.Filter{Field: "score", Range: &engine.Range{Lt: f64(4), Lte: f64(3), Gt: f64(1), Gte: f64(2)}},
			`{"key":"f.score","range":{"lt":4,"lte":3,"gt":1,"gte":2}}`},
		{"exists", engine.Filter{Field: "ratio", Exists: true},
			`{"must_not":[{"is_empty":{"key":"f.ratio"}}]}`},
		{"should", engine.Filter{Should: []engine.Filter{
			{Field: "status", Match: []any{"open"}},
			{Field: "score", Range: &engine.Range{Gte: f64(50)}},
		}}, `{"should":[{"key":"f.status","match":{"value":"open"}},{"key":"f.score","range":{"gte":50}}]}`},
		{"must_not", engine.Filter{MustNot: []engine.Filter{{Field: "active", Match: []any{true}}}},
			`{"must_not":[{"key":"f.active","match":{"value":true}}]}`},
		{"nested composite", engine.Filter{
			Must: []engine.Filter{
				{Field: "status", Match: []any{"open"}},
				{Should: []engine.Filter{{Field: "tags", Match: []any{"vip"}}, {Field: "ratio", Exists: true}}},
			},
			MustNot: []engine.Filter{{Must: []engine.Filter{{Field: "score", Range: &engine.Range{Lt: f64(5)}}}}},
		}, `{"must":[
				{"key":"f.status","match":{"value":"open"}},
				{"should":[{"key":"f.tags","match":{"value":"vip"}},{"must_not":[{"is_empty":{"key":"f.ratio"}}]}]}],
			"must_not":[{"must":[{"key":"f.score","range":{"lt":5}}]}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rendered, err := renderFilter(&c.in)
			if err != nil {
				t.Fatalf("renderFilter = %v", err)
			}
			raw, err := json.Marshal(rendered)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			assertJSON(t, c.name, got, c.want)
		})
	}
}

const candidatePayload = `"payload":{
	"_rt":{"tenantId":"T","sourceId":"s1","sourceRevision":7,"chunkId":"s1#2","ordinal":2,
		"visibleTo":["tenant:*"],"subjects":["user:x"],"contentHash":"h","ingestionProfile":"p1",
		"charStart":3,"charEnd":9,"page":4,"text":"orchid supply"},
	"f":{"status":"open","score":42}}`

func TestQuery_DecodesCandidates(t *testing.T) {
	d, _ := queryServer(t, `{"result":{"points":[
		{"id":"11111111-1111-5111-8111-111111111111","version":3,"score":0.75,`+candidatePayload+`},
		{"id":"22222222-2222-5222-8222-222222222222","version":3,"score":0.5,"payload":{"_rt":{"tenantId":"T","sourceId":"s2","visibleTo":["user:u1"],"text":"x"},"f":{}}}
	]},"status":"ok"}`, "")
	got, err := d.Query(t.Context(), "c", baseQuery())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	want := []engine.Candidate{
		{
			PointID: "11111111-1111-5111-8111-111111111111",
			Runtime: engine.Runtime{
				TenantID: "T", SourceID: "s1", SourceRevision: 7, ChunkID: "s1#2", Ordinal: 2,
				VisibleTo: []string{"tenant:*"}, Subjects: []string{"user:x"}, ContentHash: "h",
				IngestionProfile: "p1", CharStart: 3, CharEnd: 9, Page: 4,
			},
			Text:   "orchid supply",
			Fields: map[string]any{"status": "open", "score": float64(42)},
			Score:  0.75,
		},
		{
			PointID: "22222222-2222-5222-8222-222222222222",
			Runtime: engine.Runtime{TenantID: "T", SourceID: "s2", VisibleTo: []string{"user:u1"}},
			Text:    "x",
			Fields:  map[string]any{},
			Score:   0.5,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n  got:  %+v\n  want: %+v", got, want)
	}
}

func TestQuery_Diagnostics(t *testing.T) {
	const (
		id1 = "11111111-1111-5111-8111-111111111111"
		id2 = "22222222-2222-5222-8222-222222222222"
	)
	d, rec := queryServer(t, "", `{"result":[
		{"points":[
			{"id":"`+id1+`","version":1,"score":0.9,`+candidatePayload+`},
			{"id":"`+id2+`","version":1,"score":0.4,`+candidatePayload+`}]},
		{"points":[{"id":"`+id2+`","version":1,"score":0.5},{"id":"`+id1+`","version":1,"score":0.98}]},
		{"points":[{"id":"`+id1+`","version":1,"score":3.25}]}
	],"status":"ok"}`)
	q := baseQuery()
	q.Diagnostics = true
	got, err := d.Query(t.Context(), "c", q)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/collections/c/points/query/batch" {
		t.Fatalf("requests = %+v, want one POST /collections/c/points/query/batch", reqs)
	}
	withoutPayload := func(arm string) string { return arm[:len(arm)-1] + `,"with_payload":false}` }
	assertJSON(t, "batch", reqs[0].Body, `{"searches":[`+hybridJSON+`,`+
		withoutPayload(denseArmJSON)+`,`+withoutPayload(keywordArmJSON)+`]}`)

	if len(got) != 2 || got[0].PointID != id1 || got[1].PointID != id2 {
		t.Fatalf("candidates = %+v, want %s then %s", got, id1, id2)
	}
	score := func(p *float32) any {
		if p == nil {
			return nil
		}
		return *p
	}
	checks := []struct {
		label     string
		got, want any
	}{
		{"c1 score", got[0].Score, float32(0.9)},
		{"c1 dense", score(got[0].DenseScore), float32(0.98)},
		{"c1 keyword", score(got[0].KeywordScore), float32(3.25)},
		{"c2 dense", score(got[1].DenseScore), float32(0.5)},
		{"c2 keyword", score(got[1].KeywordScore), nil},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.label, c.got, c.want)
		}
	}
}

func TestQuery_DiagnosticsSingleArm(t *testing.T) {
	const id1 = "11111111-1111-5111-8111-111111111111"
	d, rec := queryServer(t, "", `{"result":[
		{"points":[{"id":"`+id1+`","version":1,"score":0.5,`+candidatePayload+`}]},
		{"points":[{"id":"`+id1+`","version":1,"score":1.5}]}
	],"status":"ok"}`)
	q := baseQuery()
	q.Dense = nil
	q.Diagnostics = true
	got, err := d.Query(t.Context(), "c", q)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if n := len(rec.all()[0].Body["searches"].([]any)); n != 2 {
		t.Errorf("batch has %d searches, want 2 (fused + keyword arm)", n)
	}
	if len(got) != 1 || got[0].DenseScore != nil || got[0].KeywordScore == nil || *got[0].KeywordScore != 1.5 {
		t.Errorf("candidates = %+v, want one with KeywordScore 1.5 and DenseScore nil", got)
	}
}

func TestRenderFilter_RefusesMalformed(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	open := engine.Filter{Field: "status", Match: []any{"open"}}
	cases := map[string]engine.Filter{
		"field with children":         {Field: "status", Match: []any{"open"}, Must: []engine.Filter{open}},
		"field with only children":    {Field: "status", Should: []engine.Filter{open}},
		"match and range":             {Field: "score", Match: []any{int64(1)}, Range: &engine.Range{Gte: f64(1)}},
		"match and exists":            {Field: "score", Match: []any{int64(1)}, Exists: true},
		"range and exists":            {Field: "score", Range: &engine.Range{Gte: f64(1)}, Exists: true},
		"leaf without condition":      {Field: "ratio"},
		"empty match":                 {Field: "ratio", Match: []any{}},
		"range without bounds":        {Field: "score", Range: &engine.Range{}},
		"empty composite":             {},
		"nested empty composite":      {Must: []engine.Filter{open, {}}},
		"composite with stray match":  {Must: []engine.Filter{open}, Match: []any{"x"}},
		"composite with stray range":  {Must: []engine.Filter{open}, Range: &engine.Range{Gte: f64(1)}},
		"composite with stray exists": {Must: []engine.Filter{open}, Exists: true},
		"composite with empty range":  {Should: []engine.Filter{open}, Range: &engine.Range{}},
		"nested stray exists":         {Must: []engine.Filter{{MustNot: []engine.Filter{open}, Exists: true}}},
		"nested malformed leaf":       {Should: []engine.Filter{open, {MustNot: []engine.Filter{{Field: "x"}}}}},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := renderFilter(&f); err == nil {
				t.Errorf("renderFilter = %v, nil; want an error", got)
			}
		})
	}
}

func TestQuery_RefusesMalformedFilter(t *testing.T) {
	d, rec := queryServer(t, emptyQueryResult, "")
	q := baseQuery()
	q.Filter = &engine.Filter{Must: []engine.Filter{{Field: "status", Match: []any{"open"}}, {Field: "ratio"}}}
	_, err := d.Query(t.Context(), "c", q)
	notSentinel(t, "Query with a malformed filter", err)
	if n := len(rec.all()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}

func TestQuery_WhitespaceKeywordIsAbsent(t *testing.T) {
	q := baseQuery()
	q.Keyword = " \t\n "
	assertJSON(t, "dense with a blank keyword", onlyRequest(t, q).Body, `{"prefetch":[`+denseArmJSON+`],
		"query":{"rrf":{"weights":[2]}},"filter":`+scopeJSON+`,"limit":10,"with_payload":["_rt","f"]}`)

	d, rec := queryServer(t, emptyQueryResult, "")
	q.Dense = nil
	_, err := d.Query(t.Context(), "c", q)
	notSentinel(t, "Query with only a blank keyword", err)
	if n := len(rec.all()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}

// outOfScopeResult holds one in-scope point, one of another tenant and one
// whose visibleTo does not meet baseQuery's principals.
const outOfScopeResult = `[
	{"id":"11111111-1111-5111-8111-111111111111","version":1,"score":0.9,"payload":{"_rt":{"tenantId":"T","sourceId":"ok","visibleTo":["user:u1"],"text":"a"},"f":{}}},
	{"id":"22222222-2222-5222-8222-222222222222","version":1,"score":0.8,"payload":{"_rt":{"tenantId":"OTHER","sourceId":"foreign","visibleTo":["tenant:*"],"text":"b"},"f":{}}},
	{"id":"33333333-3333-5333-8333-333333333333","version":1,"score":0.7,"payload":{"_rt":{"tenantId":"T","sourceId":"hidden","visibleTo":["user:zz"],"text":"c"},"f":{}}}
]`

func TestQuery_DropsOutOfScopeCandidates(t *testing.T) {
	for _, diagnostics := range []bool{false, true} {
		t.Run(fmt.Sprintf("diagnostics %t", diagnostics), func(t *testing.T) {
			var logs bytes.Buffer
			srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/batch") {
					okJSON(w, `{"result":[{"points":`+outOfScopeResult+`},{"points":[]},{"points":[]}]}`)
					return
				}
				okJSON(w, `{"result":{"points":`+outOfScopeResult+`}}`)
			})
			d := mustNew(t, Config{URL: srv.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
			q := baseQuery()
			q.Diagnostics = diagnostics
			got, err := d.Query(t.Context(), "c", q)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(got) != 1 || got[0].Runtime.SourceID != "ok" {
				t.Fatalf("candidates = %+v, want only the in-scope one", got)
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != 1 {
				t.Fatalf("logged %d lines, want 1: %s", len(lines), logs.String())
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["level"] != "WARN" || entry["dropped"] != float64(2) || entry["returned"] != float64(3) {
				t.Errorf("log entry = %v, want WARN with dropped 2 of returned 3", entry)
			}
			for _, leak := range []string{"OTHER", "foreign", "hidden", "user:zz", "2222", "3333"} {
				if strings.Contains(lines[0], leak) {
					t.Errorf("log entry carries %q: %s", leak, lines[0])
				}
			}
		})
	}
}

func TestQuery_InScopeCandidatesLogNothing(t *testing.T) {
	var logs bytes.Buffer
	srv, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"result":{"points":[{"id":"11111111-1111-5111-8111-111111111111","score":1,`+candidatePayload+`}]}}`)
	})
	d := mustNew(t, Config{URL: srv.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	got, err := d.Query(t.Context(), "c", baseQuery())
	if err != nil || len(got) != 1 {
		t.Fatalf("Query = %d candidates, %v; want 1, nil", len(got), err)
	}
	if logs.Len() != 0 {
		t.Errorf("logged %s, want nothing", logs.String())
	}
}
