package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestRequiredPersistedModules_AuthIsRequired(t *testing.T) {
	found := false
	for _, n := range requiredPersistedModules {
		if n == "auth" {
			found = true
		}
	}
	if !found {
		t.Fatal("auth must be a required persisted config: its strict password-policy reader depends on it")
	}
}

type noopSink struct{}

func (noopSink) Emit(context.Context, iface.AuditEvent) {}

func TestWireModuleAdminAudit_RequiresTheSink(t *testing.T) {
	h := module.NewModuleAdminHandler(nil, module.NewModuleRegistry(slog.Default()))
	if err := wireModuleAdminAudit(h, module.NewServiceRegistry()); err == nil {
		t.Fatal("wiring without a registered audit sink must fail — the in-tree server never runs unaudited")
	}
	svcs := module.NewServiceRegistry()
	svcs.Register(module.ServiceAuditSink, iface.AuditSink(noopSink{}))
	if err := wireModuleAdminAudit(h, svcs); err != nil {
		t.Fatalf("wiring with the sink: %v", err)
	}
}

func TestAdminActorResolver_ReadsContextWithoutEmail(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "u-1")
	ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, "someone@example.com")
	ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "t-1")
	ctx = ctxauth.WithTenantKind(ctx, "internal")
	ctx = ctxauth.WithClientIP(ctx, "203.0.113.9")
	a := adminActorResolver(ctx)
	if a.UserID != "u-1" || a.TenantID != "t-1" || a.TenantKind != "internal" || a.IP != "203.0.113.9" {
		t.Fatalf("actor = %+v", a)
	}
}
