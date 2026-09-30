package compliance

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type authAuditReceiver struct {
	sink  iface.AuditSink
	calls int
}

func (r *authAuditReceiver) SetAuditSink(sink iface.AuditSink) {
	r.calls++
	r.sink = sink
}

type auditSinkStub struct{}

func (*auditSinkStub) Emit(context.Context, iface.AuditEvent) {}

func TestModule_WiresAuditSinkToAuthService(t *testing.T) {
	registry := module.NewServiceRegistry()
	receiver := &authAuditReceiver{}
	registry.Register(module.ServiceAuthService, receiver)
	wantSink := &auditSinkStub{}
	publishAuditSink(registry, wantSink)
	sink, ok := module.GetTyped[iface.AuditSink](registry, module.ServiceAuditSink)
	if !ok || sink == nil {
		t.Fatal("compliance did not publish its sink")
	}
	if sink != wantSink || receiver.calls != 1 || receiver.sink != sink {
		t.Fatalf("auth sink wired %d times, matching published sink = %v; want once with the published sink", receiver.calls, receiver.sink == sink)
	}
}
