package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func receiptEvents(sink *capturingAuditSink) []iface.AuditEvent {
	var out []iface.AuditEvent
	for _, e := range sink.events {
		if e.Action == "gdpr.dsr.erasure_receipt" {
			out = append(out, e)
		}
	}
	return out
}

func TestBuildErasureReceiptCompleted(t *testing.T) {
	t.Parallel()
	done1 := erasureT0.Add(time.Hour)
	row := &models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Source: models.ErasureSourceAdmin, RequestedBy: "op-1", Mode: "hard_delete",
		ResolvedBy: "op-2", Status: models.ErasureRequestCompleted, RequestedAt: erasureT0, ResolvedAt: &done1, Attempts: 2,
		Approvals: []models.ErasureApproval{{By: "op-1", At: erasureT0, Note: "why"}, {By: "op-2", At: done1, Note: "executed"}},
		Progress: []models.ErasureProducerProgress{
			{Key: "user", Outcome: models.ProducerOutcomeDone, RowsDeleted: 3},
			{Key: "auth.client", Outcome: models.ProducerOutcomeDone, RowsDeleted: 4, RowsAnonymized: 1},
		},
	}
	now := erasureT0.Add(2 * time.Hour)
	rc, err := BuildErasureReceipt(row, now)
	if err != nil {
		t.Fatalf("BuildErasureReceipt: %v", err)
	}
	if rc.RequestUUID != "r-1" || rc.SubjectUserUUID != "u-9" || rc.ExecutedBy != "op-2" || rc.RequestedBy != "op-1" ||
		rc.Source != "admin" || rc.Mode != "hard_delete" || rc.Attempts != 2 || rc.CompletedAt == nil || !rc.GeneratedAt.Equal(now) {
		t.Fatalf("receipt = %+v", rc)
	}
	if rc.TotalRowsDeleted != 7 || rc.TotalRowsAnonymized != 1 {
		t.Fatalf("totals = %d/%d", rc.TotalRowsDeleted, rc.TotalRowsAnonymized)
	}
	if len(rc.Producers) != 2 || rc.Producers[0].Key != "user" || rc.Producers[1].Key != "auth.client" {
		t.Fatalf("producers not in progress order: %+v", rc.Producers)
	}
	if len(rc.Approvals) != 2 || rc.Approvals[0].Note != "why" || rc.CloseNote != "" || rc.Abandoned == nil {
		t.Fatalf("approvals/notes = %+v", rc)
	}
}

func TestBuildErasureReceiptClosedPartial(t *testing.T) {
	t.Parallel()
	row := &models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestClosedPartial, ResolvedBy: "op-1",
		ResolutionNote: "module removed", Abandoned: []string{"gone"},
		Progress: []models.ErasureProducerProgress{done("user"), failed("gone")}}
	rc, err := BuildErasureReceipt(row, erasureT0)
	if err != nil {
		t.Fatal(err)
	}
	if rc.CloseNote != "module removed" || len(rc.Abandoned) != 1 || rc.Abandoned[0] != "gone" || rc.ExecutedBy != "op-1" || rc.Producers[1].Outcome != models.ProducerOutcomeFailed {
		t.Fatalf("receipt = %+v", rc)
	}
}

func TestBuildErasureReceiptRefusedForOpenAndRejected(t *testing.T) {
	t.Parallel()
	for _, st := range []string{models.ErasureRequestPending, models.ErasureRequestIncomplete, models.ErasureRequestRejected} {
		if _, err := BuildErasureReceipt(&models.ErasureRequest{Status: st}, erasureT0); !errors.Is(err, ErrErasureReceiptUnavailable) {
			t.Errorf("%s: err = %v; want ErrErasureReceiptUnavailable", st, err)
		}
	}
}

func TestReceiptServiceUnknownAndUnavailable(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", Status: models.ErasureRequestPending})
	svc, _ := newErasureSvc(repo)
	if _, err := svc.Receipt(context.Background(), "nope"); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := svc.Receipt(context.Background(), "r-1"); !errors.Is(err, ErrErasureReceiptUnavailable) {
		t.Fatalf("pending: %v", err)
	}
}

func TestRunEmitsReceiptOnceOnCompletionWithoutNotes(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending, RequestedAt: erasureT0,
		Approvals: []models.ErasureApproval{{By: "op-1", At: erasureT0, Note: "secret-ish note"}}})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 5}})

	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "op-2"); err != nil {
		t.Fatal(err)
	}
	evs := receiptEvents(sink)
	if len(evs) != 1 {
		t.Fatalf("receipt events = %d; want 1", len(evs))
	}
	ev := evs[0]
	if ev.ResourceID != "r-1" || ev.ActorUserID != "op-2" || ev.Metadata["subjectUserUuid"] != "u-9" ||
		ev.Metadata["totalRowsDeleted"] != 5 || ev.Metadata["status"] != models.ErasureRequestCompleted {
		t.Fatalf("event = %+v", ev)
	}
	for _, a := range ev.Metadata["approvals"].([]map[string]any) {
		if _, has := a["note"]; has {
			t.Fatal("approval note leaked into the audit metadata")
		}
	}
	if _, has := ev.Metadata["closeNote"]; has {
		t.Fatal("closeNote leaked")
	}
}

func TestRunNoReceiptWhenIncompleteLostLeaseOrSettleFailed(t *testing.T) {
	t.Parallel()
	// A failing producer: the request stays incomplete.
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user", purgeErr: errors.New("boom")})
	_, _ = svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "op-2")
	// A failed settle.
	repo2 := erasuretest.NewMemRepo()
	repo2.Put(models.ErasureRequest{UUID: "r-2", UserUUID: "u-9", Status: models.ErasureRequestPending})
	repo2.FailNextSettle(errors.New("mongo blip"))
	svc2, sink2 := newCloseSvc(repo2, &stubProducer{subject: "user"})
	if _, err := svc2.Execute(context.Background(), "r-2", iface.EraseHardDelete, "op-2"); err != nil {
		t.Fatal(err)
	}
	// A lost lease: another holder takes the row before Settle.
	repo3 := erasuretest.NewMemRepo()
	repo3.Put(models.ErasureRequest{UUID: "r-3", UserUUID: "u-9", Status: models.ErasureRequestPending})
	repo3.OnSettle(func(context.Context) error { return repository.ErrErasureLeaseLost })
	svc3, sink3 := newCloseSvc(repo3, &stubProducer{subject: "user"})
	if _, err := svc3.Execute(context.Background(), "r-3", iface.EraseHardDelete, "op-2"); err != nil {
		t.Fatal(err)
	}
	for i, s := range []*capturingAuditSink{sink, sink2, sink3} {
		if n := len(receiptEvents(s)); n != 0 {
			t.Errorf("case %d: %d receipt events; want none", i, n)
		}
	}
}

func TestClosePartialEmitsReceipt(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete,
		Progress: []models.ErasureProducerProgress{done("user"), failed("gone")}, RequestedAt: erasureT0})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user"})
	if _, err := svc.ClosePartial(context.Background(), "r-1", "op-1", "module removed"); err != nil {
		t.Fatal(err)
	}
	evs := receiptEvents(sink)
	if len(evs) != 1 || evs[0].ActorUserID != "op-1" || evs[0].Metadata["status"] != models.ErasureRequestClosedPartial {
		t.Fatalf("events = %+v", evs)
	}
	if _, has := evs[0].Metadata["closeNote"]; has {
		t.Fatal("close note leaked")
	}
	// A refused close emits nothing.
	repo.Put(models.ErasureRequest{UUID: "r-2", Status: models.ErasureRequestPending})
	n := len(receiptEvents(sink))
	_, _ = svc.ClosePartial(context.Background(), "r-2", "op-1", "x")
	if len(receiptEvents(sink)) != n {
		t.Fatal("refused close emitted a receipt")
	}
}

func TestListCursorPagesAcrossEqualRequestedAt(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		repo.Put(models.ErasureRequest{UUID: id, UserUUID: "u-" + id, Status: models.ErasureRequestCompleted, RequestedAt: erasureT0})
	}
	repo.Put(models.ErasureRequest{UUID: "z", UserUUID: "u-z", Status: models.ErasureRequestPending, RequestedAt: erasureT0})
	svc, _ := newErasureSvc(repo)
	var got []string
	after := ""
	pages := 0
	for {
		rows, next, err := svc.List(context.Background(), repository.ErasureListFilter{Statuses: []string{models.ErasureRequestCompleted}, Limit: 3, After: after})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, r := range rows {
			got = append(got, r.UUID)
		}
		if next == "" {
			break
		}
		after = next
	}
	if pages != 3 || len(got) != 7 || got[0] != "g" || got[6] != "a" {
		t.Fatalf("pages=%d got=%v; want 3 pages, g..a once each", pages, got)
	}
	if _, _, err := svc.List(context.Background(), repository.ErasureListFilter{After: "!!"}); !errors.Is(err, repository.ErrErasureCursorInvalid) {
		t.Fatalf("bad cursor err = %v", err)
	}
	rows, _, _ := svc.List(context.Background(), repository.ErasureListFilter{UserUUID: "u-c"})
	if len(rows) != 1 || rows[0].UUID != "c" {
		t.Fatalf("userUuid filter = %+v", rows)
	}
}
