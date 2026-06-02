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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

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
	operatorJWT iface.JWTProvider
	clientJWT   iface.JWTProvider
	platform    module.PlatformInfo
	logger      *slog.Logger
}

// NewHandler builds a dev-token handler. Both JWT providers are required —
// operator is the back-compat default when the request omits `audience`.
func NewHandler(operatorJWT, clientJWT iface.JWTProvider, platform module.PlatformInfo, logger *slog.Logger) *Handler {
	return &Handler{
		operatorJWT: operatorJWT,
		clientJWT:   clientJWT,
		platform:    platform,
		logger:      logger.With(slog.String("component", "dev_token")),
	}
}

// RegisterRoutes mounts POST /dev/token + GET /dev/token/roles on the
// operator root router. It refuses to register in production as a
// safety net even if the caller forgets the gate.
func (h *Handler) RegisterRoutes(r chi.Router) {
	if h.platform.IsProduction() {
		h.logger.Error("refusing to register dev-token routes in production")
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
}

type generateTokenResponse struct {
	AccessToken string    `json:"accessToken"`
	Role        string    `json:"role"`
	Audience    string    `json:"audience"`
	Email       string    `json:"email"`
	ExpiresAt   time.Time `json:"expiresAt"`
	ExpiresIn   int64     `json:"expiresIn"`
	Curl        string    `json:"curl"`
}

func (h *Handler) generateToken(w http.ResponseWriter, r *http.Request) {
	if h.platform.IsProduction() {
		http.Error(w, `{"error": "dev token generation is disabled in production"}`, http.StatusForbidden)
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

	// Default 15m, min 1m, max 24h.
	expiry := 15 * time.Minute
	if req.Expiry != "" {
		parsed, err := time.ParseDuration(req.Expiry)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "invalid expiry format: %v. Use formats like '15m', '1h', '24h'"}`, err), http.StatusBadRequest)
			return
		}
		if parsed > 24*time.Hour {
			http.Error(w, `{"error": "expiry cannot exceed 24 hours"}`, http.StatusBadRequest)
			return
		}
		if parsed < time.Minute {
			http.Error(w, `{"error": "expiry must be at least 1 minute"}`, http.StatusBadRequest)
			return
		}
		expiry = parsed
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

	token, err := jwtSvc.GenerateAccessToken(user)
	if err != nil {
		h.logger.Error("failed to generate dev token", slog.String("error", err.Error()))
		http.Error(w, `{"error": "failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	if h.platform.IsStaging() {
		h.logger.Info("dev token generated in staging",
			slog.String("role", req.Role),
			slog.String("audience", audience),
			slog.String("expiry", expiry.String()),
		)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(generateTokenResponse{
		AccessToken: token,
		Role:        req.Role,
		Audience:    audience,
		Email:       syntheticEmail,
		ExpiresAt:   now.Add(expiry),
		ExpiresIn:   int64(expiry.Seconds()),
		Curl:        fmt.Sprintf("curl -H 'Authorization: Bearer %s' http://localhost:3000/v1/users", token),
	})
}

type listRolesResponse struct {
	Roles       []string `json:"roles"`
	Environment string   `json:"environment"`
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	if h.platform.IsProduction() {
		http.Error(w, `{"error": "dev token generation is disabled in production"}`, http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(listRolesResponse{
		Roles:       ValidRoles,
		Environment: h.platform.GetEnvironment(),
	})
}
