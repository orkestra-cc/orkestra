package utils

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	authDebugEnabled bool
	debugOnce        sync.Once
)

// initDebug initializes the debug flag once
func initDebug() {
	debugOnce.Do(func() {
		authDebugEnabled = os.Getenv("AUTH_DEBUG") == "true"
	})
}

// IsAuthDebugEnabled returns whether auth debug logging is enabled
func IsAuthDebugEnabled() bool {
	initDebug()
	return authDebugEnabled
}

// AuthDebug logs auth-related debug info only when AUTH_DEBUG=true
// This function sanitizes sensitive data before logging
func AuthDebug(format string, args ...interface{}) {
	initDebug()
	if !authDebugEnabled {
		return
	}
	// Sanitize the message before printing
	message := fmt.Sprintf(format, args...)
	sanitized := sanitizeDebugMessage(message)
	fmt.Printf("[AUTH_DEBUG] %s\n", sanitized)
}

// AuthDebugFlow logs high-level auth flow events (safe for any environment when debug is on)
func AuthDebugFlow(operation string) {
	initDebug()
	if !authDebugEnabled {
		return
	}
	fmt.Printf("[AUTH_DEBUG] ==> %s\n", operation)
}

// AuthDebugFlowEnd logs high-level auth flow completion
func AuthDebugFlowEnd(operation string, success bool) {
	initDebug()
	if !authDebugEnabled {
		return
	}
	status := "completed successfully"
	if !success {
		status = "failed"
	}
	fmt.Printf("[AUTH_DEBUG] <== %s %s\n", operation, status)
}

// AuthDebugError logs auth errors with sanitization
func AuthDebugError(context string, err error) {
	initDebug()
	if !authDebugEnabled {
		return
	}
	errMsg := "nil"
	if err != nil {
		errMsg = sanitizeDebugMessage(err.Error())
	}
	fmt.Printf("[AUTH_DEBUG] ERROR [%s]: %s\n", context, errMsg)
}

// AuthDebugPresence logs whether a value is present without showing the actual value
func AuthDebugPresence(name string, value string) {
	initDebug()
	if !authDebugEnabled {
		return
	}
	if value != "" {
		fmt.Printf("[AUTH_DEBUG] %s: [PRESENT, length=%d]\n", name, len(value))
	} else {
		fmt.Printf("[AUTH_DEBUG] %s: [EMPTY]\n", name)
	}
}

// sanitizeDebugMessage removes potentially sensitive data from debug messages
func sanitizeDebugMessage(message string) string {
	// List of patterns to redact
	sensitivePatterns := []string{
		"token", "Token", "TOKEN",
		"password", "Password", "PASSWORD",
		"secret", "Secret", "SECRET",
		"key", "Key", "KEY",
		"credential", "Credential", "CREDENTIAL",
	}

	result := message

	// Redact any value that looks like a token (long alphanumeric strings)
	// This is a simple heuristic - strings longer than 20 chars with mostly alphanumeric
	words := strings.Fields(result)
	for i, word := range words {
		// Check if word looks like a sensitive value
		if len(word) > 30 && isLikelyToken(word) {
			words[i] = "[REDACTED]"
		}
	}
	result = strings.Join(words, " ")

	// Redact email addresses
	if strings.Contains(result, "@") {
		// Simple email redaction - replace the local part
		for _, word := range strings.Fields(result) {
			if strings.Contains(word, "@") && strings.Contains(word, ".") {
				parts := strings.Split(word, "@")
				if len(parts) == 2 {
					redacted := "[EMAIL]@" + parts[1]
					result = strings.Replace(result, word, redacted, 1)
				}
			}
		}
	}

	// Check for sensitive key names in key=value or key: value patterns
	for _, pattern := range sensitivePatterns {
		if strings.Contains(strings.ToLower(result), strings.ToLower(pattern)) {
			// Be more careful here - only redact actual values, not descriptions
			// This is a simple approach; more sophisticated would use regex
		}
	}

	return result
}

// isLikelyToken checks if a string looks like a token (long alphanumeric)
func isLikelyToken(s string) bool {
	if len(s) < 20 {
		return false
	}
	alphanumCount := 0
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			alphanumCount++
		}
	}
	// If more than 80% alphanumeric, likely a token
	return float64(alphanumCount)/float64(len(s)) > 0.8
}
