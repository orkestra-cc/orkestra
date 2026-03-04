package models

import "encoding/json"

// OpenAPIBaseResponse represents the raw response from the OpenAPI Company API.
// GET /IT-start/{vatCode_taxCode_or_id} (and all enrichment endpoints).
// The "data" field can be either an array or a single object depending on the
// endpoint (e.g. IT-start returns an array, IT-marketing returns an object).
type OpenAPIBaseResponse struct {
	Success bool                 `json:"success"`
	Data    []OpenAPICompanyData `json:"data"`
	Message string               `json:"message,omitempty"`
	Error   interface{}          `json:"error,omitempty"`
}

// UnmarshalJSON handles the "data" field being either an array or a single object.
func (r *OpenAPIBaseResponse) UnmarshalJSON(b []byte) error {
	// Use an alias to avoid infinite recursion.
	type Alias struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message,omitempty"`
		Error   interface{}     `json:"error,omitempty"`
	}
	var raw Alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	r.Success = raw.Success
	r.Message = raw.Message
	r.Error = raw.Error

	if len(raw.Data) == 0 || string(raw.Data) == "null" {
		r.Data = nil
		return nil
	}

	// Try array first (most common), then single object.
	if raw.Data[0] == '[' {
		return json.Unmarshal(raw.Data, &r.Data)
	}
	var single OpenAPICompanyData
	if err := json.Unmarshal(raw.Data, &single); err != nil {
		return err
	}
	r.Data = []OpenAPICompanyData{single}
	return nil
}

// OpenAPICompanyData represents a single company entry from the API response.
// The API may return multiple entries (e.g. one by VAT code, one by tax code).
// Enrichment endpoints add extra fields; base fields are always present.
type OpenAPICompanyData struct {
	// Base fields (IT-start)
	ID                 string             `json:"id"`
	TaxCode            string             `json:"taxCode"`
	CompanyName        string             `json:"companyName"`
	VATCode            string             `json:"vatCode"`
	ActivityStatus     string             `json:"activityStatus"`
	SDICode            string             `json:"sdiCode"`
	SDICodeTimestamp    interface{}        `json:"sdiCodeTimestamp"`
	RegistrationDate   string             `json:"registrationDate"`
	Address            OpenAPIAddressData `json:"address"`
	CreationTimestamp   interface{}        `json:"creationTimestamp"`
	LastUpdateTimestamp interface{}        `json:"lastUpdateTimestamp"`

	// Advanced enrichment fields (IT-advanced)
	REACode             string               `json:"reaCode,omitempty"`
	CCIAA               string               `json:"cciaa,omitempty"`
	AtecoClassification *AtecoClassification `json:"atecoClassification,omitempty"`
	DetailedLegalForm   *LegalFormDetail     `json:"detailedLegalForm,omitempty"`
	PEC                 string               `json:"pec,omitempty"`
	StartDate           string               `json:"startDate,omitempty"`
	EndDate             string               `json:"endDate,omitempty"`
	TaxCodeCeased       *bool                `json:"taxCodeCeased,omitempty"`

	// Marketing enrichment fields (IT-marketing)
	Contacts     *ContactsData     `json:"contacts,omitempty"`
	WebAndSocial *WebAndSocialData `json:"webAndSocial,omitempty"`
	Mail         *MailData         `json:"mail,omitempty"`
	Employees    *EmployeesData    `json:"employees,omitempty"`
	Ecofin       *EcofinData       `json:"ecofin,omitempty"`
	Branches     *BranchesData     `json:"branches,omitempty"`
	AllOffices   []OfficeEntry     `json:"allOffices,omitempty"`

	// Stakeholders enrichment fields (IT-stakeholders)
	Managers           []Manager            `json:"managers,omitempty"`
	Shareholders       []Shareholder        `json:"shareholders,omitempty"`
	CorporateGroups    *CorporateGroupsData `json:"corporateGroups,omitempty"`
	Subsidiaries       []SubsidiaryCompany  `json:"subsidiaries,omitempty"`
	AffiliateCompanies []AffiliateCompany   `json:"affiliateCompanies,omitempty"`

	// AML enrichment fields (IT-aml)
	ForeignTrade     *ForeignTradeData     `json:"foreignTrade,omitempty"`
	PublicTenders    []PublicTender        `json:"publicTenders,omitempty"`
	OperatingResults *OperatingResultsData `json:"operatingResults,omitempty"`
	Debts            []DebtsData           `json:"debts,omitempty"`
	RAE              *CodeDescription      `json:"rae,omitempty"`
	SAE              *CodeDescription      `json:"sae,omitempty"`
}

// OpenAPIAddressData represents the address data from the API response
type OpenAPIAddressData struct {
	RegisteredOffice *OpenAPIRegisteredOffice `json:"registeredOffice"`
}

// OpenAPIRegisteredOffice represents the registered office address
type OpenAPIRegisteredOffice struct {
	Toponym      string          `json:"toponym"`
	Street       string          `json:"street"`
	StreetNumber string          `json:"streetNumber"`
	StreetName   string          `json:"streetName"`
	Town         string          `json:"town"`
	Hamlet       string          `json:"hamlet"`
	Province     string          `json:"province"`
	ZipCode      string          `json:"zipCode"`
	Region       *OpenAPIRegion  `json:"region"`
	TownCode     string          `json:"townCode"`
	GPS          *OpenAPIGPS     `json:"gps"`
}

// OpenAPIRegion represents the region in the API response
type OpenAPIRegion struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

// OpenAPIGPS represents GPS coordinates (nullable in API response)
type OpenAPIGPS struct {
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

// CompanyLookupListResponse represents a paginated list of company lookups
type CompanyLookupListResponse struct {
	Lookups    []CompanyLookup `json:"lookups" doc:"List of company lookups"`
	Total      int64           `json:"total" doc:"Total number of lookups"`
	Page       int             `json:"page" doc:"Current page"`
	PageSize   int             `json:"pageSize" doc:"Items per page"`
	TotalPages int             `json:"totalPages" doc:"Total number of pages"`
}
