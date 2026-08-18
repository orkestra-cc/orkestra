package iface

import (
	"context"
	"testing"
)

type stubSink struct{ got CRMActivityInput }

func (s *stubSink) RecordActivity(_ context.Context, in CRMActivityInput) error {
	s.got = in
	return nil
}

// Compile-time proof the contract is implementable, plus the field names
// consumers rely on.
func TestCRMActivitySinkShape(t *testing.T) {
	var sink CRMActivitySink = &stubSink{}
	err := sink.RecordActivity(context.Background(), CRMActivityInput{
		TenantUUID: "tenant-1",
		Email:      "billing@acme.test",
		Kind:       "payment_failed",
		Summary:    "Charge declined for invoice SUB-2026-000001",
		Metadata:   map[string]string{"invoiceNumber": "SUB-2026-000001"},
	})
	if err != nil {
		t.Fatalf("RecordActivity: %v", err)
	}
}
