package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// ErrTenantNotFound is returned when PromoteTenantToCustomer cannot resolve
// the tenant via the iface.TenantProvider. The handler maps this to 404.
var ErrTenantNotFound = errors.New("tenant not found")

// ErrTenantNotExternal signals an attempt to promote a Tier-1 internal
// tenant to a billing customer. Only Tier-2 external tenants can have a
// FatturaPA recipient profile.
var ErrTenantNotExternal = errors.New("tenant is not an external (Tier-2) tenant")

// ErrTenantMissingDenomination signals the tenant carries neither a legal
// name nor a name — there's nothing to seed Customer.Denomination with.
var ErrTenantMissingDenomination = errors.New("tenant has no name to derive customer denomination from")

// ErrTenantProviderUnavailable is returned when the tenant module is not
// registered in the ServiceRegistry. Indicates a misconfigured deployment.
var ErrTenantProviderUnavailable = errors.New("tenant provider unavailable")

// CustomerService defines the interface for customer business logic
type CustomerService interface {
	CreateCustomer(ctx context.Context, input *models.CreateCustomerInput, createdBy string) (*models.Customer, error)
	GetCustomer(ctx context.Context, uuid string) (*models.Customer, error)
	GetCustomerByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Customer, error)
	ListCustomers(ctx context.Context, search string, pagination models.PaginationParams) (*models.CustomerListResponse, error)
	UpdateCustomer(ctx context.Context, uuid string, input *models.UpdateCustomerInput) (*models.Customer, error)
	DeleteCustomer(ctx context.Context, uuid string) error
	// PromoteTenantToCustomer creates (or returns) the FatturaPA Customer
	// linked to a Tier-2 external tenant. Idempotent: when a customer
	// already exists for the tenant the existing row is returned.
	// ADR-0001 PR-4.
	PromoteTenantToCustomer(ctx context.Context, tenantUUID string) (*models.Customer, error)
}

type customerService struct {
	customerRepo   repository.CustomerRepository
	tenantProvider iface.TenantProvider
	logger         *slog.Logger
}

// NewCustomerService creates a new CustomerService.
// The tenantProvider is optional — when nil, the PromoteTenantToCustomer
// flow returns ErrTenantProviderUnavailable. The other methods do not
// touch the provider. ADR-0001 PR-4 added this dependency.
func NewCustomerService(customerRepo repository.CustomerRepository, tenantProvider iface.TenantProvider, logger *slog.Logger) CustomerService {
	return &customerService{
		customerRepo:   customerRepo,
		tenantProvider: tenantProvider,
		logger:         logger,
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

// PromoteTenantToCustomer creates a Customer pre-filled from a Tier-2
// tenant's iface.Tenant fields, or returns the existing customer when one
// is already linked. Idempotent end-to-end. ADR-0001 PR-4.
//
// Address fields (street/city/postal code) cannot be derived from
// iface.Tenant — that DTO only exposes Country. The caller is expected to
// fill them in via the existing customer edit UI before sending invoices.
// We therefore set IsActive=true but the row will fail FatturaPA-time
// validation until address fields are populated, which is the desired
// behavior (the customer exists in the directory but isn't billable yet).
func (s *customerService) PromoteTenantToCustomer(ctx context.Context, tenantUUID string) (*models.Customer, error) {
	if tenantUUID == "" {
		return nil, errors.New("tenantUUID is required")
	}

	// Step 1 — idempotency. If a customer is already linked to this
	// tenant, return it unchanged. The caller (handler) treats this as
	// a successful response, not a no-op error.
	existing, err := s.customerRepo.GetByTenantUUID(ctx, tenantUUID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, repository.ErrCustomerNotFound) {
		return nil, err
	}

	// Step 2 — resolve the tenant. The provider is optional; without it
	// we can't seed the customer fields, so fail loudly.
	if s.tenantProvider == nil {
		return nil, ErrTenantProviderUnavailable
	}
	tenant, err := s.tenantProvider.GetTenant(ctx, tenantUUID)
	if err != nil || tenant == nil {
		return nil, ErrTenantNotFound
	}
	if tenant.Kind != iface.TenantKindExternal {
		return nil, ErrTenantNotExternal
	}

	// Step 3 — build the Customer. LegalName wins; fall back to Name.
	denomination := tenant.LegalName
	if denomination == "" {
		denomination = tenant.Name
	}
	if denomination == "" {
		return nil, ErrTenantMissingDenomination
	}

	// FatturaPA requires a 2-char ISO country code. Default to IT when the
	// tenant has no country set yet (matches the most common case for the
	// Italian-focused billing module). The user can change it from the
	// edit form.
	country := tenant.Country
	if country == "" {
		country = "IT"
	}

	// FiscalIDCode lookup. The FatturaPA recipient identifier is required
	// downstream; if the tenant carries neither VAT nor CF the row is
	// still inserted but won't pass invoice validation until the user
	// fills it in. We prefer VATNumber (P.IVA) when present.
	fiscalIDCode := tenant.VATNumber
	if fiscalIDCode == "" {
		fiscalIDCode = tenant.FiscalCode
	}

	customer := &models.Customer{
		UUID:            uuid.New().String(),
		FiscalIDCountry: country,
		FiscalIDCode:    fiscalIDCode,
		CodiceFiscale:   tenant.FiscalCode,
		IsCompany:       true, // external tenants are organizational by default
		Denomination:    denomination,
		// Address fields intentionally empty — iface.Tenant doesn't
		// carry the breakdown. The user fills them via the edit UI.
		Country:    country,
		Email:      tenant.Email,
		IsActive:   true,
		TenantUUID: tenantUUID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := s.customerRepo.Create(ctx, customer); err != nil {
		// Race-safe idempotency: a concurrent promote can win the
		// unique-(tenantUUID) index. Re-read and return the winner.
		if errors.Is(err, repository.ErrCustomerAlreadyExists) {
			if existing, getErr := s.customerRepo.GetByTenantUUID(ctx, tenantUUID); getErr == nil {
				return existing, nil
			}
		}
		return nil, err
	}

	s.logger.Info("billing customer promoted from tenant",
		"customerUUID", customer.UUID,
		"tenantUUID", tenantUUID,
		"denomination", denomination,
	)

	return customer, nil
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
