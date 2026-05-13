package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/compliance/services"
	"github.com/orkestra/backend/internal/testkit"
)

// stubProducer satisfies iface.PIIProducer just enough to walk the
// Erase pipeline without touching any storage.
type stubProducer struct {
	subj   string
	purgeR iface.PurgeResult
}

func (s *stubProducer) Subject() string                                         { return s.subj }
func (s *stubProducer) ExportPersonalData(context.Context, string) (any, error) { return nil, nil }
func (s *stubProducer) PurgePersonalData(_ context.Context, _ string) (iface.PurgeResult, error) {
	return s.purgeR, nil
}

func newGatedHandler(t *testing.T) *MeHandler {
	t.Helper()
	reg := iface.NewPIIProducerRegistry()
	reg.Register(&stubProducer{subj: "auth", purgeR: iface.PurgeResult{RowsDeleted: 1}})
	dsr := services.NewDSRService(reg, &noopAuditSink{}, slog.Default())
	return NewMeHandler(dsr)
}

type noopAuditSink struct{}

func (noopAuditSink) Emit(context.Context, iface.AuditEvent) {}

// TestEraseGated_PolicyOff_Returns403 confirms the client surface
// rejects /v1/me/dsr/erase with 403 when the policy gate denies. The
// gate is the only thing standing between a client user and an
// irreversible erase, so this test pins it explicitly.
func TestEraseGated_PolicyOff_Returns403(t *testing.T) {
	h := newGatedHandler(t)
	identity := testkit.NewIdentity("u-1", "u@example.com", "operator")
	ctx := identity.ContextFor(context.Background(), "-")

	gate := SelfDeletionGate(func(_ context.Context) bool { return false })

	_, err := h.EraseGated(ctx, gate, &struct{}{})
	if err == nil {
		t.Fatalf("expected 403 from gate, got nil")
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != 403 {
		t.Fatalf("expected huma 403 error, got %v", err)
	}
}

// TestEraseGated_PolicyOn_ProceedsToErase verifies the gate doesn't
// short-circuit a permitted call — the wrapped Erase actually runs
// and returns the producers' purge results.
func TestEraseGated_PolicyOn_ProceedsToErase(t *testing.T) {
	h := newGatedHandler(t)
	identity := testkit.NewIdentity("u-2", "ok@example.com", "operator")
	ctx := identity.ContextFor(context.Background(), "-")

	gate := SelfDeletionGate(func(_ context.Context) bool { return true })

	out, err := h.EraseGated(ctx, gate, &struct{}{})
	if err != nil {
		t.Fatalf("policy-on must let erase run, got %v", err)
	}
	if out == nil || out.Body.TotalRows == 0 {
		t.Fatalf("expected non-empty erase result, got %+v", out)
	}
}

// TestEraseGated_NilGate_Permits matches the documented contract: a
// nil gate means "no client-side restriction is wired" → the wrapper
// behaves like raw Erase. Compliance/module.go only mounts this route
// when the policy is non-nil, so this is a defense-in-depth check.
func TestEraseGated_NilGate_Permits(t *testing.T) {
	h := newGatedHandler(t)
	identity := testkit.NewIdentity("u-3", "nilgate@example.com", "operator")
	ctx := identity.ContextFor(context.Background(), "-")

	out, err := h.EraseGated(ctx, nil, &struct{}{})
	if err != nil {
		t.Fatalf("nil gate must not block, got %v", err)
	}
	if out == nil {
		t.Fatalf("expected non-nil output")
	}
}
