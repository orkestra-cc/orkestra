package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/logging/logquery"
	"github.com/orkestra/backend/internal/core/logging/models"
	"github.com/orkestra/backend/internal/core/logging/repository"
	"github.com/orkestra/backend/internal/core/logging/services"
	"github.com/orkestra/backend/internal/testkit"
)

// fakeRepo mirrors the one in services/log_level_service_test.go so handler
// tests can drive a real LogLevelService end-to-end without standing up Mongo.
// Kept local to the handlers package to avoid the test_helpers cycle.
type fakeRepo struct {
	doc *models.LogLevelDoc
	err error
}

func (r *fakeRepo) Get(_ context.Context) (*models.LogLevelDoc, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.doc == nil {
		return nil, repository.ErrNotFound
	}
	clone := *r.doc
	return &clone, nil
}

func (r *fakeRepo) Upsert(_ context.Context, doc *models.LogLevelDoc) error {
	if r.err != nil {
		return r.err
	}
	clone := *doc
	r.doc = &clone
	return nil
}

func newHandler(t *testing.T) (*LogLevelHandler, *fakeRepo) {
	return newHandlerWithProvider(t, &fakeLogProvider{})
}

func newHandlerWithProvider(t *testing.T, provider *fakeLogProvider) (*LogLevelHandler, *fakeRepo) {
	t.Helper()
	repo := &fakeRepo{}
	logger := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth", "billing"})
	return NewLogLevelHandler(svc, provider, "https://grafana.example.test"), repo
}

type fakeLogProvider struct {
	available bool
	events    []models.LogEvent
	err       error
	query     logquery.Query
}

func (p *fakeLogProvider) Status(context.Context) logquery.Status {
	return logquery.Status{Available: p.available}
}

func (p *fakeLogProvider) Query(_ context.Context, query logquery.Query) ([]models.LogEvent, error) {
	p.query = query
	return p.events, p.err
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func TestHandler_Get_ReturnsCurrentView(t *testing.T) {
	provider := &fakeLogProvider{available: true}
	h, _ := newHandlerWithProvider(t, provider)

	resp, err := h.Get(context.Background(), &GetRequest{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Body.Global != models.LogLevelInfo {
		t.Errorf("Global = %q, want info", resp.Body.Global)
	}
	if len(resp.Body.Modules) != 2 {
		t.Errorf("Modules count = %d, want 2", len(resp.Body.Modules))
	}
	if !resp.Body.LogProvider.Available || resp.Body.LogProvider.GrafanaURL != "https://grafana.example.test" {
		t.Errorf("LogProvider = %+v, want available with Grafana URL", resp.Body.LogProvider)
	}
}

func TestLogLevelHandler_GetLogs(t *testing.T) {
	t.Run("returns the minimized provider events and forwards constrained filters", func(t *testing.T) {
		event := models.LogEvent{
			Timestamp:  time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
			Level:      models.LogLevelWarn,
			Message:    "preview message",
			Module:     "auth",
			Attributes: map[string]any{"trace_id": "trace-1"},
		}
		provider := &fakeLogProvider{available: true, events: []models.LogEvent{event}}
		h, _ := newHandlerWithProvider(t, provider)

		resp, err := h.GetLogs(context.Background(), &GetLogsRequest{
			Module:        "auth",
			WindowMinutes: "15",
			Level:         "warn",
			Q:             "failed request",
			Limit:         "25",
		})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if len(resp.Body.Events) != 1 || resp.Body.Events[0].Message != "preview message" {
			t.Errorf("response events = %+v", resp.Body.Events)
		}
		want := logquery.Query{Module: "auth", WindowMinutes: 15, Level: "warn", Text: "failed request", Limit: 25}
		if provider.query != want {
			t.Errorf("provider query = %+v, want %+v", provider.query, want)
		}
	})

	t.Run("unavailable provider returns stable 503 without querying", func(t *testing.T) {
		provider := &fakeLogProvider{}
		h, _ := newHandlerWithProvider(t, provider)

		_, err := h.GetLogs(context.Background(), &GetLogsRequest{Module: "auth", WindowMinutes: "15", Limit: "20"})
		assertStatusAndCode(t, err, 503, "logging.log_provider_unavailable")
		if provider.query.Module != "" {
			t.Errorf("unavailable provider was queried: %+v", provider.query)
		}
	})

	tests := []struct {
		name        string
		providerErr error
		wantStatus  int
		wantCode    string
	}{
		{name: "invalid query", providerErr: logquery.ErrInvalidQuery, wantStatus: 400, wantCode: "logging.log_preview_invalid"},
		{name: "provider timeout", providerErr: logquery.ErrTimeout, wantStatus: 504, wantCode: "logging.log_provider_timeout"},
		{name: "upstream failure", providerErr: errors.New("upstream-secret-body"), wantStatus: 502, wantCode: "logging.log_provider_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeLogProvider{available: true, err: tt.providerErr}
			h, _ := newHandlerWithProvider(t, provider)

			_, err := h.GetLogs(context.Background(), &GetLogsRequest{Module: "auth", WindowMinutes: "15", Limit: "20"})
			assertStatusAndCode(t, err, tt.wantStatus, tt.wantCode)
			if strings.Contains(err.Error(), "upstream-secret-body") {
				t.Errorf("handler error disclosed upstream content: %v", err)
			}
		})
	}
}

func TestHandler_SetGlobal_PersistsAndReturnsView(t *testing.T) {
	h, repo := newHandler(t)

	req := &SetGlobalRequest{}
	req.Body.Level = "warn"

	ctx := testkit.NewIdentity("admin-1", "a@example.com", "administrator").
		ContextFor(context.Background(), "")
	resp, err := h.SetGlobal(ctx, req)
	if err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if resp.Body.Global != models.LogLevelWarn {
		t.Errorf("view.Global = %q, want warn", resp.Body.Global)
	}
	if repo.doc == nil || repo.doc.Global != models.LogLevelWarn {
		t.Errorf("repo doc = %+v, want persisted Global=warn", repo.doc)
	}
	if repo.doc.UpdatedBy != "admin-1" {
		t.Errorf("UpdatedBy = %q, want admin-1 (from ctxauth)", repo.doc.UpdatedBy)
	}
}

func TestHandler_SetGlobal_RejectsInvalidLevel(t *testing.T) {
	h, _ := newHandler(t)

	req := &SetGlobalRequest{}
	req.Body.Level = "trace"

	_, err := h.SetGlobal(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
	se, ok := err.(huma.StatusError)
	if !ok {
		t.Fatalf("err = %v (%T), want huma.StatusError", err, err)
	}
	if se.GetStatus() != 400 {
		t.Errorf("status = %d, want 400", se.GetStatus())
	}
}

func TestHandler_SetGlobal_ServiceFailureSurfacesAs500(t *testing.T) {
	h, repo := newHandler(t)
	repo.err = errors.New("mongo down")

	req := &SetGlobalRequest{}
	req.Body.Level = "warn"

	_, err := h.SetGlobal(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 500 {
		t.Errorf("want 500, got %v", err)
	}
}

func TestHandler_SetModule_HappyPath(t *testing.T) {
	h, repo := newHandler(t)

	req := &SetModuleRequest{Module: "billing"}
	req.Body.Level = "debug"
	ctx := testkit.NewIdentity("u", "u@e", "administrator").ContextFor(context.Background(), "")

	resp, err := h.SetModule(ctx, req)
	if err != nil {
		t.Fatalf("SetModule: %v", err)
	}

	var billing *models.AdminModuleEntry
	for i := range resp.Body.Modules {
		if resp.Body.Modules[i].Name == "billing" {
			billing = &resp.Body.Modules[i]
		}
	}
	if billing == nil {
		t.Fatal("billing row missing from view")
	}
	if !billing.HasOverride || billing.Effective != models.LogLevelDebug {
		t.Errorf("billing row = %+v, want HasOverride=true, Effective=debug", billing)
	}
	if repo.doc.PerModule["billing"] != models.LogLevelDebug {
		t.Errorf("billing override not persisted: %+v", repo.doc.PerModule)
	}
}

func TestHandler_SetModule_RejectsEmptyModule(t *testing.T) {
	h, _ := newHandler(t)

	req := &SetModuleRequest{Module: ""}
	req.Body.Level = "debug"

	_, err := h.SetModule(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty module")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 400 {
		t.Errorf("want 400, got %v", err)
	}
	if !strings.Contains(err.Error(), "module") {
		t.Errorf("error message should mention 'module': %v", err)
	}
}

func TestHandler_UnsetModule_RemovesOverride(t *testing.T) {
	h, repo := newHandler(t)

	// Seed an override first.
	setReq := &SetModuleRequest{Module: "auth"}
	setReq.Body.Level = "warn"
	if _, err := h.SetModule(context.Background(), setReq); err != nil {
		t.Fatalf("seed SetModule: %v", err)
	}

	resp, err := h.UnsetModule(context.Background(), &UnsetModuleRequest{Module: "auth"})
	if err != nil {
		t.Fatalf("UnsetModule: %v", err)
	}
	for _, m := range resp.Body.Modules {
		if m.Name == "auth" && m.HasOverride {
			t.Errorf("auth still has override after Unset: %+v", m)
		}
	}
	if _, ok := repo.doc.PerModule["auth"]; ok {
		t.Errorf("auth still in persisted PerModule map: %+v", repo.doc.PerModule)
	}
}

func TestHandler_UnsetModule_RejectsEmptyModule(t *testing.T) {
	h, _ := newHandler(t)

	_, err := h.UnsetModule(context.Background(), &UnsetModuleRequest{Module: ""})
	if err == nil {
		t.Fatal("expected error for empty module")
	}
}

func TestHandler_Reset_RevertsToEnv(t *testing.T) {
	h, repo := newHandler(t)

	// Make some changes first.
	setG := &SetGlobalRequest{}
	setG.Body.Level = "error"
	if _, err := h.SetGlobal(context.Background(), setG); err != nil {
		t.Fatalf("SetGlobal seed: %v", err)
	}
	setM := &SetModuleRequest{Module: "billing"}
	setM.Body.Level = "debug"
	if _, err := h.SetModule(context.Background(), setM); err != nil {
		t.Fatalf("SetModule seed: %v", err)
	}

	resp, err := h.Reset(context.Background(), &ResetRequest{})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if resp.Body.Global != models.LogLevelInfo {
		t.Errorf("Global after reset = %q, want info (env default)", resp.Body.Global)
	}
	if repo.doc.Global != models.LogLevelInfo {
		t.Errorf("persisted Global after reset = %q, want info", repo.doc.Global)
	}
	if _, ok := repo.doc.PerModule["billing"]; ok {
		t.Errorf("billing override should be gone after reset")
	}
}

func TestActor_FallsBackToUnknown(t *testing.T) {
	if got := actor(context.Background()); got != "unknown" {
		t.Errorf("actor with bare ctx = %q, want %q", got, "unknown")
	}
	ctx := testkit.NewIdentity("u-42", "x@y", "administrator").ContextFor(context.Background(), "")
	if got := actor(ctx); got != "u-42" {
		t.Errorf("actor with identity ctx = %q, want u-42", got)
	}
}

func TestLogLevelHandler_Apply(t *testing.T) {
	t.Run("decodes the complete config, captures the actor, and returns the fresh view", func(t *testing.T) {
		h, repo := newHandler(t)
		req := &ApplyRequest{}
		payload := []byte(`{
			"global":"warn",
			"perModule":{"auth":"debug"},
			"expectedUpdatedAt":"0001-01-01T00:00:00Z"
		}`)
		if err := json.Unmarshal(payload, &req.Body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		ctx := testkit.NewIdentity("batch-operator", "operator@example.com", "administrator").
			ContextFor(context.Background(), "")

		resp, err := h.Apply(ctx, req)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}

		if resp.Body.Global != models.LogLevelWarn {
			t.Errorf("response global = %q, want warn", resp.Body.Global)
		}
		if resp.Body.UpdatedBy != "batch-operator" {
			t.Errorf("response actor = %q, want batch-operator", resp.Body.UpdatedBy)
		}
		if repo.doc == nil || repo.doc.UpdatedBy != "batch-operator" {
			t.Fatalf("persisted document = %+v, want batch-operator actor", repo.doc)
		}
		if got := repo.doc.PerModule["auth"]; got != models.LogLevelDebug {
			t.Errorf("persisted auth level = %q, want debug", got)
		}
		if resp.Body.UpdatedAt.IsZero() || !resp.Body.UpdatedAt.Equal(repo.doc.UpdatedAt) {
			t.Errorf("response updatedAt = %v, persisted = %v", resp.Body.UpdatedAt, repo.doc.UpdatedAt)
		}
	})

	t.Run("maps a stale snapshot to conflict", func(t *testing.T) {
		h, _ := newHandler(t)
		seed := &SetGlobalRequest{}
		seed.Body.Level = "warn"
		if _, err := h.SetGlobal(context.Background(), seed); err != nil {
			t.Fatalf("seed global: %v", err)
		}

		req := &ApplyRequest{}
		req.Body.Global = "error"
		req.Body.PerModule = map[string]string{}
		req.Body.ExpectedUpdatedAt = time.Time{}

		_, err := h.Apply(context.Background(), req)
		assertStatus(t, err, 409)
	})

	tests := []struct {
		name      string
		global    string
		perModule map[string]string
	}{
		{name: "invalid global level", global: "trace", perModule: map[string]string{}},
		{name: "invalid module level", global: "info", perModule: map[string]string{"auth": "trace"}},
		{name: "unknown module", global: "info", perModule: map[string]string{"unknown": "debug"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newHandler(t)
			req := &ApplyRequest{}
			req.Body.Global = tt.global
			req.Body.PerModule = tt.perModule

			_, err := h.Apply(context.Background(), req)
			assertStatus(t, err, 400)
		})
	}

	t.Run("maps persistence failure to internal server error", func(t *testing.T) {
		h, repo := newHandler(t)
		repo.err = errors.New("mongo unavailable")
		req := &ApplyRequest{}
		req.Body.Global = "info"
		req.Body.PerModule = map[string]string{}

		_, err := h.Apply(context.Background(), req)
		assertStatus(t, err, 500)
	})
}

func TestLogLevelHandler_StartDiagnostic(t *testing.T) {
	allowedDurations := []struct {
		name            string
		durationMinutes *int
		wantDuration    time.Duration
	}{
		{name: "15 minutes", durationMinutes: intPointer(15), wantDuration: 15 * time.Minute},
		{name: "60 minutes", durationMinutes: intPointer(60), wantDuration: time.Hour},
		{name: "240 minutes", durationMinutes: intPointer(240), wantDuration: 4 * time.Hour},
		{name: "no expiry", durationMinutes: nil},
	}
	for _, tt := range allowedDurations {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newHandler(t)
			req := &StartDiagnosticRequest{Module: "auth"}
			payload := []byte(`{"level":"debug"}`)
			if tt.durationMinutes != nil {
				payload = []byte(`{"level":"debug","durationMinutes":` +
					strings.TrimSpace(string(mustJSON(t, *tt.durationMinutes))) + `}`)
			}
			if err := json.Unmarshal(payload, &req.Body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			ctx := testkit.NewIdentity("diagnostic-operator", "operator@example.com", "administrator").
				ContextFor(context.Background(), "")
			before := time.Now().UTC()

			resp, err := h.StartDiagnostic(ctx, req)
			after := time.Now().UTC()
			if err != nil {
				t.Fatalf("StartDiagnostic: %v", err)
			}

			persisted, ok := repo.doc.Diagnostics["auth"]
			if !ok {
				t.Fatal("auth diagnostic was not persisted")
			}
			if persisted.Level != models.LogLevelDebug || persisted.StartedBy != "diagnostic-operator" {
				t.Errorf("persisted diagnostic = %+v", persisted)
			}
			if tt.durationMinutes == nil {
				if persisted.ExpiresAt != nil {
					t.Errorf("no-expiry diagnostic expires at %v", persisted.ExpiresAt)
				}
			} else if persisted.ExpiresAt == nil || persisted.ExpiresAt.Before(before.Add(tt.wantDuration)) || persisted.ExpiresAt.After(after.Add(tt.wantDuration)) {
				t.Errorf("expiresAt = %v, want between %v and %v", persisted.ExpiresAt, before.Add(tt.wantDuration), after.Add(tt.wantDuration))
			}
			if len(resp.Body.Diagnostics) != 1 || resp.Body.Diagnostics[0].StartedBy != "diagnostic-operator" {
				t.Errorf("fresh response diagnostics = %+v", resp.Body.Diagnostics)
			}
		})
	}

	tests := []struct {
		name     string
		module   string
		level    string
		duration *int
	}{
		{name: "empty module", module: "", level: "debug", duration: intPointer(15)},
		{name: "unknown module", module: "unknown", level: "debug", duration: intPointer(15)},
		{name: "invalid level", module: "auth", level: "trace", duration: intPointer(15)},
		{name: "arbitrary duration", module: "auth", level: "debug", duration: intPointer(30)},
		{name: "zero duration", module: "auth", level: "debug", duration: intPointer(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newHandler(t)
			req := &StartDiagnosticRequest{Module: tt.module}
			req.Body.Level = tt.level
			req.Body.DurationMinutes = tt.duration

			_, err := h.StartDiagnostic(context.Background(), req)
			assertStatus(t, err, 400)
		})
	}

	t.Run("maps persistence failure to internal server error", func(t *testing.T) {
		h, repo := newHandler(t)
		repo.err = errors.New("mongo unavailable")
		req := &StartDiagnosticRequest{Module: "auth"}
		req.Body.Level = "debug"
		req.Body.DurationMinutes = intPointer(15)

		_, err := h.StartDiagnostic(context.Background(), req)
		assertStatus(t, err, 500)
	})
}

func TestLogLevelHandler_StopDiagnostic(t *testing.T) {
	t.Run("captures the actor and returns the fresh view", func(t *testing.T) {
		h, repo := newHandler(t)
		start := &StartDiagnosticRequest{Module: "auth"}
		start.Body.Level = "debug"
		if _, err := h.StartDiagnostic(context.Background(), start); err != nil {
			t.Fatalf("seed diagnostic: %v", err)
		}
		ctx := testkit.NewIdentity("stop-operator", "operator@example.com", "administrator").
			ContextFor(context.Background(), "")

		resp, err := h.StopDiagnostic(ctx, &StopDiagnosticRequest{Module: "auth"})
		if err != nil {
			t.Fatalf("StopDiagnostic: %v", err)
		}

		if len(resp.Body.Diagnostics) != 0 {
			t.Errorf("fresh response diagnostics = %+v, want empty", resp.Body.Diagnostics)
		}
		if repo.doc == nil || repo.doc.UpdatedBy != "stop-operator" {
			t.Fatalf("persisted document = %+v, want stop-operator actor", repo.doc)
		}
		if _, ok := repo.doc.Diagnostics["auth"]; ok {
			t.Error("stopped diagnostic remains persisted")
		}
	})

	for _, module := range []string{"", "unknown"} {
		t.Run("rejects module "+module, func(t *testing.T) {
			h, _ := newHandler(t)
			_, err := h.StopDiagnostic(context.Background(), &StopDiagnosticRequest{Module: module})
			assertStatus(t, err, 400)
		})
	}

	t.Run("maps persistence failure to internal server error", func(t *testing.T) {
		h, repo := newHandler(t)
		start := &StartDiagnosticRequest{Module: "auth"}
		start.Body.Level = "debug"
		if _, err := h.StartDiagnostic(context.Background(), start); err != nil {
			t.Fatalf("seed diagnostic: %v", err)
		}
		repo.err = errors.New("mongo unavailable")

		_, err := h.StopDiagnostic(context.Background(), &StopDiagnosticRequest{Module: "auth"})
		assertStatus(t, err, 500)
	})
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want HTTP %d", want)
	}
	statusErr, ok := err.(huma.StatusError)
	if !ok {
		t.Fatalf("error = %v (%T), want huma.StatusError", err, err)
	}
	if got := statusErr.GetStatus(); got != want {
		t.Errorf("status = %d, want %d (error: %v)", got, want, err)
	}
}

func assertStatusAndCode(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	assertStatus(t, err, wantStatus)
	type coded interface{ ErrorCode() string }
	if value, ok := err.(coded); ok {
		if got := value.ErrorCode(); got != wantCode {
			t.Errorf("code = %q, want %q", got, wantCode)
		}
		return
	}
	// errcode.Error intentionally exposes Code as a field rather than an
	// accessor. Keep this assertion local so production interfaces stay small.
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal error response: %v", marshalErr)
	}
	var envelope struct {
		Code string `json:"code"`
	}
	if unmarshalErr := json.Unmarshal(encoded, &envelope); unmarshalErr != nil {
		t.Fatalf("decode error response: %v", unmarshalErr)
	}
	if envelope.Code != wantCode {
		t.Errorf("code = %q, want %q (error: %v)", envelope.Code, wantCode, err)
	}
}

func intPointer(value int) *int { return &value }

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return out
}
