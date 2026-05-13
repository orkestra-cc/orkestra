package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/repository"
)

var (
	ErrServiceCodeRequired      = errors.New("service code is required")
	ErrServiceNameRequired      = errors.New("service name is required")
	ErrServiceNoTiers           = errors.New("service must have at least one pricing tier")
	ErrServiceCodeAlreadyExists = errors.New("service code already exists")
	ErrInvalidCycle             = errors.New("invalid billing cycle")
)

type ServiceService struct {
	repo   repository.ServiceRepository
	logger *slog.Logger
}

func NewServiceService(repo repository.ServiceRepository, logger *slog.Logger) *ServiceService {
	return &ServiceService{repo: repo, logger: logger}
}

func (s *ServiceService) Create(ctx context.Context, in *models.Service) (*models.Service, error) {
	in.Code = strings.TrimSpace(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	if in.Code == "" {
		return nil, ErrServiceCodeRequired
	}
	if in.Name == "" {
		return nil, ErrServiceNameRequired
	}
	if len(in.PricingTiers) == 0 {
		return nil, ErrServiceNoTiers
	}
	for i := range in.PricingTiers {
		if !in.PricingTiers[i].Cycle.IsValid() {
			return nil, ErrInvalidCycle
		}
		if in.PricingTiers[i].Currency == "" {
			in.PricingTiers[i].Currency = "EUR"
		}
	}

	if existing, err := s.repo.GetByCode(ctx, in.Code); err == nil && existing != nil {
		return nil, ErrServiceCodeAlreadyExists
	}

	in.UUID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	in.UpdatedAt = in.CreatedAt
	if err := s.repo.Create(ctx, in); err != nil {
		return nil, err
	}
	return in, nil
}

func (s *ServiceService) Get(ctx context.Context, uuid string) (*models.Service, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

func (s *ServiceService) List(ctx context.Context, f repository.ServiceFilters) ([]models.Service, error) {
	return s.repo.List(ctx, f)
}

func (s *ServiceService) Update(ctx context.Context, uuid string, patch *models.Service) (*models.Service, error) {
	existing, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if patch.Name != "" {
		existing.Name = patch.Name
	}
	if patch.Category != "" {
		existing.Category = patch.Category
	}
	if patch.Description != "" {
		existing.Description = patch.Description
	}
	existing.Active = patch.Active
	if len(patch.PricingTiers) > 0 {
		for _, t := range patch.PricingTiers {
			if !t.Cycle.IsValid() {
				return nil, ErrInvalidCycle
			}
		}
		existing.PricingTiers = patch.PricingTiers
	}
	existing.SetupFeeCents = patch.SetupFeeCents
	if patch.Metadata != nil {
		existing.Metadata = patch.Metadata
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *ServiceService) Delete(ctx context.Context, uuid string) error {
	return s.repo.Delete(ctx, uuid)
}
