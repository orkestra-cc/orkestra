package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// Common errors
var (
	ErrInvoiceNotFound       = errors.New("invoice not found")
	ErrInvoiceCannotEdit     = errors.New("invoice cannot be edited in current status")
	ErrInvoiceCannotSend     = errors.New("invoice cannot be sent in current status")
	ErrInvoiceCannotDelete   = errors.New("invoice cannot be deleted in current status")
	ErrInvoiceHTMLNotReady   = errors.New("HTML view not available: invoice has not been sent to SDI")
	ErrCustomerNotFound      = errors.New("customer not found")
	ErrSupplierNotFound      = errors.New("supplier not found")
	ErrInvalidInvoiceData    = errors.New("invalid invoice data")
	ErrInvoiceDuplicate      = errors.New("invoice already exists")
	ErrXMLParseError         = errors.New("failed to parse XML")
)

// InvoiceService defines the interface for invoice business logic
type InvoiceService interface {
	// Issued invoices (fatture attive)
	CreateInvoice(ctx context.Context, input *models.CreateInvoiceInput, createdBy string) (*models.Invoice, error)
	GetInvoice(ctx context.Context, uuid string) (*models.Invoice, error)
	ListInvoices(ctx context.Context, filters *models.InvoiceFilters, pagination models.PaginationParams) (*models.InvoiceListResponse, error)
	UpdateInvoice(ctx context.Context, uuid string, input *models.UpdateInvoiceInput) (*models.Invoice, error)
	DeleteInvoice(ctx context.Context, uuid string) error
	SendInvoice(ctx context.Context, uuid string, sentBy string) (*models.SendInvoiceResponse, error)
	GetInvoicePDF(ctx context.Context, uuid string) ([]byte, error)
	GetInvoiceXML(ctx context.Context, uuid string) (string, error)
	GetInvoiceHTML(ctx context.Context, uuid string) ([]byte, error)
	DuplicateInvoice(ctx context.Context, uuid string, input *models.DuplicateInvoiceInput, createdBy string) (*models.Invoice, error)

	// Received invoices (fatture passive)
	ListReceivedInvoices(ctx context.Context, filters *models.InvoiceFilters, pagination models.PaginationParams) (*models.InvoiceListResponse, error)
	AcceptReceivedInvoice(ctx context.Context, uuid string, acceptedBy string) error
	RejectReceivedInvoice(ctx context.Context, uuid string, reason string, rejectedBy string) error
	ImportInvoice(ctx context.Context, input *models.ImportInvoiceInput) (*models.ImportInvoiceResponse, error)
	// ImportXMLInvoice imports received invoices via native FatturaPA XML parsing
	ImportXMLInvoice(ctx context.Context, input *models.ImportXMLInput, importedBy string) (*models.ImportXMLResponse, error)

	// Legal storage / preserved documents
	GetPreservedDocument(ctx context.Context, uuid string) (*models.PreservedDocument, error)

	// Statistics
	GetStats(ctx context.Context, fromDate, toDate time.Time) (*models.BillingStats, error)
}

type invoiceService struct {
	invoiceRepo   repository.InvoiceRepository
	customerRepo  repository.CustomerRepository
	supplierRepo  repository.SupplierRepository
	companyRepo   repository.CompanyRepository
	openAPIClient OpenAPIClient
	xmlBuilder    XMLBuilder
	xmlParser     XMLParser
	pdfService    iface.PDFProvider // Optional: nil if documents module disabled
	logger        *slog.Logger
}

// NewInvoiceService creates a new InvoiceService
func NewInvoiceService(
	invoiceRepo repository.InvoiceRepository,
	customerRepo repository.CustomerRepository,
	supplierRepo repository.SupplierRepository,
	companyRepo repository.CompanyRepository,
	openAPIClient OpenAPIClient,
	xmlBuilder XMLBuilder,
	xmlParser XMLParser, // Can be nil, will be created if not provided
	pdfService iface.PDFProvider, // Can be nil if documents module is disabled
	logger *slog.Logger,
) InvoiceService {
	if xmlParser == nil {
		xmlParser = NewXMLParser()
	}
	return &invoiceService{
		invoiceRepo:   invoiceRepo,
		customerRepo:  customerRepo,
		supplierRepo:  supplierRepo,
		companyRepo:   companyRepo,
		openAPIClient: openAPIClient,
		xmlBuilder:    xmlBuilder,
		xmlParser:     xmlParser,
		pdfService:    pdfService,
		logger:        logger,
	}
}

func (s *invoiceService) CreateInvoice(ctx context.Context, input *models.CreateInvoiceInput, createdBy string) (*models.Invoice, error) {
	// Validate input
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}

	// Get company (seller/provider) data
	var companyData *models.PartyData
	if input.CompanyID != "" {
		// Use specified company
		company, err := s.companyRepo.GetByUUID(ctx, input.CompanyID)
		if err != nil {
			if errors.Is(err, repository.ErrCompanyNotFound) {
				return nil, errors.New("specified company not found")
			}
			return nil, err
		}
		companyData = company.ToPartyData()
	} else {
		// Use default company
		company, err := s.companyRepo.GetDefault(ctx)
		if err != nil {
			if errors.Is(err, repository.ErrNoDefaultCompany) {
				return nil, errors.New("no company configured: please add a company in settings before creating invoices")
			}
			return nil, err
		}
		companyData = company.ToPartyData()
	}

	// Get customer data
	var customerData *models.PartyData
	if input.CustomerID != "" {
		customer, err := s.customerRepo.GetByUUID(ctx, input.CustomerID)
		if err != nil {
			if errors.Is(err, repository.ErrCustomerNotFound) {
				return nil, ErrCustomerNotFound
			}
			return nil, err
		}
		customerData = customer.ToPartyData()
	}

	// Build invoice
	invoice := &models.Invoice{
		UUID:               uuid.New().String(),
		Direction:          models.DirectionIssued,
		DocumentType:       input.DocumentType,
		Number:             input.Number,
		Date:               input.Date,
		Currency:           "EUR",
		CompanyID:          input.CompanyID,
		CedentePrestatore:  companyData,
		CustomerID:         input.CustomerID,
		CessionarioCommittente: customerData,
		Status:             models.StatusDraft,
		LegalStorageEnabled: input.LegalStorageEnabled,
		SignatureEnabled:    input.SignatureEnabled,
		Causale:      input.Causale,
		InternalNotes: input.InternalNotes,
		RelatedDocuments: input.RelatedDocuments,
		CreatedBy:    createdBy,
		ProgressivoInvio: GenerateProgressivoInvio(),
	}

	if input.Currency != "" {
		invoice.Currency = input.Currency
	}

	// Convert lines
	invoice.Lines = make([]models.InvoiceLine, 0, len(input.Lines))
	for i, line := range input.Lines {
		invoiceLine := models.InvoiceLine{
			LineNumber:          i + 1,
			Description:         line.Description,
			Quantity:            line.Quantity,
			UnitOfMeasure:       line.UnitOfMeasure,
			UnitPrice:           line.UnitPrice,
			VATRate:             line.VATRate,
			VATNature:           line.VATNature,
			Discounts:           line.Discounts,
			ProductCode:         line.ProductCode,
			StartDate:           line.StartDate,
			EndDate:             line.EndDate,
			AltriDatiGestionali: line.AltriDatiGestionali,
		}
		invoice.Lines = append(invoice.Lines, invoiceLine)
	}

	// Calculate totals
	invoice.CalculateTotals()

	// Convert payment terms
	if input.PaymentTerms != nil {
		invoice.PaymentTerms = &models.PaymentTerms{
			Condition:           input.PaymentTerms.Condition,
			PaymentMethod:       input.PaymentTerms.PaymentMethod,
			IBAN:                input.PaymentTerms.IBAN,
			BIC:                 input.PaymentTerms.BIC,
			ABI:                 input.PaymentTerms.ABI,
			CAB:                 input.PaymentTerms.CAB,
			Beneficiario:        input.PaymentTerms.Beneficiario,
			IstitutoFinanziario: input.PaymentTerms.IstitutoFinanziario,
			DueDate:             input.PaymentTerms.DueDate,
		}
	}

	// Copy stamp duty data (bollo virtuale)
	if input.DatiBollo != nil {
		invoice.DatiBollo = input.DatiBollo
	}

	// Save to database
	if err := s.invoiceRepo.Create(ctx, invoice); err != nil {
		return nil, err
	}

	s.logger.Info("invoice created",
		"invoiceUUID", invoice.UUID,
		"number", invoice.Number,
		"createdBy", createdBy,
	)

	return invoice, nil
}

func (s *invoiceService) GetInvoice(ctx context.Context, uuid string) (*models.Invoice, error) {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}
	return invoice, nil
}

func (s *invoiceService) ListInvoices(ctx context.Context, filters *models.InvoiceFilters, pagination models.PaginationParams) (*models.InvoiceListResponse, error) {
	// Default to issued invoices
	direction := models.DirectionIssued
	if filters != nil && filters.Direction != nil {
		direction = *filters.Direction
	} else if filters == nil {
		filters = &models.InvoiceFilters{}
	}
	filters.Direction = &direction

	invoices, total, err := s.invoiceRepo.List(ctx, filters, pagination)
	if err != nil {
		return nil, err
	}

	// Convert to summaries
	summaries := make([]models.InvoiceSummary, 0, len(invoices))
	for _, inv := range invoices {
		summary := models.InvoiceSummary{
			UUID:         inv.UUID,
			Direction:    inv.Direction,
			DocumentType: inv.DocumentType,
			Number:       inv.Number,
			Date:         inv.Date,
			TotalAmount:  inv.TotalAmount,
			Status:       inv.Status,
			SDIStatus:    inv.SDIStatus,
			CreatedAt:    inv.CreatedAt,
		}

		// Get party name based on direction
		if inv.Direction == models.DirectionIssued && inv.CessionarioCommittente != nil {
			summary.PartyName = inv.CessionarioCommittente.GetDisplayName()
		} else if inv.Direction == models.DirectionReceived && inv.CedentePrestatore != nil {
			summary.PartyName = inv.CedentePrestatore.GetDisplayName()
		}

		summaries = append(summaries, summary)
	}

	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	return &models.InvoiceListResponse{
		Invoices:   summaries,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *invoiceService) UpdateInvoice(ctx context.Context, uuid string, input *models.UpdateInvoiceInput) (*models.Invoice, error) {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	// Check if invoice can be edited
	if !invoice.CanBeEdited() {
		return nil, ErrInvoiceCannotEdit
	}

	// Apply updates
	if input.Number != nil {
		invoice.Number = *input.Number
	}
	if input.Date != nil {
		invoice.Date = *input.Date
	}
	if input.InternalNotes != nil {
		invoice.InternalNotes = *input.InternalNotes
	}
	if input.Causale != nil {
		invoice.Causale = input.Causale
	}
	if input.RelatedDocuments != nil {
		invoice.RelatedDocuments = input.RelatedDocuments
	}

	// Update lines if provided
	if input.Lines != nil {
		invoice.Lines = make([]models.InvoiceLine, 0, len(input.Lines))
		for i, line := range input.Lines {
			invoiceLine := models.InvoiceLine{
				LineNumber:          i + 1,
				Description:         line.Description,
				Quantity:            line.Quantity,
				UnitOfMeasure:       line.UnitOfMeasure,
				UnitPrice:           line.UnitPrice,
				VATRate:             line.VATRate,
				VATNature:           line.VATNature,
				Discounts:           line.Discounts,
				ProductCode:         line.ProductCode,
				StartDate:           line.StartDate,
				EndDate:             line.EndDate,
				AltriDatiGestionali: line.AltriDatiGestionali,
			}
			invoice.Lines = append(invoice.Lines, invoiceLine)
		}
		// Recalculate totals
		invoice.CalculateTotals()
	}

	// Update payment terms if provided
	if input.PaymentTerms != nil {
		invoice.PaymentTerms = &models.PaymentTerms{
			Condition:           input.PaymentTerms.Condition,
			PaymentMethod:       input.PaymentTerms.PaymentMethod,
			IBAN:                input.PaymentTerms.IBAN,
			BIC:                 input.PaymentTerms.BIC,
			ABI:                 input.PaymentTerms.ABI,
			CAB:                 input.PaymentTerms.CAB,
			Beneficiario:        input.PaymentTerms.Beneficiario,
			IstitutoFinanziario: input.PaymentTerms.IstitutoFinanziario,
			DueDate:             input.PaymentTerms.DueDate,
		}
	}

	// Update stamp duty (bollo virtuale) if provided
	if input.DatiBollo != nil {
		invoice.DatiBollo = input.DatiBollo
	}

	// Save updates
	if err := s.invoiceRepo.Update(ctx, invoice); err != nil {
		return nil, err
	}

	return invoice, nil
}

func (s *invoiceService) DeleteInvoice(ctx context.Context, uuid string) error {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return ErrInvoiceNotFound
		}
		return err
	}

	// Check if invoice can be deleted
	if !invoice.CanBeDeleted() {
		return ErrInvoiceCannotDelete
	}

	return s.invoiceRepo.SoftDelete(ctx, uuid)
}

func (s *invoiceService) SendInvoice(ctx context.Context, uuid string, sentBy string) (*models.SendInvoiceResponse, error) {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	// Check if invoice can be sent
	if !invoice.CanBeSent() {
		return nil, ErrInvoiceCannotSend
	}

	// Build XML
	xmlContent, err := s.xmlBuilder.Build(invoice)
	if err != nil {
		s.logger.Error("failed to build invoice XML",
			"invoiceUUID", uuid,
			"error", err,
		)
		return nil, err
	}

	// Store XML content
	invoice.XMLContent = xmlContent

	// Send to OpenAPI SDI
	response, err := s.openAPIClient.SendInvoice(ctx, invoice, xmlContent)
	if err != nil {
		s.logger.Error("failed to send invoice to SDI",
			"invoiceUUID", uuid,
			"error", err,
		)
		return nil, err
	}

	// Update invoice with OpenAPI data
	now := time.Now()
	invoice.OpenAPIUUID = response.UUID
	invoice.SDIIdentifier = response.SDIIdentifier
	invoice.Status = models.StatusSent
	invoice.SentAt = &now
	invoice.SentBy = sentBy

	// Store legal storage and signature settings that were actually applied
	invoice.LegalStorageEnabled = response.LegalStorageApplied
	invoice.SignatureEnabled = response.SignatureApplied

	if err := s.invoiceRepo.Update(ctx, invoice); err != nil {
		s.logger.Error("failed to update invoice after sending",
			"invoiceUUID", uuid,
			"error", err,
		)
		return nil, err
	}

	s.logger.Info("invoice sent to SDI",
		"invoiceUUID", uuid,
		"openAPIUUID", response.UUID,
		"sentBy", sentBy,
		"legalStorageEnabled", invoice.LegalStorageEnabled,
		"signatureEnabled", invoice.SignatureEnabled,
	)

	return &models.SendInvoiceResponse{
		InvoiceUUID:   uuid,
		OpenAPIUUID:   response.UUID,
		SDIIdentifier: response.SDIIdentifier,
		Status:        models.StatusSent,
		Message:       "Invoice sent successfully to SDI",
	}, nil
}

func (s *invoiceService) GetInvoicePDF(ctx context.Context, uuid string) ([]byte, error) {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	// Always generate PDF locally using Gotenberg (documents module)
	// OpenAPI SDI does not provide PDF downloads - only XML
	if s.pdfService == nil {
		return nil, errors.New("PDF generation not available: documents module not configured")
	}

	// Convert invoice to template data
	data := s.invoiceToTemplateData(invoice)

	// Generate PDF (uses default invoice template if none specified)
	doc, err := s.pdfService.GenerateInvoicePDF(ctx, data, "", "system")
	if err != nil {
		s.logger.Error("Failed to generate invoice PDF",
			slog.String("invoiceUUID", uuid),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to generate PDF: %w", err)
	}

	// Get the PDF content
	content, _, err := s.pdfService.GetDocumentContent(ctx, doc.UUID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve PDF content: %w", err)
	}

	return content, nil
}

func (s *invoiceService) GetInvoiceXML(ctx context.Context, uuid string) (string, error) {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return "", ErrInvoiceNotFound
		}
		return "", err
	}

	// For draft/pending invoices, always regenerate XML to include latest fixes
	// For sent/delivered invoices, use cached XML (it's what was actually sent)
	if invoice.Status == models.StatusDraft || invoice.Status == models.StatusPending {
		xmlContent, err := s.xmlBuilder.Build(invoice)
		if err != nil {
			return "", err
		}
		return ensureXMLDeclaration(xmlContent), nil
	}

	// For sent invoices, return cached XML if available
	if invoice.XMLContent != "" {
		return ensureXMLDeclaration(invoice.XMLContent), nil
	}

	// Fallback: generate XML on the fly
	xmlContent, err := s.xmlBuilder.Build(invoice)
	if err != nil {
		return "", err
	}

	// Ensure XML declaration is properly formatted
	return ensureXMLDeclaration(xmlContent), nil
}

// ensureXMLDeclaration ensures the XML has the proper declaration prolog
// FatturaPA requires: <?xml version="1.0" encoding="UTF-8"?>
// The prolog MUST be at byte 0 with no leading whitespace or BOM
func ensureXMLDeclaration(xmlContent string) string {
	const xmlDeclaration = `<?xml version="1.0" encoding="UTF-8"?>`

	// Remove any BOM (Byte Order Mark) that might be present
	// UTF-8 BOM is EF BB BF (239, 187, 191)
	content := strings.TrimPrefix(xmlContent, "\xef\xbb\xbf")

	// Trim all leading/trailing whitespace - prolog must be at byte 0
	trimmed := strings.TrimSpace(content)

	// Check if already has declaration
	if strings.HasPrefix(trimmed, "<?xml") {
		// Verify it has the correct format, replace if not
		if strings.HasPrefix(trimmed, xmlDeclaration) {
			// Return trimmed to ensure no leading whitespace
			return trimmed
		}
		// Has a declaration but might be malformed, find the end and replace
		endIdx := strings.Index(trimmed, "?>")
		if endIdx != -1 {
			rest := strings.TrimSpace(trimmed[endIdx+2:])
			return xmlDeclaration + "\n" + rest
		}
	}

	return xmlDeclaration + "\n" + trimmed
}

func (s *invoiceService) ListReceivedInvoices(ctx context.Context, filters *models.InvoiceFilters, pagination models.PaginationParams) (*models.InvoiceListResponse, error) {
	// Force direction to received
	direction := models.DirectionReceived
	if filters == nil {
		filters = &models.InvoiceFilters{}
	}
	filters.Direction = &direction

	return s.ListInvoices(ctx, filters, pagination)
}

func (s *invoiceService) AcceptReceivedInvoice(ctx context.Context, uuid string, acceptedBy string) error {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return ErrInvoiceNotFound
		}
		return err
	}

	// Verify it's a received invoice
	if invoice.Direction != models.DirectionReceived {
		return errors.New("can only accept received invoices")
	}

	// Update status
	invoice.Status = models.StatusAccepted
	return s.invoiceRepo.Update(ctx, invoice)
}

func (s *invoiceService) RejectReceivedInvoice(ctx context.Context, uuid string, reason string, rejectedBy string) error {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return ErrInvoiceNotFound
		}
		return err
	}

	// Verify it's a received invoice
	if invoice.Direction != models.DirectionReceived {
		return errors.New("can only reject received invoices")
	}

	// Update status
	invoice.Status = models.StatusRejected
	invoice.InternalNotes = reason
	return s.invoiceRepo.Update(ctx, invoice)
}

func (s *invoiceService) GetStats(ctx context.Context, fromDate, toDate time.Time) (*models.BillingStats, error) {
	return s.invoiceRepo.GetStats(ctx, fromDate, toDate)
}

// GetInvoiceHTML returns the HTML representation of an invoice from OpenAPI SDI
// Per OpenAPI SDI spec: GET /invoices/{uuid}/html
func (s *invoiceService) GetInvoiceHTML(ctx context.Context, uuid string) ([]byte, error) {
	invoice, err := s.invoiceRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	// HTML is only available for invoices that have been sent to SDI
	if invoice.OpenAPIUUID == "" {
		return nil, ErrInvoiceHTMLNotReady
	}

	return s.openAPIClient.DownloadInvoiceHTML(ctx, invoice.OpenAPIUUID)
}

// DuplicateInvoice creates a copy of an existing invoice as a draft
func (s *invoiceService) DuplicateInvoice(ctx context.Context, invoiceUUID string, input *models.DuplicateInvoiceInput, createdBy string) (*models.Invoice, error) {
	// Get the original invoice
	original, err := s.invoiceRepo.GetByUUID(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, repository.ErrInvoiceNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	// Determine the date for the duplicate
	invoiceDate := time.Now()
	if input != nil && input.Date != nil {
		invoiceDate = *input.Date
	}

	// Create the duplicated invoice
	duplicate := &models.Invoice{
		// New identifiers
		UUID:             uuid.New().String(),
		ProgressivoInvio: GenerateProgressivoInvio(),

		// Copy document type and direction
		Direction:    original.Direction,
		DocumentType: original.DocumentType,
		Currency:     original.Currency,

		// Empty number - user will set before sending
		Number: "",
		Date:   invoiceDate,

		// Copy party references
		CompanyID:  original.CompanyID,
		CustomerID: original.CustomerID,
		SupplierID: original.SupplierID,

		// Deep copy party data snapshots
		CedentePrestatore:      deepCopyPartyData(original.CedentePrestatore),
		CessionarioCommittente: deepCopyPartyData(original.CessionarioCommittente),

		// Deep copy lines
		Lines: deepCopyInvoiceLines(original.Lines),

		// Deep copy VAT summary
		VATSummary: deepCopyVATSummary(original.VATSummary),

		// Copy totals
		TotalTaxableAmount: original.TotalTaxableAmount,
		TotalVATAmount:     original.TotalVATAmount,
		TotalAmount:        original.TotalAmount,
		Rounding:           original.Rounding,

		// Deep copy payment terms
		PaymentTerms: deepCopyPaymentTerms(original.PaymentTerms),

		// Status is always draft for duplicates
		Status:    models.StatusDraft,
		SDIStatus: "", // Reset SDI status

		// Copy storage/signature settings
		LegalStorageEnabled: original.LegalStorageEnabled,
		SignatureEnabled:    original.SignatureEnabled,

		// Deep copy related documents
		RelatedDocuments: deepCopyRelatedDocuments(original.RelatedDocuments),

		// Deep copy withholding tax data
		DatiRitenuta: deepCopyDatiRitenuta(original.DatiRitenuta),

		// Deep copy stamp duty
		DatiBollo: deepCopyDatiBollo(original.DatiBollo),

		// Deep copy social security fund contributions
		DatiCassaPrevidenziale: deepCopyDatiCassa(original.DatiCassaPrevidenziale),

		// Copy causale
		Causale: append([]string(nil), original.Causale...),

		// Set internal notes to indicate this is a duplicate
		InternalNotes: fmt.Sprintf("Duplicata da fattura %s", original.Number),

		// Audit fields
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),

		// Reset SDI-related fields
		SDIIdentifier: "",
		OpenAPIUUID:   "",
		XMLContent:    "",
		PDFPath:       "",
		SentAt:        nil,
		SentBy:        "",
	}

	// Save to database
	if err := s.invoiceRepo.Create(ctx, duplicate); err != nil {
		return nil, err
	}

	s.logger.Info("invoice duplicated",
		"originalUUID", original.UUID,
		"originalNumber", original.Number,
		"duplicateUUID", duplicate.UUID,
		"createdBy", createdBy,
	)

	return duplicate, nil
}

// Deep copy helper functions

func deepCopyPartyData(original *models.PartyData) *models.PartyData {
	if original == nil {
		return nil
	}
	result := *original
	// Deep copy nested IscrizioneREA
	if original.IscrizioneREA != nil {
		reaCopy := *original.IscrizioneREA
		result.IscrizioneREA = &reaCopy
	}
	return &result
}

func deepCopyInvoiceLines(original []models.InvoiceLine) []models.InvoiceLine {
	if original == nil {
		return nil
	}
	lines := make([]models.InvoiceLine, len(original))
	for i, line := range original {
		lines[i] = line
		// Deep copy slices within each line
		if line.Discounts != nil {
			lines[i].Discounts = make([]models.LineDiscount, len(line.Discounts))
			copy(lines[i].Discounts, line.Discounts)
		}
		if line.CodiciArticolo != nil {
			lines[i].CodiciArticolo = make([]models.ProductCode, len(line.CodiciArticolo))
			copy(lines[i].CodiciArticolo, line.CodiciArticolo)
		}
		if line.AltriDatiGestionali != nil {
			lines[i].AltriDatiGestionali = make([]models.AltriDatiInput, len(line.AltriDatiGestionali))
			copy(lines[i].AltriDatiGestionali, line.AltriDatiGestionali)
		}
		// Deep copy time pointers
		if line.StartDate != nil {
			startCopy := *line.StartDate
			lines[i].StartDate = &startCopy
		}
		if line.EndDate != nil {
			endCopy := *line.EndDate
			lines[i].EndDate = &endCopy
		}
	}
	return lines
}

func deepCopyVATSummary(original []models.VATSummaryLine) []models.VATSummaryLine {
	if original == nil {
		return nil
	}
	summary := make([]models.VATSummaryLine, len(original))
	copy(summary, original)
	return summary
}

func deepCopyPaymentTerms(original *models.PaymentTerms) *models.PaymentTerms {
	if original == nil {
		return nil
	}
	result := *original
	// Deep copy DueDate pointer
	if original.DueDate != nil {
		dueCopy := *original.DueDate
		result.DueDate = &dueCopy
	}
	// Deep copy installments
	if original.Installments != nil {
		result.Installments = make([]models.PaymentInstallment, len(original.Installments))
		for i, inst := range original.Installments {
			result.Installments[i] = inst
			// Deep copy PaidAt pointer
			if inst.PaidAt != nil {
				paidAtCopy := *inst.PaidAt
				result.Installments[i].PaidAt = &paidAtCopy
			}
		}
	}
	return &result
}

func deepCopyRelatedDocuments(original []models.RelatedDocument) []models.RelatedDocument {
	if original == nil {
		return nil
	}
	docs := make([]models.RelatedDocument, len(original))
	for i, doc := range original {
		docs[i] = doc
		// Deep copy Date pointer
		if doc.Date != nil {
			dateCopy := *doc.Date
			docs[i].Date = &dateCopy
		}
	}
	return docs
}

func deepCopyDatiRitenuta(original []models.DatiRitenutaInput) []models.DatiRitenutaInput {
	if original == nil {
		return nil
	}
	ritenute := make([]models.DatiRitenutaInput, len(original))
	copy(ritenute, original)
	return ritenute
}

func deepCopyDatiBollo(original *models.DatiBolloInput) *models.DatiBolloInput {
	if original == nil {
		return nil
	}
	result := *original
	return &result
}

func deepCopyDatiCassa(original []models.DatiCassaInput) []models.DatiCassaInput {
	if original == nil {
		return nil
	}
	casse := make([]models.DatiCassaInput, len(original))
	copy(casse, original)
	return casse
}

// ImportInvoice imports a supplier invoice via base64-encoded XML
// Per OpenAPI SDI spec: POST /invoices/import
func (s *invoiceService) ImportInvoice(ctx context.Context, input *models.ImportInvoiceInput) (*models.ImportInvoiceResponse, error) {
	// Call OpenAPI SDI to import the invoice
	openAPIInput := &ImportInvoiceInput{
		Invoice:         input.Invoice,
		InvoiceFileName: input.InvoiceFileName,
		SDIID:           input.SDIID,
		Metadata:        input.Metadata,
	}

	result, err := s.openAPIClient.ImportInvoice(ctx, openAPIInput)
	if err != nil {
		s.logger.Error("failed to import invoice",
			"error", err,
		)
		return nil, err
	}

	s.logger.Info("invoice imported successfully",
		"uuids", result.UUIDs,
		"count", result.Count,
	)

	return &models.ImportInvoiceResponse{
		UUIDs:   result.UUIDs,
		Count:   result.Count,
		Message: "Invoice(s) imported successfully",
	}, nil
}

// GetPreservedDocument retrieves the preservation status of a document
// Per OpenAPI SDI spec: GET /preserved_documents/{uuid}
func (s *invoiceService) GetPreservedDocument(ctx context.Context, uuid string) (*models.PreservedDocument, error) {
	result, err := s.openAPIClient.GetPreservedDocument(ctx, uuid)
	if err != nil {
		if errors.Is(err, ErrOpenAPINotFound) {
			return nil, errors.New("preserved document not found")
		}
		return nil, err
	}

	return &models.PreservedDocument{
		UUID:             result.UUID,
		Status:           result.Status,
		ReceiptTimestamp: result.ReceiptTimestamp,
		Weight:           result.Weight,
		ObjectID:         result.ObjectID,
		ObjectType:       result.ObjectType,
	}, nil
}

// ImportXMLInvoice imports received invoices via native FatturaPA XML parsing
func (s *invoiceService) ImportXMLInvoice(ctx context.Context, input *models.ImportXMLInput, importedBy string) (*models.ImportXMLResponse, error) {
	// Decode XML content if base64 encoded
	var xmlContent []byte
	var err error

	if input.IsBase64 {
		xmlContent, err = base64.StdEncoding.DecodeString(input.XML)
		if err != nil {
			s.logger.Error("failed to decode base64 XML",
				"error", err,
			)
			return nil, fmt.Errorf("%w: invalid base64 encoding", ErrXMLParseError)
		}
	} else {
		xmlContent = []byte(input.XML)
	}

	// Parse the XML
	fattura, err := s.xmlParser.Parse(xmlContent)
	if err != nil {
		s.logger.Error("failed to parse FatturaPA XML",
			"error", err,
		)
		return nil, fmt.Errorf("%w: %v", ErrXMLParseError, err)
	}

	// Get or create supplier from CedentePrestatore
	supplier, isNewSupplier, err := s.getOrCreateSupplierFromXML(ctx, &fattura.FatturaElettronicaHeader.CedentePrestatore, importedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to process supplier: %w", err)
	}

	// Process each invoice body
	var importedInvoices []models.ImportedInvoiceSummary
	var skippedInvoices []models.SkippedInvoice

	for _, body := range fattura.FatturaElettronicaBody {
		invoiceNumber := body.DatiGenerali.DatiGeneraliDocumento.Numero

		// Check for duplicates
		existingInvoice, err := s.invoiceRepo.FindByNumberAndSupplierFiscalID(ctx, invoiceNumber, supplier.FiscalIDCode)
		if err == nil && existingInvoice != nil {
			// Invoice already exists
			if input.SkipDuplicates {
				skippedInvoices = append(skippedInvoices, models.SkippedInvoice{
					Number:     invoiceNumber,
					Reason:     "duplicate",
					ExistingID: existingInvoice.UUID,
				})
				continue
			}
			return nil, fmt.Errorf("%w: invoice %s from supplier %s", ErrInvoiceDuplicate, invoiceNumber, supplier.FiscalIDCode)
		}

		// Map FatturaPA to Invoice model
		invoice, err := s.mapFatturaToInvoice(ctx, &fattura.FatturaElettronicaHeader, &body, string(xmlContent), supplier.UUID)
		if err != nil {
			s.logger.Error("failed to map fattura to invoice",
				"invoiceNumber", invoiceNumber,
				"error", err,
			)
			return nil, fmt.Errorf("failed to map invoice %s: %w", invoiceNumber, err)
		}

		invoice.CreatedBy = importedBy

		// Save the invoice
		if err := s.invoiceRepo.Create(ctx, invoice); err != nil {
			s.logger.Error("failed to save imported invoice",
				"invoiceNumber", invoiceNumber,
				"error", err,
			)
			return nil, fmt.Errorf("failed to save invoice %s: %w", invoiceNumber, err)
		}

		importedInvoices = append(importedInvoices, models.ImportedInvoiceSummary{
			UUID:         invoice.UUID,
			Number:       invoice.Number,
			Date:         invoice.Date,
			TotalAmount:  invoice.TotalAmount,
			DocumentType: string(invoice.DocumentType),
		})

		s.logger.Info("imported invoice from XML",
			"invoiceUUID", invoice.UUID,
			"invoiceNumber", invoice.Number,
			"supplierID", supplier.UUID,
			"importedBy", importedBy,
		)
	}

	// Build response
	response := &models.ImportXMLResponse{
		Invoices: importedInvoices,
		Count:    len(importedInvoices),
		Skipped:  skippedInvoices,
		Supplier: &models.SupplierSummary{
			UUID:     supplier.UUID,
			Name:     s.getSupplierDisplayName(supplier),
			FiscalID: supplier.FiscalIDCode,
			IsNew:    isNewSupplier,
		},
		Message: fmt.Sprintf("Successfully imported %d invoice(s)", len(importedInvoices)),
	}

	if len(skippedInvoices) > 0 {
		response.Message = fmt.Sprintf("Successfully imported %d invoice(s), skipped %d duplicate(s)", len(importedInvoices), len(skippedInvoices))
	}

	return response, nil
}

// getOrCreateSupplierFromXML gets or creates a supplier from CedentePrestatore XML data
func (s *invoiceService) getOrCreateSupplierFromXML(ctx context.Context, cedente *models.CedentePrestatore, createdBy string) (*models.Supplier, bool, error) {
	fiscalIDCode := cedente.DatiAnagrafici.IdFiscaleIVA.IdCodice
	fiscalIDCountry := cedente.DatiAnagrafici.IdFiscaleIVA.IdPaese

	// Default to IT if not specified
	if fiscalIDCountry == "" {
		fiscalIDCountry = "IT"
	}

	// Try to find existing supplier
	existing, err := s.supplierRepo.GetByFiscalID(ctx, fiscalIDCode)
	if err == nil && existing != nil {
		return existing, false, nil
	}

	// Create new supplier
	supplier := &models.Supplier{
		UUID:            uuid.New().String(),
		FiscalIDCountry: fiscalIDCountry,
		FiscalIDCode:    fiscalIDCode,
		CodiceFiscale:   cedente.DatiAnagrafici.CodiceFiscale,
		RegimeFiscale:   cedente.DatiAnagrafici.RegimeFiscale,
		IsActive:        true,
		CreatedBy:       createdBy,
	}

	// Set name/denomination
	anagrafica := cedente.DatiAnagrafici.Anagrafica
	if anagrafica.Denominazione != "" {
		supplier.IsCompany = true
		supplier.Denomination = anagrafica.Denominazione
	} else {
		supplier.IsCompany = false
		supplier.Name = anagrafica.Nome
		supplier.Surname = anagrafica.Cognome
	}

	// Set address from Sede
	sede := cedente.Sede
	supplier.Address = sede.Indirizzo
	supplier.NumeroCivico = sede.NumeroCivico
	supplier.PostalCode = sede.CAP
	supplier.City = sede.Comune
	supplier.Province = sede.Provincia
	supplier.Country = sede.Nazione

	// Set contacts if available
	if cedente.Contatti != nil {
		supplier.Email = cedente.Contatti.Email
		supplier.Phone = cedente.Contatti.Telefono
	}

	if err := s.supplierRepo.Create(ctx, supplier); err != nil {
		// If duplicate key error, try to fetch again (race condition)
		if errors.Is(err, repository.ErrSupplierAlreadyExists) {
			existing, fetchErr := s.supplierRepo.GetByFiscalID(ctx, fiscalIDCode)
			if fetchErr == nil && existing != nil {
				return existing, false, nil
			}
		}
		return nil, false, err
	}

	s.logger.Info("created new supplier from XML import",
		"supplierUUID", supplier.UUID,
		"fiscalID", fiscalIDCode,
	)

	return supplier, true, nil
}

// mapFatturaToInvoice maps FatturaPA XML structures to an Invoice model
func (s *invoiceService) mapFatturaToInvoice(ctx context.Context, header *models.FatturaElettronicaHeader, body *models.FatturaElettronicaBody, xmlContent string, supplierID string) (*models.Invoice, error) {
	datiDoc := body.DatiGenerali.DatiGeneraliDocumento

	// Parse invoice date
	invoiceDate, err := time.Parse("2006-01-02", datiDoc.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid invoice date format: %s", datiDoc.Data)
	}

	// Build invoice
	invoice := &models.Invoice{
		UUID:             uuid.New().String(),
		Direction:        models.DirectionReceived,
		DocumentType:     datiDoc.TipoDocumento,
		Number:           datiDoc.Numero,
		Date:             invoiceDate,
		Currency:         datiDoc.Divisa,
		SupplierID:       supplierID,
		Status:           models.StatusPending,
		XMLContent:       xmlContent,
		ProgressivoInvio: header.DatiTrasmissione.ProgressivoInvio,
	}

	// Default currency to EUR if not specified
	if invoice.Currency == "" {
		invoice.Currency = "EUR"
	}

	// Map CedentePrestatore (supplier)
	invoice.CedentePrestatore = s.mapCedenteToPartyData(&header.CedentePrestatore)

	// Map CessionarioCommittente (buyer/us)
	invoice.CessionarioCommittente = s.mapCessionarioToPartyData(&header.CessionarioCommittente)

	// Map Causale
	invoice.Causale = datiDoc.Causale

	// Map invoice lines
	invoice.Lines = s.mapDettaglioLineeToInvoiceLines(body.DatiBeniServizi.DettaglioLinee)

	// Map VAT summary
	invoice.VATSummary = s.mapDatiRiepilogoToVATSummary(body.DatiBeniServizi.DatiRiepilogo)

	// Parse and set totals
	if datiDoc.ImportoTotaleDocumento != "" {
		if total, err := strconv.ParseFloat(datiDoc.ImportoTotaleDocumento, 64); err == nil {
			invoice.TotalAmount = total
		}
	}

	// Calculate totals from lines if not set
	if invoice.TotalAmount == 0 {
		invoice.CalculateTotals()
	} else {
		// Calculate taxable and VAT amounts from VAT summary
		var taxableTotal, vatTotal float64
		for _, vs := range invoice.VATSummary {
			taxableTotal += vs.TaxableAmount
			vatTotal += vs.VATAmount
		}
		invoice.TotalTaxableAmount = taxableTotal
		invoice.TotalVATAmount = vatTotal
	}

	// Map payment terms if present
	if body.DatiPagamento != nil {
		invoice.PaymentTerms = s.mapDatiPagamentoToPaymentTerms(body.DatiPagamento)
	}

	// Map withholding tax (ritenuta d'acconto)
	if datiDoc.DatiRitenuta != nil {
		invoice.DatiRitenuta = []models.DatiRitenutaInput{{
			TipoRitenuta:     datiDoc.DatiRitenuta.TipoRitenuta,
			ImportoRitenuta:  parseFloat(datiDoc.DatiRitenuta.ImportoRitenuta),
			AliquotaRitenuta: parseFloat(datiDoc.DatiRitenuta.AliquotaRitenuta),
			CausalePagamento: datiDoc.DatiRitenuta.CausalePagamento,
		}}
	}

	// Map stamp duty (bollo)
	if datiDoc.DatiBollo != nil && datiDoc.DatiBollo.ImportoBollo != "" {
		invoice.DatiBollo = &models.DatiBolloInput{
			ImportoBollo: parseFloat(datiDoc.DatiBollo.ImportoBollo),
		}
	}

	// Map social security contributions (cassa previdenziale)
	if len(datiDoc.DatiCassaPrevidenziale) > 0 {
		invoice.DatiCassaPrevidenziale = make([]models.DatiCassaInput, len(datiDoc.DatiCassaPrevidenziale))
		for i, cassa := range datiDoc.DatiCassaPrevidenziale {
			invoice.DatiCassaPrevidenziale[i] = models.DatiCassaInput{
				TipoCassa:              cassa.TipoCassa,
				AlCassa:                parseFloat(cassa.AlCassa),
				ImportoContributoCassa: parseFloat(cassa.ImportoContributoCassa),
				ImponibileCassa:        parseFloat(cassa.ImponibileCassa),
				AliquotaIVA:            parseFloat(cassa.AliquotaIVA),
				Ritenuta:               cassa.Ritenuta == "SI",
				Natura:                 cassa.Natura,
				RiferimentoAmm:         cassa.RiferimentoAmm,
			}
		}
	}

	return invoice, nil
}

// mapCedenteToPartyData maps CedentePrestatore to PartyData
func (s *invoiceService) mapCedenteToPartyData(cedente *models.CedentePrestatore) *models.PartyData {
	party := &models.PartyData{
		FiscalIDCountry: cedente.DatiAnagrafici.IdFiscaleIVA.IdPaese,
		FiscalIDCode:    cedente.DatiAnagrafici.IdFiscaleIVA.IdCodice,
		CodiceFiscale:   cedente.DatiAnagrafici.CodiceFiscale,
		RegimeFiscale:   cedente.DatiAnagrafici.RegimeFiscale,
	}

	if party.FiscalIDCountry == "" {
		party.FiscalIDCountry = "IT"
	}

	// Name/denomination
	anagrafica := cedente.DatiAnagrafici.Anagrafica
	if anagrafica.Denominazione != "" {
		party.IsCompany = true
		party.Denomination = anagrafica.Denominazione
	} else {
		party.IsCompany = false
		party.Name = anagrafica.Nome
		party.Surname = anagrafica.Cognome
	}

	// Address
	party.Address = cedente.Sede.Indirizzo
	party.NumeroCivico = cedente.Sede.NumeroCivico
	party.PostalCode = cedente.Sede.CAP
	party.City = cedente.Sede.Comune
	party.Province = cedente.Sede.Provincia
	party.Country = cedente.Sede.Nazione

	// Contacts
	if cedente.Contatti != nil {
		party.Email = cedente.Contatti.Email
		party.Phone = cedente.Contatti.Telefono
	}

	// REA registration - only if ALL required fields are present
	// Per Article 2250 Civil Code, incomplete REA data should be omitted entirely
	if cedente.IscrizioneREA != nil &&
		cedente.IscrizioneREA.Ufficio != "" &&
		cedente.IscrizioneREA.NumeroREA != "" &&
		cedente.IscrizioneREA.StatoLiquidazione != "" {
		party.IscrizioneREA = &models.IscrizioneREAInput{
			Ufficio:           cedente.IscrizioneREA.Ufficio,
			NumeroREA:         cedente.IscrizioneREA.NumeroREA,
			CapitaleSociale:   parseFloat(cedente.IscrizioneREA.CapitaleSociale),
			SocioUnico:        cedente.IscrizioneREA.SocioUnico,
			StatoLiquidazione: cedente.IscrizioneREA.StatoLiquidazione,
		}
	}

	return party
}

// mapCessionarioToPartyData maps CessionarioCommittente to PartyData
func (s *invoiceService) mapCessionarioToPartyData(cessionario *models.CessionarioCommittente) *models.PartyData {
	party := &models.PartyData{}

	// Fiscal ID
	if cessionario.DatiAnagrafici.IdFiscaleIVA != nil {
		party.FiscalIDCountry = cessionario.DatiAnagrafici.IdFiscaleIVA.IdPaese
		party.FiscalIDCode = cessionario.DatiAnagrafici.IdFiscaleIVA.IdCodice
	}
	party.CodiceFiscale = cessionario.DatiAnagrafici.CodiceFiscale

	if party.FiscalIDCountry == "" {
		party.FiscalIDCountry = "IT"
	}

	// Name/denomination
	anagrafica := cessionario.DatiAnagrafici.Anagrafica
	if anagrafica.Denominazione != "" {
		party.IsCompany = true
		party.Denomination = anagrafica.Denominazione
	} else {
		party.IsCompany = false
		party.Name = anagrafica.Nome
		party.Surname = anagrafica.Cognome
	}

	// Address
	party.Address = cessionario.Sede.Indirizzo
	party.NumeroCivico = cessionario.Sede.NumeroCivico
	party.PostalCode = cessionario.Sede.CAP
	party.City = cessionario.Sede.Comune
	party.Province = cessionario.Sede.Provincia
	party.Country = cessionario.Sede.Nazione

	return party
}

// mapDettaglioLineeToInvoiceLines maps FatturaPA line items to InvoiceLine
func (s *invoiceService) mapDettaglioLineeToInvoiceLines(dettagli []models.DettaglioLinea) []models.InvoiceLine {
	lines := make([]models.InvoiceLine, len(dettagli))
	for i, det := range dettagli {
		line := models.InvoiceLine{
			LineNumber:  det.NumeroLinea,
			Description: det.Descrizione,
			Quantity:    parseFloat(det.Quantita),
			UnitPrice:   parseFloat(det.PrezzoUnitario),
			TotalPrice:  parseFloat(det.PrezzoTotale),
			VATRate:     parseFloat(det.AliquotaIVA),
			Ritenuta:    det.Ritenuta == "SI",
		}

		// Unit of measure
		if det.UnitaMisura != "" {
			line.UnitOfMeasure = models.UnitOfMeasure(det.UnitaMisura)
		}

		// VAT nature for zero-rate VAT
		if det.Natura != "" {
			line.VATNature = models.VATNature(det.Natura)
		}

		// Date period
		if det.DataInizioPeriodo != "" {
			if startDate, err := time.Parse("2006-01-02", det.DataInizioPeriodo); err == nil {
				line.StartDate = &startDate
			}
		}
		if det.DataFinePeriodo != "" {
			if endDate, err := time.Parse("2006-01-02", det.DataFinePeriodo); err == nil {
				line.EndDate = &endDate
			}
		}

		// Product codes
		if len(det.CodiceArticolo) > 0 {
			line.CodiciArticolo = make([]models.ProductCode, len(det.CodiceArticolo))
			for j, cod := range det.CodiceArticolo {
				line.CodiciArticolo[j] = models.ProductCode{
					CodiceTipo:   cod.CodiceTipo,
					CodiceValore: cod.CodiceValore,
				}
			}
		}

		// Discounts/surcharges
		if len(det.ScontoMaggiorazione) > 0 {
			line.Discounts = make([]models.LineDiscount, len(det.ScontoMaggiorazione))
			for j, sc := range det.ScontoMaggiorazione {
				line.Discounts[j] = models.LineDiscount{
					Type:       sc.Tipo,
					Percentage: parseFloat(sc.Percentuale),
					Amount:     parseFloat(sc.Importo),
				}
			}
		}

		// Calculate VAT amount
		line.VATAmount = line.TotalPrice * (line.VATRate / 100)

		lines[i] = line
	}
	return lines
}

// mapDatiRiepilogoToVATSummary maps FatturaPA VAT summary to VATSummaryLine
func (s *invoiceService) mapDatiRiepilogoToVATSummary(riepilogo []models.DatiRiepilogo) []models.VATSummaryLine {
	summary := make([]models.VATSummaryLine, len(riepilogo))
	for i, r := range riepilogo {
		summary[i] = models.VATSummaryLine{
			VATRate:        parseFloat(r.AliquotaIVA),
			TaxableAmount:  parseFloat(r.ImponibileImporto),
			VATAmount:      parseFloat(r.Imposta),
			VATExigibility: r.EsigibilitaIVA,
			NormativeRef:   r.RiferimentoNormativo,
		}
		if r.Natura != "" {
			summary[i].VATNature = models.VATNature(r.Natura)
		}
	}
	return summary
}

// mapDatiPagamentoToPaymentTerms maps FatturaPA payment data to PaymentTerms
func (s *invoiceService) mapDatiPagamentoToPaymentTerms(datiPag *models.DatiPagamento) *models.PaymentTerms {
	terms := &models.PaymentTerms{
		Condition: models.PaymentCondition(datiPag.CondizioniPagamento),
	}

	if len(datiPag.DettaglioPagamento) > 0 {
		firstDetail := datiPag.DettaglioPagamento[0]
		terms.PaymentMethod = models.PaymentMethod(firstDetail.ModalitaPagamento)
		terms.IBAN = firstDetail.IBAN
		terms.BIC = firstDetail.BIC
		terms.ABI = firstDetail.ABI
		terms.CAB = firstDetail.CAB
		terms.Beneficiario = firstDetail.Beneficiario
		terms.IstitutoFinanziario = firstDetail.IstitutoFinanziario

		// Parse due date
		if firstDetail.DataScadenzaPagamento != "" {
			if dueDate, err := time.Parse("2006-01-02", firstDetail.DataScadenzaPagamento); err == nil {
				terms.DueDate = &dueDate
			}
		}

		// Map installments if multiple payment details
		if len(datiPag.DettaglioPagamento) > 1 || terms.Condition == "TP01" {
			terms.Installments = make([]models.PaymentInstallment, len(datiPag.DettaglioPagamento))
			for i, det := range datiPag.DettaglioPagamento {
				inst := models.PaymentInstallment{
					Amount: parseFloat(det.ImportoPagamento),
				}
				if det.DataScadenzaPagamento != "" {
					if dueDate, err := time.Parse("2006-01-02", det.DataScadenzaPagamento); err == nil {
						inst.DueDate = dueDate
					}
				}
				terms.Installments[i] = inst
			}
		}
	}

	return terms
}

// getSupplierDisplayName returns the display name for a supplier
func (s *invoiceService) getSupplierDisplayName(supplier *models.Supplier) string {
	if supplier.IsCompany && supplier.Denomination != "" {
		return supplier.Denomination
	}
	name := strings.TrimSpace(supplier.Name + " " + supplier.Surname)
	if name != "" {
		return name
	}
	return supplier.FiscalIDCode
}

// parseFloat safely parses a string to float64, returning 0 on error
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	// Handle Italian number format (comma as decimal separator)
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func (s *invoiceService) validateCreateInput(input *models.CreateInvoiceInput) error {
	if input.Number == "" {
		return ErrInvalidInvoiceData
	}
	if input.Date.IsZero() {
		return ErrInvalidInvoiceData
	}
	if len(input.Lines) == 0 {
		return ErrInvalidInvoiceData
	}
	return nil
}

// invoiceToTemplateData converts an Invoice model to template-compatible map[string]interface{}
// Includes all FatturaPA-specific fields for comprehensive PDF generation
func (s *invoiceService) invoiceToTemplateData(invoice *models.Invoice) map[string]interface{} {
	data := map[string]interface{}{
		"uuid":         invoice.UUID,
		"number":       invoice.Number,
		"date":         invoice.Date,
		"currency":     invoice.Currency,
		"documentType": string(invoice.DocumentType),

		// Totals
		"totalTaxable": invoice.TotalTaxableAmount,
		"totalVAT":     invoice.TotalVATAmount,
		"totalAmount":  invoice.TotalAmount,
		"rounding":     invoice.Rounding,
	}

	// Seller (CedentePrestatore)
	if invoice.CedentePrestatore != nil {
		seller := map[string]interface{}{
			"name":          getPartyName(invoice.CedentePrestatore),
			"address":       formatAddress(invoice.CedentePrestatore),
			"vatNumber":     invoice.CedentePrestatore.FiscalIDCode,
			"fiscalCode":    invoice.CedentePrestatore.CodiceFiscale,
			"email":         invoice.CedentePrestatore.Email,
			"phone":         invoice.CedentePrestatore.Phone,
			"pec":           invoice.CedentePrestatore.PEC,
			"regimeFiscale": string(invoice.CedentePrestatore.RegimeFiscale),
		}
		// REA registration (Italian company register)
		if invoice.CedentePrestatore.IscrizioneREA != nil {
			seller["rea"] = map[string]interface{}{
				"office":           invoice.CedentePrestatore.IscrizioneREA.Ufficio,
				"number":           invoice.CedentePrestatore.IscrizioneREA.NumeroREA,
				"capitale":         invoice.CedentePrestatore.IscrizioneREA.CapitaleSociale,
				"socioUnico":       invoice.CedentePrestatore.IscrizioneREA.SocioUnico,
				"statoLiquidazione": invoice.CedentePrestatore.IscrizioneREA.StatoLiquidazione,
			}
		}
		data["seller"] = seller
	}

	// Buyer (CessionarioCommittente)
	if invoice.CessionarioCommittente != nil {
		data["buyer"] = map[string]interface{}{
			"name":       getPartyName(invoice.CessionarioCommittente),
			"address":    formatAddress(invoice.CessionarioCommittente),
			"vatNumber":  invoice.CessionarioCommittente.FiscalIDCode,
			"fiscalCode": invoice.CessionarioCommittente.CodiceFiscale,
			"email":      invoice.CessionarioCommittente.Email,
		}
	}

	// Lines with full details
	lines := make([]map[string]interface{}, len(invoice.Lines))
	for i, line := range invoice.Lines {
		lineData := map[string]interface{}{
			"LineNumber":  line.LineNumber,
			"Description": line.Description,
			"Quantity":    line.Quantity,
			"UnitPrice":   line.UnitPrice,
			"VATRate":     line.VATRate,
			"VATNature":   string(line.VATNature),
			"TotalPrice":  line.TotalPrice,
			"VATAmount":   line.VATAmount,
			"Unit":        string(line.UnitOfMeasure),
			"Ritenuta":    line.Ritenuta,
		}
		// Discounts
		if len(line.Discounts) > 0 {
			discounts := make([]map[string]interface{}, len(line.Discounts))
			for j, d := range line.Discounts {
				discounts[j] = map[string]interface{}{
					"Type":       d.Type,
					"Percentage": d.Percentage,
					"Amount":     d.Amount,
				}
			}
			lineData["Discounts"] = discounts
		}
		lines[i] = lineData
	}
	data["lines"] = lines

	// VAT Summary by rate
	if len(invoice.VATSummary) > 0 {
		vatSummary := make([]map[string]interface{}, len(invoice.VATSummary))
		for i, vs := range invoice.VATSummary {
			vatSummary[i] = map[string]interface{}{
				"Rate":           vs.VATRate,
				"Nature":         string(vs.VATNature),
				"Taxable":        vs.TaxableAmount,
				"VAT":            vs.VATAmount,
				"Deductible":     vs.VATExigibility,
				"RifNormativo":   vs.NormativeRef,
			}
		}
		data["vatSummary"] = vatSummary
	}

	// Payment terms with installments
	if invoice.PaymentTerms != nil {
		payment := map[string]interface{}{
			"condition":           string(invoice.PaymentTerms.Condition),
			"method":              string(invoice.PaymentTerms.PaymentMethod),
			"iban":                invoice.PaymentTerms.IBAN,
			"bic":                 invoice.PaymentTerms.BIC,
			"beneficiario":        invoice.PaymentTerms.Beneficiario,
			"istitutoFinanziario": invoice.PaymentTerms.IstitutoFinanziario,
		}
		if len(invoice.PaymentTerms.Installments) > 0 {
			installments := make([]map[string]interface{}, len(invoice.PaymentTerms.Installments))
			for i, inst := range invoice.PaymentTerms.Installments {
				installments[i] = map[string]interface{}{
					"DueDate": inst.DueDate,
					"Amount":  inst.Amount,
					"Paid":    inst.Paid,
				}
			}
			payment["installments"] = installments
			data["dueDate"] = invoice.PaymentTerms.Installments[0].DueDate
		} else if invoice.PaymentTerms.DueDate != nil {
			data["dueDate"] = *invoice.PaymentTerms.DueDate
		}
		data["payment"] = payment
		// Backward compatibility
		data["paymentTerms"] = getPaymentMethodDescription(invoice.PaymentTerms.PaymentMethod)
	}

	// Withholding tax (Ritenuta d'acconto)
	if len(invoice.DatiRitenuta) > 0 {
		ritenute := make([]map[string]interface{}, len(invoice.DatiRitenuta))
		var totalRitenuta float64
		for i, r := range invoice.DatiRitenuta {
			ritenute[i] = map[string]interface{}{
				"Type":       r.TipoRitenuta,
				"Rate":       r.AliquotaRitenuta,
				"Amount":     r.ImportoRitenuta,
				"CausalePag": r.CausalePagamento,
			}
			totalRitenuta += r.ImportoRitenuta
		}
		data["ritenuta"] = ritenute
		data["totalRitenuta"] = totalRitenuta
		// Calculate net payable (totale - ritenuta)
		data["netPayable"] = invoice.TotalAmount - totalRitenuta
	} else {
		data["netPayable"] = invoice.TotalAmount
	}

	// Stamp duty (Bollo) - either explicitly set or auto-detected per DPR 642/1972
	if invoice.DatiBollo != nil {
		data["bollo"] = map[string]interface{}{
			"virtual": true,
			"amount":  invoice.DatiBollo.ImportoBollo,
		}
	} else if shouldApplyStampDuty(invoice) {
		// Auto-detect stamp duty requirement per Italian law (same logic as XML builder)
		data["bollo"] = map[string]interface{}{
			"virtual": true,
			"amount":  2.00, // Standard €2.00 stamp duty
		}
	}

	// Social security fund (Cassa previdenziale)
	if len(invoice.DatiCassaPrevidenziale) > 0 {
		casse := make([]map[string]interface{}, len(invoice.DatiCassaPrevidenziale))
		var totalCassa float64
		for i, c := range invoice.DatiCassaPrevidenziale {
			casse[i] = map[string]interface{}{
				"Type":     c.TipoCassa,
				"Rate":     c.AlCassa,
				"Amount":   c.ImportoContributoCassa,
				"Taxable":  c.ImponibileCassa,
				"VATRate":  c.AliquotaIVA,
				"Ritenuta": c.Ritenuta,
				"Nature":   c.Natura,
			}
			totalCassa += c.ImportoContributoCassa
		}
		data["cassaPrevidenziale"] = casse
		data["totalCassa"] = totalCassa
	}

	// Causale (invoice description lines)
	if len(invoice.Causale) > 0 {
		data["causale"] = invoice.Causale
	}

	// Internal notes
	if invoice.InternalNotes != "" {
		data["notes"] = invoice.InternalNotes
	}

	return data
}

// getPartyName returns the display name for a party
func getPartyName(party *models.PartyData) string {
	if party.IsCompany && party.Denomination != "" {
		return party.Denomination
	}
	name := strings.TrimSpace(party.Name + " " + party.Surname)
	if name != "" {
		return name
	}
	return party.FiscalIDCode
}

// formatAddress formats a party's address for display
func formatAddress(party *models.PartyData) string {
	var parts []string

	// Street address with number
	address := party.Address
	if party.NumeroCivico != "" {
		address = party.Address + ", " + party.NumeroCivico
	}
	parts = append(parts, address)

	// City with postal code and province
	cityLine := party.PostalCode + " " + party.City
	if party.Province != "" {
		cityLine += " (" + party.Province + ")"
	}
	parts = append(parts, cityLine)

	return strings.Join(parts, " - ")
}

// getPaymentMethodDescription returns a human-readable description of the payment method
func getPaymentMethodDescription(method models.PaymentMethod) string {
	descriptions := map[models.PaymentMethod]string{
		"MP01": "Contanti",
		"MP02": "Assegno",
		"MP03": "Assegno circolare",
		"MP04": "Contanti presso Tesoreria",
		"MP05": "Bonifico",
		"MP06": "Vaglia cambiario",
		"MP07": "Bollettino bancario",
		"MP08": "Carta di pagamento",
		"MP09": "RID",
		"MP10": "RID utenze",
		"MP11": "RID veloce",
		"MP12": "RIBA",
		"MP13": "MAV",
		"MP14": "Quietanza erario",
		"MP15": "Giroconto su conti di contabilità speciale",
		"MP16": "Domiciliazione bancaria",
		"MP17": "Domiciliazione postale",
		"MP18": "Bollettino di c/c postale",
		"MP19": "SEPA Direct Debit",
		"MP20": "SEPA Direct Debit CORE",
		"MP21": "SEPA Direct Debit B2B",
		"MP22": "Trattenuta su somme già riscosse",
		"MP23": "PagoPA",
	}
	if desc, ok := descriptions[method]; ok {
		return desc
	}
	return string(method)
}
