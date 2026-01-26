package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Company represents the issuing company for invoices (cedente/prestatore)
// This is the seller/provider data that appears in the FatturaPA CedentePrestatore section
type Company struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	// Unique identifier
	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Fiscal identifiers
	FiscalIDCountry string `bson:"fiscalIdCountry" json:"fiscalIdCountry" validate:"required,len=2"` // IT
	FiscalIDCode    string `bson:"fiscalIdCode" json:"fiscalIdCode" validate:"required"`             // P.IVA (11 chars)
	CodiceFiscale   string `bson:"codiceFiscale,omitempty" json:"codiceFiscale,omitempty"`           // Codice Fiscale if different from P.IVA

	// Company data (always a company, not individual for this use case)
	Denomination string `bson:"denomination" json:"denomination" validate:"required"` // Ragione sociale

	// Fiscal regime (required for CedentePrestatore in FatturaPA)
	RegimeFiscale RegimeFiscale `bson:"regimeFiscale" json:"regimeFiscale" validate:"required"` // RF01-RF19

	// Address (Sede legale)
	Address      string `bson:"address" json:"address" validate:"required"`           // Indirizzo
	NumeroCivico string `bson:"numeroCivico,omitempty" json:"numeroCivico,omitempty"` // Street number (separate per XSD)
	City         string `bson:"city" json:"city" validate:"required"`                 // Comune
	Province     string `bson:"province,omitempty" json:"province,omitempty"`         // Provincia (2 chars)
	PostalCode   string `bson:"postalCode" json:"postalCode" validate:"required"`     // CAP (5 chars)
	Country      string `bson:"country" json:"country" validate:"required,len=2"`     // ISO 3166-1 alpha-2

	// REA registration (required for Italian companies - Registro Imprese)
	REAOffice         string   `bson:"reaOffice,omitempty" json:"reaOffice,omitempty"`                 // Province code (e.g., "RM")
	REANumber         string   `bson:"reaNumber,omitempty" json:"reaNumber,omitempty"`                 // REA registration number
	CapitaleSociale   *float64 `bson:"capitaleSociale,omitempty" json:"capitaleSociale,omitempty"`     // Share capital
	SocioUnico        string   `bson:"socioUnico,omitempty" json:"socioUnico,omitempty"`               // SU=sole shareholder, SM=multiple
	StatoLiquidazione string   `bson:"statoLiquidazione,omitempty" json:"statoLiquidazione,omitempty"` // LN=not liquidating, LS=liquidating

	// Contacts
	Email string `bson:"email,omitempty" json:"email,omitempty" validate:"omitempty,email"`
	PEC   string `bson:"pec,omitempty" json:"pec,omitempty" validate:"omitempty,email"` // PEC email
	Phone string `bson:"phone,omitempty" json:"phone,omitempty"`

	// Bank details (for payment terms in invoices)
	IBAN                string `bson:"iban,omitempty" json:"iban,omitempty"`
	BIC                 string `bson:"bic,omitempty" json:"bic,omitempty"`
	ABI                 string `bson:"abi,omitempty" json:"abi,omitempty"` // Italian bank code
	CAB                 string `bson:"cab,omitempty" json:"cab,omitempty"` // Italian branch code
	Beneficiario        string `bson:"beneficiario,omitempty" json:"beneficiario,omitempty"`               // Beneficiary name
	IstitutoFinanziario string `bson:"istitutoFinanziario,omitempty" json:"istitutoFinanziario,omitempty"` // Bank name

	// Default flag - only one company can be default
	IsDefault bool `bson:"isDefault" json:"isDefault"`

	// Notes
	Notes string `bson:"notes,omitempty" json:"notes,omitempty"`

	// Status
	IsActive bool `bson:"isActive" json:"isActive"`

	// Audit
	CreatedAt time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time  `bson:"updatedAt" json:"updatedAt"`
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"deletedAt,omitempty"`
	CreatedBy string     `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
}

// ToPartyData converts Company to PartyData for invoice embedding
// This creates a snapshot of company data at invoice creation time
func (c *Company) ToPartyData() *PartyData {
	// Build IscrizioneREA only if ALL required fields are present
	// Per Article 2250 Civil Code, companies must provide complete REA data or omit entirely
	var iscrizioneREA *IscrizioneREAInput
	if c.REAOffice != "" && c.REANumber != "" && c.StatoLiquidazione != "" {
		var capitaleSociale float64
		if c.CapitaleSociale != nil {
			capitaleSociale = *c.CapitaleSociale
		}
		iscrizioneREA = &IscrizioneREAInput{
			Ufficio:           c.REAOffice,
			NumeroREA:         c.REANumber,
			CapitaleSociale:   capitaleSociale,
			SocioUnico:        c.SocioUnico,
			StatoLiquidazione: c.StatoLiquidazione,
		}
	}

	// Preserve original CodiceFiscale value - do NOT fallback to P.IVA here
	// The fallback logic is applied at XML generation time where appropriate
	// For ditte individuali, CodiceFiscale (16 chars) differs from P.IVA (11 digits)

	return &PartyData{
		FiscalIDCountry: c.FiscalIDCountry,
		FiscalIDCode:    c.FiscalIDCode,
		CodiceFiscale:   c.CodiceFiscale,
		IsCompany:       true,
		Denomination:    c.Denomination,
		RegimeFiscale:   c.RegimeFiscale,
		Address:         c.Address,
		NumeroCivico:    c.NumeroCivico,
		City:            c.City,
		Province:        c.Province,
		PostalCode:      c.PostalCode,
		Country:         c.Country,
		IscrizioneREA:   iscrizioneREA,
		Email:           c.Email,
		PEC:             c.PEC,
		Phone:           c.Phone,
	}
}

// GetDisplayName returns the display name for the company
func (c *Company) GetDisplayName() string {
	if c.Denomination != "" {
		return c.Denomination
	}
	return c.FiscalIDCode
}
