package notification

import (
	"context"
	"strconv"
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
	svc     *services.NotificationService
	handler *handlers.NotificationHandler
}

func NewModule() *NotificationModule { return &NotificationModule{} }

func (m *NotificationModule) Name() string        { return "notification" }
func (m *NotificationModule) DisplayName() string { return "Notifications" }
func (m *NotificationModule) Description() string {
	return "Email notifications, templates, and user preferences"
}
func (m *NotificationModule) Category() module.ModuleCategory { return module.CategoryCore }

func (m *NotificationModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceNotificationSender}
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
	}
}

func (m *NotificationModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "email.provider", Label: "Email provider", Type: module.FieldString, Required: true, Default: "noop", EnvVar: "NOTIFICATION_EMAIL_PROVIDER"},
		{Key: "email.from_address", Label: "From address", Type: module.FieldString, EnvVar: "NOTIFICATION_EMAIL_FROM"},
		{Key: "email.from_name", Label: "From name", Type: module.FieldString, Default: "Orkestra", EnvVar: "NOTIFICATION_EMAIL_FROM_NAME"},
		{Key: "email.reply_to", Label: "Reply-To address", Type: module.FieldString, EnvVar: "NOTIFICATION_EMAIL_REPLY_TO"},
		{Key: "email.smtp.host", Label: "SMTP host", Type: module.FieldString, EnvVar: "SMTP_HOST"},
		{Key: "email.smtp.port", Label: "SMTP port", Type: module.FieldInt, Default: "587", EnvVar: "SMTP_PORT"},
		{Key: "email.smtp.username", Label: "SMTP username", Type: module.FieldString, EnvVar: "SMTP_USERNAME"},
		{Key: "email.smtp.password", Label: "SMTP password", Type: module.FieldSecret, EnvVar: "SMTP_PASSWORD"},
		{Key: "email.smtp.tls_mode", Label: "TLS mode", Type: module.FieldString, Default: "starttls", EnvVar: "SMTP_TLS_MODE"},
		{Key: "app.name", Label: "App name (in templates)", Type: module.FieldString, Default: "Orkestra", EnvVar: "APP_NAME"},
		{Key: "app.support_email", Label: "Support email (in templates)", Type: module.FieldString, EnvVar: "SUPPORT_EMAIL"},
	}
}

func (m *NotificationModule) Init(deps *module.Dependencies) error {
	logRepo := repository.NewNotificationRepository(deps.DB)
	tmplRepo := repository.NewTemplateRepository(deps.DB)
	prefRepo := repository.NewPreferenceRepository(deps.DB)
	unsubRepo := repository.NewUnsubscribeRepository(deps.DB)

	tmplService := services.NewTemplateService(tmplRepo, deps.Logger)
	prefService := services.NewPreferenceService(prefRepo)
	unsubService := services.NewUnsubscribeService(unsubRepo)

	// Settings loader: look up DB config with env var fallback at every send.
	loader := func(ctx context.Context) services.EmailSettings {
		port, _ := strconv.Atoi(deps.GetConfig("notification", "email.smtp.port"))
		if port == 0 {
			port = 587
		}
		return services.EmailSettings{
			Provider:    deps.GetConfig("notification", "email.provider"),
			Host:        deps.GetConfig("notification", "email.smtp.host"),
			Port:        port,
			Username:    deps.GetConfig("notification", "email.smtp.username"),
			Password:    deps.GetSecret("notification", "email.smtp.password"),
			FromAddress: deps.GetConfig("notification", "email.from_address"),
			FromName:    deps.GetConfig("notification", "email.from_name"),
			ReplyTo:     deps.GetConfig("notification", "email.reply_to"),
			TLSMode:     deps.GetConfig("notification", "email.smtp.tls_mode"),
		}
	}

	emailSender := services.NewEmailService(loader, deps.Logger)

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

	m.svc = services.NewNotificationService(
		logRepo,
		tmplService,
		prefService,
		unsubService,
		emailSender,
		deps.Logger,
		services.Options{
			AppName:       appName,
			SupportEmail:  supportEmail,
			URLBuilder:    urlBuilder,
			DefaultLocale: "en",
		},
	)

	m.handler = handlers.NewNotificationHandler(m.svc)

	deps.Services.Register(module.ServiceNotificationSender, m.svc)
	return nil
}

func (m *NotificationModule) Start(ctx context.Context) error {
	if m.svc == nil {
		return nil
	}
	return m.svc.TemplateService().SeedDefaults(ctx)
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
