package models

import "time"

// ========================================
// Company DTOs (Issuing Company / Cedente Prestatore)
// ========================================

// CreateCompanyInput represents the input for creating an issuing company
type CreateCompanyInput struct {
	// Fiscal identifiers
	FiscalIDCountry string `json:"fiscalIdCountry" validate:"required,len=2" doc:"Country code (IT)"`
	FiscalIDCode    string `json:"fiscalIdCode" validate:"required" doc:"VAT number (P.IVA)"`
	CodiceFiscale   string `json:"codiceFiscale,omitempty" doc:"Fiscal code if different from P.IVA"`

	// Company data
	Denomination string `json:"denomination" validate:"required" doc:"Company name (Ragione sociale)"`

	// Fiscal regime
	RegimeFiscale RegimeFiscale `json:"regimeFiscale" validate:"required" doc:"Fiscal regime (RF01-RF19)"`

	// Address
	Address      string `json:"address" validate:"required" doc:"Street address (Indirizzo)"`
	NumeroCivico string `json:"numeroCivico,omitempty" doc:"Street number"`
	City         string `json:"city" validate:"required" doc:"City (Comune)"`
	Province     string `json:"province,omitempty" doc:"Province code (2 chars)"`
	PostalCode   string `json:"postalCode" validate:"required" doc:"Postal code (CAP)"`
	Country      string `json:"country" validate:"required,len=2" doc:"Country code ISO 3166-1 alpha-2"`

	// REA registration (flat fields for easier frontend integration)
	REAOffice          string   `json:"reaOffice,omitempty" doc:"REA office (province code)"`
	REANumber          string   `json:"reaNumber,omitempty" doc:"REA registration number"`
	CapitaleSociale    *float64 `json:"capitaleSociale,omitempty" doc:"Share capital"`
	SocioUnico         string   `json:"socioUnico,omitempty" doc:"SU=sole shareholder, SM=multiple shareholders"`
	StatoLiquidazione  string   `json:"statoLiquidazione,omitempty" doc:"LN=not in liquidation, LS=in liquidation"`

	// Contacts
	Email string `json:"email,omitempty" validate:"omitempty,email" doc:"Email address"`
	PEC   string `json:"pec,omitempty" validate:"omitempty,email" doc:"PEC address"`
	Phone string `json:"phone,omitempty" doc:"Phone number"`

	// Bank details
	IBAN                string `json:"iban,omitempty" doc:"IBAN for payments"`
	BIC                 string `json:"bic,omitempty" doc:"BIC/SWIFT code"`
	ABI                 string `json:"abi,omitempty" doc:"Italian bank code (ABI)"`
	CAB                 string `json:"cab,omitempty" doc:"Italian branch code (CAB)"`
	Beneficiario        string `json:"beneficiario,omitempty" doc:"Beneficiary name"`
	IstitutoFinanziario string `json:"istitutoFinanziario,omitempty" doc:"Bank name"`

	// Default flag
	IsDefault bool `json:"isDefault,omitempty" doc:"Set as default company for new invoices"`

	// Notes
	Notes string `json:"notes,omitempty" doc:"Internal notes"`

	// Professional flag
	IsProfessional bool `json:"isProfessional,omitempty" doc:"Professional (no withholding, with social security fund)"`
}

// UpdateCompanyInput represents the input for updating a company
type UpdateCompanyInput struct {
	Denomination        *string        `json:"denomination,omitempty"`
	CodiceFiscale       *string        `json:"codiceFiscale,omitempty" doc:"Fiscal code (required for XML export per D.P.R. 605-1973)"`
	RegimeFiscale       *RegimeFiscale `json:"regimeFiscale,omitempty"`
	Address             *string        `json:"address,omitempty"`
	NumeroCivico        *string        `json:"numeroCivico,omitempty"`
	City                *string        `json:"city,omitempty"`
	Province            *string        `json:"province,omitempty"`
	PostalCode          *string        `json:"postalCode,omitempty"`
	Country             *string        `json:"country,omitempty"`
	// REA registration (flat fields)
	REAOffice         *string  `json:"reaOffice,omitempty"`
	REANumber         *string  `json:"reaNumber,omitempty"`
	CapitaleSociale   *float64 `json:"capitaleSociale,omitempty"`
	SocioUnico        *string  `json:"socioUnico,omitempty"`
	StatoLiquidazione *string  `json:"statoLiquidazione,omitempty"`
	// Contacts
	Email *string `json:"email,omitempty"`
	PEC   *string `json:"pec,omitempty"`
	Phone *string `json:"phone,omitempty"`
	// Bank details
	IBAN                *string `json:"iban,omitempty"`
	BIC                 *string `json:"bic,omitempty"`
	ABI                 *string `json:"abi,omitempty"`
	CAB                 *string `json:"cab,omitempty"`
	Beneficiario        *string `json:"beneficiario,omitempty"`
	IstitutoFinanziario *string `json:"istitutoFinanziario,omitempty"`
	Notes               *string `json:"notes,omitempty"`
	IsProfessional      *bool   `json:"isProfessional,omitempty"`
}

// CompanyListResponse represents a paginated list of companies
type CompanyListResponse struct {
	Companies  []Company `json:"companies"`
	Total      int64     `json:"total"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	TotalPages int       `json:"totalPages"`
}

// ========================================
// Supplier DTOs
// ========================================

// CreateSupplierInput represents the input for creating a supplier
type CreateSupplierInput struct {
	// Fiscal identifiers
	FiscalIDCountry string `json:"fiscalIdCountry" validate:"required,len=2"`
	FiscalIDCode    string `json:"fiscalIdCode" validate:"required"`
	CodiceFiscale   string `json:"codiceFiscale,omitempty"`

	// Company/Person data
	IsCompany    bool   `json:"isCompany"`
	Denomination string `json:"denomination,omitempty"`
	Name         string `json:"name,omitempty"`
	Surname      string `json:"surname,omitempty"`

	// Fiscal regime
	RegimeFiscale RegimeFiscale `json:"regimeFiscale,omitempty"`

	// Address
	Address      string `json:"address" validate:"required"`
	NumeroCivico string `json:"numeroCivico,omitempty"`
	City         string `json:"city" validate:"required"`
	Province     string `json:"province,omitempty"`
	PostalCode   string `json:"postalCode" validate:"required"`
	Country      string `json:"country" validate:"required,len=2"`

	// Contacts
	Email string `json:"email,omitempty" validate:"omitempty,email"`
	PEC   string `json:"pec,omitempty" validate:"omitempty,email"`
	Phone string `json:"phone,omitempty"`

	// Bank details
	IBAN string `json:"iban,omitempty"`
	BIC  string `json:"bic,omitempty"`

	// Notes
	Notes string `json:"notes,omitempty"`
}

// UpdateSupplierInput represents the input for updating a supplier
type UpdateSupplierInput struct {
	Denomination  *string        `json:"denomination,omitempty"`
	Name          *string        `json:"name,omitempty"`
	Surname       *string        `json:"surname,omitempty"`
	RegimeFiscale *RegimeFiscale `json:"regimeFiscale,omitempty"`
	Address       *string        `json:"address,omitempty"`
	NumeroCivico  *string        `json:"numeroCivico,omitempty"`
	City          *string        `json:"city,omitempty"`
	Province      *string        `json:"province,omitempty"`
	PostalCode    *string        `json:"postalCode,omitempty"`
	Email         *string        `json:"email,omitempty"`
	PEC           *string        `json:"pec,omitempty"`
	Phone         *string        `json:"phone,omitempty"`
	IBAN          *string        `json:"iban,omitempty"`
	BIC           *string        `json:"bic,omitempty"`
	Notes         *string        `json:"notes,omitempty"`
}

// SupplierListResponse represents a paginated list of suppliers
type SupplierListResponse struct {
	Suppliers  []Supplier `json:"suppliers"`
	Total      int64      `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
	TotalPages int        `json:"totalPages"`
}

// ========================================
// Invoice DTOs
// ========================================

// CreateInvoiceInput represents the input for creating an invoice
type CreateInvoiceInput struct {
	// Document type
	DocumentType DocumentType `json:"documentType" validate:"required" doc:"Document type (TD01, TD04, etc.)"`

	// Document data
	Number   string    `json:"number" validate:"required" doc:"Invoice number"`
	Date     time.Time `json:"date" validate:"required" doc:"Invoice date"`
	Currency string    `json:"currency,omitempty" doc:"Currency code (default: EUR)"`

	// Company (seller/provider for issued invoices)
	CompanyID string `json:"companyId,omitempty" doc:"Reference to issuing company (uses default if not specified)"`

	// Tenant (Tier-2 client) whose FatturaPA profile drives the
	// CessionarioCommittente snapshot. The unified-client model resolves
	// this through BillingTenantProvider.ResolveBillingParty — the tenant
	// must carry IsItalianBillable=true and a FatturaPA routing handle
	// (CodiceDestinatario or PECDestinatario) for the send path to succeed.
	TenantUUID string `json:"tenantUUID,omitempty" doc:"Tier-2 tenant UUID (cessionario committente)"`

	// Invoice lines
	Lines []CreateInvoiceLineInput `json:"lines" validate:"required,min=1,dive" doc:"Invoice line items"`

	// Payment terms
	PaymentTerms *CreatePaymentTermsInput `json:"paymentTerms,omitempty" doc:"Payment terms"`

	// Related documents
	RelatedDocuments []RelatedDocument `json:"relatedDocuments,omitempty" doc:"Related documents (orders, contracts)"`

	// Additional data
	Causale       []string `json:"causale,omitempty" doc:"Invoice description/reason"`
	InternalNotes string   `json:"internalNotes,omitempty" doc:"Internal notes (not sent to SDI)"`

	// Stamp duty (bollo virtuale) - required for exempt invoices > €77.47
	DatiBollo *DatiBolloInput `json:"datiBollo,omitempty" doc:"Stamp duty data (bollo virtuale)"`

	// Storage options
	LegalStorageEnabled bool `json:"legalStorageEnabled,omitempty" doc:"Enable legal storage"`
	SignatureEnabled    bool `json:"signatureEnabled,omitempty" doc:"Enable digital signature"`
}

// CreateInvoiceLineInput represents input for a single invoice line
type CreateInvoiceLineInput struct {
	Description         string           `json:"description" validate:"required,max=1000"`
	Quantity            float64          `json:"quantity" validate:"required"`
	UnitOfMeasure       UnitOfMeasure    `json:"unitOfMeasure,omitempty"`
	UnitPrice           float64          `json:"unitPrice" validate:"required"`
	VATRate             float64          `json:"vatRate" validate:"required,min=0,max=100"`
	VATNature           VATNature        `json:"vatNature,omitempty"`
	Discounts           []LineDiscount   `json:"discounts,omitempty"`
	ProductCode         string           `json:"productCode,omitempty"`
	StartDate           *time.Time       `json:"startDate,omitempty"`
	EndDate             *time.Time       `json:"endDate,omitempty"`
	AltriDatiGestionali []AltriDatiInput `json:"altriDatiGestionali,omitempty" doc:"Additional management data"`
}

// CreatePaymentTermsInput represents input for payment terms
type CreatePaymentTermsInput struct {
	Condition     PaymentCondition `json:"condition" validate:"required"`
	PaymentMethod PaymentMethod    `json:"paymentMethod" validate:"required"`
	IBAN          string           `json:"iban,omitempty"`
	BIC           string           `json:"bic,omitempty"`
	ABI           string           `json:"abi,omitempty"`
	CAB           string           `json:"cab,omitempty"`
	Beneficiario        string `json:"beneficiario,omitempty" doc:"Payment beneficiary name"`
	IstitutoFinanziario string `json:"istitutoFinanziario,omitempty" doc:"Financial institution name"`
	DueDate       *time.Time       `json:"dueDate,omitempty"`
}

// UpdateInvoiceInput represents the input for updating an invoice (only draft)
type UpdateInvoiceInput struct {
	Number           *string                   `json:"number,omitempty"`
	Date             *time.Time                `json:"date,omitempty"`
	Lines            []CreateInvoiceLineInput  `json:"lines,omitempty"`
	PaymentTerms     *CreatePaymentTermsInput  `json:"paymentTerms,omitempty"`
	RelatedDocuments []RelatedDocument         `json:"relatedDocuments,omitempty"`
	Causale          []string                  `json:"causale,omitempty"`
	InternalNotes    *string                   `json:"internalNotes,omitempty"`
	DatiBollo        *DatiBolloInput           `json:"datiBollo,omitempty"`
}

// InvoiceListResponse represents a paginated list of invoices
type InvoiceListResponse struct {
	Invoices   []InvoiceSummary `json:"invoices"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	TotalPages int              `json:"totalPages"`
}

// InvoiceSummary represents a summary of an invoice for list views
type InvoiceSummary struct {
	UUID          string           `json:"id"`
	Direction     InvoiceDirection `json:"direction"`
	DocumentType  DocumentType     `json:"documentType"`
	Number        string           `json:"number"`
	Date          time.Time        `json:"date"`
	PartyName     string           `json:"partyName"` // Customer or Supplier name
	TotalAmount   float64          `json:"totalAmount"`
	Status        InvoiceStatus    `json:"status"`
	SDIStatus     SDIStatus        `json:"sdiStatus,omitempty"`
	CreatedAt     time.Time        `json:"createdAt"`
}

// InvoiceFilters for querying invoices
type InvoiceFilters struct {
	Direction    *InvoiceDirection `json:"direction,omitempty"`
	Status       *InvoiceStatus    `json:"status,omitempty"`
	SDIStatus    *SDIStatus        `json:"sdiStatus,omitempty"`
	TenantUUID   string            `json:"tenantUUID,omitempty"`
	SupplierID   string            `json:"supplierId,omitempty"`
	FromDate     *time.Time        `json:"fromDate,omitempty"`
	ToDate       *time.Time        `json:"toDate,omitempty"`
	Search       string            `json:"search,omitempty"` // Search in number, party name
	DocumentType *DocumentType     `json:"documentType,omitempty"`
}

// SendInvoiceResponse represents the response after sending an invoice to SDI
type SendInvoiceResponse struct {
	InvoiceUUID   string `json:"invoiceId"`
	OpenAPIUUID   string `json:"openApiUuid"`
	SDIIdentifier string `json:"sdiIdentifier,omitempty"`
	Status        InvoiceStatus `json:"status"`
	Message       string `json:"message"`
}

// ========================================
// Statistics DTOs
// ========================================

// WeeklyInvoiceData represents invoice data for a specific ISO week
type WeeklyInvoiceData struct {
	Year           int     `json:"year"`
	Week           int     `json:"week"` // ISO week number (1-53)
	IssuedCount    int64   `json:"issuedCount"`
	IssuedAmount   float64 `json:"issuedAmount"`
	ReceivedCount  int64   `json:"receivedCount"`
	ReceivedAmount float64 `json:"receivedAmount"`
}

// BillingStats represents billing statistics
type BillingStats struct {
	// Issued invoices
	IssuedTotal       int64   `json:"issuedTotal"`
	IssuedDraft       int64   `json:"issuedDraft"`
	IssuedSent        int64   `json:"issuedSent"`
	IssuedDelivered   int64   `json:"issuedDelivered"`
	IssuedRejected    int64   `json:"issuedRejected"`
	IssuedAmount      float64 `json:"issuedAmount"`

	// Received invoices
	ReceivedTotal     int64   `json:"receivedTotal"`
	ReceivedPending   int64   `json:"receivedPending"`
	ReceivedAccepted  int64   `json:"receivedAccepted"`
	ReceivedRejected  int64   `json:"receivedRejected"`
	ReceivedAmount    float64 `json:"receivedAmount"`

	// Notifications
	UnprocessedNotifications int64 `json:"unprocessedNotifications"`
	PendingActions           int64 `json:"pendingActions"`

	// Weekly breakdown
	WeeklyData []WeeklyInvoiceData `json:"weeklyData"`

	// Period
	PeriodStart time.Time `json:"periodStart"`
	PeriodEnd   time.Time `json:"periodEnd"`
}

// ========================================
// Pagination
// ========================================

// PaginationParams for list requests
type PaginationParams struct {
	Page     int `json:"page" validate:"min=1"`
	PageSize int `json:"pageSize" validate:"min=1,max=100"`
}

// DefaultPagination returns default pagination params
func DefaultPagination() PaginationParams {
	return PaginationParams{
		Page:     1,
		PageSize: 20,
	}
}

// ========================================
// Import Invoice DTOs
// ========================================

// ImportInvoiceInput represents the input for importing a supplier invoice
// Per OpenAPI SDI spec: POST /invoices/import
type ImportInvoiceInput struct {
	Invoice         string                 `json:"invoice" validate:"required" doc:"Base64-encoded FatturaPA XML content"`
	InvoiceFileName string                 `json:"invoice_file_name,omitempty" doc:"Optional filename for the invoice"`
	SDIID           string                 `json:"sdi_id,omitempty" doc:"Optional SDI identifier if already known"`
	Metadata        map[string]interface{} `json:"metadata,omitempty" doc:"Optional metadata for internal tracking"`
}

// ImportInvoiceResponse represents the response after importing an invoice
type ImportInvoiceResponse struct {
	UUIDs   []string `json:"uuids" doc:"List of imported invoice UUIDs"`
	Count   int      `json:"count" doc:"Number of invoices imported"`
	Message string   `json:"message" doc:"Status message"`
}

// ========================================
// Preserved Documents DTOs
// ========================================

// PreservedDocument represents the status of a preserved document in legal storage
// Per OpenAPI SDI spec: GET /preserved_documents/{uuid}
type PreservedDocument struct {
	UUID             string     `json:"uuid" doc:"Document UUID"`
	Status           string     `json:"status" doc:"Preservation status: to_be_stored, sent, stored, error"`
	ReceiptTimestamp *time.Time `json:"receipt_timestamp,omitempty" doc:"Timestamp when receipt was received"`
	Weight           int        `json:"weight,omitempty" doc:"Document weight in bytes"`
	ObjectID         string     `json:"object_id,omitempty" doc:"Storage object identifier"`
	ObjectType       string     `json:"object_type,omitempty" doc:"Type of stored object"`
}

// ========================================
// XML Import DTOs (Native FatturaPA Import)
// ========================================

// ImportXMLInput represents the input for importing invoices via native XML parsing
type ImportXMLInput struct {
	XML            string `json:"xml" validate:"required" doc:"FatturaPA XML content (raw or base64 encoded)"`
	FileName       string `json:"fileName,omitempty" doc:"Optional original filename for reference"`
	IsBase64       bool   `json:"isBase64,omitempty" doc:"Set to true if XML content is base64 encoded"`
	SkipDuplicates bool   `json:"skipDuplicates,omitempty" doc:"Skip invoices that already exist instead of returning error"`
}

// ImportXMLResponse represents the response after importing invoices via XML
type ImportXMLResponse struct {
	Invoices []ImportedInvoiceSummary `json:"invoices" doc:"List of successfully imported invoices"`
	Count    int                      `json:"count" doc:"Number of invoices imported"`
	Skipped  []SkippedInvoice         `json:"skipped,omitempty" doc:"List of invoices that were skipped"`
	Supplier *SupplierSummary         `json:"supplier,omitempty" doc:"Supplier information from the XML"`
	Message  string                   `json:"message" doc:"Status message"`
}

// ImportedInvoiceSummary represents summary information for an imported invoice
type ImportedInvoiceSummary struct {
	UUID         string    `json:"id" doc:"Invoice UUID"`
	Number       string    `json:"number" doc:"Invoice number"`
	Date         time.Time `json:"date" doc:"Invoice date"`
	TotalAmount  float64   `json:"totalAmount" doc:"Total invoice amount"`
	DocumentType string    `json:"documentType" doc:"Document type code (TD01, TD04, etc.)"`
}

// SkippedInvoice represents an invoice that was skipped during import
type SkippedInvoice struct {
	Number     string `json:"number" doc:"Invoice number that was skipped"`
	Reason     string `json:"reason" doc:"Reason for skipping"`
	ExistingID string `json:"existingId,omitempty" doc:"UUID of existing invoice if duplicate"`
}

// SupplierSummary represents supplier information extracted from imported XML
type SupplierSummary struct {
	UUID     string `json:"id" doc:"Supplier UUID"`
	Name     string `json:"name" doc:"Supplier display name"`
	FiscalID string `json:"fiscalId" doc:"Supplier fiscal ID (P.IVA)"`
	IsNew    bool   `json:"isNew" doc:"True if supplier was newly created during import"`
}

// ========================================
// Invoice Duplication DTOs
// ========================================

// DuplicateInvoiceInput represents the input for duplicating an invoice
type DuplicateInvoiceInput struct {
	Date *time.Time `json:"date,omitempty" doc:"Invoice date for the duplicate (default: today)"`
}
