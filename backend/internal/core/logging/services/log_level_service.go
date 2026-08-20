// Package services holds the LogLevelService — the DB-backed
// LevelResolver that backs ADR-0005 Phase F. The service owns an
// atomic snapshot of (global, perModule) refreshed on every admin
// mutation; the slog handler reads it lock-free on every Enabled
// call.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkestra/backend/internal/core/logging/models"
	"github.com/orkestra/backend/internal/core/logging/repository"
)

// LevelResolver mirrors utils.LevelResolver so consumers can depend
// on this package without pulling shared/utils. Both interfaces have
// the same shape — Go's structural typing lets a *LogLevelService
// satisfy both without any glue.
type LevelResolver interface {
	Global() slog.Level
	LevelFor(module string) (slog.Level, bool)
}

// ErrConfigConflict is returned when an operator applies a permanent
// configuration based on an out-of-date snapshot.
var ErrConfigConflict = errors.New("logging configuration changed since it was loaded")

// ErrWriteConflict is returned after a bounded number of CAS misses. Callers
// can safely retry after loading a fresh snapshot; no uncommitted state was
// published locally.
var ErrWriteConflict = errors.New("logging configuration is being updated concurrently")

const maxCASAttempts = 4

// snapshot is the immutable value stored under the atomic.Pointer.
// Replaced wholesale on every mutation so readers never see a
// partially-updated state.
type snapshot struct {
	global            slog.Level
	perModule         map[string]slog.Level
	diagnostics       map[string]models.DiagnosticOverride
	revision          int64
	permanentRevision int64
	updatedAt         time.Time
	updatedBy         string
	persisted         bool
}

// LogLevelService owns the persisted log-level configuration and
// serves the LevelResolver contract for the slog handler.
//
// Concurrency model: snapshot is a *snapshot pointer behind an
// atomic.Pointer, so Global / LevelFor (called on every log line)
// stay lock-free. Mutations (admin endpoints) call DB.Upsert under
// a mutex so a refresh race can't lose a write, then publish a new
// snapshot. The mutex is touched only on admin writes — never on
// the hot read path.
type LogLevelService struct {
	repo     repository.Repository
	current  atomic.Pointer[snapshot]
	mu       sync.Mutex // serializes Upsert + snapshot publish
	logger   *slog.Logger
	envBoot  envBoot // captured at construction for "reset to env" semantics
	moduleCt moduleCatalog
	now      func() time.Time
}

// envBoot is the env-driven default the service falls back to when
// the DB document hasn't been seeded yet. Carries the same shape
// the StaticLevelResolver does at boot in shared/utils — so the
// behaviour is identical until an admin mutates the DB.
type envBoot struct {
	global slog.Level
	perMod map[string]slog.Level
}

// moduleCatalog is the list of module names the admin UI surfaces
// rows for. Populated at construction from the registered module
// set; the service does not enumerate Mongo collections to learn
// which modules exist.
type moduleCatalog struct {
	names []string
}

// NewLogLevelService builds the service with an explicit env-driven
// default and the catalog of module names the admin UI cares about.
// Pass logger=deps.Logger for boot diagnostics; pass moduleNames=
// list-of-registered-modules from the module registry. envGlobal /
// envPerModule capture the static-resolver values that were used
// during early boot so "reset to env" semantics work after admin
// edits.
func NewLogLevelService(repo repository.Repository, logger *slog.Logger, envGlobal slog.Level, envPerModule map[string]slog.Level, moduleNames []string) *LogLevelService {
	svc := &LogLevelService{
		repo:     repo,
		logger:   logger,
		envBoot:  envBoot{global: envGlobal, perMod: cloneLevelMap(envPerModule)},
		moduleCt: moduleCatalog{names: append([]string(nil), moduleNames...)},
		now:      time.Now,
	}
	// Seed snapshot from the env default. Load() below replaces it
	// with the persisted document if present.
	svc.publishSnapshot(envGlobal, envPerModule, nil, time.Time{}, "")
	return svc
}

// Load reads the persisted document and publishes a snapshot if found. It is
// the boot-time spelling of Refresh.
func (s *LogLevelService) Load(ctx context.Context) error {
	return s.Refresh(ctx)
}

// Refresh reconciles the lock-free local snapshot with Mongo authority. The
// local mutation mutex prevents an older read from publishing over a write in
// flight, while revision ordering prevents delayed refreshes from regressing
// state. Legacy documents use UpdatedAt as the tie-breaker at revision zero.
func (s *LogLevelService) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.repo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // env defaults stand
		}
		return err
	}
	cur := s.current.Load()
	if cur != nil && cur.persisted {
		if doc.Revision < cur.revision {
			return nil
		}
		if doc.Revision == cur.revision && !doc.UpdatedAt.After(cur.updatedAt) {
			return nil
		}
	}
	s.applyDoc(doc)
	return nil
}

// Global implements LevelResolver.
func (s *LogLevelService) Global() slog.Level {
	if snap := s.current.Load(); snap != nil {
		return snap.global
	}
	return slog.LevelInfo
}

// LevelFor implements LevelResolver.
func (s *LogLevelService) LevelFor(module string) (slog.Level, bool) {
	snap := s.current.Load()
	if snap == nil {
		return slog.LevelInfo, false
	}
	if diagnostic, ok := snap.diagnostics[module]; ok && diagnosticActive(diagnostic, s.now()) {
		return diagnostic.Level.Slog(), true
	}
	l, ok := snap.perModule[module]
	return l, ok
}

// ApplyPermanent atomically replaces the complete permanent configuration.
// Diagnostics remain unchanged. ExpectedPermanentRevision rejects only a
// genuinely stale permanent draft; a concurrent diagnostic write is retried.
func (s *LogLevelService) ApplyPermanent(ctx context.Context, input models.PermanentConfigInput, actor string) error {
	global, err := models.Parse(string(input.Global))
	if err != nil {
		return fmt.Errorf("global log level: %w", err)
	}
	perModule := make(map[string]slog.Level, len(input.PerModule))
	for module, level := range input.PerModule {
		if !s.moduleCt.contains(module) {
			return fmt.Errorf("unknown logging module %q", module)
		}
		parsed, err := models.Parse(string(level))
		if err != nil {
			return fmt.Errorf("log level for module %q: %w", module, err)
		}
		perModule[module] = parsed.Slog()
	}

	return s.mutate(ctx, actor, true, func(cur *snapshot) (bool, error) {
		if cur.permanentRevision != input.ExpectedPermanentRevision {
			return false, ErrConfigConflict
		}
		cur.global = global.Slog()
		cur.perModule = cloneLevelMap(perModule)
		return true, nil
	})
}

// SetGlobal updates the global threshold against the authoritative document.
func (s *LogLevelService) SetGlobal(ctx context.Context, level models.LogLevel, actor string) error {
	parsed, err := models.Parse(string(level))
	if err != nil {
		return err
	}
	return s.mutate(ctx, actor, true, func(cur *snapshot) (bool, error) {
		cur.global = parsed.Slog()
		return true, nil
	})
}

// SetModule sets a per-module override. Passing a level identical
// to the current global still persists the row — operators can use
// it to "pin" a module against future global changes.
func (s *LogLevelService) SetModule(ctx context.Context, module string, level models.LogLevel, actor string) error {
	if !s.moduleCt.contains(module) {
		return fmt.Errorf("unknown logging module %q", module)
	}
	parsed, err := models.Parse(string(level))
	if err != nil {
		return err
	}
	return s.mutate(ctx, actor, true, func(cur *snapshot) (bool, error) {
		cur.perModule[module] = parsed.Slog()
		return true, nil
	})
}

// UnsetModule removes a per-module override so the module falls
// back to Global. Idempotent — returns nil even when no override
// existed (the resulting state matches the request).
func (s *LogLevelService) UnsetModule(ctx context.Context, module string, actor string) error {
	return s.mutate(ctx, actor, true, func(cur *snapshot) (bool, error) {
		if _, ok := cur.perModule[module]; !ok {
			return false, nil
		}
		delete(cur.perModule, module)
		return true, nil
	})
}

// View renders the AdminView surface for the GET endpoint. Resolves
// the per-module effective level so the UI doesn't have to repeat
// the Global fallback logic.
func (s *LogLevelService) View() models.AdminView {
	snap := s.current.Load()
	view := models.AdminView{
		Global:            levelToModelLevel(snap.global),
		Diagnostics:       make([]models.AdminDiagnosticEntry, 0),
		Revision:          snap.revision,
		PermanentRevision: snap.permanentRevision,
	}
	if snap.updatedAt != (time.Time{}) {
		view.UpdatedAt = snap.updatedAt
	}
	view.UpdatedBy = snap.updatedBy

	now := s.now().UTC()
	view.ServerTime = now
	for _, name := range s.moduleCt.names {
		entry := models.AdminModuleEntry{Name: name}
		if l, ok := snap.perModule[name]; ok {
			override := levelToModelLevel(l)
			entry.Effective = override
			entry.Override = &override
			entry.HasOverride = true
		} else {
			entry.Effective = view.Global
		}
		if diagnostic, ok := snap.diagnostics[name]; ok && diagnosticActive(diagnostic, now) {
			entry.Effective = diagnostic.Level
			view.Diagnostics = append(view.Diagnostics, models.AdminDiagnosticEntry{
				Module:    name,
				Level:     diagnostic.Level,
				StartedAt: diagnostic.StartedAt,
				StartedBy: diagnostic.StartedBy,
				ExpiresAt: cloneTimePointer(diagnostic.ExpiresAt),
			})
		}
		view.Modules = append(view.Modules, entry)
	}
	return view
}

// ResetToEnv reverts both global and per-module to the env-driven
// snapshot captured at NewLogLevelService time. Persists the result
// so a restart sees the same state.
func (s *LogLevelService) ResetToEnv(ctx context.Context, actor string) error {
	return s.mutate(ctx, actor, true, func(cur *snapshot) (bool, error) {
		cur.global = s.envBoot.global
		cur.perModule = cloneLevelMap(s.envBoot.perMod)
		return true, nil
	})
}

// StartDiagnostic installs a temporary module override. The update is
// persisted before the immutable snapshot is published.
func (s *LogLevelService) StartDiagnostic(ctx context.Context, module string, level models.LogLevel, expiresAt *time.Time, actor string) error {
	if !s.moduleCt.contains(module) {
		return fmt.Errorf("unknown logging module %q", module)
	}

	parsed, err := models.Parse(string(level))
	if err != nil {
		return err
	}
	return s.mutate(ctx, actor, false, func(cur *snapshot) (bool, error) {
		cur.diagnostics[module] = models.DiagnosticOverride{
			Level:     parsed,
			StartedAt: s.now().UTC(),
			StartedBy: actor,
			ExpiresAt: cloneTimePointer(expiresAt),
		}
		return true, nil
	})
}

// StopDiagnostic removes a module diagnostic. Stopping a diagnostic that
// is already absent is a no-op.
func (s *LogLevelService) StopDiagnostic(ctx context.Context, module string, actor string) error {
	if !s.moduleCt.contains(module) {
		return fmt.Errorf("unknown logging module %q", module)
	}
	return s.mutate(ctx, actor, false, func(cur *snapshot) (bool, error) {
		if _, ok := cur.diagnostics[module]; !ok {
			return false, nil
		}
		delete(cur.diagnostics, module)
		return true, nil
	})
}

// CleanupExpired removes persisted diagnostics whose expiry has passed.
// Expiry correctness does not depend on this method: LevelFor and View
// ignore expired entries before cleanup runs.
func (s *LogLevelService) CleanupExpired(ctx context.Context) error {
	return s.mutate(ctx, "", false, func(cur *snapshot) (bool, error) {
		now := s.now().UTC()
		removed := false
		for module, diagnostic := range cur.diagnostics {
			if diagnostic.ExpiresAt != nil && !now.Before(*diagnostic.ExpiresAt) {
				delete(cur.diagnostics, module)
				removed = true
			}
		}
		return removed, nil
	})
}

// ---- internal helpers ----

// mutate executes a read-modify-CAS cycle under the replica-local mutex. Mongo
// remains authoritative on every attempt. A CAS miss is retried from a fresh
// document so unrelated writes are preserved instead of replaying stale local
// state. permanent controls only the permanent editor token.
func (s *LogLevelService) mutate(
	ctx context.Context,
	actor string,
	permanent bool,
	change func(*snapshot) (bool, error),
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		cur, err := s.authoritativeSnapshot(ctx)
		if err != nil {
			return err
		}
		expectedRevision := cur.revision
		changed, err := change(cur)
		if err != nil {
			return err
		}
		if !changed {
			s.current.Store(cloneSnapshot(cur))
			return nil
		}

		cur.revision++
		if permanent {
			cur.permanentRevision++
		}
		cur.updatedAt = s.now().UTC()
		if actor != "" {
			cur.updatedBy = actor
		}
		cur.persisted = true

		won, err := s.repo.CompareAndSwap(ctx, expectedRevision, snapshotToDoc(cur))
		if err != nil {
			return err
		}
		if won {
			s.current.Store(cloneSnapshot(cur))
			return nil
		}
	}
	return ErrWriteConflict
}

func (s *LogLevelService) authoritativeSnapshot(ctx context.Context) (*snapshot, error) {
	doc, err := s.repo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return &snapshot{
				global:      s.envBoot.global,
				perModule:   cloneLevelMap(s.envBoot.perMod),
				diagnostics: cloneDiagnosticMap(nil),
			}, nil
		}
		return nil, err
	}
	return snapshotFromDoc(doc), nil
}

func (s *LogLevelService) applyDoc(doc *models.LogLevelDoc) {
	s.current.Store(snapshotFromDoc(doc))
}

func snapshotFromDoc(doc *models.LogLevelDoc) *snapshot {
	perMod := map[string]slog.Level{}
	for k, v := range doc.PerModule {
		perMod[k] = v.Slog()
	}
	return &snapshot{
		global:            doc.Global.Slog(),
		perModule:         perMod,
		diagnostics:       cloneDiagnosticMap(doc.Diagnostics),
		revision:          doc.Revision,
		permanentRevision: doc.PermanentRevision,
		updatedAt:         doc.UpdatedAt,
		updatedBy:         doc.UpdatedBy,
		persisted:         true,
	}
}

func snapshotToDoc(snap *snapshot) *models.LogLevelDoc {
	return &models.LogLevelDoc{
		ConfigKey:         models.DefaultConfigKey,
		Global:            levelToModelLevel(snap.global),
		PerModule:         levelMapToModelMap(snap.perModule),
		Diagnostics:       cloneDiagnosticMap(snap.diagnostics),
		Revision:          snap.revision,
		PermanentRevision: snap.permanentRevision,
		UpdatedAt:         snap.updatedAt,
		UpdatedBy:         snap.updatedBy,
	}
}

func (s *LogLevelService) publishSnapshot(global slog.Level, perModule map[string]slog.Level, diagnostics map[string]models.DiagnosticOverride, at time.Time, by string) {
	snap := &snapshot{
		global:      global,
		perModule:   cloneLevelMap(perModule),
		diagnostics: cloneDiagnosticMap(diagnostics),
		updatedAt:   at,
		updatedBy:   by,
		persisted:   false,
	}
	s.current.Store(snap)
}

func (c moduleCatalog) contains(module string) bool {
	for _, name := range c.names {
		if name == module {
			return true
		}
	}
	return false
}

func diagnosticActive(diagnostic models.DiagnosticOverride, now time.Time) bool {
	return diagnostic.ExpiresAt == nil || now.Before(*diagnostic.ExpiresAt)
}

func cloneSnapshot(in *snapshot) *snapshot {
	return &snapshot{
		global:            in.global,
		perModule:         cloneLevelMap(in.perModule),
		diagnostics:       cloneDiagnosticMap(in.diagnostics),
		revision:          in.revision,
		permanentRevision: in.permanentRevision,
		updatedAt:         in.updatedAt,
		updatedBy:         in.updatedBy,
		persisted:         in.persisted,
	}
}

func cloneLevelMap(in map[string]slog.Level) map[string]slog.Level {
	if in == nil {
		return map[string]slog.Level{}
	}
	out := make(map[string]slog.Level, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneDiagnosticMap(in map[string]models.DiagnosticOverride) map[string]models.DiagnosticOverride {
	out := make(map[string]models.DiagnosticOverride, len(in))
	for module, diagnostic := range in {
		diagnostic.ExpiresAt = cloneTimePointer(diagnostic.ExpiresAt)
		out[module] = diagnostic
	}
	return out
}

func cloneTimePointer(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	clone := *in
	return &clone
}

func levelToModelLevel(l slog.Level) models.LogLevel {
	switch {
	case l <= slog.LevelDebug:
		return models.LogLevelDebug
	case l <= slog.LevelInfo:
		return models.LogLevelInfo
	case l <= slog.LevelWarn:
		return models.LogLevelWarn
	default:
		return models.LogLevelError
	}
}

func levelMapToModelMap(in map[string]slog.Level) map[string]models.LogLevel {
	out := make(map[string]models.LogLevel, len(in))
	for k, v := range in {
		out[k] = levelToModelLevel(v)
	}
	return out
}
