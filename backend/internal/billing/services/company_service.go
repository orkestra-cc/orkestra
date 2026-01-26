package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/billing/models"
	"github.com/orkestra/backend/internal/billing/repository"
)

// Company-specific errors
var (
	ErrCompanyNotFound  = errors.New("company not found")
	ErrNoDefaultCompany = errors.New("no default company configured")
)

// CompanyService defines the interface for company business logic
type CompanyService interface {
	CreateCompany(ctx context.Context, input *models.CreateCompanyInput, createdBy string) (*models.Company, error)
	GetCompany(ctx context.Context, uuid string) (*models.Company, error)
	GetCompanyByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Company, error)
	GetDefaultCompany(ctx context.Context) (*models.Company, error)
	ListCompanies(ctx context.Context, search string, pagination models.PaginationParams) (*models.CompanyListResponse, error)
	UpdateCompany(ctx context.Context, uuid string, input *models.UpdateCompanyInput) (*models.Company, error)
	DeleteCompany(ctx context.Context, uuid string) error
	SetDefaultCompany(ctx context.Context, uuid string) (*models.Company, error)
}

type companyService struct {
	companyRepo repository.CompanyRepository
	logger      *slog.Logger
}

// NewCompanyService creates a new CompanyService
func NewCompanyService(companyRepo repository.CompanyRepository, logger *slog.Logger) CompanyService {
	return &companyService{
		companyRepo: companyRepo,
		logger:      logger,
	}
}

func (s *companyService) CreateCompany(ctx context.Context, input *models.CreateCompanyInput, createdBy string) (*models.Company, error) {
	// Validate input
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}

	// Check if company with same fiscal ID already exists
	exists, err := s.companyRepo.ExistsByFiscalID(ctx, input.FiscalIDCode)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, repository.ErrCompanyAlreadyExists
	}

	// If this is the first company or marked as default, handle default setting
	count, err := s.companyRepo.Count(ctx)
	if err != nil {
		return nil, err
	}
	isDefault := input.IsDefault || count == 0 // First company is automatically default

	company := &models.Company{
		UUID:                uuid.New().String(),
		FiscalIDCountry:     input.FiscalIDCountry,
		FiscalIDCode:        input.FiscalIDCode,
		CodiceFiscale:       input.CodiceFiscale,
		Denomination:        input.Denomination,
		RegimeFiscale:       input.RegimeFiscale,
		Address:             input.Address,
		NumeroCivico:        input.NumeroCivico,
		City:                input.City,
		Province:            input.Province,
		PostalCode:          input.PostalCode,
		Country:             input.Country,
		// REA registration (flat fields)
		REAOffice:         input.REAOffice,
		REANumber:         input.REANumber,
		CapitaleSociale:   input.CapitaleSociale,
		SocioUnico:        input.SocioUnico,
		StatoLiquidazione: input.StatoLiquidazione,
		// Contacts
		Email: input.Email,
		PEC:   input.PEC,
		Phone: NormalizePhone(input.Phone),
		// Bank details
		IBAN:                input.IBAN,
		BIC:                 input.BIC,
		ABI:                 input.ABI,
		CAB:                 input.CAB,
		Beneficiario:        input.Beneficiario,
		IstitutoFinanziario: input.IstitutoFinanziario,
		IsDefault:           isDefault,
		Notes:               input.Notes,
		IsActive:            true,
		CreatedBy:           createdBy,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	// If this company should be the default, clear other defaults first
	if isDefault && count > 0 {
		if err := s.companyRepo.ClearDefault(ctx); err != nil {
			return nil, err
		}
	}

	if err := s.companyRepo.Create(ctx, company); err != nil {
		return nil, err
	}

	s.logger.Info("company created",
		"companyUUID", company.UUID,
		"fiscalID", company.FiscalIDCode,
		"denomination", company.Denomination,
		"isDefault", company.IsDefault,
		"createdBy", createdBy,
	)

	return company, nil
}

func (s *companyService) GetCompany(ctx context.Context, uuid string) (*models.Company, error) {
	company, err := s.companyRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrCompanyNotFound) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}
	return company, nil
}

func (s *companyService) GetCompanyByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Company, error) {
	company, err := s.companyRepo.GetByFiscalID(ctx, fiscalIDCode)
	if err != nil {
		if errors.Is(err, repository.ErrCompanyNotFound) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}
	return company, nil
}

func (s *companyService) GetDefaultCompany(ctx context.Context) (*models.Company, error) {
	company, err := s.companyRepo.GetDefault(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNoDefaultCompany) {
			return nil, ErrNoDefaultCompany
		}
		return nil, err
	}
	return company, nil
}

func (s *companyService) ListCompanies(ctx context.Context, search string, pagination models.PaginationParams) (*models.CompanyListResponse, error) {
	companies, total, err := s.companyRepo.List(ctx, search, pagination)
	if err != nil {
		return nil, err
	}

	// Ensure companies is never nil (serializes as [] instead of null)
	if companies == nil {
		companies = []models.Company{}
	}

	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	return &models.CompanyListResponse{
		Companies:  companies,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *companyService) UpdateCompany(ctx context.Context, uuid string, input *models.UpdateCompanyInput) (*models.Company, error) {
	company, err := s.companyRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrCompanyNotFound) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}

	// Apply updates
	if input.Denomination != nil {
		company.Denomination = *input.Denomination
	}
	if input.CodiceFiscale != nil {
		company.CodiceFiscale = *input.CodiceFiscale
	}
	if input.RegimeFiscale != nil {
		company.RegimeFiscale = *input.RegimeFiscale
	}
	if input.Address != nil {
		company.Address = *input.Address
	}
	if input.NumeroCivico != nil {
		company.NumeroCivico = *input.NumeroCivico
	}
	if input.City != nil {
		company.City = *input.City
	}
	if input.Province != nil {
		company.Province = *input.Province
	}
	if input.PostalCode != nil {
		company.PostalCode = *input.PostalCode
	}
	if input.Country != nil {
		company.Country = *input.Country
	}
	// REA registration (flat fields)
	if input.REAOffice != nil {
		company.REAOffice = *input.REAOffice
	}
	if input.REANumber != nil {
		company.REANumber = *input.REANumber
	}
	if input.CapitaleSociale != nil {
		company.CapitaleSociale = input.CapitaleSociale
	}
	if input.SocioUnico != nil {
		company.SocioUnico = *input.SocioUnico
	}
	if input.StatoLiquidazione != nil {
		company.StatoLiquidazione = *input.StatoLiquidazione
	}
	if input.Email != nil {
		company.Email = *input.Email
	}
	if input.PEC != nil {
		company.PEC = *input.PEC
	}
	if input.Phone != nil {
		company.Phone = NormalizePhone(*input.Phone)
	}
	if input.IBAN != nil {
		company.IBAN = *input.IBAN
	}
	if input.BIC != nil {
		company.BIC = *input.BIC
	}
	if input.ABI != nil {
		company.ABI = *input.ABI
	}
	if input.CAB != nil {
		company.CAB = *input.CAB
	}
	if input.Beneficiario != nil {
		company.Beneficiario = *input.Beneficiario
	}
	if input.IstitutoFinanziario != nil {
		company.IstitutoFinanziario = *input.IstitutoFinanziario
	}
	if input.Notes != nil {
		company.Notes = *input.Notes
	}

	company.UpdatedAt = time.Now()

	if err := s.companyRepo.Update(ctx, company); err != nil {
		return nil, err
	}

	return company, nil
}

func (s *companyService) DeleteCompany(ctx context.Context, uuid string) error {
	return s.companyRepo.SoftDelete(ctx, uuid)
}

func (s *companyService) SetDefaultCompany(ctx context.Context, uuid string) (*models.Company, error) {
	// Verify company exists
	company, err := s.companyRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrCompanyNotFound) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}

	// Set as default
	if err := s.companyRepo.SetDefault(ctx, uuid); err != nil {
		return nil, err
	}

	// Return updated company
	company.IsDefault = true
	company.UpdatedAt = time.Now()

	s.logger.Info("company set as default",
		"companyUUID", company.UUID,
		"denomination", company.Denomination,
	)

	return company, nil
}

func (s *companyService) validateCreateInput(input *models.CreateCompanyInput) error {
	// Validate FiscalIDCountry is a valid 2-letter country code (ISO 3166-1 alpha-2)
	if input.FiscalIDCountry == "" {
		return errors.New("fiscal ID country is required")
	}
	if err := ValidateNazione(input.FiscalIDCountry); err != nil {
		return errors.New("fiscal ID country must be a valid 2-letter country code (e.g., IT)")
	}
	if input.FiscalIDCode == "" {
		return errors.New("fiscal ID code (P.IVA) is required")
	}
	if input.Denomination == "" {
		return errors.New("company denomination is required")
	}
	if input.RegimeFiscale == "" {
		return errors.New("fiscal regime is required")
	}
	if input.Address == "" || input.City == "" || input.PostalCode == "" {
		return errors.New("address, city, and postal code are required")
	}
	if input.Country == "" || len(input.Country) != 2 {
		return errors.New("country must be 2 characters")
	}

	// Validate fiscal regime (RF01-RF20, excluding RF03 which is invalid)
	validRegimes := map[models.RegimeFiscale]bool{
		models.RegimeOrdinario:          true, // RF01
		models.RegimeContributtiMinimi:  true, // RF02
		models.RegimeAgevolato:          true, // RF04
		models.RegimeVenditaSaliTabacchi: true, // RF05
		models.RegimeCommercioFiammiferi: true, // RF06
		models.RegimeEditoria:           true, // RF07
		models.RegimeTelefonia:          true, // RF08
		models.RegimeRivenditaDocumenti: true, // RF09
		models.RegimeIntrattenimenti:    true, // RF10
		models.RegimeAgenzieViaggio:     true, // RF11
		models.RegimeAgroalimentare:     true, // RF12
		models.RegimeVenditePortaPorta:  true, // RF13
		models.RegimeRivenditaBeniUsati: true, // RF14
		models.RegimeAgenzieVenditeAste: true, // RF15
		models.RegimeIVAPerCassa:        true, // RF16
		models.RegimeIVAPerCassaGenerale: true, // RF17
		models.RegimeAltro:              true, // RF18
		models.RegimeForfettario:        true, // RF19
		models.RegimeFranchigiaIVA:      true, // RF20
	}
	if !validRegimes[input.RegimeFiscale] {
		return errors.New("invalid fiscal regime")
	}

	return nil
}
