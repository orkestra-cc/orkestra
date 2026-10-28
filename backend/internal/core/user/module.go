package user

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	authRepo "github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/user/handlers"
	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type UserModule struct {
	module.BaseModule
	handler               *handlers.UserHandler
	adminClientHandler    *handlers.AdminClientUserHandler
	operatorAvatarHandler *handlers.AvatarHandler
	clientAvatarHandler   *handlers.AvatarHandler
	// preferenceHandler serves /v1/me/preferences* and the profile-section
	// discovery routes (both tiers); see handlers.PreferenceHandler.
	preferenceHandler  *handlers.PreferenceHandler
	preferenceRepo     repository.PreferenceRepository
	preferenceRegistry *module.UserPreferenceRegistry
	services           *module.ServiceRegistry
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
		{Keys: map[string]int{"kind": 1}},
	}
	return []module.CollectionSpec{
		{Name: repository.OperatorUsersCollection, Indexes: tierUserIndexes},
		{Name: repository.ClientUsersCollection, Indexes: tierUserIndexes},
		// Per-user, per-module preference documents (module.HasUserPreferences).
		// User-keyed: preferences follow the user across tenants.
		{Name: repository.UserPreferencesCollection, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{{Field: "userUuid", Direction: 1}, {Field: "module", Direction: 1}}, Unique: true},
		}},
	}
}

func (m *UserModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		// No Section: these live directly under the "personal" realm whose
		// canonical label is already "My workspace" (see realmLabel in
		// navigation/services/dynamic_navigation.go). Setting a section equal
		// to the realm label rendered a duplicate "My workspace" header.
		{Realm: "personal", Name: "Dashboard", Icon: "chart-pie", Path: "/user/dashboard", Active: true},
		{Realm: "personal", Name: "Profile", Icon: "user", Path: "/user/profile", Active: true},
		{Realm: "personal", Name: "Calendar", Icon: "calendar-alt", Path: "/user/calendar", Active: true},
		{Realm: "platform", Tier: "internal", Name: "User Management", Icon: "users-cog", Path: "/admin/users", MinRole: "manager", Active: true},
		{Realm: "platform", Tier: "internal", Name: "Module Management", Icon: "puzzle-piece", Path: "/admin/modules", MinRole: "administrator", Active: true},
	}
}

func (m *UserModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "user.read", Module: "user", Description: "List and read users"},
		{Key: "user.create", Module: "user", Description: "Create and invite users"},
		{Key: "user.update", Module: "user", Description: "Update user profiles, roles and status"},
		{Key: "user.delete", Module: "user", Description: "Delete users"},
		{Key: "user.self", Module: "user", Description: "Read and save your own per-module preferences; discover your profile sections"},
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
	// The auth module initialises after this one (registry topological
	// order), so its iface.SessionTerminator cannot be resolved here.
	// Hand the handler the registry instead and let it resolve at
	// request time — deactivate / delete use it to end the target's
	// sessions instead of leaving live bearers behind.
	m.handler.SetServiceRegistry(deps.Services)
	// The caller-role guards (D28) resolve the acting operator from the
	// store. A synthetic dev-token principal has no row by design, so the
	// guards need the environment classification to decide whether that
	// documented local flow may fall back to the token's own claim — see
	// handlers.devTokenSystemRole. Wiring it is what opens the exception;
	// an un-wired handler keeps it shut.
	m.handler.SetPlatform(deps.Platform)
	m.adminClientHandler = handlers.NewAdminClientUserHandler(clientSvc, deps.Services)
	m.adminClientHandler.SetPlatform(deps.Platform)
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
	// Per-user preferences: the schema catalog is the SDK registry the module
	// registry created before Init and fills after it, so resolving the
	// pointer here is safe and its content is read at request time. Module
	// enabled/disabled is read from the config service at request time too
	// (wired in RegisterRoutes, where RouteInfo carries it).
	prefRepo := repository.NewPreferenceRepository(deps.DB)
	m.preferenceRepo = prefRepo
	if prefReg, ok := module.GetTyped[*module.UserPreferenceRegistry](deps.Services, module.ServiceUserPreferenceRegistry); ok {
		m.preferenceRegistry = prefReg
	} else {
		m.preferenceRegistry = module.NewUserPreferenceRegistry()
	}
	m.services = deps.Services

	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(canonicalRepo, prefRepo))
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

// userRouteGroup binds one slice of the user-management surface to the
// permission that gates it (plus whether it also needs a fresh step-up
// proof). Data rather than inline middleware calls so
// route_permissions_test.go can register each group on a throwaway API
// and assert which operation landed under which key: "read means read" is
// checked, not reviewed.
type userRouteGroup struct {
	Permission string
	StepUp     bool
	Register   func(huma.API)
}

// routeGroups is the per-permission map of the platform user-management
// surface, operator and client tiers alike (the keys are the same for
// both: a user admin is a user admin whichever collection the row lives
// in). Users are global, so every group is gated with
// RequireSystemPermission — evaluated at global scope, where the caller's
// platform role conveys its seeded permission set (authz rule 2b) and a
// tenant membership never does.
//
//   - user.read   — every GET.
//   - user.create — POST /v1/users and the client-tier create/invite/
//     resend/resend-verification mails; the role-escalation guard in the
//     handlers refuses privilege seeding, so no step-up.
//   - user.update — PUT /v1/users/{id}, PATCH client user, both under
//     RequireStepUp(5m) (role change, deactivate); plus send-password-reset,
//     which mails a credential link to an EXISTING user: an update by
//     nature, but no state changes until the recipient redeems it, so it
//     stays outside the step-up group.
//   - user.delete — both DELETEs, under RequireStepUp(5m).
//
// The handlers add the target-tier guard on every update/delete: holding
// a key never lets a lower tier touch a higher one.
func (m *UserModule) routeGroups() []userRouteGroup {
	return []userRouteGroup{
		{Permission: "user.read", Register: func(api huma.API) {
			RegisterReadRoutes(api, m.handler)
			RegisterAdminClientReadRoutes(api, m.adminClientHandler)
			if m.preferenceHandler != nil {
				handlers.RegisterProfileSectionReadRoute(api, m.preferenceHandler)
			}
		}},
		{Permission: "user.create", Register: func(api huma.API) {
			RegisterCreateRoutes(api, m.handler)
			RegisterAdminClientCreateRoutes(api, m.adminClientHandler)
		}},
		{Permission: "user.update", Register: func(api huma.API) {
			RegisterAdminClientPasswordResetRoute(api, m.adminClientHandler)
		}},
		{Permission: "user.update", StepUp: true, Register: func(api huma.API) {
			RegisterUpdateRoutes(api, m.handler)
			RegisterAdminClientUpdateRoutes(api, m.adminClientHandler)
		}},
		{Permission: "user.delete", StepUp: true, Register: func(api huma.API) {
			RegisterDeleteRoutes(api, m.handler)
			RegisterAdminClientDeleteRoutes(api, m.adminClientHandler)
		}},
	}
}

// registrarFor returns the registrar of the group bound to (permission,
// stepUp). Panics on an unknown pair: that can only mean RegisterRoutes
// and routeGroups have drifted, a programming error to surface at boot,
// not a route to silently leave unmounted.
func (m *UserModule) registrarFor(permission string, stepUp bool) func(huma.API) {
	for _, g := range m.routeGroups() {
		if g.Permission == permission && g.StepUp == stepUp {
			return g.Register
		}
	}
	panic("user: no route group declared for " + permission)
}

// buildPreferenceHandler wires the self-service preference store once the
// RouteInfo (config service for enabled checks) is known. Idempotent.
func (m *UserModule) buildPreferenceHandler(ri *module.RouteInfo) {
	if m.preferenceHandler != nil || m.preferenceRepo == nil {
		return
	}
	var enabled services.ModuleEnabledChecker
	if ri.ConfigService != nil {
		enabled = ri.ConfigService.IsEnabled
	}
	var authz iface.AuthzProvider
	if m.services != nil {
		if p, ok := module.GetTyped[iface.AuthzProvider](m.services, module.ServiceAuthzProvider); ok {
			authz = p
		}
	}
	svc := services.NewPreferenceService(m.preferenceRepo, m.preferenceRegistry, enabled)
	m.preferenceHandler = handlers.NewPreferenceHandler(svc, m.preferenceRegistry, enabled, authz)
}

func (m *UserModule) RegisterRoutes(ri *module.RouteInfo) {
	m.buildPreferenceHandler(ri)
	// User management is a platform-level concern: users are global, so
	// every group sits on the system-permission gate (global scope), one
	// per-action key each — see routeGroups. The permission literals stay
	// written out here (not looped) so policycoverage can see which key
	// gates which group. The step-up groups add RequireStepUp(5m) so a
	// long-lived session can't destructively mutate platform state hours
	// after the last MFA proof; the SPA's StepUpModal catches the 401 and
	// prompts the caller to re-verify.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("user.read"))
		m.registrarFor("user.read", false)(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("user.create"))
		m.registrarFor("user.create", false)(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("user.update"))
		m.registrarFor("user.update", false)(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("user.update"))
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		m.registrarFor("user.update", true)(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("user.delete"))
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		m.registrarFor("user.delete", true)(humachi.New(r, ri.APIConfig))
	})

	// Self-service avatar surface — mounted on BOTH audiences so a
	// Tier-2 client can change their own avatar on api.* just like a
	// Tier-1 operator does on console.*. Gate is the per-action
	// permission `user.avatar.self` (matches the .self suffix rule
	// granted to operator / manager / developer / administrator /
	// super_admin; guest does NOT carry it because guest is read-only
	// across the catalog). The handler additionally enforces owner-
	// self via the JWT user UUID, so a misconfigured role grant still
	// can't let user A edit user B's avatar.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("user.avatar.self"))
		api := humachi.New(r, ri.APIConfig)
		registerAvatarRoutes(api, m.operatorAvatarHandler, "operator-")
	})
	if ri.Client != nil && ri.Client.ProtectedRouter != nil && m.clientAvatarHandler != nil {
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequirePermission("user.avatar.self"))
			api := humachi.New(r, ri.APIConfig)
			registerAvatarRoutes(api, m.clientAvatarHandler, "client-")
		})
	}

	// Self-service preferences and profile-section discovery — both
	// audiences, like the avatar, on the per-action key `user.self`
	// (conveyed by the .self suffix rule to every platform role but guest
	// and to org members). The store is user-keyed, so the one handler
	// serves both tiers.
	if m.preferenceHandler != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequirePermission("user.self"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterSelfPreferenceRoutes(api, m.preferenceHandler, "operator-")
		})
		if ri.Client != nil && ri.Client.ProtectedRouter != nil {
			ri.Client.ProtectedRouter.Group(func(r chi.Router) {
				r.Use(ri.Client.AuthMW.RequirePermission("user.self"))
				api := humachi.New(r, ri.APIConfig)
				handlers.RegisterSelfPreferenceRoutes(api, m.preferenceHandler, "client-")
			})
		}
	}
}
