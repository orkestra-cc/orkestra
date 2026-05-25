package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

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
	t.Helper()
	repo := &fakeRepo{}
	logger := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth", "billing"})
	return NewLogLevelHandler(svc), repo
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func TestHandler_Get_ReturnsCurrentView(t *testing.T) {
	h, _ := newHandler(t)

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
