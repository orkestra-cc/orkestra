package middleware

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTValidator is a lightweight middleware that validates RS256 JWTs using only
// the public key. It does not require the full auth module — designed for the
// AI service sidecar where we only need to verify tokens, not issue them.
//
// It populates the same context keys as AuthMiddleware.RequireAuth:
//
//	"userUUID", "userEmail", "userRole", "claims"
type JWTValidator struct {
	publicKey *rsa.PublicKey
}

// JWTClaims mirrors auth/models.JWTClaims with only the fields needed for
// request context and RBAC checks. Avoids importing the auth module.
type JWTClaims struct {
	UserUUID string `json:"sub"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Type     string `json:"type"`
	UserID   string `json:"uid,omitempty"` // legacy
}

// NewJWTValidator creates a JWTValidator from a PEM-encoded RSA public key file.
func NewJWTValidator(publicKeyPath string) (*JWTValidator, error) {
	keyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("jwt_validator: read public key %s: %w", publicKeyPath, err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("jwt_validator: no PEM block found in %s", publicKeyPath)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("jwt_validator: parse public key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("jwt_validator: key is not RSA")
	}

	return &JWTValidator{publicKey: rsaPub}, nil
}

// RequireAuth validates the Bearer token and populates the request context.
// Returns 401 if the token is missing, invalid, expired, or not an access token.
func (v *JWTValidator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearer(r)
		if tokenStr == "" {
			http.Error(w, `{"title":"Unauthorized","status":401,"detail":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return v.publicKey, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, `{"title":"Unauthorized","status":401,"detail":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}

		mapClaims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, `{"title":"Unauthorized","status":401,"detail":"invalid claims"}`, http.StatusUnauthorized)
			return
		}

		// Reject refresh tokens
		if getStr(mapClaims, "type") == "refresh" {
			http.Error(w, `{"title":"Unauthorized","status":401,"detail":"refresh tokens not accepted"}`, http.StatusUnauthorized)
			return
		}

		claims := &JWTClaims{
			UserUUID: getStr(mapClaims, "sub"),
			Email:    getStr(mapClaims, "email"),
			Role:     getStr(mapClaims, "role"),
			Type:     getStr(mapClaims, "type"),
			UserID:   getStr(mapClaims, "uid"),
		}

		userID := claims.UserUUID
		if userID == "" {
			userID = claims.UserID
		}

		ctx := context.WithValue(r.Context(), "userUUID", userID)
		ctx = context.WithValue(ctx, "userID", claims.UserID)
		ctx = context.WithValue(ctx, "userEmail", claims.Email)
		ctx = context.WithValue(ctx, "userRole", claims.Role)
		ctx = context.WithValue(ctx, "claims", claims)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireHierarchicalRole returns middleware that checks the user's role against a
// minimum level. Same signature as AuthMiddleware.RequireHierarchicalRole so both
// satisfy the module.RoleMiddleware interface.
func (v *JWTValidator) RequireHierarchicalRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value("userRole").(string)
			if !roleAtLeast(role, minRole) {
				http.Error(w, `{"title":"Forbidden","status":403,"detail":"insufficient role"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Role hierarchy: developer > ceo > administrator > manager > operator > guest
var roleLevel = map[string]int{
	"developer":     6,
	"ceo":           5,
	"administrator": 4,
	"manager":       3,
	"operator":      2,
	"guest":         1,
}

func roleAtLeast(userRole, minRole string) bool {
	return roleLevel[userRole] >= roleLevel[minRole]
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return parts[1]
	}
	return ""
}

func getStr(m jwt.MapClaims, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
