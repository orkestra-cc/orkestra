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
	ID                  string             `json:"id"`
	TaxCode             string             `json:"taxCode"`
	CompanyName         string             `json:"companyName"`
	VATCode             string             `json:"vatCode"`
	ActivityStatus      string             `json:"activityStatus"`
	SDICode             string             `json:"sdiCode"`
	SDICodeTimestamp    interface{}        `json:"sdiCodeTimestamp"`
	RegistrationDate    string             `json:"registrationDate"`
	Address             OpenAPIAddressData `json:"address"`
	CreationTimestamp   interface{}        `json:"creationTimestamp"`
	LastUpdateTimestamp interface{}        `json:"lastUpdateTimestamp"`

	// Advanced enrichment fields (IT-advanced)
	REACode             string                `json:"reaCode,omitempty"`
	CCIAA               string                `json:"cciaa,omitempty"`
	AtecoClassification *AtecoClassification  `json:"atecoClassification,omitempty"`
	DetailedLegalForm   *LegalFormDetail      `json:"detailedLegalForm,omitempty"`
	PEC                 string                `json:"pec,omitempty"`
	StartDate           string                `json:"startDate,omitempty"`
	EndDate             string                `json:"endDate,omitempty"`
	TaxCodeCeased       *bool                 `json:"taxCodeCeased,omitempty"`
	VATGroup            *VATGroupData         `json:"vatGroup,omitempty"`
	BalanceSheets       *BalanceSheetsData    `json:"balanceSheets,omitempty"`
	ShareHolders        []AdvancedShareholder `json:"shareHolders,omitempty"`

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
	Toponym      string         `json:"toponym"`
	Street       string         `json:"street"`
	StreetNumber string         `json:"streetNumber"`
	StreetName   string         `json:"streetName"`
	Town         string         `json:"town"`
	Hamlet       string         `json:"hamlet"`
	Province     string         `json:"province"`
	ZipCode      string         `json:"zipCode"`
	Region       *OpenAPIRegion `json:"region"`
	TownCode     string         `json:"townCode"`
	GPS          *OpenAPIGPS    `json:"gps"`
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

// OpenAPISearchResponse represents the response from the IT-search endpoint.
// Same structure as OpenAPIBaseResponse but includes totalResults for pagination.
type OpenAPISearchResponse struct {
	Success      bool                 `json:"success"`
	Data         []OpenAPICompanyData `json:"data"`
	Message      string               `json:"message,omitempty"`
	Error        interface{}          `json:"error,omitempty"`
	TotalResults *int                 `json:"totalResults,omitempty"`
}

// UnmarshalJSON handles the "data" field being either an array or a single object.
// The external API returns "count" for total results; we also accept "totalResults"
// for backwards compatibility with cached responses.
func (r *OpenAPISearchResponse) UnmarshalJSON(b []byte) error {
	type Alias struct {
		Success      bool            `json:"success"`
		Data         json.RawMessage `json:"data"`
		Message      string          `json:"message,omitempty"`
		Error        interface{}     `json:"error,omitempty"`
		TotalResults *int            `json:"totalResults,omitempty"`
		Count        *int            `json:"count,omitempty"`
	}
	var raw Alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	r.Success = raw.Success
	r.Message = raw.Message
	r.Error = raw.Error
	// Prefer "count" (external API field), fall back to "totalResults" (cached responses)
	if raw.Count != nil {
		r.TotalResults = raw.Count
	} else {
		r.TotalResults = raw.TotalResults
	}

	if len(raw.Data) == 0 || string(raw.Data) == "null" {
		r.Data = nil
		return nil
	}

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

// CompanySearchParams holds all IT-search query parameters
type CompanySearchParams struct {
	CompanyName         string   `json:"companyName,omitempty"`
	Autocomplete        string   `json:"autocomplete,omitempty"`
	Province            string   `json:"province,omitempty"`
	TownCode            string   `json:"townCode,omitempty"`
	AtecoCode           string   `json:"atecoCode,omitempty"`
	CCIAA               string   `json:"cciaa,omitempty"`
	REACode             string   `json:"reaCode,omitempty"`
	MinTurnover         *int64   `json:"minTurnover,omitempty"`
	MaxTurnover         *int64   `json:"maxTurnover,omitempty"`
	MinEmployees        *int     `json:"minEmployees,omitempty"`
	MaxEmployees        *int     `json:"maxEmployees,omitempty"`
	SDICode             string   `json:"sdiCode,omitempty"`
	LegalFormCode       string   `json:"legalFormCode,omitempty"`
	PEC                 string   `json:"pec,omitempty"`
	ShareHolderTaxCode  string   `json:"shareHolderTaxCode,omitempty"`
	Latitude            *float64 `json:"lat,omitempty"`
	Longitude           *float64 `json:"long,omitempty"`
	Radius              *int     `json:"radius,omitempty"`
	ActivityStatus      string   `json:"activityStatus,omitempty"`
	DataEnrichment      string   `json:"dataEnrichment,omitempty"`
	CreationTimestamp   *int64   `json:"creationTimestamp,omitempty"`
	LastUpdateTimestamp *int64   `json:"lastUpdateTimestamp,omitempty"`
	DryRun              *int     `json:"dryRun,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	Skip                int      `json:"skip,omitempty"`
}

// CompanySearchResult represents the search response
type CompanySearchResult struct {
	Companies    []CompanyLookup `json:"companies"`
	TotalResults *int            `json:"totalResults,omitempty"`
	Limit        int             `json:"limit"`
	Skip         int             `json:"skip"`
	DryRun       bool            `json:"dryRun"`
}
