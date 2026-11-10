package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// DSRExportHandler is the admin surface of the stored DSR exports.
type DSRExportHandler struct {
	svc *services.DSRExportService
}

// NewDSRExportHandler binds the handler to its service.
func NewDSRExportHandler(svc *services.DSRExportService) *DSRExportHandler {
	return &DSRExportHandler{svc: svc}
}

// ComplianceDSRExportRecord is one stored export; it never carries the object key.
type ComplianceDSRExportRecord struct {
	UUID        string    `json:"uuid"`
	UserUUID    string    `json:"userUuid"`
	Format      string    `json:"format" enum:"json,csv"`
	RequestedBy string    `json:"requestedBy"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	SizeBytes   int64     `json:"sizeBytes"`
	Producers   []string  `json:"producers,omitempty"`
	ErrorKeys   []string  `json:"errorKeys,omitempty" doc:"Producers whose export failed"`
}

func toExportRecord(e models.DSRExport) ComplianceDSRExportRecord {
	return ComplianceDSRExportRecord{
		UUID: e.UUID, UserUUID: e.UserUUID, Format: e.Format, RequestedBy: e.RequestedBy,
		Reason: e.Reason, CreatedAt: e.CreatedAt, ExpiresAt: e.ExpiresAt, SizeBytes: e.SizeBytes,
		Producers: e.Producers, ErrorKeys: e.ErrorKeys,
	}
}

// ComplianceDSRExportDownload is a short-lived download link.
type ComplianceDSRExportDownload struct {
	URL       string `json:"url" doc:"Open this URL to download the export"`
	ExpiresIn int    `json:"expiresIn" doc:"Seconds the URL works"`
}

type CreateDSRExportInput struct {
	Body struct {
		UserUUID string `json:"userUuid" minLength:"1" doc:"Data subject"`
		Format   string `json:"format" enum:"json,csv" doc:"Export format"`
		Reason   string `json:"reason" doc:"Why the operator generates the export (1 to 500 characters)"`
	}
}
type CreateDSRExportOutput struct {
	Body ComplianceDSRExportRecord
}

func (h *DSRExportHandler) Create(ctx context.Context, in *CreateDSRExportInput) (*CreateDSRExportOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	row, err := h.svc.Create(ctx, in.Body.UserUUID, in.Body.Format, in.Body.Reason, actor)
	if err != nil {
		return nil, mapDSRExportError(err, "create dsr export")
	}
	return &CreateDSRExportOutput{Body: toExportRecord(*row)}, nil
}

type ListDSRExportsInput struct {
	UserUUID string `query:"userUuid" minLength:"1" doc:"Data subject"`
}
type ListDSRExportsOutput struct {
	Body struct {
		Items []ComplianceDSRExportRecord `json:"items"`
	}
}

func (h *DSRExportHandler) List(ctx context.Context, in *ListDSRExportsInput) (*ListDSRExportsOutput, error) {
	rows, err := h.svc.List(ctx, in.UserUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list dsr exports", err)
	}
	out := &ListDSRExportsOutput{}
	out.Body.Items = make([]ComplianceDSRExportRecord, 0, len(rows))
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, toExportRecord(r))
	}
	return out, nil
}

type DownloadDSRExportInput struct {
	ID string `path:"id"`
}
type DownloadDSRExportOutput struct {
	Body ComplianceDSRExportDownload
}

func (h *DSRExportHandler) Download(ctx context.Context, in *DownloadDSRExportInput) (*DownloadDSRExportOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	url, err := h.svc.DownloadURL(ctx, in.ID, actor)
	if err != nil {
		return nil, mapDSRExportError(err, "download dsr export")
	}
	return &DownloadDSRExportOutput{Body: ComplianceDSRExportDownload{URL: url, ExpiresIn: int(services.DSRExportDownloadTTL.Seconds())}}, nil
}

func mapDSRExportError(err error, op string) error {
	switch {
	case errors.Is(err, services.ErrExportStorageUnavailable):
		return errcode.ServiceUnavailable(errcode.ComplianceExportStorageUnavailable, "Object storage is not configured, so exports cannot be stored.")
	case errors.Is(err, services.ErrExportExpired):
		return errcode.New(http.StatusGone, errcode.ComplianceExportExpired, "This export has expired.")
	case errors.Is(err, repository.ErrDSRExportNotFound):
		return errcode.NotFound(errcode.ComplianceExportNotFound, "Export not found.")
	case errors.Is(err, services.ErrReasonRequired):
		return errcode.UnprocessableEntity(errcode.ComplianceReasonRequired, "A reason of 1 to 500 characters is required.")
	case errors.Is(err, services.ErrExportFormatInvalid):
		return huma.Error422UnprocessableEntity("format must be json or csv")
	}
	return huma.Error500InternalServerError(op, err)
}

// RegisterDSRExportReadRoutes mounts the list (audit.read).
func RegisterDSRExportReadRoutes(api huma.API, h *DSRExportHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-dsr-export-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/dsr/exports",
		Summary:     "List the stored DSR exports of a data subject",
		Tags:        []string{"Compliance"},
	}, h.List)
}

// RegisterDSRExportWriteRoutes mounts create and download, which the caller
// wraps with dsr.manage + step-up.
func RegisterDSRExportWriteRoutes(api huma.API, h *DSRExportHandler) {
	huma.Register(api, huma.Operation{
		OperationID:   "compliance-dsr-export-create",
		Method:        http.MethodPost,
		Path:          "/v1/admin/compliance/dsr/exports",
		Summary:       "Generate a data export on behalf of a data subject and store it until it expires",
		Tags:          []string{"Compliance"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-dsr-export-download",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/dsr/exports/{id}/download",
		Summary:     "Get a short-lived download URL for a stored export",
		Tags:        []string{"Compliance"},
	}, h.Download)
}
