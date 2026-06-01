package company

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/company/handlers"
)

// RegisterRoutes registers all company lookup module routes
func RegisterRoutes(api huma.API, handler *handlers.CompanyHandler) {
	// ========================================
	// Company Lookup Routes
	// ========================================

	huma.Register(api, huma.Operation{
		OperationID: "search-companies",
		Method:      http.MethodGet,
		Path:        "/v1/company/search",
		Summary:     "Search Italian companies",
		Description: "Search Italian companies by name, province, ATECO code, revenue, employees, and more via the OpenAPI IT-search endpoint. Results are auto-persisted.",
		Tags:        []string{"Company Lookup"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.SearchCompanies)

	huma.Register(api, huma.Operation{
		OperationID: "lookup-company",
		Method:      http.MethodGet,
		Path:        "/v1/company/lookup/{taxCode}",
		Summary:     "Look up company by tax code",
		Description: "Looks up an Italian company by tax code (Codice Fiscale) or VAT number (Partita IVA) using the OpenAPI Company API. Results are cached and persisted.",
		Tags:        []string{"Company Lookup"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.LookupCompany)

	huma.Register(api, huma.Operation{
		OperationID: "list-company-lookups",
		Method:      http.MethodGet,
		Path:        "/v1/company/lookups",
		Summary:     "List company lookups",
		Description: "Lists previously looked-up companies with pagination",
		Tags:        []string{"Company Lookup"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListCompanyLookups)

	huma.Register(api, huma.Operation{
		OperationID: "search-company-lookups",
		Method:      http.MethodGet,
		Path:        "/v1/company/lookups/search",
		Summary:     "Search company lookups",
		Description: "Searches stored company lookups by company name, tax code, or VAT code",
		Tags:        []string{"Company Lookup"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.SearchCompanyLookups)

	huma.Register(api, huma.Operation{
		OperationID: "get-company-lookup",
		Method:      http.MethodGet,
		Path:        "/v1/company/lookups/{id}",
		Summary:     "Get company lookup",
		Description: "Retrieves a specific stored company lookup by its UUID",
		Tags:        []string{"Company Lookup"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetCompanyLookup)

	// ========================================
	// Company Enrichment Routes
	// ========================================

	huma.Register(api, huma.Operation{
		OperationID: "enrich-company-lookup",
		Method:      http.MethodGet,
		Path:        "/v1/company/lookup/{taxCode}/enrich/{type}",
		Summary:     "Enrich company lookup",
		Description: "Fetches additional enrichment data (advanced, marketing, stakeholders, aml, or full) for a company and stores it on the existing lookup document.",
		Tags:        []string{"Company Lookup"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.EnrichCompany)
}
