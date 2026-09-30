package compliance

import (
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type authAuditReceiver struct {
	sink  iface.AuditSink
	calls int
}

func (r *authAuditReceiver) SetAuditSink(sink iface.AuditSink) {
	r.calls++
	r.sink = sink
}

func TestModule_WiresAuditSinkToAuthService(t *testing.T) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatal(err)
	}
	registry := module.NewServiceRegistry()
	receiver := &authAuditReceiver{}
	registry.Register(module.ServiceAuthService, receiver)
	m := NewModule()
	if err := m.Init(&module.Dependencies{
		DB: client.Database("test"), Services: registry,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}); err != nil {
		t.Fatal(err)
	}
	sink, ok := module.GetTyped[iface.AuditSink](registry, module.ServiceAuditSink)
	if !ok || sink == nil {
		t.Fatal("compliance did not publish its sink")
	}
	if receiver.calls != 1 || receiver.sink != sink {
		t.Fatalf("auth sink wired %d times, matching published sink = %v; want once with the published sink", receiver.calls, receiver.sink == sink)
	}
}
