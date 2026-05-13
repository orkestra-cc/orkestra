package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// stubProducer is a test double for iface.PIIProducer. Lets each test
// pin expected behavior without touching Mongo.
type stubProducer struct {
	subject      string
	exportData   any
	exportErr    error
	purgeResult  iface.PurgeResult
	purgeErr     error
	exportCalls  int
	purgeCalls   int
	lastUserUUID string
}

func (s *stubProducer) Subject() string { return s.subject }
func (s *stubProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	s.exportCalls++
	s.lastUserUUID = userUUID
	return s.exportData, s.exportErr
}
func (s *stubProducer) PurgePersonalData(ctx context.Context, userUUID string) (iface.PurgeResult, error) {
	s.purgeCalls++
	s.lastUserUUID = userUUID
	return s.purgeResult, s.purgeErr
}

// capturingAuditSink records every audit event for assertion. Satisfies
// iface.AuditSink.
type capturingAuditSink struct {
	events []iface.AuditEvent
}

func (c *capturingAuditSink) Emit(ctx context.Context, e iface.AuditEvent) {
	c.events = append(c.events, e)
}

func newDSRService(producers ...iface.PIIProducer) (*DSRService, *capturingAuditSink) {
	reg := iface.NewPIIProducerRegistry()
	for _, p := range producers {
		reg.Register(p)
	}
	sink := &capturingAuditSink{}
	return NewDSRService(reg, sink, slog.Default()), sink
}

// TestExportBundlesProducerData pins the happy-path behavior: each
// producer's non-nil payload lands under its Subject() key and metadata
// is always present.
func TestExportBundlesProducerData(t *testing.T) {
	t.Parallel()

	userP := &stubProducer{subject: "user", exportData: map[string]any{"email": "a@b.com"}}
	authP := &stubProducer{subject: "auth", exportData: map[string]any{"sessions": []any{}}}
	svc, sink := newDSRService(userP, authP)

	res, err := svc.Export(context.Background(), "u-123")
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if _, ok := res.Bundle["metadata"]; !ok {
		t.Fatal("bundle missing metadata key")
	}
	if _, ok := res.Bundle["user"]; !ok {
		t.Fatal("bundle missing user key")
	}
	if _, ok := res.Bundle["auth"]; !ok {
		t.Fatal("bundle missing auth key")
	}
	if len(res.Producers) != 2 {
		t.Fatalf("expected 2 producers in result, got %d", len(res.Producers))
	}
	if userP.exportCalls != 1 || authP.exportCalls != 1 {
		t.Fatalf("each producer should have been called once; got user=%d auth=%d",
			userP.exportCalls, authP.exportCalls)
	}
	if len(sink.events) != 1 || sink.events[0].Action != "gdpr.dsr.exported" {
		t.Fatalf("expected one gdpr.dsr.exported audit event, got %+v", sink.events)
	}
}

// TestExportSkipsNilPayloads pins that a producer returning (nil, nil)
// — meaning "no data held" — is excluded from the bundle but still
// counted in Producers for audit provenance.
func TestExportSkipsNilPayloads(t *testing.T) {
	t.Parallel()

	empty := &stubProducer{subject: "empty", exportData: nil}
	full := &stubProducer{subject: "full", exportData: map[string]any{"x": 1}}
	svc, _ := newDSRService(empty, full)

	res, err := svc.Export(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if _, ok := res.Bundle["empty"]; ok {
		t.Fatal("empty producer should have been excluded from bundle")
	}
	if _, ok := res.Bundle["full"]; !ok {
		t.Fatal("full producer missing from bundle")
	}
	if len(res.Producers) != 2 {
		t.Fatalf("both producers should appear in Producers list; got %v", res.Producers)
	}
}

// TestExportCapturesProducerErrors pins that a producer error is
// reported in ExportResult.Errors rather than aborting the pipeline.
// This is the partial-bundle contract.
func TestExportCapturesProducerErrors(t *testing.T) {
	t.Parallel()

	bad := &stubProducer{subject: "bad", exportErr: errors.New("mongo down")}
	good := &stubProducer{subject: "good", exportData: map[string]any{"x": 1}}
	svc, _ := newDSRService(bad, good)

	res, err := svc.Export(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("Export should not return an error on per-producer failure; got %v", err)
	}
	if msg, ok := res.Errors["bad"]; !ok || msg == "" {
		t.Fatalf("expected 'bad' subject in Errors; got %+v", res.Errors)
	}
	if _, ok := res.Bundle["good"]; !ok {
		t.Fatal("surviving producer's data should still be bundled")
	}
}

// TestEraseAggregatesPurgeResults pins the erase pipeline contract:
// each producer's PurgeResult is indexed under its subject, the audit
// event's metadata carries the per-subject summary, and errors are
// surfaced without aborting the remaining producers.
func TestEraseAggregatesPurgeResults(t *testing.T) {
	t.Parallel()

	user := &stubProducer{
		subject:     "user",
		purgeResult: iface.PurgeResult{RowsDeleted: 1, Collections: []string{"users"}},
	}
	auth := &stubProducer{
		subject:     "auth",
		purgeResult: iface.PurgeResult{RowsDeleted: 5, Collections: []string{"auth_sessions", "auth_refresh_tokens"}},
	}
	svc, sink := newDSRService(user, auth)

	res, err := svc.Erase(context.Background(), "u-42")
	if err != nil {
		t.Fatalf("Erase returned error: %v", err)
	}
	if got := res.Purged["user"].RowsDeleted; got != 1 {
		t.Fatalf("user purge rows = %d; want 1", got)
	}
	if got := res.Purged["auth"].RowsDeleted; got != 5 {
		t.Fatalf("auth purge rows = %d; want 5", got)
	}
	if user.purgeCalls != 1 || auth.purgeCalls != 1 {
		t.Fatalf("each producer should have been called once; got user=%d auth=%d",
			user.purgeCalls, auth.purgeCalls)
	}
	if user.lastUserUUID != "u-42" {
		t.Fatalf("producer received wrong userUUID: %q", user.lastUserUUID)
	}
	if len(sink.events) != 1 || sink.events[0].Action != "gdpr.dsr.erased" {
		t.Fatalf("expected one gdpr.dsr.erased audit event, got %+v", sink.events)
	}
	meta, ok := sink.events[0].Metadata["totalRows"].(int)
	if !ok || meta != 6 {
		t.Fatalf("erase audit metadata.totalRows = %v; want 6", sink.events[0].Metadata["totalRows"])
	}
}
