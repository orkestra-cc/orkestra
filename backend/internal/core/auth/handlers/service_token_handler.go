package handlers

// Task 8: exposes ServiceAccountService.Grant over HTTP as an OAuth2
// client-credentials token endpoint for machine principals. Mounted on
// the operator PublicAPI only — service accounts are an operator-tier
// concept (Task 1/5/6/7), there is no client-tier equivalent.

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/auth/services"
)

// ServiceTokenHandler wraps ServiceAccountService.Grant with an HTTP
// binding for the OAuth2 client-credentials grant.
type ServiceTokenHandler struct {
	svc *services.ServiceAccountService
}

// NewServiceTokenHandler constructs the handler over the shared
// ServiceAccountService instance built in module.go's Init.
func NewServiceTokenHandler(svc *services.ServiceAccountService) *ServiceTokenHandler {
	return &ServiceTokenHandler{svc: svc}
}

type ServiceTokenRequest struct {
	Body struct {
		GrantType    string `json:"grantType" required:"true" enum:"client_credentials" doc:"OAuth2 grant type; only client_credentials is supported"`
		ClientID     string `json:"clientId" required:"true"`
		ClientSecret string `json:"clientSecret" required:"true"`
	}
}

type ServiceTokenResponse struct {
	Body struct {
		AccessToken string `json:"accessToken"`
		TokenType   string `json:"tokenType"`
		ExpiresIn   int    `json:"expiresIn" doc:"Access-token lifetime in seconds; no refresh token is issued — repeat the grant"`
	}
}

// Token exchanges a service account's clientId+clientSecret for a
// short-lived access token minted with aud="service".
func (h *ServiceTokenHandler) Token(ctx context.Context, req *ServiceTokenRequest) (*ServiceTokenResponse, error) {
	res, err := h.svc.Grant(ctx, services.GrantInput{
		GrantType: req.Body.GrantType, ClientID: req.Body.ClientID,
		ClientSecret: req.Body.ClientSecret, IP: clientIPFromCtx(ctx),
	})
	if err != nil {
		return nil, mapServiceTokenError(err)
	}
	resp := &ServiceTokenResponse{}
	resp.Body.AccessToken = res.AccessToken
	resp.Body.TokenType = "Bearer"
	resp.Body.ExpiresIn = res.ExpiresIn
	return resp, nil
}

// mapServiceTokenError translates ServiceAccountService.Grant's typed
// sentinels to the matching HTTP status. Anything unrecognized passes
// through unchanged (default 500 via Huma's fallback), matching the
// auth package's existing mapXError idiom.
func mapServiceTokenError(err error) error {
	switch {
	case errors.Is(err, services.ErrUnsupportedGrantType):
		return huma.Error400BadRequest("unsupported grant type")
	case errors.Is(err, services.ErrClientRateLimited):
		return huma.Error429TooManyRequests("Too many failed attempts. Please try again later.")
	case errors.Is(err, services.ErrInvalidClientCredentials):
		return huma.Error401Unauthorized("invalid client credentials")
	default:
		return err
	}
}

// RegisterPublicRoutes mounts the single client-credentials grant
// endpoint. Operator-tier only — there is no /v1/auth/{tier}/token
// split; unlike the password/OAuth handlers this takes the API
// directly, no RouteMount, since the path has no per-tier prefix.
func (h *ServiceTokenHandler) RegisterPublicRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-service-token",
		Method:      http.MethodPost,
		Path:        "/v1/auth/token",
		Summary:     "OAuth2 client-credentials token grant for service accounts",
		Tags:        []string{"Authentication"},
	}, h.Token)
}
