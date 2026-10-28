package qdrant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

var errMalformedFilter = errors.New("qdrant: query: malformed filter")

// renderFilter compiles a user filter into a Qdrant filter. Every field is
// rendered under payload namespace f, so a user filter can never address the
// runtime namespace _rt. A malformed node is refused rather than rendered
// into something wider: a leaf has a field and exactly one condition (a
// non-empty Match, a Range with at least one bound, or Exists) and no
// children; a composite has no field and at least one child.
func renderFilter(f *engine.Filter) (map[string]any, error) {
	hasChildren := len(f.Must)+len(f.Should)+len(f.MustNot) > 0
	if f.Field != "" {
		if hasChildren {
			return nil, errMalformedFilter
		}
		return renderLeaf(f)
	}
	if !hasChildren {
		return nil, errMalformedFilter
	}
	out := map[string]any{}
	for _, part := range []struct {
		key   string
		nodes []engine.Filter
	}{{"must", f.Must}, {"should", f.Should}, {"must_not", f.MustNot}} {
		if len(part.nodes) == 0 {
			continue
		}
		rendered := make([]any, len(part.nodes))
		for i := range part.nodes {
			r, err := renderFilter(&part.nodes[i])
			if err != nil {
				return nil, err
			}
			rendered[i] = r
		}
		out[part.key] = rendered
	}
	return out, nil
}

func renderLeaf(f *engine.Filter) (map[string]any, error) {
	conditions := 0
	for _, set := range []bool{len(f.Match) > 0, f.Range != nil, f.Exists} {
		if set {
			conditions++
		}
	}
	if conditions != 1 {
		return nil, errMalformedFilter
	}
	key := "f." + f.Field
	switch {
	case len(f.Match) == 1:
		return matchValue(key, f.Match[0]), nil
	case len(f.Match) > 1:
		return matchAny(key, f.Match), nil
	case f.Range != nil:
		r := map[string]any{}
		for _, b := range []struct {
			name string
			v    *float64
		}{{"lt", f.Range.Lt}, {"lte", f.Range.Lte}, {"gt", f.Range.Gt}, {"gte", f.Range.Gte}} {
			if b.v != nil {
				r[b.name] = *b.v
			}
		}
		if len(r) == 0 {
			return nil, errMalformedFilter
		}
		return map[string]any{"key": key, "range": r}, nil
	}
	return map[string]any{"must_not": []any{map[string]any{"is_empty": map[string]any{"key": key}}}}, nil
}

// scopeFilter is the tenant and principal scope ANDed with the user filter.
// It is applied to every prefetch and at top level.
func scopeFilter(tenantID string, principals []string, user *engine.Filter) (map[string]any, error) {
	must := []any{matchValue(rtTenantID, tenantID), matchAny(rtVisibleTo, principals)}
	if user != nil {
		rendered, err := renderFilter(user)
		if err != nil {
			return nil, err
		}
		must = append(must, rendered)
	}
	return map[string]any{"must": must}, nil
}

// checkQuery refuses a query that is unscoped or out of bounds, and returns
// the per-arm candidate count.
func checkQuery(q engine.Query) (perArm int, err error) {
	if q.TenantID == "" {
		return 0, errors.New("qdrant: query: tenant id is required")
	}
	if len(q.Principals) == 0 {
		return 0, errors.New("qdrant: query: principals are required")
	}
	for _, p := range q.Principals {
		if p == "" {
			return 0, errors.New("qdrant: query: empty principal")
		}
	}
	hasDense, hasKeyword := len(q.Dense) > 0, q.Keyword != ""
	switch {
	case !hasDense && !hasKeyword:
		return 0, errors.New("qdrant: query: neither a dense vector nor a keyword")
	case hasDense && !(q.Weights.Dense > 0):
		return 0, errors.New("qdrant: query: dense weight must be positive")
	case hasKeyword && !(q.Weights.Keyword > 0):
		return 0, errors.New("qdrant: query: keyword weight must be positive")
	}
	if q.Limit < 1 || q.Limit > maxQueryLimit {
		return 0, fmt.Errorf("qdrant: query: limit %d out of range 1..%d", q.Limit, maxQueryLimit)
	}
	perArm = max(q.PerArm, q.Limit)
	if perArm > maxQueryLimit {
		return 0, fmt.Errorf("qdrant: query: per-arm %d above %d", perArm, maxQueryLimit)
	}
	return perArm, nil
}

// arm is one prefetch of the hybrid query.
type arm struct {
	body   map[string]any
	weight float64
	dense  bool
}

func queryArms(q engine.Query, scope map[string]any, perArm int) []arm {
	var arms []arm
	if len(q.Dense) > 0 {
		arms = append(arms, arm{dense: true, weight: q.Weights.Dense, body: map[string]any{
			"query":  q.Dense,
			"using":  denseVector,
			"filter": scope,
			"limit":  perArm,
		}})
	}
	if q.Keyword != "" {
		arms = append(arms, arm{weight: q.Weights.Keyword, body: map[string]any{
			"query":  map[string]any{"text": q.Keyword},
			"using":  textField,
			"filter": scope,
			// BM25 statistics (IDF) over the tenant's points only.
			"params": map[string]any{"idf": map[string]any{"corpus": map[string]any{
				"must": []any{matchValue(rtTenantID, q.TenantID)},
			}}},
			"limit": perArm,
		}})
	}
	return arms
}

type scoredPoint struct {
	ID      string  `json:"id"`
	Score   float32 `json:"score"`
	Payload struct {
		RT runtimePayload `json:"_rt"`
		F  map[string]any `json:"f"`
	} `json:"payload"`
}

type queryResult struct {
	Points []scoredPoint `json:"points"`
}

// Query runs the hybrid search: one prefetch per present arm, fused with
// weighted RRF. The scope filter is repeated on every prefetch and at top
// level, so no propagation rule can leave an arm unscoped. With Diagnostics
// the fused query and each arm alone run in one batch, and the arms' own
// scores are attached to the fused candidates.
func (d *Driver) Query(ctx context.Context, collection string, q engine.Query) ([]engine.Candidate, error) {
	q.Keyword = strings.TrimSpace(q.Keyword) // a blank keyword is no keyword arm
	perArm, err := checkQuery(q)
	if err != nil {
		return nil, err
	}
	scope, err := scopeFilter(q.TenantID, q.Principals, q.Filter)
	if err != nil {
		return nil, err
	}
	arms := queryArms(q, scope, perArm)
	prefetch := make([]any, len(arms))
	weights := make([]float64, len(arms))
	for i, a := range arms {
		prefetch[i] = a.body
		weights[i] = a.weight
	}
	fused := map[string]any{
		"prefetch":     prefetch,
		"query":        map[string]any{"rrf": map[string]any{"weights": weights}},
		"filter":       scope,
		"limit":        q.Limit,
		"with_payload": []string{"_rt", "f"},
	}
	path := pointsPath(collection) + "/query"

	if !q.Diagnostics {
		var res queryResult
		if err := d.do(ctx, "query", http.MethodPost, path, fused, &res); err != nil {
			return nil, err
		}
		return d.inScope(ctx, q, candidates(res.Points)), nil
	}

	searches := []any{fused}
	for _, a := range arms {
		alone := make(map[string]any, len(a.body)+1)
		for k, v := range a.body {
			alone[k] = v
		}
		alone["with_payload"] = false
		searches = append(searches, alone)
	}
	var res []queryResult
	if err := d.do(ctx, "query", http.MethodPost, path+"/batch", map[string]any{"searches": searches}, &res); err != nil {
		return nil, err
	}
	if len(res) != len(searches) {
		return nil, &transportError{op: "query", reason: "returned an undecodable body"}
	}
	out := candidates(res[0].Points)
	for i, a := range arms {
		scores := make(map[string]float32, len(res[i+1].Points))
		for _, p := range res[i+1].Points {
			scores[p.ID] = p.Score
		}
		for j := range out {
			s, ok := scores[out[j].PointID]
			if !ok {
				continue
			}
			if a.dense {
				out[j].DenseScore = &s
			} else {
				out[j].KeywordScore = &s
			}
		}
	}
	return d.inScope(ctx, q, out), nil
}

// inScope drops any candidate outside the query's tenant or principals. The
// scope filter sent to Qdrant is the control; this catches a regression in
// it, and the warning carries counts only.
func (d *Driver) inScope(ctx context.Context, q engine.Query, cands []engine.Candidate) []engine.Candidate {
	principals := make(map[string]bool, len(q.Principals))
	for _, p := range q.Principals {
		principals[p] = true
	}
	kept := cands[:0]
	for _, c := range cands {
		if c.Runtime.TenantID == q.TenantID && slices.ContainsFunc(c.Runtime.VisibleTo, func(v string) bool { return principals[v] }) {
			kept = append(kept, c)
		}
	}
	if dropped := len(cands) - len(kept); dropped > 0 {
		d.cfg.Logger.WarnContext(ctx, "qdrant: query returned candidates outside the requested scope; dropped them",
			slog.Int("dropped", dropped), slog.Int("returned", len(cands)))
	}
	return kept
}

func candidates(points []scoredPoint) []engine.Candidate {
	out := make([]engine.Candidate, len(points))
	for i, p := range points {
		out[i] = engine.Candidate{
			PointID: p.ID,
			Runtime: p.Payload.RT.Runtime,
			Text:    p.Payload.RT.Text,
			Fields:  p.Payload.F,
			Score:   p.Score,
		}
	}
	return out
}
