// Package enginetest is the conformance suite of engine.Engine: each case is
// the definition of the behaviour the retrieval services rely on, and every
// driver runs it against a live engine (ADR-0023 D3).
//
// Every case calls newEngine, works only in collections named
// engine.CollectionName("ct"+<8 random hex>, <case in snake_case>, <generation>)
// and drops them in t.Cleanup, so the suite can run repeatedly and next to
// anything else on the same engine. A killed run cannot clean up: its
// collections stay behind on a shared engine, and every one of them matches
// ct<8 hex>_*, so they can be found and dropped by hand.
package enginetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Run runs every conformance case as a subtest of t. newEngine is called once
// per case with the subtest's t.
func Run(t *testing.T, newEngine func(t *testing.T) engine.Engine) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, h *harness)
	}{
		{"Health", testHealth},
		{"EnsureCollectionIsIdempotent", testEnsureCollectionIsIdempotent},
		{"MissingCollection", testMissingCollection},
		{"UpsertRequiresDenseVector", testUpsertRequiresDenseVector},
		{"RoundTrip", testRoundTrip},
		{"TenantIsolation", testTenantIsolation},
		{"TenantScopedKeywordStatistics", testTenantScopedKeywordStatistics},
		{"PrincipalFilter", testPrincipalFilter},
		{"ReservedNamespaceUnreachable", testReservedNamespaceUnreachable},
		{"UserFilter", testUserFilter},
		{"FusionWeightsAndModes", testFusionWeightsAndModes},
		{"Diagnostics", testDiagnostics},
		{"ConditionalUpsertNeverLowersRevision", testConditionalUpsertNeverLowersRevision},
		{"DeleteOtherRevisions", testDeleteOtherRevisions},
		{"DeleteByFilterAcrossCollections", testDeleteByFilterAcrossCollections},
		{"Count", testCount},
		{"StaleWriterAfterSuccessor", testStaleWriterAfterSuccessor},
		{"RecreatedPointIsDetectableByRevision", testRecreatedPointIsDetectableByRevision},
		{"AnalyzerStemming", testAnalyzerStemming},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEngine(t)
			if e == nil {
				t.Fatal("newEngine returned nil")
			}
			c.run(t, &harness{
				e:       e,
				prefix:  "ct" + randomHex(t),
				corpus:  snakeCase(c.name),
				cleaned: map[string]bool{},
			})
		})
	}
}

// --- harness -----------------------------------------------------------------

type harness struct {
	e       engine.Engine
	prefix  string // "ct" + 8 random hex, unique per case
	corpus  string // the case name in snake_case
	cleaned map[string]bool
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random collection prefix: %v", err)
	}
	return hex.EncodeToString(b)
}

func snakeCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// name is the case's collection of the given generation.
func (h *harness) name(generation int) string {
	return engine.CollectionName(h.prefix, h.corpus, generation)
}

// spec is a 4-dimension collection of the given generation with no declared
// fields and the zero analyzer.
func (h *harness) spec(generation int) engine.CollectionSpec {
	return engine.CollectionSpec{Name: h.name(generation), Dimensions: 4}
}

// dropOnCleanup drops the collection when the case ends, whatever happened.
// It is registered before the collection is created so a half-created one is
// dropped too.
func (h *harness) dropOnCleanup(t *testing.T, name string) {
	t.Helper()
	if h.cleaned[name] {
		return
	}
	h.cleaned[name] = true
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.e.DropCollection(ctx, name); err != nil {
			t.Errorf("cleanup: DropCollection(%s) = %v", name, err)
		}
	})
}

func (h *harness) ensure(t *testing.T, spec engine.CollectionSpec) string {
	t.Helper()
	h.dropOnCleanup(t, spec.Name)
	if err := h.e.EnsureCollection(t.Context(), spec); err != nil {
		t.Fatalf("EnsureCollection(%s) = %v", spec.Name, err)
	}
	return spec.Name
}

// collection creates the case's generation-1 collection with the default spec.
func (h *harness) collection(t *testing.T) string {
	t.Helper()
	return h.ensure(t, h.spec(1))
}

func (h *harness) upsert(t *testing.T, collection string, points ...engine.Point) {
	t.Helper()
	if err := h.e.Upsert(t.Context(), collection, points); err != nil {
		t.Fatalf("Upsert(%s, %d points) = %v", collection, len(points), err)
	}
}

func (h *harness) deleteOtherRevisions(t *testing.T, collection, tenantID, sourceID string, keep int64) {
	t.Helper()
	if err := h.e.DeleteOtherRevisions(t.Context(), collection, tenantID, sourceID, keep); err != nil {
		t.Fatalf("DeleteOtherRevisions(%s, %s, %s, keep %d) = %v", collection, tenantID, sourceID, keep, err)
	}
}

func (h *harness) deleteByFilter(t *testing.T, collection string, sel engine.Selector) {
	t.Helper()
	if err := h.e.DeleteByFilter(t.Context(), collection, sel); err != nil {
		t.Fatalf("DeleteByFilter(%s, %+v) = %v", collection, sel, err)
	}
}

func (h *harness) wantCount(t *testing.T, collection string, sel engine.Selector, want int64) {
	t.Helper()
	got, err := h.e.Count(t.Context(), collection, sel)
	if err != nil {
		t.Fatalf("Count(%s, %+v) = %v", collection, sel, err)
	}
	if got != want {
		t.Errorf("Count(%s, %+v) = %d, want %d", collection, sel, got, want)
	}
}

func (h *harness) search(t *testing.T, collection string, q engine.Query) []engine.Candidate {
	t.Helper()
	got, err := h.e.Query(t.Context(), collection, q)
	if err != nil {
		t.Fatalf("Query(%s, %s) = %v", collection, describeQuery(q), err)
	}
	return got
}

// query is a query as tenantID with the tenant-wide principal, both arms
// weighted 1 and room for every fixture point.
func query(tenantID string, dense []float32, keyword string) engine.Query {
	return engine.Query{
		TenantID:   tenantID,
		Principals: everyone,
		Dense:      dense,
		Keyword:    keyword,
		Weights:    engine.Weights{Dense: 1, Keyword: 1},
		PerArm:     100,
		Limit:      100,
	}
}

func describeQuery(q engine.Query) string {
	return fmt.Sprintf("tenant %q principals %q dense %v keyword %q weights %+v perArm %d limit %d diagnostics %t filter %s",
		q.TenantID, q.Principals, q.Dense, q.Keyword, q.Weights, q.PerArm, q.Limit, q.Diagnostics, describeFilter(q.Filter))
}

func describeFilter(f *engine.Filter) string {
	if f == nil {
		return "none"
	}
	return fmt.Sprintf("%+v", *f)
}

// mode is one way of running the same search: dense arm only, keyword arm
// only, or both fused.
type mode struct {
	name    string
	dense   []float32
	keyword string
}

func modes(dense []float32, keyword string) []mode {
	return []mode{
		{"dense", dense, ""},
		{"keyword", nil, keyword},
		{"hybrid", dense, keyword},
	}
}

// key identifies a stored chunk version: its point id plus tenant, source,
// ordinal, revision and text. Two writes of the same chunk share the id and
// differ in the rest.
func pointKey(p engine.Point) string {
	return describePoint(p) + " id=" + p.ID
}

func candidateKey(c engine.Candidate) string {
	return describeCandidate(c) + " id=" + c.PointID
}

func candidateKeys(got []engine.Candidate) []string {
	keys := make([]string, 0, len(got))
	for _, c := range got {
		keys = append(keys, candidateKey(c))
	}
	return keys
}

func pointKeys(want []engine.Point) []string {
	keys := make([]string, 0, len(want))
	for _, p := range want {
		keys = append(keys, pointKey(p))
	}
	return keys
}

// assertSet fails unless got holds exactly the want chunk versions, in any
// order, each once.
func assertSet(t *testing.T, label string, got []engine.Candidate, want ...engine.Point) {
	t.Helper()
	g, w := candidateKeys(got), pointKeys(want)
	sort.Strings(g)
	sort.Strings(w)
	if reflect.DeepEqual(g, w) {
		return
	}
	t.Errorf("%s: got %d candidates, want %d\n  missing:    %s\n  unexpected: %s\n  got:  %s\n  want: %s",
		label, len(g), len(w), lines(difference(w, g)), lines(difference(g, w)), lines(g), lines(w))
}

// assertOrder fails unless got holds exactly the want chunk versions in that
// order.
func assertOrder(t *testing.T, label string, got []engine.Candidate, want ...engine.Point) {
	t.Helper()
	g, w := candidateKeys(got), pointKeys(want)
	if reflect.DeepEqual(g, w) {
		return
	}
	t.Errorf("%s: wrong candidates or order\n  got:  %s\n  want: %s", label, lines(g), lines(w))
}

// assertFirst fails unless got has n candidates and the first is want.
func assertFirst(t *testing.T, label string, got []engine.Candidate, n int, want engine.Point) {
	t.Helper()
	if len(got) != n || candidateKey(got[0]) != pointKey(want) {
		t.Errorf("%s: want %d candidates led by %s\n  got: %s", label, n, describePoint(want), lines(candidateKeys(got)))
	}
}

func difference(a, b []string) []string {
	in := make(map[string]int, len(b))
	for _, s := range b {
		in[s]++
	}
	var out []string
	for _, s := range a {
		if in[s] > 0 {
			in[s]--
			continue
		}
		out = append(out, s)
	}
	return out
}

func lines(keys []string) string {
	if len(keys) == 0 {
		return "(none)"
	}
	return "\n    " + strings.Join(keys, "\n    ")
}

// refusal fails unless err is a refusal of the request: non-nil and neither
// engine sentinel, so the services do not mistake it for an outage or a
// missing collection.
func refusal(t *testing.T, label string, err error) {
	t.Helper()
	switch {
	case err == nil:
		t.Errorf("%s = nil, want a refusal", label)
	case errors.Is(err, engine.ErrUnavailable):
		t.Errorf("%s = %v, want a refusal, not engine.ErrUnavailable", label, err)
	case errors.Is(err, engine.ErrNotFound):
		t.Errorf("%s = %v, want a refusal, not engine.ErrNotFound", label, err)
	}
}

func f64(v float64) *float64 { return &v }

// --- cases -------------------------------------------------------------------

func testHealth(t *testing.T, h *harness) {
	if err := h.e.Health(t.Context()); err != nil {
		t.Fatalf("Health() = %v, want nil", err)
	}
}

func testEnsureCollectionIsIdempotent(t *testing.T, h *harness) {
	spec := h.spec(1)
	spec.Fields = []iface.RetrievalFilterField{
		{Name: "status", Type: iface.RetrievalFieldKeyword},
		{Name: "created", Type: iface.RetrievalFieldDatetime},
	}
	spec.Analyzer = iface.RetrievalAnalyzer{Tokenizer: "word", Language: "english", Stemming: true, StopWords: true}
	name := h.ensure(t, spec)
	if err := h.e.EnsureCollection(t.Context(), spec); err != nil {
		t.Fatalf("second EnsureCollection(%s) = %v, want nil", name, err)
	}

	wider := spec
	wider.Dimensions = 8
	refusal(t, "EnsureCollection with 8 dimensions on a 4-dimension collection",
		h.e.EnsureCollection(t.Context(), wider))

	// The refusal leaves the existing collection in place and usable.
	h.wantCount(t, name, engine.Selector{TenantID: tenantA}, 0)
}

func testMissingCollection(t *testing.T, h *harness) {
	name := h.name(1)
	h.dropOnCleanup(t, name) // in case a broken driver creates it implicitly
	ctx := t.Context()
	p := simple(tenantA, "s1", 1, 0, "never stored", vec(1, 0, 0, 0))
	calls := []struct {
		op   string
		call func() error
	}{
		{"Count", func() error {
			_, err := h.e.Count(ctx, name, engine.Selector{TenantID: tenantA})
			return err
		}},
		{"Query", func() error {
			_, err := h.e.Query(ctx, name, query(tenantA, vec(1, 0, 0, 0), "never"))
			return err
		}},
		{"DeleteByFilter", func() error {
			return h.e.DeleteByFilter(ctx, name, engine.Selector{TenantID: tenantA})
		}},
		{"DeleteOtherRevisions", func() error {
			return h.e.DeleteOtherRevisions(ctx, name, tenantA, "s1", 1)
		}},
		{"Upsert", func() error {
			return h.e.Upsert(ctx, name, []engine.Point{p})
		}},
	}
	for _, c := range calls {
		if err := c.call(); !errors.Is(err, engine.ErrNotFound) {
			t.Errorf("%s on a missing collection = %v, want engine.ErrNotFound", c.op, err)
		}
	}
	if err := h.e.DropCollection(ctx, name); err != nil {
		t.Errorf("DropCollection of a missing collection = %v, want nil", err)
	}
}

func testUpsertRequiresDenseVector(t *testing.T, h *harness) {
	name := h.collection(t)
	sel := engine.Selector{TenantID: tenantA}

	noVector := simple(tenantA, "s1", 1, 0, "keyword only", nil)
	if err := h.e.Upsert(t.Context(), name, []engine.Point{noVector}); err == nil {
		t.Error("Upsert of a point with Dense nil = nil, want an error")
	}
	h.wantCount(t, name, sel, 0)

	short := simple(tenantA, "s2", 1, 0, "three dimensions", []float32{1, 0, 0})
	refusal(t, "Upsert of a 3-dimension vector into a 4-dimension collection",
		h.e.Upsert(t.Context(), name, []engine.Point{short}))
	h.wantCount(t, name, sel, 0)
}

func testRoundTrip(t *testing.T, h *harness) {
	spec := h.spec(1)
	spec.Fields = []iface.RetrievalFilterField{
		{Name: "status", Type: iface.RetrievalFieldKeyword},
		{Name: "score", Type: iface.RetrievalFieldInteger},
		{Name: "tags", Type: iface.RetrievalFieldKeywords},
	}
	name := h.ensure(t, spec)

	text := "quarterly orchid supply agreement"
	p := point(tenantA, "s1", 7, 2, text, vec(0.1, 0.2, 0.3, 0.4),
		[]string{"tenant:*", "user:u1"}, []string{"user:u1", "crm:contact-1"},
		map[string]any{"status": "open", "score": int64(42), "tags": []string{"vip", "eu"}})
	p.Runtime.CharStart = 120
	p.Runtime.CharEnd = 120 + len([]rune(text))
	p.Runtime.Page = 3
	h.upsert(t, name, p)

	got := h.search(t, name, query(tenantA, p.Dense, ""))
	if len(got) != 1 {
		t.Fatalf("dense query: got %d candidates, want 1: %s", len(got), lines(candidateKeys(got)))
	}
	c := got[0]
	if c.PointID != p.ID {
		t.Errorf("PointID = %s, want %s", c.PointID, p.ID)
	}
	if !reflect.DeepEqual(c.Runtime, p.Runtime) {
		t.Errorf("Runtime did not round-trip\n  got:  %+v\n  want: %+v", c.Runtime, p.Runtime)
	}
	if c.Text != p.Text {
		t.Errorf("Text = %q, want %q", c.Text, p.Text)
	}
	if c.Fields["status"] != "open" {
		t.Errorf(`Fields["status"] = %#v, want "open" (Fields %#v)`, c.Fields["status"], c.Fields)
	}
	// Candidate.Fields is JSON-decoded: numbers come back as float64, lists
	// as []any of their elements.
	if c.Fields["score"] != float64(42) {
		t.Errorf(`Fields["score"] = %#v, want float64(42) (Fields %#v)`, c.Fields["score"], c.Fields)
	}
	if !reflect.DeepEqual(c.Fields["tags"], []any{"vip", "eu"}) {
		t.Errorf(`Fields["tags"] = %#v, want []any{"vip", "eu"} (Fields %#v)`, c.Fields["tags"], c.Fields)
	}
}

func testTenantIsolation(t *testing.T, h *harness) {
	name := h.collection(t)
	texts := []string{"apple pie", "apple orchard harvest", "apple cider vinegar"}
	vecs := [][]float32{vec(1, 0, 0, 0), vec(0, 1, 0, 0), vec(0, 0, 1, 0)}
	aPoints := make([]engine.Point, 0, len(texts))
	all := make([]engine.Point, 0, 2*len(texts))
	for i := range texts {
		source := fmt.Sprintf("s%d", i)
		a := simple(tenantA, source, 1, 0, texts[i], vecs[i])
		b := simple(tenantB, source, 1, 0, texts[i], vecs[i])
		aPoints = append(aPoints, a)
		all = append(all, a, b)
	}
	h.upsert(t, name, all...)

	for _, m := range modes(vec(1, 1, 1, 0), "apple") {
		got := h.search(t, name, query(tenantA, m.dense, m.keyword))
		for _, c := range got {
			if c.Runtime.TenantID != tenantA {
				t.Errorf("%s as %s: candidate %s belongs to another tenant", m.name, tenantA, describeCandidate(c))
			}
		}
		assertSet(t, m.name+" as "+tenantA, got, aPoints...)
	}
}

func testTenantScopedKeywordStatistics(t *testing.T, h *harness) {
	x := h.ensure(t, h.spec(1)) // tenant B alone
	y := h.ensure(t, h.spec(2)) // tenant B plus a large, orchid-heavy tenant A

	bPoints := []engine.Point{
		simple(tenantB, "b1", 1, 0, "orchid supplier contract", vec(1, 0, 0, 0)),
		simple(tenantB, "b2", 1, 0, "orchid", vec(0, 1, 0, 0)),
		simple(tenantB, "b3", 1, 0, "supplier invoice archive quarterly", vec(0, 0, 1, 0)),
	}
	h.upsert(t, x, bPoints...)
	h.upsert(t, y, bPoints...)

	// 50 documents of 40 tokens, each with "orchid" 5 times: with
	// collection-wide statistics they would change both the term's IDF and
	// the average document length B's scores are computed with.
	aPoints := make([]engine.Point, 0, 50)
	for i := range 50 {
		tokens := make([]string, 40)
		for j := range tokens {
			if j%8 == 0 {
				tokens[j] = "orchid"
			} else {
				tokens[j] = fmt.Sprintf("filler%dx%d", i, j)
			}
		}
		aPoints = append(aPoints, simple(tenantA, fmt.Sprintf("a%d", i), 1, 0,
			strings.Join(tokens, " "), vec(0, 0, 0.5, 1)))
	}
	h.upsert(t, y, aPoints...)

	keyword := func(collection, tenantID string) []engine.Candidate {
		q := query(tenantID, nil, "orchid supplier")
		q.Diagnostics = true
		return h.search(t, collection, q)
	}

	inX := keyword(x, tenantB)
	inY := keyword(y, tenantB)
	assertSet(t, "as B in X", inX, bPoints...)
	if !reflect.DeepEqual(candidateKeys(inX), candidateKeys(inY)) {
		t.Fatalf("as B, tenant A's documents changed the ranking\n  X: %s\n  Y: %s",
			lines(candidateKeys(inX)), lines(candidateKeys(inY)))
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-5*math.Max(1, math.Abs(a)) }
	for i := range inX {
		cx, cy := inX[i], inY[i]
		if !near(float64(cx.Score), float64(cy.Score)) {
			t.Errorf("as B, hit %d %s: fused score %v in X, %v in Y", i, describeCandidate(cx), cx.Score, cy.Score)
		}
		if cx.KeywordScore == nil || cy.KeywordScore == nil {
			t.Errorf("as B, hit %d %s: KeywordScore X=%v Y=%v, want both set with Diagnostics", i, describeCandidate(cx), cx.KeywordScore, cy.KeywordScore)
			continue
		}
		if !near(float64(*cx.KeywordScore), float64(*cy.KeywordScore)) {
			t.Errorf("as B, hit %d %s: BM25 score %v in X, %v in Y — tenant A's documents leak into B's keyword statistics",
				i, describeCandidate(cx), *cx.KeywordScore, *cy.KeywordScore)
		}
	}

	asA := keyword(y, tenantA)
	for _, c := range asA {
		if c.Runtime.TenantID != tenantA {
			t.Errorf("as A in Y: candidate %s belongs to another tenant", describeCandidate(c))
		}
	}
	assertSet(t, "as A in Y", asA, aPoints...)
}

func testPrincipalFilter(t *testing.T, h *harness) {
	name := h.collection(t)
	v := vec(1, 0, 0, 0)
	pu := point(tenantA, "pu", 1, 0, "memo for user u1", v, []string{"user:u1"}, nil, nil)
	pr := point(tenantA, "pr", 1, 0, "memo for role r1", v, []string{"role:r1"}, nil, nil)
	pt := point(tenantA, "pt", 1, 0, "memo for the tenant", v, []string{"tenant:*"}, nil, nil)
	pn := point(tenantA, "pn", 1, 0, "memo for nobody", v, []string{}, nil, nil)
	h.upsert(t, name, pu, pr, pt, pn)

	cases := []struct {
		principals []string
		want       []engine.Point
	}{
		{[]string{"user:u2", "role:r1"}, []engine.Point{pr}},
		{[]string{"tenant:*", "user:u1"}, []engine.Point{pu, pt}},
		// Pn (empty visibleTo) is found by nobody.
		{[]string{"tenant:*", "user:u1", "role:r1"}, []engine.Point{pu, pr, pt}},
	}
	for _, c := range cases {
		for _, m := range modes(v, "memo") {
			q := query(tenantA, m.dense, m.keyword)
			q.Principals = c.principals
			assertSet(t, fmt.Sprintf("%s with principals %q", m.name, c.principals), h.search(t, name, q), c.want...)
		}
	}

	unscoped := []struct {
		label  string
		mutate func(q *engine.Query)
	}{
		{"Principals nil", func(q *engine.Query) { q.Principals = nil }},
		{"Principals empty", func(q *engine.Query) { q.Principals = []string{} }},
		{`TenantID ""`, func(q *engine.Query) { q.TenantID = "" }},
	}
	for _, u := range unscoped {
		q := query(tenantA, v, "memo")
		u.mutate(&q)
		got, err := h.e.Query(t.Context(), name, q)
		if err == nil {
			t.Errorf("Query with %s = %d candidates and no error, want an error: %s", u.label, len(got), lines(candidateKeys(got)))
		}
	}
}

func testReservedNamespaceUnreachable(t *testing.T, h *harness) {
	name := h.collection(t)
	pts := []engine.Point{
		simple(tenantA, "s1", 1, 0, "ledger entry one", vec(1, 0, 0, 0)),
		simple(tenantA, "s2", 1, 0, "ledger entry two", vec(0, 1, 0, 0)),
	}
	h.upsert(t, name, pts...)

	for _, m := range modes(vec(1, 1, 0, 0), "ledger") {
		// Control: without the filter both points are found.
		assertSet(t, m.name+" without a filter", h.search(t, name, query(tenantA, m.dense, m.keyword)), pts...)
		for _, field := range []string{"_rt.tenantId", "tenantId"} {
			q := query(tenantA, m.dense, m.keyword)
			q.Filter = &engine.Filter{Field: field, Match: []any{tenantA}}
			if got := h.search(t, name, q); len(got) != 0 {
				t.Errorf("%s with a user filter on %q reached the runtime namespace: %s", m.name, field, lines(candidateKeys(got)))
			}
		}
	}
}

func testUserFilter(t *testing.T, h *harness) {
	spec := h.spec(1)
	spec.Fields = []iface.RetrievalFilterField{
		{Name: "status", Type: iface.RetrievalFieldKeyword},
		{Name: "tags", Type: iface.RetrievalFieldKeywords},
		{Name: "score", Type: iface.RetrievalFieldInteger},
		{Name: "ratio", Type: iface.RetrievalFieldFloat},
		{Name: "active", Type: iface.RetrievalFieldBool},
		{Name: "created", Type: iface.RetrievalFieldDatetime},
	}
	name := h.ensure(t, spec)

	const (
		t0 = int64(1_690_000_000_000) // Unix ms
		t1 = int64(1_700_000_000_000)
		t2 = int64(1_710_000_000_000)
		t3 = int64(1_720_000_000_000)
	)
	row := func(source, text string, v []float32, fields map[string]any) engine.Point {
		return point(tenantA, source, 1, 0, text, v, everyone, nil, fields)
	}
	p1 := row("p1", "row one", vec(1, 0, 0, 0), map[string]any{
		"status": "open", "tags": []string{"vip", "eu"}, "score": int64(5), "ratio": 0.5, "active": true, "created": t1,
	})
	p2 := row("p2", "row two", vec(0.9, 0.1, 0, 0), map[string]any{
		"status": "closed", "tags": []string{"vip"}, "score": int64(55), "ratio": 1.5, "active": false, "created": t2,
	})
	p3 := row("p3", "row three", vec(0.8, 0.2, 0, 0), map[string]any{ // no ratio
		"status": "open", "tags": []string{"eu"}, "score": int64(60), "active": false, "created": t3,
	})
	p4 := row("p4", "row four", vec(0.7, 0.3, 0, 0), map[string]any{
		"status": "pending", "tags": []string{}, "score": int64(10), "ratio": 0.1, "active": true, "created": t0,
	})
	h.upsert(t, name, p1, p2, p3, p4)

	// Points the scope must hide whatever the user filter says: tenant B's,
	// and tenant A's visible only to user:zz. Between the "open" and the
	// "pending" variant they satisfy every row below, so each row (Should and
	// MustNot included) is also a check that the user filter is ANDed with
	// the tenant and principal scope, never merged into or ORed with it.
	leak := func(tenant, source string, visibleTo []string, status string) engine.Point {
		return point(tenant, source, 1, 0, "hidden "+status, vec(1, 0, 0, 0), visibleTo, nil, map[string]any{
			"status": status, "tags": []string{"vip"}, "score": int64(70), "ratio": 0.9, "active": false, "created": t0 - 1000,
		})
	}
	h.upsert(t, name,
		leak(tenantB, "leak-b-open", everyone, "open"),
		leak(tenantB, "leak-b-pending", everyone, "pending"),
		leak(tenantA, "leak-zz-open", []string{"user:zz"}, "open"),
		leak(tenantA, "leak-zz-pending", []string{"user:zz"}, "pending"),
	)

	cases := []struct {
		label  string
		filter engine.Filter
		want   []engine.Point
	}{
		{"Must{status in [open]}", engine.Filter{Must: []engine.Filter{{Field: "status", Match: []any{"open"}}}},
			[]engine.Point{p1, p3}},
		{"status in [closed pending]", engine.Filter{Field: "status", Match: []any{"closed", "pending"}},
			[]engine.Point{p2, p4}},
		{"tags match [vip]", engine.Filter{Field: "tags", Match: []any{"vip"}},
			[]engine.Point{p1, p2}},
		{"score gte 10", engine.Filter{Field: "score", Range: &engine.Range{Gte: f64(10)}},
			[]engine.Point{p2, p3, p4}},
		{"ratio gt 0.2", engine.Filter{Field: "ratio", Range: &engine.Range{Gt: f64(0.2)}},
			[]engine.Point{p1, p2}},
		{"created lt t2", engine.Filter{Field: "created", Range: &engine.Range{Lt: f64(float64(t2))}},
			[]engine.Point{p1, p4}},
		{"MustNot{active match [true]}", engine.Filter{MustNot: []engine.Filter{{Field: "active", Match: []any{true}}}},
			[]engine.Point{p2, p3}},
		{"ratio exists", engine.Filter{Field: "ratio", Exists: true},
			[]engine.Point{p1, p2, p4}},
		{"Should{status = open, score gte 50}", engine.Filter{Should: []engine.Filter{
			{Field: "status", Match: []any{"open"}},
			{Field: "score", Range: &engine.Range{Gte: f64(50)}},
		}}, []engine.Point{p1, p2, p3}},
	}
	for _, c := range cases {
		q := query(tenantA, vec(1, 0, 0, 0), "")
		q.Filter = &c.filter
		assertSet(t, c.label, h.search(t, name, q), c.want...)
	}
}

// fusionPoints are the three points of the fusion cases: P1 wins the dense
// arm, P2 is the only keyword match, P3 is dense runner-up.
func fusionPoints() (p1, p2, p3 engine.Point) {
	return simple(tenantA, "p1", 1, 0, "alpha", vec(1, 0, 0, 0)),
		simple(tenantA, "p2", 1, 0, "zebra quagga", vec(0, 1, 0, 0)),
		simple(tenantA, "p3", 1, 0, "lorem", vec(0.9, 0.1, 0, 0))
}

func testFusionWeightsAndModes(t *testing.T, h *harness) {
	name := h.collection(t)
	p1, p2, p3 := fusionPoints()
	h.upsert(t, name, p1, p2, p3)

	base := func() engine.Query {
		q := query(tenantA, vec(1, 0, 0, 0), "quagga")
		q.PerArm = 2
		q.Limit = 2
		return q
	}

	q := base()
	q.Keyword = ""
	assertOrder(t, `keyword "" (dense arm only)`, h.search(t, name, q), p1, p3)

	q = base()
	q.Dense = nil
	assertOrder(t, "dense nil (keyword arm only)", h.search(t, name, q), p2)

	q = base()
	q.Weights = engine.Weights{Dense: 10, Keyword: 1}
	assertFirst(t, "hybrid weights {10,1}", h.search(t, name, q), 2, p1)

	q = base()
	q.Weights = engine.Weights{Dense: 1, Keyword: 10}
	assertFirst(t, "hybrid weights {1,10}", h.search(t, name, q), 2, p2)
}

func testDiagnostics(t *testing.T, h *harness) {
	name := h.collection(t)
	p1, p2, p3 := fusionPoints()
	h.upsert(t, name, p1, p2, p3)

	q := query(tenantA, vec(1, 0, 0, 0), "quagga")
	q.PerArm = 3
	q.Limit = 3
	q.Diagnostics = true
	got := h.search(t, name, q)
	assertSet(t, "hybrid with Diagnostics", got, p1, p2, p3)
	byID := map[string]engine.Candidate{}
	for _, c := range got {
		byID[c.PointID] = c
	}
	if c, ok := byID[p2.ID]; ok {
		if c.DenseScore == nil || c.KeywordScore == nil {
			t.Errorf("P2 (in both arms): DenseScore %v KeywordScore %v, want both set", c.DenseScore, c.KeywordScore)
		} else if *c.KeywordScore <= 0 {
			t.Errorf("P2: KeywordScore %v, want > 0", *c.KeywordScore)
		}
	}
	for _, p := range []engine.Point{p1, p3} {
		c, ok := byID[p.ID]
		if !ok {
			continue
		}
		if c.DenseScore == nil {
			t.Errorf("%s (dense arm only): DenseScore nil, want set", describePoint(p))
		}
		if c.KeywordScore != nil {
			t.Errorf("%s (no keyword match): KeywordScore %v, want nil", describePoint(p), *c.KeywordScore)
		}
	}
	if c, ok := byID[p1.ID]; ok && c.DenseScore != nil && math.Abs(float64(*c.DenseScore)-1) > 1e-5 {
		t.Errorf("P1: DenseScore %v, want the cosine similarity 1 of identical vectors", *c.DenseScore)
	}

	q.Diagnostics = false
	for _, c := range h.search(t, name, q) {
		if c.DenseScore != nil || c.KeywordScore != nil {
			t.Errorf("without Diagnostics, %s: DenseScore %v KeywordScore %v, want both nil",
				describeCandidate(c), c.DenseScore, c.KeywordScore)
		}
	}
}

func testConditionalUpsertNeverLowersRevision(t *testing.T, h *harness) {
	name := h.collection(t)
	v := vec(1, 0, 0, 0)
	stored := func(label string) engine.Candidate {
		t.Helper()
		q := query(tenantA, v, "")
		q.Principals = []string{"tenant:*", "user:u9"}
		got := h.search(t, name, q)
		if len(got) != 1 {
			t.Fatalf("%s: got %d stored points, want 1: %s", label, len(got), lines(candidateKeys(got)))
		}
		return got[0]
	}

	h.upsert(t, name, simple(tenantA, "s1", 2, 0, "new", v))

	h.upsert(t, name, simple(tenantA, "s1", 1, 0, "old", v))
	if c := stored("after writing rev 1 over rev 2"); c.Runtime.SourceRevision != 2 || c.Text != "new" {
		t.Errorf("after writing rev 1 over rev 2: stored %s, want rev 2 \"new\" kept", describeCandidate(c))
	}

	h.upsert(t, name, point(tenantA, "s1", 2, 0, "new", v, []string{"user:u9"}, nil, nil))
	if c := stored("after rewriting rev 2"); c.Runtime.SourceRevision != 2 || !reflect.DeepEqual(c.Runtime.VisibleTo, []string{"user:u9"}) {
		t.Errorf("after rewriting rev 2 with visibleTo [user:u9]: stored %s visibleTo %q, want the equal revision applied",
			describeCandidate(c), c.Runtime.VisibleTo)
	}

	h.upsert(t, name, simple(tenantA, "s1", 3, 0, "newest", v))
	if c := stored("after writing rev 3"); c.Runtime.SourceRevision != 3 || c.Text != "newest" {
		t.Errorf("after writing rev 3: stored %s, want rev 3 \"newest\"", describeCandidate(c))
	}
}

func testDeleteOtherRevisions(t *testing.T, h *harness) {
	name := h.collection(t)
	v := vec(1, 0, 0, 0)
	h.upsert(t, name,
		simple(tenantA, "s1", 1, 0, "s1 rev1 part0", v),
		simple(tenantA, "s1", 1, 1, "s1 rev1 part1", v),
		simple(tenantA, "s1", 1, 2, "s1 rev1 part2", v),
		simple(tenantA, "s2", 1, 0, "s2 rev1 part0", v),
		simple(tenantB, "s1", 1, 0, "B s1 rev1 part0", v),
	)
	// Rev 2 has two chunks: it rewrites ordinals 0 and 1 and leaves ordinal 2 at rev 1.
	keep0 := simple(tenantA, "s1", 2, 0, "s1 rev2 part0", v)
	keep1 := simple(tenantA, "s1", 2, 1, "s1 rev2 part1", v)
	h.upsert(t, name, keep0, keep1)
	// A chunk of a higher revision than the one kept: "differs from keep"
	// removes it too, not only the older ones.
	h.upsert(t, name, simple(tenantA, "s1", 3, 5, "s1 rev3 part5", v))
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1"}}, 4)

	h.deleteOtherRevisions(t, name, tenantA, "s1", 2)
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1"}}, 2)
	var s1 []engine.Candidate
	for _, c := range h.search(t, name, query(tenantA, v, "")) {
		if c.Runtime.SourceID == "s1" {
			s1 = append(s1, c)
		}
	}
	assertSet(t, "A/s1 after DeleteOtherRevisions(keep 2)", s1, keep0, keep1)
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s2"}}, 1)
	h.wantCount(t, name, engine.Selector{TenantID: tenantB, SourceIDs: []string{"s1"}}, 1)
}

func testDeleteByFilterAcrossCollections(t *testing.T, h *harness) {
	g1 := h.ensure(t, h.spec(1))
	g2 := h.ensure(t, h.spec(2))
	v := vec(1, 0, 0, 0)
	for _, c := range []string{g1, g2} {
		h.upsert(t, c,
			point(tenantA, "s1", 1, 0, "A about x", v, everyone, []string{"user:x"}, nil),
			point(tenantA, "s2", 1, 0, "A about y", v, everyone, []string{"user:y"}, nil),
			point(tenantB, "s1", 1, 0, "B about x", v, everyone, []string{"user:x"}, nil),
		)
	}

	for _, c := range []string{g1, g2} {
		h.deleteByFilter(t, c, engine.Selector{TenantID: tenantA, Subject: "user:x"})
	}
	for _, c := range []string{g1, g2} {
		h.wantCount(t, c, engine.Selector{TenantID: tenantA, Subject: "user:x"}, 0)
		h.wantCount(t, c, engine.Selector{TenantID: tenantB, Subject: "user:x"}, 1)
		h.wantCount(t, c, engine.Selector{TenantID: tenantA, Subject: "user:y"}, 1)
	}

	if err := h.e.DeleteByFilter(t.Context(), g1, engine.Selector{}); err == nil {
		t.Error("DeleteByFilter with an empty selector = nil, want an error")
	}
	for _, c := range []string{g1, g2} {
		h.wantCount(t, c, engine.Selector{TenantID: tenantA}, 1)
		h.wantCount(t, c, engine.Selector{TenantID: tenantB}, 1)
	}

	h.deleteByFilter(t, g1, engine.Selector{TenantID: tenantB})
	h.wantCount(t, g1, engine.Selector{TenantID: tenantB}, 0)
	h.wantCount(t, g1, engine.Selector{TenantID: tenantA}, 1)
	h.wantCount(t, g2, engine.Selector{TenantID: tenantB}, 1)
}

func testCount(t *testing.T, h *harness) {
	name := h.collection(t)
	v := vec(1, 0, 0, 0)
	h.upsert(t, name,
		point(tenantA, "s1", 1, 0, "A s1 part0", v, everyone, []string{"user:x"}, nil),
		point(tenantA, "s1", 1, 1, "A s1 part1", v, everyone, []string{"user:y"}, nil),
		point(tenantA, "s2", 1, 0, "A s2 part0", v, everyone, nil, nil),
		point(tenantB, "s1", 1, 0, "B s1 part0", v, everyone, []string{"user:x"}, nil),
	)

	h.wantCount(t, name, engine.Selector{TenantID: tenantA}, 3)
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1", "s2"}}, 3)
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1"}}, 2)
	h.wantCount(t, name, engine.Selector{TenantID: tenantB}, 1)
	h.wantCount(t, name, engine.Selector{Subject: "user:x"}, 2) // across tenants
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1"}, Subject: "user:x"}, 1)

	if n, err := h.e.Count(t.Context(), name, engine.Selector{}); err == nil {
		t.Errorf("Count with an empty selector = %d, nil, want an error", n)
	}
}

func testStaleWriterAfterSuccessor(t *testing.T, h *harness) {
	name := h.collection(t)
	v0, v1, v2 := vec(1, 0, 0, 0), vec(0.9, 0.1, 0, 0), vec(0.8, 0.2, 0, 0)

	// The successor indexes rev 2 and removes every other revision.
	succ0 := simple(tenantA, "s1", 2, 0, "rev2 part0", v0)
	succ1 := simple(tenantA, "s1", 2, 1, "rev2 part1", v1)
	h.upsert(t, name, succ0, succ1)
	h.deleteOtherRevisions(t, name, tenantA, "s1", 2)

	// The stale worker resumes and writes rev 1 in full.
	stale2 := simple(tenantA, "s1", 1, 2, "rev1 part2", v2)
	h.upsert(t, name,
		simple(tenantA, "s1", 1, 0, "rev1 part0", v0),
		simple(tenantA, "s1", 1, 1, "rev1 part1", v1),
		stale2,
	)
	assertSet(t, "after the stale rev 1 write", h.search(t, name, query(tenantA, v0, "")), succ0, succ1, stale2)

	// The successor's cleanup pass removes the orphan.
	h.deleteOtherRevisions(t, name, tenantA, "s1", 2)
	assertSet(t, "after the successor re-applies DeleteOtherRevisions", h.search(t, name, query(tenantA, v0, "")), succ0, succ1)
}

func testRecreatedPointIsDetectableByRevision(t *testing.T, h *harness) {
	name := h.collection(t)
	v := vec(1, 0, 0, 0)
	h.upsert(t, name, simple(tenantA, "s1", 3, 0, "rev3", v))
	h.deleteByFilter(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1"}})
	h.wantCount(t, name, engine.Selector{TenantID: tenantA, SourceIDs: []string{"s1"}}, 0)

	// ADR D11: the engine cannot refuse the insert of a point it no longer
	// has; the read path drops it because the source reports revision 3.
	recreated := simple(tenantA, "s1", 2, 0, "rev2 stale", v)
	h.upsert(t, name, recreated)
	got := h.search(t, name, query(tenantA, v, ""))
	assertSet(t, "after the stale rev 2 write", got, recreated)
	for _, c := range got {
		if c.Runtime.SourceRevision != 2 {
			t.Errorf("recreated point carries revision %d, want the stale 2", c.Runtime.SourceRevision)
		}
	}
}

func testAnalyzerStemming(t *testing.T, h *harness) {
	spec := h.spec(1)
	spec.Analyzer = iface.RetrievalAnalyzer{Tokenizer: "word", Language: "english", Stemming: true, StopWords: true}
	name := h.ensure(t, spec)
	p := simple(tenantA, "s1", 1, 0, "running shoes for the marathon", vec(1, 0, 0, 0))
	h.upsert(t, name, p)

	assertSet(t, `keyword "runs" (stemmed like "running")`, h.search(t, name, query(tenantA, nil, "runs")), p)
	if got := h.search(t, name, query(tenantA, nil, "the")); len(got) != 0 {
		t.Errorf(`keyword "the" (a stop word) matched: %s`, lines(candidateKeys(got)))
	}
}
