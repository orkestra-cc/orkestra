package services

import (
	"context"
	"errors"
	"slices"
	"time"

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
// tenant cannot starve the others, and processes each at once.
func (w *Worker) indexPass(ctx context.Context, gates map[string]*corpusGate, names []string, s WorkerSettings, rep *PassReport) error {
	if len(names) == 0 {
		return nil
	}
	tenants, err := w.Outbox.DueTenants(ctx, w.Now(), names, maxTenantsPerPass)
	if err != nil {
		return err
	}
	exhausted := make(map[string]bool, len(tenants))
	for rep.Claimed < s.Batch {
		progress := false
		for _, t := range tenants {
			if rep.Claimed == s.Batch {
				break
			}
			if exhausted[t] {
				continue
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
			w.process(ctx, gates[e.Corpus], e, s, rep)
		}
		if !progress {
			break
		}
	}
	return nil
}

// buildPass enqueues one Enumerate page per (corpus, tenant) with an open
// build, at most buildPagesPerPass pages per pass. It only enqueues: the
// index pass does the writes.
func (w *Worker) buildPass(ctx context.Context, gates map[string]*corpusGate, names []string, s WorkerSettings, rep *PassReport) error {
	if len(names) == 0 {
		return nil
	}
	tenants, err := w.Tenants.ListActiveTenantIDs(ctx, iface.TenantKindInternal)
	if err != nil {
		return err
	}
	pages := 0
	for _, name := range names { // names follow Catalog.Corpora(): sorted
		g := gates[name]
		for _, t := range tenants {
			if pages >= buildPagesPerPass {
				return nil
			}
			loaded, err := w.buildPage(ctx, g, t, s, rep)
			if err != nil {
				return err
			}
			if loaded {
				pages++
			}
		}
	}
	return nil
}

// buildPage claims the build row of one tenant and enqueues its next page.
// It reports whether an Enumerate page was requested.
func (w *Worker) buildPage(ctx context.Context, g *corpusGate, tenant string, s WorkerSettings, rep *PassReport) (bool, error) {
	corpus, gen := g.Spec.Name, g.State.ActiveGeneration
	tctx := tenantCtx(ctx, tenant)
	p, err := w.Progress.ClaimBuild(tctx, corpus, gen, w.owner, w.Now(), s.Lease)
	if err != nil {
		return false, err
	}
	if p == nil {
		return false, nil
	}
	advance := func(cursor string, added int64, done bool) error {
		err := w.Progress.Advance(tctx, corpus, gen, p.Epoch, w.owner, cursor, added, done, w.Now())
		if errors.Is(err, repository.ErrLeaseLost) {
			return nil // another replica took the build over
		}
		return err
	}

	ectx, cancel := context.WithTimeout(tctx, s.Lease/2)
	page, err := g.Spec.Source.Enumerate(ectx, tenant, p.Cursor, buildPageSize)
	cancel()
	if err != nil {
		w.Logger.Warn("retrieval: enumerate failed", "corpus", corpus, "tenant", tenant)
		return true, advance(p.Cursor, 0, false)
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
			return true, err
		}
	}
	rep.Enqueued += len(intents)
	return true, advance(page.Next, int64(len(intents)), page.Next == "")
}
