package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/golang-jwt/jwt/v5"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// Canonical `iss` values. A validator checks the issuer ALWAYS (spec §4.10
// D35): a token minted by anyone would otherwise pass the signature check
// against a key set the attacker also controls.
var (
	GoogleIDTokenIssuers = []string{"https://accounts.google.com", "accounts.google.com"}
	AppleIDTokenIssuers  = []string{"https://appleid.apple.com"}
)

// validateIDTokenClaims is the one ID-token validator both providers use.
// In order: a non-empty expected audience (an EMPTY one is an error, not a
// skip — MobileAudience returning "" used to disable the check), the
// signing key by kid, signature + required exp, iss ∈ issuers, aud, and
// the nonce when one is expected.
//
// Every failure returns the same OPAQUE error to the caller (L-27): the
// detail goes to the log, never to the response.
func validateIDTokenClaims(ctx context.Context, provider models.OAuthProvider, request *IDTokenValidationRequest, defaultIssuers []string, keyFor func(ctx context.Context, kid string) (interface{}, error)) (jwt.MapClaims, error) {
	opaque := func(outcome string, detail error) error {
		msg := ""
		if detail != nil {
			msg = detail.Error()
		}
		slog.Default().Warn("id token validation failed",
			slog.String("provider", string(provider)), slog.String("outcome", outcome), slog.String("error", msg))
		return NewProviderError(provider, "id_token_validation", ErrInvalidIDToken)
	}
	if request == nil || request.IDToken == "" {
		return nil, opaque("missing_token", errors.New("empty id token"))
	}
	if request.Audience == "" {
		return nil, opaque("audience_unresolved", errors.New("expected audience is empty"))
	}
	issuers := request.Issuers
	if len(issuers) == 0 {
		issuers = defaultIssuers
	}
	if len(issuers) == 0 {
		return nil, opaque("issuers_unset", errors.New("no acceptable issuer configured"))
	}

	unverified, _, err := jwt.NewParser().ParseUnverified(request.IDToken, jwt.MapClaims{})
	if err != nil {
		return nil, opaque("malformed", err)
	}
	kid, _ := unverified.Header["kid"].(string)
	if kid == "" {
		return nil, opaque("missing_kid", errors.New("missing kid in token header"))
	}
	key, err := keyFor(ctx, kid)
	if err != nil {
		return nil, opaque("key_fetch_failed", err)
	}

	parsed, err := jwt.Parse(request.IDToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return key, nil
	}, jwt.WithExpirationRequired())
	if err != nil {
		return nil, opaque("signature_or_time", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return nil, opaque("invalid_claims", errors.New("invalid claims format"))
	}

	iss, _ := claims["iss"].(string)
	if !slices.Contains(issuers, iss) {
		return nil, opaque("issuer_mismatch", fmt.Errorf("iss %q not accepted", iss))
	}
	aud, err := claims.GetAudience()
	if err != nil || !slices.Contains(aud, request.Audience) {
		return nil, opaque("audience_mismatch", errors.New("aud does not match the expected audience"))
	}
	if request.ExpectedNonce != "" {
		nonce, _ := claims["nonce"].(string)
		if nonce == "" || subtle.ConstantTimeCompare([]byte(nonce), []byte(request.ExpectedNonce)) != 1 {
			return nil, opaque("nonce_mismatch", errors.New("nonce missing or different"))
		}
	}
	return claims, nil
}
