package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/orkestra-cc/orkestra-addon-identity/internal/cryptoutil"
	"github.com/orkestra-cc/orkestra-addon-identity/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// ScimAdminHandler owns the tenant-scoped endpoints for managing the
// SCIM bearer token lifecycle. Separate from ScimHandler (which is the
// actual SCIM protocol surface) so admin mutations stay on the
// authenticated/protected router with the existing permission gates.
type ScimAdminHandler struct {
	tokens    *repository.ScimTokenRepository
	auditSink iface.AuditSink
}

// NewScimAdminHandler wires the admin handler.
func NewScimAdminHandler(tokens *repository.ScimTokenRepository) *ScimAdminHandler {
	return &ScimAdminHandler{tokens: tokens}
}

// SetAuditSink wires the compliance audit sink post-construction.
func (h *ScimAdminHandler) SetAuditSink(sink iface.AuditSink) { h.auditSink = sink }

// --- Rotate ---

type RotateScimTokenResponse struct {
	Body RotateScimTokenBody
}

// RotateScimTokenBody is the wire payload returned once on rotation.
// Token is the raw bearer value; it is shown exactly here and nowhere
// else — the IdP configures it immediately, and a fresh rotation is the
// only recovery path if the operator loses the value.
type RotateScimTokenBody struct {
	UUID      string    `json:"uuid"`
	Token     string    `json:"token" doc:"The raw bearer token. Shown exactly once — store it immediately in the IdP config."`
	CreatedAt time.Time `json:"createdAt"`
}

// Rotate revokes any existing token for the caller's tenant and returns
// a freshly minted one. 32 bytes of randomness (base64-url) keeps offline
// brute force out of reach; SHA-256 of the raw token is the persisted
// hash (not argon2id — throughput matters at token validation time, and
// the input is high-entropy so there's no rainbow-table risk).
func (h *ScimAdminHandler) Rotate(ctx context.Context, _ *struct{}) (*RotateScimTokenResponse, error) {
	raw, err := cryptoutil.SecureRandomString(32) // 43-char base64url
	if err != nil {
		return nil, huma.Error500InternalServerError("generate SCIM token: " + err.Error())
	}
	hash := sha256Hex(raw)
	row, err := h.tokens.Rotate(ctx, hash, uuid.New().String())
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if h.auditSink != nil {
		tenantID, _ := ctxauth.GetTenantID(ctx)
		userUUID, _ := ctxauth.GetUserUUID(ctx)
		email, _ := ctxauth.GetUserEmail(ctx)
		h.auditSink.Emit(ctx, iface.AuditEvent{
			TenantID:     tenantID,
			ActorUserID:  userUUID,
			ActorEmail:   email,
			ActorType:    "user",
			Action:       "identity.scim.token_rotated",
			ResourceType: "scim_token",
			ResourceID:   row.UUID,
		})
	}
	out := &RotateScimTokenResponse{}
	out.Body.UUID = row.UUID
	out.Body.Token = raw
	out.Body.CreatedAt = row.CreatedAt
	return out, nil
}

// --- Status ---

type ScimTokenStatusResponse struct {
	Body ScimTokenStatusBody
}

// ScimTokenStatusBody is the read-only metadata about the tenant's
// current token. Token value is never returned — only "exists, created
// at X". Operators who lost the raw token must rotate.
type ScimTokenStatusBody struct {
	Exists    bool      `json:"exists"`
	UUID      string    `json:"uuid,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

// Status returns whether the tenant currently has an active SCIM token.
func (h *ScimAdminHandler) Status(ctx context.Context, _ *struct{}) (*ScimTokenStatusResponse, error) {
	row, err := h.tokens.GetActiveForCurrentTenant(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrScimTokenNotFound) {
			return &ScimTokenStatusResponse{Body: ScimTokenStatusBody{Exists: false}}, nil
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &ScimTokenStatusResponse{Body: ScimTokenStatusBody{
		Exists:    true,
		UUID:      row.UUID,
		CreatedAt: row.CreatedAt,
	}}, nil
}

// RegisterScimAdminRoutes mounts the rotate + status endpoints. Caller
// is expected to wrap the API in RequirePermission("tenant.update")
// — see module.go.
func RegisterScimAdminRoutes(api huma.API, h *ScimAdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "identity-scim-rotate-token",
		Method:      http.MethodPost,
		Path:        "/v1/identity/scim/rotate-token",
		Summary:     "Rotate the current tenant's SCIM bearer token",
		Description: "Revokes any existing SCIM token for the caller's tenant and returns a freshly minted one. The raw token is returned exactly once — store it immediately in the IdP's SCIM configuration.",
		Tags:        []string{"Identity"},
	}, h.Rotate)

	huma.Register(api, huma.Operation{
		OperationID: "identity-scim-token-status",
		Method:      http.MethodGet,
		Path:        "/v1/identity/scim/token",
		Summary:     "Get SCIM token metadata for the current tenant",
		Description: "Returns { exists, uuid, createdAt } for the active SCIM token. The raw token value is never returned — only revealed at rotation time.",
		Tags:        []string{"Identity"},
	}, h.Status)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
