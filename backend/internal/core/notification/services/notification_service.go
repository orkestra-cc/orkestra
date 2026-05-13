package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
)

// URLBuilder renders the absolute URLs used inside templates.
// It's injected so the notification module stays agnostic of FrontendURL config.
type URLBuilder func(path string) string

// Options configures the orchestrator.
type Options struct {
	AppName        string
	SupportEmail   string
	URLBuilder     URLBuilder
	DefaultLocale  string
	IdempotencyTTL time.Duration
}

// NotificationService orchestrates preferences, templates, delivery and
// logging. It satisfies iface.NotificationSender.
type NotificationService struct {
	logRepo      repository.NotificationRepository
	tmplService  TemplateService
	prefService  PreferenceService
	unsubService UnsubscribeService
	email        EmailSender
	logger       *slog.Logger
	opts         Options
}

func NewNotificationService(
	logRepo repository.NotificationRepository,
	tmplService TemplateService,
	prefService PreferenceService,
	unsubService UnsubscribeService,
	email EmailSender,
	logger *slog.Logger,
	opts Options,
) *NotificationService {
	if opts.DefaultLocale == "" {
		opts.DefaultLocale = "en"
	}
	if opts.IdempotencyTTL == 0 {
		opts.IdempotencyTTL = 1 * time.Hour
	}
	return &NotificationService{
		logRepo:      logRepo,
		tmplService:  tmplService,
		prefService:  prefService,
		unsubService: unsubService,
		email:        email,
		logger:       logger,
		opts:         opts,
	}
}

// IsConfigured returns true when the active email channel has usable
// transport credentials. Auth uses this to decide whether to allow signups
// when email verification is required.
func (s *NotificationService) IsConfigured(ctx context.Context) bool {
	if s.email == nil {
		return false
	}
	return s.email.IsConfigured()
}

// Send dispatches a fully rendered notification. Consumers that already
// produced subject+body use this; consumers that want templating use
// SendTemplated instead.
func (s *NotificationService) Send(ctx context.Context, req iface.NotificationRequest) (*iface.NotificationResult, error) {
	if req.Channel == "" {
		req.Channel = models.ChannelEmail
	}
	if req.Channel != models.ChannelEmail {
		return nil, fmt.Errorf("notification: channel %q not supported", req.Channel)
	}
	if len(req.Recipients) == 0 {
		return nil, errors.New("notification: no recipients")
	}

	if existing, _ := s.logRepo.FindByIdempotencyKey(ctx, req.IdempotencyKey, time.Now().Add(-s.opts.IdempotencyTTL)); existing != nil {
		return &iface.NotificationResult{
			ID:       existing.UUID,
			Status:   existing.Status,
			Provider: existing.Provider,
			Error:    existing.Error,
		}, nil
	}

	recipient := req.Recipients[0]
	return s.dispatchEmail(ctx, dispatchInput{
		Category:       req.Category,
		Type:           req.Type,
		Recipient:      recipient,
		Subject:        req.Subject,
		BodyText:       req.Body,
		BodyHTML:       req.BodyHTML,
		IdempotencyKey: req.IdempotencyKey,
	})
}

// SendTemplated resolves a template, injects automatic variables
// (unsubscribe URL, preferences URL, app metadata), renders and sends.
func (s *NotificationService) SendTemplated(ctx context.Context, req iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	if req.Channel == "" {
		req.Channel = models.ChannelEmail
	}
	if req.Channel != models.ChannelEmail {
		return nil, fmt.Errorf("notification: channel %q not supported", req.Channel)
	}
	if len(req.Recipients) == 0 {
		return nil, errors.New("notification: no recipients")
	}

	if existing, _ := s.logRepo.FindByIdempotencyKey(ctx, req.IdempotencyKey, time.Now().Add(-s.opts.IdempotencyTTL)); existing != nil {
		return &iface.NotificationResult{
			ID:       existing.UUID,
			Status:   existing.Status,
			Provider: existing.Provider,
			Error:    existing.Error,
		}, nil
	}

	locale := req.Locale
	if locale == "" {
		locale = s.opts.DefaultLocale
	}
	tmpl, err := s.tmplService.Get(ctx, req.TemplateID, locale)
	if err != nil {
		return nil, fmt.Errorf("notification: load template %s: %w", req.TemplateID, err)
	}

	recipient := req.Recipients[0]

	// Build template data: caller values + auto-injected footer URLs.
	data := map[string]any{}
	for k, v := range req.Data {
		data[k] = v
	}
	if _, ok := data["AppName"]; !ok {
		data["AppName"] = s.opts.AppName
	}
	if _, ok := data["SupportEmail"]; !ok {
		data["SupportEmail"] = s.opts.SupportEmail
	}

	unsubToken, err := s.unsubService.IssueToken(ctx, recipient.UserUUID, recipient.Address, req.Category)
	if err != nil {
		s.logger.Warn("notification: failed to issue unsubscribe token", slog.String("error", err.Error()))
	}
	data["UnsubscribeURL"] = s.buildURL(fmt.Sprintf("/notifications/unsubscribe?token=%s", unsubToken))
	data["PreferencesURL"] = s.buildURL("/account/notifications")

	rendered, err := s.tmplService.Render(tmpl, data)
	if err != nil {
		return nil, err
	}

	return s.dispatchEmail(ctx, dispatchInput{
		Category:       req.Category,
		Type:           req.Type,
		Recipient:      recipient,
		Subject:        rendered.Subject,
		BodyText:       rendered.BodyText,
		BodyHTML:       rendered.BodyHTML,
		TemplateID:     tmpl.TemplateID,
		IdempotencyKey: req.IdempotencyKey,
	})
}

type dispatchInput struct {
	Category       string
	Type           string
	Recipient      iface.Recipient
	Subject        string
	BodyText       string
	BodyHTML       string
	TemplateID     string
	IdempotencyKey string
}

func (s *NotificationService) dispatchEmail(ctx context.Context, in dispatchInput) (*iface.NotificationResult, error) {
	logDoc := &models.NotificationDoc{
		UUID:              uuid.Must(uuid.NewV7()).String(),
		Channel:           models.ChannelEmail,
		Type:              in.Type,
		Category:          in.Category,
		TemplateID:        in.TemplateID,
		RecipientUserUUID: in.Recipient.UserUUID,
		RecipientAddress:  in.Recipient.Address,
		Subject:           in.Subject,
		IdempotencyKey:    in.IdempotencyKey,
		CreatedAt:         time.Now(),
	}

	// Preference check (marketing only).
	canDeliver, err := s.prefService.CanDeliver(ctx, in.Recipient.UserUUID, in.Category, models.ChannelEmail, in.Type)
	if err != nil {
		s.logger.Warn("notification: preference lookup failed", slog.String("error", err.Error()))
	}
	if !canDeliver {
		logDoc.Status = models.StatusSuppressed
		logDoc.Error = "user opted out of category"
		_ = s.logRepo.Create(ctx, logDoc)
		return &iface.NotificationResult{ID: logDoc.UUID, Status: logDoc.Status}, nil
	}

	// Dispatch via the email sender. The sender returns an error on
	// transport failures; we capture it in the log doc but still create
	// the document so the admin can see the failure.
	provider := s.email.ProviderName()
	sendErr := s.email.Send(ctx, EmailMessage{
		To:       in.Recipient.Address,
		ToName:   in.Recipient.Name,
		Subject:  in.Subject,
		BodyText: in.BodyText,
		BodyHTML: in.BodyHTML,
	})

	now := time.Now()
	if sendErr != nil {
		logDoc.Status = models.StatusFailed
		logDoc.Error = sendErr.Error()
		logDoc.Provider = provider
		_ = s.logRepo.Create(ctx, logDoc)
		return &iface.NotificationResult{
			ID:       logDoc.UUID,
			Status:   logDoc.Status,
			Provider: provider,
			Error:    sendErr.Error(),
		}, sendErr
	}

	logDoc.Status = models.StatusSent
	logDoc.Provider = provider
	logDoc.SentAt = &now
	_ = s.logRepo.Create(ctx, logDoc)

	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: provider,
	}, nil
}

func (s *NotificationService) buildURL(path string) string {
	if s.opts.URLBuilder == nil {
		return path
	}
	return s.opts.URLBuilder(path)
}

// TemplateService exposes the template service for admin endpoints.
func (s *NotificationService) TemplateService() TemplateService { return s.tmplService }

// PreferenceService exposes the preference service for user-facing endpoints.
func (s *NotificationService) PreferenceService() PreferenceService { return s.prefService }

// UnsubscribeService exposes the unsubscribe service for public endpoints.
func (s *NotificationService) UnsubscribeService() UnsubscribeService { return s.unsubService }

// LogRepo exposes the log repository for admin listing.
func (s *NotificationService) LogRepo() repository.NotificationRepository { return s.logRepo }

// EmailSender exposes the configured sender (used by the /test endpoint).
func (s *NotificationService) EmailSender() EmailSender { return s.email }

// NormalizeAddress lowercases and trims an email address.
func NormalizeAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}
