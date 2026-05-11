package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Invoice represents a billing invoice (both issued and received)
type Invoice struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	// Unique identifier
	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Type and direction
	Direction    InvoiceDirection `bson:"direction" json:"direction" validate:"required,oneof=issued received"`
	DocumentType DocumentType     `bson:"documentType" json:"documentType" validate:"required"`

	// SDI identifiers
	SDIIdentifier    string `bson:"sdiIdentifier,omitempty" json:"sdiIdentifier,omitempty"`       // Identificativo SDI
	OpenAPIUUID      string `bson:"openApiUuid,omitempty" json:"openApiUuid,omitempty"`           // UUID from OpenAPI response
	ProgressivoInvio string `bson:"progressivoInvio,omitempty" json:"progressivoInvio,omitempty"` // Progressivo invio

	// Document data
	Number   string    `bson:"number" json:"number" validate:"required"`
	Date     time.Time `bson:"date" json:"date" validate:"required"`
	Currency string    `bson:"currency" json:"currency" validate:"required,len=3"` // ISO 4217 (default: EUR)

	// Related parties
	CompanyID  string `bson:"companyId,omitempty" json:"companyId,omitempty"`   // Reference to Company (for issued invoices)
	SupplierID string `bson:"supplierId,omitempty" json:"supplierId,omitempty"` // Reference to Supplier (for received invoices)
	// TenantUUID is the Tier-2 tenant whose FatturaPA profile drives the
	// CessionarioCommittente snapshot. Replaces the legacy CustomerID in the
	// Unified Client Aggregate model — billing.Customer was deleted in Phase
	// 5; the resolution path is now BillingTenantProvider.ResolveBillingParty.
	TenantUUID string `bson:"tenantUUID,omitempty" json:"tenantUUID,omitempty"`
	// CustomerID is retained as a legacy safety-belt field for one phase so
	// post-migration forensics can still trace an invoice back to its old
	// billing.Customer row. New invoices stamp TenantUUID instead. A
	// follow-up migration drops this field once invoice send is verified
	// working in production.
	//
	// Deprecated: use TenantUUID.
	CustomerID string `bson:"customerId,omitempty" json:"-"`

	// Party data snapshots (embedded for immutability)
	CedentePrestatore       *PartyData `bson:"cedentePrestatore" json:"cedentePrestatore"`             // Seller/Provider
	CessionarioCommittente  *PartyData `bson:"cessionarioCommittente" json:"cessionarioCommittente"`   // Buyer/Client

	// Invoice lines
	Lines []InvoiceLine `bson:"lines" json:"lines" validate:"required,min=1,dive"`

	// VAT summary (computed from lines)
	VATSummary []VATSummaryLine `bson:"vatSummary" json:"vatSummary"`

	// Totals
	TotalTaxableAmount float64 `bson:"totalTaxableAmount" json:"totalTaxableAmount"` // Imponibile totale
	TotalVATAmount     float64 `bson:"totalVatAmount" json:"totalVatAmount"`         // IVA totale
	TotalAmount        float64 `bson:"totalAmount" json:"totalAmount"`               // Totale documento

	// Rounding
	Rounding float64 `bson:"rounding,omitempty" json:"rounding,omitempty"` // Arrotondamento

	// Payment terms
	PaymentTerms *PaymentTerms `bson:"paymentTerms,omitempty" json:"paymentTerms,omitempty"`

	// Workflow status
	Status    InvoiceStatus `bson:"status" json:"status" validate:"required"`
	SDIStatus SDIStatus     `bson:"sdiStatus,omitempty" json:"sdiStatus,omitempty"`

	// Legal storage and signature
	LegalStorageEnabled bool   `bson:"legalStorageEnabled" json:"legalStorageEnabled"`
	SignatureEnabled    bool   `bson:"signatureEnabled" json:"signatureEnabled"`
	PreservedDocumentID string `bson:"preservedDocumentId,omitempty" json:"preservedDocumentId,omitempty"`

	// File content
	XMLContent string `bson:"xmlContent,omitempty" json:"-"`     // Raw XML content (not exposed in JSON)
	PDFPath    string `bson:"pdfPath,omitempty" json:"-"`        // Path to PDF file

	// References to other documents
	RelatedDocuments []RelatedDocument `bson:"relatedDocuments,omitempty" json:"relatedDocuments,omitempty"`

	// Withholding tax (ritenuta d'acconto) - for professionals/freelancers
	DatiRitenuta []DatiRitenutaInput `bson:"datiRitenuta,omitempty" json:"datiRitenuta,omitempty"`

	// Stamp duty (bollo virtuale) - required for exempt invoices > €77.47
	DatiBollo *DatiBolloInput `bson:"datiBollo,omitempty" json:"datiBollo,omitempty"`

	// Social security fund contributions (cassa previdenziale)
	DatiCassaPrevidenziale []DatiCassaInput `bson:"datiCassa,omitempty" json:"datiCassa,omitempty"`

	// Additional data
	Causale     []string          `bson:"causale,omitempty" json:"causale,omitempty"`         // Causale (max 200 chars each)
	Attachments []InvoiceAttachment `bson:"attachments,omitempty" json:"attachments,omitempty"`

	// Notes (internal)
	InternalNotes string `bson:"internalNotes,omitempty" json:"internalNotes,omitempty"`

	// Audit
	CreatedAt time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time  `bson:"updatedAt" json:"updatedAt"`
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"deletedAt,omitempty"`
	CreatedBy string     `bson:"createdBy" json:"createdBy"`
	SentAt    *time.Time `bson:"sentAt,omitempty" json:"sentAt,omitempty"`
	SentBy    string     `bson:"sentBy,omitempty" json:"sentBy,omitempty"`
}

// InvoiceLine represents a single line item in an invoice
type InvoiceLine struct {
	LineNumber  int    `bson:"lineNumber" json:"lineNumber" validate:"required,min=1"`
	Description string `bson:"description" json:"description" validate:"required,max=1000"`

	// Quantity and unit
	Quantity      float64       `bson:"quantity" json:"quantity" validate:"required"`
	UnitOfMeasure UnitOfMeasure `bson:"unitOfMeasure,omitempty" json:"unitOfMeasure,omitempty"`
	UnitPrice     float64       `bson:"unitPrice" json:"unitPrice" validate:"required"`

	// Discounts
	Discounts []LineDiscount `bson:"discounts,omitempty" json:"discounts,omitempty"`

	// Totals
	TotalPrice float64 `bson:"totalPrice" json:"totalPrice"` // Prezzo totale (qty * unitPrice - discounts)

	// VAT
	VATRate   float64   `bson:"vatRate" json:"vatRate"`                         // Aliquota IVA (es: 22.00)
	VATNature VATNature `bson:"vatNature,omitempty" json:"vatNature,omitempty"` // Natura IVA (se aliquota 0)
	VATAmount float64   `bson:"vatAmount" json:"vatAmount"`                     // Importo IVA calcolato

	// Withholding tax indicator
	Ritenuta bool `bson:"ritenuta,omitempty" json:"ritenuta,omitempty"` // SI if withholding tax applies

	// Additional info
	AdministrativeRef string        `bson:"administrativeRef,omitempty" json:"administrativeRef,omitempty"` // Riferimento amministrazione
	ProductCode       string        `bson:"productCode,omitempty" json:"productCode,omitempty"`             // Codice articolo (legacy single code)
	CodiciArticolo    []ProductCode `bson:"codiciArticolo,omitempty" json:"codiciArticolo,omitempty"`       // Multiple product codes (XSD unbounded)
	StartDate         *time.Time    `bson:"startDate,omitempty" json:"startDate,omitempty"`                 // Data inizio periodo
	EndDate           *time.Time    `bson:"endDate,omitempty" json:"endDate,omitempty"`                     // Data fine periodo

	// Additional management data (AltriDatiGestionali per FatturaPA)
	AltriDatiGestionali []AltriDatiInput `bson:"altriDatiGestionali,omitempty" json:"altriDatiGestionali,omitempty"`
}

// LineDiscount represents a discount applied to an invoice line
type LineDiscount struct {
	Type       string  `bson:"type" json:"type" validate:"required,oneof=SC MG"` // SC=sconto, MG=maggiorazione
	Percentage float64 `bson:"percentage,omitempty" json:"percentage,omitempty"` // Percentuale sconto
	Amount     float64 `bson:"amount,omitempty" json:"amount,omitempty"`         // Importo fisso sconto
}

// AltriDatiInput represents additional management data for a line item (AltriDatiGestionali)
// Used for automation, commercial data, and marketing purposes in FatturaPA
type AltriDatiInput struct {
	TipoDato          string     `bson:"tipoDato" json:"tipoDato" validate:"required,max=10"`                       // Data type identifier (max 10 chars)
	RiferimentoTesto  string     `bson:"riferimentoTesto,omitempty" json:"riferimentoTesto,omitempty" validate:"max=60"` // Text reference (max 60 chars)
	RiferimentoNumero float64    `bson:"riferimentoNumero,omitempty" json:"riferimentoNumero,omitempty"`             // Numeric reference
	RiferimentoData   *time.Time `bson:"riferimentoData,omitempty" json:"riferimentoData,omitempty"`                 // Date reference
}

// VATSummaryLine represents a VAT summary line (riepilogo IVA)
type VATSummaryLine struct {
	VATRate         float64   `bson:"vatRate" json:"vatRate"`
	VATNature       VATNature `bson:"vatNature,omitempty" json:"vatNature,omitempty"`
	TaxableAmount   float64   `bson:"taxableAmount" json:"taxableAmount"`     // Imponibile
	VATAmount       float64   `bson:"vatAmount" json:"vatAmount"`             // Imposta
	VATExigibility  string    `bson:"vatExigibility,omitempty" json:"vatExigibility,omitempty"` // I=immediata, D=differita, S=split payment
	NormativeRef    string    `bson:"normativeRef,omitempty" json:"normativeRef,omitempty"`    // Riferimento normativo
}

// PaymentTerms represents the payment terms for an invoice
type PaymentTerms struct {
	Condition     PaymentCondition `bson:"condition" json:"condition" validate:"required,oneof=TP01 TP02 TP03"`
	PaymentMethod PaymentMethod    `bson:"paymentMethod" json:"paymentMethod" validate:"required"`

	// Bank details for payment
	IBAN string `bson:"iban,omitempty" json:"iban,omitempty"`
	BIC  string `bson:"bic,omitempty" json:"bic,omitempty"`
	ABI  string `bson:"abi,omitempty" json:"abi,omitempty"` // 5 digits
	CAB  string `bson:"cab,omitempty" json:"cab,omitempty"` // 5 digits

	// Beneficiary and financial institution
	Beneficiario        string `bson:"beneficiario,omitempty" json:"beneficiario,omitempty"`
	IstitutoFinanziario string `bson:"istitutoFinanziario,omitempty" json:"istitutoFinanziario,omitempty"`

	// Payment deadline
	DueDate *time.Time `bson:"dueDate,omitempty" json:"dueDate,omitempty"`

	// For installment payments (TP01)
	Installments []PaymentInstallment `bson:"installments,omitempty" json:"installments,omitempty"`
}

// PaymentInstallment represents a single payment installment
type PaymentInstallment struct {
	DueDate time.Time `bson:"dueDate" json:"dueDate"`
	Amount  float64   `bson:"amount" json:"amount"`
	Paid    bool      `bson:"paid" json:"paid"`
	PaidAt  *time.Time `bson:"paidAt,omitempty" json:"paidAt,omitempty"`
}

// RelatedDocument represents a reference to another document
type RelatedDocument struct {
	Type      string     `bson:"type" json:"type"` // ordine, contratto, convenzione, ricezione, fattura collegata
	ID        string     `bson:"id,omitempty" json:"id,omitempty"`
	Date      *time.Time `bson:"date,omitempty" json:"date,omitempty"`
	Number    string     `bson:"number,omitempty" json:"number,omitempty"`
	CIG       string     `bson:"cig,omitempty" json:"cig,omitempty"` // Codice Identificativo Gara
	CUP       string     `bson:"cup,omitempty" json:"cup,omitempty"` // Codice Unico Progetto
	LineRef   string     `bson:"lineRef,omitempty" json:"lineRef,omitempty"`
}

// InvoiceAttachment represents an attachment to an invoice
type InvoiceAttachment struct {
	Name        string `bson:"name" json:"name" validate:"required"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	Format      string `bson:"format,omitempty" json:"format,omitempty"` // MIME type
	Content     string `bson:"content,omitempty" json:"-"`               // Base64 encoded content (not in JSON)
	FilePath    string `bson:"filePath,omitempty" json:"-"`              // Path to stored file
}

// DatiRitenutaInput represents withholding tax data (ritenuta d'acconto)
// Used for invoices to professionals/freelancers subject to withholding
type DatiRitenutaInput struct {
	TipoRitenuta     string  `bson:"tipoRitenuta" json:"tipoRitenuta" validate:"required,oneof=RT01 RT02 RT03 RT04 RT05 RT06"`
	ImportoRitenuta  float64 `bson:"importoRitenuta" json:"importoRitenuta" validate:"required"`  // Withholding amount
	AliquotaRitenuta float64 `bson:"aliquotaRitenuta" json:"aliquotaRitenuta" validate:"required"` // Withholding rate (e.g., 20.00)
	CausalePagamento string  `bson:"causalePagamento,omitempty" json:"causalePagamento,omitempty"` // Payment reason code (A-Z)
}

// DatiBolloInput represents stamp duty data (bollo virtuale)
// Required for exempt invoices exceeding €77.47
type DatiBolloInput struct {
	ImportoBollo float64 `bson:"importoBollo" json:"importoBollo"` // Usually 2.00 EUR
}

// DatiCassaInput represents social security fund contribution data
// Used for professionals with mandatory fund contributions (INPS, ENPAM, etc.)
type DatiCassaInput struct {
	TipoCassa              string  `bson:"tipoCassa" json:"tipoCassa" validate:"required"` // TC01-TC22
	AlCassa                float64 `bson:"alCassa" json:"alCassa" validate:"required"`     // Fund contribution rate
	ImportoContributoCassa float64 `bson:"importoCassa" json:"importoCassa" validate:"required"` // Contribution amount
	ImponibileCassa        float64 `bson:"imponibileCassa,omitempty" json:"imponibileCassa,omitempty"` // Taxable base
	AliquotaIVA            float64 `bson:"aliquotaIva" json:"aliquotaIva"`                 // VAT rate on contribution
	Ritenuta               bool    `bson:"ritenuta,omitempty" json:"ritenuta,omitempty"`  // Subject to withholding
	Natura                 string  `bson:"natura,omitempty" json:"natura,omitempty"`      // VAT nature if rate is 0
	RiferimentoAmm         string  `bson:"riferimentoAmm,omitempty" json:"riferimentoAmm,omitempty"` // Admin reference
}

// ProductCode represents a product code with type identifier
// Supports multiple coding systems (EAN, TARIC, internal codes, etc.)
type ProductCode struct {
	CodiceTipo   string `bson:"codiceTipo" json:"codiceTipo" validate:"required"`     // Code type (INTERNO, EAN, TARIC, etc.)
	CodiceValore string `bson:"codiceValore" json:"codiceValore" validate:"required"` // Code value
}

// CalculateTotals recalculates all totals from invoice lines
func (inv *Invoice) CalculateTotals() {
	vatSummaryMap := make(map[string]*VATSummaryLine)

	var totalTaxable float64
	var totalVAT float64

	for i := range inv.Lines {
		line := &inv.Lines[i]

		// Calculate line total with discounts
		lineTotal := line.Quantity * line.UnitPrice
		for _, discount := range line.Discounts {
			if discount.Type == "SC" { // Sconto
				if discount.Percentage > 0 {
					lineTotal -= lineTotal * (discount.Percentage / 100)
				} else if discount.Amount > 0 {
					lineTotal -= discount.Amount
				}
			} else if discount.Type == "MG" { // Maggiorazione
				if discount.Percentage > 0 {
					lineTotal += lineTotal * (discount.Percentage / 100)
				} else if discount.Amount > 0 {
					lineTotal += discount.Amount
				}
			}
		}
		line.TotalPrice = roundTo2Decimals(lineTotal)

		// Calculate VAT
		line.VATAmount = roundTo2Decimals(line.TotalPrice * (line.VATRate / 100))

		// Accumulate totals
		totalTaxable += line.TotalPrice
		totalVAT += line.VATAmount

		// Update VAT summary
		key := formatVATKey(line.VATRate, line.VATNature)
		if summary, exists := vatSummaryMap[key]; exists {
			summary.TaxableAmount += line.TotalPrice
			summary.VATAmount += line.VATAmount
		} else {
			vatSummaryMap[key] = &VATSummaryLine{
				VATRate:       line.VATRate,
				VATNature:     line.VATNature,
				TaxableAmount: line.TotalPrice,
				VATAmount:     line.VATAmount,
			}
		}
	}

	// Build VAT summary slice
	inv.VATSummary = make([]VATSummaryLine, 0, len(vatSummaryMap))
	for _, summary := range vatSummaryMap {
		summary.TaxableAmount = roundTo2Decimals(summary.TaxableAmount)
		summary.VATAmount = roundTo2Decimals(summary.VATAmount)
		inv.VATSummary = append(inv.VATSummary, *summary)
	}

	// Set totals
	// TotalAmount = Imponibile + IVA + Arrotondamento (bollo NON incluso)
	inv.TotalTaxableAmount = roundTo2Decimals(totalTaxable)
	inv.TotalVATAmount = roundTo2Decimals(totalVAT)
	inv.TotalAmount = roundTo2Decimals(totalTaxable + totalVAT + inv.Rounding)
}

// CanBeEdited returns true if the invoice can be modified
func (inv *Invoice) CanBeEdited() bool {
	return inv.Status == StatusDraft
}

// CanBeSent returns true if the invoice can be sent to SDI
func (inv *Invoice) CanBeSent() bool {
	return inv.Status == StatusDraft || inv.Status == StatusRejected
}

// CanBeDeleted returns true if the invoice can be deleted
func (inv *Invoice) CanBeDeleted() bool {
	return inv.Status == StatusDraft
}

// IsIssued returns true if this is an issued invoice (fattura attiva)
func (inv *Invoice) IsIssued() bool {
	return inv.Direction == DirectionIssued
}

// IsReceived returns true if this is a received invoice (fattura passiva)
func (inv *Invoice) IsReceived() bool {
	return inv.Direction == DirectionReceived
}

// Helper functions

func formatVATKey(rate float64, nature VATNature) string {
	if nature != "" {
		return string(nature)
	}
	return formatFloat(rate)
}

func formatFloat(f float64) string {
	return primitive.NewObjectID().Hex() // Placeholder, actual implementation would format the float
}

func roundTo2Decimals(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}
