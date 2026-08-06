// Package devtoken serves the dev-only synthetic JWT endpoint used by
// scripts/devtoken.sh and the operator console's "Sign in with dev token"
// affordance for first login + local API testing.
//
// ADR-0006 deleted the `dev` addon that previously owned `POST /dev/token`.
// The capability is core dev-tooling (not a product vertical), so it is
// re-provided here as a small shared package mounted directly on the
// operator root chi router by cmd/server/main.go — gated to non-production.
// It writes no database rows: every token is minted for a synthetic user.
package devtoken

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// DefaultTenantResolver returns an internal operator tenant to stamp onto a
// dev token when the caller doesn't specify one, so dev tokens satisfy
// tenant-scoped reads (billing/documents). Returns ("", "") when none is
// available — the token is then minted without a tenant (legacy behavior).
type DefaultTenantResolver func(ctx context.Context) (tenantUUID, tenantKind string)

// ValidRoles are the system roles a dev token may carry (highest → lowest).
var ValidRoles = []string{"super_admin", "administrator", "developer", "manager", "operator", "guest"}

// ValidAudiences lists the JWT `aud` values the endpoint can mint
// (ADR-0003 PR-D D-10). Empty defaults to operator.
var ValidAudiences = []string{"operator", "client"}

// DefaultAudience matches the canonical operator JWT binding.
const DefaultAudience = "operator"

// Handler mints synthetic dev tokens. It holds one JWTProvider per tier
// and dispatches by the request's `audience` field.
type Handler struct {
	operatorJWT   iface.JWTProvider
	clientJWT     iface.JWTProvider
	platform      module.PlatformInfo
	defaultTenant DefaultTenantResolver
	logger        *slog.Logger
}

// NewHandler builds a dev-token handler. Both JWT providers are required —
// operator is the back-compat default when the request omits `audience`.
// defaultTenant may be nil — when set, operator-audience tokens that don't
// specify a tenant are stamped with the resolved internal tenant so they
// satisfy tenant-scoped reads (billing/documents).
func NewHandler(operatorJWT, clientJWT iface.JWTProvider, platform module.PlatformInfo, defaultTenant DefaultTenantResolver, logger *slog.Logger) *Handler {
	return &Handler{
		operatorJWT:   operatorJWT,
		clientJWT:     clientJWT,
		platform:      platform,
		defaultTenant: defaultTenant,
		logger:        logger.With(slog.String("component", "dev_token")),
	}
}

// RegisterRoutes mounts POST /dev/token + GET /dev/token/roles on the
// operator root router. It refuses to register in any production-like
// environment as a safety net even if the caller forgets the gate.
//
// The gate is IsProductionLike, NOT IsProduction: this endpoint mints a
// signed super_admin token to an anonymous caller, so it must never be
// reachable from an environment that is exposed to the internet.
// Staging is exposed, so staging is out.
func (h *Handler) RegisterRoutes(r chi.Router) {
	if h.platform.IsProductionLike() {
		h.logger.Error("refusing to register dev-token routes",
			slog.String("environment", h.platform.GetEnvironment()))
		return
	}
	r.Post("/dev/token", h.generateToken)
	r.Get("/dev/token/roles", h.listRoles)
	h.logger.Info("dev-token routes registered (/dev/token)")
}

func isValidRole(role string) bool {
	for _, r := range ValidRoles {
		if r == role {
			return true
		}
	}
	return false
}

// resolveAudience normalizes the request's audience and returns the
// matching JWTProvider (empty → operator).
func (h *Handler) resolveAudience(audience string) (string, iface.JWTProvider, error) {
	if audience == "" {
		audience = DefaultAudience
	}
	switch audience {
	case "operator":
		return audience, h.operatorJWT, nil
	case "client":
		return audience, h.clientJWT, nil
	default:
		return "", nil, fmt.Errorf("invalid audience %q (valid: %v)", audience, ValidAudiences)
	}
}

type generateTokenRequest struct {
	Role     string `json:"role"`
	Audience string `json:"audience,omitempty"`
	Expiry   string `json:"expiry,omitempty"`
	// TenantUuid optionally pins the acting tenant on the minted token so it
	// satisfies tenant-scoped reads. Empty + operator audience falls back to
	// the server's default internal tenant resolver.
	TenantUuid string `json:"tenantUuid,omitempty"`
}

type generateTokenResponse struct {
	AccessToken string    `json:"accessToken"`
	Role        string    `json:"role"`
	Audience    string    `json:"audience"`
	Email       string    `json:"email"`
	Tenant      string    `json:"tenant,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	ExpiresIn   int64     `json:"expiresIn"`
	Curl        string    `json:"curl"`
}

func (h *Handler) generateToken(w http.ResponseWriter, r *http.Request) {
	if h.platform.IsProductionLike() {
		http.Error(w, `{"error": "dev token generation is disabled in production-like environments"}`, http.StatusForbidden)
		return
	}

	var req generateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if !isValidRole(req.Role) {
		http.Error(w, fmt.Sprintf(`{"error": "invalid role '%s'. Valid roles: %v"}`, req.Role, ValidRoles), http.StatusBadRequest)
		return
	}

	audience, jwtSvc, err := h.resolveAudience(req.Audience)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Token lifetime is NOT caller-controlled. The JWTProvider seam mints
	// with the deployment's configured access-token TTL
	// (JWT_ACCESS_TOKEN_EXPIRY, or the admin-managed accessTokenTTL
	// policy) and exposes no per-call override. Previously this field was
	// parsed, range-checked, and echoed into expiresAt/expiresIn — while
	// the minted token carried the server TTL regardless. Rather than keep
	// reporting a lifetime the token does not have, reject the field.
	if req.Expiry != "" {
		http.Error(w, `{"error": "expiry is not configurable per request; the token uses the server's access-token TTL (JWT_ACCESS_TOKEN_EXPIRY / admin accessTokenTTL)"}`, http.StatusBadRequest)
		return
	}

	// Synthetic user — no database write.
	syntheticEmail := fmt.Sprintf("%s@orkestra.dev", req.Role)
	now := time.Now()
	user := &iface.User{
		UUID:          fmt.Sprintf("dev-%s-%d", req.Role, now.Unix()),
		Email:         syntheticEmail,
		FullName:      fmt.Sprintf("Dev %s", req.Role),
		Role:          req.Role,
		IsActive:      true,
		EmailVerified: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Resolve the acting tenant to stamp on the token. An explicit tenantUuid
	// wins; otherwise an operator-audience token defaults to the server's
	// internal tenant so it satisfies tenant-scoped reads (billing/documents).
	tenantUUID := strings.TrimSpace(req.TenantUuid)
	tenantKind := "internal"
	if tenantUUID == "" && audience == DefaultAudience && h.defaultTenant != nil {
		tenantUUID, tenantKind = h.defaultTenant(r.Context())
		if tenantKind == "" {
			tenantKind = "internal"
		}
	}

	var token string
	if tsp, ok := jwtSvc.(iface.TenantScopedTokenProvider); ok && tenantUUID != "" {
		token, err = tsp.GenerateAccessTokenForTenant(user, tenantUUID, tenantKind, []string{req.Role})
	} else {
		tenantUUID = "" // not stamped
		token, err = jwtSvc.GenerateAccessToken(user)
	}
	if err != nil {
		h.logger.Error("failed to generate dev token", slog.String("error", err.Error()))
		http.Error(w, `{"error": "failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	// Report the lifetime the token actually carries by reading its own
	// exp claim, rather than a number this handler made up. Unparseable
	// tokens (test doubles) simply report a zero expiry.
	expiresAt := tokenExpiry(token)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(generateTokenResponse{
		AccessToken: token,
		Role:        req.Role,
		Audience:    audience,
		Email:       syntheticEmail,
		Tenant:      tenantUUID,
		ExpiresAt:   expiresAt,
		ExpiresIn:   int64(time.Until(expiresAt).Seconds()),
		Curl:        fmt.Sprintf("curl -H 'Authorization: Bearer %s' http://localhost:3000/v1/users", token),
	})
}

// tokenExpiry reads the exp claim off a freshly minted token. No
// signature check: we just produced this token ourselves and the value
// is used only to report the lifetime back to the caller.
func tokenExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0).UTC()
}

type listRolesResponse struct {
	Roles       []string `json:"roles"`
	Environment string   `json:"environment"`
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	if h.platform.IsProductionLike() {
		http.Error(w, `{"error": "dev token generation is disabled in production-like environments"}`, http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(listRolesResponse{
		Roles:       ValidRoles,
		Environment: h.platform.GetEnvironment(),
	})
}
