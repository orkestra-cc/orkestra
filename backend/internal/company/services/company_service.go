package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/company/models"
	"github.com/orkestra/backend/internal/company/repository"
)

// Service errors
var (
	ErrLookupNotFound    = errors.New("company lookup not found")
	ErrInvalidLookupType = errors.New("invalid lookup type")
)

// CompanyService defines the interface for company lookup business logic
type CompanyService interface {
	LookupCompany(ctx context.Context, taxCode string) (*models.CompanyLookup, error)
	EnrichCompany(ctx context.Context, taxCode string, lookupType string) (*models.CompanyLookup, error)
	GetLookup(ctx context.Context, uuid string) (*models.CompanyLookup, error)
	ListLookups(ctx context.Context, page, pageSize int) ([]models.CompanyLookup, int64, error)
	SearchLookups(ctx context.Context, query string, page, pageSize int) ([]models.CompanyLookup, int64, error)
}

type companyService struct {
	repo      repository.CompanyRepository
	apiClient CompanyAPIClient
	logger    *slog.Logger
}

// NewCompanyService creates a new CompanyService
func NewCompanyService(repo repository.CompanyRepository, apiClient CompanyAPIClient, logger *slog.Logger) CompanyService {
	return &companyService{
		repo:      repo,
		apiClient: apiClient,
		logger:    logger,
	}
}

// LookupCompany looks up a company by tax code from the external API,
// persists the result in MongoDB, and returns it
func (s *companyService) LookupCompany(ctx context.Context, taxCode string) (*models.CompanyLookup, error) {
	if taxCode == "" {
		return nil, ErrInvalidTaxCode
	}

	// Call external API (client handles Redis caching internally)
	response, err := s.apiClient.LookupByTaxCode(ctx, taxCode)
	if err != nil {
		if errors.Is(err, ErrCompanyNotFound) {
			return nil, ErrCompanyNotFound
		}
		s.logger.Error("company API lookup failed",
			"taxCode", taxCode,
			"error", err,
		)
		return nil, err
	}

	if len(response.Data) == 0 {
		return nil, ErrCompanyNotFound
	}

	// Pick the most complete entry (prefer one with taxCode and activityStatus)
	best := pickBestEntry(response.Data)

	// Map API response to domain model
	lookup := mapResponseToLookup(&best)

	// Check if we already have this lookup
	existing, err := s.repo.GetByTaxCode(ctx, lookup.TaxCode)
	if err == nil {
		lookup.UUID = existing.UUID
	} else {
		lookup.UUID = uuid.New().String()
	}

	// Upsert into MongoDB
	if err := s.repo.Upsert(ctx, lookup); err != nil {
		s.logger.Error("failed to upsert company lookup",
			"taxCode", taxCode,
			"error", err,
		)
		return nil, err
	}

	s.logger.Info("company lookup completed",
		"taxCode", taxCode,
		"companyName", lookup.CompanyName,
		"activityStatus", lookup.ActivityStatus,
	)

	return lookup, nil
}

// EnrichCompany fetches enrichment data for a company from the external API
// and stores it as a partial update on the existing CompanyLookup document
func (s *companyService) EnrichCompany(ctx context.Context, taxCode string, lookupType string) (*models.CompanyLookup, error) {
	if taxCode == "" {
		return nil, ErrInvalidTaxCode
	}
	if !models.IsEnrichmentType(lookupType) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidLookupType, lookupType)
	}

	// Ensure the base company lookup exists first
	existing, err := s.repo.GetByTaxCode(ctx, taxCode)
	if err != nil {
		if errors.Is(err, repository.ErrLookupNotFound) {
			// Auto-create via start lookup first
			s.logger.Info("auto-creating base lookup before enrichment",
				"taxCode", taxCode,
				"enrichmentType", lookupType,
			)
			existing, err = s.LookupCompany(ctx, taxCode)
			if err != nil {
				return nil, fmt.Errorf("failed to create base lookup: %w", err)
			}
		} else {
			return nil, err
		}
	}

	// Call external API with the enrichment type
	response, err := s.apiClient.LookupByType(ctx, taxCode, lookupType)
	if err != nil {
		if errors.Is(err, ErrCompanyNotFound) {
			return nil, ErrCompanyNotFound
		}
		s.logger.Error("company API enrichment lookup failed",
			"taxCode", taxCode,
			"type", lookupType,
			"error", err,
		)
		return nil, err
	}

	if len(response.Data) == 0 {
		return nil, ErrCompanyNotFound
	}

	best := pickBestEntry(response.Data)
	now := time.Now()

	// For "full" type, populate all four enrichment fields
	if lookupType == models.LookupTypeFull {
		if err := s.applyFullEnrichment(ctx, taxCode, &best, now); err != nil {
			return nil, err
		}
	} else {
		// Single enrichment type
		enrichmentField, data := mapEnrichmentData(lookupType, &best)
		if err := s.repo.UpdateEnrichment(ctx, taxCode, enrichmentField, data, lookupType, now); err != nil {
			s.logger.Error("failed to update enrichment",
				"taxCode", taxCode,
				"type", lookupType,
				"error", err,
			)
			return nil, err
		}
	}

	s.logger.Info("company enrichment completed",
		"taxCode", taxCode,
		"type", lookupType,
		"companyName", existing.CompanyName,
	)

	// Return the updated document
	updated, err := s.repo.GetByTaxCode(ctx, taxCode)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// applyFullEnrichment applies all enrichment types from a full response
func (s *companyService) applyFullEnrichment(ctx context.Context, taxCode string, data *models.OpenAPICompanyData, now time.Time) error {
	enrichments := []string{
		models.LookupTypeAdvanced,
		models.LookupTypeMarketing,
		models.LookupTypeStakeholders,
		models.LookupTypeAML,
	}

	for _, t := range enrichments {
		field, enrichData := mapEnrichmentData(t, data)
		// For the last enrichment, also mark "full" as fetched by passing it as the fetchedType
		if err := s.repo.UpdateEnrichment(ctx, taxCode, field, enrichData, t, now); err != nil {
			return fmt.Errorf("failed to apply %s enrichment: %w", t, err)
		}
	}

	// Mark "full" itself as fetched (re-sets advanced field as a no-op side effect)
	advField, advData := mapEnrichmentData(models.LookupTypeAdvanced, data)
	if err := s.repo.UpdateEnrichment(ctx, taxCode, advField, advData, models.LookupTypeFull, now); err != nil {
		s.logger.Warn("failed to mark full enrichment type",
			"taxCode", taxCode,
			"error", err,
		)
	}

	return nil
}

// mapEnrichmentData maps API response fields to the appropriate enrichment struct
func mapEnrichmentData(lookupType string, data *models.OpenAPICompanyData) (string, interface{}) {
	switch lookupType {
	case models.LookupTypeAdvanced:
		return "advanced", &models.AdvancedData{
			REACode:             data.REACode,
			CCIAA:               data.CCIAA,
			AtecoClassification: data.AtecoClassification,
			DetailedLegalForm:   data.DetailedLegalForm,
			PEC:                 data.PEC,
			StartDate:           data.StartDate,
			EndDate:             data.EndDate,
			TaxCodeCeased:       data.TaxCodeCeased,
		}
	case models.LookupTypeMarketing:
		return "marketing", &models.MarketingData{
			Contacts:     data.Contacts,
			WebAndSocial: data.WebAndSocial,
			Mail:         data.Mail,
			PEC:          data.PEC,
			Employees:    data.Employees,
			Ecofin:       data.Ecofin,
			Branches:     data.Branches,
			AllOffices:   data.AllOffices,
		}
	case models.LookupTypeStakeholders:
		return "stakeholders", &models.StakeholdersData{
			Managers:           data.Managers,
			Shareholders:       data.Shareholders,
			CorporateGroups:    data.CorporateGroups,
			Subsidiaries:       data.Subsidiaries,
			AffiliateCompanies: data.AffiliateCompanies,
		}
	case models.LookupTypeAML:
		return "aml", &models.AMLData{
			Managers:         data.Managers,
			Shareholders:     data.Shareholders,
			CorporateGroups:  data.CorporateGroups,
			ForeignTrade:     data.ForeignTrade,
			PublicTenders:    data.PublicTenders,
			OperatingResults: data.OperatingResults,
			Debts:            data.Debts,
			RAE:              data.RAE,
			SAE:              data.SAE,
		}
	default:
		return "", nil
	}
}

// GetLookup retrieves a previously stored company lookup by UUID
func (s *companyService) GetLookup(ctx context.Context, id string) (*models.CompanyLookup, error) {
	lookup, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrLookupNotFound) {
			return nil, ErrLookupNotFound
		}
		return nil, err
	}
	return lookup, nil
}

// ListLookups returns a paginated list of stored company lookups
func (s *companyService) ListLookups(ctx context.Context, page, pageSize int) ([]models.CompanyLookup, int64, error) {
	return s.repo.List(ctx, page, pageSize)
}

// SearchLookups searches stored company lookups by name, tax code, or VAT code
func (s *companyService) SearchLookups(ctx context.Context, query string, page, pageSize int) ([]models.CompanyLookup, int64, error) {
	return s.repo.Search(ctx, query, page, pageSize)
}

// pickBestEntry selects the most complete entry from the API response array.
// Prefers entries that have a taxCode and activityStatus set.
func pickBestEntry(entries []models.OpenAPICompanyData) models.OpenAPICompanyData {
	best := entries[0]
	for _, e := range entries[1:] {
		// Prefer entry with taxCode + activityStatus (more complete record)
		if e.TaxCode != "" && e.ActivityStatus != "" {
			return e
		}
	}
	return best
}

// mapResponseToLookup converts an API response to a domain model
func mapResponseToLookup(data *models.OpenAPICompanyData) *models.CompanyLookup {
	lookup := &models.CompanyLookup{
		TaxCode:          data.TaxCode,
		CompanyName:      data.CompanyName,
		VATCode:          data.VATCode,
		ActivityStatus:   data.ActivityStatus,
		SDICode:          data.SDICode,
		RegistrationDate: data.RegistrationDate,
		SourceID:         data.ID,
	}

	if data.Address.RegisteredOffice != nil {
		office := data.Address.RegisteredOffice
		region := ""
		if office.Region != nil {
			region = office.Region.Description
		}
		lookup.Address = models.Address{
			Street:       office.StreetName,
			StreetNumber: office.StreetNumber,
			Town:         office.Town,
			Province:     office.Province,
			ZipCode:      office.ZipCode,
			Region:       region,
		}
	}

	return lookup
}
