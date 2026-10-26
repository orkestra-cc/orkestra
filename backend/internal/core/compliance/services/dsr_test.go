package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
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

	// purgeResults/purgeErrs, when set, give the result of the Nth purge call
	// (falling back to purgeResult/purgeErr once exhausted).
	purgeResults []iface.PurgeResult
	purgeErrs    []error
}

// keyedStub adds iface.PIIProducerKeyer to a stubProducer.
type keyedStub struct {
	*stubProducer
	key string
}

func (k keyedStub) ProducerKey() string { return k.key }

func (s *stubProducer) Subject() string { return s.subject }
func (s *stubProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	s.exportCalls++
	s.lastUserUUID = userUUID
	return s.exportData, s.exportErr
}
func (s *stubProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	i := s.purgeCalls
	s.purgeCalls++
	s.lastUserUUID = userUUID
	res, err := s.purgeResult, s.purgeErr
	if i < len(s.purgeResults) {
		res = s.purgeResults[i]
	}
	if i < len(s.purgeErrs) {
		err = s.purgeErrs[i]
	}
	return res, err
}

// fakeHoldChecker is a test double for LegalHoldChecker. Lets erase tests
// pin the legal-hold gate without the legal-hold subsystem.
type fakeHoldChecker struct {
	held     bool
	err      error
	calls    int
	lastUUID string
}

func (f *fakeHoldChecker) IsHeld(_ context.Context, userUUID string) (bool, error) {
	f.calls++
	f.lastUUID = userUUID
	return f.held, f.err
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

	res, err := svc.Erase(context.Background(), "u-42", iface.EraseHardDelete)
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

// TestEraseBlockedByLegalHold pins the legal-hold gate: when the subject is
// held, Erase returns ErrLegalHoldActive, runs no producer, and emits no
// "erased" audit row (the blocked attempt is the caller's to log).
func TestEraseBlockedByLegalHold(t *testing.T) {
	t.Parallel()

	user := &stubProducer{subject: "user"}
	svc, sink := newDSRService(user)
	svc.SetLegalHoldChecker(&fakeHoldChecker{held: true})

	_, err := svc.Erase(context.Background(), "u-held", iface.EraseHardDelete)
	if !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("expected ErrLegalHoldActive, got %v", err)
	}
	if user.purgeCalls != 0 {
		t.Fatalf("no producer should run when the subject is held; got %d calls", user.purgeCalls)
	}
	if len(sink.events) != 0 {
		t.Fatalf("no audit row should be emitted on a blocked erase; got %+v", sink.events)
	}
}

// TestEraseProceedsWhenNotHeld pins that an inactive hold lets the pipeline
// run and the checker is consulted with the right subject.
func TestEraseProceedsWhenNotHeld(t *testing.T) {
	t.Parallel()

	user := &stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}}
	svc, sink := newDSRService(user)
	checker := &fakeHoldChecker{held: false}
	svc.SetLegalHoldChecker(checker)

	if _, err := svc.Erase(context.Background(), "u-ok", iface.EraseHardDelete); err != nil {
		t.Fatalf("Erase returned error: %v", err)
	}
	if checker.calls != 1 || checker.lastUUID != "u-ok" {
		t.Fatalf("hold checker should be called once with u-ok; calls=%d uuid=%q", checker.calls, checker.lastUUID)
	}
	if user.purgeCalls != 1 {
		t.Fatalf("producer should run when not held; got %d calls", user.purgeCalls)
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected one erased audit row; got %+v", sink.events)
	}
}

// TestErasePropagatesHoldCheckerError pins that a checker failure aborts
// erasure with the underlying error rather than silently erasing.
func TestErasePropagatesHoldCheckerError(t *testing.T) {
	t.Parallel()

	user := &stubProducer{subject: "user"}
	svc, _ := newDSRService(user)
	boom := errors.New("mongo down")
	svc.SetLegalHoldChecker(&fakeHoldChecker{err: boom})

	_, err := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the hold-checker error to propagate, got %v", err)
	}
	if user.purgeCalls != 0 {
		t.Fatalf("no producer should run when the hold check errors; got %d", user.purgeCalls)
	}
}

// TestEraseModeLabelInAudit pins that the erase mode is rendered into the
// audit metadata so SOC2 review can tell anonymize from hard-delete.
func TestEraseModeLabelInAudit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode iface.EraseMode
		want string
	}{
		{iface.EraseAnonymize, "anonymize"},
		{iface.EraseHardDelete, "hard_delete"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			svc, sink := newDSRService(&stubProducer{subject: "user"})
			if _, err := svc.Erase(context.Background(), "u-1", tc.mode); err != nil {
				t.Fatalf("Erase: %v", err)
			}
			if got := sink.events[0].Metadata["mode"]; got != tc.want {
				t.Fatalf("audit metadata.mode = %v; want %q", got, tc.want)
			}
		})
	}
}

// TestEraseNilSinkIsSafe pins the nil-sink contract: a DSR service wired
// without an audit sink must still erase without panicking.
func TestEraseNilSinkIsSafe(t *testing.T) {
	t.Parallel()

	reg := iface.NewPIIProducerRegistry()
	reg.Register(&stubProducer{subject: "user"})
	svc := NewDSRService(reg, nil, slog.Default())

	if _, err := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete); err != nil {
		t.Fatalf("Erase with nil sink should not error: %v", err)
	}
	if _, err := svc.Export(context.Background(), "u-1"); err != nil {
		t.Fatalf("Export with nil sink should not error: %v", err)
	}
}

func TestEraseTracksProducersWithTheSameSubjectSeparately(t *testing.T) {
	t.Parallel()
	op := &stubProducer{subject: "auth", purgeResult: iface.PurgeResult{RowsDeleted: 2}}
	cl := &stubProducer{subject: "auth", purgeResult: iface.PurgeResult{RowsDeleted: 1}, purgeErr: errors.New("mongo down")}
	svc, _ := newDSRService(op, cl)

	res, err := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete)
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if res.Progress[0].Key != "auth" || res.Progress[1].Key != "auth#2" {
		t.Fatalf("keys = %q, %q", res.Progress[0].Key, res.Progress[1].Key)
	}
	if res.Progress[0].Outcome != models.ProducerOutcomeDone || res.Progress[1].Outcome != models.ProducerOutcomeFailed {
		t.Fatalf("outcomes = %+v", res.Progress)
	}
	if res.Purged["auth"].RowsDeleted != 2 || res.Purged["auth#2"].RowsDeleted != 1 {
		t.Fatalf("purged = %+v", res.Purged)
	}
	if res.Errors["auth#2"] == "" || res.Complete() {
		t.Fatalf("errors=%+v complete=%v", res.Errors, res.Complete())
	}
}

func TestEraseUsesProducerKeyWhenDeclared(t *testing.T) {
	t.Parallel()
	op := keyedStub{&stubProducer{subject: "auth"}, "auth.operator"}
	cl := keyedStub{&stubProducer{subject: "auth"}, "auth.client"}
	svc, _ := newDSRService(op, cl)

	res, err := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete)
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if res.Progress[0].Key != "auth.operator" || res.Progress[1].Key != "auth.client" {
		t.Fatalf("progress = %+v", res.Progress)
	}
	if res.Progress[0].Subject != "auth" || res.Progress[1].Subject != "auth" {
		t.Fatalf("subjects = %+v", res.Progress)
	}
}

func TestEraseKeepsPartialResultOfFailedProducer(t *testing.T) {
	t.Parallel()
	llm := &stubProducer{subject: "llm",
		purgeResults: []iface.PurgeResult{{RowsDeleted: 3}, {RowsDeleted: 1}},
		purgeErrs:    []error{errors.New("usage anonymize failed"), nil}}
	user := &stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}}
	svc, _ := newDSRService(user, llm)

	first, _ := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete)
	if first.Purged["llm"].RowsDeleted != 3 {
		t.Fatalf("partial result lost: %+v", first.Purged)
	}
	p := first.Progress[1]
	if p.Key != "llm" || p.Outcome != models.ProducerOutcomeFailed || p.RowsDeleted != 3 || p.Attempts != 1 {
		t.Fatalf("first.Progress[1] = %+v", p)
	}
	second, _ := svc.EraseRemaining(context.Background(), "u-1", iface.EraseHardDelete, first.Progress)
	if user.purgeCalls != 1 || llm.purgeCalls != 2 {
		t.Fatalf("calls user=%d llm=%d", user.purgeCalls, llm.purgeCalls)
	}
	p = second.Progress[1]
	if p.Key != "llm" || p.Outcome != models.ProducerOutcomeDone || p.RowsDeleted != 4 || p.Attempts != 2 {
		t.Fatalf("second.Progress[1] = %+v", p)
	}
	if !second.Complete() {
		t.Fatal("second run should be complete")
	}
}

func TestErasePendingProducerIsNotDone(t *testing.T) {
	t.Parallel()
	svc, _ := newDSRService(&stubProducer{subject: "retrieval", purgeResult: iface.PurgeResult{Pending: true}})
	res, err := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete)
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if res.Progress[0].Outcome != models.ProducerOutcomePending || res.Complete() || len(res.Errors) != 0 {
		t.Fatalf("progress=%+v errors=%+v", res.Progress, res.Errors)
	}
}

func TestEraseRemainingReportsPriorKeyNoLongerRegistered(t *testing.T) {
	t.Parallel()
	prior := []models.ErasureProducerProgress{{Key: "gone", Subject: "gone", Outcome: models.ProducerOutcomeFailed, Attempts: 1}}
	svc, _ := newDSRService(&stubProducer{subject: "user"})
	res, err := svc.EraseRemaining(context.Background(), "u-1", iface.EraseHardDelete, prior)
	if err != nil {
		t.Fatalf("EraseRemaining: %v", err)
	}
	if len(res.Progress) != 2 || res.Progress[0].Key != "user" || res.Progress[0].Outcome != models.ProducerOutcomeDone ||
		res.Progress[1].Key != "gone" || res.Progress[1].Outcome != models.ProducerOutcomeFailed || res.Progress[1].Attempts != 1 {
		t.Fatalf("progress = %+v", res.Progress)
	}
	if res.Complete() {
		t.Fatal("must not be complete")
	}
}

func TestEraseAuditRecordsOutcomes(t *testing.T) {
	t.Parallel()
	svc, sink := newDSRService(
		&stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}},
		&stubProducer{subject: "retrieval", purgeResult: iface.PurgeResult{Pending: true}},
	)
	if _, err := svc.Erase(context.Background(), "u-1", iface.EraseHardDelete); err != nil {
		t.Fatalf("Erase: %v", err)
	}
	md := sink.events[0].Metadata
	if md["complete"] != false || md["pendingCount"] != 1 || md["errorCount"] != 0 {
		t.Fatalf("metadata = %+v", md)
	}
	per := md["perSubject"].(map[string]any)
	if per["retrieval"].(map[string]any)["outcome"] != "pending" {
		t.Fatalf("perSubject = %+v", per)
	}
}

func TestCheckLegalHold(t *testing.T) {
	t.Parallel()
	svc, _ := newDSRService()
	if err := svc.CheckLegalHold(context.Background(), "u"); err != nil {
		t.Fatalf("no checker: %v", err)
	}
	svc.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	if err := svc.CheckLegalHold(context.Background(), "u"); !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("held: %v", err)
	}
	boom := errors.New("boom")
	svc.SetLegalHoldChecker(&fakeHoldChecker{err: boom})
	if err := svc.CheckLegalHold(context.Background(), "u"); !errors.Is(err, boom) {
		t.Fatalf("checker error: %v", err)
	}
}

func TestExportIncludesBothAuthTiers(t *testing.T) {
	t.Parallel()
	op := keyedStub{&stubProducer{subject: "auth", exportData: map[string]any{"sessions": []string{"op-s1"}}}, "auth.operator"}
	cl := keyedStub{&stubProducer{subject: "auth", exportData: map[string]any{"sessions": []string{"cl-s1"}}}, "auth.client"}
	svc, _ := newDSRService(op, cl)

	res, err := svc.Export(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, ok := res.Bundle["auth"]; ok {
		t.Fatal("bundle must not carry the bare subject key")
	}
	if res.Bundle["auth.operator"].(map[string]any)["sessions"].([]string)[0] != "op-s1" ||
		res.Bundle["auth.client"].(map[string]any)["sessions"].([]string)[0] != "cl-s1" {
		t.Fatalf("bundle = %+v", res.Bundle)
	}
	if len(res.Producers) != 2 || res.Producers[0] != "auth.operator" || res.Producers[1] != "auth.client" {
		t.Fatalf("producers = %v", res.Producers)
	}
	if res.Bundle["metadata"].(map[string]any)["version"] != "2" {
		t.Fatalf("metadata = %+v", res.Bundle["metadata"])
	}
}

func TestExportKeysUnkeyedDuplicatesWithSuffix(t *testing.T) {
	t.Parallel()
	a := &stubProducer{subject: "auth", exportData: map[string]any{"n": 1}}
	b := &stubProducer{subject: "auth", exportErr: errors.New("down")}
	svc, _ := newDSRService(a, b)
	res, err := svc.Export(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, ok := res.Bundle["auth"]; !ok {
		t.Fatalf("bundle = %+v", res.Bundle)
	}
	if res.Errors["auth#2"] == "" {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

func TestRegisteredKeys(t *testing.T) {
	t.Parallel()
	svc, _ := newDSRService(
		&stubProducer{subject: "user"},
		keyedStub{&stubProducer{subject: "auth"}, "auth.operator"},
		keyedStub{&stubProducer{subject: "auth"}, "auth.client"},
	)
	got := svc.RegisteredKeys()
	if len(got) != 3 || !got["user"] || !got["auth.operator"] || !got["auth.client"] {
		t.Fatalf("keys = %v", got)
	}
}

func TestParseEraseMode(t *testing.T) {
	t.Parallel()
	if parseEraseMode("anonymize") != iface.EraseAnonymize || parseEraseMode("hard_delete") != iface.EraseHardDelete || parseEraseMode("") != iface.EraseHardDelete {
		t.Fatal("parseEraseMode mismatch")
	}
}
