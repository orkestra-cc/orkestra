package handlers

// Task 9: exposes services.ServiceAccountService's six lifecycle methods
// (Task 6) over the operator admin surface at /v1/admin/service-accounts.
// Thin delegation only — validation, the max-two-active-credentials cap,
// and every not-found/conflict rule live in the service; this handler just
// binds HTTP and maps the service's typed sentinels to status codes.

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
)

// ServiceAccountAdminHandler wraps services.ServiceAccountService for the
// operator admin console. Constructed once in module.go's Init over the
// same *services.ServiceAccountService instance the public token endpoint
// (Task 8) uses.
type ServiceAccountAdminHandler struct {
	svc *services.ServiceAccountService
}

// NewServiceAccountAdminHandler constructs the handler.
func NewServiceAccountAdminHandler(svc *services.ServiceAccountService) *ServiceAccountAdminHandler {
	return &ServiceAccountAdminHandler{svc: svc}
}

// --- GET list ---

type ServiceAccountListRequest struct{}

type ServiceAccountListResponse struct {
	Body []services.AccountView
}

// List returns every service account with its live active-credential
// count. Gated by auth.service_accounts.read (no step-up — read-only).
func (h *ServiceAccountAdminHandler) List(ctx context.Context, _ *ServiceAccountListRequest) (*ServiceAccountListResponse, error) {
	views, err := h.svc.ListAccounts(ctx)
	if err != nil {
		return nil, mapServiceAccountAdminError(err)
	}
	return &ServiceAccountListResponse{Body: views}, nil
}

// --- GET get ---

type ServiceAccountGetRequest struct {
	ID string `path:"id" doc:"UUID of the service account (its backing user UUID)"`
}

type ServiceAccountGetResponse struct {
	Body services.AccountDetail
}

// Get returns one service account plus its full credential history
// (active and revoked). Gated by auth.service_accounts.read.
func (h *ServiceAccountAdminHandler) Get(ctx context.Context, req *ServiceAccountGetRequest) (*ServiceAccountGetResponse, error) {
	detail, err := h.svc.GetAccount(ctx, req.ID)
	if err != nil {
		return nil, mapServiceAccountAdminError(err)
	}
	return &ServiceAccountGetResponse{Body: *detail}, nil
}

// --- POST create ---

type ServiceAccountCreateRequest struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" maxLength:"100" doc:"Display name; slugified into the account's synthetic email"`
	}
}

type ServiceAccountCreateResponse struct {
	Body services.AccountWithSecret
}

// Create mints a new service-account user row plus its first credential.
// The response carries the plaintext client secret exactly once — gated
// by auth.service_accounts.manage + a fresh <5min step-up.
func (h *ServiceAccountAdminHandler) Create(ctx context.Context, req *ServiceAccountCreateRequest) (*ServiceAccountCreateResponse, error) {
	acct, err := h.svc.CreateAccount(ctx, req.Body.Name)
	if err != nil {
		return nil, mapServiceAccountAdminError(err)
	}
	return &ServiceAccountCreateResponse{Body: *acct}, nil
}

// --- PATCH update ---

type ServiceAccountUpdateRequest struct {
	ID   string `path:"id" doc:"UUID of the service account (its backing user UUID)"`
	Body struct {
		// minLength:"1" turns a present-but-empty {"name": ""} into a 422
		// instead of a silent no-op — the service applies any non-nil
		// pointer as-is (see ServiceAccountService.UpdateAccount), so an
		// empty string would otherwise blank the display name.
		Name   *string `json:"name,omitempty" minLength:"1"`
		Active *bool   `json:"active,omitempty"`
	}
}

type ServiceAccountUpdateResponse struct {
	Body services.AccountView
}

// Update renames and/or enables/disables a service account. Only non-nil
// fields are applied. Gated by auth.service_accounts.manage + step-up.
func (h *ServiceAccountAdminHandler) Update(ctx context.Context, req *ServiceAccountUpdateRequest) (*ServiceAccountUpdateResponse, error) {
	view, err := h.svc.UpdateAccount(ctx, req.ID, req.Body.Name, req.Body.Active)
	if err != nil {
		return nil, mapServiceAccountAdminError(err)
	}
	return &ServiceAccountUpdateResponse{Body: *view}, nil
}

// --- POST credentials (issue) ---

type ServiceAccountIssueCredentialRequest struct {
	ID   string `path:"id" doc:"UUID of the service account (its backing user UUID)"`
	Body struct {
		Label string `json:"label,omitempty" maxLength:"60" doc:"Optional label for the new credential; defaults to \"rotated\""`
	}
}

type ServiceAccountIssueCredentialResponse struct {
	Body services.CredentialWithSecret
}

// IssueCredential mints a rotation credential, enforcing the max-two-
// active cap. The response carries the plaintext client secret exactly
// once. Gated by auth.service_accounts.manage + step-up.
func (h *ServiceAccountAdminHandler) IssueCredential(ctx context.Context, req *ServiceAccountIssueCredentialRequest) (*ServiceAccountIssueCredentialResponse, error) {
	cred, err := h.svc.IssueCredential(ctx, req.ID, req.Body.Label)
	if err != nil {
		return nil, mapServiceAccountAdminError(err)
	}
	return &ServiceAccountIssueCredentialResponse{Body: *cred}, nil
}

// --- DELETE credentials/{credentialId} (revoke) ---

type ServiceAccountRevokeCredentialRequest struct {
	ID           string `path:"id" doc:"UUID of the service account (its backing user UUID)"`
	CredentialID string `path:"credentialId" doc:"UUID of the credential to revoke"`
}

type ServiceAccountRevokeCredentialResponse struct{}

// RevokeCredential revokes one of the account's credentials. Gated by
// auth.service_accounts.manage + step-up. 204 on success.
func (h *ServiceAccountAdminHandler) RevokeCredential(ctx context.Context, req *ServiceAccountRevokeCredentialRequest) (*ServiceAccountRevokeCredentialResponse, error) {
	if err := h.svc.RevokeCredential(ctx, req.ID, req.CredentialID); err != nil {
		return nil, mapServiceAccountAdminError(err)
	}
	return &ServiceAccountRevokeCredentialResponse{}, nil
}

// --- error mapping ---

// mapServiceAccountAdminError translates ServiceAccountService's typed
// sentinels (Task 6) to the matching HTTP status, mirroring the auth
// package's existing mapXError idiom (see mapServiceTokenError,
// mapAdminUserAuthError). Anything unrecognized passes through unchanged
// (default 500 via Huma's fallback).
func mapServiceAccountAdminError(err error) error {
	switch {
	// Ahead of the not-found arm on purpose: a directory the platform could
	// not READ says nothing about whether the account exists, and answering
	// it 404 sends an operator hunting for a deletion that never happened
	// (spec §8 #17 — §4.9's class, one module over). huma's ErrorModel has no
	// top-level code field, so the machine-readable token goes in `detail`,
	// the shape avatar_handler.go already uses on a huma route.
	case errors.Is(err, services.ErrServiceAccountLookupUnavailable):
		return huma.NewError(http.StatusServiceUnavailable,
			"service_account_lookup_unavailable",
			&huma.ErrorDetail{Message: "the service-account directory could not be read; try again shortly"})
	case errors.Is(err, services.ErrServiceAccountNotFound):
		return huma.Error404NotFound("service account not found")
	case errors.Is(err, repository.ErrServiceAccountCredentialNotFound):
		return huma.Error404NotFound("credential not found")
	case errors.Is(err, services.ErrAccountAlreadyExists):
		return huma.Error409Conflict("service account already exists")
	case errors.Is(err, services.ErrTooManyActiveCredentials):
		return huma.Error409Conflict("service account already has two active credentials")
	case errors.Is(err, services.ErrInvalidAccountName):
		return huma.Error422UnprocessableEntity("service account name yields an empty slug")
	default:
		return err
	}
}

// --- registration ---

// RegisterReadRoutes mounts the read-only list/get endpoints. Caller
// wires RequireSystemPermission("auth.service_accounts.read") around
// this API instance — see auth/module.go::RegisterRoutes. No step-up:
// these are read-only.
func (h *ServiceAccountAdminHandler) RegisterReadRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-service-accounts-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/service-accounts",
		Summary:     "List service accounts",
		Tags:        []string{"Service Accounts"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "auth-service-accounts-get",
		Method:      http.MethodGet,
		Path:        "/v1/admin/service-accounts/{id}",
		Summary:     "Get a service account with credential metadata",
		Tags:        []string{"Service Accounts"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.Get)
}

// RegisterManageRoutes mounts the mutating endpoints. Caller wires
// RequireSystemPermission("auth.service_accounts.manage") +
// RequireStepUp(5m) around this API instance — every route here either
// creates a credential-bearing account or mints/revokes a credential.
func (h *ServiceAccountAdminHandler) RegisterManageRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "auth-service-accounts-create",
		Method:        http.MethodPost,
		Path:          "/v1/admin/service-accounts",
		Summary:       "Create a service account (returns the client secret once)",
		Tags:          []string{"Service Accounts"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "auth-service-accounts-update",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/service-accounts/{id}",
		Summary:     "Rename or enable/disable a service account",
		Tags:        []string{"Service Accounts"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID:   "auth-service-account-credentials-issue",
		Method:        http.MethodPost,
		Path:          "/v1/admin/service-accounts/{id}/credentials",
		Summary:       "Issue a rotation credential (max 2 active; secret returned once)",
		Tags:          []string{"Service Accounts"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusCreated,
	}, h.IssueCredential)
	huma.Register(api, huma.Operation{
		OperationID:   "auth-service-account-credentials-revoke",
		Method:        http.MethodDelete,
		Path:          "/v1/admin/service-accounts/{id}/credentials/{credentialId}",
		Summary:       "Revoke a credential",
		Tags:          []string{"Service Accounts"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.RevokeCredential)
}
