package handlers

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/company/models"
	"github.com/orkestra/backend/internal/addons/company/services"
)

// CompanyHandler handles company lookup HTTP requests
type CompanyHandler struct {
	service services.CompanyService
}

// NewCompanyHandler creates a new CompanyHandler
func NewCompanyHandler(service services.CompanyService) *CompanyHandler {
	return &CompanyHandler{
		service: service,
	}
}

// ========================================
// Request/Response Types
// ========================================

// LookupCompanyRequest represents the request to look up a company
type LookupCompanyRequest struct {
	TaxCode string `path:"taxCode" doc:"Italian tax code (Codice Fiscale) or VAT number (Partita IVA)" minLength:"11" maxLength:"16"`
}

// LookupCompanyResponse represents the response with company lookup data
type LookupCompanyResponse struct {
	Body models.CompanyLookup `json:"lookup" doc:"Company lookup result"`
}

// GetCompanyLookupRequest represents the request to get a stored lookup
type GetCompanyLookupRequest struct {
	ID string `path:"id" doc:"Company lookup UUID"`
}

// GetCompanyLookupResponse represents the response with a stored lookup
type GetCompanyLookupResponse struct {
	Body models.CompanyLookup `json:"lookup" doc:"Company lookup details"`
}

// ListCompanyLookupsRequest represents the request to list stored lookups
type ListCompanyLookupsRequest struct {
	Page     int `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// ListCompanyLookupsResponse represents the paginated list of lookups
type ListCompanyLookupsResponse struct {
	Body models.CompanyLookupListResponse `json:"result" doc:"Paginated list of company lookups"`
}

// SearchCompanyLookupsRequest represents the request to search stored lookups
type SearchCompanyLookupsRequest struct {
	Query    string `query:"q" doc:"Search query (company name, tax code, or VAT code)"`
	Page     int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// SearchCompanyLookupsResponse represents the search results
type SearchCompanyLookupsResponse struct {
	Body models.CompanyLookupListResponse `json:"result" doc:"Search results"`
}

// SearchCompaniesRequest represents the request to search Italian companies via IT-search
// Note: Huma v2 does not support pointer types for query params, so we use zero-value
// semantics and convert to pointers in the handler when non-zero.
type SearchCompaniesRequest struct {
	CompanyName         string  `query:"companyName" doc:"Company name or partial match"`
	Autocomplete        string  `query:"autocomplete" doc:"Autocomplete prefix search"`
	Province            string  `query:"province" doc:"Italian province code"`
	TownCode            string  `query:"townCode" doc:"Cadastral/Belfiore town code"`
	AtecoCode           string  `query:"atecoCode" doc:"ATECO industry code"`
	CCIAA               string  `query:"cciaa" doc:"Chamber of Commerce"`
	REACode             string  `query:"reaCode" doc:"REA registry code"`
	MinTurnover         int64   `query:"minTurnover" doc:"Minimum annual turnover"`
	MaxTurnover         int64   `query:"maxTurnover" doc:"Maximum annual turnover"`
	MinEmployees        int     `query:"minEmployees" doc:"Minimum employees"`
	MaxEmployees        int     `query:"maxEmployees" doc:"Maximum employees"`
	SDICode             string  `query:"sdiCode" doc:"SDI electronic invoicing code"`
	LegalFormCode       string  `query:"legalFormCode" doc:"Legal form code"`
	PEC                 string  `query:"pec" doc:"Certified email (PEC)"`
	ShareHolderTaxCode  string  `query:"shareHolderTaxCode" doc:"Shareholder tax code"`
	Latitude            float64 `query:"lat" doc:"GPS latitude"`
	Longitude           float64 `query:"long" doc:"GPS longitude"`
	Radius              int     `query:"radius" doc:"Search radius in meters"`
	ActivityStatus      string  `query:"activityStatus" doc:"Business status filter" enum:"ATTIVA,CESSATA,REGISTRATA,INATTIVA,SOSPESA,IN_ISCRIZIONE"`
	DataEnrichment      string  `query:"dataEnrichment" doc:"Response detail level (omit for basic results)" enum:"name,start,advanced,pec,address,shareholders"`
	CreationTimestamp   int64   `query:"creationTimestamp" doc:"Companies created after this Unix timestamp"`
	LastUpdateTimestamp int64   `query:"lastUpdateTimestamp" doc:"Companies updated after this Unix timestamp"`
	DryRun              int     `query:"dryRun" doc:"Set to 1 to count results without consuming quota" minimum:"0" maximum:"1"`
	Limit               int     `query:"limit" default:"100" minimum:"1" maximum:"1000" doc:"Max results to return"`
	Skip                int     `query:"skip" default:"0" minimum:"0" doc:"Number of results to skip"`
}

// SearchCompaniesResponse represents the response with search results
type SearchCompaniesResponse struct {
	Body models.CompanySearchResult `json:"result" doc:"Company search results"`
}

// EnrichCompanyRequest represents the request to enrich a company lookup
type EnrichCompanyRequest struct {
	TaxCode    string `path:"taxCode" doc:"Italian tax code (Codice Fiscale) or VAT number (Partita IVA)" minLength:"11" maxLength:"16"`
	LookupType string `path:"type" doc:"Enrichment type" enum:"advanced,marketing,stakeholders,aml,full"`
}

// EnrichCompanyResponse represents the response with enriched company data
type EnrichCompanyResponse struct {
	Body models.CompanyLookup `json:"lookup" doc:"Enriched company lookup result"`
}

// ========================================
// Handler Methods
// ========================================

// LookupCompany looks up a company by tax code via the external API
func (h *CompanyHandler) LookupCompany(ctx context.Context, req *LookupCompanyRequest) (*LookupCompanyResponse, error) {
	lookup, err := h.service.LookupCompany(ctx, req.TaxCode)
	if err != nil {
		if errors.Is(err, services.ErrCompanyNotFound) {
			return nil, huma.Error404NotFound("Company not found for the given tax code", err)
		}
		if errors.Is(err, services.ErrInvalidTaxCode) {
			return nil, huma.Error400BadRequest("Invalid tax code", err)
		}
		if errors.Is(err, services.ErrCircuitBreakerOpen) {
			return nil, huma.Error503ServiceUnavailable("Company lookup service temporarily unavailable", err)
		}
		if errors.Is(err, services.ErrUpstreamAuth) {
			return nil, huma.Error502BadGateway("Company API authentication failed — rotate OPENAPI_COMPANY_BEARER_TOKEN", err)
		}
		return nil, huma.Error500InternalServerError("Failed to look up company", err)
	}

	return &LookupCompanyResponse{Body: *lookup}, nil
}

// GetCompanyLookup retrieves a previously stored company lookup by ID
func (h *CompanyHandler) GetCompanyLookup(ctx context.Context, req *GetCompanyLookupRequest) (*GetCompanyLookupResponse, error) {
	lookup, err := h.service.GetLookup(ctx, req.ID)
	if err != nil {
		if errors.Is(err, services.ErrLookupNotFound) {
			return nil, huma.Error404NotFound("Company lookup not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get company lookup", err)
	}

	return &GetCompanyLookupResponse{Body: *lookup}, nil
}

// ListCompanyLookups lists previously looked-up companies with pagination
func (h *CompanyHandler) ListCompanyLookups(ctx context.Context, req *ListCompanyLookupsRequest) (*ListCompanyLookupsResponse, error) {
	lookups, total, err := h.service.ListLookups(ctx, req.Page, req.PageSize)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list company lookups", err)
	}

	if lookups == nil {
		lookups = []models.CompanyLookup{}
	}

	totalPages := int(total) / req.PageSize
	if int(total)%req.PageSize > 0 {
		totalPages++
	}

	return &ListCompanyLookupsResponse{
		Body: models.CompanyLookupListResponse{
			Lookups:    lookups,
			Total:      total,
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalPages: totalPages,
		},
	}, nil
}

// SearchCompanyLookups searches stored lookups by name, tax code, or VAT code
func (h *CompanyHandler) SearchCompanyLookups(ctx context.Context, req *SearchCompanyLookupsRequest) (*SearchCompanyLookupsResponse, error) {
	if req.Query == "" {
		return nil, huma.Error400BadRequest("Search query is required", nil)
	}

	lookups, total, err := h.service.SearchLookups(ctx, req.Query, req.Page, req.PageSize)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to search company lookups", err)
	}

	if lookups == nil {
		lookups = []models.CompanyLookup{}
	}

	totalPages := int(total) / req.PageSize
	if int(total)%req.PageSize > 0 {
		totalPages++
	}

	return &SearchCompanyLookupsResponse{
		Body: models.CompanyLookupListResponse{
			Lookups:    lookups,
			Total:      total,
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalPages: totalPages,
		},
	}, nil
}

// ptrIfNonZero returns a pointer to v if it's non-zero, otherwise nil.
func ptrIfNonZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// SearchCompanies searches Italian companies via the external IT-search API
func (h *CompanyHandler) SearchCompanies(ctx context.Context, req *SearchCompaniesRequest) (*SearchCompaniesResponse, error) {
	params := &models.CompanySearchParams{
		CompanyName:         req.CompanyName,
		Autocomplete:        req.Autocomplete,
		Province:            req.Province,
		TownCode:            req.TownCode,
		AtecoCode:           req.AtecoCode,
		CCIAA:               req.CCIAA,
		REACode:             req.REACode,
		MinTurnover:         ptrIfNonZero(req.MinTurnover),
		MaxTurnover:         ptrIfNonZero(req.MaxTurnover),
		MinEmployees:        ptrIfNonZero(req.MinEmployees),
		MaxEmployees:        ptrIfNonZero(req.MaxEmployees),
		SDICode:             req.SDICode,
		LegalFormCode:       req.LegalFormCode,
		PEC:                 req.PEC,
		ShareHolderTaxCode:  req.ShareHolderTaxCode,
		Latitude:            ptrIfNonZero(req.Latitude),
		Longitude:           ptrIfNonZero(req.Longitude),
		Radius:              ptrIfNonZero(req.Radius),
		ActivityStatus:      req.ActivityStatus,
		DataEnrichment:      req.DataEnrichment,
		CreationTimestamp:   ptrIfNonZero(req.CreationTimestamp),
		LastUpdateTimestamp: ptrIfNonZero(req.LastUpdateTimestamp),
		DryRun:              ptrIfNonZero(req.DryRun),
		Limit:               req.Limit,
		Skip:                req.Skip,
	}

	result, err := h.service.SearchCompanies(ctx, params)
	if err != nil {
		if errors.Is(err, services.ErrCircuitBreakerOpen) {
			return nil, huma.Error503ServiceUnavailable("Company search service temporarily unavailable", err)
		}
		if errors.Is(err, services.ErrUpstreamAuth) {
			return nil, huma.Error502BadGateway("Company API authentication failed — rotate OPENAPI_COMPANY_BEARER_TOKEN", err)
		}
		if errors.Is(err, services.ErrAPIRequestFailed) {
			return nil, huma.Error502BadGateway("Company search API request failed", err)
		}
		return nil, huma.Error500InternalServerError("Failed to search companies", err)
	}

	return &SearchCompaniesResponse{Body: *result}, nil
}

// EnrichCompany enriches a company lookup with additional data from a paid endpoint
func (h *CompanyHandler) EnrichCompany(ctx context.Context, req *EnrichCompanyRequest) (*EnrichCompanyResponse, error) {
	lookup, err := h.service.EnrichCompany(ctx, req.TaxCode, req.LookupType)
	if err != nil {
		if errors.Is(err, services.ErrCompanyNotFound) {
			return nil, huma.Error404NotFound("Company not found for the given tax code", err)
		}
		if errors.Is(err, services.ErrInvalidTaxCode) {
			return nil, huma.Error400BadRequest("Invalid tax code", err)
		}
		if errors.Is(err, services.ErrInvalidLookupType) {
			return nil, huma.Error400BadRequest("Invalid enrichment type", err)
		}
		if errors.Is(err, services.ErrCircuitBreakerOpen) {
			return nil, huma.Error503ServiceUnavailable("Company lookup service temporarily unavailable", err)
		}
		if errors.Is(err, services.ErrUpstreamAuth) {
			return nil, huma.Error502BadGateway("Company API authentication failed — rotate OPENAPI_COMPANY_BEARER_TOKEN", err)
		}
		return nil, huma.Error500InternalServerError("Failed to enrich company", err)
	}

	return &EnrichCompanyResponse{Body: *lookup}, nil
}
