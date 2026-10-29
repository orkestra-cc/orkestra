package services

import (
	"context"
	"sync"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Audit actions of the retrieval module. Metadata never carries a subject
// value, a query or a source id.
const (
	AuditErasureRecorded  = "retrieval.erasure.recorded"
	AuditErasureFinalized = "retrieval.erasure.finalized"
	AuditDiagnosticsRun   = "retrieval.diagnostics.run"
	AuditCorpusDropped    = "retrieval.corpus.dropped"
	AuditSearchLogRead    = "retrieval.searchlog.read"
)

// AuditRelay forwards retrieval audit events to the sink compliance publishes
// (iface.AuditSinkSetter). Before a sink is set, and when none is ever set,
// every emit is a no-op.
type AuditRelay struct {
	mu   sync.RWMutex
	sink iface.AuditSink
}

func NewAuditRelay() *AuditRelay { return &AuditRelay{} }

// SetAuditSink satisfies iface.AuditSinkSetter; nil is ignored.
func (a *AuditRelay) SetAuditSink(s iface.AuditSink) {
	if s == nil {
		return
	}
	a.mu.Lock()
	a.sink = s
	a.mu.Unlock()
}

func (a *AuditRelay) current() iface.AuditSink {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sink
}

// Emit records a successful action of the user in ctx (admin actions): tenant,
// tenant kind, actor, email and client IP are read from ctx.
func (a *AuditRelay) Emit(ctx context.Context, action, resourceType, resourceID string, meta map[string]any) {
	a.EmitOutcome(ctx, action, resourceType, resourceID, "success", meta)
}

// EmitOutcome records an action of the user in ctx with an explicit outcome
// ("success" or "failure"); Emit is EmitOutcome with "success".
func (a *AuditRelay) EmitOutcome(ctx context.Context, action, resourceType, resourceID, outcome string, meta map[string]any) {
	sink := a.current()
	if sink == nil {
		return
	}
	tenantID, _ := tenantrepo.CurrentTenantID(ctx)
	actor, _ := ctxauth.GetUserUUID(ctx)
	email, _ := ctxauth.GetUserEmail(ctx)
	ip, _ := ctxauth.GetClientIP(ctx)
	sink.Emit(ctx, iface.AuditEvent{
		TenantID: tenantID, TenantKind: tenantKind(ctx), ActorUserID: actor, ActorEmail: email, ActorType: "user",
		Action: action, ResourceType: resourceType, ResourceID: resourceID, Outcome: outcome, IPAddress: ip, Metadata: meta,
	})
}

// EmitSystem records a successful unattended action on an explicit tenant
// ("" for a global tombstone): ActorType "system" with no actor id.
func (a *AuditRelay) EmitSystem(ctx context.Context, tenantID, action, resourceType, resourceID string, meta map[string]any) {
	sink := a.current()
	if sink == nil {
		return
	}
	sink.Emit(ctx, iface.AuditEvent{
		TenantID: tenantID, TenantKind: tenantKind(ctx), ActorType: "system",
		Action: action, ResourceType: resourceType, ResourceID: resourceID, Outcome: "success", Metadata: meta,
	})
}

// tenantKind is the ctx tenant kind; every retrieval tenant is internal.
func tenantKind(ctx context.Context) string {
	if kind := tenantrepo.CurrentTenantKind(ctx); kind != "" {
		return kind
	}
	return "internal"
}
