package qdrant

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// collectionServer answers the collection endpoints: exists reports whether
// the collection is there, size is its dense dimension, createStatus the
// answer to the create call.
func collectionServer(t *testing.T, exists bool, size, createStatus int) (*Driver, *recorder) {
	t.Helper()
	srv, rec := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/exists"):
			if exists {
				okJSON(w, `{"result":{"exists":true}}`)
			} else {
				okJSON(w, `{"result":{"exists":false}}`)
			}
		case r.Method == http.MethodGet:
			okJSON(w, `{"result":{"config":{"params":{"vectors":{"dense":{"size":`+strconv.Itoa(size)+`,"distance":"Cosine"}}}}}}`)
		case r.Method == http.MethodPut && !strings.Contains(r.URL.Path, "/index"):
			if createStatus != 200 {
				w.WriteHeader(createStatus)
				return
			}
			okJSON(w, `{"result":true}`)
		default:
			okJSON(w, `{"result":{"status":"acknowledged"}}`)
		}
	})
	return mustNew(t, Config{URL: srv.URL}), rec
}

type indexReq struct {
	field  string
	schema any
}

func indexRequests(reqs []recorded) []indexReq {
	var out []indexReq
	for _, r := range reqs {
		if r.Method == http.MethodPut && strings.Contains(r.Path, "/index") {
			out = append(out, indexReq{field: r.Body["field_name"].(string), schema: r.Body["field_schema"]})
		}
	}
	return out
}

func TestEnsureCollection_CreatesWithIndexes(t *testing.T) {
	d, rec := collectionServer(t, false, 4, 200)
	spec := engine.CollectionSpec{
		Name:       "c_one",
		Dimensions: 4,
		Fields: []iface.RetrievalFilterField{
			{Name: "status", Type: iface.RetrievalFieldKeyword},
			{Name: "tags", Type: iface.RetrievalFieldKeywords},
			{Name: "score", Type: iface.RetrievalFieldInteger},
			{Name: "ratio", Type: iface.RetrievalFieldFloat},
			{Name: "active", Type: iface.RetrievalFieldBool},
			{Name: "created", Type: iface.RetrievalFieldDatetime},
		},
	}
	if err := d.EnsureCollection(t.Context(), spec); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	reqs := rec.all()
	if len(reqs) < 2 || reqs[0].Method != "GET" || reqs[0].Path != "/collections/c_one/exists" {
		t.Fatalf("first request = %+v", reqs[0])
	}
	if reqs[1].Method != "PUT" || reqs[1].Path != "/collections/c_one" {
		t.Fatalf("second request = %s %s", reqs[1].Method, reqs[1].Path)
	}
	wantCreate := map[string]any{
		"vectors":      map[string]any{"dense": map[string]any{"size": float64(4), "distance": "Cosine"}},
		"shard_number": float64(1),
	}
	if !reflect.DeepEqual(reqs[1].Body, wantCreate) {
		t.Errorf("create body = %v, want %v", reqs[1].Body, wantCreate)
	}
	want := []indexReq{
		{"_rt.tenantId", map[string]any{"type": "keyword", "is_tenant": true}},
		{"_rt.sourceId", "keyword"},
		{"_rt.visibleTo", "keyword"},
		{"_rt.subjects", "keyword"},
		{"_rt.sourceRevision", "integer"},
		{"_rt.text", map[string]any{"type": "text", "tokenizer": "word", "lowercase": true, "scoring": true}},
		{"f.status", "keyword"},
		{"f.tags", "keyword"},
		{"f.score", "integer"},
		{"f.ratio", "float"},
		{"f.active", "bool"},
		{"f.created", "integer"},
	}
	if got := indexRequests(reqs); !reflect.DeepEqual(got, want) {
		t.Errorf("index requests =\n%v\nwant\n%v", got, want)
	}
	for _, r := range reqs {
		if strings.Contains(r.Path, "/index") && !strings.HasSuffix(r.Path, "/index?wait=true") {
			t.Errorf("index path %q lacks wait=true", r.Path)
		}
	}
}

func TestEnsureCollection_AnalyzerParams(t *testing.T) {
	d, rec := collectionServer(t, false, 4, 200)
	spec := engine.CollectionSpec{
		Name: "c_two", Dimensions: 4,
		Analyzer: iface.RetrievalAnalyzer{Tokenizer: "multilingual", Language: "italian", Stemming: true, StopWords: true},
	}
	if err := d.EnsureCollection(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"type": "text", "tokenizer": "multilingual", "lowercase": true, "scoring": true,
		"stemmer":   map[string]any{"type": "snowball", "language": "italian"},
		"stopwords": "italian",
	}
	for _, ir := range indexRequests(rec.all()) {
		if ir.field == "_rt.text" {
			if !reflect.DeepEqual(ir.schema, want) {
				t.Fatalf("_rt.text schema = %v, want %v", ir.schema, want)
			}
			return
		}
	}
	t.Fatal("no _rt.text index request")
}

func TestEnsureCollection_DimensionMismatch(t *testing.T) {
	d, rec := collectionServer(t, true, 8, 200)
	err := d.EnsureCollection(t.Context(), engine.CollectionSpec{Name: "c_three", Dimensions: 4})
	if err == nil || !strings.Contains(err.Error(), "8 dimensions, want 4") {
		t.Fatalf("EnsureCollection = %v, want a dimension mismatch", err)
	}
	for _, r := range rec.all() {
		if r.Method == http.MethodPut {
			t.Errorf("unexpected %s %s", r.Method, r.Path)
		}
	}
}

func TestEnsureCollection_ConflictMeansExists(t *testing.T) {
	d, rec := collectionServer(t, false, 4, http.StatusConflict)
	if err := d.EnsureCollection(t.Context(), engine.CollectionSpec{Name: "c_four", Dimensions: 4}); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	var info bool
	for _, r := range rec.all() {
		if r.Method == http.MethodGet && r.Path == "/collections/c_four" {
			info = true
		}
	}
	if !info {
		t.Error("collection info was not read after the 409")
	}
	if len(indexRequests(rec.all())) == 0 {
		t.Error("no index requests after the 409")
	}
}

func TestEnsureCollection_RejectsInvalidSpec(t *testing.T) {
	d, rec := collectionServer(t, false, 4, 200)
	for _, spec := range []engine.CollectionSpec{
		{Name: "", Dimensions: 4},
		{Name: "c", Dimensions: 0},
		{Name: "c", Dimensions: 65537},
	} {
		if err := d.EnsureCollection(t.Context(), spec); err == nil {
			t.Errorf("EnsureCollection(%+v) = nil", spec)
		}
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("invalid specs reached the server (%d requests)", n)
	}
}
