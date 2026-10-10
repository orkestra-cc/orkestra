package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErrUnauthorized: the provider rejected the credential (401/403). It is
// always wrapped together with iface.ErrLLMProviderUnavailable so consumers
// see an ordinary availability error, while the router/admin "test" endpoint
// can match it to report credential_rejected and tell the operator the key
// is bad.
var ErrUnauthorized = errors.New("llm providers: credential rejected")

// quotaErrorCodes is the closed allowlist of billing markers. A
// match, or a bare 402, means "the plan/credits are exhausted", which the
// router never retries on another credential of the same kind.
var quotaErrorCodes = map[string]bool{
	"insufficient_quota":         true,
	"billing_hard_limit_reached": true,
	"billing_not_active":         true,
	"quota_exceeded":             true,
	"rate_limit_exceeded_quota":  true,
}

// capabilityErrorCodes: the request asked for more than the model can do.
var capabilityErrorCodes = map[string]bool{
	"context_length_exceeded": true,
	"model_not_found":         true,
	"unsupported_parameter":   true,
	"unsupported_value":       true,
}

// ErrorInfo is the allowlisted projection of a provider error response.
// Free text (the message) is deliberately never captured: it can echo
// request content or credentials.
type ErrorInfo struct {
	Status    int
	Type      string
	Code      string
	Param     string
	RequestID string
	// QuotaID: the Google QuotaFailure violation id (e.g.
	// "GenerateRequestsPerDayPerProjectPerModel-FreeTier"), identifier-checked.
	QuotaID string
}

const (
	maxErrorBody   = 64 << 10
	maxIdentLength = 64
	maxRequestID   = 128
)

// ReadErrorInfo extracts the structured fields of a provider error body
// and discards everything else. Fields that are not short identifiers are
// dropped, so a provider cannot smuggle free text into logs or the ledger.
func ReadErrorInfo(resp *http.Response) ErrorInfo {
	info := ErrorInfo{Status: resp.StatusCode}
	for _, h := range []string{"X-Request-Id", "Request-Id", "Anthropic-Request-Id"} {
		if v := resp.Header.Get(h); v != "" && isIdentifier(v, maxRequestID) {
			info.RequestID = v
			break
		}
	}
	if resp.Body == nil {
		return info
	}
	body, err := ReadAllBounded(resp.Body, maxErrorBody)
	if err != nil {
		return info
	}
	type payload struct {
		Error struct {
			Type    any              `json:"type"`    // OpenAI / Anthropic
			Code    any              `json:"code"`    // OpenAI string; Gemini numeric (the HTTP status)
			Param   any              `json:"param"`   // OpenAI
			Status  any              `json:"status"`  // Gemini: RESOURCE_EXHAUSTED, ...
			Details []map[string]any `json:"details"` // Gemini: google.rpc.QuotaFailure, ...
		} `json:"error"`
	}
	var p payload
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		// Gemini wraps streamed errors in a one-element array.
		var arr []payload
		if json.Unmarshal(trimmed, &arr) != nil || len(arr) == 0 {
			return info
		}
		p = arr[0]
	} else if json.Unmarshal(body, &p) != nil {
		return info
	}
	info.Type = identifier(p.Error.Type)
	if info.Type == "" {
		info.Type = identifier(p.Error.Status)
	}
	info.Code = identifier(p.Error.Code)
	info.Param = identifier(p.Error.Param)
	info.QuotaID = QuotaIDFromDetails(p.Error.Details)
	return info
}

// QuotaIDFromDetails returns the first quotaId of a google.rpc.QuotaFailure
// entry that passes the identifier check ("" when there is none).
func QuotaIDFromDetails(details []map[string]any) string {
	for _, d := range details {
		if t, _ := d["@type"].(string); !strings.HasSuffix(t, ".QuotaFailure") {
			continue
		}
		violations, _ := d["violations"].([]any)
		for _, v := range violations {
			m, _ := v.(map[string]any)
			if id := identifier(m["quotaId"]); id != "" {
				return id
			}
		}
	}
	return ""
}

// identifier returns v when it is a short string of [A-Za-z0-9_.:-]; numbers,
// nulls and anything else yield "".
func identifier(v any) string {
	s, ok := v.(string)
	if !ok || !isIdentifier(s, maxIdentLength) {
		return ""
	}
	return s
}

func isIdentifier(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == ':', r == '-':
		default:
			return false
		}
	}
	return true
}

// Classify maps an ErrorInfo onto the module's sentinels.
func Classify(info ErrorInfo) error {
	detail := fmt.Sprintf("status %d, type %q, code %q, request %s", info.Status, info.Type, info.Code, info.RequestID)
	switch {
	case info.Status == http.StatusPaymentRequired || quotaErrorCodes[info.Code] || quotaErrorCodes[info.Type]:
		return fmt.Errorf("%w (%s)", iface.ErrLLMQuotaExhausted, detail)
	case info.Status == http.StatusTooManyRequests && strings.Contains(info.QuotaID, "PerDay"):
		// Google's daily quotas do not recover within a retry window, unlike
		// the per-minute ones that share the RESOURCE_EXHAUSTED status.
		return fmt.Errorf("%w (%s)", iface.ErrLLMQuotaExhausted, detail)
	case info.Status == http.StatusUnauthorized || info.Status == http.StatusForbidden:
		return fmt.Errorf("%w: %w (%s)", ErrUnauthorized, iface.ErrLLMProviderUnavailable, detail)
	case info.Status == http.StatusTooManyRequests || info.Status == http.StatusRequestTimeout || info.Status >= 500:
		return fmt.Errorf("%w (%s)", iface.ErrLLMProviderUnavailable, detail)
	case info.Status == http.StatusNotFound || capabilityErrorCodes[info.Code] || capabilityErrorCodes[info.Type]:
		return fmt.Errorf("%w (%s)", iface.ErrLLMCapabilityMismatch, detail)
	case info.Status >= 400:
		return fmt.Errorf("%w (%s)", iface.ErrLLMInvalidRequest, detail)
	}
	return fmt.Errorf("%w (%s)", iface.ErrLLMProviderUnavailable, detail)
}
