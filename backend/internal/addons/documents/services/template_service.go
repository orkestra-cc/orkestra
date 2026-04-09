package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"github.com/orkestra/backend/internal/addons/documents/repository"
)

// Errors for template service
var (
	ErrTemplateNameRequired    = errors.New("template name is required")
	ErrTemplateContentRequired = errors.New("template HTML content is required")
	ErrInvalidTemplateType     = errors.New("invalid template type")
	ErrInvalidPageSize         = errors.New("invalid page size")
	ErrInvalidOrientation      = errors.New("invalid page orientation")
)

// TemplateService defines the interface for template business logic
type TemplateService interface {
	// CRUD operations
	Create(ctx context.Context, input *models.CreateTemplateInput, createdBy string) (*models.Template, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Template, error)
	GetDefault(ctx context.Context, templateType models.TemplateType) (*models.Template, error)
	List(ctx context.Context, filters *models.TemplateFilters, pagination models.PaginationParams) (*models.TemplateListResponse, error)
	Update(ctx context.Context, uuid string, input *models.UpdateTemplateInput, updatedBy string) (*models.Template, error)
	Delete(ctx context.Context, uuid string) error

	// Template management
	SetDefault(ctx context.Context, uuid string) error
	Duplicate(ctx context.Context, uuid string, newName string, createdBy string) (*models.Template, error)

	// Template variables
	GetVariables(ctx context.Context, templateType models.TemplateType) (*models.TemplateVariablesResponse, error)

	// Seeding
	SeedBuiltInTemplates(ctx context.Context) error
}

type templateService struct {
	templateRepo   repository.TemplateRepository
	templateEngine TemplateEngine
	logger         *slog.Logger
}

// NewTemplateService creates a new template service
func NewTemplateService(
	templateRepo repository.TemplateRepository,
	templateEngine TemplateEngine,
	logger *slog.Logger,
) TemplateService {
	return &templateService{
		templateRepo:   templateRepo,
		templateEngine: templateEngine,
		logger:         logger,
	}
}

// Create creates a new template
func (s *templateService) Create(ctx context.Context, input *models.CreateTemplateInput, createdBy string) (*models.Template, error) {
	// Validate input
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}

	// Validate template content
	if err := s.templateEngine.Validate(ctx, input.HTMLContent); err != nil {
		return nil, err
	}

	// Set defaults
	pageSize := input.PageSize
	if pageSize == "" {
		pageSize = models.PageSizeA4
	}
	orientation := input.Orientation
	if orientation == "" {
		orientation = models.PageOrientationPortrait
	}
	margins := input.Margins
	if margins == nil {
		defaultMargins := models.DefaultMargins()
		margins = &defaultMargins
	}

	template := &models.Template{
		UUID:        uuid.New().String(),
		Name:        input.Name,
		Description: input.Description,
		Type:        input.Type,
		HTMLContent: input.HTMLContent,
		CSSContent:  input.CSSContent,
		PageSize:    pageSize,
		Orientation: orientation,
		Margins:     *margins,
		HeaderHTML:  input.HeaderHTML,
		FooterHTML:  input.FooterHTML,
		IsDefault:   false,
		IsBuiltIn:   false,
		IsActive:    true,
		Version:     1,
		CreatedBy:   createdBy,
	}

	if err := s.templateRepo.Create(ctx, template); err != nil {
		s.logger.Error("Failed to create template",
			slog.String("name", input.Name),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("Template created",
		slog.String("uuid", template.UUID),
		slog.String("name", template.Name),
		slog.String("type", string(template.Type)),
	)

	return template, nil
}

// GetByUUID retrieves a template by UUID
func (s *templateService) GetByUUID(ctx context.Context, uuid string) (*models.Template, error) {
	return s.templateRepo.GetByUUID(ctx, uuid)
}

// GetDefault retrieves the default template for a type
func (s *templateService) GetDefault(ctx context.Context, templateType models.TemplateType) (*models.Template, error) {
	return s.templateRepo.GetDefault(ctx, templateType)
}

// List retrieves templates with filters and pagination
func (s *templateService) List(ctx context.Context, filters *models.TemplateFilters, pagination models.PaginationParams) (*models.TemplateListResponse, error) {
	templates, total, err := s.templateRepo.List(ctx, filters, pagination)
	if err != nil {
		return nil, err
	}

	// Convert to list items
	items := make([]models.TemplateListItem, len(templates))
	for i, t := range templates {
		items[i] = t.ToListItem()
	}

	// Calculate total pages
	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	return &models.TemplateListResponse{
		Templates:  items,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

// Update updates a template
func (s *templateService) Update(ctx context.Context, uuid string, input *models.UpdateTemplateInput, updatedBy string) (*models.Template, error) {
	// Get existing template
	template, err := s.templateRepo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if input.Name != nil {
		if *input.Name == "" {
			return nil, ErrTemplateNameRequired
		}
		template.Name = *input.Name
	}
	if input.Description != nil {
		template.Description = *input.Description
	}
	if input.HTMLContent != nil {
		if *input.HTMLContent == "" {
			return nil, ErrTemplateContentRequired
		}
		// Validate new content
		if err := s.templateEngine.Validate(ctx, *input.HTMLContent); err != nil {
			return nil, err
		}
		template.HTMLContent = *input.HTMLContent
	}
	if input.CSSContent != nil {
		template.CSSContent = *input.CSSContent
	}
	if input.PageSize != nil {
		if !input.PageSize.IsValid() {
			return nil, ErrInvalidPageSize
		}
		template.PageSize = *input.PageSize
	}
	if input.Orientation != nil {
		if !input.Orientation.IsValid() {
			return nil, ErrInvalidOrientation
		}
		template.Orientation = *input.Orientation
	}
	if input.Margins != nil {
		template.Margins = *input.Margins
	}
	if input.HeaderHTML != nil {
		template.HeaderHTML = *input.HeaderHTML
	}
	if input.FooterHTML != nil {
		template.FooterHTML = *input.FooterHTML
	}
	if input.IsActive != nil {
		template.IsActive = *input.IsActive
	}

	template.UpdatedBy = updatedBy

	if err := s.templateRepo.Update(ctx, template); err != nil {
		s.logger.Error("Failed to update template",
			slog.String("uuid", uuid),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("Template updated",
		slog.String("uuid", template.UUID),
		slog.String("name", template.Name),
	)

	return template, nil
}

// Delete soft-deletes a template
func (s *templateService) Delete(ctx context.Context, uuid string) error {
	if err := s.templateRepo.SoftDelete(ctx, uuid); err != nil {
		s.logger.Error("Failed to delete template",
			slog.String("uuid", uuid),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("Template deleted", slog.String("uuid", uuid))
	return nil
}

// SetDefault sets a template as the default for its type
func (s *templateService) SetDefault(ctx context.Context, uuid string) error {
	// Get template to find its type
	template, err := s.templateRepo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	if err := s.templateRepo.SetDefault(ctx, uuid, template.Type); err != nil {
		s.logger.Error("Failed to set default template",
			slog.String("uuid", uuid),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("Template set as default",
		slog.String("uuid", uuid),
		slog.String("type", string(template.Type)),
	)
	return nil
}

// Duplicate creates a copy of an existing template
func (s *templateService) Duplicate(ctx context.Context, uuid string, newName string, createdBy string) (*models.Template, error) {
	// Get existing template
	original, err := s.templateRepo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	// Create new template with same content
	input := &models.CreateTemplateInput{
		Name:        newName,
		Description: original.Description,
		Type:        original.Type,
		HTMLContent: original.HTMLContent,
		CSSContent:  original.CSSContent,
		PageSize:    original.PageSize,
		Orientation: original.Orientation,
		Margins:     &original.Margins,
		HeaderHTML:  original.HeaderHTML,
		FooterHTML:  original.FooterHTML,
	}

	return s.Create(ctx, input, createdBy)
}

// GetVariables returns available variables for a template type
func (s *templateService) GetVariables(ctx context.Context, templateType models.TemplateType) (*models.TemplateVariablesResponse, error) {
	vars := GetTemplateVariables(templateType)
	return &vars, nil
}

// SeedBuiltInTemplates seeds the default built-in templates
func (s *templateService) SeedBuiltInTemplates(ctx context.Context) error {
	// Check if templates already exist
	count, err := s.templateRepo.Count(ctx)
	if err != nil {
		return err
	}

	if count > 0 {
		s.logger.Info("Built-in templates already seeded", slog.Int64("count", count))
		return nil
	}

	templates := getBuiltInTemplates()
	if err := s.templateRepo.CreateBatch(ctx, templates); err != nil {
		s.logger.Error("Failed to seed built-in templates", slog.String("error", err.Error()))
		return err
	}

	s.logger.Info("Built-in templates seeded", slog.Int("count", len(templates)))
	return nil
}

func (s *templateService) validateCreateInput(input *models.CreateTemplateInput) error {
	if input.Name == "" {
		return ErrTemplateNameRequired
	}
	if input.HTMLContent == "" {
		return ErrTemplateContentRequired
	}
	if !input.Type.IsValid() {
		return ErrInvalidTemplateType
	}
	if input.PageSize != "" && !input.PageSize.IsValid() {
		return ErrInvalidPageSize
	}
	if input.Orientation != "" && !input.Orientation.IsValid() {
		return ErrInvalidOrientation
	}
	return nil
}

// getBuiltInTemplates returns the default built-in templates
func getBuiltInTemplates() []*models.Template {
	defaultMargins := models.DefaultMargins()

	return []*models.Template{
		{
			UUID:        uuid.New().String(),
			Name:        "Invoice - Default",
			Description: "Default Italian invoice template (Fattura)",
			Type:        models.TemplateTypeInvoice,
			HTMLContent: getDefaultInvoiceHTML(),
			CSSContent:  getDefaultInvoiceCSS(),
			PageSize:    models.PageSizeA4,
			Orientation: models.PageOrientationPortrait,
			Margins:     defaultMargins,
			IsDefault:   true,
			IsBuiltIn:   true,
			IsActive:    true,
			Version:     1,
		},
		{
			UUID:        uuid.New().String(),
			Name:        "Offer - Default",
			Description: "Default offer/quote template (Preventivo)",
			Type:        models.TemplateTypeOffer,
			HTMLContent: getDefaultOfferHTML(),
			CSSContent:  getDefaultOfferCSS(),
			PageSize:    models.PageSizeA4,
			Orientation: models.PageOrientationPortrait,
			Margins:     defaultMargins,
			IsDefault:   true,
			IsBuiltIn:   true,
			IsActive:    true,
			Version:     1,
		},
	}
}

// Default template content (will be replaced with embedded files later)
func getDefaultInvoiceHTML() string {
	return `<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <title>Fattura {{.number}}</title>
</head>
<body>
  <div class="header">
    <div class="title-section">
      <div class="invoice-title">FATTURA{{if .documentType}} <span class="doc-type">({{.documentType}})</span>{{end}}</div>
      <div class="invoice-meta">N. {{.number}} del {{formatDateIT .date}}</div>
    </div>
    <div class="company-info">
      <strong>{{.seller.name}}</strong><br>
      {{.seller.address}}<br>
      P.IVA: {{.seller.vatNumber}}
      {{if .seller.fiscalCode}}<br>C.F.: {{.seller.fiscalCode}}{{end}}
      {{if .seller.pec}}<br>PEC: {{.seller.pec}}{{end}}
      {{if .seller.email}}<br>Email: {{.seller.email}}{{end}}
      {{if .seller.phone}}<br>Tel: {{.seller.phone}}{{end}}
      {{if .seller.rea}}
      <br><span class="rea-info">REA: {{.seller.rea.office}}-{{.seller.rea.number}}</span>
      {{end}}
    </div>
  </div>

  <div class="customer-box">
    <strong>Spett.le</strong><br>
    {{.buyer.name}}<br>
    {{.buyer.address}}<br>
    {{if .buyer.vatNumber}}P.IVA: {{.buyer.vatNumber}}<br>{{end}}
    {{if .buyer.fiscalCode}}C.F.: {{.buyer.fiscalCode}}{{end}}
    {{if .buyer.email}}<br>Email: {{.buyer.email}}{{end}}
  </div>

  {{if .causale}}
  <div class="causale-section">
    <strong>Causale:</strong>
    {{range .causale}}<p>{{.}}</p>{{end}}
  </div>
  {{end}}

  <table class="items-table">
    <thead>
      <tr>
        <th class="col-num">#</th>
        <th class="col-desc">Descrizione</th>
        <th class="col-qty">Q.tà</th>
        <th class="col-unit">U.M.</th>
        <th class="col-price">Prezzo Unit.</th>
        <th class="col-vat">Aliq. IVA</th>
        <th class="col-total">Importo</th>
      </tr>
    </thead>
    <tbody>
      {{range $i, $line := .lines}}
      <tr>
        <td class="col-num">{{$line.LineNumber}}</td>
        <td class="col-desc">
          {{$line.Description}}
          {{if $line.Discounts}}
          <div class="line-discounts">
            {{range $line.Discounts}}
              {{if eq .Type "SC"}}
                <span class="discount">Sconto: {{if gt .Percentage 0}}{{formatNumber .Percentage 2}}%{{else}}{{formatMoneyEUR .Amount}}{{end}}</span>
              {{else}}
                <span class="surcharge">Magg.: {{if gt .Percentage 0}}{{formatNumber .Percentage 2}}%{{else}}{{formatMoneyEUR .Amount}}{{end}}</span>
              {{end}}
            {{end}}
          </div>
          {{end}}
        </td>
        <td class="col-qty">{{formatNumber $line.Quantity 2}}</td>
        <td class="col-unit">{{default "-" $line.Unit}}</td>
        <td class="col-price">{{formatMoneyEUR $line.UnitPrice}}</td>
        <td class="col-vat">{{if gt $line.VATRate 0}}{{formatNumber $line.VATRate 0}}%{{else}}{{$line.VATNature}}{{end}}</td>
        <td class="col-total">{{formatMoneyEUR $line.TotalPrice}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  {{if .vatSummary}}
  <div class="vat-summary">
    <h4>Riepilogo IVA</h4>
    <table class="vat-table">
      <thead>
        <tr>
          <th>Aliquota</th>
          <th>Natura</th>
          <th>Imponibile</th>
          <th>Imposta</th>
        </tr>
      </thead>
      <tbody>
        {{range .vatSummary}}
        <tr>
          <td>{{if gt .Rate 0}}{{formatNumber .Rate 0}}%{{else}}-{{end}}</td>
          <td>{{default "-" .Nature}}</td>
          <td>{{formatMoneyEUR .Taxable}}</td>
          <td>{{formatMoneyEUR .VAT}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  {{end}}

  <div class="totals-section">
    <div class="totals">
      {{if .cassaPrevidenziale}}
      <div class="totals-row subtotal">
        <span class="label">Cassa Previdenziale:</span>
        <span class="value">{{formatMoneyEUR .totalCassa}}</span>
      </div>
      {{end}}
      <div class="totals-row">
        <span class="label">Imponibile:</span>
        <span class="value">{{formatMoneyEUR .totalTaxable}}</span>
      </div>
      <div class="totals-row">
        <span class="label">IVA:</span>
        <span class="value">{{formatMoneyEUR .totalVAT}}</span>
      </div>
      {{if .bollo}}
      <div class="totals-row bollo">
        <span class="label">Bollo Virtuale:</span>
        <span class="value">{{formatMoneyEUR .bollo.amount}}</span>
      </div>
      <div class="bollo-note">
        Imposta di bollo importo 2 euro assolta in modo virtuale ai sensi dell'art.15 del D.P.R. 642/1972 e del DM 17/06/2014 e ss.mm
      </div>
      {{end}}
      {{if gt .rounding 0}}
      <div class="totals-row">
        <span class="label">Arrotondamento:</span>
        <span class="value">{{formatMoneyEUR .rounding}}</span>
      </div>
      {{end}}
      <div class="totals-row total-document">
        <span class="label">TOTALE DOCUMENTO:</span>
        <span class="value">{{formatMoneyEUR .totalAmount}}</span>
      </div>
      {{if .ritenuta}}
      <div class="totals-row ritenuta">
        <span class="label">Ritenuta d'Acconto:</span>
        <span class="value">- {{formatMoneyEUR .totalRitenuta}}</span>
      </div>
      <div class="totals-row total-final">
        <span class="label">NETTO A PAGARE:</span>
        <span class="value">{{formatMoneyEUR .netPayable}}</span>
      </div>
      {{end}}
    </div>
  </div>

  {{if .ritenuta}}
  <div class="ritenuta-details">
    <h4>Dettaglio Ritenuta d'Acconto</h4>
    <table class="ritenuta-table">
      <thead>
        <tr>
          <th>Tipo</th>
          <th>Aliquota</th>
          <th>Importo</th>
          <th>Causale</th>
        </tr>
      </thead>
      <tbody>
        {{range .ritenuta}}
        <tr>
          <td>{{.Type}}</td>
          <td>{{formatNumber .Rate 2}}%</td>
          <td>{{formatMoneyEUR .Amount}}</td>
          <td>{{default "-" .CausalePag}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  {{end}}

  {{if .cassaPrevidenziale}}
  <div class="cassa-details">
    <h4>Contributi Cassa Previdenziale</h4>
    <table class="cassa-table">
      <thead>
        <tr>
          <th>Tipo Cassa</th>
          <th>Aliquota</th>
          <th>Imponibile</th>
          <th>Importo</th>
        </tr>
      </thead>
      <tbody>
        {{range .cassaPrevidenziale}}
        <tr>
          <td>{{.Type}}</td>
          <td>{{formatNumber .Rate 2}}%</td>
          <td>{{formatMoneyEUR .Taxable}}</td>
          <td>{{formatMoneyEUR .Amount}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  {{end}}

  {{if .payment}}
  <div class="payment-section">
    <h4>Modalità di Pagamento</h4>
    <div class="payment-info">
      <p><strong>Metodo:</strong> {{.paymentTerms}}</p>
      {{if .dueDate}}<p><strong>Scadenza:</strong> {{formatDateIT .dueDate}}</p>{{end}}
      {{if .payment.beneficiario}}<p><strong>Beneficiario:</strong> {{.payment.beneficiario}}</p>{{end}}
      {{if .payment.istitutoFinanziario}}<p><strong>Istituto Finanziario:</strong> {{.payment.istitutoFinanziario}}</p>{{end}}
      {{if .payment.iban}}<p><strong>IBAN:</strong> {{.payment.iban}}</p>{{end}}
      {{if .payment.bic}}<p><strong>BIC/SWIFT:</strong> {{.payment.bic}}</p>{{end}}
    </div>
    {{if .payment.installments}}
    <table class="installments-table">
      <thead>
        <tr>
          <th>Rata</th>
          <th>Scadenza</th>
          <th>Importo</th>
          <th>Stato</th>
        </tr>
      </thead>
      <tbody>
        {{range $i, $inst := .payment.installments}}
        <tr>
          <td>{{lineNumber $i}}</td>
          <td>{{formatDateIT $inst.DueDate}}</td>
          <td>{{formatMoneyEUR $inst.Amount}}</td>
          <td>{{if $inst.Paid}}Pagato{{else}}Da pagare{{end}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
  </div>
  {{end}}

  {{if .notes}}
  <div class="notes">
    <strong>Note:</strong><br>
    {{.notes}}
  </div>
  {{end}}

  <div class="footer">
    <p class="legal-notice">Documento informatico ai sensi dell'art. 21 D.Lgs. 82/2005</p>
  </div>
</body>
</html>`
}

func getDefaultInvoiceCSS() string {
	return `@page {
  size: A4;
  margin: 15mm;
}

body {
  font-family: 'Helvetica Neue', Arial, sans-serif;
  font-size: 9pt;
  line-height: 1.4;
  color: #333;
  margin: 0;
  padding: 0;
}

.header {
  display: flex;
  justify-content: space-between;
  margin-bottom: 20px;
  padding-bottom: 15px;
  border-bottom: 2px solid #333;
}

.title-section {
  flex: 1;
}

.invoice-title {
  font-size: 24pt;
  font-weight: bold;
  color: #333;
  margin-bottom: 5px;
}

.doc-type {
  font-size: 12pt;
  color: #666;
  font-weight: normal;
}

.invoice-meta {
  font-size: 10pt;
  color: #666;
}

.company-info {
  text-align: right;
  font-size: 8pt;
  line-height: 1.5;
}

.rea-info {
  font-size: 7pt;
  color: #888;
}

.customer-box {
  background: #f8f8f8;
  padding: 12px 15px;
  margin-bottom: 15px;
  border-left: 4px solid #333;
  font-size: 9pt;
  line-height: 1.5;
}

.causale-section {
  margin-bottom: 15px;
  padding: 10px 15px;
  background: #fafafa;
  border-left: 3px solid #666;
  font-size: 8pt;
}

.causale-section p {
  margin: 5px 0;
}

.items-table {
  width: 100%;
  border-collapse: collapse;
  margin: 15px 0;
  font-size: 8pt;
}

.items-table th {
  background: #333;
  color: white;
  padding: 8px 6px;
  text-align: left;
  font-weight: 600;
  font-size: 8pt;
  text-transform: uppercase;
}

.items-table td {
  padding: 8px 6px;
  border-bottom: 1px solid #ddd;
  vertical-align: top;
}

.col-num { width: 4%; text-align: center; }
.col-desc { width: 36%; }
.col-qty { width: 8%; text-align: right; }
.col-unit { width: 8%; text-align: center; }
.col-price { width: 14%; text-align: right; }
.col-vat { width: 10%; text-align: center; }
.col-total { width: 16%; text-align: right; }

.line-discounts {
  margin-top: 4px;
  font-size: 7pt;
}

.discount {
  color: #28a745;
  display: inline-block;
  margin-right: 8px;
}

.surcharge {
  color: #dc3545;
  display: inline-block;
  margin-right: 8px;
}

/* VAT Summary */
.vat-summary {
  margin: 15px 0;
  page-break-inside: avoid;
}

.vat-summary h4 {
  font-size: 9pt;
  margin-bottom: 8px;
  color: #333;
}

.vat-table {
  width: 60%;
  margin-left: auto;
  border-collapse: collapse;
  font-size: 8pt;
}

.vat-table th {
  background: #e9e9e9;
  padding: 6px 8px;
  text-align: left;
  font-weight: 600;
}

.vat-table td {
  padding: 6px 8px;
  border-bottom: 1px solid #ddd;
}

/* Totals Section */
.totals-section {
  margin-top: 15px;
  page-break-inside: avoid;
}

.totals {
  margin-left: auto;
  width: 300px;
}

.totals-row {
  display: flex;
  justify-content: space-between;
  padding: 6px 0;
  border-bottom: 1px solid #eee;
  font-size: 9pt;
}

.totals-row .label {
  color: #666;
}

.totals-row .value {
  font-weight: 500;
}

.totals-row.subtotal {
  font-size: 8pt;
  color: #666;
}

.totals-row.bollo {
  font-size: 8pt;
  background: #fff8e1;
}

.bollo-note {
  font-size: 7pt;
  color: #666;
  font-style: italic;
  margin-top: 4px;
  margin-bottom: 8px;
  padding-left: 4px;
}

.totals-row.ritenuta {
  color: #c62828;
  background: #ffebee;
}

.totals-row.total-document {
  font-weight: bold;
  font-size: 10pt;
  border-top: 1px solid #333;
  padding-top: 8px;
}

.total-final {
  border-bottom: none;
  border-top: 2px solid #333;
  padding-top: 10px;
  margin-top: 5px;
  font-size: 12pt;
  font-weight: bold;
}

.total-final .label,
.total-final .value {
  color: #333;
}

/* Ritenuta Details */
.ritenuta-details,
.cassa-details {
  margin-top: 15px;
  page-break-inside: avoid;
}

.ritenuta-details h4,
.cassa-details h4 {
  font-size: 9pt;
  margin-bottom: 8px;
  color: #333;
}

.ritenuta-table,
.cassa-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 8pt;
}

.ritenuta-table th,
.cassa-table th {
  background: #e9e9e9;
  padding: 6px 8px;
  text-align: left;
  font-weight: 600;
}

.ritenuta-table td,
.cassa-table td {
  padding: 6px 8px;
  border-bottom: 1px solid #ddd;
}

/* Payment Section */
.payment-section {
  margin-top: 20px;
  padding: 12px 15px;
  background: #f5f5f5;
  page-break-inside: avoid;
}

.payment-section h4 {
  font-size: 9pt;
  margin-bottom: 10px;
  color: #333;
}

.payment-info {
  font-size: 8pt;
}

.payment-info p {
  margin: 4px 0;
}

.installments-table {
  width: 100%;
  margin-top: 10px;
  border-collapse: collapse;
  font-size: 8pt;
}

.installments-table th {
  background: #e0e0e0;
  padding: 5px 8px;
  text-align: left;
}

.installments-table td {
  padding: 5px 8px;
  border-bottom: 1px solid #ddd;
}

/* Notes */
.notes {
  margin-top: 15px;
  padding: 12px 15px;
  background: #fffbf0;
  border-left: 4px solid #f0ad4e;
  font-size: 8pt;
}

/* Footer */
.footer {
  margin-top: 25px;
  padding-top: 10px;
  border-top: 1px solid #ddd;
  text-align: center;
}

.legal-notice {
  font-size: 7pt;
  color: #888;
  margin: 0;
}`
}

func getDefaultOfferHTML() string {
	return `<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <title>Preventivo {{.number}}</title>
</head>
<body>
  <div class="header">
    <div class="title-section">
      <div class="offer-title">PREVENTIVO</div>
      <div class="offer-meta">N. {{.number}} del {{formatDateIT .date}}</div>
      {{if .validUntil}}<div class="validity">Valido fino al: {{formatDateIT .validUntil}}</div>{{end}}
    </div>
    <div class="company-info">
      <strong>{{.company.name}}</strong><br>
      {{.company.address}}<br>
      {{if .company.vatNumber}}P.IVA: {{.company.vatNumber}}<br>{{end}}
      {{if .company.email}}{{.company.email}}<br>{{end}}
      {{if .company.phone}}Tel: {{.company.phone}}{{end}}
    </div>
  </div>

  <div class="customer-box">
    <strong>Cliente</strong><br>
    {{.customer.name}}<br>
    {{if .customer.address}}{{.customer.address}}<br>{{end}}
    {{if .customer.email}}{{.customer.email}}{{end}}
  </div>

  {{if .subject}}
  <div class="subject">
    <strong>Oggetto:</strong> {{.subject}}
  </div>
  {{end}}

  <table class="items-table">
    <thead>
      <tr>
        <th class="col-num">#</th>
        <th class="col-desc">Descrizione</th>
        <th class="col-qty">Q.tà</th>
        <th class="col-price">Prezzo Unit.</th>
        <th class="col-total">Totale</th>
      </tr>
    </thead>
    <tbody>
      {{range $i, $item := .items}}
      <tr>
        <td class="col-num">{{lineNumber $i}}</td>
        <td class="col-desc">{{$item.Description}}</td>
        <td class="col-qty">{{formatNumber $item.Quantity 2}}</td>
        <td class="col-price">{{formatMoneyEUR $item.UnitPrice}}</td>
        <td class="col-total">{{formatMoneyEUR $item.Total}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <div class="totals">
    <div class="totals-row">
      <span class="label">Subtotale:</span>
      <span class="value">{{formatMoneyEUR .subtotal}}</span>
    </div>
    {{if .tax}}
    <div class="totals-row">
      <span class="label">IVA:</span>
      <span class="value">{{formatMoneyEUR .tax}}</span>
    </div>
    {{end}}
    <div class="totals-row total-final">
      <span class="label">TOTALE:</span>
      <span class="value">{{formatMoneyEUR .total}}</span>
    </div>
  </div>

  {{if .notes}}
  <div class="notes">
    <strong>Note:</strong><br>
    {{.notes}}
  </div>
  {{end}}

  <div class="footer">
    <p>Questo preventivo è valido per 30 giorni dalla data di emissione, salvo diversamente specificato.</p>
  </div>
</body>
</html>`
}

func getDefaultOfferCSS() string {
	return `@page {
  size: A4;
  margin: 20mm;
}

body {
  font-family: 'Helvetica Neue', Arial, sans-serif;
  font-size: 10pt;
  line-height: 1.4;
  color: #333;
  margin: 0;
  padding: 0;
}

.header {
  display: flex;
  justify-content: space-between;
  margin-bottom: 30px;
  padding-bottom: 20px;
  border-bottom: 2px solid #2563eb;
}

.title-section {
  flex: 1;
}

.offer-title {
  font-size: 28pt;
  font-weight: bold;
  color: #2563eb;
  margin-bottom: 5px;
}

.offer-meta {
  font-size: 11pt;
  color: #666;
}

.validity {
  font-size: 10pt;
  color: #dc2626;
  margin-top: 5px;
}

.company-info {
  text-align: right;
  font-size: 9pt;
  line-height: 1.6;
}

.customer-box {
  background: #eff6ff;
  padding: 15px 20px;
  margin-bottom: 25px;
  border-left: 4px solid #2563eb;
  font-size: 10pt;
  line-height: 1.6;
}

.subject {
  margin-bottom: 20px;
  font-size: 11pt;
}

.items-table {
  width: 100%;
  border-collapse: collapse;
  margin: 25px 0;
}

.items-table th {
  background: #2563eb;
  color: white;
  padding: 12px 10px;
  text-align: left;
  font-weight: 600;
  font-size: 9pt;
  text-transform: uppercase;
}

.items-table td {
  padding: 12px 10px;
  border-bottom: 1px solid #ddd;
  font-size: 9pt;
}

.items-table tbody tr:hover {
  background: #f8fafc;
}

.col-num { width: 5%; text-align: center; }
.col-desc { width: 50%; }
.col-qty { width: 10%; text-align: right; }
.col-price { width: 17%; text-align: right; }
.col-total { width: 18%; text-align: right; }

.totals {
  margin-top: 20px;
  margin-left: auto;
  width: 280px;
}

.totals-row {
  display: flex;
  justify-content: space-between;
  padding: 8px 0;
  border-bottom: 1px solid #eee;
  font-size: 10pt;
}

.totals-row .label {
  color: #666;
}

.totals-row .value {
  font-weight: 500;
}

.total-final {
  border-bottom: none;
  border-top: 2px solid #2563eb;
  padding-top: 12px;
  margin-top: 5px;
  font-size: 14pt;
  font-weight: bold;
  color: #2563eb;
}

.notes {
  margin-top: 30px;
  padding: 15px;
  background: #fffbf0;
  border-left: 4px solid #f0ad4e;
  font-size: 9pt;
}

.footer {
  margin-top: 40px;
  padding-top: 20px;
  border-top: 1px solid #eee;
  font-size: 8pt;
  color: #666;
  text-align: center;
}`
}
