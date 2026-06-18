package services

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErasureRequestService runs the mediated right-to-erasure workflow: data
// subjects lodge requests; operators review, then execute (running the DSR
// erase) or reject. Distinct from the immediate /me/dsr/erase path.
type ErasureRequestService struct {
	repo *repository.ErasureRequestRepository
	dsr  *DSRService
}

// NewErasureRequestService wires the workflow service to its repo + the DSR
// pipeline used at execution time.
func NewErasureRequestService(repo *repository.ErasureRequestRepository, dsr *DSRService) *ErasureRequestService {
	return &ErasureRequestService{repo: repo, dsr: dsr}
}

// Lodge records a pending erasure request for the subject.
func (s *ErasureRequestService) Lodge(ctx context.Context, userUUID, tenantID, reason string) (*models.ErasureRequest, error) {
	req := &models.ErasureRequest{
		UUID:        uuid.NewString(),
		UserUUID:    userUUID,
		TenantID:    tenantID,
		Reason:      reason,
		Status:      models.ErasureRequestPending,
		RequestedAt: time.Now().UTC(),
	}
	if err := s.repo.Insert(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

// ListPending returns requests awaiting operator action.
func (s *ErasureRequestService) ListPending(ctx context.Context) ([]models.ErasureRequest, error) {
	return s.repo.ListByStatus(ctx, models.ErasureRequestPending)
}

// Execute runs the DSR erase for the request's subject in the given mode and
// marks the request completed. The DSR legal-hold gate applies — a held
// subject errors (ErrLegalHoldActive) and the request stays pending.
func (s *ErasureRequestService) Execute(ctx context.Context, requestUUID string, mode iface.EraseMode, resolvedBy string) (*EraseResult, error) {
	req, err := s.repo.Get(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	if req.Status != models.ErasureRequestPending {
		return nil, repository.ErrErasureRequestNotFound
	}
	res, err := s.dsr.Erase(ctx, req.UserUUID, mode)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Resolve(ctx, requestUUID, models.ErasureRequestCompleted, resolvedBy, eraseModeLabel(mode), ""); err != nil {
		return res, err
	}
	return res, nil
}

// Reject closes a request without erasing.
func (s *ErasureRequestService) Reject(ctx context.Context, requestUUID, resolvedBy, note string) error {
	return s.repo.Resolve(ctx, requestUUID, models.ErasureRequestRejected, resolvedBy, "", note)
}
