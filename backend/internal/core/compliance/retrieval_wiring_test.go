package compliance

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type fakeRetrieval struct{ audit iface.AuditSink }

func (f *fakeRetrieval) SetAuditSink(s iface.AuditSink) { f.audit = s }

func TestRetrievalService_ReceivesAuditSink(t *testing.T) {
	reg := module.NewServiceRegistry()
	rs := &fakeRetrieval{}
	reg.Register(module.ServiceRetrieval, rs)
	publishAuditSink(reg, nopSink{})
	if rs.audit == nil {
		t.Fatal("retrieval service not wired with the audit sink")
	}
}

func TestRetrievalService_AbsentIsTolerated(t *testing.T) {
	publishAuditSink(module.NewServiceRegistry(), nopSink{}) // must not panic
}
