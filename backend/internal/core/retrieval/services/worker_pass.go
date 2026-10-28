package services

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	maxTenantsPerPass = 64  // DueTenants limit
	buildPageSize     = 200 // ids per Enumerate call
	buildPagesPerPass = 16  // Enumerate pages loaded per pass, all corpora and tenants
)

// Problems returns a sorted copy of the last completed pass's problems.
func (w *Worker) Problems() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.problems)
}

// Run passes at once and then every PollInterval (re-read each time) until
// ctx is done.
func (w *Worker) Run(ctx context.Context) {
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			// Store errors are driver errors; they carry no item data.
			w.Logger.Warn("retrieval: index pass failed", "error", err.Error())
		}
		if w.afterPass != nil {
			w.afterPass()
		}
		timer := time.NewTimer(w.settings().PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// RunOnce is one worker pass: prepare every corpus, drain the outbox fairly,
// advance the initial builds. A store error aborts the pass.
func (w *Worker) RunOnce(ctx context.Context) (PassReport, error) {
	var rep PassReport
	s := w.settings()

	gates := map[string]*corpusGate{}
	var names, problems []string
	for _, spec := range w.Catalog.Corpora() {
		g, problem, err := w.prepareCorpus(ctx, spec)
		if err != nil {
			return rep, err
		}
		if problem != "" {
			problems = append(problems, problem)
		}
		if g != nil {
			gates[spec.Name] = g
			names = append(names, spec.Name)
		}
	}

	if err := w.indexPass(ctx, gates, names, s, &rep); err != nil {
		return rep, err
	}
	if err := w.buildPass(ctx, gates, names, s, &rep); err != nil {
		return rep, err
	}

	slices.Sort(problems)
	w.mu.Lock()
	w.problems = problems
	w.mu.Unlock()
	return rep, nil
}

// indexPass claims keys round-robin across the due tenants, so one busy
// tenant cannot starve the others, and processes each at once. The tenants
// come from a cursor over tenant IDs kept by this worker: a pass starts after
// the last tenant the previous pass served, so with more due tenants than
// worker_batch every tenant gets a claim within ceil(tenants/batch) passes.
func (w *Worker) indexPass(ctx context.Context, gates map[string]*corpusGate, names []string, s WorkerSettings, rep *PassReport) error {
	if len(names) == 0 {
		return nil
	}
	w.mu.Lock()
	after := w.tenantCursor
	w.mu.Unlock()
	tenants, err := w.Outbox.DueTenants(ctx, w.Now(), names, after, maxTenantsPerPass)
	if err == nil && len(tenants) == 0 && after != "" {
		// nothing due after the cursor: wrap and serve the head of the list
		after = ""
		tenants, err = w.Outbox.DueTenants(ctx, w.Now(), names, after, maxTenantsPerPass)
	}
	if err != nil {
		return err
	}

	visited := 0 // tenants offered a claim in the first round, a prefix of tenants
	last := -1   // index of the last tenant that received a claim
	exhausted := make(map[string]bool, len(tenants))
	for round := 0; rep.Claimed < s.Batch; round++ {
		progress := false
		for i, t := range tenants {
			if rep.Claimed == s.Batch {
				break
			}
			if exhausted[t] {
				continue
			}
			if round == 0 {
				visited = i + 1
			}
			e, err := w.Outbox.Claim(tenantCtx(ctx, t), w.Now(), names, w.owner, s.Lease)
			if err != nil {
				return err
			}
			if e == nil {
				exhausted[t] = true
				continue
			}
			rep.Claimed++
			progress = true
			last = max(last, i)
			w.process(ctx, gates[e.Corpus], e, s, rep)
		}
		if !progress {
			break
		}
	}

	switch {
	case len(tenants) < maxTenantsPerPass && visited == len(tenants):
		// every listed tenant was offered a claim and the list ended: wrap
		after = ""
	case last >= 0:
		after = tenants[last]
	case len(tenants) > 0:
		after = tenants[len(tenants)-1] // none claimable any more: move past them
	}
	w.mu.Lock()
	w.tenantCursor = after
	w.mu.Unlock()
	return nil
}

// buildPass enqueues one Enumerate page per (corpus, tenant) with an open
// build, at most buildPagesPerPass pages per pass. It only enqueues: the
// index pass does the writes. The pairs are walked in (corpus, tenant) order
// from where the previous pass stopped, so open builds share the page budget
// across passes; after an Enumerate failure the corpus's remaining tenants
// are skipped for the rest of the pass, so one broken source costs one call
// per pass and leaves the budget to the other corpora.
func (w *Worker) buildPass(ctx context.Context, gates map[string]*corpusGate, names []string, s WorkerSettings, rep *PassReport) error {
	if len(names) == 0 {
		return nil
	}
	tenants, err := w.Tenants.ListActiveTenantIDs(ctx, iface.TenantKindInternal)
	if err != nil {
		return err
	}
	tenants = slices.Sorted(slices.Values(tenants)) // a sorted copy: the lister owns its slice
	type pair struct {
		g      *corpusGate
		tenant string
	}
	pairs := make([]pair, 0, len(names)*len(tenants))
	for _, name := range names { // names follow Catalog.Corpora(): sorted
		for _, t := range tenants {
			pairs = append(pairs, pair{gates[name], t})
		}
	}
	if len(pairs) == 0 {
		return nil
	}

	w.mu.Lock()
	start := w.buildOffset % len(pairs)
	w.mu.Unlock()
	visited := 0
	defer func() {
		w.mu.Lock()
		w.buildOffset = (start + visited) % len(pairs)
		w.mu.Unlock()
	}()
	pages := 0
	failed := map[string]bool{}
	for visited < len(pairs) && pages < buildPagesPerPass {
		p := pairs[(start+visited)%len(pairs)]
		visited++
		if failed[p.g.Spec.Name] {
			continue
		}
		loaded, enumFailed, err := w.buildPage(ctx, p.g, p.tenant, s, rep)
		if err != nil {
			return err
		}
		if loaded {
			pages++
		}
		if enumFailed {
			failed[p.g.Spec.Name] = true
		}
	}
	return nil
}

// buildKey names the initial build of one tenant for one generation.
type buildKey struct {
	corpus     string
	generation int
	tenant     string
}

func (w *Worker) buildDone(k buildKey) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.built[k]
}

func (w *Worker) markBuilt(k buildKey) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.built[k] = true
}

// buildPage claims the build row of one tenant and enqueues its next page.
// It reports whether an Enumerate page was requested and whether that call
// failed. A build known to be done (finished here, or seen done once) costs
// no store call.
func (w *Worker) buildPage(ctx context.Context, g *corpusGate, tenant string, s WorkerSettings, rep *PassReport) (loaded, enumFailed bool, err error) {
	corpus, gen := g.Spec.Name, g.State.ActiveGeneration
	key := buildKey{corpus, gen, tenant}
	if w.buildDone(key) {
		return false, false, nil
	}
	tctx := tenantCtx(ctx, tenant)
	p, err := w.Progress.ClaimBuild(tctx, corpus, gen, w.owner, w.Now(), s.Lease)
	if err != nil {
		return false, false, err
	}
	if p == nil {
		// done, or held by another replica: only done is remembered
		cur, err := w.Progress.Get(tctx, corpus, gen)
		if err != nil {
			return false, false, err
		}
		if cur != nil && cur.Status == models.ProgressDone {
			w.markBuilt(key)
		}
		return false, false, nil
	}
	advance := func(cursor string, added int64, done bool) error {
		err := w.Progress.Advance(tctx, corpus, gen, p.Epoch, w.owner, cursor, added, done, w.Now())
		if errors.Is(err, repository.ErrLeaseLost) {
			return nil // another replica took the build over
		}
		if err == nil && done {
			w.markBuilt(key)
		}
		return err
	}

	ectx, cancel := context.WithTimeout(tctx, s.Lease/2)
	page, err := g.Spec.Source.Enumerate(ectx, tenant, p.Cursor, buildPageSize)
	cancel()
	if err != nil {
		w.Logger.Warn("retrieval: enumerate failed", "corpus", corpus, "tenant", tenant)
		return true, true, advance(p.Cursor, 0, false)
	}

	intents := make([]iface.RetrievalIntent, 0, len(page.Items))
	for _, it := range page.Items {
		in := iface.RetrievalIntent{Corpus: corpus, SourceID: it.SourceID, Revision: it.Revision}
		if ValidateIntent(in) == nil {
			intents = append(intents, in)
		}
	}
	if skipped := len(page.Items) - len(intents); skipped > 0 {
		w.Logger.Warn("retrieval: enumerate returned invalid ids", "corpus", corpus, "tenant", tenant, "skipped", skipped)
	}
	if len(intents) > 0 {
		if err := w.Outbox.Enqueue(tctx, intents, w.Now()); err != nil {
			return true, false, err
		}
	}
	rep.Enqueued += len(intents)
	return true, false, advance(page.Next, int64(len(intents)), page.Next == "")
}
