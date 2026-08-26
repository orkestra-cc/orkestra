package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
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
	resolver     SenderResolver
	drivers      *DriverRegistry
	logger       *slog.Logger
	opts         Options
}

func NewNotificationService(
	logRepo repository.NotificationRepository,
	tmplService TemplateService,
	prefService PreferenceService,
	unsubService UnsubscribeService,
	resolver SenderResolver,
	drivers *DriverRegistry,
	logger *slog.Logger,
	opts Options,
) *NotificationService {
	if opts.DefaultLocale == "" {
		opts.DefaultLocale = "en"
	}
	if opts.IdempotencyTTL == 0 {
		opts.IdempotencyTTL = 1 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationService{
		logRepo:      logRepo,
		tmplService:  tmplService,
		prefService:  prefService,
		unsubService: unsubService,
		resolver:     resolver,
		drivers:      drivers,
		logger:       logger,
		opts:         opts,
	}
}

// IsConfigured keeps its pre-ADR-0019 meaning: the default ("*") profile
// resolves and its driver accepts it. It is deliberately coarse — a caller
// about to send should ask IsConfiguredFor (PR 2, D7).
func (s *NotificationService) IsConfigured(ctx context.Context) bool {
	if s.resolver == nil || s.drivers == nil {
		return false
	}
	p, err := s.resolver.Default(ctx)
	if err != nil {
		return false
	}
	_, err = s.usableDriver(p)
	return err == nil
}

// IsConfiguredFor answers for one category: it resolves the profile that
// would carry the send and checks its driver with secrets in view — which
// the save-time gate cannot do. No match ⇒ false (fail-closed, D7).
func (s *NotificationService) IsConfiguredFor(ctx context.Context, category string) bool {
	if s.resolver == nil || s.drivers == nil {
		return false
	}
	// Same input the dispatch path builds — TenantID included — so the
	// pre-flight can never check a different profile than the send uses.
	tenantID, _ := ctxauth.GetTenantID(ctx)
	p, err := s.resolver.Resolve(ctx, ResolveInput{Category: category, TenantID: tenantID})
	if err != nil {
		return false
	}
	_, err = s.usableDriver(p)
	return err == nil
}

// usableDriver is the second and third fail-closed step: the profile's
// driver must be registered and must accept the profile with secrets in view.
func (s *NotificationService) usableDriver(p SenderProfile) (EmailDriver, error) {
	d, ok := s.drivers.Get(p.Provider)
	if !ok {
		return nil, ErrUnknownDriver
	}
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return nil, err
	}
	return d, nil
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

	// Resolve → validate → send. Every failure before the driver is
	// fail-closed (D5) and still writes a failed log row, so the delivery
	// log answers which profile failed and why.
	// TenantID rides to the resolver (D4) so per-tenant routing later needs
	// no change at this chokepoint; the resolver ignores it today.
	tenantID, _ := ctxauth.GetTenantID(ctx)
	profile, err := s.resolver.Resolve(ctx, ResolveInput{Category: in.Category, Type: in.Type, TenantID: tenantID})
	if err != nil {
		return s.failSend(ctx, logDoc, profile, err)
	}
	driver, err := s.usableDriver(profile)
	if err != nil {
		return s.failSend(ctx, logDoc, profile, err)
	}

	sendErr := driver.Send(ctx, profile, EmailMessage{
		To:       in.Recipient.Address,
		ToName:   in.Recipient.Name,
		Subject:  in.Subject,
		BodyText: in.BodyText,
		BodyHTML: in.BodyHTML,
		Category: in.Category,
	})
	if sendErr != nil {
		return s.failSend(ctx, logDoc, profile, sendErr)
	}

	now := time.Now()
	logDoc.Status = models.StatusSent
	logDoc.Provider = profile.Provider
	logDoc.SentAt = &now
	_ = s.logRepo.Create(ctx, logDoc)

	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: profile.Provider,
	}, nil
}

// ErrSendFailed: the driver refused or could not deliver. The reason is in
// the bounded diagnostic; the driver's own error is never in the chain.
var ErrSendFailed = errors.New("notification: send failed")

// trustedSentinels are the only errors a caller may test with errors.Is.
var trustedSentinels = []error{ErrNoSenderForCategory, ErrSenderConfigUnavailable, ErrSenderNotFound, ErrUnknownDriver, ErrSenderNotConfigured}

// DispatchError is the only error dispatchEmail and SendTest return. Error()
// is the sanitized reason (the same string the delivery log stores). Is
// answers true for ErrSendFailed on EVERY dispatch failure, and for the one
// trusted sentinel that names the cause (ErrNoSenderForCategory, …) when
// there is one — never for the raw driver error, which is dropped rather
// than wrapped: auth logs err.Error() of a failed send, so a fork driver's
// fmt.Errorf("vendor: %s", body) must not be reachable there.
type DispatchError struct {
	Reason   string
	sentinel error
}

func (e *DispatchError) Error() string { return e.Reason }

func (e *DispatchError) Is(target error) bool { return target == ErrSendFailed || e.sentinel == target }

func newDispatchError(profile SenderProfile, err error) *DispatchError {
	sentinel := ErrSendFailed
	for _, s := range trustedSentinels {
		if errors.Is(err, s) {
			sentinel = s
			break
		}
	}
	return &DispatchError{Reason: describeSendError(profile, err), sentinel: sentinel}
}

// failSend records a failed attempt with the bounded reason and returns the
// matching DispatchError. Nothing the driver wrote leaves this function.
func (s *NotificationService) failSend(ctx context.Context, logDoc *models.NotificationDoc, profile SenderProfile, err error) (*iface.NotificationResult, error) {
	de := newDispatchError(profile, err)
	logDoc.Status = models.StatusFailed
	logDoc.Provider = profile.Provider
	logDoc.Error = de.Reason
	_ = s.logRepo.Create(ctx, logDoc)
	s.logger.Warn("notification: send failed",
		slog.String("category", logDoc.Category),
		slog.String("reason", de.Reason),
	)
	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: profile.Provider,
		Error:    de.Reason,
	}, de
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

// Drivers exposes the driver registry (tests, admin diagnostics).
func (s *NotificationService) Drivers() *DriverRegistry { return s.drivers }

// Resolver exposes the sender resolver.
func (s *NotificationService) Resolver() SenderResolver { return s.resolver }

// TestSendInput is one operator-initiated test message.
type TestSendInput struct {
	To       string
	Subject  string
	BodyText string
}

// TestSendResult reports which profile carried (or refused) a test send.
// Diagnostic is the bounded reason on failure — safe to show an operator.
type TestSendResult struct {
	Provider   string
	SenderSlug string
	Diagnostic string
}

// SendTest sends through the default profile, bypassing preferences,
// idempotency and the delivery log exactly as the /test endpoint always has.
func (s *NotificationService) SendTest(ctx context.Context, in TestSendInput) (TestSendResult, error) {
	profile, err := s.resolver.Default(ctx)
	if err != nil {
		de := newDispatchError(profile, err)
		return TestSendResult{Diagnostic: de.Reason}, de
	}
	res := TestSendResult{Provider: profile.Provider, SenderSlug: profile.Slug}
	driver, err := s.usableDriver(profile)
	if err != nil {
		de := newDispatchError(profile, err)
		res.Diagnostic = de.Reason
		return res, de
	}
	if err := driver.Send(ctx, profile, EmailMessage{To: in.To, Subject: in.Subject, BodyText: in.BodyText}); err != nil {
		de := newDispatchError(profile, err)
		res.Diagnostic = de.Reason
		return res, de
	}
	return res, nil
}

// NormalizeAddress lowercases and trims an email address.
func NormalizeAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}
