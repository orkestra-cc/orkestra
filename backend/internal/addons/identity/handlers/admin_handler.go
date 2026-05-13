// Package handlers wires the identity service to Huma request/response
// shapes. Split into two files: admin_handler.go (authenticated,
// tenant-scoped CRUD on IdPConfig) and public_handler.go (anonymous OIDC
// start + callback endpoints).
package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/orkestra-cc/orkestra-addon-identity/internal/cryptoutil"
	"github.com/orkestra-cc/orkestra-addon-identity/models"
	"github.com/orkestra-cc/orkestra-addon-identity/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// AdminHandler exposes tenant-scoped IdP config CRUD to authenticated
// tenant administrators. All routes require the `tenant.update`
// permission — the same gate the tenant module uses for its own mutation
// surface — so only operators who can already manage the tenant can edit
// its IdP.
type AdminHandler struct {
	repo      *repository.Repository
	auditSink iface.AuditSink
}

// NewAdminHandler wires the admin handler. The repo is the only
// collaborator; admin CRUD does not touch OIDC discovery, the login flow
// handles that at request time.
func NewAdminHandler(repo *repository.Repository) *AdminHandler {
	return &AdminHandler{repo: repo}
}

// SetAuditSink wires the compliance audit sink post-construction. Called
// from the compliance module's Init after both modules are up.
func (h *AdminHandler) SetAuditSink(sink iface.AuditSink) { h.auditSink = sink }

// emitIdPChange is the shared emit site for IdP CRUD — pulls the actor
// from the request context (admin route is already gated by RequireAuth +
// RequirePermission) and stamps the tenant-scoped resource context.
func (h *AdminHandler) emitIdPChange(ctx context.Context, action, idpConfigUUID string) {
	if h.auditSink == nil {
		return
	}
	tenantID, _ := ctxauth.GetTenantID(ctx)
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	email, _ := ctxauth.GetUserEmail(ctx)
	h.auditSink.Emit(ctx, iface.AuditEvent{
		TenantID:     tenantID,
		ActorUserID:  userUUID,
		ActorEmail:   email,
		ActorType:    "user",
		Action:       action,
		ResourceType: "identity_idp",
		ResourceID:   idpConfigUUID,
	})
}

// --- DTOs ---

// IdPConfigPayload is the wire shape accepted from admins. Mirrors
// models.IdPConfig but strips tenantId/uuid/timestamps (server-owned) and
// accepts clientSecret as plaintext — the handler encrypts before storage.
type IdPConfigPayload struct {
	DisplayName  string   `json:"displayName" doc:"Human-friendly label for the admin UI"`
	IssuerURL    string   `json:"issuerURL" doc:"OIDC discovery base URL (no trailing slash)"`
	ClientID     string   `json:"clientId" doc:"OAuth client ID registered at the IdP"`
	ClientSecret string   `json:"clientSecret,omitempty" doc:"OAuth client secret; omit or send empty to preserve the stored value"`
	RedirectURL  string   `json:"redirectURL" doc:"Absolute callback URL registered at the IdP (typically /v1/identity/oidc/callback)"`
	Scopes       []string `json:"scopes,omitempty" doc:"Requested scopes; defaults to openid email profile"`
	SubClaim     string   `json:"subClaim,omitempty" doc:"Override for the subject claim (default: sub)"`
	EmailClaim   string   `json:"emailClaim,omitempty" doc:"Override for the email claim (default: email)"`
	NameClaim    string   `json:"nameClaim,omitempty" doc:"Override for the display name claim (default: name)"`
	Enabled      bool     `json:"enabled" doc:"If false, public /start endpoint returns 404 even when the config exists"`
}

// IdPConfigView is the read shape returned to admins. ClientSecret is
// always redacted to "***" when present — the plaintext is never leaked.
type IdPConfigView struct {
	UUID         string    `json:"uuid"`
	TenantID     string    `json:"tenantId"`
	Protocol     string    `json:"protocol"`
	DisplayName  string    `json:"displayName"`
	IssuerURL    string    `json:"issuerURL"`
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret,omitempty"`
	RedirectURL  string    `json:"redirectURL"`
	Scopes       []string  `json:"scopes,omitempty"`
	SubClaim     string    `json:"subClaim,omitempty"`
	EmailClaim   string    `json:"emailClaim,omitempty"`
	NameClaim    string    `json:"nameClaim,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// --- Request/response wrappers ---

type GetIdPConfigResponse struct {
	Body IdPConfigView
}

type PutIdPConfigRequest struct {
	Body IdPConfigPayload
}

type PutIdPConfigResponse struct {
	Body IdPConfigView
}

type DeleteIdPConfigResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// --- Handler methods ---

// Get returns the caller tenant's OIDC config or 404 when unset.
func (h *AdminHandler) Get(ctx context.Context, _ *struct{}) (*GetIdPConfigResponse, error) {
	cfg, err := h.repo.GetForCurrentTenant(ctx, models.ProtocolOIDC)
	if err != nil {
		if errors.Is(err, repository.ErrIdPConfigNotFound) {
			return nil, huma.Error404NotFound("no OIDC config for this tenant")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetIdPConfigResponse{Body: toView(cfg)}, nil
}

// Put either creates or replaces the tenant's OIDC config. One call site
// for both operations keeps the admin UI trivial — the frontend doesn't
// need to know whether the config already exists.
func (h *AdminHandler) Put(ctx context.Context, req *PutIdPConfigRequest) (*PutIdPConfigResponse, error) {
	if err := validatePayload(req.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	existing, err := h.repo.GetForCurrentTenant(ctx, models.ProtocolOIDC)
	if err != nil && !errors.Is(err, repository.ErrIdPConfigNotFound) {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	cfg := &models.IdPConfig{
		Protocol:    models.ProtocolOIDC,
		DisplayName: req.Body.DisplayName,
		IssuerURL:   strings.TrimRight(req.Body.IssuerURL, "/"),
		ClientID:    req.Body.ClientID,
		RedirectURL: req.Body.RedirectURL,
		Scopes:      req.Body.Scopes,
		SubClaim:    req.Body.SubClaim,
		EmailClaim:  req.Body.EmailClaim,
		NameClaim:   req.Body.NameClaim,
		Enabled:     req.Body.Enabled,
	}

	// Preserve the stored secret when the caller sent an empty value on
	// update. This avoids clients having to reenter the secret on every
	// cosmetic edit.
	if req.Body.ClientSecret != "" {
		encrypted, err := cryptoutil.Encrypt(req.Body.ClientSecret)
		if err != nil {
			return nil, huma.Error500InternalServerError("encrypt client secret: " + err.Error())
		}
		cfg.ClientSecret = encrypted
	} else if existing != nil {
		cfg.ClientSecret = existing.ClientSecret
	}

	var action string
	if existing == nil {
		cfg.UUID = uuid.New().String()
		if err := h.repo.Create(ctx, cfg); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		action = "identity.idp.created"
	} else {
		cfg.UUID = existing.UUID
		cfg.TenantID = existing.TenantID
		if err := h.repo.UpdateForCurrentTenant(ctx, cfg); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		action = "identity.idp.updated"
	}

	// Re-read to pick up server-owned timestamps.
	fresh, err := h.repo.GetForCurrentTenant(ctx, models.ProtocolOIDC)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.emitIdPChange(ctx, action, fresh.UUID)
	return &PutIdPConfigResponse{Body: toView(fresh)}, nil
}

// Delete removes the tenant's OIDC config. 404 when none is configured.
func (h *AdminHandler) Delete(ctx context.Context, _ *struct{}) (*DeleteIdPConfigResponse, error) {
	// Read first so we can carry the UUID on the audit emit below.
	existing, lookupErr := h.repo.GetForCurrentTenant(ctx, models.ProtocolOIDC)
	if err := h.repo.DeleteForCurrentTenant(ctx, models.ProtocolOIDC); err != nil {
		if errors.Is(err, repository.ErrIdPConfigNotFound) {
			return nil, huma.Error404NotFound("no OIDC config for this tenant")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	var deletedUUID string
	if lookupErr == nil && existing != nil {
		deletedUUID = existing.UUID
	}
	h.emitIdPChange(ctx, "identity.idp.deleted", deletedUUID)
	out := &DeleteIdPConfigResponse{}
	out.Body.Success = true
	return out, nil
}

// RegisterAdminRoutes mounts the admin endpoints on an authenticated API
// already gated by RequireAuth + RequirePermission at the caller side.
func RegisterAdminRoutes(api huma.API, h *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "identity-idp-get",
		Method:      http.MethodGet,
		Path:        "/v1/identity/idp",
		Summary:     "Get the current tenant's OIDC IdP configuration",
		Tags:        []string{"Identity"},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "identity-idp-put",
		Method:      http.MethodPut,
		Path:        "/v1/identity/idp",
		Summary:     "Create or replace the current tenant's OIDC IdP configuration",
		Tags:        []string{"Identity"},
	}, h.Put)

	huma.Register(api, huma.Operation{
		OperationID: "identity-idp-delete",
		Method:      http.MethodDelete,
		Path:        "/v1/identity/idp",
		Summary:     "Delete the current tenant's OIDC IdP configuration",
		Tags:        []string{"Identity"},
	}, h.Delete)
}

// --- helpers ---

func toView(cfg *models.IdPConfig) IdPConfigView {
	v := IdPConfigView{
		UUID:        cfg.UUID,
		TenantID:    cfg.TenantID,
		Protocol:    cfg.Protocol,
		DisplayName: cfg.DisplayName,
		IssuerURL:   cfg.IssuerURL,
		ClientID:    cfg.ClientID,
		RedirectURL: cfg.RedirectURL,
		Scopes:      cfg.Scopes,
		SubClaim:    cfg.SubClaim,
		EmailClaim:  cfg.EmailClaim,
		NameClaim:   cfg.NameClaim,
		Enabled:     cfg.Enabled,
		CreatedAt:   cfg.CreatedAt,
		UpdatedAt:   cfg.UpdatedAt,
	}
	if cfg.ClientSecret != "" {
		v.ClientSecret = "***"
	}
	return v
}

func validatePayload(p IdPConfigPayload) error {
	if strings.TrimSpace(p.IssuerURL) == "" {
		return errors.New("issuerURL is required")
	}
	if strings.TrimSpace(p.ClientID) == "" {
		return errors.New("clientId is required")
	}
	if strings.TrimSpace(p.RedirectURL) == "" {
		return errors.New("redirectURL is required")
	}
	return nil
}
