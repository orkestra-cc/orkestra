package notification

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/notification/handlers"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type NotificationModule struct {
	module.BaseModule
	svc        *services.NotificationService
	drivers    *services.DriverRegistry
	handler    *handlers.NotificationHandler
	reconciler *services.OptoutReconciler

	// configService is kept for livePolicy (config_validation.go), which
	// re-reads the one-click requirement on every marketing send rather
	// than trusting a value captured here in Init. nil outside a wired boot,
	// which livePolicy treats as an unreadable config: fail closed.
	configService *module.ModuleConfigService
}

func NewModule() *NotificationModule { return &NotificationModule{} }

func (m *NotificationModule) Name() string        { return "notification" }
func (m *NotificationModule) DisplayName() string { return "Notifications" }
func (m *NotificationModule) Description() string {
	return "Email notifications, templates, and user preferences"
}
func (m *NotificationModule) Category() module.ModuleCategory { return module.CategoryCore }

func (m *NotificationModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceNotificationSender, module.ServiceNotificationTemplateSeeder}
}

func (m *NotificationModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "notification.preferences.self", Module: "notification", Description: "View and edit your own notification preferences"},
		{Key: "notification.log.read", Module: "notification", Description: "Read the delivery log", System: true},
		{Key: "notification.template.manage", Module: "notification", Description: "Create and override email templates", System: true},
		{Key: "notification.test", Module: "notification", Description: "Send test emails", System: true},
	}
}

func (m *NotificationModule) HotReloadConfig() bool { return true }

func (m *NotificationModule) Collections() []module.CollectionSpec {
	day30 := 30 * 24 * time.Hour
	day90 := 90 * 24 * time.Hour
	return []module.CollectionSpec{
		{
			Name: models.NotificationMessagesCollection,
			Indexes: []module.IndexSpec{
				{Keys: map[string]int{"uuid": 1}, Unique: true},
				{Keys: map[string]int{"recipientUserUuid": 1}},
				{Keys: map[string]int{"category": 1}},
				{Keys: map[string]int{"idempotencyKey": 1}},
				{Keys: map[string]int{"senderSlug": 1}, Sparse: true},
				{Keys: map[string]int{"createdAt": 1}, TTL: day90},
			},
		},
		{
			Name: models.NotificationTemplatesCollection,
			Indexes: []module.IndexSpec{
				{Keys: map[string]int{"uuid": 1}, Unique: true},
				{OrderedKeys: []module.IndexKey{
					{Field: "templateId", Direction: 1},
					{Field: "locale", Direction: 1},
				}, Unique: true},
			},
		},
		{
			Name: models.NotificationPreferencesCollection,
			Indexes: []module.IndexSpec{
				{OrderedKeys: []module.IndexKey{
					{Field: "userUuid", Direction: 1},
					{Field: "category", Direction: 1},
					{Field: "channel", Direction: 1},
				}, Unique: true},
			},
		},
		{
			Name: models.NotificationSuppressionsCollection,
			Indexes: []module.IndexSpec{
				{Keys: map[string]int{"address": 1}, Unique: true},
			},
		},
		{
			Name: models.NotificationUnsubscribeTokensCollect,
			Indexes: []module.IndexSpec{
				{Keys: map[string]int{"uuid": 1}, Unique: true},
				{Keys: map[string]int{"tokenHash": 1}, Unique: true},
				{Keys: map[string]int{"expiresAt": 1}, TTL: day30},
				// The reconciler's scan (repository.ListPending) runs on a
				// ticker for the life of the process, so it must not read a
				// collection that holds 30 days of tokens to find the few
				// rows that still owe work. Partial rather than sparse: these
				// index ONLY the pending rows, and a flag is $unset — not set
				// to false — when its work completes, so a settled row leaves
				// the index. Two indexes because the scan is an $or and each
				// branch needs one of its own; nextAttemptAt is the second
				// key so each branch comes back oldest-due-first.
				{OrderedKeys: []module.IndexKey{
					{Field: "sinkPending", Direction: 1},
					{Field: "nextAttemptAt", Direction: 1},
				}, PartialFilter: map[string]any{"sinkPending": true}},
				{OrderedKeys: []module.IndexKey{
					{Field: "prefPending", Direction: 1},
					{Field: "nextAttemptAt", Direction: 1},
				}, PartialFilter: map[string]any{"prefPending": true}},
			},
		},
		// ADR-0003 PR-B: tier-split unsubscribe tokens. Same shape as
		// the legacy collection — only the name differs.
		{
			Name: models.NotificationOperatorUnsubscribeTokensCollect,
			Indexes: []module.IndexSpec{
				{Keys: map[string]int{"uuid": 1}, Unique: true},
				{Keys: map[string]int{"tokenHash": 1}, Unique: true},
				{Keys: map[string]int{"expiresAt": 1}, TTL: day30},
			},
		},
		{
			Name: models.NotificationClientUnsubscribeTokensCollect,
			Indexes: []module.IndexSpec{
				{Keys: map[string]int{"uuid": 1}, Unique: true},
				{Keys: map[string]int{"tokenHash": 1}, Unique: true},
				{Keys: map[string]int{"expiresAt": 1}, TTL: day30},
			},
		},
		{Name: models.NotificationMarketingOptoutsCollection, Indexes: []module.IndexSpec{
			// One opt-out per address, enforced here. Under concurrency this
			// index is what makes two racing upserts land on one document —
			// Upsert (repository/optout_repository.go) swallows the loser's
			// resulting duplicate-key error, which is what makes the call
			// itself idempotent, not just the stored state.
			{OrderedKeys: []module.IndexKey{{Field: "address", Direction: 1}}, Unique: true},
		}},
	}
}

// ConfigGroups upgrades /admin/modules/notification from one flat card to the
// full-page rail. Four sections: Delivery (how mail leaves the platform),
// Sender profiles (the record list, ADR-0019), Sender (the addresses
// recipients see), Branding (values injected into every templated email).
// The SMTP connection settings live under Delivery and are hidden until the
// provider is set to smtp.
func (m *NotificationModule) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "delivery", Label: "Delivery", Order: 1,
			Description: "The default transport. With the noop provider, rendered mail is logged to the backend instead of sent — set the provider to SMTP to reveal the connection settings. Ignored once at least one sender profile below routes a category."},
		{Key: "senders", Label: "Sender profiles", Order: 2,
			Description: "Several senders, each with its own transport and identity, selected per message by category patterns (auth.*, crm.*, * for the default). Until one profile declares a pattern, the Delivery and Sender settings remain the single default."},
		{Key: "sender", Label: "Sender", Order: 3,
			Description: "The addresses recipients see when the default transport is used."},
		{Key: "branding", Label: "Branding & templates", Order: 4,
			Description: "Values injected into every templated email."},
	}
}

func (m *NotificationModule) ConfigSchema() []module.ConfigField {
	// The five SMTP connection settings are only meaningful when the provider
	// dials a real server. Shared read-only condition — never mutated.
	smtpOnly := []module.FieldCondition{{Key: "email.provider", In: []string{"smtp"}}}
	return []module.ConfigField{
		{Key: "email.provider", Label: "Email provider", Group: "delivery", Type: module.FieldEnum, Options: []string{"noop", "smtp"}, Required: true, Default: "noop", EnvVar: "NOTIFICATION_EMAIL_PROVIDER"},
		// Required composes with DependsOn as required-when-visible: the admin
		// UI enforces it only while the provider is smtp. Username/password
		// stay optional (unauthenticated relays), port/tls_mode have defaults.
		{Key: "email.smtp.host", Label: "SMTP host", Group: "delivery", Type: module.FieldString, Required: true, DependsOn: smtpOnly, EnvVar: "SMTP_HOST"},
		{Key: "email.smtp.port", Label: "SMTP port", Group: "delivery", Type: module.FieldInt, Default: "587", DependsOn: smtpOnly, EnvVar: "SMTP_PORT"},
		{Key: "email.smtp.username", Label: "SMTP username", Group: "delivery", Type: module.FieldString, DependsOn: smtpOnly, EnvVar: "SMTP_USERNAME"},
		{Key: "email.smtp.password", Label: "SMTP password", Group: "delivery", Type: module.FieldSecret, DependsOn: smtpOnly, EnvVar: "SMTP_PASSWORD"},
		{Key: "email.smtp.tls_mode", Label: "TLS mode", Group: "delivery", Type: module.FieldEnum, Options: []string{"starttls", "tls", "none"}, Default: "starttls", DependsOn: smtpOnly, EnvVar: "SMTP_TLS_MODE"},
		{Key: services.PublicAPIBaseURLField, Label: "Public API base URL", Group: "delivery", Type: module.FieldString, EnvVar: "NOTIFICATION_PUBLIC_API_BASE_URL",
			Description: "The API's own https origin (e.g. https://api.example.com), used only to build the RFC 8058 List-Unsubscribe header on marketing mail. Not derived automatically: PlatformInfo exposes the frontend origin, not the API's, and a request's Host header cannot be trusted for a URL that will sit in a recipient's mailbox for months. Empty by default. While it is empty AND \"Require one-click unsubscribe\" below is on — both are the defaults — no marketing sender profile can be saved and no marketing message is sent; turning that requirement off instead sends marketing with no unsubscribe header."},
		{Key: services.UnsubscribePageURLField, Label: "Hosted unsubscribe page URL", Group: "delivery", Type: module.FieldString, EnvVar: "NOTIFICATION_UNSUBSCRIBE_PAGE_URL",
			Description: "Optional. The public API base URL above is for the mail client's own one-click button — there is no browser involved, so it is never a page a person can look at. Set this to a hosted page's own https origin (e.g. https://unsubscribe.example.com) to give the footer link in every templated email's footer somewhere a human actually clicks: a sentence explaining what is about to happen, and a button pressed deliberately. This repository does not build that page — a deployment that sets this must provide one, and it must (1) read the single-use token from the URL FRAGMENT, never the query string, so it never reaches the page's access logs, a proxy in front of it, or a Referer header it might leak onward; (2) never call the API automatically on page load — the token is single-use and mail scanners follow links the moment a message arrives; and (3) POST to /v1/notifications/unsubscribe only when the visitor presses a button. Empty by default, in which case the footer link stays the direct API GET link it has always been. A value that is not a bare https origin (no path, query string, or fragment) is treated the same as empty — a clean fallback rather than a broken link — and never blocks a send: this footer renders on transactional templates too."},
		{Key: requireOneClickKey, Label: "Require one-click unsubscribe", Group: "delivery", Type: module.FieldBool, Default: "true", EnvVar: "NOTIFICATION_REQUIRE_ONE_CLICK_UNSUBSCRIBE",
			Description: "On by default. While it is on, a sender profile may be used for marketing only if its provider can add the RFC 8058 one-click unsubscribe headers and the public API base URL above is set — and a marketing message that would leave without them is refused instead of sent. " +
				"Turning this off does not simply skip a check: marketing email will then be delivered WITHOUT a one-click unsubscribe. Large mailbox providers require one from bulk senders and penalise domains that omit it, and giving recipients an easy way to withdraw consent is a legal requirement for commercial email in several jurisdictions, so turn it off only if you meet that obligation another way."},
		{Key: "email.from_address", Label: "From address", Group: "sender", Type: module.FieldString, EnvVar: "NOTIFICATION_EMAIL_FROM"},
		{Key: "email.from_name", Label: "From name", Group: "sender", Type: module.FieldString, Default: "Orkestra", EnvVar: "NOTIFICATION_EMAIL_FROM_NAME"},
		{Key: "email.reply_to", Label: "Reply-To address", Group: "sender", Type: module.FieldString, EnvVar: "NOTIFICATION_EMAIL_REPLY_TO"},
		{Key: services.SendersField, Label: "Sender profiles", Group: "senders", Type: module.FieldRecordList, Items: services.SenderItems(),
			Description: "Each profile is a transport and an identity. Patterns decide which categories it carries; the most specific pattern wins and * is the default. A profile without patterns is a draft and receives no mail. Once any profile declares a pattern, exactly one must declare *."},
		{Key: "app.name", Label: "App name (in templates)", Group: "branding", Type: module.FieldString, Default: "Orkestra", EnvVar: "APP_NAME"},
		{Key: "app.support_email", Label: "Support email (in templates)", Group: "branding", Type: module.FieldString, EnvVar: "SUPPORT_EMAIL"},
		{Key: "app.default_locale", Label: "Default template locale", Group: "branding", Type: module.FieldEnum, Options: models.SupportedLocales, Default: "en", EnvVar: "NOTIFICATION_DEFAULT_LOCALE",
			Description: "Language used when a caller does not name one. Template lookup has no locale fallback, so this must name a locale that has seeded templates — callers that resolve a locale per recipient are unaffected."},
	}
}

func (m *NotificationModule) Init(deps *module.Dependencies) error {
	logRepo := repository.NewNotificationRepository(deps.DB)
	tmplRepo := repository.NewTemplateRepository(deps.DB)
	prefRepo := repository.NewPreferenceRepository(deps.DB)
	unsubRepo := repository.NewUnsubscribeRepository(deps.DB)
	optoutRepo := repository.NewMarketingOptoutRepository(deps.DB)

	// Register the notification PII producer with the DSR registry (created in
	// main.go before InitAll) so the compliance DSR pipeline exports / erases a
	// data subject's message history + delivery preferences.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(deps.DB))
	}

	tmplService := services.NewTemplateService(tmplRepo, deps.Logger)
	prefService := services.NewPreferenceService(prefRepo)

	// The sink lives on the notification service, which takes the unsubscribe
	// service as a constructor argument — so the reference is resolved at call
	// time rather than at wiring time. Consume only ever runs from an HTTP
	// route and the reconciler only from its ticker, both long after Init
	// returned and m.svc was assigned; the guard is there so an unfinished
	// boot would leave sinkPending up for the reconciler instead of panicking.
	// One firer serves both paths, so they share the nil-sink and panic
	// guards inside FireMarketingUnsubscribe.
	fireSink := services.MarketingUnsubscribeFirer(func(ctx context.Context, address, category, refContext string) error {
		if m.svc == nil {
			return errors.New("notification: service not initialised")
		}
		return m.svc.FireMarketingUnsubscribe(ctx, address, category, refContext)
	})

	unsubService := services.NewUnsubscribeService(unsubRepo,
		services.WithOptouts(optoutRepo),
		services.WithPreferences(prefService),
		services.WithUnsubscribeLogger(deps.Logger),
		services.WithUnsubscribeSink(fireSink),
	)

	// The reconciler replays what a live consume could not finish: the
	// preference row and the sink call, marked on the token by the claim.
	// Started in Start() and stopped in Stop() so a runtime disable takes the
	// ticker down with the module.
	m.reconciler = services.NewOptoutReconciler(services.OptoutReconcilerDeps{
		Tokens: unsubRepo,
		Prefs:  prefService,
		Sink:   fireSink,
		Logger: deps.Logger,
	})

	// One document read per send: values and secrets of the active
	// environment come from the same snapshot (D4), and admin UI changes
	// still propagate without a restart. Until PR 2 reads the email.senders
	// roster the legacy profile is the only one (ADR-0019 D6).
	snapshot := func(ctx context.Context) (*module.ModuleConfig, error) {
		if deps.ConfigService == nil {
			return nil, nil
		}
		return deps.ConfigService.GetConfig(ctx, "notification")
	}
	loader := services.NewSnapshotLoader(snapshot)

	m.drivers = services.NewDriverRegistry(services.CoreDrivers(deps.Logger)...)
	resolver := services.NewSenderResolver(loader)

	frontendURL := deps.Platform.FrontendURL()
	urlBuilder := func(path string) string {
		if len(path) > 0 && path[0] != '/' {
			path = "/" + path
		}
		return frontendURL + path
	}

	appName := deps.GetConfig("notification", "app.name")
	if appName == "" {
		appName = "Orkestra"
	}
	supportEmail := deps.GetConfig("notification", "app.support_email")
	// The one-click requirement, the base URL the header is built on, and
	// the optional hosted unsubscribe page URL the footer link is built on
	// are NOT captured here. All three are read per send through
	// m.livePolicy (services.OneClickPolicy), because this module declares
	// HotReloadConfig() == true: a config write leaves no restart-required
	// flag, and this admin surface has no per-field way to say that one
	// field in the "delivery" group is the exception. An operator who
	// switches require_one_click_unsubscribe ON must see marketing start
	// being refused immediately; an operator who sets the page URL must see
	// the very next templated send's footer point at it — neither after the
	// next restart.
	m.configService = deps.ConfigService

	// Template lookup is exact on (templateID, locale) with no fallback, so a
	// default naming a locale without seeded templates fails every send that
	// does not carry an explicit one — and only logs. Hence the enum over
	// SupportedLocales rather than a free string.
	defaultLocale := deps.GetConfig("notification", "app.default_locale")
	if defaultLocale == "" {
		defaultLocale = "en"
	}

	m.svc = services.NewNotificationService(
		logRepo,
		tmplService,
		prefService,
		unsubService,
		resolver,
		m.drivers,
		deps.Logger,
		services.Options{
			AppName:        appName,
			SupportEmail:   supportEmail,
			URLBuilder:     urlBuilder,
			DefaultLocale:  defaultLocale,
			OneClickSource: m.livePolicy,
		},
	)
	m.svc.SetOptouts(optoutRepo)

	m.handler = handlers.NewNotificationHandler(m.svc)

	deps.Services.Register(module.ServiceNotificationSender, m.svc)
	deps.Services.Register(module.ServiceNotificationTemplateSeeder,
		module.NotificationTemplateSeeder(m.svc.TemplateService()))
	return nil
}

func (m *NotificationModule) Start(ctx context.Context) error {
	if m.reconciler != nil {
		// The pending flags are only worth writing if something replays them.
		m.reconciler.Start(ctx)
	}
	if m.svc == nil {
		return nil
	}
	return m.svc.TemplateService().SeedDefaults(ctx)
}

// Stop halts the reconciler ticker on module disable / host shutdown.
func (m *NotificationModule) Stop(_ context.Context) error {
	if m.reconciler != nil {
		m.reconciler.Stop()
	}
	return nil
}

func (m *NotificationModule) RegisterRoutes(ri *module.RouteInfo) {
	// Public unsubscribe endpoint — no auth required.
	m.handler.RegisterPublicRoutes(ri.Operator.PublicAPI)

	// User-facing preference endpoints: self-service, no org context needed.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterUserRoutes(api)
	})

	// Admin endpoints: platform-level (delivery log, templates, test email).
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("notification.log.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterAdminRoutes(api)
	})
}
