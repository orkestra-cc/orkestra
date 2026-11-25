package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestTEIClient_EmbedSendsInputsAndKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" || r.Method != http.MethodPost {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k1" {
			t.Errorf("Authorization = %q", got)
		}
		b, _ := io.ReadAll(r.Body)
		if want := `{"inputs":["a","b"],"normalize":true,"truncate":true}`; string(b) != want {
			t.Errorf("body = %s, want %s", b, want)
		}
		_, _ = w.Write([]byte(`[[1,2],[3,4]]`))
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, APIKey: "k1", Normalize: true, Truncate: true, HTTP: srv.Client()}
	got, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]float32{{1, 2}, {3, 4}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTEIClient_EmbedBatchesOf32InOrder(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Inputs []string `json:"inputs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		sizes = append(sizes, len(body.Inputs))
		mu.Unlock()
		out := make([][]float32, len(body.Inputs))
		for i, s := range body.Inputs {
			n, _ := strconv.Atoi(strings.TrimPrefix(s, "t"))
			out[i] = []float32{float32(n)}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	inputs := make([]string, 70)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("t%d", i)
	}
	c := &TEIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{32, 32, 6}; !reflect.DeepEqual(sizes, want) {
		t.Fatalf("batch sizes = %v, want %v", sizes, want)
	}
	if len(got) != 70 {
		t.Fatalf("len = %d", len(got))
	}
	for i, v := range got {
		if v[0] != float32(i) {
			t.Fatalf("vector %d = %v: order lost", i, v)
		}
	}
}

func TestTEIClient_EmbedRejectsCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[[1]]`))
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("want error on vector count mismatch")
	}
}

func TestTEIClient_RerankSortsByScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"index":0,"score":0.1},{"index":1,"score":0.9}]`))
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Rerank(context.Background(), "q", []string{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	want := []iface.RerankScore{{Index: 1, Score: 0.9}, {Index: 0, Score: 0.1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTEIClient_RerankBodyFieldsAndBatching(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Texts []string `json:"texts"`
		}
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		out := make([]map[string]any, len(req.Texts))
		for i := range req.Texts {
			out[i] = map[string]any{"index": i, "score": 0.5} // ties: index order decides
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, Truncate: true, HTTP: srv.Client()}
	texts := make([]string, 40)
	for i := range texts {
		texts[i] = "d"
	}
	got, err := c.Rerank(context.Background(), "q", texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["query"] != "q" || first["truncate"] != true || first["raw_scores"] != false || first["return_text"] != false {
		t.Fatalf("body = %s", bodies[0])
	}
	if _, ok := first["normalize"]; ok {
		t.Fatalf("rerank body must not carry normalize: %s", bodies[0])
	}
	if len(got) != 40 {
		t.Fatalf("len = %d", len(got))
	}
	for i, s := range got {
		if s.Index != i { // global indices, ties broken by ascending index
			t.Fatalf("got[%d].Index = %d", i, s.Index)
		}
	}
}

func TestTEIClient_Info(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" || r.Method != http.MethodGet {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"model_id":"BAAI/bge-m3","model_sha":"abc"}`))
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (TEIInfo{ModelID: "BAAI/bge-m3", ModelSHA: "abc"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestTEIClient_InfoNullShaIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model_id":"m","model_sha":null}`))
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Info(context.Background())
	if err != nil || got.ModelSHA != "" {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestTEIClient_ErrorStatusHidesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	c := &TEIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}
