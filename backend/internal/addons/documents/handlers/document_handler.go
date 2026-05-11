package handlers

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"github.com/orkestra/backend/internal/addons/documents/repository"
	"github.com/orkestra/backend/internal/addons/documents/services"
)

// DocumentHandler handles document-related HTTP requests
type DocumentHandler struct {
	pdfService services.PDFService
}

// NewDocumentHandler creates a new document handler
func NewDocumentHandler(pdfService services.PDFService) *DocumentHandler {
	return &DocumentHandler{
		pdfService: pdfService,
	}
}

// -------- Request/Response types --------

// GeneratePDFRequest is the request for generating a PDF
type GeneratePDFRequest struct {
	Body models.GeneratePDFInput `json:"input" doc:"PDF generation input"`
}

// GeneratePDFResponse is the response for PDF generation
type GeneratePDFResponse struct {
	Body models.GeneratedDocumentMeta `json:"document" doc:"Generated document metadata"`
}

// PreviewHTMLRequest is the request for HTML preview
type PreviewHTMLRequest struct {
	Body models.PreviewHTMLInput `json:"input" doc:"Preview input"`
}

// PreviewHTMLResponse is the response containing rendered HTML
type PreviewHTMLResponse struct {
	Body struct {
		HTML string `json:"html" doc:"Rendered HTML content"`
	}
}

// PreviewHTMLFromContentRequest is the request for previewing raw HTML/CSS
type PreviewHTMLFromContentRequest struct {
	Body models.PreviewHTMLFromContentInput `json:"input" doc:"Preview input with raw content"`
}

// GetDocumentRequest is the request for getting a document by ID
type GetDocumentRequest struct {
	ID string `path:"id" doc:"Document UUID"`
}

// GetDocumentResponse is the response containing document metadata
type GetDocumentResponse struct {
	Body models.GeneratedDocumentMeta `json:"document" doc:"Document metadata"`
}

// DownloadDocumentRequest is the request for downloading a document
type DownloadDocumentRequest struct {
	ID string `path:"id" doc:"Document UUID"`
}

// DownloadDocumentResponse represents a binary PDF download
type DownloadDocumentResponse struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

// ServiceStatusRequest is the request for checking service status
type ServiceStatusRequest struct{}

// ServiceStatusResponse is the response containing service status
type ServiceStatusResponse struct {
	Body struct {
		Available bool   `json:"available" doc:"Whether the PDF service is available"`
		Message   string `json:"message" doc:"Status message"`
	}
}

// -------- Handler methods --------

// GeneratePDF generates a PDF from a template and data
func (h *DocumentHandler) GeneratePDF(ctx context.Context, req *GeneratePDFRequest) (*GeneratePDFResponse, error) {
	userID := getUserIDFromContext(ctx)

	doc, err := h.pdfService.GeneratePDF(ctx, &req.Body, userID)
	if err != nil {
		if err == services.ErrTemplateRequired || err == services.ErrDataRequired {
			return nil, huma.Error400BadRequest(err.Error(), err)
		}
		if err == services.ErrServiceUnavailable || err == services.ErrCircuitBreakerOpen {
			return nil, huma.Error503ServiceUnavailable("PDF generation service is temporarily unavailable", err)
		}
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		if err == services.ErrTemplateRenderError || err == services.ErrTemplateParseError {
			return nil, huma.Error400BadRequest("Failed to render template", err)
		}
		return nil, huma.Error500InternalServerError("Failed to generate PDF", err)
	}

	return &GeneratePDFResponse{Body: models.MetaOf(doc)}, nil
}

// PreviewHTML renders a template to HTML without generating PDF
func (h *DocumentHandler) PreviewHTML(ctx context.Context, req *PreviewHTMLRequest) (*PreviewHTMLResponse, error) {
	html, err := h.pdfService.PreviewHTML(ctx, &req.Body)
	if err != nil {
		if err == services.ErrTemplateRequired || err == services.ErrDataRequired {
			return nil, huma.Error400BadRequest(err.Error(), err)
		}
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		if err == services.ErrTemplateRenderError || err == services.ErrTemplateParseError {
			return nil, huma.Error400BadRequest("Failed to render template", err)
		}
		return nil, huma.Error500InternalServerError("Failed to preview template", err)
	}

	return &PreviewHTMLResponse{
		Body: struct {
			HTML string `json:"html" doc:"Rendered HTML content"`
		}{
			HTML: html,
		},
	}, nil
}

// PreviewHTMLFromContent renders raw HTML/CSS with data
func (h *DocumentHandler) PreviewHTMLFromContent(ctx context.Context, req *PreviewHTMLFromContentRequest) (*PreviewHTMLResponse, error) {
	html, err := h.pdfService.PreviewHTMLFromContent(ctx, &req.Body)
	if err != nil {
		if err == services.ErrTemplateContentRequired || err == services.ErrDataRequired {
			return nil, huma.Error400BadRequest(err.Error(), err)
		}
		if err == services.ErrTemplateRenderError || err == services.ErrTemplateParseError {
			return nil, huma.Error400BadRequest("Failed to render template content", err)
		}
		return nil, huma.Error500InternalServerError("Failed to preview content", err)
	}

	return &PreviewHTMLResponse{
		Body: struct {
			HTML string `json:"html" doc:"Rendered HTML content"`
		}{
			HTML: html,
		},
	}, nil
}

// GetDocument retrieves a document by UUID
func (h *DocumentHandler) GetDocument(ctx context.Context, req *GetDocumentRequest) (*GetDocumentResponse, error) {
	doc, err := h.pdfService.GetDocument(ctx, req.ID)
	if err != nil {
		if err == repository.ErrDocumentNotFound {
			return nil, huma.Error404NotFound("Document not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get document", err)
	}

	return &GetDocumentResponse{Body: models.MetaOf(doc)}, nil
}

// DownloadDocument downloads a document's PDF content
func (h *DocumentHandler) DownloadDocument(ctx context.Context, req *DownloadDocumentRequest) (*DownloadDocumentResponse, error) {
	content, filename, err := h.pdfService.GetDocumentContent(ctx, req.ID)
	if err != nil {
		if err == repository.ErrDocumentNotFound {
			return nil, huma.Error404NotFound("Document not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get document content", err)
	}

	if filename == "" {
		filename = "document.pdf"
	}

	return &DownloadDocumentResponse{
		ContentType:        "application/pdf",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s"`, filename),
		Body:               content,
	}, nil
}

// GetServiceStatus checks if the PDF service is available
func (h *DocumentHandler) GetServiceStatus(ctx context.Context, req *ServiceStatusRequest) (*ServiceStatusResponse, error) {
	available := h.pdfService.IsAvailable(ctx)

	message := "PDF generation service is available"
	if !available {
		message = "PDF generation service is temporarily unavailable"
	}

	return &ServiceStatusResponse{
		Body: struct {
			Available bool   `json:"available" doc:"Whether the PDF service is available"`
			Message   string `json:"message" doc:"Status message"`
		}{
			Available: available,
			Message:   message,
		},
	}, nil
}
