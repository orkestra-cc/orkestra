package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// ---- Fakes --------------------------------------------------------------

type fakeNotifRepo struct {
	created     []*models.NotificationDoc
	existing    *models.NotificationDoc // returned by FindByIdempotencyKey
	findErr     error
	createErr   error
	findSinces  []time.Time
}

func newFakeNotifRepo() *fakeNotifRepo { return &fakeNotifRepo{} }

func (f *fakeNotifRepo) Create(_ context.Context, doc *models.NotificationDoc) error {
	if f.createErr != nil {
		return f.createErr
	}
	cp := *doc
	f.created = append(f.created, &cp)
	return nil
}

func (f *fakeNotifRepo) FindByIdempotencyKey(_ context.Context, key string, since time.Time) (*models.NotificationDoc, error) {
	f.findSinces = append(f.findSinces, since)
	if f.findErr != nil {
		return nil, f.findErr
	}
	if key == "" {
		return nil, nil
	}
	return f.existing, nil
}

func (f *fakeNotifRepo) List(_ context.Context, _ repository.Filter, _ int64) ([]*models.NotificationDoc, error) {
	return nil, nil
}

func (f *fakeNotifRepo) GetByUUID(_ context.Context, _ string) (*models.NotificationDoc, error) {
	return nil, nil
}

type fakeTemplateService struct {
	tmpl    *models.TemplateDoc
	getErr  error
	getCall struct {
		id, locale string
	}
	renderErr error
	rendered  *Rendered
}

func (f *fakeTemplateService) SeedDefaults(_ context.Context) error { return nil }

func (f *fakeTemplateService) Get(_ context.Context, id, locale string) (*models.TemplateDoc, error) {
	f.getCall.id = id
	f.getCall.locale = locale
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.tmpl, nil
}

func (f *fakeTemplateService) List(_ context.Context) ([]*models.TemplateDoc, error) { return nil, nil }
func (f *fakeTemplateService) Upsert(_ context.Context, _ *models.TemplateDoc) error { return nil }
func (f *fakeTemplateService) Delete(_ context.Context, _ string, _ string) error    { return nil }

func (f *fakeTemplateService) Render(_ *models.TemplateDoc, data map[string]any) (*Rendered, error) {
	if f.renderErr != nil {
		return nil, f.renderErr
	}
	if f.rendered != nil {
		// Capture the data the orchestrator passed in by embedding it in the subject for the assert.
		if v, ok := data["UnsubscribeURL"].(string); ok {
			f.rendered.Subject = "[unsub=" + v + "] " + f.rendered.Subject
		}
		return f.rendered, nil
	}
	return &Rendered{Subject: "S", BodyText: "B", BodyHTML: "<p>B</p>"}, nil
}

type fakePrefService struct {
	can      bool
	err      error
	calledN  int
}

func (f *fakePrefService) CanDeliver(_ context.Context, _, _, _, _ string) (bool, error) {
	f.calledN++
	if f.err != nil {
		return false, f.err
	}
	return f.can, nil
}

func (f *fakePrefService) List(_ context.Context, _ string) ([]*models.PreferenceDoc, error) {
	return nil, nil
}

func (f *fakePrefService) Set(_ context.Context, _, _, _ string, _ bool) error { return nil }

type fakeUnsubService struct {
	token      string
	tokenErr   error
	issueN     int
	lastUser   string
	lastAddr   string
	lastCateg  string
}

func (f *fakeUnsubService) IssueToken(_ context.Context, user, addr, category string) (string, error) {
	f.issueN++
	f.lastUser, f.lastAddr, f.lastCateg = user, addr, category
	if f.tokenErr != nil {
		return "", f.tokenErr
	}
	if f.token == "" {
		return "raw-token", nil
	}
	return f.token, nil
}

func (f *fakeUnsubService) ConsumeToken(_ context.Context, _ string) (*models.UnsubscribeTokenDoc, error) {
	return nil, nil
}

func (f *fakeUnsubService) MarkUsed(_ context.Context, _ string) error { return nil }

type fakeSender struct {
	configured bool
	provider   string
	sendErr    error
	sent       []EmailMessage
}

func (f *fakeSender) IsConfigured() bool   { return f.configured }
func (f *fakeSender) ProviderName() string { return f.provider }
func (f *fakeSender) Send(_ context.Context, msg EmailMessage) error {
	f.sent = append(f.sent, msg)
	return f.sendErr
}

// ---- helpers ------------------------------------------------------------

type kit struct {
	logRepo *fakeNotifRepo
	tmpl    *fakeTemplateService
	pref    *fakePrefService
	unsub   *fakeUnsubService
	email   *fakeSender
	svc     *NotificationService
}

func newKit(opts Options) *kit {
	k := &kit{
		logRepo: newFakeNotifRepo(),
		tmpl:    &fakeTemplateService{},
		pref:    &fakePrefService{can: true},
		unsub:   &fakeUnsubService{token: "raw-token"},
		email:   &fakeSender{configured: true, provider: "noop"},
	}
	k.svc = NewNotificationService(k.logRepo, k.tmpl, k.pref, k.unsub, k.email, discardLogger(), opts)
	return k
}

// ---- tests --------------------------------------------------------------

func TestNotificationService_Options_FillsDefaults(t *testing.T) {
	k := newKit(Options{})
	if k.svc.opts.DefaultLocale != "en" {
		t.Fatalf("DefaultLocale default = %q, want en", k.svc.opts.DefaultLocale)
	}
	if k.svc.opts.IdempotencyTTL != time.Hour {
		t.Fatalf("IdempotencyTTL default = %v, want 1h", k.svc.opts.IdempotencyTTL)
	}
}

func TestNotificationService_Options_PreservesProvided(t *testing.T) {
	k := newKit(Options{DefaultLocale: "it", IdempotencyTTL: 30 * time.Minute})
	if k.svc.opts.DefaultLocale != "it" {
		t.Fatalf("DefaultLocale = %q", k.svc.opts.DefaultLocale)
	}
	if k.svc.opts.IdempotencyTTL != 30*time.Minute {
		t.Fatalf("IdempotencyTTL = %v", k.svc.opts.IdempotencyTTL)
	}
}

func TestNotificationService_IsConfigured(t *testing.T) {
	k := newKit(Options{})
	if !k.svc.IsConfigured(context.Background()) {
		t.Fatalf("expected configured")
	}
	k.email.configured = false
	if k.svc.IsConfigured(context.Background()) {
		t.Fatalf("expected not configured")
	}

	// Nil email sender → never configured.
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{}, nil, discardLogger(), Options{})
	if svc.IsConfigured(context.Background()) {
		t.Fatalf("nil email sender should report not configured")
	}
}

func TestNotificationService_Send_DefaultsChannelToEmail(t *testing.T) {
	k := newKit(Options{})
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Type:       models.TypeTransactional,
		Category:   models.CategoryAuthVerifyEmail,
		Subject:    "S",
		Body:       "B",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q, want sent", res.Status)
	}
	if len(k.email.sent) != 1 {
		t.Fatalf("expected one email sent")
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].Channel != models.ChannelEmail {
		t.Fatalf("expected one log doc with email channel")
	}
}

func TestNotificationService_Send_UnsupportedChannel(t *testing.T) {
	k := newKit(Options{})
	_, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Channel:    "sms",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
	})
	if err == nil || !strings.Contains(err.Error(), "sms") {
		t.Fatalf("expected sms-not-supported error, got %v", err)
	}
}

func TestNotificationService_Send_NoRecipients(t *testing.T) {
	k := newKit(Options{})
	_, err := k.svc.Send(context.Background(), iface.NotificationRequest{Type: models.TypeTransactional})
	if err == nil || !strings.Contains(err.Error(), "no recipients") {
		t.Fatalf("expected no-recipients error, got %v", err)
	}
}

func TestNotificationService_Send_IdempotencyHitShortCircuits(t *testing.T) {
	k := newKit(Options{})
	k.logRepo.existing = &models.NotificationDoc{
		UUID:     "prev-uuid",
		Status:   models.StatusSent,
		Provider: "noop",
	}
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:           models.TypeTransactional,
		IdempotencyKey: "key-1",
		Recipients:     []iface.Recipient{{Address: "a@example.com"}},
		Subject:        "S",
		Body:           "B",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.ID != "prev-uuid" {
		t.Fatalf("expected prev-uuid, got %q", res.ID)
	}
	if len(k.email.sent) != 0 {
		t.Fatalf("idempotency hit should skip email send")
	}
	if len(k.logRepo.created) != 0 {
		t.Fatalf("idempotency hit should skip log create")
	}
}

func TestNotificationService_Send_IdempotencyWindowMatchesTTL(t *testing.T) {
	k := newKit(Options{IdempotencyTTL: 15 * time.Minute})
	_, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:           models.TypeTransactional,
		IdempotencyKey: "k",
		Recipients:     []iface.Recipient{{Address: "a@example.com"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(k.logRepo.findSinces) != 1 {
		t.Fatalf("expected one FindByIdempotencyKey call")
	}
	since := k.logRepo.findSinces[0]
	delta := time.Since(since)
	// since should be ~15m in the past; allow some slack for test latency.
	if delta < 14*time.Minute+50*time.Second || delta > 15*time.Minute+10*time.Second {
		t.Fatalf("idempotency window not derived from TTL, got delta=%v", delta)
	}
}

func TestNotificationService_Send_SuppressedByPreference(t *testing.T) {
	k := newKit(Options{})
	k.pref.can = false
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeMarketing,
		Recipients: []iface.Recipient{{Address: "a@example.com", UserUUID: "u1"}},
		Subject:    "S",
		Body:       "B",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSuppressed {
		t.Fatalf("status = %q, want suppressed", res.Status)
	}
	if len(k.email.sent) != 0 {
		t.Fatalf("suppressed mail must not call the sender")
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].Status != models.StatusSuppressed {
		t.Fatalf("expected one suppressed log doc")
	}
}

func TestNotificationService_Send_TransportFailureLogsAndReturnsError(t *testing.T) {
	k := newKit(Options{})
	k.email.sendErr = errors.New("smtp boom")
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
	})
	if err == nil || !strings.Contains(err.Error(), "smtp boom") {
		t.Fatalf("expected smtp error, got %v", err)
	}
	if res == nil || res.Status != models.StatusFailed {
		t.Fatalf("expected failed result, got %+v", res)
	}
	if res.Error != "smtp boom" {
		t.Fatalf("result.Error = %q", res.Error)
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].Status != models.StatusFailed {
		t.Fatalf("expected one failed log doc")
	}
}

func TestNotificationService_Send_SuccessLogsRecipientAndProvider(t *testing.T) {
	k := newKit(Options{})
	k.email.provider = "smtp"
	_, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com", UserUUID: "u-7", Name: "Alice"}},
		Subject:    "S",
		Body:       "B",
		BodyHTML:   "<p>B</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(k.email.sent) != 1 {
		t.Fatalf("expected one send")
	}
	msg := k.email.sent[0]
	if msg.To != "a@example.com" || msg.ToName != "Alice" {
		t.Fatalf("recipient passed incorrectly: %+v", msg)
	}
	if msg.Subject != "S" || msg.BodyText != "B" || msg.BodyHTML != "<p>B</p>" {
		t.Fatalf("subject/body not forwarded: %+v", msg)
	}
	doc := k.logRepo.created[0]
	if doc.Status != models.StatusSent || doc.Provider != "smtp" {
		t.Fatalf("log doc = %+v", doc)
	}
	if doc.RecipientUserUUID != "u-7" {
		t.Fatalf("log doc UserUUID = %q", doc.RecipientUserUUID)
	}
	if doc.SentAt == nil {
		t.Fatalf("expected SentAt to be stamped on success")
	}
}

func TestNotificationService_SendTemplated_HappyPath_AutoInjectsVariables(t *testing.T) {
	k := newKit(Options{
		AppName:      "Orkestra",
		SupportEmail: "support@example.com",
		URLBuilder:   func(p string) string { return "https://example.com" + p },
	})
	k.tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en", Subject: "subject"}
	k.tmpl.rendered = &Rendered{Subject: "rendered", BodyText: "txt", BodyHTML: "<p>html</p>"}
	k.unsub.token = "raw-XYZ"

	res, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Category:   models.CategoryAuthVerifyEmail,
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com", UserUUID: "u-1"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q", res.Status)
	}
	// Template lookup used the default locale.
	if k.tmpl.getCall.id != "tpl" || k.tmpl.getCall.locale != "en" {
		t.Fatalf("Get called with %+v", k.tmpl.getCall)
	}
	// Unsubscribe token was issued for the recipient + category.
	if k.unsub.issueN != 1 || k.unsub.lastAddr != "a@example.com" ||
		k.unsub.lastUser != "u-1" || k.unsub.lastCateg != models.CategoryAuthVerifyEmail {
		t.Fatalf("unsubscribe token issuance mismatch: %+v", k.unsub)
	}
	// Render saw an UnsubscribeURL built from the raw token and the URLBuilder.
	wantSubject := "[unsub=https://example.com/notifications/unsubscribe?token=raw-XYZ] rendered"
	if k.email.sent[0].Subject != wantSubject {
		t.Fatalf("Subject = %q, want %q", k.email.sent[0].Subject, wantSubject)
	}
}

func TestNotificationService_SendTemplated_RespectsExplicitLocale(t *testing.T) {
	k := newKit(Options{DefaultLocale: "en"})
	k.tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl"}
	_, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Locale:     "it",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	if k.tmpl.getCall.locale != "it" {
		t.Fatalf("expected locale=it, got %q", k.tmpl.getCall.locale)
	}
}

func TestNotificationService_SendTemplated_TemplateNotFound(t *testing.T) {
	k := newKit(Options{})
	k.tmpl.getErr = ErrTemplateNotFound
	_, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "missing",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
	})
	if err == nil || !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestNotificationService_SendTemplated_RenderError(t *testing.T) {
	k := newKit(Options{})
	k.tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl"}
	k.tmpl.renderErr = errors.New("render boom")
	_, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
	})
	if err == nil || !strings.Contains(err.Error(), "render boom") {
		t.Fatalf("expected render error, got %v", err)
	}
}

func TestNotificationService_SendTemplated_UnsupportedChannel(t *testing.T) {
	k := newKit(Options{})
	_, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		Channel:    "sms",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
	})
	if err == nil || !strings.Contains(err.Error(), "sms") {
		t.Fatalf("expected unsupported-channel error, got %v", err)
	}
}

func TestNotificationService_SendTemplated_NoRecipients(t *testing.T) {
	k := newKit(Options{})
	_, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
	})
	if err == nil || !strings.Contains(err.Error(), "no recipients") {
		t.Fatalf("expected no-recipients error, got %v", err)
	}
}

func TestNotificationService_SendTemplated_IdempotencyShortCircuit(t *testing.T) {
	k := newKit(Options{})
	k.logRepo.existing = &models.NotificationDoc{UUID: "prev", Status: models.StatusSent}
	res, err := k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID:     "tpl",
		IdempotencyKey: "kk",
		Type:           models.TypeTransactional,
		Recipients:     []iface.Recipient{{Address: "a@example.com"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	if res.ID != "prev" || len(k.email.sent) != 0 {
		t.Fatalf("idempotency hit should short-circuit, got res=%+v sent=%d", res, len(k.email.sent))
	}
}

func TestNotificationService_BuildURL_NoBuilderUsesRawPath(t *testing.T) {
	k := newKit(Options{}) // no URLBuilder
	got := k.svc.buildURL("/account/notifications")
	if got != "/account/notifications" {
		t.Fatalf("buildURL = %q, want raw path", got)
	}
}

func TestNotificationService_BuildURL_DelegatesToBuilder(t *testing.T) {
	k := newKit(Options{URLBuilder: func(p string) string { return "https://x" + p }})
	if got := k.svc.buildURL("/foo"); got != "https://x/foo" {
		t.Fatalf("buildURL = %q", got)
	}
}

func TestNormalizeAddress(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Alice@Example.COM", "alice@example.com"},
		{"  bob@example.com  ", "bob@example.com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeAddress(c.in); got != c.want {
			t.Fatalf("NormalizeAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNotificationService_Accessors(t *testing.T) {
	k := newKit(Options{})
	if k.svc.TemplateService() != k.tmpl {
		t.Fatalf("TemplateService() accessor mismatch")
	}
	if k.svc.PreferenceService() != k.pref {
		t.Fatalf("PreferenceService() accessor mismatch")
	}
	if k.svc.UnsubscribeService() != k.unsub {
		t.Fatalf("UnsubscribeService() accessor mismatch")
	}
	if k.svc.LogRepo() != k.logRepo {
		t.Fatalf("LogRepo() accessor mismatch")
	}
	if k.svc.EmailSender() != k.email {
		t.Fatalf("EmailSender() accessor mismatch")
	}
}
