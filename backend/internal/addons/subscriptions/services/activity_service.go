package services

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/repository"
)

// ActivityService appends audit events to a subscription's activity log.
// Failures are logged but never propagated — an audit write should not
// break a charge flow.
type ActivityService struct {
	repo   repository.ActivityRepository
	logger *slog.Logger
}

func NewActivityService(repo repository.ActivityRepository, logger *slog.Logger) *ActivityService {
	return &ActivityService{repo: repo, logger: logger}
}

func (s *ActivityService) Log(ctx context.Context, sub *models.Subscription, actor string, t models.ActivityType, message string, payload map[string]any) {
	entry := &models.ActivityLog{
		UUID:             uuid.NewString(),
		SubscriptionUUID: sub.UUID,
		TenantUUID:       sub.TenantUUID,
		Type:             t,
		Actor:            actor,
		Message:          message,
		Payload:          payload,
	}
	if err := s.repo.Create(ctx, entry); err != nil {
		s.logger.Warn("activity log write failed",
			slog.String("subscriptionUUID", sub.UUID),
			slog.String("type", string(t)),
			slog.String("error", err.Error()),
		)
	}
}

func (s *ActivityService) List(ctx context.Context, subscriptionUUID string, limit int64) ([]models.ActivityLog, error) {
	return s.repo.ListBySubscription(ctx, subscriptionUUID, limit)
}
