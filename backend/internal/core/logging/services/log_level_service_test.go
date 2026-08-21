package services

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/logging/models"
	"github.com/orkestra/backend/internal/core/logging/repository"
)

// fakeRepo is an in-memory Repository for unit tests. Wraps a mutex
// around the single doc since concurrent SetGlobal/SetModule paths
// would otherwise race in -race mode.
type fakeRepo struct {
	mu      sync.Mutex
	doc     *models.LogLevelDoc
	err     error // injectable CAS failure for failure-path tests
	getErr  error
	misses  int // injectable CAS misses before success
	upserts int
	calls   int
}

func (r *fakeRepo) Get(_ context.Context) (*models.LogLevelDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.doc == nil {
		return nil, repository.ErrNotFound
	}
	return cloneLogLevelDoc(r.doc), nil
}

func (r *fakeRepo) CompareAndSwap(_ context.Context, expectedRevision int64, doc *models.LogLevelDoc) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return false, r.err
	}
	if r.misses > 0 {
		r.misses--
		return false, nil
	}
	currentRevision := int64(0)
	if r.doc != nil {
		currentRevision = r.doc.Revision
	}
	if currentRevision != expectedRevision {
		return false, nil
	}
	r.doc = cloneLogLevelDoc(doc)
	r.upserts++
	return true, nil
}

func cloneLogLevelDoc(doc *models.LogLevelDoc) *models.LogLevelDoc {
	clone := *doc
	clone.PerModule = make(map[string]models.LogLevel, len(doc.PerModule))
	for module, level := range doc.PerModule {
		clone.PerModule[module] = level
	}
	clone.Diagnostics = make(map[string]models.DiagnosticOverride, len(doc.Diagnostics))
	for module, diagnostic := range doc.Diagnostics {
		if diagnostic.ExpiresAt != nil {
			expiresAt := *diagnostic.ExpiresAt
			diagnostic.ExpiresAt = &expiresAt
		}
		clone.Diagnostics[module] = diagnostic
	}
	return &clone
}

func newTestSvc(t *testing.T) (*LogLevelService, *fakeRepo) {
	t.Helper()
	repo := &fakeRepo{}
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := NewLogLevelService(repo, logger, slog.LevelInfo, map[string]slog.Level{
		"rag": slog.LevelDebug,
	}, []string{"rag", "billing", "auth"})
	return svc, repo
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func TestLogLevelService_EnvDefaultsWhenNothingPersisted(t *testing.T) {
	svc, _ := newTestSvc(t)

	if got := svc.Global(); got != slog.LevelInfo {
		t.Errorf("Global = %v, want info", got)
	}
	if l, ok := svc.LevelFor("rag"); !ok || l != slog.LevelDebug {
		t.Errorf("LevelFor(rag) = %v,%v want debug,true", l, ok)
	}
	if _, ok := svc.LevelFor("billing"); ok {
		t.Errorf("LevelFor(billing) returned ok=true without an env override")
	}
}

func TestLogLevelService_LoadFromDB(t *testing.T) {
	svc, repo := newTestSvc(t)

	repo.doc = &models.LogLevelDoc{
		ConfigKey: models.DefaultConfigKey,
		Global:    models.LogLevelWarn,
		PerModule: map[string]models.LogLevel{"billing": models.LogLevelError},
	}

	if err := svc.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := svc.Global(); got != slog.LevelWarn {
		t.Errorf("Global = %v, want warn", got)
	}
	if l, ok := svc.LevelFor("billing"); !ok || l != slog.LevelError {
		t.Errorf("LevelFor(billing) = %v,%v want error,true", l, ok)
	}
	// rag was in the env seed but the persisted doc didn't include it
	// — Load REPLACES the snapshot wholesale; rag override is gone.
	if _, ok := svc.LevelFor("rag"); ok {
		t.Errorf("LevelFor(rag) should NOT be set after a Load that doesn't include it")
	}
}

func TestLogLevelService_SetGlobalPersistsAndPublishes(t *testing.T) {
	svc, repo := newTestSvc(t)

	if err := svc.SetGlobal(context.Background(), models.LogLevelError, "test-user"); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	// In-memory snapshot updated.
	if got := svc.Global(); got != slog.LevelError {
		t.Errorf("Global = %v, want error", got)
	}
	// Persisted.
	if repo.doc == nil || repo.doc.Global != models.LogLevelError {
		t.Errorf("repo doc not persisted: %+v", repo.doc)
	}
	if repo.doc.UpdatedBy != "test-user" {
		t.Errorf("UpdatedBy = %q, want test-user", repo.doc.UpdatedBy)
	}
}

func TestLogLevelService_SetModule_AddAndUnset(t *testing.T) {
	svc, repo := newTestSvc(t)

	if err := svc.SetModule(context.Background(), "billing", models.LogLevelWarn, "u"); err != nil {
		t.Fatalf("SetModule: %v", err)
	}
	if l, ok := svc.LevelFor("billing"); !ok || l != slog.LevelWarn {
		t.Errorf("billing override missing")
	}
	if repo.doc.PerModule["billing"] != models.LogLevelWarn {
		t.Errorf("billing not persisted: %+v", repo.doc.PerModule)
	}

	if err := svc.UnsetModule(context.Background(), "billing", "u"); err != nil {
		t.Fatalf("UnsetModule: %v", err)
	}
	if _, ok := svc.LevelFor("billing"); ok {
		t.Errorf("billing override should have been removed")
	}
	if _, persisted := repo.doc.PerModule["billing"]; persisted {
		t.Errorf("billing should be absent from persisted doc")
	}
}

func TestLogLevelService_UnsetModule_Idempotent(t *testing.T) {
	svc, _ := newTestSvc(t)
	if err := svc.UnsetModule(context.Background(), "nonexistent", "u"); err != nil {
		t.Errorf("UnsetModule for missing key should be a no-op, got %v", err)
	}
}

func TestLogLevelService_View(t *testing.T) {
	svc, _ := newTestSvc(t)
	_ = svc.SetModule(context.Background(), "billing", models.LogLevelError, "u")
	_ = svc.StartDiagnostic(context.Background(), "billing", models.LogLevelDebug, nil, "u")

	view := svc.View()
	if view.Global != models.LogLevelInfo {
		t.Errorf("view.Global = %v, want info", view.Global)
	}
	if len(view.Modules) != 3 {
		t.Errorf("expected 3 module rows, got %d", len(view.Modules))
	}
	if view.Diagnostics == nil {
		t.Error("view.Diagnostics is nil, want an empty JSON array")
	}

	byName := map[string]models.AdminModuleEntry{}
	for _, m := range view.Modules {
		byName[m.Name] = m
	}
	if e := byName["rag"]; !e.HasOverride || e.Effective != models.LogLevelDebug {
		t.Errorf("rag entry = %+v", e)
	}
	if e := byName["billing"]; !e.HasOverride || e.Override == nil || *e.Override != models.LogLevelError || e.Effective != models.LogLevelDebug {
		t.Errorf("billing entry = %+v, want permanent error and effective diagnostic debug", e)
	}
	if e := byName["auth"]; e.HasOverride || e.Override != nil || e.Effective != models.LogLevelInfo {
		t.Errorf("auth should inherit Global without override, got %+v", e)
	}
}

func TestLogLevelService_ResetToEnv(t *testing.T) {
	svc, _ := newTestSvc(t)
	_ = svc.SetGlobal(context.Background(), models.LogLevelError, "u")
	_ = svc.SetModule(context.Background(), "billing", models.LogLevelWarn, "u")

	if err := svc.ResetToEnv(context.Background(), "u"); err != nil {
		t.Fatalf("ResetToEnv: %v", err)
	}
	if got := svc.Global(); got != slog.LevelInfo {
		t.Errorf("Global after reset = %v, want info", got)
	}
	if _, ok := svc.LevelFor("billing"); ok {
		t.Errorf("billing override should be gone after reset")
	}
	if l, ok := svc.LevelFor("rag"); !ok || l != slog.LevelDebug {
		t.Errorf("rag env seed should be restored after reset")
	}
}

func TestLogLevelService_PersistFailurePreservesSnapshot(t *testing.T) {
	svc, repo := newTestSvc(t)
	// Capture pre-mutation state.
	preGlobal := svc.Global()

	repo.err = errors.New("boom")
	err := svc.SetGlobal(context.Background(), models.LogLevelError, "u")
	if err == nil {
		t.Fatalf("expected error from broken repo")
	}
	// In-memory snapshot must NOT advance on persist failure.
	if svc.Global() != preGlobal {
		t.Errorf("snapshot updated despite persist failure: %v -> %v", preGlobal, svc.Global())
	}
}

func TestLogLevelService_ApplyPermanent(t *testing.T) {
	ctx := context.Background()
	baseTime := time.Date(2026, 8, 20, 12, 0, 0, 123456000, time.UTC)

	setup := func(t *testing.T) (*LogLevelService, *fakeRepo, *time.Time) {
		t.Helper()
		svc, repo := newTestSvc(t)
		now := baseTime
		svc.now = func() time.Time { return now }
		expiresAt := baseTime.Add(time.Hour)
		if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, &expiresAt, "diagnostic-operator"); err != nil {
			t.Fatalf("StartDiagnostic: %v", err)
		}
		return svc, repo, &now
	}

	t.Run("replaces the complete permanent configuration with one write and preserves diagnostics", func(t *testing.T) {
		svc, repo, now := setup(t)
		expectedPermanentRevision := svc.View().PermanentRevision
		*now = baseTime.Add(time.Minute)
		beforeCalls := repo.calls

		err := svc.ApplyPermanent(ctx, models.PermanentConfigInput{
			Global: models.LogLevelError,
			PerModule: map[string]models.LogLevel{
				"auth":    models.LogLevelWarn,
				"billing": models.LogLevelDebug,
			},
			ExpectedPermanentRevision: expectedPermanentRevision,
		}, "config-operator")
		if err != nil {
			t.Fatalf("ApplyPermanent: %v", err)
		}
		if repo.calls != beforeCalls+1 {
			t.Fatalf("repository calls = %d, want %d", repo.calls, beforeCalls+1)
		}
		if repo.doc.Global != models.LogLevelError {
			t.Errorf("persisted global = %q, want error", repo.doc.Global)
		}
		if got := svc.Global(); got != slog.LevelError {
			t.Errorf("published global = %v, want error", got)
		}
		wantPerModule := map[string]models.LogLevel{
			"auth":    models.LogLevelWarn,
			"billing": models.LogLevelDebug,
		}
		if !reflect.DeepEqual(repo.doc.PerModule, wantPerModule) {
			t.Errorf("persisted per-module = %#v, want %#v", repo.doc.PerModule, wantPerModule)
		}
		if repo.doc.UpdatedAt != now.UTC() || repo.doc.UpdatedBy != "config-operator" {
			t.Errorf("persisted metadata = %v,%q, want %v,%q", repo.doc.UpdatedAt, repo.doc.UpdatedBy, now.UTC(), "config-operator")
		}
		view := svc.View()
		if view.UpdatedAt != now.UTC() || view.UpdatedBy != "config-operator" {
			t.Errorf("published metadata = %v,%q, want %v,%q", view.UpdatedAt, view.UpdatedBy, now.UTC(), "config-operator")
		}
		diagnostic, ok := repo.doc.Diagnostics["auth"]
		if !ok || diagnostic.Level != models.LogLevelDebug || diagnostic.StartedBy != "diagnostic-operator" {
			t.Fatalf("persisted diagnostic = %+v,%v, want preserved auth diagnostic", diagnostic, ok)
		}
		if level, explicit := svc.LevelFor("auth"); level != slog.LevelDebug || !explicit {
			t.Errorf("active LevelFor(auth) = %v,%v, want debug,true", level, explicit)
		}
		*now = baseTime.Add(time.Hour)
		if level, explicit := svc.LevelFor("auth"); level != slog.LevelWarn || !explicit {
			t.Errorf("expired LevelFor(auth) = %v,%v, want permanent warn,true", level, explicit)
		}
		if _, explicit := svc.LevelFor("rag"); explicit {
			t.Error("rag env override survived complete permanent replacement")
		}
	})

	tests := []struct {
		name      string
		input     func(int64) models.PermanentConfigInput
		wantError error
		persist   bool
	}{
		{
			name: "rejects an unknown module before persistence",
			input: func(permanentRevision int64) models.PermanentConfigInput {
				return models.PermanentConfigInput{
					Global:                    models.LogLevelInfo,
					PerModule:                 map[string]models.LogLevel{"unknown": models.LogLevelDebug},
					ExpectedPermanentRevision: permanentRevision,
				}
			},
		},
		{
			name: "rejects an invalid global level before persistence",
			input: func(permanentRevision int64) models.PermanentConfigInput {
				return models.PermanentConfigInput{
					Global:                    models.LogLevel("trace"),
					PerModule:                 map[string]models.LogLevel{"auth": models.LogLevelInfo},
					ExpectedPermanentRevision: permanentRevision,
				}
			},
			wantError: models.ErrInvalidLogLevel,
		},
		{
			name: "rejects an invalid module level before persistence",
			input: func(permanentRevision int64) models.PermanentConfigInput {
				return models.PermanentConfigInput{
					Global:                    models.LogLevelInfo,
					PerModule:                 map[string]models.LogLevel{"auth": models.LogLevel("verbose")},
					ExpectedPermanentRevision: permanentRevision,
				}
			},
			wantError: models.ErrInvalidLogLevel,
		},
		{
			name: "rejects a stale permanent revision before persistence",
			input: func(permanentRevision int64) models.PermanentConfigInput {
				return models.PermanentConfigInput{
					Global:                    models.LogLevelError,
					PerModule:                 map[string]models.LogLevel{"auth": models.LogLevelInfo},
					ExpectedPermanentRevision: permanentRevision - 1,
				}
			},
			wantError: ErrConfigConflict,
		},
		{
			name: "keeps the published snapshot when persistence fails",
			input: func(permanentRevision int64) models.PermanentConfigInput {
				return models.PermanentConfigInput{
					Global:                    models.LogLevelError,
					PerModule:                 map[string]models.LogLevel{"auth": models.LogLevelInfo},
					ExpectedPermanentRevision: permanentRevision,
				}
			},
			persist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, now := setup(t)
			before := svc.View()
			beforeCalls := repo.calls
			*now = baseTime.Add(time.Minute)
			if tt.persist {
				repo.err = errors.New("boom")
			}

			err := svc.ApplyPermanent(ctx, tt.input(before.PermanentRevision), "config-operator")
			if err == nil {
				t.Fatal("ApplyPermanent succeeded, want error")
			}
			if tt.wantError != nil && !errors.Is(err, tt.wantError) {
				t.Errorf("ApplyPermanent error = %v, want errors.Is(%v)", err, tt.wantError)
			}
			wantCalls := beforeCalls
			if tt.persist {
				wantCalls++
			}
			if repo.calls != wantCalls {
				t.Errorf("repository calls = %d, want %d", repo.calls, wantCalls)
			}
			after := svc.View()
			after.ServerTime = before.ServerTime
			if !reflect.DeepEqual(after, before) {
				t.Errorf("view changed after failure:\n before: %+v\n  after: %+v", before, after)
			}
		})
	}
}

func TestLogLevelService_Diagnostic(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "takes precedence then expires back to permanent override",
			run: func(t *testing.T) {
				svc, repo := newTestSvc(t)
				now := fixedNow
				svc.now = func() time.Time { return now }

				if err := svc.SetModule(ctx, "auth", models.LogLevelWarn, "operator-1"); err != nil {
					t.Fatalf("SetModule: %v", err)
				}
				expiresAt := now.Add(time.Hour)
				if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, &expiresAt, "operator-1"); err != nil {
					t.Fatalf("StartDiagnostic: %v", err)
				}

				if level, explicit := svc.LevelFor("auth"); level != slog.LevelDebug || !explicit {
					t.Fatalf("active LevelFor(auth) = %v,%v want debug,true", level, explicit)
				}
				persisted, ok := repo.doc.Diagnostics["auth"]
				if !ok {
					t.Fatal("diagnostic was not persisted")
				}
				if persisted.Level != models.LogLevelDebug || persisted.StartedAt != fixedNow || persisted.StartedBy != "operator-1" {
					t.Errorf("persisted diagnostic = %+v", persisted)
				}
				if persisted.ExpiresAt == nil || !persisted.ExpiresAt.Equal(expiresAt) {
					t.Errorf("persisted expiry = %v, want %v", persisted.ExpiresAt, expiresAt)
				}

				view := svc.View()
				if len(view.Diagnostics) != 1 {
					t.Fatalf("view diagnostics = %+v, want one entry", view.Diagnostics)
				}
				entry := view.Diagnostics[0]
				if entry.Module != "auth" || entry.Level != models.LogLevelDebug || entry.StartedAt != fixedNow || entry.StartedBy != "operator-1" {
					t.Errorf("admin diagnostic = %+v", entry)
				}

				now = expiresAt
				if level, explicit := svc.LevelFor("auth"); level != slog.LevelWarn || !explicit {
					t.Errorf("at-expiry LevelFor(auth) = %v,%v want warn,true", level, explicit)
				}
				if got := len(svc.View().Diagnostics); got != 0 {
					t.Errorf("expired diagnostics in view = %d, want 0", got)
				}

				now = now.Add(time.Hour)
				if level, explicit := svc.LevelFor("auth"); level != slog.LevelWarn || !explicit {
					t.Errorf("expired LevelFor(auth) = %v,%v want warn,true", level, explicit)
				}
			},
		},
		{
			name: "no-expiry diagnostic persists across restart",
			run: func(t *testing.T) {
				svc, repo := newTestSvc(t)
				now := fixedNow
				svc.now = func() time.Time { return now }
				if err := svc.StartDiagnostic(ctx, "billing", models.LogLevelDebug, nil, "operator-2"); err != nil {
					t.Fatalf("StartDiagnostic: %v", err)
				}

				now = now.Add(365 * 24 * time.Hour)
				restarted := NewLogLevelService(repo, svc.logger, slog.LevelError, nil, []string{"rag", "billing", "auth"})
				restarted.now = func() time.Time { return now }
				if err := restarted.Load(ctx); err != nil {
					t.Fatalf("Load after restart: %v", err)
				}
				if level, explicit := restarted.LevelFor("billing"); level != slog.LevelDebug || !explicit {
					t.Errorf("restarted LevelFor(billing) = %v,%v want debug,true", level, explicit)
				}
				view := restarted.View()
				if len(view.Diagnostics) != 1 || view.Diagnostics[0].ExpiresAt != nil {
					t.Errorf("restarted diagnostics = %+v, want one without expiry", view.Diagnostics)
				}
			},
		},
		{
			name: "rejects an unknown module without persistence",
			run: func(t *testing.T) {
				svc, repo := newTestSvc(t)
				svc.now = func() time.Time { return fixedNow }
				if err := svc.StartDiagnostic(ctx, "unknown", models.LogLevelDebug, nil, "operator-1"); err == nil {
					t.Fatal("StartDiagnostic accepted an unknown module")
				}
				if repo.upserts != 0 {
					t.Errorf("repository upserts = %d, want 0", repo.upserts)
				}
				if _, explicit := svc.LevelFor("unknown"); explicit {
					t.Error("unknown module became explicit after rejected diagnostic")
				}
			},
		},
		{
			name: "persistence failure leaves the published snapshot unchanged",
			run: func(t *testing.T) {
				svc, repo := newTestSvc(t)
				svc.now = func() time.Time { return fixedNow }
				if err := svc.SetModule(ctx, "auth", models.LogLevelWarn, "operator-1"); err != nil {
					t.Fatalf("SetModule: %v", err)
				}
				repo.err = errors.New("boom")
				if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, nil, "operator-1"); err == nil {
					t.Fatal("StartDiagnostic succeeded with a broken repository")
				}
				if level, explicit := svc.LevelFor("auth"); level != slog.LevelWarn || !explicit {
					t.Errorf("LevelFor(auth) = %v,%v want unchanged warn,true", level, explicit)
				}
				if got := len(svc.View().Diagnostics); got != 0 {
					t.Errorf("published diagnostics = %d, want 0", got)
				}
			},
		},
		{
			name: "stop removes a diagnostic and is idempotent",
			run: func(t *testing.T) {
				svc, repo := newTestSvc(t)
				svc.now = func() time.Time { return fixedNow }
				if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, nil, "operator-1"); err != nil {
					t.Fatalf("StartDiagnostic: %v", err)
				}
				if err := svc.StopDiagnostic(ctx, "auth", "operator-2"); err != nil {
					t.Fatalf("StopDiagnostic: %v", err)
				}
				if _, ok := repo.doc.Diagnostics["auth"]; ok {
					t.Error("stopped diagnostic remains persisted")
				}
				if _, explicit := svc.LevelFor("auth"); explicit {
					t.Error("auth remains explicit after diagnostic stop")
				}

				upserts := repo.upserts
				if err := svc.StopDiagnostic(ctx, "auth", "operator-2"); err != nil {
					t.Fatalf("second StopDiagnostic: %v", err)
				}
				if repo.upserts != upserts {
					t.Errorf("idempotent stop persisted again: %d -> %d", upserts, repo.upserts)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestLogLevelService_CleanupExpired(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	svc, repo := newTestSvc(t)
	svc.now = func() time.Time { return now }

	expiredAt := now.Add(time.Hour)
	futureAt := now.Add(4 * time.Hour)
	if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, &expiredAt, "operator-1"); err != nil {
		t.Fatalf("start auth diagnostic: %v", err)
	}
	if err := svc.StartDiagnostic(ctx, "billing", models.LogLevelDebug, nil, "operator-1"); err != nil {
		t.Fatalf("start billing diagnostic: %v", err)
	}
	if err := svc.StartDiagnostic(ctx, "rag", models.LogLevelInfo, &futureAt, "operator-1"); err != nil {
		t.Fatalf("start rag diagnostic: %v", err)
	}

	now = now.Add(2 * time.Hour)
	if err := svc.CleanupExpired(ctx); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if _, ok := repo.doc.Diagnostics["auth"]; ok {
		t.Error("expired auth diagnostic remains persisted")
	}
	for _, module := range []string{"billing", "rag"} {
		if _, ok := repo.doc.Diagnostics[module]; !ok {
			t.Errorf("active %s diagnostic was removed", module)
		}
	}
	if got := len(svc.View().Diagnostics); got != 2 {
		t.Errorf("active diagnostics in view = %d, want 2", got)
	}

	upserts := repo.upserts
	if err := svc.CleanupExpired(ctx); err != nil {
		t.Fatalf("second CleanupExpired: %v", err)
	}
	if repo.upserts != upserts {
		t.Errorf("cleanup without expired entries persisted again: %d -> %d", upserts, repo.upserts)
	}
}

func TestLogLevelService_CleanupExpired_PersistFailurePreservesSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	svc, repo := newTestSvc(t)
	svc.now = func() time.Time { return now }

	expiresAt := now.Add(time.Hour)
	if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, &expiresAt, "operator-1"); err != nil {
		t.Fatalf("StartDiagnostic: %v", err)
	}
	now = now.Add(2 * time.Hour)
	repo.err = errors.New("boom")
	if err := svc.CleanupExpired(ctx); err == nil {
		t.Fatal("CleanupExpired succeeded with a broken repository")
	}
	if _, ok := svc.current.Load().diagnostics["auth"]; !ok {
		t.Error("expired diagnostic was unpublished despite persistence failure")
	}
}

func TestLogLevelService_MultiReplicaMutationsPreserveAuthoritativeState(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	newReplica := func() *LogLevelService {
		return NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth", "billing"})
	}
	replicaA := newReplica()
	replicaB := newReplica()

	if err := replicaA.SetGlobal(ctx, models.LogLevelWarn, "operator-a"); err != nil {
		t.Fatalf("replica A SetGlobal: %v", err)
	}
	if err := replicaB.SetModule(ctx, "auth", models.LogLevelDebug, "operator-b"); err != nil {
		t.Fatalf("replica B SetModule: %v", err)
	}

	persisted, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("load authoritative document: %v", err)
	}
	if persisted.Global != models.LogLevelWarn {
		t.Errorf("replica B overwrote replica A global level: got %q, want warn", persisted.Global)
	}
	if persisted.PerModule["auth"] != models.LogLevelDebug {
		t.Errorf("authoritative auth override = %q, want debug", persisted.PerModule["auth"])
	}
}

func TestLogLevelService_DiagnosticWriteDoesNotConflictPermanentDraft(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestSvc(t)
	permanentSnapshot := svc.View()

	if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, nil, "diagnostic-operator"); err != nil {
		t.Fatalf("StartDiagnostic: %v", err)
	}
	err := svc.ApplyPermanent(ctx, models.PermanentConfigInput{
		Global:                    models.LogLevelWarn,
		PerModule:                 map[string]models.LogLevel{"auth": models.LogLevelError},
		ExpectedPermanentRevision: permanentSnapshot.PermanentRevision,
	}, "config-operator")
	if err != nil {
		t.Fatalf("diagnostic-only write caused permanent conflict: %v", err)
	}
	if got := svc.Global(); got != slog.LevelWarn {
		t.Errorf("global = %v, want warn", got)
	}
	if got := len(svc.View().Diagnostics); got != 1 {
		t.Errorf("diagnostics = %d, want preserved diagnostic", got)
	}
}

func TestLogLevelService_RevisionsSeparatePermanentAndDiagnosticWrites(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestSvc(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	if err := svc.SetGlobal(ctx, models.LogLevelWarn, "config"); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if got := svc.View(); got.Revision != 1 || got.PermanentRevision != 1 {
		t.Fatalf("after permanent write revisions = %d/%d, want 1/1", got.Revision, got.PermanentRevision)
	}
	expiresAt := now.Add(time.Minute)
	if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, &expiresAt, "diagnostic"); err != nil {
		t.Fatalf("StartDiagnostic: %v", err)
	}
	if got := svc.View(); got.Revision != 2 || got.PermanentRevision != 1 {
		t.Fatalf("after diagnostic write revisions = %d/%d, want 2/1", got.Revision, got.PermanentRevision)
	}
	now = now.Add(2 * time.Minute)
	if err := svc.CleanupExpired(ctx); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if repo.doc.Revision != 3 || repo.doc.PermanentRevision != 1 {
		t.Fatalf("after cleanup revisions = %d/%d, want 3/1", repo.doc.Revision, repo.doc.PermanentRevision)
	}
	if err := svc.ApplyPermanent(ctx, models.PermanentConfigInput{
		Global:                    models.LogLevelError,
		PerModule:                 map[string]models.LogLevel{},
		ExpectedPermanentRevision: 1,
	}, "config"); err != nil {
		t.Fatalf("ApplyPermanent: %v", err)
	}
	if got := svc.View(); got.Revision != 4 || got.PermanentRevision != 2 {
		t.Fatalf("after apply revisions = %d/%d, want 4/2", got.Revision, got.PermanentRevision)
	}
}

func TestLogLevelService_RetriesCASAndRejectsPermanentRace(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestSvc(t)
	repo.misses = 1
	if err := svc.SetGlobal(ctx, models.LogLevelWarn, "operator"); err != nil {
		t.Fatalf("SetGlobal after one CAS miss: %v", err)
	}
	if repo.calls != 2 || svc.Global() != slog.LevelWarn {
		t.Fatalf("CAS retry calls/global = %d/%v, want 2/WARN", repo.calls, svc.Global())
	}

	staleRevision := svc.View().PermanentRevision
	other := NewLogLevelService(repo, svc.logger, slog.LevelInfo, nil, []string{"rag", "billing", "auth"})
	if err := other.SetGlobal(ctx, models.LogLevelError, "other"); err != nil {
		t.Fatalf("other SetGlobal: %v", err)
	}
	err := svc.ApplyPermanent(ctx, models.PermanentConfigInput{
		Global:                    models.LogLevelDebug,
		PerModule:                 map[string]models.LogLevel{},
		ExpectedPermanentRevision: staleRevision,
	}, "stale")
	if !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("stale ApplyPermanent error = %v, want ErrConfigConflict", err)
	}
}

func TestLogLevelService_LegacyDocumentMigratesOnFirstCAS(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestSvc(t)
	repo.doc = &models.LogLevelDoc{
		ConfigKey: models.DefaultConfigKey,
		Global:    models.LogLevelWarn,
		PerModule: map[string]models.LogLevel{"auth": models.LogLevelError},
		// revision fields deliberately absent/zero.
	}
	if err := svc.Load(ctx); err != nil {
		t.Fatalf("Load legacy document: %v", err)
	}
	if err := svc.StartDiagnostic(ctx, "auth", models.LogLevelDebug, nil, "operator"); err != nil {
		t.Fatalf("migrate legacy document: %v", err)
	}
	if repo.doc.Revision != 1 || repo.doc.PermanentRevision != 0 {
		t.Fatalf("migrated revisions = %d/%d, want 1/0", repo.doc.Revision, repo.doc.PermanentRevision)
	}
	if repo.doc.Global != models.LogLevelWarn || repo.doc.PerModule["auth"] != models.LogLevelError {
		t.Fatalf("legacy permanent state changed during migration: %+v", repo.doc)
	}
}

func TestLogLevelService_ConcurrentReadsAndWrites(t *testing.T) {
	// Smoke test under -race: many concurrent readers (Global / LevelFor)
	// while a writer mutates permanent and diagnostic overrides.
	// atomic.Pointer snapshots keep reads consistent without locking the
	// hot path.
	svc, _ := newTestSvc(t)
	var (
		stop atomic.Bool
		wg   sync.WaitGroup
	)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_ = svc.Global()
				_, _ = svc.LevelFor("rag")
				_, _ = svc.LevelFor("billing")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 200; n++ {
			lvl := models.LogLevelInfo
			if n%2 == 0 {
				lvl = models.LogLevelDebug
			}
			if err := svc.SetModule(context.Background(), "billing", lvl, "u"); err != nil {
				t.Errorf("SetModule: %v", err)
				return
			}
			if n%2 == 0 {
				if err := svc.StartDiagnostic(context.Background(), "auth", models.LogLevelDebug, nil, "u"); err != nil {
					t.Errorf("StartDiagnostic: %v", err)
					return
				}
			} else if err := svc.StopDiagnostic(context.Background(), "auth", "u"); err != nil {
				t.Errorf("StopDiagnostic: %v", err)
				return
			}
		}
		stop.Store(true)
	}()
	wg.Wait()
}
