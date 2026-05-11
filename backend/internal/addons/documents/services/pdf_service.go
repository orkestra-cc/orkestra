package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/documents/config"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"github.com/orkestra/backend/internal/addons/documents/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// Errors for PDF service
var (
	ErrPDFGenerationFailed = errors.New("PDF generation failed")
	ErrTemplateRequired    = errors.New("template UUID is required")
	ErrDataRequired        = errors.New("data is required for PDF generation")
	ErrServiceUnavailable  = errors.New("PDF generation service is unavailable")
)

// PDFService defines the interface for PDF generation
type PDFService interface {
	// PDF generation
	GeneratePDF(ctx context.Context, input *models.GeneratePDFInput, generatedBy string) (*iface.GeneratedDocument, error)
	GeneratePDFFromContent(ctx context.Context, htmlContent string, cssContent string, opts *config.PDFOptions) ([]byte, error)

	// Preview
	PreviewHTML(ctx context.Context, input *models.PreviewHTMLInput) (string, error)
	PreviewHTMLFromContent(ctx context.Context, input *models.PreviewHTMLFromContentInput) (string, error)

	// Document retrieval
	GetDocument(ctx context.Context, uuid string) (*iface.GeneratedDocument, error)
	GetDocumentContent(ctx context.Context, uuid string) ([]byte, string, error)

	// Specific document type generation
	GenerateInvoicePDF(ctx context.Context, invoiceData map[string]interface{}, templateUUID string, generatedBy string) (*iface.GeneratedDocument, error)

	// Service status
	IsAvailable(ctx context.Context) bool
}

type pdfService struct {
	templateRepo   repository.TemplateRepository
	documentRepo   repository.DocumentRepository
	gotenbergClient GotenbergClient
	templateEngine TemplateEngine
	logger         *slog.Logger
}

// NewPDFService creates a new PDF service
func NewPDFService(
	templateRepo repository.TemplateRepository,
	documentRepo repository.DocumentRepository,
	gotenbergClient GotenbergClient,
	templateEngine TemplateEngine,
	logger *slog.Logger,
) PDFService {
	return &pdfService{
		templateRepo:    templateRepo,
		documentRepo:    documentRepo,
		gotenbergClient: gotenbergClient,
		templateEngine:  templateEngine,
		logger:          logger,
	}
}

// GeneratePDF generates a PDF from a template and data
func (s *pdfService) GeneratePDF(ctx context.Context, input *models.GeneratePDFInput, generatedBy string) (*iface.GeneratedDocument, error) {
	if input.TemplateUUID == "" {
		return nil, ErrTemplateRequired
	}
	if input.Data == nil {
		return nil, ErrDataRequired
	}

	// Check service availability
	if !s.gotenbergClient.IsAvailable() {
		return nil, ErrServiceUnavailable
	}

	// Get template
	template, err := s.templateRepo.GetByUUID(ctx, input.TemplateUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get template: %w", err)
	}

	// Render HTML
	html, err := s.templateEngine.Render(ctx, template, input.Data)
	if err != nil {
		s.logger.Error("Failed to render template",
			slog.String("templateUUID", input.TemplateUUID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	// Build PDF options
	pdfOpts := &config.PDFOptions{
		PageSize:    template.PageSize.String(),
		Orientation: template.Orientation.String(),
		Margins: config.PDFMargins{
			Top:    template.Margins.Top,
			Bottom: template.Margins.Bottom,
			Left:   template.Margins.Left,
			Right:  template.Margins.Right,
		},
		HeaderHTML: template.HeaderHTML,
		FooterHTML: template.FooterHTML,
		Scale:      1.0,
	}

	// Generate PDF
	pdfContent, err := s.gotenbergClient.ConvertHTMLToPDF(ctx, html, pdfOpts)
	if err != nil {
		s.logger.Error("Failed to generate PDF",
			slog.String("templateUUID", input.TemplateUUID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: %s", ErrPDFGenerationFailed, err.Error())
	}

	// Generate filename
	fileName := input.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("document_%s", time.Now().Format("20060102_150405"))
	}
	fileName = fileName + ".pdf"

	// Create document record
	doc := &iface.GeneratedDocument{
		UUID:         uuid.New().String(),
		SourceType:   input.SourceType,
		SourceUUID:   input.SourceUUID,
		TemplateUUID: input.TemplateUUID,
		FileName:     fileName,
		FileSize:     int64(len(pdfContent)),
		ContentType:  "application/pdf",
		PDFContent:   pdfContent,
		GeneratedBy:  generatedBy,
	}

	if err := s.documentRepo.Create(ctx, doc); err != nil {
		s.logger.Error("Failed to save generated document",
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to save document: %w", err)
	}

	s.logger.Info("PDF generated successfully",
		slog.String("documentUUID", doc.UUID),
		slog.String("templateUUID", input.TemplateUUID),
		slog.Int64("fileSize", doc.FileSize),
	)

	return doc, nil
}

// GeneratePDFFromContent generates a PDF from raw HTML/CSS content without saving
func (s *pdfService) GeneratePDFFromContent(ctx context.Context, htmlContent string, cssContent string, opts *config.PDFOptions) ([]byte, error) {
	if !s.gotenbergClient.IsAvailable() {
		return nil, ErrServiceUnavailable
	}

	// Render template (combine HTML and CSS)
	html, err := s.templateEngine.RenderString(ctx, htmlContent, cssContent, map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to render content: %w", err)
	}

	if opts == nil {
		opts = config.DefaultPDFOptions()
	}

	return s.gotenbergClient.ConvertHTMLToPDF(ctx, html, opts)
}

// PreviewHTML renders a template to HTML without generating PDF
func (s *pdfService) PreviewHTML(ctx context.Context, input *models.PreviewHTMLInput) (string, error) {
	if input.TemplateUUID == "" {
		return "", ErrTemplateRequired
	}
	if input.Data == nil {
		return "", ErrDataRequired
	}

	// Get template
	template, err := s.templateRepo.GetByUUID(ctx, input.TemplateUUID)
	if err != nil {
		return "", fmt.Errorf("failed to get template: %w", err)
	}

	// Render HTML
	html, err := s.templateEngine.Render(ctx, template, input.Data)
	if err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return html, nil
}

// PreviewHTMLFromContent renders raw HTML/CSS with data without a saved template
func (s *pdfService) PreviewHTMLFromContent(ctx context.Context, input *models.PreviewHTMLFromContentInput) (string, error) {
	if input.HTMLContent == "" {
		return "", ErrTemplateContentRequired
	}
	if input.Data == nil {
		return "", ErrDataRequired
	}

	html, err := s.templateEngine.RenderString(ctx, input.HTMLContent, input.CSSContent, input.Data)
	if err != nil {
		return "", fmt.Errorf("failed to render content: %w", err)
	}

	return html, nil
}

// GetDocument retrieves a generated document by UUID (without binary content)
func (s *pdfService) GetDocument(ctx context.Context, uuid string) (*iface.GeneratedDocument, error) {
	return s.documentRepo.GetByUUID(ctx, uuid)
}

// GetDocumentContent retrieves the PDF binary content
func (s *pdfService) GetDocumentContent(ctx context.Context, uuid string) ([]byte, string, error) {
	doc, err := s.documentRepo.GetByUUIDWithContent(ctx, uuid)
	if err != nil {
		return nil, "", err
	}
	return doc.PDFContent, doc.FileName, nil
}

// GenerateInvoicePDF generates a PDF for an invoice
func (s *pdfService) GenerateInvoicePDF(ctx context.Context, invoiceData map[string]interface{}, templateUUID string, generatedBy string) (*iface.GeneratedDocument, error) {
	// If no template specified, use default invoice template
	if templateUUID == "" {
		defaultTemplate, err := s.templateRepo.GetDefault(ctx, models.TemplateTypeInvoice)
		if err != nil {
			return nil, fmt.Errorf("no default invoice template found: %w", err)
		}
		templateUUID = defaultTemplate.UUID
	}

	// Get invoice number for filename
	invoiceNumber := "invoice"
	if num, ok := invoiceData["number"].(string); ok {
		invoiceNumber = num
	}

	// Get source UUID if available
	sourceUUID := ""
	if id, ok := invoiceData["uuid"].(string); ok {
		sourceUUID = id
	}

	input := &models.GeneratePDFInput{
		TemplateUUID: templateUUID,
		Data:         invoiceData,
		FileName:     fmt.Sprintf("fattura_%s", invoiceNumber),
		SourceType:   iface.SourceTypeInvoice,
		SourceUUID:   sourceUUID,
	}

	return s.GeneratePDF(ctx, input, generatedBy)
}

// IsAvailable checks if the PDF generation service is available
func (s *pdfService) IsAvailable(ctx context.Context) bool {
	if err := s.gotenbergClient.Ping(ctx); err != nil {
		s.logger.Warn("Gotenberg service unavailable", slog.String("error", err.Error()))
		return false
	}
	return true
}
