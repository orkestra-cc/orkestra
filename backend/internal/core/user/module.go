package user

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	authRepo "github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/user/handlers"
	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/blob"
)

type UserModule struct {
	module.BaseModule
	handler               *handlers.UserHandler
	adminClientHandler    *handlers.AdminClientUserHandler
	operatorAvatarHandler *handlers.AvatarHandler
	clientAvatarHandler   *handlers.AvatarHandler
}

func NewModule() *UserModule { return &UserModule{} }

func (m *UserModule) Name() string        { return "user" }
func (m *UserModule) DisplayName() string { return "User Management" }
func (m *UserModule) Description() string { return "User accounts, profiles, and RBAC" }

func (m *UserModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserService}
}

func (m *UserModule) Collections() []module.CollectionSpec {
	// Email uniqueness on the per-tier collections is scoped to that
	// collection — the same email address may legitimately exist as both
	// an operator user and a client user (the same human running an
	// internal staff account and an external client account).
	tierUserIndexes := []module.IndexSpec{
		{Keys: map[string]int{"uuid": 1}, Unique: true},
		{Keys: map[string]int{"email": 1}, Unique: true},
		{Keys: map[string]int{"tier": 1}},
	}
	return []module.CollectionSpec{
		{Name: repository.OperatorUsersCollection, Indexes: tierUserIndexes},
		{Name: repository.ClientUsersCollection, Indexes: tierUserIndexes},
	}
}

func (m *UserModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "personal", Section: "My workspace", Name: "Dashboard", Icon: "chart-pie", Path: "/user/dashboard", Active: true},
		{Realm: "personal", Section: "My workspace", Name: "Profile", Icon: "user", Path: "/user/profile", Active: true},
		{Realm: "personal", Section: "My workspace", Name: "Calendar", Icon: "calendar-alt", Path: "/user/calendar", Active: true},
		{Realm: "platform", Tier: "internal", Name: "User Management", Icon: "users-cog", Path: "/admin/users", MinRole: "administrator", Active: true},
		{Realm: "platform", Tier: "internal", Name: "Module Management", Icon: "puzzle-piece", Path: "/admin/modules", MinRole: "administrator", Active: true},
	}
}

func (m *UserModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "user.read", Module: "user", Description: "List users"},
		{Key: "user.update", Module: "user", Description: "Update user profiles"},
		{Key: "user.delete", Module: "user", Description: "Delete users"},
		{Key: "user.self", Module: "user", Description: "Edit your own profile"},
		{Key: "user.avatar.self", Module: "user", Description: "Manage your own avatar (upload, pick from linked OAuth provider, reset to initials)"},
	}
}

func (m *UserModule) Init(deps *module.Dependencies) error {
	// ADR-0003 PR-D D-8: operator-tier user provider is the canonical
	// ServiceUserService. Both tier providers are registered under
	// their dedicated keys so audience-specific consumers (onboarding
	// → client, setup wizard → operator) can pick the right one
	// directly rather than going through the canonical key.
	operatorOAuthRepo := authRepo.NewOperatorOAuthProviderRepository(deps.DB)
	clientOAuthRepo := authRepo.NewClientOAuthProviderRepository(deps.DB)

	operatorRepo := repository.NewOperatorUserRepository(deps.DB)
	operatorSvc := services.NewUserService(operatorRepo, operatorOAuthRepo)

	clientRepo := repository.NewClientUserRepository(deps.DB)
	clientSvc := services.NewUserService(clientRepo, clientOAuthRepo)

	deps.Services.Register(module.ServiceOperatorUserProvider, operatorSvc)
	deps.Services.Register(module.ServiceClientUserProvider, clientSvc)

	canonical := operatorSvc
	canonicalRepo := operatorRepo
	m.handler = handlers.NewUserHandler(canonical)
	m.adminClientHandler = handlers.NewAdminClientUserHandler(clientSvc, deps.Services)
	deps.Services.Register(module.ServiceUserService, canonical)

	// Wire the blob store into both per-tier services so uploaded
	// avatars get fresh presigned GETs on every read path. Optional —
	// when the store isn't wired (storage env unset, S3 endpoint down)
	// uploaded-source avatars fall back to whatever URL the document
	// happens to carry.
	var blobStore blob.Store
	if store, ok := module.GetTyped[blob.Store](deps.Services, module.ServiceBlobStore); ok {
		blobStore = store
		operatorSvc.(interface{ SetBlobStore(blob.Store) }).SetBlobStore(store)
		clientSvc.(interface{ SetBlobStore(blob.Store) }).SetBlobStore(store)
	}

	// Per-tier avatar handlers — each bound to its own UserService so
	// SetAvatarSource lands on the right collection. blobStore may be
	// nil; the handler degrades to 503 for uploads in that case.
	m.operatorAvatarHandler = handlers.NewAvatarHandler(operatorSvc, blobStore, iface.TierOperator)
	m.clientAvatarHandler = handlers.NewAvatarHandler(clientSvc, blobStore, iface.TierClient)

	// Register the user PII producer with the DSR registry pre-created in
	// main.go. Missing registry means the platform was booted without
	// compliance infrastructure — tolerate and skip.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(canonicalRepo))
	}

	// Backfill the language field on accounts that predate it so /me
	// can always return a stable BCP-47 tag. Idempotent: the filter
	// matches only rows missing or with an empty language. Boot-time
	// cost is one indexed-or-collscan UpdateMany per tier — sub-second
	// on realistic operator+client populations.
	ctx := context.Background()
	if _, err := operatorRepo.BackfillDefaultLanguage(ctx, iface.DefaultLanguage); err != nil {
		deps.Logger.Warn("user language backfill (operator) failed", "err", err)
	}
	if _, err := clientRepo.BackfillDefaultLanguage(ctx, iface.DefaultLanguage); err != nil {
		deps.Logger.Warn("user language backfill (client) failed", "err", err)
	}
	return nil
}

// registerAvatarRoutes mounts the three self-service avatar endpoints
// on the given Huma API + handler instance. Path prefix is /v1/me/avatar
// — outside the /v1/auth namespace because these are user-profile
// mutations, not auth flows. Caller wraps with RequireGlobal() so any
// authenticated user can manage their own avatar.
func registerAvatarRoutes(api huma.API, h *handlers.AvatarHandler, opIDPrefix string) {
	huma.Register(api, huma.Operation{
		OperationID: opIDPrefix + "presign-avatar-upload",
		Method:      "POST",
		Path:        "/v1/me/avatar/presign-upload",
		Summary:     "Mint a short-lived presigned PUT URL for an avatar upload",
		Description: "Returns a URL the SPA PUTs the image directly to S3-compatible storage. Cap 2 MiB; MIME must be image/png|jpeg|webp. The SPA echoes the returned key on the commit call.",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.PresignAvatarUpload)

	huma.Register(api, huma.Operation{
		OperationID: opIDPrefix + "commit-avatar-upload",
		Method:      "POST",
		Path:        "/v1/me/avatar/commit",
		Summary:     "Promote a freshly-uploaded blob to be the user's active avatar",
		Description: "Verifies the object landed in storage, sets AvatarSource=uploaded, and GCs the previous blob. Returns the updated user profile.",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.CommitAvatarUpload)

	huma.Register(api, huma.Operation{
		OperationID: opIDPrefix + "set-avatar-source",
		Method:      "PATCH",
		Path:        "/v1/me/avatar/source",
		Summary:     "Switch the avatar to initials or to a linked OAuth provider's picture",
		Description: "oauth_* requires the matching provider to be linked (422 otherwise). Use presign-upload + commit to set source to uploaded.",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.SetAvatarSource)
}

func (m *UserModule) RegisterRoutes(ri *module.RouteInfo) {
	// User management is a platform-level concern: users are global, so
	// routes live on the system permission gate (administrators) rather
	// than per-org.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.admin"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
		RegisterAdminClientRoutes(api, m.adminClientHandler)
	})

	// Self-service avatar surface — mounted on BOTH audiences so a
	// Tier-2 client can change their own avatar on api.* just like a
	// Tier-1 operator does on console.*. Gate is RequireGlobal — any
	// authenticated user manages their own row; the handler enforces
	// owner-self via the JWT user UUID.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		registerAvatarRoutes(api, m.operatorAvatarHandler, "operator-")
	})
	if ri.Client != nil && ri.Client.ProtectedRouter != nil && m.clientAvatarHandler != nil {
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			registerAvatarRoutes(api, m.clientAvatarHandler, "client-")
		})
	}
}
