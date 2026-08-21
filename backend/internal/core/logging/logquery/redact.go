package logquery

import (
	"strings"
	"unicode"
)

const redactedValue = "[REDACTED]"

var sensitiveKeyFragments = [...]string{
	"password",
	"secret",
	"token",
	"authorization",
	"cookie",
	"email",
	"phone",
	"address",
	"userid",
}

// Redact returns a recursively copied JSON-like value with known sensitive
// keys masked. It is defense in depth for structured attributes only: free-text
// log messages are deliberately outside this function and may still contain
// personal data.
func Redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitiveKey(key) {
				out[key] = redactedValue
				continue
			}
			out[key] = Redact(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			out[index] = Redact(child)
		}
		return out
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
