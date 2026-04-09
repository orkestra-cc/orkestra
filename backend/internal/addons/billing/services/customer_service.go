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

// CustomerService defines the interface for customer business logic
type CustomerService interface {
	CreateCustomer(ctx context.Context, input *models.CreateCustomerInput, createdBy string) (*models.Customer, error)
	GetCustomer(ctx context.Context, uuid string) (*models.Customer, error)
	GetCustomerByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Customer, error)
	ListCustomers(ctx context.Context, search string, pagination models.PaginationParams) (*models.CustomerListResponse, error)
	UpdateCustomer(ctx context.Context, uuid string, input *models.UpdateCustomerInput) (*models.Customer, error)
	DeleteCustomer(ctx context.Context, uuid string) error
}

type customerService struct {
	customerRepo repository.CustomerRepository
	logger       *slog.Logger
}

// NewCustomerService creates a new CustomerService
func NewCustomerService(customerRepo repository.CustomerRepository, logger *slog.Logger) CustomerService {
	return &customerService{
		customerRepo: customerRepo,
		logger:       logger,
	}
}

func (s *customerService) CreateCustomer(ctx context.Context, input *models.CreateCustomerInput, createdBy string) (*models.Customer, error) {
	// Validate input
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}

	// Check if customer with same fiscal ID already exists
	exists, err := s.customerRepo.ExistsByFiscalID(ctx, input.FiscalIDCode)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, repository.ErrCustomerAlreadyExists
	}

	// Determine codice destinatario
	codiceDestinatario := input.CodiceDestinatario
	if input.IsPA && input.CodiceUfficio != "" {
		codiceDestinatario = input.CodiceUfficio
	}

	customer := &models.Customer{
		UUID:               uuid.New().String(),
		FiscalIDCountry:    input.FiscalIDCountry,
		FiscalIDCode:       input.FiscalIDCode,
		CodiceFiscale:      input.CodiceFiscale,
		IsCompany:          input.IsCompany,
		Denomination:       input.Denomination,
		Name:               input.Name,
		Surname:            input.Surname,
		Address:            input.Address,
		NumeroCivico:       input.NumeroCivico,
		City:               input.City,
		Province:           input.Province,
		PostalCode:         input.PostalCode,
		Country:            input.Country,
		Email:              input.Email,
		PEC:                input.PEC,
		Phone:              NormalizePhone(input.Phone),
		CodiceDestinatario: codiceDestinatario,
		PECDestinatario:    input.PECDestinatario,
		IsPA:               input.IsPA,
		CodiceUfficio:      input.CodiceUfficio,
		Notes:              input.Notes,
		IsActive:           true,
		CreatedBy:          createdBy,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := s.customerRepo.Create(ctx, customer); err != nil {
		return nil, err
	}

	s.logger.Info("customer created",
		"customerUUID", customer.UUID,
		"fiscalID", customer.FiscalIDCode,
		"createdBy", createdBy,
	)

	return customer, nil
}

func (s *customerService) GetCustomer(ctx context.Context, uuid string) (*models.Customer, error) {
	customer, err := s.customerRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, err
	}
	return customer, nil
}

func (s *customerService) GetCustomerByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Customer, error) {
	customer, err := s.customerRepo.GetByFiscalID(ctx, fiscalIDCode)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, err
	}
	return customer, nil
}

func (s *customerService) ListCustomers(ctx context.Context, search string, pagination models.PaginationParams) (*models.CustomerListResponse, error) {
	customers, total, err := s.customerRepo.List(ctx, search, pagination)
	if err != nil {
		return nil, err
	}

	// Ensure customers is never nil (serializes as [] instead of null)
	if customers == nil {
		customers = []models.Customer{}
	}

	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	return &models.CustomerListResponse{
		Customers:  customers,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *customerService) UpdateCustomer(ctx context.Context, uuid string, input *models.UpdateCustomerInput) (*models.Customer, error) {
	customer, err := s.customerRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, err
	}

	// Apply updates
	if input.Denomination != nil {
		customer.Denomination = *input.Denomination
	}
	if input.Name != nil {
		customer.Name = *input.Name
	}
	if input.Surname != nil {
		customer.Surname = *input.Surname
	}
	if input.Address != nil {
		customer.Address = *input.Address
	}
	if input.NumeroCivico != nil {
		customer.NumeroCivico = *input.NumeroCivico
	}
	if input.City != nil {
		customer.City = *input.City
	}
	if input.Province != nil {
		customer.Province = *input.Province
	}
	if input.PostalCode != nil {
		customer.PostalCode = *input.PostalCode
	}
	if input.Email != nil {
		customer.Email = *input.Email
	}
	if input.PEC != nil {
		customer.PEC = *input.PEC
	}
	if input.Phone != nil {
		customer.Phone = NormalizePhone(*input.Phone)
	}
	if input.CodiceDestinatario != nil {
		customer.CodiceDestinatario = *input.CodiceDestinatario
	}
	if input.PECDestinatario != nil {
		customer.PECDestinatario = *input.PECDestinatario
	}
	if input.Notes != nil {
		customer.Notes = *input.Notes
	}

	customer.UpdatedAt = time.Now()

	if err := s.customerRepo.Update(ctx, customer); err != nil {
		return nil, err
	}

	return customer, nil
}

func (s *customerService) DeleteCustomer(ctx context.Context, uuid string) error {
	return s.customerRepo.SoftDelete(ctx, uuid)
}

func (s *customerService) validateCreateInput(input *models.CreateCustomerInput) error {
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

	// Validate codice destinatario format
	if input.CodiceDestinatario != "" {
		if input.IsPA && len(input.CodiceDestinatario) != 6 {
			return errors.New("codice destinatario for PA must be 6 characters")
		}
		if !input.IsPA && len(input.CodiceDestinatario) != 7 {
			return errors.New("codice destinatario for B2B must be 7 characters")
		}
	}

	return nil
}
