package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	scratchPrefix = "bench"
	scratchGen    = 1
	dropTimeout   = 30 * time.Second
	maxLimit      = 500 // engine.Query bound on Limit and max(PerArm, Limit)
)

var tagRE = regexp.MustCompile(`^[a-z0-9]{1,24}$`)

// DeclaredModels is the model identity the installation declares for the
// endpoints under test. Run compares each non-empty model (and revision) with
// what the endpoint reports on GET /info, so a benchmark never silently
// measures a different model than the declared one. Empty fields skip the
// comparison (endpoints given by URL only).
type DeclaredModels struct {
	EmbedModel, EmbedRevision   string
	RerankModel, RerankRevision string
}

// Config configures one benchmark run.
type Config struct {
	Engine engine.Engine
	Embed  *TEIClient
	Rerank *TEIClient // nil: the hybrid+rerank mode is skipped and reported as such

	// QueryPrefix and DocumentPrefix come from the declared embed endpoint and
	// are prepended to the embedded query and document text only; the indexed
	// and reranked text never carries them.
	QueryPrefix, DocumentPrefix string
	Declared                    DeclaredModels

	TenantID      string                  // default "t-bench"
	K             int                     // default 10
	PerArm        int                     // default 50
	RerankTopK    int                     // default 20
	Repeat        int                     // default 3 (latency samples per query and mode)
	BatchSize     int                     // default 32
	Analyzer      iface.RetrievalAnalyzer // default {Tokenizer: "multilingual"}
	CollectionTag string                  // ^[a-z0-9]{1,24}$; default base-36 UnixNano
}

func (c *Config) applyDefaults() {
	if c.TenantID == "" {
		c.TenantID = "t-bench"
	}
	if c.K == 0 {
		c.K = 10
	}
	if c.PerArm == 0 {
		c.PerArm = 50
	}
	if c.RerankTopK == 0 {
		c.RerankTopK = 20
	}
	if c.Repeat == 0 {
		c.Repeat = 3
	}
	if c.BatchSize == 0 {
		c.BatchSize = 32
	}
	if c.Analyzer == (iface.RetrievalAnalyzer{}) {
		c.Analyzer = iface.RetrievalAnalyzer{Tokenizer: "multilingual"}
	}
	if c.CollectionTag == "" {
		c.CollectionTag = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
}

func (c *Config) validate(fx *Fixture) error {
	switch {
	case c.Engine == nil:
		return errors.New("bench: no engine")
	case c.Embed == nil:
		return errors.New("bench: no embed client")
	case fx == nil || len(fx.Items) == 0 || len(fx.Queries) == 0:
		return errors.New("bench: fixture needs items and queries")
	case !tagRE.MatchString(c.CollectionTag):
		return fmt.Errorf("bench: collection tag %q must match %s", c.CollectionTag, tagRE)
	case c.K < 1 || c.PerArm < 1 || c.RerankTopK < 1 || c.Repeat < 1 || c.BatchSize < 1:
		return errors.New("bench: k, per-arm, rerank-top-k, repeat and batch size must be positive")
	case c.RerankTopK < c.K:
		return fmt.Errorf("bench: rerank-top-k %d is below k %d", c.RerankTopK, c.K)
	case max(c.PerArm, c.K, c.RerankTopK) > maxLimit:
		return fmt.Errorf("bench: k, per-arm and rerank-top-k must not exceed %d", maxLimit)
	}
	return nil
}

// checkDeclared compares what /info serves with the declared identity.
func checkDeclared(kind string, info TEIInfo, model, revision string) error {
	if model != "" && info.ModelID != model || revision != "" && info.ModelSHA != revision {
		return fmt.Errorf("%s endpoint serves %s@%s, declared %s@%s", kind, info.ModelID, info.ModelSHA, model, revision)
	}
	return nil
}

// session is one indexed scratch collection.
type session struct {
	cfg  Config
	fx   *Fixture
	coll string
	dims int
}

// documentText is the production chunk shape: title, blank line, text.
func documentText(it FixtureItem) string { return it.Title + "\n\n" + it.Text }

// index embeds every item and upserts one point per item.
func (s *session) index(ctx context.Context) error {
	items := s.fx.Items
	corpus := "scratch" + s.cfg.CollectionTag
	for start := 0; start < len(items); start += s.cfg.BatchSize {
		batch := items[start:min(start+s.cfg.BatchSize, len(items))]
		texts := make([]string, len(batch))
		inputs := make([]string, len(batch))
		for i, it := range batch {
			texts[i] = documentText(it)
			inputs[i] = s.cfg.DocumentPrefix + texts[i]
		}
		vecs, err := s.cfg.Embed.Embed(ctx, inputs)
		if err != nil {
			return fmt.Errorf("embed items: %w", err)
		}
		if s.dims == 0 {
			if len(vecs) == 0 || len(vecs[0]) == 0 {
				return errors.New("embed items: empty vector")
			}
			s.dims = len(vecs[0])
			if err := s.cfg.Engine.EnsureCollection(ctx, engine.CollectionSpec{
				Name: s.coll, Dimensions: s.dims, Analyzer: s.cfg.Analyzer,
			}); err != nil {
				return fmt.Errorf("ensure collection: %w", err)
			}
		}
		points := make([]engine.Point, len(batch))
		for i, it := range batch {
			if len(vecs[i]) != s.dims {
				return fmt.Errorf("embed items: item %q has %d dimensions, want %d", it.ID, len(vecs[i]), s.dims)
			}
			sum := sha256.Sum256([]byte(texts[i]))
			points[i] = engine.Point{
				ID:    engine.PointID(s.cfg.TenantID, corpus, it.ID, 0),
				Dense: vecs[i],
				Text:  texts[i],
				Runtime: engine.Runtime{
					TenantID: s.cfg.TenantID, SourceID: it.ID, SourceRevision: 1, ChunkID: it.ID + "#0",
					VisibleTo: it.VisibleTo, ContentHash: hex.EncodeToString(sum[:]),
					IngestionProfile: "bench", CharEnd: utf8.RuneCountInString(texts[i]),
				},
			}
		}
		if err := s.cfg.Engine.Upsert(ctx, s.coll, points); err != nil {
			return fmt.Errorf("upsert items: %w", err)
		}
	}
	return nil
}

// rank runs one query in one mode and returns the ranked candidates: at most
// K, or RerankTopK reordered by the reranker in hybrid+rerank mode. Its
// duration is the measured latency: query embedding, the engine and the
// rerank, nothing else.
func (s *session) rank(ctx context.Context, mode string, q FixtureQuery) ([]engine.Candidate, error) {
	eq := engine.Query{TenantID: s.cfg.TenantID, Principals: q.Principals, PerArm: s.cfg.PerArm, Limit: s.cfg.K}
	if mode == ModeHybridRerank {
		eq.Limit = s.cfg.RerankTopK
	}
	if mode != ModeDense {
		eq.Keyword = q.Text
		eq.Weights.Keyword = 1
	}
	if mode != ModeKeyword {
		vecs, err := s.cfg.Embed.Embed(ctx, []string{s.cfg.QueryPrefix + q.Text})
		if err != nil {
			return nil, fmt.Errorf("embed query %q: %w", q.ID, err)
		}
		if len(vecs) != 1 || len(vecs[0]) != s.dims {
			return nil, fmt.Errorf("embed query %q: unexpected vector", q.ID)
		}
		eq.Dense = vecs[0]
		eq.Weights.Dense = 1
	}
	cands, err := s.cfg.Engine.Query(ctx, s.coll, eq)
	if err != nil {
		return nil, fmt.Errorf("query %q (%s): %w", q.ID, mode, err)
	}
	if mode == ModeHybridRerank && len(cands) > 0 {
		texts := make([]string, len(cands))
		for i, c := range cands {
			texts[i] = c.Text
		}
		scores, err := s.cfg.Rerank.Rerank(ctx, q.Text, texts)
		if err != nil {
			return nil, fmt.Errorf("rerank query %q: %w", q.ID, err)
		}
		reordered := make([]engine.Candidate, 0, len(scores))
		for _, sc := range scores {
			if sc.Index < 0 || sc.Index >= len(cands) {
				return nil, fmt.Errorf("rerank query %q: index %d out of range", q.ID, sc.Index)
			}
			reordered = append(reordered, cands[sc.Index])
		}
		cands = reordered
	}
	return cands, nil
}

// account returns the top-K source ids of the candidates plus the number of
// candidates invisible to the query's principals. It runs outside the timed
// region: on a large fixture the bookkeeping must not inflate the latency.
func (s *session) account(cands []engine.Candidate, q FixtureQuery) ([]string, int) {
	violations := 0
	ids := make([]string, 0, min(len(cands), s.cfg.K))
	for _, c := range cands {
		if !s.fx.VisibleTo(c.Runtime.SourceID, q.Principals) {
			violations++
		}
		if len(ids) < s.cfg.K {
			ids = append(ids, c.Runtime.SourceID)
		}
	}
	return ids, violations
}

func (s *session) runMode(ctx context.Context, mode string) (ModeResult, error) {
	res := ModeResult{Mode: mode}
	var lat []time.Duration
	var recall, ndcg float64
	judged := 0
	for _, q := range s.fx.Queries {
		for rep := 0; rep < s.cfg.Repeat; rep++ {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			t0 := time.Now()
			cands, err := s.rank(ctx, mode, q)
			lat = append(lat, time.Since(t0))
			if err != nil {
				return res, err
			}
			ids, viol := s.account(cands, q)
			res.VisibilityViolations += viol
			if rep == 0 && len(q.Expected) > 0 {
				judged++
				recall += RecallAtK(ids, q.Expected, s.cfg.K)
				ndcg += NDCGAtK(ids, q.Expected, s.cfg.K)
			}
		}
	}
	if judged > 0 {
		res.RecallAtK, res.NDCGAtK = recall/float64(judged), ndcg/float64(judged)
	}
	res.P95, res.Samples = P95(lat), len(lat)
	return res, nil
}

// Run indexes the fixture into a scratch collection of its own, measures the
// four search modes and drops the collection on every exit path. A failed
// drop is joined to the run's error, so a leftover collection is reported
// even when the run itself failed; on an otherwise successful run it is
// returned together with the report.
func Run(ctx context.Context, cfg Config, fx *Fixture, fixtureName string) (rep *Report, err error) {
	cfg.applyDefaults()
	if err := cfg.validate(fx); err != nil {
		return nil, err
	}

	embedInfo, err := cfg.Embed.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("embed endpoint: %w", err)
	}
	if err := checkDeclared("embed", embedInfo, cfg.Declared.EmbedModel, cfg.Declared.EmbedRevision); err != nil {
		return nil, err
	}
	var rerankInfo TEIInfo
	if cfg.Rerank != nil {
		if rerankInfo, err = cfg.Rerank.Info(ctx); err != nil {
			return nil, fmt.Errorf("rerank endpoint: %w", err)
		}
		if err := checkDeclared("rerank", rerankInfo, cfg.Declared.RerankModel, cfg.Declared.RerankRevision); err != nil {
			return nil, err
		}
	}

	s := &session{cfg: cfg, fx: fx, coll: engine.CollectionName(scratchPrefix, "scratch"+cfg.CollectionTag, scratchGen)}
	// Registered before the collection exists: a failed EnsureCollection may
	// still have created it, and dropping a missing one is not an error.
	defer func() {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dropTimeout)
		defer cancel()
		if derr := cfg.Engine.DropCollection(dctx, s.coll); derr != nil {
			err = errors.Join(err, fmt.Errorf("drop scratch collection %s: %w", s.coll, derr))
		}
	}()

	t0 := time.Now()
	if err := s.index(ctx); err != nil {
		return nil, err
	}
	indexDur := time.Since(t0)

	rep = &Report{
		Fixture: fixtureName, Items: len(fx.Items), Queries: len(fx.Queries), K: cfg.K, Dimensions: s.dims,
		EmbedModel: embedInfo.ModelID, EmbedRevision: embedInfo.ModelSHA,
		RerankModel: rerankInfo.ModelID, RerankRevision: rerankInfo.ModelSHA,
		IndexDuration: indexDur,
	}
	if indexDur > 0 {
		rep.ItemsPerSecond = float64(len(fx.Items)) / indexDur.Seconds()
	}
	for _, mode := range []string{ModeKeyword, ModeDense, ModeHybrid, ModeHybridRerank} {
		if mode == ModeHybridRerank && cfg.Rerank == nil {
			rep.Modes = append(rep.Modes, ModeResult{Mode: mode, Skipped: "no rerank endpoint"})
			continue
		}
		mr, err := s.runMode(ctx, mode)
		if err != nil {
			return nil, err
		}
		rep.Modes = append(rep.Modes, mr)
	}
	rep.PeakRSSBytes = peakRSS()
	return rep, nil
}
