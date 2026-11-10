package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/internal/core/compliance/services/exporttest"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const knownSubject = "7c1e5b3a-4f0d-4a2e-9b8c-1d2e3f4a5b6c"

// subjectDSR knows only knownSubject.
func subjectDSR() *services.DSRService {
	dsr := services.NewDSRService(iface.NewPIIProducerRegistry(), nil, slog.Default())
	dsr.SetSubjectLookup(func(_ context.Context, id string) (bool, error) { return id == knownSubject, nil })
	return dsr
}

func TestOpenUnknownSubjectIs404WithCode(t *testing.T) {
	t.Parallel()
	h := NewErasureRequestHandler(services.NewErasureRequestService(erasuretest.NewMemRepo(), subjectDSR(), nil, nil))
	_, err := h.Open(authedCtx("op-a"), openInput("00000000-0000-4000-8000-000000000000", "contract ended", "anonymize"))
	assertCode(t, err, http.StatusNotFound, errcode.ComplianceSubjectNotFound)
}

func TestOpenLowerCasesSubject(t *testing.T) {
	t.Parallel()
	h := NewErasureRequestHandler(services.NewErasureRequestService(erasuretest.NewMemRepo(), subjectDSR(), nil, nil))
	out, err := h.Open(authedCtx("op-a"), openInput(strings.ToUpper(knownSubject), "contract ended", "anonymize"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.UserUUID != knownSubject {
		t.Fatalf("stored subject = %q, want lower-case", out.Body.UserUUID)
	}
}

func TestDSRExportCreateUnknownSubjectIs404WithCode(t *testing.T) {
	t.Parallel()
	svc := services.NewDSRExportService(exporttest.NewMemRepo(), subjectDSR(), exporttest.NewMemStore(), nil, func() int { return 7 }, slog.Default())
	h := NewDSRExportHandler(svc)
	_, err := h.Create(adminTenantCtx("op-1", "t-1"), createInput("00000000-0000-4000-8000-000000000000", "json", "request"))
	assertCode(t, err, http.StatusNotFound, errcode.ComplianceSubjectNotFound)
	out, err := h.Create(adminTenantCtx("op-1", "t-1"), createInput(strings.ToUpper(knownSubject), "json", "request"))
	if err != nil || out.Body.UserUUID != knownSubject {
		t.Fatalf("known upper-case subject: %+v %v", out, err)
	}
}

func TestDSRPreviewUnknownSubjectIs404WithCode(t *testing.T) {
	t.Parallel()
	h := NewDSRPreviewHandler(services.NewDSRPreviewService(subjectDSR(), noHolds{}, noRequests{}, nil, slog.Default()))
	_, err := h.Preview(context.Background(), &DSRPreviewInput{UserUUID: "00000000-0000-4000-8000-000000000000"})
	assertCode(t, err, http.StatusNotFound, errcode.ComplianceSubjectNotFound)
	if _, err := h.Preview(context.Background(), &DSRPreviewInput{UserUUID: strings.ToUpper(knownSubject)}); err != nil {
		t.Fatalf("known upper-case subject: %v", err)
	}
}

// A malformed subject is refused by the schema before any handler runs.
func TestSubjectUUIDFormatIsEnforcedOnTheRoutes(t *testing.T) {
	t.Parallel()
	_, api := humatest.New(t)
	RegisterErasureRequestAdminWriteRoutes(api, NewErasureRequestHandler(services.NewErasureRequestService(erasuretest.NewMemRepo(), subjectDSR(), nil, nil)))
	RegisterErasureRequestAdminReadRoutes(api, NewErasureRequestHandler(services.NewErasureRequestService(erasuretest.NewMemRepo(), subjectDSR(), nil, nil)))
	RegisterDSRPreviewRoutes(api, NewDSRPreviewHandler(services.NewDSRPreviewService(subjectDSR(), noHolds{}, noRequests{}, nil, slog.Default())))
	exp := NewDSRExportHandler(services.NewDSRExportService(exporttest.NewMemRepo(), subjectDSR(), exporttest.NewMemStore(), nil, nil, slog.Default()))
	RegisterDSRExportReadRoutes(api, exp)
	RegisterDSRExportWriteRoutes(api, exp)

	for name, resp := range map[string]int{
		"open":    api.Post("/v1/admin/compliance/erasure-requests", map[string]any{"userUuid": "u-1", "reason": "r", "mode": "anonymize"}).Code,
		"export":  api.Post("/v1/admin/compliance/dsr/exports", map[string]any{"userUuid": "u-1", "format": "json", "reason": "r"}).Code,
		"list":    api.Get("/v1/admin/compliance/dsr/exports?userUuid=u-1").Code,
		"preview": api.Get("/v1/admin/compliance/dsr/preview?userUuid=u-1").Code,
	} {
		if resp != http.StatusUnprocessableEntity {
			t.Errorf("%s with a malformed subject = %d, want 422", name, resp)
		}
	}
	// Upper-case passes the schema; the service lower-cases it.
	if c := api.Get("/v1/admin/compliance/dsr/preview?userUuid=" + strings.ToUpper(knownSubject)).Code; c != http.StatusOK {
		t.Errorf("upper-case preview = %d, want 200", c)
	}
}
