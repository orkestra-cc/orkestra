package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type previewCounter struct{}

func (previewCounter) Subject() string                                         { return "user" }
func (previewCounter) ExportPersonalData(context.Context, string) (any, error) { return nil, nil }
func (previewCounter) PurgePersonalData(context.Context, string, iface.EraseMode) (iface.PurgeResult, error) {
	return iface.PurgeResult{}, nil
}
func (previewCounter) CountPersonalData(context.Context, string) (iface.PersonalDataCount, error) {
	return iface.PersonalDataCount{Rows: 4}, nil
}

type previewPlain struct{ previewCounter }

func (previewPlain) Subject() string { return "plain" }

type noHolds struct{}

func (noHolds) ListActive(context.Context, string) ([]models.LegalHold, error) { return nil, nil }

type noRequests struct{}

func (noRequests) List(context.Context, repository.ErasureListFilter) ([]models.ErasureRequest, string, error) {
	return nil, "", nil
}

func newPreviewHandler() *DSRPreviewHandler {
	reg := iface.NewPIIProducerRegistry()
	reg.Register(previewCounter{})
	dsr := services.NewDSRService(reg, nil, slog.Default())
	return NewDSRPreviewHandler(services.NewDSRPreviewService(dsr, noHolds{}, noRequests{}, nil, slog.Default()))
}

func TestDSRPreviewHandler(t *testing.T) {
	out, err := newPreviewHandler().Preview(context.Background(), &DSRPreviewInput{UserUUID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	b := out.Body
	if !b.HardDeleteAllowed || !b.AnonymizeAllowed || b.Retention != nil || b.LegalHolds == nil || b.OpenRequests == nil {
		t.Fatalf("body = %+v", b)
	}
	if len(b.Producers) != 1 || b.Producers[0].Rows != 4 || !b.Producers[0].Countable || b.Producers[0].Collections == nil {
		t.Fatalf("producers = %+v", b.Producers)
	}
}

func TestDSRPreviewRouteRequiresUserUuid(t *testing.T) {
	_, api := humatest.New(t)
	RegisterDSRPreviewRoutes(api, newPreviewHandler())
	var resp *httptest.ResponseRecorder = api.Get("/v1/admin/compliance/dsr/preview")
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing userUuid = %d", resp.Code)
	}
	if ok := api.Get("/v1/admin/compliance/dsr/preview?userUuid=u"); ok.Code != http.StatusOK {
		t.Fatalf("ok = %d %s", ok.Code, ok.Body.String())
	}
}
