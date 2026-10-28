package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// DefaultFusion holds the module's fusion defaults; zero fields of a spec take
// these values.
var DefaultFusion = iface.RetrievalFusion{DenseWeight: 1, KeywordWeight: 1, Candidates: 50}

// DefaultRerankTopK is the rerank depth used when a spec leaves TopK at zero.
const DefaultRerankTopK = 20

const (
	maxFilters      = 32
	maxDescription  = 500
	maxEndpointName = 64
	maxWeight       = 10
	maxCandidates   = 500
	maxRerankTopK   = 200
	maxChunkTokens  = 8192
	stepThree       = "available from delivery step 3"
)

var (
	corpusNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	ownerRe       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	permissionRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,3}$`)
	filterNameRe  = regexp.MustCompile(`^[a-z][a-zA-Z0-9_]{0,63}$`)
	contribNameRe = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
)

// snowballLanguages is Qdrant v1.20's SnowballLanguage enum.
var snowballLanguages = setOf("arabic armenian danish dutch english finnish french german greek hungarian italian norwegian portuguese romanian russian spanish swedish tamil turkish")

// stopWordLanguages is Qdrant v1.20's Language enum (stop-word lists).
var stopWordLanguages = setOf("arabic azerbaijani basque bengali catalan chinese danish dutch english finnish french german greek hebrew hinglish hungarian indonesian italian japanese kazakh nepali norwegian portuguese romanian russian slovene spanish swedish tajik turkish")

var filterTypes = map[iface.RetrievalFieldType]struct{}{
	iface.RetrievalFieldKeyword:  {},
	iface.RetrievalFieldKeywords: {},
	iface.RetrievalFieldInteger:  {},
	iface.RetrievalFieldFloat:    {},
	iface.RetrievalFieldBool:     {},
	iface.RetrievalFieldDatetime: {},
}

func setOf(words string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, w := range strings.Fields(words) {
		m[w] = struct{}{}
	}
	return m
}

func specErr(name, format string, args ...any) error {
	return fmt.Errorf("retrieval: corpus %q: %s: %w", name, fmt.Sprintf(format, args...), iface.ErrRetrievalInvalidSpec)
}

func closedErr() error {
	return fmt.Errorf("retrieval: registration is closed: %w", iface.ErrRetrievalInvalidSpec)
}

// RegisterCorpus validates, normalizes and stores a corpus declaration. It is
// syntax-only and never contacts the engine or llm.
func (s *Service) RegisterCorpus(spec iface.RetrievalCorpusSpec) error {
	norm, err := normalizeSpec(spec)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return specErr(spec.Name, "registration is closed")
	}
	if _, dup := s.corpora[norm.Name]; dup {
		return specErr(norm.Name, "already registered")
	}
	s.corpora[norm.Name] = norm
	return nil
}

// RegisterPrincipalContributor adds a contributor of principal strings.
func (s *Service) RegisterPrincipalContributor(c iface.RetrievalPrincipalContributor) error {
	if c == nil {
		return fmt.Errorf("retrieval: principal contributor is nil: %w", iface.ErrRetrievalInvalidSpec)
	}
	name := c.Name()
	if !contribNameRe.MatchString(name) {
		return fmt.Errorf("retrieval: principal contributor %q: name must match %s: %w", name, contribNameRe, iface.ErrRetrievalInvalidSpec)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return closedErr()
	}
	if _, dup := s.contribNames[name]; dup {
		return fmt.Errorf("retrieval: principal contributor %q: already registered: %w", name, iface.ErrRetrievalInvalidSpec)
	}
	s.contribNames[name] = struct{}{}
	s.contributors = append(s.contributors, c)
	return nil
}

// Corpus returns a normalized copy of the named corpus.
func (s *Service) Corpus(name string) (iface.RetrievalCorpusSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.corpora[name]
	if !ok {
		return iface.RetrievalCorpusSpec{}, false
	}
	return cloneSpec(spec), true
}

// Corpora returns copies of every corpus, sorted by name.
func (s *Service) Corpora() []iface.RetrievalCorpusSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]iface.RetrievalCorpusSpec, 0, len(s.corpora))
	for _, spec := range s.corpora {
		out = append(out, cloneSpec(spec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PrincipalContributors returns the contributors in registration order.
func (s *Service) PrincipalContributors() []iface.RetrievalPrincipalContributor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]iface.RetrievalPrincipalContributor(nil), s.contributors...)
}

// FilterSchema returns a copy of the corpus's declared filter fields.
func (s *Service) FilterSchema(corpus string) ([]iface.RetrievalFilterField, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.corpora[corpus]
	if !ok {
		return nil, fmt.Errorf("retrieval: corpus %q: %w", corpus, iface.ErrRetrievalUnknownCorpus)
	}
	return append([]iface.RetrievalFilterField(nil), spec.Filters...), nil
}

func cloneSpec(spec iface.RetrievalCorpusSpec) iface.RetrievalCorpusSpec {
	spec.Filters = append([]iface.RetrievalFilterField(nil), spec.Filters...)
	return spec
}

func normalizeSpec(in iface.RetrievalCorpusSpec) (iface.RetrievalCorpusSpec, error) {
	spec := cloneSpec(in)
	name := spec.Name
	switch {
	case !corpusNameRe.MatchString(name):
		return spec, specErr(name, "name must match %s", corpusNameRe)
	case !ownerRe.MatchString(spec.Owner):
		return spec, specErr(name, "owner must match %s", ownerRe)
	case !permissionRe.MatchString(spec.Permission):
		return spec, specErr(name, "permission must match %s", permissionRe)
	case spec.Source == nil:
		return spec, specErr(name, "source is required")
	}
	if err := checkFilters(name, spec.Filters); err != nil {
		return spec, err
	}
	if err := normalizeIngestion(name, &spec.Ingestion); err != nil {
		return spec, err
	}
	switch spec.Locality {
	case "":
		spec.Locality = iface.RetrievalLocalOnly
	case iface.RetrievalLocalOnly, iface.RetrievalHostedAllowed:
	default:
		return spec, specErr(name, "locality %q is not local_only or hosted_allowed", spec.Locality)
	}
	if err := normalizeFusion(name, &spec.Fusion); err != nil {
		return spec, err
	}
	return spec, normalizeRerank(name, &spec.Rerank)
}

func checkFilters(name string, filters []iface.RetrievalFilterField) error {
	if len(filters) > maxFilters {
		return specErr(name, "at most %d filters", maxFilters)
	}
	seen := map[string]struct{}{}
	for _, f := range filters {
		if !filterNameRe.MatchString(f.Name) {
			return specErr(name, "filter name %q must match %s", f.Name, filterNameRe)
		}
		if _, dup := seen[f.Name]; dup {
			return specErr(name, "duplicate filter %q", f.Name)
		}
		seen[f.Name] = struct{}{}
		if _, ok := filterTypes[f.Type]; !ok {
			return specErr(name, "filter %q: unknown type %q", f.Name, f.Type)
		}
		if d := len(strings.TrimSpace(f.Description)); d < 1 || d > maxDescription {
			return specErr(name, "filter %q: description must be 1..%d bytes", f.Name, maxDescription)
		}
	}
	return nil
}

func normalizeIngestion(name string, in *iface.RetrievalIngestionProfile) error {
	ep := strings.TrimSpace(in.EmbedEndpoint)
	if ep == "" || len(in.EmbedEndpoint) > maxEndpointName || strings.ContainsAny(in.EmbedEndpoint, " \t\r\n\v\f") || ep != in.EmbedEndpoint {
		return specErr(name, "embedEndpoint must be a non-empty name of at most %d bytes without whitespace", maxEndpointName)
	}
	a := &in.Analyzer
	switch a.Tokenizer {
	case "":
		a.Tokenizer = "word"
	case "word", "multilingual":
	default:
		return specErr(name, "tokenizer %q is not word or multilingual", a.Tokenizer)
	}
	if a.Stemming {
		if _, ok := snowballLanguages[a.Language]; !ok {
			return specErr(name, "stemming needs a snowball language, got %q", a.Language)
		}
	}
	if a.StopWords {
		if _, ok := stopWordLanguages[a.Language]; !ok {
			return specErr(name, "stopWords needs a stop-word language, got %q", a.Language)
		}
	}
	c := in.Chunking
	switch c.Strategy {
	case "none":
		if c.MaxTokens != 0 || c.Overlap != 0 {
			return specErr(name, "chunking none takes no maxTokens or overlap")
		}
	case "fixed":
		if c.MaxTokens < 1 || c.MaxTokens > maxChunkTokens {
			return specErr(name, "chunking maxTokens must be 1..%d", maxChunkTokens)
		}
		if c.Overlap < 0 || c.Overlap >= c.MaxTokens {
			return specErr(name, "chunking overlap must be >= 0 and < maxTokens")
		}
	case "hierarchical":
		return specErr(name, "chunking hierarchical is %s", stepThree)
	default:
		return specErr(name, "chunking strategy %q is not none or fixed", c.Strategy)
	}
	if in.Enrichment != nil {
		return specErr(name, "enrichment is %s", stepThree)
	}
	return nil
}

func normalizeFusion(name string, f *iface.RetrievalFusion) error {
	if f.DenseWeight == 0 {
		f.DenseWeight = DefaultFusion.DenseWeight
	}
	if f.KeywordWeight == 0 {
		f.KeywordWeight = DefaultFusion.KeywordWeight
	}
	if f.Candidates == 0 {
		f.Candidates = DefaultFusion.Candidates
	}
	// NaN fails every comparison, so the positive form is used.
	if !(f.DenseWeight > 0 && f.DenseWeight <= maxWeight) || !(f.KeywordWeight > 0 && f.KeywordWeight <= maxWeight) {
		return specErr(name, "fusion weights must be in (0, %d]", maxWeight)
	}
	if f.Candidates < 1 || f.Candidates > maxCandidates {
		return specErr(name, "fusion candidates must be 1..%d", maxCandidates)
	}
	return nil
}

func normalizeRerank(name string, r *iface.RetrievalRerank) error {
	if r.TopK == 0 {
		r.TopK = DefaultRerankTopK
	}
	if r.TopK < 1 || r.TopK > maxRerankTopK {
		return specErr(name, "rerank topK must be 1..%d", maxRerankTopK)
	}
	if r.Enabled && strings.TrimSpace(r.Endpoint) == "" {
		return specErr(name, "rerank enabled needs an endpoint")
	}
	return nil
}
