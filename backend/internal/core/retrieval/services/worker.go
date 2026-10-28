package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// CorpusStateStore is the corpus state persistence the worker needs;
// satisfied by *repository.CorpusStates.
type CorpusStateStore interface {
	Get(ctx context.Context, corpus string) (*models.CorpusState, error)
	Insert(ctx context.Context, st *models.CorpusState) error
	Save(ctx context.Context, st *models.CorpusState) error
}

// OutboxStore is the outbox the worker drains; satisfied by *repository.Outbox.
type OutboxStore interface {
	OutboxWriter
	DueTenants(ctx context.Context, now time.Time, corpora []string, limit int) ([]string, error)
	Claim(ctx context.Context, now time.Time, corpora []string, owner string, lease time.Duration) (*models.OutboxEntry, error)
	HoldsLease(ctx context.Context, corpus, sourceID string, epoch int64, owner string, now time.Time) (bool, error)
	Release(ctx context.Context, corpus, sourceID string, epoch int64, owner string, applied *models.AppliedState, now time.Time) error
	Reschedule(ctx context.Context, corpus, sourceID string, epoch int64, owner string, attempts int, next time.Time, code string) error
	Requeue(ctx context.Context, corpus, sourceID string, now time.Time) error
}

// ProgressStore is the per-tenant build progress; satisfied by
// *repository.TenantProgress.
type ProgressStore interface {
	ClaimBuild(ctx context.Context, corpus string, generation int, owner string, now time.Time, lease time.Duration) (*models.TenantProgress, error)
	Advance(ctx context.Context, corpus string, generation int, epoch int64, owner, cursor string, added int64, done bool, now time.Time) error
}

// TombstoneChecker answers which probed items an erasure covers; satisfied
// by *Tombstones.
type TombstoneChecker interface {
	Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error)
}

var (
	_ OutboxStore      = (*repository.Outbox)(nil)
	_ ProgressStore    = (*repository.TenantProgress)(nil)
	_ CorpusStateStore = (*repository.CorpusStates)(nil)
	_ TombstoneChecker = (*Tombstones)(nil)
)

// WorkerSettings are the installation's indexing knobs, clamped on every read.
type WorkerSettings struct {
	Lease        time.Duration
	Batch        int
	PollInterval time.Duration
}

const (
	MinWorkerLease = 30 * time.Second
	MaxWorkerLease = 15 * time.Minute
	// IndexingFencingWindow is how long a stale worker may still write after
	// losing its lease (consumed by erasure, PR 6).
	IndexingFencingWindow = 2 * MaxWorkerLease

	defaultWorkerLease = 2 * time.Minute
	defaultWorkerBatch = 32
	maxWorkerBatch     = 256
	defaultPollEvery   = 5 * time.Second
	minPollEvery       = time.Second
	maxPollEvery       = 5 * time.Minute
)

// ClampWorkerSettings applies defaults to unset values and clamps the rest.
func ClampWorkerSettings(s WorkerSettings) WorkerSettings {
	if s.Lease == 0 {
		s.Lease = defaultWorkerLease
	}
	s.Lease = min(max(s.Lease, MinWorkerLease), MaxWorkerLease)
	if s.Batch <= 0 {
		s.Batch = defaultWorkerBatch
	}
	s.Batch = min(s.Batch, maxWorkerBatch)
	if s.PollInterval == 0 {
		s.PollInterval = defaultPollEvery
	}
	s.PollInterval = min(max(s.PollInterval, minPollEvery), maxPollEvery)
	return s
}

// WorkerDeps are the index worker's collaborators.
type WorkerDeps struct {
	Outbox     OutboxStore
	States     CorpusStateStore
	Progress   ProgressStore
	Tombstones TombstoneChecker
	Catalog    CorpusCatalog
	Engine     engine.Engine
	Profiles   iface.LLMInferenceProfiles
	Embedder   iface.LLMPinnedEmbedder
	Modules    module.ModuleStatusReader
	Tenants    iface.TenantIDLister
	Prefix     string
	Settings   func() WorkerSettings // read every pass (hot reload), clamped
	Now        func() time.Time
	Logger     *slog.Logger
}

// Worker drains the outbox into the vector engine and builds generations.
type Worker struct {
	WorkerDeps
	owner string
}

// NewWorker builds a Worker with a fresh owner ID; it performs no I/O.
func NewWorker(d WorkerDeps) *Worker {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Settings == nil {
		d.Settings = func() WorkerSettings { return WorkerSettings{} }
	}
	return &Worker{WorkerDeps: d, owner: uuid.NewString()}
}

func (w *Worker) settings() WorkerSettings { return ClampWorkerSettings(w.Settings()) }
