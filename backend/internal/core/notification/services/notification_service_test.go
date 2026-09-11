package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// store is an in-memory map populated by Upsert and read by Get when
	// neither tmpl nor getErr is explicitly set. Key: templateID+"/"+locale.
	store   map[string]*models.TemplateDoc
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
	if f.tmpl != nil {
		return f.tmpl, nil
	}
	// When the store is initialised (at least one Upsert happened), look up the key
	// and return ErrTemplateNotFound on miss. When store is still nil (nothing ever
	// upserted), also return ErrTemplateNotFound — callers that want a fake "found"
	// result should set f.tmpl directly.
	if f.store != nil {
		if doc, ok := f.store[id+"/"+locale]; ok {
			return doc, nil
		}
	}
	return nil, ErrTemplateNotFound
}

func (f *fakeTemplateService) List(_ context.Context) ([]*models.TemplateDoc, error) { return nil, nil }

func (f *fakeTemplateService) Upsert(_ context.Context, doc *models.TemplateDoc) error {
	if f.store == nil {
		f.store = make(map[string]*models.TemplateDoc)
	}
	cp := *doc
	f.store[cp.TemplateID+"/"+cp.Locale] = &cp
	return nil
}

func (f *fakeTemplateService) Delete(_ context.Context, _ string, _ string) error { return nil }

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
	token       string
	tokenErr    error
	issueN      int
	lastUser    string
	lastAddr    string
	lastCateg   string
	lastContext string // the opaque producer-context 5th argument to IssueToken
}

func (f *fakeUnsubService) IssueToken(_ context.Context, user, addr, category, ctxArg string) (string, error) {
	f.issueN++
	f.lastUser, f.lastAddr, f.lastCateg, f.lastContext = user, addr, category, ctxArg
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

func (f *fakeUnsubService) Consume(_ context.Context, _ string) error { return nil }

type fakeDriver struct {
	name     string
	requires []ProfileRequirement
	sendErr  error
	sent     []EmailMessage
	profiles []SenderProfile // the profile handed to each Send
	sends    int             // count-only convenience alongside sent, for tests that just assert "did it reach the driver"
}

func (f *fakeDriver) Name() string                   { return f.name }
func (f *fakeDriver) Requires() []ProfileRequirement { return f.requires }
func (f *fakeDriver) Capabilities() DriverCapabilities {
	return DriverCapabilities{ListUnsubscribeHeaders: true}
}
func (f *fakeDriver) Send(_ context.Context, p SenderProfile, msg EmailMessage) error {
	f.sends++
	f.sent = append(f.sent, msg)
	f.profiles = append(f.profiles, p)
	return f.sendErr
}

type fakeResolver struct {
	profile     SenderProfile
	err         error
	inputs      []ResolveInput
	bySlugCalls int             // records lookups so a test can assert the guard short-circuited
	all         []SenderProfile // All's return value when set; nil falls back to [profile]
	allCalls    int
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
	f.bySlugCalls++
	if f.err != nil {
		return SenderProfile{}, f.err
	}
	if slug != f.profile.Slug {
		return SenderProfile{}, ErrSenderNotFound
	}
	return f.profile, nil
}

// All defaults to [f.profile] — a single-profile roster is what every
// existing kit fixture already models. Tests exercising ListEligibleSenders
// over a multi-profile roster set f.all directly.
func (f *fakeResolver) All(context.Context) ([]SenderProfile, error) {
	f.allCalls++
	if f.err != nil {
		return nil, f.err
	}
	if f.all != nil {
		return f.all, nil
	}
	return []SenderProfile{f.profile}, nil
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

func TestNotificationService_IsConfiguredFor(t *testing.T) {
	auth := &fakeDriver{name: "smtp", requires: []ProfileRequirement{{Key: SubSMTPHost}}}
	def := &fakeDriver{name: "noop"}
	cfg := SenderConfig{Profiles: []SenderProfile{
		{Slug: "default", Provider: "noop", Categories: []string{"*"}},
		{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}}, // broken: no host
	}}
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		NewSenderResolver(fixedLoader(cfg)), NewDriverRegistry(auth, def), discardLogger(), Options{})
	ctx := context.Background()

	// Today's global boolean gets both of these wrong.
	if svc.IsConfiguredFor(ctx, "auth.verify_email") {
		t.Fatal("valid default + broken auth.* must be false for auth.*")
	}
	if !svc.IsConfiguredFor(ctx, "crm.campaign") || !svc.IsConfigured(ctx) {
		t.Fatal("the default profile is fine")
	}

	// Secret-only gap: invisible to ValidateConfig, caught here.
	secretDriver := &fakeDriver{name: "vendor", requires: []ProfileRequirement{{Key: SubSMTPPassword, Secret: true}}}
	cfg2 := SenderConfig{Profiles: []SenderProfile{{Slug: "v", Provider: "vendor", Categories: []string{"*"}}}}
	svc2 := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		NewSenderResolver(fixedLoader(cfg2)), NewDriverRegistry(secretDriver), discardLogger(), Options{})
	if svc2.IsConfiguredFor(ctx, "anything") {
		t.Fatal("a profile missing only its secret must be reported unconfigured at request time")
	}

	// Malformed category ⇒ false, even with a "*" profile.
	if svc.IsConfiguredFor(ctx, "") || svc.IsConfiguredFor(ctx, " crm.campaign") {
		t.Fatal("an empty or untrimmed category must not ride the default")
	}

	// D4: the pre-flight hands the resolver the same TenantID the dispatch does.
	k := newKit(Options{})
	tctx := context.WithValue(context.Background(), ctxauth.KeyTenantID, "t-42")
	if !k.svc.IsConfiguredFor(tctx, "crm.campaign") {
		t.Fatal("fake default profile is usable")
	}
	if len(k.resolver.inputs) != 1 || k.resolver.inputs[0].TenantID != "t-42" || k.resolver.inputs[0].Category != "crm.campaign" {
		t.Fatalf("pre-flight resolver input = %+v", k.resolver.inputs)
	}

	// Unmatched category ⇒ false.
	cfg3 := SenderConfig{Profiles: []SenderProfile{{Slug: "auth", Provider: "noop", Categories: []string{"auth.*"}}}}
	svc3 := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		NewSenderResolver(fixedLoader(cfg3)), NewDriverRegistry(def), discardLogger(), Options{})
	if svc3.IsConfiguredFor(ctx, "crm.campaign") {
		t.Fatal("no match must be false")
	}
	var _ iface.CategoryConfiguredChecker = svc3
}

func TestNotificationService_SendTest_ExplicitSender(t *testing.T) {
	k := newKit(Options{})
	res, err := k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com", Sender: "default"})
	if err != nil || res.SenderSlug != "default" || len(k.driver.sent) != 1 {
		t.Fatalf("explicit default: %+v %v", res, err)
	}
	if _, err := k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com", Sender: "nope"}); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("unknown slug must be ErrSenderNotFound, got %v", err)
	}
	if len(k.driver.sent) != 1 {
		t.Fatal("nothing must be sent for an unknown slug")
	}
}

func TestNotificationService_Dispatch_StampsSenderSlug(t *testing.T) {
	k := newKit(Options{})
	k.resolver.profile.Slug = "esp-campagne"
	if _, err := sendOne(t, k); err != nil {
		t.Fatal(err)
	}
	if k.logRepo.created[0].SenderSlug != "esp-campagne" {
		t.Fatalf("sent row must carry the sender slug: %+v", k.logRepo.created[0])
	}
	k.driver.sendErr = errors.New("boom")
	_, _ = sendOne(t, k)
	if k.logRepo.created[1].SenderSlug != "esp-campagne" || !strings.HasPrefix(k.logRepo.created[1].Error, "sender=esp-campagne ") {
		t.Fatalf("failed row must carry the slug in both the field and the reason: %+v", k.logRepo.created[1])
	}
	k.resolver.err = ErrNoSenderForCategory
	_, _ = sendOne(t, k)
	if k.logRepo.created[2].SenderSlug != "" {
		t.Fatalf("no resolved profile ⇒ no slug: %+v", k.logRepo.created[2])
	}
}

// seamSvc builds a service with one noop driver and a fixed default profile —
// the shape these extension-seam tests need now that ADR-0019 replaced the
// single EmailSender with a resolver plus a driver registry.
// The marketing sends below are about the tracking rewriter, so the service
// is given a base URL the one-click unsubscribe header can be built on —
// satisfying the requirement rather than waiving it, which would make these
// tests silently stop covering the configured path.
func seamSvc() (*NotificationService, *fakeDriver) {
	d := &fakeDriver{name: "noop"}
	r := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}}}
	return NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{}, r, NewDriverRegistry(d), discardLogger(), Options{PublicAPIBaseURL: "https://api.example"}), d
}

// lastBodyHTML returns the BodyHTML of the last email handed to the driver,
// or "" if nothing was sent yet.
func (f *fakeDriver) lastBodyHTML() string {
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1].BodyHTML
}

// rewriterFunc adapts a func to iface.EmailTrackingRewriter for tests.
type rewriterFunc func(ctx context.Context, in iface.OutboundEmail) string

func (f rewriterFunc) RewriteOutboundEmail(ctx context.Context, in iface.OutboundEmail) string {
	return f(ctx, in)
}

func TestDispatchEmailAppliesRewriterWhenRefSet(t *testing.T) {
	svc, sender := seamSvc()
	svc.SetOptouts(&fakeOptouts{}) // neutral: nobody has opted out — this test is about the rewriter, not opt-outs
	svc.SetEmailTrackingRewriter(rewriterFunc(func(_ context.Context, in iface.OutboundEmail) string {
		if in.ContactRef == "" {
			return in.BodyHTML
		}
		return in.BodyHTML + "<!--rw:" + in.ContactRef + "-->"
	}))
	_, err := svc.Send(context.Background(), iface.NotificationRequest{
		Type: "marketing", Recipients: []iface.Recipient{{Address: "a@b.c"}},
		Subject: "s", BodyHTML: "<p>hi</p>", TrackingContactRef: "ref-1",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	got := sender.lastBodyHTML()
	if !strings.Contains(got, "rw:ref-1") {
		t.Fatalf("rewriter not applied: %q", got)
	}
}

func TestDispatchEmailUnchangedWhenNoRewriterOrNoRef(t *testing.T) {
	// no rewriter set
	svc, sender := seamSvc()
	svc.SetOptouts(&fakeOptouts{}) // neutral: nobody has opted out — this test is about the rewriter, not opt-outs
	_, _ = svc.Send(context.Background(), iface.NotificationRequest{Type: "marketing", Recipients: []iface.Recipient{{Address: "a@b.c"}}, BodyHTML: "<p>hi</p>", TrackingContactRef: "ref-1"})
	if got := sender.lastBodyHTML(); got != "<p>hi</p>" {
		t.Fatalf("no rewriter → unchanged; got %q", got)
	}
	// rewriter set but empty ref
	svc2, sender2 := seamSvc()
	svc2.SetOptouts(&fakeOptouts{}) // neutral: nobody has opted out — this test is about the rewriter, not opt-outs
	svc2.SetEmailTrackingRewriter(rewriterFunc(func(_ context.Context, in iface.OutboundEmail) string { return "MUTATED" }))
	_, _ = svc2.Send(context.Background(), iface.NotificationRequest{Type: "marketing", Recipients: []iface.Recipient{{Address: "a@b.c"}}, BodyHTML: "<p>hi</p>"})
	if got := sender2.lastBodyHTML(); got != "<p>hi</p>" {
		t.Fatalf("empty ref → unchanged; got %q", got)
	}
}

func TestDispatchEmailRewriterPanicIsSafe(t *testing.T) {
	svc, sender := seamSvc()
	svc.SetOptouts(&fakeOptouts{}) // neutral: nobody has opted out — this test is about the rewriter, not opt-outs
	svc.SetEmailTrackingRewriter(rewriterFunc(func(_ context.Context, _ iface.OutboundEmail) string { panic("boom") }))
	if _, err := svc.Send(context.Background(), iface.NotificationRequest{Type: "marketing", Recipients: []iface.Recipient{{Address: "a@b.c"}}, BodyHTML: "<p>hi</p>", TrackingContactRef: "ref-1"}); err != nil {
		t.Fatalf("panic must not fail the send: %v", err)
	}
	if got := sender.lastBodyHTML(); got != "<p>hi</p>" {
		t.Fatalf("panic → original body; got %q", got)
	}
}

// ---------------------------------------------------------------------------
// MarketingUnsubscribeSink tests
// ---------------------------------------------------------------------------

type capturingSink struct {
	addr, cat, refCtx string
	n                 int
	err               error
}

func (c *capturingSink) OnMarketingUnsubscribe(_ context.Context, a, cat, cx string) error {
	c.addr, c.cat, c.refCtx, c.n = a, cat, cx, c.n+1
	return c.err
}

func TestFireMarketingUnsubscribe_SinkReceivesArgs(t *testing.T) {
	svc, _ := seamSvc()
	sink := &capturingSink{}
	svc.SetMarketingUnsubscribeSink(sink)
	if err := svc.FireMarketingUnsubscribe(context.Background(), "x@y.com", "marketing", "ref-9"); err != nil {
		t.Fatalf("FireMarketingUnsubscribe: %v", err)
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

func TestFireMarketingUnsubscribe_NilSink_IsNoOp(t *testing.T) {
	svc, _ := seamSvc()
	// No sink set. That is not a failure: the base ships no sink, and
	// "nothing to mirror" must not read as "the mirror failed", or the
	// caller would keep an opt-out marked pending forever.
	if err := svc.FireMarketingUnsubscribe(context.Background(), "x@y.com", "marketing", "ref-9"); err != nil {
		t.Fatalf("a nil sink is not a failure, got %v", err)
	}
}

func TestFireMarketingUnsubscribe_SinkErrorReachesTheCaller(t *testing.T) {
	// The whole point of the error return: the caller decides whether the
	// opt-out still has to be replayed downstream, so a failure it never
	// hears about is the one bug this signature exists to prevent.
	svc, _ := seamSvc()
	boom := errors.New("sink down")
	svc.SetMarketingUnsubscribeSink(&capturingSink{err: boom})
	err := svc.FireMarketingUnsubscribe(context.Background(), "x@y.com", "marketing", "ref-9")
	if !errors.Is(err, boom) {
		t.Fatalf("want the sink's error, got %v", err)
	}
}

func TestFireMarketingUnsubscribe_PanicSink_BecomesAnError(t *testing.T) {
	// Recovered, so one bad sink cannot take the request down — but
	// reported, because a swallowed panic is an unmirrored opt-out nobody
	// ever retries.
	svc, _ := seamSvc()
	svc.SetMarketingUnsubscribeSink(panicSink{})
	if err := svc.FireMarketingUnsubscribe(context.Background(), "x@y.com", "marketing", "ref-9"); err == nil {
		t.Fatal("a panicking sink must surface as an error, not as success")
	}
}

type panicSink struct{}

func (panicSink) OnMarketingUnsubscribe(_ context.Context, _, _, _ string) error { panic("sink boom") }

// addressPanicSink panics with the address core just handed it — the shape a
// real sink's crash takes when it interpolates what it was working on.
type addressPanicSink struct{}

func (addressPanicSink) OnMarketingUnsubscribe(_ context.Context, address, _, _ string) error {
	panic("contact sync exploded for " + address)
}

func TestFireMarketingUnsubscribe_PanicValueIsScrubbedOfTheAddress(t *testing.T) {
	// The recovered value goes to two places — a log line and the error the
	// caller then logs in turn — so a panic carrying the address would leak
	// it twice over. "Never log a full address" has no best-effort tier.
	var buf bytes.Buffer
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{},
		&fakeResolver{profile: SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}}},
		NewDriverRegistry(&fakeDriver{name: "noop"}),
		slog.New(slog.NewTextHandler(&buf, nil)), Options{})
	svc.SetMarketingUnsubscribeSink(addressPanicSink{})

	err := svc.FireMarketingUnsubscribe(context.Background(), "ada@example.test", "marketing", "ref-9")
	if err == nil {
		t.Fatal("a panicking sink must surface as an error")
	}
	if strings.Contains(err.Error(), "ada@example.test") {
		t.Fatalf("the address must not ride out in the error: %v", err)
	}
	if strings.Contains(buf.String(), "ada@example.test") {
		t.Fatalf("the address must not reach the log: %s", buf.String())
	}
	if !strings.Contains(err.Error(), "[address]") {
		t.Fatalf("the panic value should still be reported, scrubbed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Template read/write capability tests
// ---------------------------------------------------------------------------

// newTestNotificationService returns a *NotificationService wired with the
// in-memory fakes (store-backed fakeTemplateService) for testing
// UpsertTemplate / GetTemplate.
func newTestNotificationService(t *testing.T) *NotificationService {
	t.Helper()
	k := newKit(Options{DefaultLocale: "it"})
	return k.svc
}

func TestTemplatePortRoundTrip(t *testing.T) {
	svc := newTestNotificationService(t)
	if err := svc.UpsertTemplate(context.Background(), "campaign:x", "it", "Ciao {{.firstName}}", "<p>Hi</p>", "Hi"); err != nil {
		t.Fatal(err)
	}
	v, err := svc.GetTemplate(context.Background(), "campaign:x", "it")
	if err != nil || v.Subject != "Ciao {{.firstName}}" || v.BodyHTML != "<p>Hi</p>" {
		t.Fatalf("got %+v err %v", v, err)
	}
}

func TestTemplatePortGetNotFound(t *testing.T) {
	svc := newTestNotificationService(t)
	_, err := svc.GetTemplate(context.Background(), "campaign:missing", "it")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestTemplatePortLocaleDefault(t *testing.T) {
	// Upsert with empty locale → defaults to svc's DefaultLocale ("it").
	svc := newTestNotificationService(t)
	if err := svc.UpsertTemplate(context.Background(), "campaign:y", "", "Subj", "<p>body</p>", "body"); err != nil {
		t.Fatal(err)
	}
	// Get with empty locale should also resolve to "it".
	v, err := svc.GetTemplate(context.Background(), "campaign:y", "")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if v.Locale != "it" {
		t.Fatalf("Locale = %q, want it", v.Locale)
	}
}

// ---- ADR-0021: explicit Sender at dispatch --------------------------------

// fullyPopulatedProfile returns a profile with every transport field set to
// a distinctive, greppable sentinel value, so a test asserting that a
// persisted diagnostic never leaks transport identity has something real to
// catch.
func fullyPopulatedProfile(slug string, allowedTypes []string) SenderProfile {
	return SenderProfile{
		Slug:         slug,
		Label:        "sentinel-label-zz9",
		Provider:     "smtp",
		Categories:   []string{"*"},
		AllowedTypes: allowedTypes,
		FromAddress:  "sentinel-from-zz9@example.com",
		FromName:     "sentinel-fromname-zz9",
		ReplyTo:      "sentinel-replyto-zz9@example.com",
		SMTPHost:     "sentinel-host-zz9.example.net",
		SMTPPort:     2525,
		SMTPUsername: "sentinel-username-zz9",
		SMTPPassword: "sentinel-password-zz9",
		SMTPTLSMode:  "starttls",
		MailUpUser:   "sentinel-mailupuser-zz9",
		MailUpSecret: "sentinel-mailupsecret-zz9",
	}
}

func TestNotificationService_Dispatch_ExplicitSender_EligibleSlugSends(t *testing.T) {
	k := newKit(Options{})
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", Categories: []string{"*"}, AllowedTypes: []string{models.TypeTransactional}}
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Category:   "crm.campaign",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
		Sender:     "camp",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q, want sent", res.Status)
	}
	if len(k.logRepo.created) != 1 {
		t.Fatalf("expected one log row, got %d", len(k.logRepo.created))
	}
	doc := k.logRepo.created[0]
	if doc.SenderSlug != "camp" {
		t.Fatalf("SenderSlug = %q, want camp", doc.SenderSlug)
	}
	if doc.AttemptedSenderSlug != "camp" {
		t.Fatalf("AttemptedSenderSlug = %q, want camp", doc.AttemptedSenderSlug)
	}
	if k.resolver.bySlugCalls != 1 {
		t.Fatalf("BySlug calls = %d, want 1", k.resolver.bySlugCalls)
	}
	if len(k.resolver.inputs) != 0 {
		t.Fatalf("category-routed Resolve must not be called when Sender is set, got %d calls", len(k.resolver.inputs))
	}
}

func TestNotificationService_Dispatch_ExplicitSender_IneligibleType_ErrorFreeOfSecrets(t *testing.T) {
	k := newKit(Options{})
	k.svc.SetOptouts(&fakeOptouts{}) // neutral: nobody has opted out — this test is about sender eligibility, not opt-outs
	k.resolver.profile = fullyPopulatedProfile("camp", []string{models.TypeTransactional})
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeMarketing, // profile only allows transactional
		Category:   "crm.campaign",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
		Sender:     "camp",
	})
	if !errors.Is(err, iface.ErrSenderNotEligible) || !errors.Is(err, ErrSendFailed) {
		t.Fatalf("err = %v, want iface.ErrSenderNotEligible + ErrSendFailed", err)
	}
	if res == nil || res.Status != models.StatusFailed {
		t.Fatalf("res = %+v", res)
	}
	if len(k.logRepo.created) != 1 {
		t.Fatalf("expected one log row, got %d", len(k.logRepo.created))
	}
	doc := k.logRepo.created[0]
	if doc.AttemptedSenderSlug != "camp" {
		t.Fatalf("AttemptedSenderSlug = %q, want camp", doc.AttemptedSenderSlug)
	}
	secrets := []string{
		"sentinel-from-zz9@example.com", "sentinel-fromname-zz9", "sentinel-replyto-zz9@example.com",
		"sentinel-host-zz9.example.net", "sentinel-username-zz9", "sentinel-password-zz9",
		"sentinel-mailupuser-zz9", "sentinel-mailupsecret-zz9", "2525",
	}
	for _, s := range secrets {
		if strings.Contains(doc.Error, s) {
			t.Fatalf("persisted Error leaks transport identity %q: %q", s, doc.Error)
		}
	}
}

func TestNotificationService_Dispatch_ExplicitSender_UnknownSlug(t *testing.T) {
	k := newKit(Options{})
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", Categories: []string{"*"}, AllowedTypes: []string{models.TypeTransactional}}
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Category:   "crm.campaign",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
		Sender:     "ghost",
	})
	if err == nil {
		t.Fatal("expected an error for an unknown slug")
	}
	if res == nil || res.Status != models.StatusFailed {
		t.Fatalf("res = %+v", res)
	}
	doc := k.logRepo.created[0]
	if doc.SenderSlug != "" {
		t.Fatalf("SenderSlug = %q, want empty", doc.SenderSlug)
	}
	if doc.AttemptedSenderSlug != "ghost" {
		t.Fatalf("AttemptedSenderSlug = %q, want ghost", doc.AttemptedSenderSlug)
	}
	// The sentinel a consumer outside this module matches. It cannot import
	// this package, so the iface sentinel is the ONLY thing it can key on: the
	// resolver's local ErrSenderNotFound must be mapped before it leaves
	// the chokepoint, exactly as PreflightDelivery maps it.
	if !errors.Is(err, iface.ErrSenderNotFound) {
		t.Fatalf("err = %v, want errors.Is iface.ErrSenderNotFound", err)
	}
	// ...and the delivery row must name the cause, not fall through to the
	// unknown-shape bucket reserved for errors nothing recognises.
	if !strings.Contains(doc.Error, "err=sender_not_found") {
		t.Fatalf("doc.Error = %q, want it to carry err=sender_not_found", doc.Error)
	}
}

func TestNotificationService_Dispatch_ExplicitSender_MalformedNeverReachesResolver(t *testing.T) {
	cases := []string{"Bad Slug!", strings.Repeat("a", 65), " camp "}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			k := newKit(Options{})
			res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
				Type:       models.TypeTransactional,
				Category:   "crm.campaign",
				Recipients: []iface.Recipient{{Address: "a@example.com"}},
				Subject:    "S",
				Body:       "B",
				Sender:     raw,
			})
			if !errors.Is(err, iface.ErrSenderInvalid) || !errors.Is(err, ErrSendFailed) {
				t.Fatalf("err = %v, want iface.ErrSenderInvalid + ErrSendFailed", err)
			}
			if res == nil || res.Status != models.StatusFailed {
				t.Fatalf("res = %+v", res)
			}
			if k.resolver.bySlugCalls != 0 {
				t.Fatalf("BySlug must not be called for a malformed slug, got %d calls", k.resolver.bySlugCalls)
			}
			if len(k.resolver.inputs) != 0 {
				t.Fatalf("Resolve must not be called for a malformed slug, got %d calls", len(k.resolver.inputs))
			}
			if len(k.logRepo.created) != 1 {
				t.Fatalf("expected one log row, got %d", len(k.logRepo.created))
			}
			doc := k.logRepo.created[0]
			if doc.AttemptedSenderSlug != "invalid" {
				t.Fatalf("AttemptedSenderSlug = %q, want the constant marker %q", doc.AttemptedSenderSlug, "invalid")
			}
			if strings.Contains(doc.Error, raw) || strings.Contains(doc.AttemptedSenderSlug, raw) || strings.Contains(doc.SenderSlug, raw) {
				t.Fatalf("raw malformed slug %q leaked into the persisted row: %+v", raw, doc)
			}
		})
	}
}

func TestNotificationService_Dispatch_EmptySender_ResolvePathUnchanged(t *testing.T) {
	k := newKit(Options{})
	res, err := sendOne(t, k)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q, want sent", res.Status)
	}
	if len(k.resolver.inputs) != 1 {
		t.Fatalf("Resolve must be called once for an empty Sender, got %d", len(k.resolver.inputs))
	}
	if k.resolver.bySlugCalls != 0 {
		t.Fatalf("BySlug must not be called for an empty Sender, got %d", k.resolver.bySlugCalls)
	}
	doc := k.logRepo.created[0]
	if doc.AttemptedSenderSlug != "" {
		t.Fatalf("AttemptedSenderSlug = %q, want empty", doc.AttemptedSenderSlug)
	}
}

func TestNotificationService_Dispatch_OptedOut_ValidSender_SuppressedBeforeResolution(t *testing.T) {
	k := newKit(Options{})
	k.pref.can = false
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", Categories: []string{"*"}, AllowedTypes: []string{models.TypeTransactional}}
	res, err := k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Category:   "crm.campaign",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
		Sender:     "camp",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSuppressed {
		t.Fatalf("status = %q, want suppressed", res.Status)
	}
	if k.pref.calledN != 1 {
		t.Fatalf("preference check must still run, calledN = %d", k.pref.calledN)
	}
	if k.resolver.bySlugCalls != 0 || len(k.resolver.inputs) != 0 {
		t.Fatalf("resolution must not run once suppressed: bySlugCalls=%d, resolveInputs=%d", k.resolver.bySlugCalls, len(k.resolver.inputs))
	}
	doc := k.logRepo.created[0]
	if doc.AttemptedSenderSlug != "" {
		t.Fatalf("AttemptedSenderSlug = %q, want empty (never reached resolution)", doc.AttemptedSenderSlug)
	}
}

// ---- ADR-0021 D6: SenderDirectory companion --------------------------------

func TestNotificationService_ListEligibleSenders_FiltersByAllowedTypeAndReady(t *testing.T) {
	noop := &fakeDriver{name: "noop"}
	broken := &fakeDriver{name: "smtp", requires: []ProfileRequirement{{Key: SubSMTPPassword, Secret: true}}}
	resolver := &fakeResolver{all: []SenderProfile{
		{Slug: "camp-mkt", Label: "Campaigns", Provider: "noop", FromAddress: "camp@x.example", AllowedTypes: []string{models.TypeMarketing}},
		{Slug: "camp-broken", Label: "Broken", Provider: "smtp", FromAddress: "broken@x.example", AllowedTypes: []string{models.TypeMarketing}, SMTPHost: "h"},
		{Slug: "txn-only", Label: "Txn", Provider: "noop", AllowedTypes: []string{models.TypeTransactional}},
		{Slug: "draft", Label: "Draft", Provider: "noop"}, // no AllowedTypes at all
	}}
	// A base URL the header can be built on: Ready is then about the driver
	// and the profile, which is what this test is for.
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		resolver, NewDriverRegistry(noop, broken), discardLogger(), Options{PublicAPIBaseURL: "https://api.example"})

	got, err := svc.ListEligibleSenders(context.Background(), models.TypeMarketing)
	if err != nil {
		t.Fatalf("ListEligibleSenders: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d senders, want 2 (txn-only and draft must be filtered): %+v", len(got), got)
	}
	if got[0].Slug != "camp-mkt" || !got[0].Ready {
		t.Fatalf("got[0] = %+v, want camp-mkt Ready=true", got[0])
	}
	if got[0].Label != "Campaigns" || got[0].Provider != "noop" || got[0].FromAddress != "camp@x.example" {
		t.Fatalf("got[0] identity fields = %+v", got[0])
	}
	if got[1].Slug != "camp-broken" || got[1].Ready {
		t.Fatalf("got[1] = %+v, want camp-broken Ready=false (missing SMTPPassword)", got[1])
	}
}

func TestNotificationService_ListEligibleSenders_NoSecretHostUsernameInOutput(t *testing.T) {
	profile := fullyPopulatedProfile("camp", []string{models.TypeMarketing})
	resolver := &fakeResolver{all: []SenderProfile{profile}}
	driver := &fakeDriver{name: "smtp"} // no Requires(): Ready is unaffected either way
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		resolver, NewDriverRegistry(driver), discardLogger(), Options{})

	got, err := svc.ListEligibleSenders(context.Background(), models.TypeMarketing)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListEligibleSenders = %+v, %v", got, err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	secrets := []string{
		"sentinel-replyto-zz9@example.com", "sentinel-host-zz9.example.net", "sentinel-username-zz9",
		"sentinel-password-zz9", "sentinel-mailupuser-zz9", "sentinel-mailupsecret-zz9", "2525", "starttls",
	}
	for _, s := range secrets {
		if strings.Contains(string(blob), s) {
			t.Fatalf("marshalled SenderInfo leaks transport identity %q: %s", s, blob)
		}
	}
	// Identity fields ARE expected to be present — this pins the assertion
	// above to something real: FromAddress is on the public shape.
	if !strings.Contains(string(blob), "sentinel-from-zz9@example.com") {
		t.Fatalf("expected FromAddress to survive marshalling: %s", blob)
	}
}

func TestNotificationService_ListEligibleSenders_ConfigUnavailable_NeverEmptyList(t *testing.T) {
	resolver := &fakeResolver{err: ErrSenderConfigUnavailable}
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		resolver, NewDriverRegistry(&fakeDriver{name: "noop"}), discardLogger(), Options{})

	got, err := svc.ListEligibleSenders(context.Background(), models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderUnavailable) {
		t.Fatalf("err = %v, want iface.ErrSenderUnavailable", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty/nil on error — never a silent empty-list success", got)
	}
}

// ---- PreflightDelivery: explicit arm ---------------------------------------

func TestNotificationService_PreflightDelivery_ExplicitSender_Eligible_ReturnsNil(t *testing.T) {
	k := newKit(Options{PublicAPIBaseURL: "https://api.example"}) // marketing preflight also needs one-click to be satisfiable
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", AllowedTypes: []string{models.TypeMarketing}}
	err := k.svc.PreflightDelivery(context.Background(), "camp", "marketing", models.TypeMarketing)
	if err != nil {
		t.Fatalf("PreflightDelivery: %v", err)
	}
	if k.resolver.bySlugCalls != 1 {
		t.Fatalf("BySlug calls = %d, want 1", k.resolver.bySlugCalls)
	}
}

func TestNotificationService_PreflightDelivery_ExplicitSender_Malformed_NoResolverLookup(t *testing.T) {
	k := newKit(Options{})
	err := k.svc.PreflightDelivery(context.Background(), "Bad Slug!", "marketing", models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderInvalid) {
		t.Fatalf("err = %v, want iface.ErrSenderInvalid", err)
	}
	if k.resolver.bySlugCalls != 0 {
		t.Fatalf("BySlug must not be called for a malformed slug, got %d calls", k.resolver.bySlugCalls)
	}
	if len(k.resolver.inputs) != 0 {
		t.Fatalf("Resolve must not be called for a malformed slug, got %d calls", len(k.resolver.inputs))
	}
}

func TestNotificationService_PreflightDelivery_ExplicitSender_NotFound(t *testing.T) {
	k := newKit(Options{})
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", AllowedTypes: []string{models.TypeMarketing}}
	err := k.svc.PreflightDelivery(context.Background(), "ghost", "marketing", models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderNotFound) {
		t.Fatalf("err = %v, want iface.ErrSenderNotFound", err)
	}
}

func TestNotificationService_PreflightDelivery_ExplicitSender_NotEligible(t *testing.T) {
	k := newKit(Options{})
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", AllowedTypes: []string{models.TypeTransactional}}
	err := k.svc.PreflightDelivery(context.Background(), "camp", "marketing", models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderNotEligible) {
		t.Fatalf("err = %v, want iface.ErrSenderNotEligible", err)
	}
}

func TestNotificationService_PreflightDelivery_ExplicitSender_NotConfigured(t *testing.T) {
	k := newKit(Options{})
	k.driver.requires = []ProfileRequirement{{Key: SubSMTPHost}}
	k.resolver.profile = SenderProfile{Slug: "camp", Provider: "noop", AllowedTypes: []string{models.TypeMarketing}} // no SMTPHost
	err := k.svc.PreflightDelivery(context.Background(), "camp", "marketing", models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderNotConfigured) {
		t.Fatalf("err = %v, want iface.ErrSenderNotConfigured", err)
	}
}

func TestNotificationService_PreflightDelivery_ExplicitSender_Unavailable(t *testing.T) {
	k := newKit(Options{})
	k.resolver.err = ErrSenderConfigUnavailable
	err := k.svc.PreflightDelivery(context.Background(), "camp", "marketing", models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderUnavailable) {
		t.Fatalf("err = %v, want iface.ErrSenderUnavailable", err)
	}
}

// ---- PreflightDelivery: default arm (category routing) --------------------

func TestNotificationService_PreflightDelivery_DefaultArm_Routed_ReturnsNil(t *testing.T) {
	k := newKit(Options{PublicAPIBaseURL: "https://api.example"}) // marketing preflight also needs one-click to be satisfiable
	k.resolver.profile = SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}}
	err := k.svc.PreflightDelivery(context.Background(), "", "marketing", models.TypeMarketing)
	if err != nil {
		t.Fatalf("PreflightDelivery: %v", err)
	}
	if len(k.resolver.inputs) != 1 || k.resolver.inputs[0].Category != "marketing" || k.resolver.inputs[0].Type != models.TypeMarketing {
		t.Fatalf("resolver input = %+v", k.resolver.inputs)
	}
	if k.resolver.bySlugCalls != 0 {
		t.Fatalf("BySlug must not be called for the default arm, got %d calls", k.resolver.bySlugCalls)
	}
}

func TestNotificationService_PreflightDelivery_DefaultArm_NothingRoutes(t *testing.T) {
	k := newKit(Options{})
	k.resolver.err = ErrNoSenderForCategory
	err := k.svc.PreflightDelivery(context.Background(), "", "unrouted.category", models.TypeMarketing)
	if !errors.Is(err, iface.ErrNoSenderForCategory) {
		t.Fatalf("err = %v, want iface.ErrNoSenderForCategory", err)
	}
}

func TestNotificationService_PreflightDelivery_DefaultArm_RoutedButNotConfigured(t *testing.T) {
	k := newKit(Options{})
	k.driver.requires = []ProfileRequirement{{Key: SubSMTPHost}}
	k.resolver.profile = SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}} // no SMTPHost
	err := k.svc.PreflightDelivery(context.Background(), "", "marketing", models.TypeMarketing)
	if !errors.Is(err, iface.ErrSenderNotConfigured) {
		t.Fatalf("err = %v, want iface.ErrSenderNotConfigured", err)
	}
}
