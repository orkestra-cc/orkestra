package utils

import (
	"regexp"
)

// regexMetaChars matches regex metacharacters that need escaping
var regexMetaChars = regexp.MustCompile(`[.*+?^${}()|[\]\\]`)

// EscapeRegex escapes special regex characters in a string to prevent ReDoS attacks
// Use this when building regex patterns from user input
func EscapeRegex(s string) string {
	return regexMetaChars.ReplaceAllString(s, `\$0`)
}

// TruncateString safely truncates a string to maxLen characters
// Useful for logging without exposing full sensitive values
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// MaskEmail masks an email address for safe logging
// Example: "user@example.com" -> "u***@example.com"
func MaskEmail(email string) string {
	if email == "" {
		return "[EMPTY]"
	}

	atIndex := -1
	for i, c := range email {
		if c == '@' {
			atIndex = i
			break
		}
	}

	if atIndex <= 0 {
		return "[INVALID_EMAIL]"
	}

	// Show first character, mask the rest of local part
	localPart := email[:atIndex]
	domain := email[atIndex:]

	if len(localPart) <= 1 {
		return localPart + "***" + domain
	}

	return string(localPart[0]) + "***" + domain
}

// MaskToken masks a token for safe logging
// Example: "abc123xyz789" -> "abc1***789"
func MaskToken(token string) string {
	if token == "" {
		return "[EMPTY]"
	}

	if len(token) <= 8 {
		return "[REDACTED]"
	}

	return token[:4] + "***" + token[len(token)-4:]
}
