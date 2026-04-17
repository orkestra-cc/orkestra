package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
)

var (
	ErrClientEmailRequired = errors.New("client email is required")
	ErrClientNameRequired  = errors.New("client legalName is required")
	ErrClientEmailTaken    = errors.New("a client with this email already exists")
)

type ClientService struct {
	repo   repository.ClientRepository
	logger *slog.Logger
}

func NewClientService(repo repository.ClientRepository, logger *slog.Logger) *ClientService {
	return &ClientService{repo: repo, logger: logger}
}

func (s *ClientService) Create(ctx context.Context, in *models.Client) (*models.Client, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.LegalName = strings.TrimSpace(in.LegalName)
	if in.Email == "" {
		return nil, ErrClientEmailRequired
	}
	if in.LegalName == "" {
		return nil, ErrClientNameRequired
	}

	if existing, err := s.repo.GetByEmail(ctx, in.Email); err == nil && existing != nil {
		return nil, ErrClientEmailTaken
	}

	in.UUID = uuid.NewString()
	if in.Status == "" {
		in.Status = models.ClientActive
	}
	if in.DisplayName == "" {
		in.DisplayName = in.LegalName
	}
	in.CreatedAt = time.Now().UTC()
	in.UpdatedAt = in.CreatedAt
	if err := s.repo.Create(ctx, in); err != nil {
		return nil, err
	}
	return in, nil
}

func (s *ClientService) Get(ctx context.Context, uuid string) (*models.Client, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

func (s *ClientService) List(ctx context.Context, f repository.ClientFilters) ([]models.Client, error) {
	return s.repo.List(ctx, f)
}

func (s *ClientService) Update(ctx context.Context, uuid string, patch *models.Client) (*models.Client, error) {
	existing, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if patch.LegalName != "" {
		existing.LegalName = patch.LegalName
	}
	if patch.DisplayName != "" {
		existing.DisplayName = patch.DisplayName
	}
	if patch.Email != "" {
		existing.Email = strings.ToLower(strings.TrimSpace(patch.Email))
	}
	if patch.VATNumber != "" {
		existing.VATNumber = patch.VATNumber
	}
	if patch.FiscalCode != "" {
		existing.FiscalCode = patch.FiscalCode
	}
	if patch.Status != "" {
		existing.Status = patch.Status
	}
	if patch.OrgUUID != "" {
		existing.OrgUUID = patch.OrgUUID
	}
	if patch.Notes != "" {
		existing.Notes = patch.Notes
	}
	if (models.ClientAddress{}) != patch.BillingAddr {
		existing.BillingAddr = patch.BillingAddr
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *ClientService) Archive(ctx context.Context, uuid string) error {
	c, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}
	c.Status = models.ClientArchived
	return s.repo.Update(ctx, c)
}

func (s *ClientService) SetStripeCustomerID(ctx context.Context, clientUUID, stripeCustomerID string) error {
	return s.repo.SetStripeCustomerID(ctx, clientUUID, stripeCustomerID)
}
