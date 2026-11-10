package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/internal/core/compliance/services/exporttest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const subjA = "7c1e5b3a-4f0d-4a2e-9b8c-1d2e3f4a5b6c"

func withSubject(dsr *DSRService, known ...string) {
	dsr.SetSubjectLookup(func(_ context.Context, id string) (bool, error) {
		for _, k := range known {
			if k == id {
				return true, nil
			}
		}
		return false, nil
	})
}

func TestOpenRefusesUnknownSubjectAndLowerCasesKnownOne(t *testing.T) {
	t.Parallel()
	svc, dsr, repo := approvalSvc(1, &stubProducer{subject: "user"})
	withSubject(dsr, subjA)
	ctx := context.Background()
	if _, err := svc.Open(ctx, OpenInput{UserUUID: "00000000-0000-4000-8000-000000000000", Reason: "r", RequestedBy: "op-a", Mode: iface.EraseAnonymize}); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("unknown subject: %v", err)
	}
	if rows, _ := repo.ListByStatus(ctx); len(rows) != 0 {
		t.Fatalf("a row was written for an unknown subject: %+v", rows)
	}
	req, err := svc.Open(ctx, OpenInput{UserUUID: " " + strings.ToUpper(subjA) + " ", Reason: "r", RequestedBy: "op-a", Mode: iface.EraseAnonymize})
	if err != nil || req.UserUUID != subjA {
		t.Fatalf("known upper-case subject: %+v %v", req, err)
	}
}

func TestOpenSelfCheckPrecedesSubjectHoldAndRetention(t *testing.T) {
	t.Parallel()
	svc, dsr, _ := approvalSvc(1, &stubProducer{subject: "user"})
	withSubject(dsr) // nobody exists
	_, err := svc.Open(context.Background(), OpenInput{UserUUID: strings.ToUpper(subjA), Reason: "r", RequestedBy: subjA, Mode: iface.EraseHardDelete})
	if !errors.Is(err, ErrErasureSelfApproval) {
		t.Fatalf("err = %v, want self approval before the lookup", err)
	}
}

func TestSubjectCannotRetryAnIncompleteRequest(t *testing.T) {
	t.Parallel()
	svc, _, repo := approvalSvc(1, &stubProducer{subject: "user"})
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		Progress: []models.ErasureProducerProgress{pendingProgress("user")}})
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "u-1"); !errors.Is(err, ErrErasureSelfApproval) {
		t.Fatalf("retry by the subject: %v", err)
	}
	if row := repo.Row("r-1"); row.Attempts != 0 || row.LastRunBy != "" {
		t.Fatalf("row touched: %+v", row)
	}
}

func TestOperatorRetryNamesTheRetrierOnReceiptAndAudit(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete, ResolvedBy: "op-first",
		Mode: "hard_delete", Attempts: 4, RequestedAt: erasureT0.Add(-time.Hour),
		Progress: []models.ErasureProducerProgress{pendingProgress("user")}})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user"})

	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "op-retry"); err != nil {
		t.Fatal(err)
	}
	var retried, receipt *iface.AuditEvent
	for i := range sink.events {
		switch sink.events[i].Action {
		case "gdpr.dsr.erasure_retried":
			retried = &sink.events[i]
		case "gdpr.dsr.erasure_receipt":
			receipt = &sink.events[i]
		}
	}
	if retried == nil || retried.ActorUserID != "op-retry" || retried.ActorType != "user" || retried.ResourceID != "r-1" ||
		retried.Metadata["subjectUserUuid"] != "u-9" || retried.Metadata["attempts"] != 4 {
		t.Fatalf("retried event = %+v", retried)
	}
	if receipt == nil || receipt.ActorUserID != "op-retry" || receipt.Metadata["executedBy"] != "op-first" || receipt.Metadata["retriedBy"] != "op-retry" {
		t.Fatalf("receipt event = %+v", receipt)
	}
	rc, err := svc.Receipt(context.Background(), "r-1")
	if err != nil || rc.ExecutedBy != "op-first" || rc.RetriedBy != "op-retry" {
		t.Fatalf("receipt = %+v %v", rc, err)
	}
}

func TestFirstExecutionHasNoRetriedBy(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending, RequestedAt: erasureT0})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user"})
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "op-1"); err != nil {
		t.Fatal(err)
	}
	for _, e := range sink.events {
		if e.Action == "gdpr.dsr.erasure_retried" {
			t.Fatal("a first execution is not a retry")
		}
	}
	if rc, _ := svc.Receipt(context.Background(), "r-1"); rc.RetriedBy != "" {
		t.Fatalf("retriedBy = %q", rc.RetriedBy)
	}
}

func TestSystemActorsAreAuditedAsSystem(t *testing.T) {
	t.Parallel()
	for actor, want := range map[string]string{"": "system", "system:retention": "system", erasureWorkerActor: "system", "op-1": "user"} {
		if got := actorTypeOf(actor); got != want {
			t.Errorf("actorTypeOf(%q) = %q, want %q", actor, got, want)
		}
	}
	// A retention-sourced erasure completes with a system receipt.
	repo := erasuretest.NewMemRepo()
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user"})
	if _, err := svc.EraseNow(context.Background(), "u-9", "", models.ErasureSourceRetention, "system:retention", iface.EraseHardDelete); err != nil {
		t.Fatal(err)
	}
	evs := receiptEvents(sink)
	if len(evs) != 1 || evs[0].ActorType != "system" || evs[0].ActorUserID != "system:retention" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestRejectMasksNoteAuditsAndRequiresNote(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending, RequestedAt: erasureT0})
	svc, sink := newCloseSvc(repo)
	ctx := context.Background()
	for _, bad := range []string{"", "   ", strings.Repeat("x", 501)} {
		if err := svc.Reject(ctx, "r-1", "op-1", bad); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("note %q: %v", bad, err)
		}
	}
	if err := svc.Reject(ctx, "r-1", "op-1", "write to jane.doe@example.com"); err != nil {
		t.Fatal(err)
	}
	row := repo.Row("r-1")
	if row.Status != models.ErasureRequestRejected || strings.Contains(row.ResolutionNote, "jane.doe@example.com") || row.ResolutionNote == "" {
		t.Fatalf("row = %+v", row)
	}
	var found bool
	for _, e := range sink.events {
		if e.Action == "gdpr.dsr.erasure_rejected" {
			found = e.ActorUserID == "op-1" && e.ResourceID == "r-1" && e.Metadata["subjectUserUuid"] == "u-9"
		}
	}
	if !found {
		t.Fatalf("events = %+v", sink.events)
	}
}

func TestExportCreateUnknownSubjectAndLowerCase(t *testing.T) {
	f := newExportFixture(t, true, &stubProducer{subject: "user", exportData: "x"})
	f.svc.dsr.SetSubjectLookup(func(_ context.Context, id string) (bool, error) { return id == subjA, nil })
	if _, err := f.svc.Create(context.Background(), "00000000-0000-4000-8000-000000000000", "json", "r", "op"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(f.store.Objects) != 0 {
		t.Fatal("nothing may be stored")
	}
	row, err := f.svc.Create(context.Background(), strings.ToUpper(subjA), "json", "r", "op")
	if err != nil || row.UserUUID != subjA {
		t.Fatalf("row = %+v %v", row, err)
	}
}

// A marker written a little before the export began (another replica's clock
// runs behind) still counts as racing it; one clearly older does not.
func TestCreateToleratesClockSkewOnTheErasureMarker(t *testing.T) {
	for name, tc := range map[string]struct {
		before  time.Duration
		wantErr bool
	}{
		"1s behind is racing":     {time.Second, true},
		"1h behind is an old one": {time.Hour, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newExportFixture(t, true)
			m := exporttest.NewMemMarkers()
			_ = m.Mark(context.Background(), "subj", f.now.Add(-tc.before))
			f.svc.SetErasureGuard(m, func(context.Context, string) (bool, error) { return false, nil })
			_, err := f.svc.Create(context.Background(), "subj", "json", "r", "op")
			if errors.Is(err, ErrExportErasureInProgress) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tc.wantErr && (len(f.store.Objects) != 0 || len(f.repo.Rows) != 0) {
				t.Fatal("racing export must be removed")
			}
		})
	}
}

func TestPreviewUnknownSubjectIsNotAuditedAndLowerCases(t *testing.T) {
	dsr, _ := newDSRService(&stubProducer{subject: "user"})
	withSubject(dsr, subjA)
	sink := &capturingAuditSink{}
	svc := NewDSRPreviewService(dsr, fakeHolds{}, &fakeRequests{}, sink, nil)
	if _, err := svc.Preview(context.Background(), "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("an unknown subject was audited: %+v", sink.events)
	}
	p, err := svc.Preview(context.Background(), strings.ToUpper(subjA))
	if err != nil || p.UserUUID != subjA || len(sink.events) != 1 {
		t.Fatalf("preview = %+v %v events=%d", p, err, len(sink.events))
	}
}
