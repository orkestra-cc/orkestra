package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Customer represents a billing customer (cliente per fatture attive)
type Customer struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	// Unique identifier
	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Fiscal identifiers
	FiscalIDCountry string `bson:"fiscalIdCountry" json:"fiscalIdCountry" validate:"required,len=2"` // IT
	FiscalIDCode    string `bson:"fiscalIdCode" json:"fiscalIdCode" validate:"required"`             // P.IVA (11 chars) o CF (16 chars)
	CodiceFiscale   string `bson:"codiceFiscale,omitempty" json:"codiceFiscale,omitempty"`           // Codice Fiscale se diverso

	// Company/Person data
	IsCompany    bool   `bson:"isCompany" json:"isCompany"`
	Denomination string `bson:"denomination,omitempty" json:"denomination,omitempty"` // Ragione sociale (aziende)
	Name         string `bson:"name,omitempty" json:"name,omitempty"`                 // Nome (persone fisiche)
	Surname      string `bson:"surname,omitempty" json:"surname,omitempty"`           // Cognome (persone fisiche)

	// Address
	Address      string `bson:"address" json:"address" validate:"required"`
	NumeroCivico string `bson:"numeroCivico,omitempty" json:"numeroCivico,omitempty"` // Street number
	City         string `bson:"city" json:"city" validate:"required"`
	Province     string `bson:"province,omitempty" json:"province,omitempty"` // Sigla provincia (2 chars)
	PostalCode   string `bson:"postalCode" json:"postalCode" validate:"required"`
	Country      string `bson:"country" json:"country" validate:"required,len=2"` // ISO 3166-1 alpha-2

	// Contacts
	Email string `bson:"email,omitempty" json:"email,omitempty" validate:"omitempty,email"`
	PEC   string `bson:"pec,omitempty" json:"pec,omitempty" validate:"omitempty,email"`
	Phone string `bson:"phone,omitempty" json:"phone,omitempty"`

	// SDI delivery
	CodiceDestinatario string `bson:"codiceDestinatario,omitempty" json:"codiceDestinatario,omitempty"` // 7 chars for B2B, 6 chars for PA
	PECDestinatario    string `bson:"pecDestinatario,omitempty" json:"pecDestinatario,omitempty"`       // PEC for delivery (alternative to codice)

	// PA specific
	IsPA              bool   `bson:"isPA" json:"isPA"`                                           // Is Public Administration
	CodiceUfficio     string `bson:"codiceUfficio,omitempty" json:"codiceUfficio,omitempty"`     // Codice Univoco Ufficio (6 chars for PA)
	RiferimentoAmm    string `bson:"riferimentoAmm,omitempty" json:"riferimentoAmm,omitempty"`   // Riferimento Amministrazione
	ConvenzioneNumero string `bson:"convenzioneNumero,omitempty" json:"convenzioneNumero,omitempty"` // Numero convenzione

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

// Supplier represents a billing supplier (fornitore per fatture passive)
type Supplier struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	// Unique identifier
	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Fiscal identifiers
	FiscalIDCountry string `bson:"fiscalIdCountry" json:"fiscalIdCountry" validate:"required,len=2"` // IT
	FiscalIDCode    string `bson:"fiscalIdCode" json:"fiscalIdCode" validate:"required"`             // P.IVA
	CodiceFiscale   string `bson:"codiceFiscale,omitempty" json:"codiceFiscale,omitempty"`

	// Company/Person data
	IsCompany    bool   `bson:"isCompany" json:"isCompany"`
	Denomination string `bson:"denomination,omitempty" json:"denomination,omitempty"` // Ragione sociale
	Name         string `bson:"name,omitempty" json:"name,omitempty"`
	Surname      string `bson:"surname,omitempty" json:"surname,omitempty"`

	// Fiscal regime
	RegimeFiscale RegimeFiscale `bson:"regimeFiscale,omitempty" json:"regimeFiscale,omitempty"` // RF01-RF19

	// Address
	Address      string `bson:"address" json:"address" validate:"required"`
	NumeroCivico string `bson:"numeroCivico,omitempty" json:"numeroCivico,omitempty"` // Street number
	City         string `bson:"city" json:"city" validate:"required"`
	Province     string `bson:"province,omitempty" json:"province,omitempty"`
	PostalCode   string `bson:"postalCode" json:"postalCode" validate:"required"`
	Country      string `bson:"country" json:"country" validate:"required,len=2"`

	// Contacts
	Email string `bson:"email,omitempty" json:"email,omitempty" validate:"omitempty,email"`
	PEC   string `bson:"pec,omitempty" json:"pec,omitempty" validate:"omitempty,email"`
	Phone string `bson:"phone,omitempty" json:"phone,omitempty"`

	// Bank details (for payments)
	IBAN string `bson:"iban,omitempty" json:"iban,omitempty"`
	BIC  string `bson:"bic,omitempty" json:"bic,omitempty"`

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

// PartyData represents embedded party data in an invoice (snapshot at invoice creation time)
type PartyData struct {
	// Fiscal identifiers
	FiscalIDCountry string `bson:"fiscalIdCountry" json:"fiscalIdCountry"`
	FiscalIDCode    string `bson:"fiscalIdCode" json:"fiscalIdCode"`
	CodiceFiscale   string `bson:"codiceFiscale,omitempty" json:"codiceFiscale,omitempty"`

	// Company/Person data
	IsCompany    bool   `bson:"isCompany" json:"isCompany"`
	Denomination string `bson:"denomination,omitempty" json:"denomination,omitempty"`
	Name         string `bson:"name,omitempty" json:"name,omitempty"`
	Surname      string `bson:"surname,omitempty" json:"surname,omitempty"`

	// Fiscal regime (only for cedente/prestatore)
	RegimeFiscale RegimeFiscale `bson:"regimeFiscale,omitempty" json:"regimeFiscale,omitempty"`

	// Address
	Address      string `bson:"address" json:"address"`
	NumeroCivico string `bson:"numeroCivico,omitempty" json:"numeroCivico,omitempty"` // Street number (separate per XSD)
	City         string `bson:"city" json:"city"`
	Province     string `bson:"province,omitempty" json:"province,omitempty"`
	PostalCode   string `bson:"postalCode" json:"postalCode"`
	Country      string `bson:"country" json:"country"`

	// REA registration (for Italian companies)
	IscrizioneREA *IscrizioneREAInput `bson:"iscrizioneREA,omitempty" json:"iscrizioneREA,omitempty"`

	// Contacts
	Email string `bson:"email,omitempty" json:"email,omitempty"`
	PEC   string `bson:"pec,omitempty" json:"pec,omitempty"`
	Phone string `bson:"phone,omitempty" json:"phone,omitempty"`

	// SDI delivery (only for cessionario/committente)
	CodiceDestinatario string `bson:"codiceDestinatario,omitempty" json:"codiceDestinatario,omitempty"`
	PECDestinatario    string `bson:"pecDestinatario,omitempty" json:"pecDestinatario,omitempty"`
}

// IscrizioneREAInput represents REA (Registro Imprese) registration data
// Required for Italian companies in the seller/provider section
type IscrizioneREAInput struct {
	Ufficio           string  `bson:"ufficio" json:"ufficio" validate:"required,len=2"`      // Province code (2 chars)
	NumeroREA         string  `bson:"numeroREA" json:"numeroREA" validate:"required"`        // REA registration number
	CapitaleSociale   float64 `bson:"capitaleSociale,omitempty" json:"capitaleSociale,omitempty"` // Share capital
	SocioUnico        string  `bson:"socioUnico,omitempty" json:"socioUnico,omitempty"`      // SU=single shareholder, SM=multiple
	StatoLiquidazione string  `bson:"statoLiquidazione" json:"statoLiquidazione" validate:"required,oneof=LS LN"` // LS=in liquidation, LN=not in liquidation
}

// GetDisplayName returns the display name for a party
func (p *PartyData) GetDisplayName() string {
	if p.IsCompany && p.Denomination != "" {
		return p.Denomination
	}
	if p.Name != "" && p.Surname != "" {
		return p.Name + " " + p.Surname
	}
	if p.Name != "" {
		return p.Name
	}
	return p.FiscalIDCode
}

// CustomerFromPartyData creates a Customer from PartyData
func CustomerFromPartyData(pd *PartyData, uuid string) *Customer {
	return &Customer{
		UUID:               uuid,
		FiscalIDCountry:    pd.FiscalIDCountry,
		FiscalIDCode:       pd.FiscalIDCode,
		CodiceFiscale:      pd.CodiceFiscale,
		IsCompany:          pd.IsCompany,
		Denomination:       pd.Denomination,
		Name:               pd.Name,
		Surname:            pd.Surname,
		Address:            pd.Address,
		NumeroCivico:       pd.NumeroCivico,
		City:               pd.City,
		Province:           pd.Province,
		PostalCode:         pd.PostalCode,
		Country:            pd.Country,
		Email:              pd.Email,
		PEC:                pd.PEC,
		CodiceDestinatario: pd.CodiceDestinatario,
		PECDestinatario:    pd.PECDestinatario,
		IsActive:           true,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
}

// ToPartyData converts a Customer to PartyData
func (c *Customer) ToPartyData() *PartyData {
	return &PartyData{
		FiscalIDCountry:    c.FiscalIDCountry,
		FiscalIDCode:       c.FiscalIDCode,
		CodiceFiscale:      c.CodiceFiscale,
		IsCompany:          c.IsCompany,
		Denomination:       c.Denomination,
		Name:               c.Name,
		Surname:            c.Surname,
		Address:            c.Address,
		NumeroCivico:       c.NumeroCivico,
		City:               c.City,
		Province:           c.Province,
		PostalCode:         c.PostalCode,
		Country:            c.Country,
		Email:              c.Email,
		PEC:                c.PEC,
		CodiceDestinatario: c.CodiceDestinatario,
		PECDestinatario:    c.PECDestinatario,
	}
}

// ToPartyData converts a Supplier to PartyData
func (s *Supplier) ToPartyData() *PartyData {
	return &PartyData{
		FiscalIDCountry: s.FiscalIDCountry,
		FiscalIDCode:    s.FiscalIDCode,
		CodiceFiscale:   s.CodiceFiscale,
		IsCompany:       s.IsCompany,
		Denomination:    s.Denomination,
		Name:            s.Name,
		Surname:         s.Surname,
		RegimeFiscale:   s.RegimeFiscale,
		Address:         s.Address,
		NumeroCivico:    s.NumeroCivico,
		City:            s.City,
		Province:        s.Province,
		PostalCode:      s.PostalCode,
		Country:         s.Country,
		Email:           s.Email,
		PEC:             s.PEC,
	}
}
