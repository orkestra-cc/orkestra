package setup

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// Handler binds the setup service to Huma v2 HTTP routes.
type Handler struct {
	svc          *Service
	cookieName   string
	cookieDomain string
	cookieSecure bool
}

// NewHandler constructs the HTTP adapter. Cookie settings come from the
// shared config so the refresh cookie matches what /v1/auth/login would emit.
// Setup is mounted on the operator host, so the cookie carries the operator
// refresh-cookie domain (ADR-0003 PR-D D-9).
func NewHandler(svc *Service, cookieCfg config.CookieConfig) *Handler {
	name := cookieCfg.Name
	if name == "" {
		name = "orkestra_cookie"
	}
	return &Handler{
		svc:          svc,
		cookieName:   name,
		cookieDomain: cookieCfg.OperatorDomain,
		cookieSecure: cookieCfg.Secure,
	}
}

// --- GET /v1/setup/status ---

type StatusResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         Status
}

// Status maps the service's fail-closed contract onto HTTP: a read error
// becomes a 503 with a stable code and Retry-After — never an inferred
// phase — while a successful read is marked non-cacheable so the wizard
// never renders a stale phase from an intermediary or browser cache.
func (h *Handler) Status(ctx context.Context, _ *struct{}) (*StatusResponse, error) {
	st, err := h.svc.Status(ctx)
	if err != nil {
		return nil, huma.ErrorWithHeaders(
			errcode.ServiceUnavailable(errcode.SetupStatusUnavailable, "The setup state store is unavailable. Retry shortly."),
			http.Header{"Retry-After": []string{"5"}, "Cache-Control": []string{"no-store"}},
		)
	}
	return &StatusResponse{CacheControl: "no-store", Body: st}, nil
}

// --- POST /v1/setup/admin ---

type CreateAdminRequest struct {
	Body struct {
		Email    string `json:"email" doc:"Email address" format:"email"`
		Password string `json:"password" doc:"New password (min 10 chars)"`
		FullName string `json:"fullName" doc:"Full name"`
	}
}

type CreateAdminResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success     bool        `json:"success"`
		AccessToken string      `json:"accessToken"`
		TokenType   string      `json:"tokenType"`
		ExpiresIn   int64       `json:"expiresIn"`
		User        interface{} `json:"user"`
	}
}

func (h *Handler) CreateAdmin(ctx context.Context, req *CreateAdminRequest) (*CreateAdminResponse, error) {
	ip := clientIPFromCtx(ctx)
	tokens, err := h.svc.CreateInitialAdmin(ctx, req.Body.Email, req.Body.Password, req.Body.FullName, ip)
	if err != nil {
		return nil, mapSetupError(err)
	}

	resp := &CreateAdminResponse{}
	resp.SetCookie = buildRefreshCookie(h.cookieName, tokens.RefreshToken, h.cookieDomain, h.cookieSecure,
		int(h.svc.RefreshTokenTTL().Seconds()))
	resp.Body.Success = true
	resp.Body.AccessToken = tokens.AccessToken
	resp.Body.TokenType = tokens.TokenType
	resp.Body.ExpiresIn = tokens.ExpiresIn
	resp.Body.User = tokens.User
	return resp, nil
}

// --- GET /v1/setup/finalization-access ---

type FinalizationAccessResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         FinalizationAccess
}

// FinalizationAccess reports whether the calling operator may finalize the
// in-progress setup saga, or claim recovery of an unusable binding — a
// pure read against the coordinator record and the bound administrator's
// lifecycle state. The wizard calls this before rendering an actionable
// organization form so an operator who cannot submit is never shown a
// form the finalize POST would reject. It never mutates the coordinator
// and never emits an audit event; the actual claim happens only on the
// finalize POST (Task 5.5), which shares evaluateAccess for its
// authorization decision.
//
// Gating: the probe is meaningful only while setup is tenant_required.
// Phase is checked BEFORE evaluateAccess runs — deliberately, so that once
// setup is complete a lifecycle lookup failure on the now-irrelevant bound
// admin can never surface as a spurious 503 instead of the correct 409.
func (h *Handler) FinalizationAccess(ctx context.Context, _ *struct{}) (*FinalizationAccessResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	systemRole, _ := ctxauth.GetSystemRole(ctx)

	st, err := h.svc.Status(ctx)
	if err != nil {
		return nil, finalizerStateUnavailable()
	}
	if st.Phase == PhaseComplete {
		return nil, errcode.Conflict(errcode.SetupAlreadyCompleted, "Setup is already complete.")
	}

	access, _, err := h.svc.evaluateAccess(ctx, userUUID, systemRole)
	if err != nil {
		return nil, finalizerStateUnavailable()
	}
	return &FinalizationAccessResponse{CacheControl: "no-store", Body: access}, nil
}

// finalizerStateUnavailable is the stable 503 mapping for a coordinator or
// bound-user lookup failure — shared by the probe above and (Task 5.5) the
// finalize POST. It is never returned in a way that could be mistaken for
// a recovery opportunity: it fails the request outright.
func finalizerStateUnavailable() error {
	return huma.ErrorWithHeaders(
		errcode.ServiceUnavailable(errcode.SetupFinalizerStateUnavailable, "Finalizer state is temporarily unavailable. Retry shortly."),
		http.Header{"Retry-After": []string{"5"}, "Cache-Control": []string{"no-store"}},
	)
}

// --- POST /v1/setup/finalize ---

type FinalizeRequest struct {
	Body struct {
		TenantName string `json:"tenantName" minLength:"1" maxLength:"120" doc:"Initial Tier-1 organization name"`
		TenantSlug string `json:"tenantSlug" minLength:"1" maxLength:"48" pattern:"^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$" doc:"URL slug for the initial Tier-1 organization"`
		// Required POINTER bool. An omitted value is a 422 schema error —
		// it can never silently mean false, because false is the
		// consequential choice here (single mode permanently caps Tier-1
		// at one tenant).
		AllowAdditionalInternalTenants *bool `json:"allowAdditionalInternalTenants" required:"true" doc:"true → manual provisioning mode (more Tier-1 tenants may be created later), false → single mode"`
	}
}

// FinalizeResponseBody serves both terminal shapes. Every member is
// omitempty so the 202 serializes as exactly {"state":"…"} — a typed
// accepted body, NOT a Problem Details envelope — while the 200 carries
// the result snapshot and no state.
type FinalizeResponseBody struct {
	State                          string `json:"state,omitempty" doc:"Set to setup.finalization_in_progress on a 202 accepted response; absent on the terminal 200"`
	TenantID                       string `json:"tenantId,omitempty" doc:"UUID of the created Tier-1 tenant"`
	TenantName                     string `json:"tenantName,omitempty" doc:"Normalized tenant name"`
	TenantSlug                     string `json:"tenantSlug,omitempty" doc:"Normalized tenant slug"`
	Mode                           string `json:"mode,omitempty" doc:"Selected Tier-1 provisioning mode: manual | single"`
	AllowAdditionalInternalTenants *bool  `json:"allowAdditionalInternalTenants,omitempty" doc:"Derived from mode == manual"`
}

// finalizeOutput carries a dynamic status: 200 for a terminal result
// (fresh completion or authorized replay) and 202 for "an identical
// request already holds the stage lease".
type finalizeOutput struct {
	Status       int
	RetryAfter   string `header:"Retry-After"`
	CacheControl string `header:"Cache-Control"`
	Body         FinalizeResponseBody
}

// Finalize drives the resumable finalization saga.
//
// The 202 is the one non-obvious shape: it is a SUCCESS (no error
// returned), so the frontend never has to parse a Huma error envelope for
// the "somebody else is already running my exact request" path. It honors
// Retry-After, reloads setup status, and retries the identical payload.
func (h *Handler) Finalize(ctx context.Context, req *FinalizeRequest) (*finalizeOutput, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	systemRole, _ := ctxauth.GetSystemRole(ctx)

	if req.Body.AllowAdditionalInternalTenants == nil {
		// Defense in depth: the schema marks this required, so Huma
		// rejects an omitted value before the handler runs. Reaching here
		// would mean the field was decoded as absent anyway — never treat
		// that as false.
		return nil, huma.Error422UnprocessableEntity("allowAdditionalInternalTenants is required and must be true or false.")
	}

	res, err := h.svc.Finalize(ctx, userUUID, systemRole, FinalizeInput{
		TenantName:                     req.Body.TenantName,
		TenantSlug:                     req.Body.TenantSlug,
		AllowAdditionalInternalTenants: *req.Body.AllowAdditionalInternalTenants,
	})
	if err != nil {
		if errors.Is(err, ErrFinalizationInProgress) {
			return &finalizeOutput{
				Status:       http.StatusAccepted,
				RetryAfter:   "3",
				CacheControl: "no-store",
				Body:         FinalizeResponseBody{State: finalizeStateInProgress},
			}, nil
		}
		return nil, mapFinalizeError(h.svc.logger, err)
	}

	allow := res.Mode == modeManual
	return &finalizeOutput{
		Status:       http.StatusOK,
		CacheControl: "no-store",
		Body: FinalizeResponseBody{
			TenantID:                       res.TenantUUID,
			TenantName:                     res.TenantName,
			TenantSlug:                     res.TenantSlug,
			Mode:                           res.Mode,
			AllowAdditionalInternalTenants: &allow,
		},
	}, nil
}

// mapFinalizeError is the finalize error table. Every client-facing
// detail is a fixed written sentence — the underlying error goes to the
// log, never to the caller.
func mapFinalizeError(logger *slog.Logger, err error) error {
	if logger == nil {
		logger = slog.Default()
	}

	// Tenant-layer failures arrive pre-classified through the seam
	// adapter cmd/server wires (see SeamError): shared/setup never
	// matches a tenant sentinel or its text.
	var seam *SeamError
	if errors.As(err, &seam) {
		switch seam.Kind {
		case SeamSlugConflict:
			return errcode.Conflict(errcode.TenantSlugAlreadyInUse,
				"That organization slug is already in use. Choose a different slug.")
		case SeamProvisioningLocked:
			return errcode.Conflict(errcode.TenantProvisioningLocked,
				"Tier-1 provisioning is locked to a single organization and one already exists.")
		case SeamRemediation:
			logger.Warn("setup: finalization requires operator remediation", "error", err.Error())
			return huma.Error409Conflict(
				"The reserved setup organization was archived or purged and cannot be restored automatically. Operator remediation is required.")
		case SeamIdentityConflict:
			logger.Error("setup: reserved setup tenant identity conflict", "error", err.Error())
			return huma.Error500InternalServerError(
				"The reserved setup organization does not match this setup request. Check the server logs.")
		default:
			logger.Error("setup: unclassified tenant seam failure", "error", err.Error())
			return huma.Error500InternalServerError(
				"Setup finalization failed while provisioning the organization. Check the server logs.")
		}
	}

	switch {
	case errors.Is(err, ErrFinalizerStateUnavailable):
		logger.Warn("setup: finalizer state unavailable", "error", err.Error())
		return finalizerStateUnavailable()
	case errors.Is(err, ErrFinalizerBoundToAnotherAdmin):
		return errcode.Forbidden(errcode.SetupFinalizerBoundToAnotherAdmin,
			"Setup finalization is reserved for a different administrator account.")
	case errors.Is(err, ErrRecoveryRequiresSuperAdmin):
		return errcode.Forbidden(errcode.SetupRecoveryRequiresSuperAdmin,
			"Recovering setup finalization requires an active super administrator.")
	case errors.Is(err, ErrFinalizationAlreadyStarted):
		return errcode.Conflict(errcode.SetupFinalizationAlreadyStarted,
			"A different setup finalization request is already in progress.")
	case errors.Is(err, ErrFinalizationAlreadyCompleted):
		return errcode.Conflict(errcode.SetupAlreadyCompleted,
			"Setup is already complete.")
	default:
		// An error the handler cannot name is a server fault, never the
		// caller's — and its text stays in the log.
		logger.Error("setup: finalization failed", "error", err.Error())
		return huma.Error500InternalServerError(
			"Setup finalization could not be completed. Retry the same request; the saga resumes where it stopped.")
	}
}

// --- registration ---

// RegisterPublicRoutes mounts the two unauthenticated setup endpoints —
// status and create-admin — on the provided public Huma API. Both routes
// are unauthenticated by design: the invariant that protects them is "no
// users exist yet," enforced inside Service.CreateInitialAdmin.
//
// Do not add anything else here. A route that needs an authenticated
// caller belongs on RegisterProtectedRoutes instead, even though it may
// share this package's /v1/setup/ path prefix — that prefix's entry in
// shared/middleware.PublicRoutes is a tenant-baggage/span-coverage
// exemption (no tenant exists yet during setup), not a statement that
// everything under it is reachable anonymously. See the comment on that
// entry.
func (h *Handler) RegisterPublicRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "setup-status",
		Method:      http.MethodGet,
		Path:        "/v1/setup/status",
		Summary:     "Check whether the system has been initialized",
		Description: "Returns the authoritative setup `phase` (`admin_required` | `tenant_required` | `complete`), the derived `setupCompleted` (phase == complete, kept for backward compatibility), and `smtpConfigured` (notification module has real SMTP credentials). A failure to read the authoritative phase answers 503 with Retry-After rather than an inferred phase. Used by the frontend SetupGate to decide whether to route to the onboarding wizard.",
		Tags:        []string{"Setup"},
	}, h.Status)

	huma.Register(api, huma.Operation{
		OperationID: "setup-create-admin",
		Method:      http.MethodPost,
		Path:        "/v1/setup/admin",
		Summary:     "Create the first administrator (first-install only)",
		Description: "Creates the initial developer-role user during the first-install wizard. Returns 409 Conflict once any user exists. Email verification is bypassed because this endpoint runs before SMTP can be configured.",
		Tags:        []string{"Setup"},
	}, h.CreateAdmin)
}

// RegisterProtectedRoutes mounts the authenticated-operator setup
// endpoints — finalization access and finalize — on the given Huma API.
// This method registers operations only; it enforces nothing about
// authentication itself. The caller MUST build api from a router mounted
// behind RequireAuth (see cmd/server/main.go's operatorProtected group),
// exactly the way every other authenticated-operator route is wired.
// Never register these on the same API RegisterPublicRoutes uses.
//
// Both routes still fall under the /v1/setup/ prefix in
// shared/middleware.PublicRoutes, but — as that entry's comment says —
// the prefix is a tenant-baggage exemption, not an authentication
// statement: no tenant exists yet at this point in the bootstrap flow,
// authenticated or not.
func (h *Handler) RegisterProtectedRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "setup-finalization-access",
		Method:      http.MethodGet,
		Path:        "/v1/setup/finalization-access",
		Summary:     "Check whether the caller may finalize setup",
		Description: "Authenticated-operator route. Reports whether the calling operator may finalize the in-progress setup saga (default-tenant provisioning) via POST /v1/setup/finalize, or claim recovery of an unusable binding. Read-only: never mutates the coordinator or emits an audit event. Returns 409 setup.already_completed once setup is complete, and 503 setup.finalizer_state_unavailable on a coordinator/lifecycle lookup failure.",
		Tags:        []string{"Setup"},
	}, h.FinalizationAccess)

	huma.Register(api, huma.Operation{
		OperationID:   "setup-finalize",
		Method:        http.MethodPost,
		Path:          "/v1/setup/finalize",
		Summary:       "Finalize setup (default tenant provisioning)",
		Description:   "Authenticated-operator route. Drives the resumable setup-finalization saga for the administrator bound to it: persists the Tier-1 provisioning mode, ensures the reserved internal tenant, assigns it as the platform default, and marks setup complete. Idempotent — retrying the identical payload resumes the saga, and retrying after completion replays the persisted result. Answers 202 with `{\"state\":\"setup.finalization_in_progress\"}` plus Retry-After when an identical request already holds the stage lease (a success, not an error envelope), 403 when the caller is not the bound administrator or may not claim recovery, 409 for a different payload or an already-completed setup, and 503 when coordinator state cannot be read.",
		DefaultStatus: http.StatusOK,
		Responses: map[string]*huma.Response{
			"202": {
				Description: "Accepted — an identical finalization request already holds the current stage lease. Retry the identical payload after Retry-After seconds.",
				Headers: map[string]*huma.Param{
					"Retry-After": {Description: "Seconds to wait before retrying the identical payload", Schema: &huma.Schema{Type: huma.TypeString}},
				},
				Content: map[string]*huma.MediaType{
					"application/json": {Schema: &huma.Schema{Ref: "#/components/schemas/FinalizeResponseBody"}},
				},
			},
		},
		Tags: []string{"Setup"},
	}, h.Finalize)
}

// --- helpers ---

func mapSetupError(err error) error {
	switch {
	case errors.Is(err, ErrAlreadyCompleted):
		return huma.Error409Conflict("Setup already completed — an administrator account exists.")
	case errors.Is(err, authServices.ErrPasswordTooShort),
		errors.Is(err, authServices.ErrPasswordTooLong),
		errors.Is(err, authServices.ErrPasswordContainsEmail),
		errors.Is(err, authServices.ErrPasswordBreached):
		return huma.Error400BadRequest(err.Error())
	default:
		slog.Default().Warn("setup: create admin failed", slog.String("error", err.Error()))
		return huma.Error400BadRequest("Could not create administrator account")
	}
}

func clientIPFromCtx(ctx context.Context) string {
	if r, ok := ctx.Value("http_request").(*http.Request); ok && r != nil {
		return utils.GetClientIP(r)
	}
	return ""
}

// buildRefreshCookie mirrors the helper in auth/handlers/password_handler.go
// so the cookie emitted by POST /v1/setup/admin matches POST /v1/auth/login.
func buildRefreshCookie(name, value, domain string, secure bool, maxAgeSeconds int) string {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Domain:   domain,
		MaxAge:   maxAgeSeconds,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}
