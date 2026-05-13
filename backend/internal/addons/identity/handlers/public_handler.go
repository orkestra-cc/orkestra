package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/identity/repository"
	"github.com/orkestra/backend/internal/addons/identity/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/utils"
)

// PublicHandler exposes the anonymous OIDC login endpoints.
//
//   - GET  /v1/identity/oidc/{tenantSlug}/start
//     Looks up the tenant's IdP config and returns the IdP's authorization
//     URL along with the state token. Frontends are expected to 302 the
//     browser there; XHR clients can take the URL as JSON and navigate
//     themselves.
//   - GET  /v1/identity/oidc/callback
//     The redirect URI registered at the IdP. Validates state, exchanges
//     the code, verifies the ID token, finds-or-creates the user, mints
//     Orkestra tokens, and returns them as JSON. In a browser SPA flow
//     the frontend handles the redirect after receiving the tokens —
//     handing them off via URL fragment was deemed out of scope for v1.
type PublicHandler struct {
	svc *services.Service
}

// NewPublicHandler wires the handler.
func NewPublicHandler(svc *services.Service) *PublicHandler {
	return &PublicHandler{svc: svc}
}

// --- Start ---

type StartRequest struct {
	TenantSlug string `path:"tenantSlug" doc:"Tenant slug (or UUID in v1) whose IdP should be used"`
	RedirectTo string `query:"redirect_to" doc:"Optional URL the frontend should land on after callback"`
}

type StartResponseBody struct {
	AuthURL string `json:"authUrl"`
	State   string `json:"state"`
}

type StartResponse struct {
	Body StartResponseBody
}

func (h *PublicHandler) Start(ctx context.Context, req *StartRequest) (*StartResponse, error) {
	res, err := h.svc.StartLogin(ctx, services.StartInput{
		TenantSlug: req.TenantSlug,
		RedirectTo: req.RedirectTo,
	})
	if err != nil {
		return nil, mapPublicError(err)
	}
	return &StartResponse{Body: StartResponseBody{AuthURL: res.AuthURL, State: res.State}}, nil
}

// --- Callback ---

type CallbackRequest struct {
	Code  string `query:"code" doc:"OIDC authorization code returned by the IdP"`
	State string `query:"state" doc:"State token issued by /start"`
}

type CallbackResponseBody struct {
	Success      bool                               `json:"success"`
	AccessToken  string                             `json:"accessToken"`
	RefreshToken string                             `json:"refreshToken"`
	TokenType    string                             `json:"tokenType"`
	ExpiresIn    int64                              `json:"expiresIn"`
	User         *userModels.UserManagementResponse `json:"user,omitempty"`
	RedirectTo   string                             `json:"redirectTo,omitempty"`
}

type CallbackResponse struct {
	Body CallbackResponseBody
}

func (h *PublicHandler) Callback(ctx context.Context, req *CallbackRequest) (*CallbackResponse, error) {
	res, err := h.svc.Callback(ctx, services.CallbackInput{
		Code:     req.Code,
		State:    req.State,
		IP:       clientIPFromCtx(ctx),
		Platform: "web",
	})
	if err != nil {
		return nil, mapPublicError(err)
	}
	body := CallbackResponseBody{
		Success:      true,
		TokenType:    res.Tokens.TokenType,
		ExpiresIn:    res.Tokens.ExpiresIn,
		AccessToken:  res.Tokens.AccessToken,
		RefreshToken: res.Tokens.RefreshToken,
		User:         res.Tokens.User,
		RedirectTo:   res.RedirectTo,
	}
	return &CallbackResponse{Body: body}, nil
}

// RegisterPublicRoutes mounts the unauthenticated OIDC endpoints on the
// shared public API. The routes are deliberately outside /v1/auth/* so
// the identity module owns the namespace cleanly; a future SAML surface
// will share the `/v1/identity/` prefix.
func RegisterPublicRoutes(api huma.API, h *PublicHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "identity-oidc-start",
		Method:      http.MethodGet,
		Path:        "/v1/identity/oidc/{tenantSlug}/start",
		Summary:     "Start an OIDC login against a tenant's configured IdP",
		Tags:        []string{"Identity"},
	}, h.Start)

	huma.Register(api, huma.Operation{
		OperationID: "identity-oidc-callback",
		Method:      http.MethodGet,
		Path:        "/v1/identity/oidc/callback",
		Summary:     "Complete an OIDC login: exchanges the code, mints an Orkestra session",
		Tags:        []string{"Identity"},
	}, h.Callback)
}

func mapPublicError(err error) error {
	switch {
	case errors.Is(err, services.ErrIdPDisabled),
		errors.Is(err, repository.ErrIdPConfigNotFound):
		return huma.Error404NotFound("no OIDC configuration is available for this tenant")
	case errors.Is(err, services.ErrInvalidState):
		return huma.Error400BadRequest("invalid or expired state")
	default:
		return huma.Error400BadRequest(err.Error())
	}
}

func clientIPFromCtx(ctx context.Context) string {
	if r, ok := ctx.Value("http_request").(*http.Request); ok && r != nil {
		return utils.GetClientIP(r)
	}
	return ""
}
