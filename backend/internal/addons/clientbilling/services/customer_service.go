// Package services contains the business logic for the user-level billing
// projection. Validation lives here so both the HTTP handler and the
// cross-module provider go through the same input contract.
package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/orkestra/backend/internal/addons/clientbilling/models"
	"github.com/orkestra/backend/internal/addons/clientbilling/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// ErrInvalidProfile signals the caller-supplied billing input failed shape
// validation (missing legal name when IsCompany=true, missing both first
// and last name otherwise, etc.). Handlers map to 400.
var ErrInvalidProfile = errors.New("clientbilling: invalid billing profile")

// CustomerService owns the user-level billing projection.
type CustomerService struct {
	repo   repository.CustomerRepository
	logger *slog.Logger
}

// NewCustomerService constructs the service.
func NewCustomerService(repo repository.CustomerRepository, logger *slog.Logger) *CustomerService {
	return &CustomerService{repo: repo, logger: logger}
}

// Get returns (nil, nil) when the user has no billing profile yet —
// expected state for a freshly-registered client. Other errors propagate.
func (s *CustomerService) Get(ctx context.Context, userUUID string) (*iface.UserBillingProfile, error) {
	if userUUID == "" {
		return nil, ErrInvalidProfile
	}
	row, err := s.repo.GetByUserUUID(ctx, userUUID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toProfile(row), nil
}

// Upsert validates the input and creates / replaces the user's profile.
func (s *CustomerService) Upsert(ctx context.Context, in iface.UpsertUserBillingProfileInput) (*iface.UserBillingProfile, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	row := &models.UserBillingCustomer{
		UserUUID:     in.UserUUID,
		LegalName:    strings.TrimSpace(in.LegalName),
		FirstName:    strings.TrimSpace(in.FirstName),
		LastName:     strings.TrimSpace(in.LastName),
		Email:        strings.TrimSpace(in.Email),
		VATNumber:    strings.TrimSpace(in.VATNumber),
		FiscalCode:   strings.TrimSpace(in.FiscalCode),
		Country:      strings.ToUpper(strings.TrimSpace(in.Country)),
		AddressLine1: strings.TrimSpace(in.AddressLine1),
		AddressLine2: strings.TrimSpace(in.AddressLine2),
		City:         strings.TrimSpace(in.City),
		PostalCode:   strings.TrimSpace(in.PostalCode),
		Province:     strings.TrimSpace(in.Province),
		IsCompany:    in.IsCompany,
	}
	saved, err := s.repo.Upsert(ctx, row)
	if err != nil {
		return nil, err
	}
	return toProfile(saved), nil
}

// SetStripeCustomerID is the lazy persistence hook called by the payment
// flow on first charge / setup. Idempotent — re-applying the same value is
// a cheap no-op write.
func (s *CustomerService) SetStripeCustomerID(ctx context.Context, userUUID, stripeCustomerID string) error {
	if userUUID == "" || stripeCustomerID == "" {
		return ErrInvalidProfile
	}
	return s.repo.SetStripeCustomerID(ctx, userUUID, stripeCustomerID)
}

func validate(in iface.UpsertUserBillingProfileInput) error {
	if strings.TrimSpace(in.UserUUID) == "" {
		return ErrInvalidProfile
	}
	if in.IsCompany {
		if strings.TrimSpace(in.LegalName) == "" {
			return ErrInvalidProfile
		}
	} else {
		if strings.TrimSpace(in.FirstName) == "" && strings.TrimSpace(in.LastName) == "" {
			return ErrInvalidProfile
		}
	}
	// Country is required so Stripe customer creation can stamp it. Empty
	// string is the only invalid form — no list-of-countries enforcement.
	if strings.TrimSpace(in.Country) == "" {
		return ErrInvalidProfile
	}
	return nil
}

func toProfile(c *models.UserBillingCustomer) *iface.UserBillingProfile {
	if c == nil {
		return nil
	}
	return &iface.UserBillingProfile{
		UserUUID:         c.UserUUID,
		LegalName:        c.LegalName,
		FirstName:        c.FirstName,
		LastName:         c.LastName,
		Email:            c.Email,
		VATNumber:        c.VATNumber,
		FiscalCode:       c.FiscalCode,
		Country:          c.Country,
		AddressLine1:     c.AddressLine1,
		AddressLine2:     c.AddressLine2,
		City:             c.City,
		PostalCode:       c.PostalCode,
		Province:         c.Province,
		IsCompany:        c.IsCompany,
		StripeCustomerID: c.StripeCustomerID,
		CreatedAt:        c.CreatedAt,
		UpdatedAt:        c.UpdatedAt,
	}
}
