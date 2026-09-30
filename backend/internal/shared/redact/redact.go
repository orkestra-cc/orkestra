// Package redact holds the key rules shared by everything that masks log
// attributes: the operational log PolicyHandler and the Loki preview.
package redact

import (
	"strings"
	"unicode"
)

// Redacted replaces a secret value.
const Redacted = "[REDACTED]"

// secretFragments are always masked, whatever the compliance policy says
// (spec D11). Matching is "contains" on the normalized key: over-masking a
// secret-looking key is safe.
var secretFragments = [...]string{
	"password", "passwd", "secret", "token", "authorization",
	"cookie", "apikey", "privatekey", "credential",
}

// NormalizeKey keeps letters and digits only, lowercased, so user_id,
// userId and user-id compare equal.
func NormalizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

// IsSecretKey reports whether key names a secret.
func IsSecretKey(key string) bool { return IsSecretNormalized(NormalizeKey(key)) }

// IsSecretNormalized is IsSecretKey for a key already passed through
// NormalizeKey.
func IsSecretNormalized(normalized string) bool {
	for _, f := range secretFragments {
		if strings.Contains(normalized, f) {
			return true
		}
	}
	return false
}
