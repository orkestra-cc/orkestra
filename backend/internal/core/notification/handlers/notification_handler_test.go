package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ---------------------------------------------------------------------------
// Minimal fakes for the handler integration test
// ---------------------------------------------------------------------------

type htNotifRepo struct{}

func (htNotifRepo) Create(_ context.Context, _ *models.NotificationDoc) error { return nil }
func (htNotifRepo) FindByIdempotencyKey(_ context.Context, _ string, _ time.Time) (*models.NotificationDoc, error) {
	return nil, nil
}
func (htNotifRepo) List(_ context.Context, _ repository.Filter, _ int64) ([]*models.NotificationDoc, error) {
	return nil, nil
}
func (htNotifRepo) GetByUUID(_ context.Context, _ string) (*models.NotificationDoc, error) {
	return nil, nil
}

type htTmplService struct{}

func (htTmplService) SeedDefaults(_ context.Context) error { return nil }
func (htTmplService) SeedModuleTemplates(_ context.Context, _ []module.NotificationTemplateSpec) error {
	return nil
}
func (htTmplService) Get(_ context.Context, _, _ string) (*models.TemplateDoc, error) {
	return nil, nil
}
func (htTmplService) List(_ context.Context) ([]*models.TemplateDoc, error) { return nil, nil }
func (htTmplService) Upsert(_ context.Context, _ *models.TemplateDoc) error { return nil }
func (htTmplService) Delete(_ context.Context, _, _ string) error           { return nil }
func (htTmplService) Render(_ *models.TemplateDoc, _ map[string]any) (*services.Rendered, error) {
	return &services.Rendered{}, nil
}

type htPrefService struct{}

func (htPrefService) CanDeliver(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}
func (htPrefService) List(_ context.Context, _ string) ([]*models.PreferenceDoc, error) {
	return nil, nil
}
func (htPrefService) Set(_ context.Context, _, _, _ string, _ bool) error { return nil }

// htDriver accepts everything, behind a resolver answering with one noop
// profile — the ADR-0019 shape that replaced the single email sender.
type htDriver struct{}

func (htDriver) Name() string                            { return "noop" }
func (htDriver) Requires() []services.ProfileRequirement { return nil }
func (htDriver) Send(context.Context, services.SenderProfile, services.EmailMessage) error {
	return nil
}
func (htDriver) Capabilities() services.DriverCapabilities {
	return services.DriverCapabilities{ListUnsubscribeHeaders: true}
}

func newHandlerTestSvc(unsubSvc services.UnsubscribeService) *services.NotificationService {
	return services.NewNotificationService(
		htNotifRepo{},
		htTmplService{},
		htPrefService{},
		unsubSvc,
		services.NewSenderResolver(func(context.Context) services.SenderConfig {
			return services.SenderConfig{Legacy: services.LegacyProfile(services.SenderProfile{Provider: "noop"})}
		}),
		services.NewDriverRegistry(htDriver{}),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		services.Options{},
	)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------
//
// The handler no longer orchestrates a read + PreferenceService.Set + a
// mark-used write + FireMarketingUnsubscribe itself — that sequence now lives
// in services.UnsubscribeService.Consume (see unsubscribe_service.go) and is
// exhaustively covered there (unsubscribe_consume_test.go). What belongs at
// this layer — that both endpoints call Consume, answer generically for
// every token state, and surface a real Consume failure rather than
// flattening it to success — is covered by unsubscribe_handler_test.go.

const hostileSecret = "s3cr=t hunter2"

// hostileDriver is what a careless fork driver looks like: it returns the
// vendor's response verbatim.
type hostileDriver struct{}

func (hostileDriver) Name() string                            { return "hostile" }
func (hostileDriver) Requires() []services.ProfileRequirement { return nil }
func (hostileDriver) Capabilities() services.DriverCapabilities {
	return services.DriverCapabilities{ListUnsubscribeHeaders: true}
}
func (hostileDriver) Send(context.Context, services.SenderProfile, services.EmailMessage) error {
	return fmt.Errorf("vendor response: 401 user=s12345_67 secret=%s <html>", hostileSecret)
}

func hostileService() *services.NotificationService {
	loader := func(context.Context) services.SenderConfig {
		return services.SenderConfig{Legacy: services.LegacyProfile(services.SenderProfile{Provider: "hostile"})}
	}
	return services.NewNotificationService(nil, nil, nil, nil,
		services.NewSenderResolver(loader), services.NewDriverRegistry(hostileDriver{}), nil, services.Options{})
}

// TestSendTestEmail_HostileDriverTextNeverReachesTheResponse: the HTTP
// detail, and every message huma attaches from the error chain, carry only
// the bounded reason.
func TestSendTestEmail_HostileDriverTextNeverReachesTheResponse(t *testing.T) {
	h := NewNotificationHandler(hostileService())
	req := &testEmailRequest{}
	req.Body.To = "a@example.com"
	_, err := h.SendTestEmail(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error")
	}
	var ee *errcode.Error
	if !errors.As(err, &ee) || ee.Code != errcode.NotificationSendFailed || ee.Status != http.StatusBadGateway {
		t.Fatalf("want 502 %s, got %v", errcode.NotificationSendFailed, err)
	}
	if ee.Detail != "The sender did not accept the test message: sender=_legacy err=unknown" {
		t.Fatalf("detail = %q", ee.Detail)
	}
	texts := []string{err.Error(), ee.Detail}
	var em *huma.ErrorModel
	if errors.As(err, &em) {
		texts = append(texts, em.Detail)
		for _, d := range em.Errors {
			texts = append(texts, d.Message)
		}
	}
	for _, text := range texts {
		if strings.Contains(text, hostileSecret) || strings.Contains(text, "<html>") || strings.Contains(text, "vendor response") {
			t.Fatalf("driver text reached the response: %q", text)
		}
	}
}
