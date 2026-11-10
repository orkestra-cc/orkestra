package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/compliance/services/exporttest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type exportHandlerFixture struct {
	h     *DSRExportHandler
	repo  *exporttest.MemRepo
	store *exporttest.MemStore
	now   time.Time
}

func newExportHandler(withStore bool) *exportHandlerFixture {
	f := &exportHandlerFixture{repo: exporttest.NewMemRepo(), store: exporttest.NewMemStore(), now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	dsr := services.NewDSRService(iface.NewPIIProducerRegistry(), nil, slog.Default())
	var st iface.ObjectStore
	if withStore {
		st = f.store
	}
	svc := services.NewDSRExportService(f.repo, dsr, st, nil, func() int { return 7 }, slog.Default())
	svc.SetClock(func() time.Time { return f.now })
	f.h = NewDSRExportHandler(svc)
	return f
}

func createInput(user, format, reason string) *CreateDSRExportInput {
	in := &CreateDSRExportInput{}
	in.Body.UserUUID, in.Body.Format, in.Body.Reason = user, format, reason
	return in
}

func TestDSRExportCreateReturnsRecordWithoutObjectKey(t *testing.T) {
	f := newExportHandler(true)
	out, err := f.h.Create(adminTenantCtx("op-1", "t-1"), createInput("subj", "json", "request"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.RequestedBy != "op-1" || out.Body.UserUUID != "subj" || out.Body.UUID == "" {
		t.Fatalf("body = %+v", out.Body)
	}
}

func TestDSRExportCreateErrors(t *testing.T) {
	ctx := adminTenantCtx("op-1", "t-1")
	_, err := newExportHandler(false).h.Create(ctx, createInput("s", "json", "r"))
	assertStatus(t, err, http.StatusServiceUnavailable)
	_, err = newExportHandler(true).h.Create(ctx, createInput("s", "json", " "))
	assertStatus(t, err, http.StatusUnprocessableEntity)
	_, err = newExportHandler(true).h.Create(ctx, createInput("s", "xml", "r"))
	assertStatus(t, err, http.StatusUnprocessableEntity)
}

func TestDSRExportListAndDownload(t *testing.T) {
	f := newExportHandler(true)
	ctx := adminTenantCtx("op-1", "t-1")
	created, _ := f.h.Create(ctx, createInput("subj", "csv", "r"))
	list, err := f.h.List(ctx, &ListDSRExportsInput{UserUUID: "subj"})
	if err != nil || len(list.Body.Items) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	empty, _ := f.h.List(ctx, &ListDSRExportsInput{UserUUID: "nobody"})
	if empty.Body.Items == nil || len(empty.Body.Items) != 0 {
		t.Fatal("an empty list must be [] not null")
	}
	dl, err := f.h.Download(ctx, &DownloadDSRExportInput{ID: created.Body.UUID})
	if err != nil || dl.Body.ExpiresIn != 300 || !strings.HasPrefix(dl.Body.URL, "https://") {
		t.Fatalf("download = %+v, %v", dl, err)
	}
}

func TestDSRExportDownloadExpiredIs410(t *testing.T) {
	f := newExportHandler(true)
	ctx := adminTenantCtx("op-1", "t-1")
	created, _ := f.h.Create(ctx, createInput("subj", "json", "r"))
	f.now = created.Body.ExpiresAt
	_, err := f.h.Download(ctx, &DownloadDSRExportInput{ID: created.Body.UUID})
	assertStatus(t, err, http.StatusGone)
	if len(f.store.Presigns) != 0 {
		t.Fatal("expired export was presigned")
	}
}

func TestDSRExportDownloadUnknownIs404(t *testing.T) {
	_, err := newExportHandler(true).h.Download(context.Background(), &DownloadDSRExportInput{ID: "nope"})
	assertStatus(t, err, http.StatusNotFound)
}

var _ = models.DSRExport{}
