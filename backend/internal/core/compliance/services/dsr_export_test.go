package services

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services/exporttest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type exportFixture struct {
	svc   *DSRExportService
	repo  *exporttest.MemRepo
	store *exporttest.MemStore
	audit *capturingAuditSink
	now   time.Time
	reg   *iface.PIIProducerRegistry
}

func newExportFixture(t *testing.T, withStore bool, producers ...iface.PIIProducer) *exportFixture {
	t.Helper()
	reg := iface.NewPIIProducerRegistry()
	for _, p := range producers {
		reg.Register(p)
	}
	audit := &capturingAuditSink{}
	dsr := NewDSRService(reg, audit, slog.Default())
	f := &exportFixture{repo: exporttest.NewMemRepo(), store: exporttest.NewMemStore(), audit: audit, now: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), reg: reg}
	var store iface.ObjectStore
	if withStore {
		store = f.store
	}
	f.svc = NewDSRExportService(f.repo, dsr, store, audit, func() int { return 10 }, slog.Default())
	f.svc.SetClock(func() time.Time { return f.now })
	return f
}

func TestCreateJSONExportStoresBytesAndAudits(t *testing.T) {
	f := newExportFixture(t, true, &stubProducer{subject: "user", exportData: map[string]any{"email": "a@example.com"}})
	row, err := f.svc.Create(context.Background(), "subj-12345678", "json", "  access request  ", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if row.Format != "json" || row.RequestedBy != "op-1" || row.ObjectKey != row.UUID+".json" {
		t.Fatalf("row = %+v", row)
	}
	if want := f.now.Add(10 * 24 * time.Hour); !row.ExpiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", row.ExpiresAt, want)
	}
	b := f.store.Objects[row.ObjectKey]
	if int64(len(b)) != row.SizeBytes || f.store.ContentTypes[row.ObjectKey] != "application/json" {
		t.Fatalf("stored %d bytes ct %q", len(b), f.store.ContentTypes[row.ObjectKey])
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("stored JSON does not parse: %v", err)
	}
	if parsed["user"].(map[string]any)["email"] != "a@example.com" {
		t.Fatalf("bundle = %v", parsed)
	}
	if len(f.audit.events) != 1 || f.audit.events[0].Action != "gdpr.dsr.exported" ||
		f.audit.events[0].ActorUserID != "op-1" || f.audit.events[0].ResourceID != "subj-12345678" {
		t.Fatalf("audit = %+v", f.audit.events)
	}
	if row.Reason != "access request" {
		t.Fatalf("reason = %q", row.Reason)
	}
}

func TestCreateCSVExportStoresParsableCSV(t *testing.T) {
	f := newExportFixture(t, true, &stubProducer{subject: "user", exportData: map[string]any{"name": "=cmd"}})
	row, err := f.svc.Create(context.Background(), "subj-1", "csv", "request", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if f.store.ContentTypes[row.ObjectKey] != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", f.store.ContentTypes[row.ObjectKey])
	}
	rows, err := csv.NewReader(strings.NewReader(string(f.store.Objects[row.ObjectKey]))).ReadAll()
	if err != nil || len(rows) < 3 || strings.Join(rows[0], ",") != "producer,path,value" {
		t.Fatalf("csv = %v, %v", rows, err)
	}
	found := false
	for _, r := range rows {
		if r[0] == "user" && r[1] == "name" && r[2] == "'=cmd" {
			found = true
		}
	}
	if !found {
		t.Fatalf("escaped cell missing: %v", rows)
	}
}

func TestCreateRecordsFailedProducerKeysOnly(t *testing.T) {
	f := newExportFixture(t, true,
		&stubProducer{subject: "ok", exportData: "x"},
		&stubProducer{subject: "bad", exportErr: errors.New("secret detail")})
	row, err := f.svc.Create(context.Background(), "s", "json", "r", "op")
	if err != nil {
		t.Fatal(err)
	}
	if len(row.ErrorKeys) != 1 || row.ErrorKeys[0] != "bad" || len(row.Producers) != 1 {
		t.Fatalf("row = %+v", row)
	}
}

func TestCreateWithoutStoreIsUnavailableAndRunsNoExport(t *testing.T) {
	p := &stubProducer{subject: "user", exportData: "x"}
	f := newExportFixture(t, false, p)
	_, err := f.svc.Create(context.Background(), "s", "json", "r", "op")
	if !errors.Is(err, ErrExportStorageUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if p.exportCalls != 0 || len(f.audit.events) != 0 || len(f.repo.Rows) != 0 {
		t.Fatal("an unavailable store must not export, audit or record")
	}
}

func TestCreateValidatesFormatAndReason(t *testing.T) {
	f := newExportFixture(t, true)
	if _, err := f.svc.Create(context.Background(), "s", "xml", "r", "op"); !errors.Is(err, ErrExportFormatInvalid) {
		t.Fatalf("format err = %v", err)
	}
	if _, err := f.svc.Create(context.Background(), "s", "json", "   ", "op"); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("reason err = %v", err)
	}
}

func TestCreateDefaultsRetentionTo30Days(t *testing.T) {
	f := newExportFixture(t, true)
	f.svc.retentionDays = func() int { return 0 }
	row, err := f.svc.Create(context.Background(), "s", "json", "r", "op")
	if err != nil {
		t.Fatal(err)
	}
	if !row.ExpiresAt.Equal(f.now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expiresAt = %v", row.ExpiresAt)
	}
}

func TestDownloadURLUsesDownloadPresignerAndAudits(t *testing.T) {
	f := newExportFixture(t, true)
	row, _ := f.svc.Create(context.Background(), "abcdefgh-1234", "csv", "r", "op-1")
	url, err := f.svc.DownloadURL(context.Background(), row.UUID, "op-2")
	if err != nil || !strings.Contains(url, "/download/") {
		t.Fatalf("url = %q, err %v", url, err)
	}
	p := f.store.Presigns[0]
	if p.TTL != 5*time.Minute || p.DownloadAs != "dsr-export-abcdefgh-2026-05-01.csv" || p.Key != row.ObjectKey {
		t.Fatalf("presign = %+v", p)
	}
	last := f.audit.events[len(f.audit.events)-1]
	if last.Action != "gdpr.dsr.export_downloaded" || last.ActorUserID != "op-2" ||
		last.Metadata["exportUuid"] != row.UUID || last.Metadata["subjectUserUuid"] != "abcdefgh-1234" {
		t.Fatalf("audit = %+v", last)
	}
}

type plainStore struct{ iface.ObjectStore }

func TestDownloadURLFallsBackToPlainPresign(t *testing.T) {
	f := newExportFixture(t, true)
	f.svc.store = plainStore{f.store}
	row, _ := f.svc.Create(context.Background(), "s", "json", "r", "op")
	url, err := f.svc.DownloadURL(context.Background(), row.UUID, "op")
	if err != nil || !strings.Contains(url, "/get/") {
		t.Fatalf("url = %q, err %v", url, err)
	}
	if f.store.Presigns[0].TTL != 5*time.Minute {
		t.Fatalf("ttl = %v", f.store.Presigns[0].TTL)
	}
}

// Review Focus 4: after expiresAt but before the cleanup job, the download is
// refused and nothing is minted or audited.
func TestDownloadAfterExpiryIsGoneWithoutPresign(t *testing.T) {
	f := newExportFixture(t, true)
	row, _ := f.svc.Create(context.Background(), "s", "json", "r", "op")
	f.now = row.ExpiresAt // ExpiresAt <= now
	events := len(f.audit.events)
	_, err := f.svc.DownloadURL(context.Background(), row.UUID, "op")
	if !errors.Is(err, ErrExportExpired) {
		t.Fatalf("err = %v", err)
	}
	if len(f.store.Presigns) != 0 || len(f.audit.events) != events {
		t.Fatal("an expired export must not be presigned or audited as downloaded")
	}
}

func TestDownloadUnknownExport(t *testing.T) {
	f := newExportFixture(t, true)
	if _, err := f.svc.DownloadURL(context.Background(), "nope", "op"); !errors.Is(err, repository.ErrDSRExportNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCleanupExpiredDeletesObjectAndRowOnly(t *testing.T) {
	f := newExportFixture(t, true)
	old, _ := f.svc.Create(context.Background(), "s", "json", "r", "op")
	f.now = f.now.Add(5 * 24 * time.Hour)
	fresh, _ := f.svc.Create(context.Background(), "s", "json", "r", "op")
	f.now = old.ExpiresAt.Add(time.Second)
	n, err := f.svc.CleanupExpired(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n = %d, err %v", n, err)
	}
	if _, ok := f.store.Objects[old.ObjectKey]; ok {
		t.Fatal("expired object kept")
	}
	if _, ok := f.repo.Rows[old.UUID]; ok {
		t.Fatal("expired row kept")
	}
	if _, ok := f.store.Objects[fresh.ObjectKey]; !ok || len(f.repo.Rows) != 1 {
		t.Fatal("unexpired export removed")
	}
	if n, _ := f.svc.CleanupExpired(context.Background()); n != 0 {
		t.Fatalf("second run deleted %d", n)
	}
}

func TestCleanupKeepsRowWhenObjectDeleteFails(t *testing.T) {
	f := newExportFixture(t, true)
	row, _ := f.svc.Create(context.Background(), "s", "json", "r", "op")
	f.now = row.ExpiresAt.Add(time.Second)
	f.store.DeleteErr = exporttest.Err
	n, err := f.svc.CleanupExpired(context.Background())
	if err == nil || n != 0 || len(f.repo.Rows) != 1 {
		t.Fatalf("n = %d, err %v, rows %d", n, err, len(f.repo.Rows))
	}
}

func TestCleanupToleratesMissingObject(t *testing.T) {
	f := newExportFixture(t, true)
	row, _ := f.svc.Create(context.Background(), "s", "json", "r", "op")
	delete(f.store.Objects, row.ObjectKey)
	f.now = row.ExpiresAt.Add(time.Second)
	if n, err := f.svc.CleanupExpired(context.Background()); err != nil || n != 1 {
		t.Fatalf("n = %d, err %v", n, err)
	}
}

func TestProducerExportCountAndPurge(t *testing.T) {
	f := newExportFixture(t, true)
	ctx := context.Background()
	mine1, _ := f.svc.Create(ctx, "subj", "json", "r", "op")
	mine2, _ := f.svc.Create(ctx, "subj", "csv", "r", "op")
	other, _ := f.svc.Create(ctx, "other", "json", "r", "op")
	p := NewDSRExportProducer(f.svc)

	if p.ProducerKey() != "compliance.dsr_exports" || p.Subject() != "compliance" {
		t.Fatal("producer identity")
	}
	data, err := p.ExportPersonalData(ctx, "subj")
	list, _ := data.([]map[string]any)
	if err != nil || len(list) != 2 {
		t.Fatalf("export = %v, %v", data, err)
	}
	for _, e := range list {
		if _, leaked := e["objectKey"]; leaked || e["uuid"] == nil || e["format"] == nil || e["createdAt"] == nil || e["expiresAt"] == nil {
			t.Fatalf("entry = %v", e)
		}
	}
	if d, _ := p.ExportPersonalData(ctx, "nobody"); d != nil {
		t.Fatalf("no exports must export nil, got %v", d)
	}
	if c, err := p.CountPersonalData(ctx, "subj"); err != nil || c.Rows != 2 {
		t.Fatalf("count = %+v, %v", c, err)
	}

	for _, mode := range []iface.EraseMode{iface.EraseHardDelete, iface.EraseAnonymize} {
		res, err := p.PurgePersonalData(ctx, "subj", mode)
		if err != nil {
			t.Fatal(err)
		}
		_ = res
	}
	for _, r := range []*models.DSRExport{mine1, mine2} {
		if _, ok := f.store.Objects[r.ObjectKey]; ok {
			t.Fatal("subject object kept")
		}
		if _, ok := f.repo.Rows[r.UUID]; ok {
			t.Fatal("subject row kept")
		}
	}
	if _, ok := f.store.Objects[other.ObjectKey]; !ok || len(f.repo.Rows) != 1 {
		t.Fatal("another subject's export was purged")
	}
}

func TestProducerPurgeCountsRowsAndIsIdempotent(t *testing.T) {
	f := newExportFixture(t, true)
	ctx := context.Background()
	_, _ = f.svc.Create(ctx, "subj", "json", "r", "op")
	_, _ = f.svc.Create(ctx, "subj", "json", "r", "op")
	p := NewDSRExportProducer(f.svc)
	res, err := p.PurgePersonalData(ctx, "subj", iface.EraseHardDelete)
	if err != nil || res.RowsDeleted != 2 || res.Pending {
		t.Fatalf("res = %+v, %v", res, err)
	}
	res, err = p.PurgePersonalData(ctx, "subj", iface.EraseHardDelete)
	if err != nil || res.RowsDeleted != 0 {
		t.Fatalf("second purge = %+v, %v", res, err)
	}
}

// Review Focus 4: erasing the subject through the DSR pipeline deletes the exports.
func TestErasureOfSubjectDeletesStoredExports(t *testing.T) {
	f := newExportFixture(t, true)
	f.reg.Register(NewDSRExportProducer(f.svc))
	row, _ := f.svc.Create(context.Background(), "subj", "json", "r", "op")
	dsr := NewDSRService(f.reg, f.audit, slog.Default())
	res, err := dsr.Erase(context.Background(), "subj", iface.EraseHardDelete)
	if err != nil || !res.Complete() {
		t.Fatalf("erase = %+v, %v", res, err)
	}
	if _, ok := f.store.Objects[row.ObjectKey]; ok || len(f.repo.Rows) != 0 {
		t.Fatal("stored export survived the erasure")
	}
}

func TestProducerPurgeWithoutStoreFailsWhenExportsExist(t *testing.T) {
	f := newExportFixture(t, true)
	_, _ = f.svc.Create(context.Background(), "subj", "json", "r", "op")
	f.svc.store = nil
	p := NewDSRExportProducer(f.svc)
	if _, err := p.PurgePersonalData(context.Background(), "subj", iface.EraseHardDelete); err == nil {
		t.Fatal("must not report done while objects cannot be deleted")
	}
	if _, err := p.PurgePersonalData(context.Background(), "nobody", iface.EraseHardDelete); err != nil {
		t.Fatalf("nothing to purge must succeed: %v", err)
	}
}

func TestExportAsAuditsTheActor(t *testing.T) {
	svc, sink := newDSRService(&stubProducer{subject: "user", exportData: "x"})
	if _, err := svc.ExportAs(context.Background(), "subj", "op-9"); err != nil {
		t.Fatal(err)
	}
	e := sink.events[0]
	if e.ActorUserID != "op-9" || e.ResourceType != "user" || e.ResourceID != "subj" || e.Action != "gdpr.dsr.exported" {
		t.Fatalf("event = %+v", e)
	}
	if _, err := svc.Export(context.Background(), "subj"); err != nil {
		t.Fatal(err)
	}
	if sink.events[1].ActorUserID != "subj" {
		t.Fatalf("Export must audit the subject as actor: %+v", sink.events[1])
	}
}
