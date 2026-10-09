package logquery

import (
	"strings"

	"github.com/orkestra/backend/internal/shared/redact"
)

const redactedValue = redact.Redacted

// previewOnlyFragments are masked in the preview on top of the shared
// secret list: the preview shows raw Loki lines to an operator, so it keeps
// its historic stricter set.
var previewOnlyFragments = [...]string{
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
	normalized := redact.NormalizeKey(key)
	if redact.IsSecretNormalized(normalized) {
		return true
	}
	for _, fragment := range previewOnlyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
