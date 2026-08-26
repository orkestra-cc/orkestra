package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ---- Fakes --------------------------------------------------------------

type fakeNotifRepo struct {
	created    []*models.NotificationDoc
	existing   *models.NotificationDoc // returned by FindByIdempotencyKey
	findErr    error
	createErr  error
	findSinces []time.Time
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

func (f *fakeTemplateService) SeedModuleTemplates(_ context.Context, _ []module.NotificationTemplateSpec) error {
	return nil
}

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
	can     bool
	err     error
	calledN int
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
	token     string
	tokenErr  error
	issueN    int
	lastUser  string
	lastAddr  string
	lastCateg string
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

type fakeDriver struct {
	name     string
	requires []ProfileRequirement
	sendErr  error
	sent     []EmailMessage
	profiles []SenderProfile // the profile handed to each Send
}

func (f *fakeDriver) Name() string                   { return f.name }
func (f *fakeDriver) Requires() []ProfileRequirement { return f.requires }
func (f *fakeDriver) Send(_ context.Context, p SenderProfile, msg EmailMessage) error {
	f.sent = append(f.sent, msg)
	f.profiles = append(f.profiles, p)
	return f.sendErr
}

type fakeResolver struct {
	profile SenderProfile
	err     error
	inputs  []ResolveInput
}

// On error the fake returns the ZERO profile, as senderResolver does: a
// resolver that failed has no profile to name, and the chokepoint's
// diagnostic ("sender=-") depends on that.
func (f *fakeResolver) Resolve(_ context.Context, in ResolveInput) (SenderProfile, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return SenderProfile{}, f.err
	}
	return f.profile, nil
}

func (f *fakeResolver) Default(context.Context) (SenderProfile, error) {
	if f.err != nil {
		return SenderProfile{}, f.err
	}
	return f.profile, nil
}

func (f *fakeResolver) BySlug(_ context.Context, slug string) (SenderProfile, error) {
	if f.err != nil {
		return SenderProfile{}, f.err
	}
	if slug != f.profile.Slug {
		return SenderProfile{}, ErrSenderNotFound
	}
	return f.profile, nil
}

// ---- helpers ------------------------------------------------------------

type kit struct {
	logRepo  *fakeNotifRepo
	tmpl     *fakeTemplateService
	pref     *fakePrefService
	unsub    *fakeUnsubService
	driver   *fakeDriver
	resolver *fakeResolver
	drivers  *DriverRegistry
	svc      *NotificationService
}

func newKit(opts Options) *kit {
	k := &kit{
		logRepo:  newFakeNotifRepo(),
		tmpl:     &fakeTemplateService{},
		pref:     &fakePrefService{can: true},
		unsub:    &fakeUnsubService{token: "raw-token"},
		driver:   &fakeDriver{name: "noop"},
		resolver: &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}}},
	}
	k.rewire(opts)
	return k
}

// rewire rebuilds the registry and service after a test changed the fake
// driver's name or the resolver's profile.
func (k *kit) rewire(opts Options) {
	k.drivers = NewDriverRegistry(k.driver)
	k.svc = NewNotificationService(k.logRepo, k.tmpl, k.pref, k.unsub, k.resolver, k.drivers, discardLogger(), opts)
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
	k.resolver.err = ErrNoSenderForCategory
	if k.svc.IsConfigured(context.Background()) {
		t.Fatalf("expected not configured")
	}

	// Nil resolver / registry → never configured.
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{}, nil, nil, discardLogger(), Options{})
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
	if len(k.driver.sent) != 1 {
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
	if len(k.driver.sent) != 0 {
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
	if len(k.driver.sent) != 0 {
		t.Fatalf("suppressed mail must not call the sender")
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].Status != models.StatusSuppressed {
		t.Fatalf("expected one suppressed log doc")
	}
}

func TestNotificationService_Send_TransportFailureLogsAndReturnsError(t *testing.T) {
	k := newKit(Options{})
	boom := errors.New("smtp boom")
	k.driver.sendErr = boom
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
	})
	if !errors.Is(err, ErrSendFailed) || errors.Is(err, boom) {
		t.Fatalf("the caller gets ErrSendFailed and never the raw driver error, got %v", err)
	}
	if err.Error() != "sender=default err=unknown" {
		t.Fatalf("err.Error() must be the bounded reason (auth logs it), got %q", err.Error())
	}
	if res == nil || res.Status != models.StatusFailed {
		t.Fatalf("expected failed result, got %+v", res)
	}
	// An error of unknown shape is never persisted: only its kind is.
	if res.Error != "sender=default err=unknown" {
		t.Fatalf("result.Error = %q", res.Error)
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].Status != models.StatusFailed || k.logRepo.created[0].Error != "sender=default err=unknown" {
		t.Fatalf("expected one failed log doc with the bounded reason, got %+v", k.logRepo.created)
	}
}

func TestNotificationService_Send_SuccessLogsRecipientAndProvider(t *testing.T) {
	k := newKit(Options{})
	k.driver.name = "smtp"
	k.resolver.profile.Provider = "smtp"
	k.rewire(Options{})
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
	if len(k.driver.sent) != 1 {
		t.Fatalf("expected one send")
	}
	msg := k.driver.sent[0]
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
	if k.driver.sent[0].Subject != wantSubject {
		t.Fatalf("Subject = %q, want %q", k.driver.sent[0].Subject, wantSubject)
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
	if res.ID != "prev" || len(k.driver.sent) != 0 {
		t.Fatalf("idempotency hit should short-circuit, got res=%+v sent=%d", res, len(k.driver.sent))
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
	if k.svc.Drivers() != k.drivers {
		t.Fatalf("Drivers() accessor mismatch")
	}
}

func TestNotificationService_IsConfigured_DefaultProfileMustBeUsable(t *testing.T) {
	k := newKit(Options{})
	k.driver.requires = []ProfileRequirement{{Key: SubSMTPHost}}
	if k.svc.IsConfigured(context.Background()) {
		t.Fatal("a default profile missing a required field must not report configured")
	}
	k.resolver.profile.SMTPHost = "h"
	if !k.svc.IsConfigured(context.Background()) {
		t.Fatal("a complete default profile must report configured")
	}
	k.resolver.profile.Provider = "ses" // no such driver registered
	if k.svc.IsConfigured(context.Background()) {
		t.Fatal("an unregistered driver must not report configured")
	}
}

func sendOne(t *testing.T, k *kit) (*iface.NotificationResult, error) {
	t.Helper()
	return k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Category:   "crm.campaign",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
	})
}

func TestNotificationService_Dispatch_FailClosedPaths(t *testing.T) {
	cases := []struct {
		name      string
		arrange   func(k *kit)
		wantErr   error
		wantError string
		wantProv  string
	}{
		{"no sender for category", func(k *kit) { k.resolver.err = ErrNoSenderForCategory },
			ErrNoSenderForCategory, "sender=- err=no_sender_for_category", ""},
		{"config unavailable", func(k *kit) { k.resolver.err = ErrSenderConfigUnavailable },
			ErrSenderConfigUnavailable, "sender=- err=config_unavailable", ""},
		{"unknown driver", func(k *kit) { k.resolver.profile.Provider = "ses" },
			ErrUnknownDriver, "sender=default driver=ses err=unknown_driver", "ses"},
		{"incomplete profile", func(k *kit) { k.driver.requires = []ProfileRequirement{{Key: SubSMTPHost}, {Key: SubFromAddress}} },
			ErrSenderNotConfigured, "sender=default driver=noop err=not_configured missing=smtp_host,from_address", "noop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := newKit(Options{})
			c.arrange(k)
			res, err := sendOne(t, k)
			if !errors.Is(err, c.wantErr) || !errors.Is(err, ErrSendFailed) {
				t.Fatalf("err = %v, want %v and the ErrSendFailed umbrella", err, c.wantErr)
			}
			if res == nil || res.Status != models.StatusFailed || res.Error != c.wantError || res.Provider != c.wantProv {
				t.Fatalf("res = %+v", res)
			}
			if len(k.driver.sent) != 0 {
				t.Fatal("a fail-closed path must never reach the driver")
			}
			if len(k.logRepo.created) != 1 || k.logRepo.created[0].Error != c.wantError || k.logRepo.created[0].Status != models.StatusFailed {
				t.Fatalf("every fail-closed path writes a failed log row naming the reason: %+v", k.logRepo.created)
			}
		})
	}
}

func TestNotificationService_Dispatch_CarriesCategoryAndResolveInput(t *testing.T) {
	k := newKit(Options{})
	if _, err := sendOne(t, k); err != nil {
		t.Fatal(err)
	}
	if len(k.driver.sent) != 1 || k.driver.sent[0].Category != "crm.campaign" {
		t.Fatalf("Category must ride on EmailMessage: %+v", k.driver.sent)
	}
	if len(k.resolver.inputs) != 1 || k.resolver.inputs[0].Category != "crm.campaign" || k.resolver.inputs[0].Type != models.TypeTransactional {
		t.Fatalf("resolver input = %+v", k.resolver.inputs)
	}
	if k.resolver.inputs[0].TenantID != "" {
		t.Fatalf("no tenant in ctx ⇒ empty TenantID, got %q", k.resolver.inputs[0].TenantID)
	}

	// D4: the tenant on the request context reaches the resolver unchanged.
	ctx := context.WithValue(context.Background(), ctxauth.KeyTenantID, "t-42")
	if _, err := k.svc.Send(ctx, iface.NotificationRequest{Type: models.TypeTransactional, Category: "crm.campaign",
		Recipients: []iface.Recipient{{Address: "b@example.com"}}, Subject: "S", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	if k.resolver.inputs[1].TenantID != "t-42" {
		t.Fatalf("TenantID must be propagated from ctx, got %q", k.resolver.inputs[1].TenantID)
	}
	if k.driver.profiles[0].Slug != "default" {
		t.Fatalf("driver must receive the resolved profile, got %+v", k.driver.profiles[0])
	}
	if k.logRepo.created[0].Provider != "noop" {
		t.Fatalf("log row provider = %q", k.logRepo.created[0].Provider)
	}
}

// TestNotificationService_Dispatch_HostileDriverErrorNeverEscapes is the
// end-to-end containment test: a driver (a fork's, say) that returns a raw
// error carrying a vendor body must leave no trace of it in the stored row,
// in the result, or in the error a caller receives and logs.
func TestNotificationService_Dispatch_HostileDriverErrorNeverEscapes(t *testing.T) {
	const secret = "s3cr=t hunter2"
	k := newKit(Options{})
	k.driver.sendErr = fmt.Errorf("vendor response: 401 user=s12345_67 secret=%s <html>", secret)
	res, err := sendOne(t, k)
	for what, text := range map[string]string{
		"stored row":     k.logRepo.created[0].Error,
		"result.Error":   res.Error,
		"returned error": err.Error(),
	} {
		if strings.Contains(text, secret) || strings.Contains(text, "vendor response") || strings.Contains(text, "<html>") {
			t.Fatalf("%s leaked the driver's text: %q", what, text)
		}
		if text != "sender=default err=unknown" {
			t.Fatalf("%s = %q", what, text)
		}
	}
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	var de *DispatchError
	if !errors.As(err, &de) {
		t.Fatalf("the chokepoint returns only *DispatchError, got %T", err)
	}
}

func TestNotificationService_Dispatch_DriverDiagnosticIsStored(t *testing.T) {
	k := newKit(Options{})
	k.driver.sendErr = rejectionError("smtp", smtpOpAuth, 535, errors.New("535 AHVzZXIAcGFzcw=="))
	res, _ := sendOne(t, k)
	if res.Error != "sender=default smtp op=auth code=535" {
		t.Fatalf("res.Error = %q", res.Error)
	}
}

func TestNotificationService_SendTest(t *testing.T) {
	k := newKit(Options{})
	res, err := k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com", Subject: "T", BodyText: "B"})
	if err != nil || res.Provider != "noop" || res.SenderSlug != "default" {
		t.Fatalf("SendTest = %+v, %v", res, err)
	}
	if len(k.driver.sent) != 1 || k.driver.sent[0].To != "a@example.com" || k.driver.sent[0].Category != "" {
		t.Fatalf("test send not delivered to the driver: %+v", k.driver.sent)
	}
	if len(k.logRepo.created) != 0 {
		t.Fatal("a test send must not write a delivery-log row (unchanged from today)")
	}

	k.driver.sendErr = errors.New("boom secret=hunter2")
	res, err = k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com"})
	if err == nil || res.Diagnostic != "sender=default err=unknown" || res.Provider != "noop" {
		t.Fatalf("SendTest failure = %+v, %v", res, err)
	}
	if err.Error() != res.Diagnostic || !errors.Is(err, ErrSendFailed) {
		t.Fatalf("SendTest must return the sanitized DispatchError, got %v", err)
	}
}
