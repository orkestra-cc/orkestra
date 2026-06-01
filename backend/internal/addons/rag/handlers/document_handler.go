package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/services"
)

// DocumentHandler handles document ingestion HTTP requests
type DocumentHandler struct {
	ingestionService services.IngestionService
}

// NewDocumentHandler creates a new DocumentHandler
func NewDocumentHandler(ingestionSvc services.IngestionService) *DocumentHandler {
	return &DocumentHandler{ingestionService: ingestionSvc}
}

// UploadDocument handles multipart file upload for document ingestion.
// This uses raw HTTP request because Huma v2 doesn't natively support multipart file uploads.
func (h *DocumentHandler) UploadDocument(ctx context.Context, input *struct{}) (*models.GetDocumentResponse, error) {
	// Get the raw HTTP request from context
	httpReq, ok := ctx.Value("http_request").(*http.Request)
	if !ok {
		return nil, huma.Error400BadRequest("failed to get HTTP request")
	}

	// Parse multipart form (max 50MB)
	if err := httpReq.ParseMultipartForm(50 << 20); err != nil {
		return nil, huma.Error400BadRequest("failed to parse form", err)
	}

	file, header, err := httpReq.FormFile("file")
	if err != nil {
		return nil, huma.Error400BadRequest("file is required", err)
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		return nil, huma.Error400BadRequest("failed to read file", err)
	}

	title := httpReq.FormValue("title")
	if title == "" {
		title = header.Filename
	}

	isoStandard := httpReq.FormValue("isoStandard")
	version := httpReq.FormValue("version")
	documentCategory := httpReq.FormValue("documentCategory")

	chunkSize, _ := strconv.Atoi(httpReq.FormValue("chunkSize"))
	chunkOverlap, _ := strconv.Atoi(httpReq.FormValue("chunkOverlap"))
	llmModelUUID := httpReq.FormValue("llmModelUuid")

	doc, err := h.ingestionService.IngestDocument(ctx, title, header.Filename, fileData, isoStandard, version, documentCategory, chunkSize, chunkOverlap, llmModelUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("ingestion failed: %v", err))
	}

	return &models.GetDocumentResponse{Body: *doc}, nil
}

func (h *DocumentHandler) ListDocuments(ctx context.Context, req *models.ListDocumentsRequest) (*models.ListDocumentsResponse, error) {
	docs, err := h.ingestionService.ListDocuments(ctx, req.Status, req.ISOStandard)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list documents", err)
	}
	resp := &models.ListDocumentsResponse{}
	resp.Body.Documents = docs
	return resp, nil
}

func (h *DocumentHandler) GetDocument(ctx context.Context, req *models.GetDocumentRequest) (*models.GetDocumentResponse, error) {
	doc, err := h.ingestionService.GetDocument(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Document not found", err)
	}
	return &models.GetDocumentResponse{Body: *doc}, nil
}

func (h *DocumentHandler) UpdateDocument(ctx context.Context, req *models.UpdateDocumentRequest) (*models.UpdateDocumentResponse, error) {
	if req.Body.Title == nil && req.Body.ISOStandard == nil && req.Body.Version == nil {
		return nil, huma.Error400BadRequest("at least one field must be provided")
	}

	doc, err := h.ingestionService.UpdateDocument(ctx, req.UUID, req.Body.Title, req.Body.ISOStandard, req.Body.Version)
	if err != nil {
		return nil, huma.Error404NotFound("Document not found", err)
	}
	return &models.UpdateDocumentResponse{Body: *doc}, nil
}

func (h *DocumentHandler) GetDocumentChunks(ctx context.Context, req *models.GetDocumentChunksRequest) (*models.GetDocumentChunksResponse, error) {
	chunks, err := h.ingestionService.GetDocumentChunks(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Document not found or no chunks available", err)
	}
	resp := &models.GetDocumentChunksResponse{}
	resp.Body.Chunks = chunks
	return resp, nil
}

func (h *DocumentHandler) GetDocumentSections(ctx context.Context, req *models.GetDocumentSectionsRequest) (*models.GetDocumentSectionsResponse, error) {
	sections, err := h.ingestionService.GetDocumentSections(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Document not found or no sections available", err)
	}
	resp := &models.GetDocumentSectionsResponse{}
	resp.Body.Sections = sections
	return resp, nil
}

func (h *DocumentHandler) ReprocessDocument(ctx context.Context, req *models.ReprocessDocumentRequest) (*models.ReprocessDocumentResponse, error) {
	if err := h.ingestionService.ReprocessDocument(ctx, req.UUID); err != nil {
		return nil, huma.Error400BadRequest("Reprocessing failed", err)
	}
	resp := &models.ReprocessDocumentResponse{}
	resp.Body.Message = "Document reprocessing started"
	return resp, nil
}

func (h *DocumentHandler) DeleteDocument(ctx context.Context, req *models.DeleteDocumentRequest) (*models.DeleteDocumentResponse, error) {
	if err := h.ingestionService.DeleteDocument(ctx, req.UUID); err != nil {
		return nil, huma.Error400BadRequest("Failed to delete document", err)
	}
	resp := &models.DeleteDocumentResponse{}
	resp.Body.Message = "Document deleted successfully"
	return resp, nil
}

func (h *DocumentHandler) GetDocumentRelations(ctx context.Context, req *models.GetDocumentRelationsRequest) (*models.GetDocumentRelationsResponse, error) {
	summaries, links, err := h.ingestionService.GetDocumentRelations(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get document relations", err)
	}
	resp := &models.GetDocumentRelationsResponse{}
	resp.Body.RelatedDocuments = summaries
	resp.Body.Links = links
	resp.Body.TotalLinks = len(links)
	if resp.Body.RelatedDocuments == nil {
		resp.Body.RelatedDocuments = []models.RelatedDocSummary{}
	}
	if resp.Body.Links == nil {
		resp.Body.Links = []models.CrossDocLink{}
	}
	return resp, nil
}
