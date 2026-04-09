package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
)

// SupplierService defines the interface for supplier business logic
type SupplierService interface {
	CreateSupplier(ctx context.Context, input *models.CreateSupplierInput, createdBy string) (*models.Supplier, error)
	GetSupplier(ctx context.Context, uuid string) (*models.Supplier, error)
	GetSupplierByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Supplier, error)
	ListSuppliers(ctx context.Context, search string, pagination models.PaginationParams) (*models.SupplierListResponse, error)
	UpdateSupplier(ctx context.Context, uuid string, input *models.UpdateSupplierInput) (*models.Supplier, error)
	DeleteSupplier(ctx context.Context, uuid string) error
}

type supplierService struct {
	supplierRepo repository.SupplierRepository
	logger       *slog.Logger
}

// NewSupplierService creates a new SupplierService
func NewSupplierService(supplierRepo repository.SupplierRepository, logger *slog.Logger) SupplierService {
	return &supplierService{
		supplierRepo: supplierRepo,
		logger:       logger,
	}
}

func (s *supplierService) CreateSupplier(ctx context.Context, input *models.CreateSupplierInput, createdBy string) (*models.Supplier, error) {
	// Validate input
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}

	// Check if supplier with same fiscal ID already exists
	exists, err := s.supplierRepo.ExistsByFiscalID(ctx, input.FiscalIDCode)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, repository.ErrSupplierAlreadyExists
	}

	supplier := &models.Supplier{
		UUID:            uuid.New().String(),
		FiscalIDCountry: input.FiscalIDCountry,
		FiscalIDCode:    input.FiscalIDCode,
		CodiceFiscale:   input.CodiceFiscale,
		IsCompany:       input.IsCompany,
		Denomination:    input.Denomination,
		Name:            input.Name,
		Surname:         input.Surname,
		RegimeFiscale:   input.RegimeFiscale,
		Address:         input.Address,
		NumeroCivico:    input.NumeroCivico,
		City:            input.City,
		Province:        input.Province,
		PostalCode:      input.PostalCode,
		Country:         input.Country,
		Email:           input.Email,
		PEC:             input.PEC,
		Phone:           NormalizePhone(input.Phone),
		IBAN:            input.IBAN,
		BIC:             input.BIC,
		Notes:           input.Notes,
		IsActive:        true,
		CreatedBy:       createdBy,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.supplierRepo.Create(ctx, supplier); err != nil {
		return nil, err
	}

	s.logger.Info("supplier created",
		"supplierUUID", supplier.UUID,
		"fiscalID", supplier.FiscalIDCode,
		"createdBy", createdBy,
	)

	return supplier, nil
}

func (s *supplierService) GetSupplier(ctx context.Context, uuid string) (*models.Supplier, error) {
	supplier, err := s.supplierRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}
	return supplier, nil
}

func (s *supplierService) GetSupplierByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Supplier, error) {
	supplier, err := s.supplierRepo.GetByFiscalID(ctx, fiscalIDCode)
	if err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}
	return supplier, nil
}

func (s *supplierService) ListSuppliers(ctx context.Context, search string, pagination models.PaginationParams) (*models.SupplierListResponse, error) {
	suppliers, total, err := s.supplierRepo.List(ctx, search, pagination)
	if err != nil {
		return nil, err
	}

	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	return &models.SupplierListResponse{
		Suppliers:  suppliers,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *supplierService) UpdateSupplier(ctx context.Context, uuid string, input *models.UpdateSupplierInput) (*models.Supplier, error) {
	supplier, err := s.supplierRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}

	// Apply updates
	if input.Denomination != nil {
		supplier.Denomination = *input.Denomination
	}
	if input.Name != nil {
		supplier.Name = *input.Name
	}
	if input.Surname != nil {
		supplier.Surname = *input.Surname
	}
	if input.RegimeFiscale != nil {
		supplier.RegimeFiscale = *input.RegimeFiscale
	}
	if input.Address != nil {
		supplier.Address = *input.Address
	}
	if input.NumeroCivico != nil {
		supplier.NumeroCivico = *input.NumeroCivico
	}
	if input.City != nil {
		supplier.City = *input.City
	}
	if input.Province != nil {
		supplier.Province = *input.Province
	}
	if input.PostalCode != nil {
		supplier.PostalCode = *input.PostalCode
	}
	if input.Email != nil {
		supplier.Email = *input.Email
	}
	if input.PEC != nil {
		supplier.PEC = *input.PEC
	}
	if input.Phone != nil {
		supplier.Phone = NormalizePhone(*input.Phone)
	}
	if input.IBAN != nil {
		supplier.IBAN = *input.IBAN
	}
	if input.BIC != nil {
		supplier.BIC = *input.BIC
	}
	if input.Notes != nil {
		supplier.Notes = *input.Notes
	}

	supplier.UpdatedAt = time.Now()

	if err := s.supplierRepo.Update(ctx, supplier); err != nil {
		return nil, err
	}

	return supplier, nil
}

func (s *supplierService) DeleteSupplier(ctx context.Context, uuid string) error {
	return s.supplierRepo.SoftDelete(ctx, uuid)
}

func (s *supplierService) validateCreateInput(input *models.CreateSupplierInput) error {
	if input.FiscalIDCountry == "" || len(input.FiscalIDCountry) != 2 {
		return errors.New("fiscal ID country must be 2 characters")
	}
	if input.FiscalIDCode == "" {
		return errors.New("fiscal ID code is required")
	}
	if input.IsCompany && input.Denomination == "" {
		return errors.New("denomination is required for companies")
	}
	if !input.IsCompany && (input.Name == "" || input.Surname == "") {
		return errors.New("name and surname are required for individuals")
	}
	if input.Address == "" || input.City == "" || input.PostalCode == "" {
		return errors.New("address, city, and postal code are required")
	}
	if input.Country == "" || len(input.Country) != 2 {
		return errors.New("country must be 2 characters")
	}

	return nil
}
