package services

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ADR-0003 PR-D D-6: OAuth state-encoded tier dispatch.
//
// Pre-D-6 the OAuth state parameter was an opaque random string that
// indexed a Redis row holding all per-flow side data (provider, deviceInfo,
// securityContext). With the audience split that opaque string can no
// longer carry the dispatch decision the single callback needs to make:
// "did this flow originate from the operator login page or the client
// login page?". A leaked Redis row could be cross-targeted; the OAuth
// provider has no awareness of tier.
//
// The fix: state becomes a signed JWT (HS256) carrying the tier as a
// signed claim. The CSRF/nonce field doubles as the Redis key that still
// holds the per-flow side data — so the JWT itself stays small and the
// existing one-time-use Redis semantics carry over. Callback decodes the
// JWT, verifies signature + expiry, then loads side data from Redis using
// the CSRF claim. The two are then cross-checked (Redis-side tier ==
// JWT-side tier) so neither half can be tampered with in isolation.
//
// HMAC secret is derived deterministically from the JWT RS256 private
// key. Daily rotation (per the ADR text) is left as a follow-up — the
// derivation reuses an existing rotation cadence rather than introducing
// a new env var that ops would need to manage.

// ErrInvalidStateToken is returned when an OAuth state JWT fails parsing,
// signature verification, or expiry validation. Callers surface this as
// a 400 to the OAuth callback so a tampered/expired state cannot be
// silently coerced into a valid login.
var ErrInvalidStateToken = errors.New("invalid OAuth state token")

// OAuthStateClaims is the payload of the signed state JWT. Tier carries
// the dispatch target (operator|client|"" for legacy paths); CSRF is the
// random nonce that doubles as the Redis key holding the per-flow side
// data; ExpiresAt enforces the 10-minute OAuth state window.
//
// Mode discriminates a login flow ("" or "login", default) from a
// link flow ("link"). When Mode=="link" the callback is acting on a
// user who is already authenticated — LinkUserUUID names the
// authenticated user the new identity must be bound to. The
// signed-state JWT is the only carrier for that UUID; the OAuth
// provider has no awareness of it. Callbacks must reject any
// link-mode state whose LinkUserUUID is empty.
type OAuthStateClaims struct {
	Tier         string `json:"tier,omitempty"`
	Mode         string `json:"mode,omitempty"`
	LinkUserUUID string `json:"linkUserUuid,omitempty"`
	CSRF         string `json:"csrf"`
	jwt.RegisteredClaims
}

// OAuth state-token modes. Empty / OAuthStateModeLogin → the legacy
// login flow (the callback mints a token pair). OAuthStateModeLink →
// the user-security self-service "add a sign-in provider" flow (the
// callback binds the new identity to LinkUserUUID and does not mint
// tokens).
const (
	OAuthStateModeLogin = "login"
	OAuthStateModeLink  = "link"
)

// SignOAuthStateToken mints the signed state JWT sent to the OAuth
// provider as the `state` query parameter. tier may be empty for legacy
// (pre-tier-split) flows; the callback treats an empty tier as "use the
// callback handler's own authService" so existing /v1/auth/oauth/login
// requests keep working through the cutover.
func SignOAuthStateToken(secret []byte, tier, csrf string, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("oauth state token: secret is required")
	}
	if csrf == "" {
		return "", fmt.Errorf("oauth state token: csrf is required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := time.Now()
	claims := OAuthStateClaims{
		Tier: tier,
		CSRF: csrf,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("oauth state token: sign: %w", err)
	}
	return signed, nil
}

// SignOAuthLinkStateToken mints a state JWT for the self-service
// "add a sign-in provider" flow. linkUserUUID is the authenticated
// user the new identity must be bound to on callback — empty is
// rejected.
func SignOAuthLinkStateToken(secret []byte, tier, csrf, linkUserUUID string, ttl time.Duration) (string, error) {
	if linkUserUUID == "" {
		return "", fmt.Errorf("oauth link state token: linkUserUUID is required")
	}
	if len(secret) == 0 {
		return "", fmt.Errorf("oauth state token: secret is required")
	}
	if csrf == "" {
		return "", fmt.Errorf("oauth state token: csrf is required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := time.Now()
	claims := OAuthStateClaims{
		Tier:         tier,
		Mode:         OAuthStateModeLink,
		LinkUserUUID: linkUserUUID,
		CSRF:         csrf,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("oauth link state token: sign: %w", err)
	}
	return signed, nil
}

// ValidateOAuthStateToken parses and verifies a state JWT minted by
// SignOAuthStateToken. Returns the decoded claims on success; any
// signature, expiry, or alg mismatch surfaces as ErrInvalidStateToken so
// the callback can render a single neutral 400 regardless of the failure
// mode (no oracle for an attacker probing state forgery).
func ValidateOAuthStateToken(secret []byte, raw string) (*OAuthStateClaims, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("oauth state token: secret is required")
	}
	if raw == "" {
		return nil, ErrInvalidStateToken
	}
	parsed, err := jwt.ParseWithClaims(raw, &OAuthStateClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, ErrInvalidStateToken
	}
	claims, ok := parsed.Claims.(*OAuthStateClaims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidStateToken
	}
	if claims.CSRF == "" {
		return nil, ErrInvalidStateToken
	}
	// Cross-claim sanity: a link-mode state must always carry the
	// authenticated user UUID. If the JWT was minted by a legitimate
	// path this is set; if an attacker stripped it to slip past the
	// link branch, reject as forged.
	if claims.Mode == OAuthStateModeLink && claims.LinkUserUUID == "" {
		return nil, ErrInvalidStateToken
	}
	return claims, nil
}

// GenerateOAuthCSRF returns a 32-byte cryptographically random nonce
// encoded as base64url (no padding). Used as the CSRF claim of the
// state JWT and as the Redis key for the per-flow side-data row.
func GenerateOAuthCSRF() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth csrf: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// DeriveOAuthStateSecret produces the HMAC secret used to sign state
// JWTs from the deployment's JWT RS256 private key. The derivation is
// deterministic so every replica of the monolith agrees on the secret
// without needing a separate env var, and the secret rotates implicitly
// whenever the JWT key pair rotates. Returns an error when the private
// key is nil — callers should treat that as "OAuth disabled" since
// state cannot be signed.
func DeriveOAuthStateSecret(privateKey *rsa.PrivateKey) ([]byte, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("oauth state secret: jwt private key is required")
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("oauth state secret: marshal private key: %w", err)
	}
	sum := sha256.Sum256(append([]byte("orkestra-oauth-state-secret-v1\x00"), der...))
	return sum[:], nil
}
