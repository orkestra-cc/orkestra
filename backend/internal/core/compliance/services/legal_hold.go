package services

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
)

// legalHoldRepo is the persistence seam the service depends on. Satisfied by
// *repository.LegalHoldRepository in production; a fake stands in for unit
// tests so the service logic is exercised without MongoDB.
type legalHoldRepo interface {
	Insert(ctx context.Context, h *models.LegalHold) error
	Release(ctx context.Context, uuid, releasedBy, reason string) error
	ListActive(ctx context.Context, userUUID string) ([]models.LegalHold, error)
	IsHeld(ctx context.Context, userUUID string) (bool, error)
}

// LegalHoldService manages litigation/investigation holds and answers the
// DSR pipeline's IsHeld check — it satisfies DSRService's LegalHoldChecker.
type LegalHoldService struct {
	repo legalHoldRepo
}

// NewLegalHoldService binds the service to its repository.
func NewLegalHoldService(repo legalHoldRepo) *LegalHoldService {
	return &LegalHoldService{repo: repo}
}

// PlaceInput carries the fields an operator supplies when placing a hold.
type PlaceInput struct {
	UserUUID string
	TenantID string
	Reason   string
	CaseRef  string
	PlacedBy string
}

// Place records a new active hold for the subject.
func (s *LegalHoldService) Place(ctx context.Context, in PlaceInput) (*models.LegalHold, error) {
	h := &models.LegalHold{
		UUID:     uuid.NewString(),
		UserUUID: in.UserUUID,
		TenantID: in.TenantID,
		Reason:   in.Reason,
		CaseRef:  in.CaseRef,
		PlacedBy: in.PlacedBy,
		PlacedAt: time.Now().UTC(),
		Active:   true,
	}
	if err := s.repo.Insert(ctx, h); err != nil {
		return nil, err
	}
	return h, nil
}

// Release marks a hold inactive.
func (s *LegalHoldService) Release(ctx context.Context, uuid, releasedBy, reason string) error {
	return s.repo.Release(ctx, uuid, releasedBy, reason)
}

// ListActive returns active holds, optionally scoped to one subject.
func (s *LegalHoldService) ListActive(ctx context.Context, userUUID string) ([]models.LegalHold, error) {
	return s.repo.ListActive(ctx, userUUID)
}

// IsHeld satisfies the DSR pipeline's LegalHoldChecker — an active hold blocks
// erasure (and retention auto-cleanup) for the subject.
func (s *LegalHoldService) IsHeld(ctx context.Context, userUUID string) (bool, error) {
	return s.repo.IsHeld(ctx, userUUID)
}
