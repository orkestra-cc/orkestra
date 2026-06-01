package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
)

// NotificationService defines the interface for notification business logic
type NotificationService interface {
	GetNotification(ctx context.Context, uuid string) (*models.SDINotification, error)
	GetNotificationsByInvoice(ctx context.Context, invoiceUUID string) ([]models.SDINotification, error)
	ListNotifications(ctx context.Context, filters *models.NotificationFilters, pagination models.PaginationParams) ([]models.SDINotification, int64, error)
	MarkAsProcessed(ctx context.Context, uuid string, processedBy string) error
	GetSummary(ctx context.Context, fromDate, toDate *time.Time) (*models.NotificationSummary, error)
}

type notificationService struct {
	notificationRepo repository.NotificationRepository
	logger           *slog.Logger
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(notificationRepo repository.NotificationRepository, logger *slog.Logger) NotificationService {
	return &notificationService{
		notificationRepo: notificationRepo,
		logger:           logger,
	}
}

func (s *notificationService) GetNotification(ctx context.Context, uuid string) (*models.SDINotification, error) {
	notification, err := s.notificationRepo.GetByUUID(ctx, uuid)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationNotFound) {
			return nil, errors.New("notification not found")
		}
		return nil, err
	}
	return notification, nil
}

func (s *notificationService) GetNotificationsByInvoice(ctx context.Context, invoiceUUID string) ([]models.SDINotification, error) {
	return s.notificationRepo.GetByInvoiceUUID(ctx, invoiceUUID)
}

func (s *notificationService) ListNotifications(ctx context.Context, filters *models.NotificationFilters, pagination models.PaginationParams) ([]models.SDINotification, int64, error) {
	return s.notificationRepo.List(ctx, filters, pagination)
}

func (s *notificationService) MarkAsProcessed(ctx context.Context, uuid string, processedBy string) error {
	return s.notificationRepo.MarkAsProcessed(ctx, uuid, processedBy)
}

func (s *notificationService) GetSummary(ctx context.Context, fromDate, toDate *time.Time) (*models.NotificationSummary, error) {
	return s.notificationRepo.GetSummary(ctx, fromDate, toDate)
}
