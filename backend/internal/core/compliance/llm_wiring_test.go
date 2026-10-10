package compliance

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type fakeGateway struct {
	kms   iface.KMSProvider
	audit iface.AuditSink
}

func (f *fakeGateway) SetKMSProvider(k iface.KMSProvider) { f.kms = k }
func (f *fakeGateway) SetAuditSink(s iface.AuditSink)     { f.audit = s }

type nopSink struct{}

func (nopSink) Emit(context.Context, iface.AuditEvent) {}

type nopKMS struct{}

func (nopKMS) CreateKey(context.Context, string) (string, error)       { return "k", nil }
func (nopKMS) Encrypt(context.Context, string, []byte) ([]byte, error) { return nil, nil }
func (nopKMS) Decrypt(context.Context, string, []byte) ([]byte, error) { return nil, nil }
func (nopKMS) DeleteKey(context.Context, string) error                 { return nil }

func TestLLMGateway_ReceivesKMSAndAuditSink(t *testing.T) {
	reg := module.NewServiceRegistry()
	gw := &fakeGateway{}
	reg.Register(module.ServiceLLMGateway, gw)

	publishKMSProvider(reg, nopKMS{})
	publishAuditSink(reg, nopSink{})

	if gw.kms == nil || gw.audit == nil {
		t.Fatalf("llm gateway not wired: kms=%v audit=%v", gw.kms != nil, gw.audit != nil)
	}
}

func TestLLMGateway_AbsentIsTolerated(t *testing.T) {
	reg := module.NewServiceRegistry()
	publishKMSProvider(reg, nopKMS{}) // must not panic
	publishAuditSink(reg, nopSink{})
}
