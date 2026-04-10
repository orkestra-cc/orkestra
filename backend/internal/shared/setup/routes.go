package setup

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
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
func NewHandler(svc *Service, cookieCfg config.CookieConfig) *Handler {
	name := cookieCfg.Name
	if name == "" {
		name = "access_token"
	}
	return &Handler{
		svc:          svc,
		cookieName:   name,
		cookieDomain: cookieCfg.Domain,
		cookieSecure: cookieCfg.Secure,
	}
}

// --- GET /v1/setup/status ---

type StatusResponse struct {
	Body Status
}

func (h *Handler) Status(ctx context.Context, _ *struct{}) (*StatusResponse, error) {
	return &StatusResponse{Body: h.svc.Status(ctx)}, nil
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
	resp.SetCookie = buildRefreshCookie(h.cookieName, tokens.RefreshToken, h.cookieDomain, h.cookieSecure)
	resp.Body.Success = true
	resp.Body.AccessToken = tokens.AccessToken
	resp.Body.TokenType = tokens.TokenType
	resp.Body.ExpiresIn = tokens.ExpiresIn
	resp.Body.User = tokens.User
	return resp, nil
}

// --- registration ---

// RegisterRoutes mounts the setup endpoints on the provided public Huma API.
// Both routes are unauthenticated — the invariant that protects them is
// "no users exist yet," enforced inside Service.CreateInitialAdmin.
func (h *Handler) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "setup-status",
		Method:      http.MethodGet,
		Path:        "/v1/setup/status",
		Summary:     "Check whether the system has been initialized",
		Description: "Returns `setupCompleted` (at least one user exists) and `smtpConfigured` (notification module has real SMTP credentials). Used by the frontend SetupGate to decide whether to route to the onboarding wizard.",
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
func buildRefreshCookie(name, value, domain string, secure bool) string {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Domain:   domain,
		MaxAge:   7 * 24 * 3600,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}
