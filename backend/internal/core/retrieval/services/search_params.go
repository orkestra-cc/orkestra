package services

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Search request bounds. Fusion defaults and the default rerank depth are
// DefaultFusion and DefaultRerankTopK (registry.go); the weight, candidate and
// rerank bounds are the registry's own, shared with request overrides.
const (
	DefaultSearchLimit = 10
	MaxSearchLimit     = 100
	MaxQueryRunes      = 2000
	MaxRerankTopK      = maxRerankTopK
)

// searchPlan is the effective parameter set of one search.
type searchPlan struct {
	Limit   int                   // 1..100
	Mode    iface.RetrievalMode   // never ""
	Fusion  iface.RetrievalFusion // effective; Candidates >= Limit
	RerankK int                   // 0 when the corpus has rerank disabled
	Fetch   int                   // first engine Query.Limit
}

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", iface.ErrRetrievalInvalidRequest, fmt.Sprintf(format, args...))
}

// planSearch validates the request shape (not the filter) and derives the
// effective parameters. Errors wrap iface.ErrRetrievalInvalidRequest.
func planSearch(req iface.RetrievalSearchRequest, spec iface.RetrievalCorpusSpec) (searchPlan, error) {
	var p searchPlan
	if req.Corpus == "" {
		return p, invalidRequest("corpus is required")
	}
	if strings.TrimSpace(req.Query) == "" {
		return p, invalidRequest("query is blank")
	}
	if utf8.RuneCountInString(req.Query) > MaxQueryRunes {
		return p, invalidRequest("query exceeds %d characters", MaxQueryRunes)
	}
	switch req.Mode {
	case "":
		p.Mode = iface.RetrievalModeHybrid
	case iface.RetrievalModeHybrid, iface.RetrievalModeDense, iface.RetrievalModeKeyword:
		p.Mode = req.Mode
	default:
		return p, invalidRequest("unknown mode %q", string(req.Mode))
	}
	p.Limit = req.Limit
	if p.Limit == 0 {
		p.Limit = DefaultSearchLimit
	}
	if p.Limit < 1 || p.Limit > MaxSearchLimit {
		return p, invalidRequest("limit must be within 1-%d", MaxSearchLimit)
	}

	// Fusion: request, then corpus spec, then module default, field by field.
	f := DefaultFusion
	if spec.Fusion.DenseWeight != 0 {
		f.DenseWeight = spec.Fusion.DenseWeight
	}
	if spec.Fusion.KeywordWeight != 0 {
		f.KeywordWeight = spec.Fusion.KeywordWeight
	}
	if spec.Fusion.Candidates != 0 {
		f.Candidates = spec.Fusion.Candidates
	}
	if o := req.Fusion; o != nil {
		if o.DenseWeight != 0 {
			if !validWeight(o.DenseWeight) {
				return p, invalidRequest("dense weight must satisfy 0 < w <= %d", maxWeight)
			}
			f.DenseWeight = o.DenseWeight
		}
		if o.KeywordWeight != 0 {
			if !validWeight(o.KeywordWeight) {
				return p, invalidRequest("keyword weight must satisfy 0 < w <= %d", maxWeight)
			}
			f.KeywordWeight = o.KeywordWeight
		}
		if o.Candidates != 0 {
			if o.Candidates < 1 || o.Candidates > maxCandidates {
				return p, invalidRequest("candidates must be within 1-%d", maxCandidates)
			}
			f.Candidates = o.Candidates
		}
	}
	f.Candidates = max(f.Candidates, p.Limit)
	p.Fusion = f

	if spec.Rerank.Enabled {
		k := spec.Rerank.TopK
		if k == 0 {
			k = DefaultRerankTopK
		}
		p.RerankK = min(MaxRerankTopK, max(k, p.Limit))
	}
	p.Fetch = min(f.Candidates, max(4*p.Limit, p.RerankK))
	return p, nil
}

// validWeight uses the positive form so NaN is refused.
func validWeight(w float64) bool { return w > 0 && w <= maxWeight }
