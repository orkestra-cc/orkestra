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

type htUnsubService struct {
	doc    *models.UnsubscribeTokenDoc
	docErr error
}

func (f *htUnsubService) IssueToken(_ context.Context, _, _, _, _ string) (string, error) {
	return "raw", nil
}
func (f *htUnsubService) ConsumeToken(_ context.Context, _ string) (*models.UnsubscribeTokenDoc, error) {
	return f.doc, f.docErr
}
func (f *htUnsubService) MarkUsed(_ context.Context, _ string) error { return nil }

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

// htCapturingSink records the arguments passed to OnMarketingUnsubscribe.
type htCapturingSink struct {
	addr, cat, refCtx string
	n                 int
	err               error
}

func (c *htCapturingSink) OnMarketingUnsubscribe(_ context.Context, a, cat, cx string) error {
	c.addr, c.cat, c.refCtx, c.n = a, cat, cx, c.n+1
	return c.err
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

func TestUnsubscribeHandler_FiresSink(t *testing.T) {
	unsubSvc := &htUnsubService{
		doc: &models.UnsubscribeTokenDoc{
			Address:  "x@y.com",
			Category: "marketing",
			Context:  "ref-9",
		},
	}
	svc := newHandlerTestSvc(unsubSvc)
	sink := &htCapturingSink{}
	svc.SetMarketingUnsubscribeSink(sink)
	h := NewNotificationHandler(svc)

	_, err := h.Unsubscribe(context.Background(), &unsubscribeRequest{Token: "any-token"})
	if err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if sink.n != 1 {
		t.Fatalf("sink called %d times, want 1", sink.n)
	}
	if sink.addr != "x@y.com" {
		t.Fatalf("addr = %q, want x@y.com", sink.addr)
	}
	if sink.cat != "marketing" {
		t.Fatalf("cat = %q, want marketing", sink.cat)
	}
	if sink.refCtx != "ref-9" {
		t.Fatalf("refCtx = %q, want ref-9", sink.refCtx)
	}
}

func TestUnsubscribeHandler_NilSink_IsNoOp(t *testing.T) {
	// No sink wired — Unsubscribe must complete without panic.
	unsubSvc := &htUnsubService{
		doc: &models.UnsubscribeTokenDoc{Address: "a@b.com", Category: "marketing"},
	}
	svc := newHandlerTestSvc(unsubSvc)
	h := NewNotificationHandler(svc)
	if _, err := h.Unsubscribe(context.Background(), &unsubscribeRequest{Token: "tok"}); err != nil {
		t.Fatalf("Unsubscribe with nil sink: %v", err)
	}
}

func TestUnsubscribeHandler_SinkFailureDoesNotFailTheRecipient(t *testing.T) {
	// A consumer that is down is not the recipient's problem: the answer
	// stays the same generic success. What the failure must NOT do is
	// disappear — that is what the sink's error return is for, and what the
	// consume sequence acts on.
	unsubSvc := &htUnsubService{
		doc: &models.UnsubscribeTokenDoc{Address: "a@b.com", Category: "marketing"},
	}
	svc := newHandlerTestSvc(unsubSvc)
	sink := &htCapturingSink{err: errors.New("sink down")}
	svc.SetMarketingUnsubscribeSink(sink)
	h := NewNotificationHandler(svc)

	if _, err := h.Unsubscribe(context.Background(), &unsubscribeRequest{Token: "tok"}); err != nil {
		t.Fatalf("a failing sink must not fail the unsubscribe: %v", err)
	}
	if sink.n != 1 {
		t.Fatalf("sink called %d times, want 1", sink.n)
	}
}

func TestUnsubscribeHandler_DefaultsEmptyCategoryToMarketing(t *testing.T) {
	// If the token doc carries an empty Category, the handler defaults to
	// "marketing" and the sink must receive that defaulted value.
	unsubSvc := &htUnsubService{
		doc: &models.UnsubscribeTokenDoc{
			Address:  "z@z.com",
			Category: "", // intentionally empty
			Context:  "ctx-42",
		},
	}
	svc := newHandlerTestSvc(unsubSvc)
	sink := &htCapturingSink{}
	svc.SetMarketingUnsubscribeSink(sink)
	h := NewNotificationHandler(svc)

	if _, err := h.Unsubscribe(context.Background(), &unsubscribeRequest{Token: "tok2"}); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if sink.cat != "marketing" {
		t.Fatalf("cat = %q after default, want marketing", sink.cat)
	}
	if sink.refCtx != "ctx-42" {
		t.Fatalf("refCtx = %q, want ctx-42", sink.refCtx)
	}
}

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
