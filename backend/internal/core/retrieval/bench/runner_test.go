package bench

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
)

const testDims = 16

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	})
}

// hashVec is a deterministic bag-of-tokens vector, L2 normalised.
func hashVec(s string) []float32 {
	v := make([]float32, testDims)
	for _, t := range tokens(s) {
		h := uint32(2166136261)
		for _, c := range []byte(t) {
			h = (h ^ uint32(c)) * 16777619
		}
		v[h%testDims]++
	}
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	if n > 0 {
		for i := range v {
			v[i] /= float32(math.Sqrt(n))
		}
	}
	return v
}

// fakeEngine wraps the shared enginefake for storage and the driver's
// refusals and adds ranking, call recording, a blocking mode and an
// "ignore principals" mode.
type fakeEngine struct {
	*enginefake.Engine
	mu               sync.Mutex
	ensured          []engine.CollectionSpec
	dropped          []string
	queries          []engine.Query
	block            bool
	ignorePrincipals bool
}

func newFake() *fakeEngine { return &fakeEngine{Engine: enginefake.New()} }

func (f *fakeEngine) EnsureCollection(ctx context.Context, spec engine.CollectionSpec) error {
	f.mu.Lock()
	f.ensured = append(f.ensured, spec)
	f.mu.Unlock()
	return f.Engine.EnsureCollection(ctx, spec)
}

func (f *fakeEngine) DropCollection(ctx context.Context, name string) error {
	f.mu.Lock()
	f.dropped = append(f.dropped, name)
	f.mu.Unlock()
	return f.Engine.DropCollection(ctx, name)
}

func (f *fakeEngine) Query(ctx context.Context, name string, q engine.Query) ([]engine.Candidate, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	block, ignore := f.block, f.ignorePrincipals
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	base := q
	base.Limit = 500 // rank before cutting; the base applies tenant, principal and bounds refusals
	if ignore {
		base.Principals = nil
		for _, p := range f.Points(name) {
			base.Principals = append(base.Principals, p.Runtime.VisibleTo...)
		}
	}
	all, err := f.Engine.Query(ctx, name, base)
	if err != nil {
		return nil, err
	}
	pts := map[string]engine.Point{}
	for _, p := range f.Points(name) {
		pts[p.ID] = p
	}
	rankArm := func(score func(engine.Point) float64) []string {
		type sc struct {
			id string
			s  float64
		}
		var arm []sc
		for _, c := range all {
			if s := score(pts[c.PointID]); s > 0 {
				arm = append(arm, sc{c.PointID, s})
			}
		}
		sort.SliceStable(arm, func(i, j int) bool { return arm[i].s > arm[j].s })
		ids := make([]string, 0, q.PerArm)
		for _, a := range arm[:min(len(arm), q.PerArm)] {
			ids = append(ids, a.id)
		}
		return ids
	}
	fused := map[string]float64{}
	if strings.TrimSpace(q.Keyword) != "" {
		qt := tokens(q.Keyword)
		for r, id := range rankArm(func(p engine.Point) float64 {
			n := 0
			for _, t := range tokens(p.Text) {
				if slices.Contains(qt, t) {
					n++
				}
			}
			return float64(n)
		}) {
			fused[id] += q.Weights.Keyword / float64(60+r+1)
		}
	}
	if len(q.Dense) > 0 {
		for r, id := range rankArm(func(p engine.Point) float64 {
			var d float64
			for i := range q.Dense {
				d += float64(q.Dense[i] * p.Dense[i])
			}
			return d
		}) {
			fused[id] += q.Weights.Dense / float64(60+r+1)
		}
	}
	var out []engine.Candidate
	for id, s := range fused {
		p := pts[id]
		out = append(out, engine.Candidate{PointID: id, Runtime: p.Runtime, Text: p.Text, Score: float32(s)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].PointID < out[j].PointID
	})
	return out[:min(len(out), q.Limit)], nil
}

type teiOpts struct {
	rerankStatus int
	rerankByIdx  bool // score = index: the reverse of the candidate order
	embedBodies  *[][]string
	mu           *sync.Mutex
}

func newTEI(t *testing.T, id, sha string, o teiOpts) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"model_id": id, "model_sha": sha})
		case "/embed":
			var in struct{ Inputs []string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if o.embedBodies != nil {
				o.mu.Lock()
				*o.embedBodies = append(*o.embedBodies, in.Inputs)
				o.mu.Unlock()
			}
			out := make([][]float32, len(in.Inputs))
			for i, s := range in.Inputs {
				out[i] = hashVec(s)
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/rerank":
			if o.rerankStatus != 0 {
				w.WriteHeader(o.rerankStatus)
				return
			}
			var in struct {
				Query string
				Texts []string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			qt := tokens(in.Query)
			out := make([]map[string]any, len(in.Texts))
			for i, s := range in.Texts {
				score := 0.0
				for _, tk := range tokens(s) {
					if slices.Contains(qt, tk) {
						score++
					}
				}
				if o.rerankByIdx {
					score = float64(i)
				}
				out[i] = map[string]any{"index": i, "score": score}
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func loadSmoke(t *testing.T) *Fixture {
	t.Helper()
	f, err := os.Open("testdata/smoke-it-en.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fx, err := LoadFixture(f)
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func baseCfg(eng engine.Engine, embed, rerank *httptest.Server) Config {
	cfg := Config{Engine: eng, Embed: &TEIClient{BaseURL: embed.URL, Normalize: true, Truncate: true, HTTP: embed.Client()}, Repeat: 1, CollectionTag: "t1"}
	if rerank != nil {
		cfg.Rerank = &TEIClient{BaseURL: rerank.URL, Truncate: true, HTTP: rerank.Client()}
	}
	return cfg
}

func TestRun_ModesAndCleanup(t *testing.T) {
	fx := loadSmoke(t)
	fake := newFake()
	var bodies [][]string
	var mu sync.Mutex
	emb := newTEI(t, "embed-m", "sha-e", teiOpts{embedBodies: &bodies, mu: &mu})
	rr := newTEI(t, "rerank-m", "sha-r", teiOpts{})
	cfg := baseCfg(fake, emb, rr)
	cfg.QueryPrefix, cfg.DocumentPrefix = "query: ", "passage: "

	rep, err := Run(context.Background(), cfg, fx, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	var modes []string
	for _, m := range rep.Modes {
		modes = append(modes, m.Mode)
		if m.Skipped != "" || m.Samples != len(fx.Queries) {
			t.Errorf("mode %s: skipped %q samples %d", m.Mode, m.Skipped, m.Samples)
		}
	}
	if want := []string{"keyword", "dense", "hybrid", "hybrid+rerank"}; !slices.Equal(modes, want) {
		t.Fatalf("modes = %v", modes)
	}
	const wantName = "bench_scratcht1_g1"
	if len(fake.ensured) != 1 || fake.ensured[0].Name != wantName || fake.ensured[0].Dimensions != testDims {
		t.Fatalf("ensured = %+v", fake.ensured)
	}
	if !slices.Equal(fake.dropped, []string{wantName}) {
		t.Fatalf("dropped = %v", fake.dropped)
	}
	if fake.Points(wantName) != nil {
		t.Error("collection still present")
	}
	if len(fake.queries) != 4*len(fx.Queries) {
		t.Fatalf("queries = %d", len(fake.queries))
	}
	for i, q := range fake.queries {
		want := fx.Queries[i%len(fx.Queries)]
		if q.TenantID != "t-bench" || !slices.Equal(q.Principals, want.Principals) {
			t.Fatalf("query %d: tenant %q principals %v", i, q.TenantID, q.Principals)
		}
	}
	if rep.Dimensions != testDims || rep.EmbedModel != "embed-m" || rep.RerankRevision != "sha-r" || rep.Items != len(fx.Items) {
		t.Errorf("report = %+v", rep)
	}
	if !rep.OK() {
		t.Error("violations on a filtering engine")
	}
	// P21: documents carry the document prefix and the title first; queries the query prefix.
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 || !strings.HasPrefix(bodies[0][0], "passage: "+fx.Items[0].Title+"\n\n"+fx.Items[0].Text) {
		t.Errorf("document input = %q", bodies[0][0])
	}
	if last := bodies[len(bodies)-1]; !strings.HasPrefix(last[0], "query: ") {
		t.Errorf("query input = %q", last[0])
	}
}

func TestRun_IndexesProductionShape(t *testing.T) {
	fx := loadSmoke(t)
	fake := newFake()
	emb := newTEI(t, "e", "s", teiOpts{})
	cfg := baseCfg(fake, emb, nil)
	cfg.Repeat = 1
	s := &session{cfg: cfg, fx: fx, coll: "bench_scratcht1_g1"}
	s.cfg.applyDefaults()
	if err := s.index(context.Background()); err != nil {
		t.Fatal(err)
	}
	pts := fake.Points(s.coll)
	if len(pts) != len(fx.Items) {
		t.Fatalf("points = %d", len(pts))
	}
	it := fx.Items[0]
	p := pts[0]
	if p.Text != it.Title+"\n\n"+it.Text || p.ID != engine.PointID("t-bench", "scratcht1", it.ID, 0) ||
		p.Runtime.CharEnd != len([]rune(p.Text)) || p.Runtime.SourceID != it.ID || p.Runtime.IngestionProfile != "bench" {
		t.Errorf("point = %+v", p)
	}
}

func TestRun_DropsCollectionOnError(t *testing.T) {
	fx := loadSmoke(t)
	fake := newFake()
	emb := newTEI(t, "e", "s", teiOpts{})
	rr := newTEI(t, "r", "s", teiOpts{rerankStatus: 500})
	if _, err := Run(context.Background(), baseCfg(fake, emb, rr), fx, "smoke"); err == nil {
		t.Fatal("expected an error")
	}
	if len(fake.dropped) != 1 {
		t.Fatalf("dropped = %v", fake.dropped)
	}
}

func TestRun_DropsCollectionOnCancel(t *testing.T) {
	fx := loadSmoke(t)
	fake := newFake()
	fake.block = true
	emb := newTEI(t, "e", "s", teiOpts{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			fake.mu.Lock()
			n := len(fake.queries)
			fake.mu.Unlock()
			if n > 0 {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	_, err := Run(ctx, baseCfg(fake, emb, nil), fx, "smoke")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(fake.dropped) != 1 {
		t.Fatalf("dropped = %v", fake.dropped)
	}
}

func TestRun_CountsVisibilityViolations(t *testing.T) {
	fx := loadSmoke(t)
	fake := newFake()
	fake.ignorePrincipals = true
	emb := newTEI(t, "e", "s", teiOpts{})
	rep, err := Run(context.Background(), baseCfg(fake, emb, nil), fx, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("OK() with a leaking engine")
	}
	total := 0
	for _, m := range rep.Modes {
		total += m.VisibilityViolations
	}
	if total == 0 {
		t.Fatal("no violation counted")
	}
}

func TestRun_RerankReordersHybrid(t *testing.T) {
	fx := loadSmoke(t)
	fake := newFake()
	emb := newTEI(t, "e", "s", teiOpts{})
	rr := newTEI(t, "r", "s", teiOpts{rerankByIdx: true})
	cfg := baseCfg(fake, emb, rr)
	cfg.K, cfg.RerankTopK = 5, 5
	s := &session{cfg: cfg, fx: fx, coll: "bench_scratcht1_g1"}
	s.cfg.applyDefaults()
	if err := s.index(context.Background()); err != nil {
		t.Fatal(err)
	}
	q := fx.Queries[0]
	hy, _, err := s.rank(context.Background(), ModeHybrid, q)
	if err != nil {
		t.Fatal(err)
	}
	rk, _, err := s.rank(context.Background(), ModeHybridRerank, q)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(hy)
	if len(hy) < 2 || !slices.Equal(hy, rk) {
		t.Fatalf("hybrid reversed %v, reranked %v", hy, rk)
	}
}

func TestRun_NoRerankClientSkipsMode(t *testing.T) {
	fx := loadSmoke(t)
	emb := newTEI(t, "e", "s", teiOpts{})
	rep, err := Run(context.Background(), baseCfg(newFake(), emb, nil), fx, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	last := rep.Modes[3]
	if last.Mode != "hybrid+rerank" || last.Skipped == "" || !rep.OK() {
		t.Fatalf("last = %+v", last)
	}
}

func TestRun_RefusesBadInput(t *testing.T) {
	fx := loadSmoke(t)
	emb := newTEI(t, "e", "s", teiOpts{})

	fake := newFake()
	cfg := baseCfg(fake, emb, nil)
	cfg.CollectionTag = "Bad_Tag"
	if _, err := Run(context.Background(), cfg, fx, "x"); err == nil || len(fake.dropped) != 0 {
		t.Fatalf("bad tag: err %v dropped %v", err, fake.dropped)
	}

	// P22: declared model drift is an error before anything is created.
	cfg = baseCfg(fake, emb, nil)
	cfg.Declared = DeclaredModels{EmbedModel: "e", EmbedRevision: "other"}
	_, err := Run(context.Background(), cfg, fx, "x")
	if err == nil || !strings.Contains(err.Error(), "embed endpoint serves e@s, declared e@other") || len(fake.ensured) != 0 {
		t.Fatalf("drift: err %v", err)
	}
	cfg.Declared = DeclaredModels{EmbedModel: "e", EmbedRevision: "s"}
	if _, err := Run(context.Background(), cfg, fx, "x"); err != nil {
		t.Fatalf("matching declaration: %v", err)
	}
}
