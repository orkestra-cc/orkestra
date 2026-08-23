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

// --- GET /v1/setup/finalization-access (stub — Task 5.4 implements) ---

type FinalizationAccessResponse struct {
	Body struct{}
}

// FinalizationAccess is a stub: the route, its authenticated-operator
// mount, and its OpenAPI operation are locked in by this task so Task 5.4
// only has to fill in the body. A stub must fail closed — 501, never a
// fabricated 200 — so nothing downstream can mistake "not implemented
// yet" for "access granted."
func (h *Handler) FinalizationAccess(_ context.Context, _ *struct{}) (*FinalizationAccessResponse, error) {
	return nil, huma.Error501NotImplemented("Setup finalization access is not implemented yet.")
}

// --- POST /v1/setup/finalize (stub — Task 5.5 implements) ---

type FinalizeRequest struct {
	Body struct{}
}

type FinalizeResponse struct {
	Body struct{}
}

// Finalize is a stub for the same reason FinalizationAccess is — Task 5.5
// fills in the body against the mount point this task establishes.
func (h *Handler) Finalize(_ context.Context, _ *FinalizeRequest) (*FinalizeResponse, error) {
	return nil, huma.Error501NotImplemented("Setup finalization is not implemented yet.")
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
		Description: "Authenticated-operator route. Reports whether the calling operator is bound to the in-progress setup finalization saga (default-tenant provisioning) and may call POST /v1/setup/finalize. Stub pending Task 5.4 — currently always answers 501.",
		Tags:        []string{"Setup"},
	}, h.FinalizationAccess)

	huma.Register(api, huma.Operation{
		OperationID: "setup-finalize",
		Method:      http.MethodPost,
		Path:        "/v1/setup/finalize",
		Summary:     "Finalize setup (default tenant provisioning)",
		Description: "Authenticated-operator route. Drives the resumable setup-finalization saga to completion for the caller bound to it. Stub pending Task 5.5 — currently always answers 501.",
		Tags:        []string{"Setup"},
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
