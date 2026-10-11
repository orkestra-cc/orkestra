package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Sign in with ChatGPT (SIWC) error identities. They live in providers, not
// services, because the OpenAI adapter classifies plan-route codes too and
// providers cannot import services; services re-exports what its callers use.
//
// Every sentinel but ErrSIWCRefreshTerminal is returned wrapped together with
// iface.ErrLLMProviderUnavailable ("%w: %w", the ErrUnauthorized precedent),
// so a consumer that only knows the iface sentinels still classifies it.
// ErrSIWCRefreshTerminal never leaves the module: the accounts service turns
// it into iface.ErrLLMUserAccountReauth.
var (
	ErrSIWCRefreshTerminal   = errors.New("llm siwc: refresh token is no longer usable")      // → needs_reauth
	ErrSIWCInvalidUser       = errors.New("llm siwc: subscriber context not validated")       // 401: refresh once, reauth only if terminal
	ErrSIWCRouteNotSupported = errors.New("llm siwc: client or grant configuration rejected") // not retryable, not a reauth
	ErrSIWCInvalidClient     = errors.New("llm siwc: invalid client configuration")
	ErrSIWCExchangeFailed    = errors.New("llm siwc: token exchange failed")
)

// siwcRefreshTerminalCodes: "unusable refresh token, clear tokens and repeat
// OAuth with the saved client id" (guide "Errors and recovery", 2026-10-10).
var siwcRefreshTerminalCodes = map[string]bool{
	"invalid_grant": true, "invalid_refresh_token": true, "token_expired": true,
	"refresh_token_expired": true, "refresh_token_invalidated": true, "refresh_token_reused": true,
}

// ClassifySIWCCode maps the documented OpenAI plan-route and OAuth codes
// onto the module's sentinels. Only an identifier-shaped code is echoed in
// the error text; anything else is dropped.
func ClassifySIWCCode(code string) error {
	if !isIdentifier(code, maxIdentLength) {
		code = ""
	}
	unavailable := func(e error) error {
		return fmt.Errorf("%w: %w (siwc code %q)", e, iface.ErrLLMProviderUnavailable, code)
	}
	switch code {
	case "subscription_sharing_user_not_eligible":
		return fmt.Errorf("%w (siwc code %q)", iface.ErrLLMPlanUsageDisabled, code)
	case "subscription_sharing_usage_limit_exceeded":
		return fmt.Errorf("%w (siwc code %q)", iface.ErrLLMQuotaExhausted, code)
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		return fmt.Errorf("%w (siwc code %q)", iface.ErrLLMProviderUnavailable, code)
	case "subscription_sharing_unsupported_capability":
		return fmt.Errorf("%w (siwc code %q)", iface.ErrLLMCapabilityMismatch, code)
	case "subscription_sharing_route_not_supported", "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		return unavailable(ErrSIWCRouteNotSupported)
	case "subscription_sharing_invalid_user":
		return unavailable(ErrSIWCInvalidUser)
	case "invalid_client":
		return unavailable(ErrSIWCInvalidClient)
	}
	if siwcRefreshTerminalCodes[code] {
		return fmt.Errorf("%w (siwc code %q)", ErrSIWCRefreshTerminal, code)
	}
	return fmt.Errorf("%w (siwc code %q)", iface.ErrLLMProviderUnavailable, code)
}

// IsSIWCPlanCode reports whether code belongs to the plan-route families
// that ClassifySIWCCode owns (subscription_sharing_*, chatpass_v2_*).
func IsSIWCPlanCode(code string) bool {
	return strings.HasPrefix(code, "subscription_sharing_") || strings.HasPrefix(code, "chatpass_v2_")
}

// PlanRouteFailure is a classified ChatGPT plan-route failure together with
// its allowlisted identifiers (HTTP status, plan code, request id), so the
// router can log them (ruling 5.3: never provider text, only these). Its
// text and its Unwrap chain are the classified error's.
type PlanRouteFailure struct {
	Status    int    // 0 for a failure delivered inside the stream
	Code      string // identifier-checked, may be ""
	RequestID string // identifier-checked, may be ""
	err       error
}

func (f *PlanRouteFailure) Error() string { return f.err.Error() }
func (f *PlanRouteFailure) Unwrap() error { return f.err }

// ClassifySIWCHTTP classifies a pre-stream HTTP failure on the ChatGPT plan
// route: a documented plan code wins; otherwise 401 is ErrSIWCInvalidUser
// (refresh once, then reauth only on a terminal refresh) and 403 is "plan
// usage not granted"; anything else falls back to Classify. Neither 401 nor
// 403 is ever ErrUnauthorized: the remedy is the user's account, not the
// operator rotating an API key. The result is a *PlanRouteFailure.
func ClassifySIWCHTTP(info ErrorInfo) error {
	var err error
	switch {
	case IsSIWCPlanCode(info.Code):
		err = ClassifySIWCCode(info.Code)
	case info.Status == http.StatusUnauthorized:
		err = fmt.Errorf("%w: %w (status 401, request %s)", ErrSIWCInvalidUser, iface.ErrLLMProviderUnavailable, info.RequestID)
	case info.Status == http.StatusForbidden:
		err = fmt.Errorf("%w (status 403, request %s)", iface.ErrLLMPlanUsageDisabled, info.RequestID)
	default:
		err = Classify(info)
	}
	return &PlanRouteFailure{Status: info.Status, Code: identifier(info.Code), RequestID: info.RequestID, err: err}
}

// classifySIWCEvent classifies a response.failed / error event on the plan
// route: a plan code goes through ClassifySIWCCode, anything else through
// the generic event mapping.
func classifySIWCEvent(code string) error {
	code = identifier(code)
	if !IsSIWCPlanCode(code) {
		return Classify(failedInfo(code))
	}
	return &PlanRouteFailure{Code: code, err: ClassifySIWCCode(code)}
}

// OAuthErrorCode returns the identifier-checked error code of an OAuth error
// body: the RFC 6749 flat {"error":"code"} or the nested {"error":{"code":…}}
// API shape. Everything else in the body (error_description, messages) is
// discarded; "" when no identifier is found.
func OAuthErrorCode(body []byte) string {
	var p struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &p) != nil || len(p.Error) == 0 {
		return ""
	}
	var flat string
	if json.Unmarshal(p.Error, &flat) == nil {
		return identifier(flat)
	}
	var nested struct {
		Code any `json:"code"`
		Type any `json:"type"`
	}
	if json.Unmarshal(p.Error, &nested) != nil {
		return ""
	}
	if c := identifier(nested.Code); c != "" {
		return c
	}
	return identifier(nested.Type)
}
