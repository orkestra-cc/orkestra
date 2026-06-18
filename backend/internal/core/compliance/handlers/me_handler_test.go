package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- shared test helpers (package handlers) ---

// assertStatus pulls the status code out of a huma.StatusError. Mirror of the
// tenant/user handler test helpers so the suites read uniformly.
func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected huma error with status %d, got nil", want)
	}
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err %v is not a huma.StatusError", err)
	}
	if got := se.GetStatus(); got != want {
		t.Errorf("status = %d, want %d (err: %v)", got, want, err)
	}
}

// fakeProducer is a handlers-package stand-in for iface.PIIProducer so the DSR
// service can be exercised through the handler without MongoDB.
type fakeProducer struct {
	subject     string
	exportData  any
	exportErr   error
	purgeResult iface.PurgeResult
	purgeErr    error
}

func (p *fakeProducer) Subject() string { return p.subject }
func (p *fakeProducer) ExportPersonalData(_ context.Context, _ string) (any, error) {
	return p.exportData, p.exportErr
}
func (p *fakeProducer) PurgePersonalData(_ context.Context, _ string, _ iface.EraseMode) (iface.PurgeResult, error) {
	return p.purgeResult, p.purgeErr
}

// fakeHoldChecker satisfies services.LegalHoldChecker for the erase-gate tests.
type fakeHoldChecker struct {
	held bool
	err  error
}

func (f *fakeHoldChecker) IsHeld(_ context.Context, _ string) (bool, error) {
	return f.held, f.err
}

// newDSR builds a real DSRService over an in-memory producer registry.
func newDSR(producers ...iface.PIIProducer) *services.DSRService {
	reg := iface.NewPIIProducerRegistry()
	for _, p := range producers {
		reg.Register(p)
	}
	return services.NewDSRService(reg, nil, slog.Default())
}

// authedCtx stamps a userUUID into the context the way AuthMiddleware would.
func authedCtx(userUUID string) context.Context {
	return testkit.NewIdentity(userUUID, userUUID+"@example.com", "operator").
		ContextFor(context.Background(), "-")
}

// --- me_handler tests ---

func TestMeExportRequiresAuth(t *testing.T) {
	t.Parallel()
	h := NewMeHandler(newDSR())
	_, err := h.Export(context.Background(), &struct{}{})
	assertStatus(t, err, 401)
}

func TestMeEraseRequiresAuth(t *testing.T) {
	t.Parallel()
	h := NewMeHandler(newDSR())
	_, err := h.Erase(context.Background(), &struct{}{})
	assertStatus(t, err, 401)
}

// TestMeExportReturnsBundleAndErrors pins that the handler surfaces both the
// bundled producer data and the per-producer error map.
func TestMeExportReturnsBundleAndErrors(t *testing.T) {
	t.Parallel()
	good := &fakeProducer{subject: "user", exportData: map[string]any{"email": "a@b.com"}}
	bad := &fakeProducer{subject: "auth", exportErr: errors.New("mongo down")}
	h := NewMeHandler(newDSR(good, bad))

	out, err := h.Export(authedCtx("u-1"), &struct{}{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, ok := out.Body.Bundle["user"]; !ok {
		t.Fatal("bundle missing the surviving producer's data")
	}
	if out.Body.Errors["auth"] == "" {
		t.Fatalf("expected the failing producer in Errors; got %+v", out.Body.Errors)
	}
}

// TestMeEraseAggregatesTotalRows pins the totalRows roll-up across producers.
func TestMeEraseAggregatesTotalRows(t *testing.T) {
	t.Parallel()
	h := NewMeHandler(newDSR(
		&fakeProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1, RowsAnonymized: 2}},
		&fakeProducer{subject: "auth", purgeResult: iface.PurgeResult{RowsDeleted: 3}},
	))
	out, err := h.Erase(authedCtx("u-1"), &struct{}{})
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if out.Body.TotalRows != 6 {
		t.Fatalf("totalRows = %d; want 6", out.Body.TotalRows)
	}
}

// TestMeEraseBlockedByLegalHoldIs409 pins the legal-hold → 409 mapping.
func TestMeEraseBlockedByLegalHoldIs409(t *testing.T) {
	t.Parallel()
	dsr := newDSR(&fakeProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	h := NewMeHandler(dsr)

	_, err := h.Erase(authedCtx("u-held"), &struct{}{})
	assertStatus(t, err, 409)
}

// TestMeEraseInternalErrorIs500 pins that a non-legal-hold failure (here a
// hold-checker error) maps to 500 rather than leaking as 409.
func TestMeEraseInternalErrorIs500(t *testing.T) {
	t.Parallel()
	dsr := newDSR(&fakeProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{err: errors.New("mongo down")})
	h := NewMeHandler(dsr)

	_, err := h.Erase(authedCtx("u-1"), &struct{}{})
	assertStatus(t, err, 500)
}
