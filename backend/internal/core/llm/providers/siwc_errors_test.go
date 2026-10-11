package providers

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestClassifySIWCCode(t *testing.T) {
	cases := map[string]error{
		"subscription_sharing_user_not_eligible":      iface.ErrLLMPlanUsageDisabled,
		"subscription_sharing_usage_limit_exceeded":   iface.ErrLLMQuotaExhausted,
		"subscription_sharing_usage_unavailable":      iface.ErrLLMProviderUnavailable,
		"subscription_sharing_user_unavailable":       iface.ErrLLMProviderUnavailable,
		"subscription_sharing_unsupported_capability": iface.ErrLLMCapabilityMismatch,
		"subscription_sharing_route_not_supported":    ErrSIWCRouteNotSupported,
		"chatpass_v2_scope_not_authorized":            ErrSIWCRouteNotSupported,
		"chatpass_v2_invalid_authorization_context":   ErrSIWCRouteNotSupported,
		"subscription_sharing_invalid_user":           ErrSIWCInvalidUser,
		"invalid_client":                              ErrSIWCInvalidClient,
		"invalid_grant":                               ErrSIWCRefreshTerminal,
		"invalid_refresh_token":                       ErrSIWCRefreshTerminal,
		"token_expired":                               ErrSIWCRefreshTerminal,
		"refresh_token_expired":                       ErrSIWCRefreshTerminal,
		"refresh_token_invalidated":                   ErrSIWCRefreshTerminal,
		"refresh_token_reused":                        ErrSIWCRefreshTerminal,
		"something_else":                              iface.ErrLLMProviderUnavailable,
		"":                                            iface.ErrLLMProviderUnavailable,
	}
	for code, want := range cases {
		if got := ClassifySIWCCode(code); !errors.Is(got, want) {
			t.Errorf("%s → %v, want %v", code, got, want)
		}
	}
}

// 3.T4.2: the module-internal sentinels carry an iface identity so a
// consumer (and MapError) can classify them; RefreshTerminal does not, it
// never leaves the module.
func TestClassifySIWCCode_InternalSentinelsWrapProviderUnavailable(t *testing.T) {
	for _, code := range []string{"subscription_sharing_route_not_supported", "subscription_sharing_invalid_user", "invalid_client"} {
		if err := ClassifySIWCCode(code); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Errorf("%s → %v, want it to wrap ErrLLMProviderUnavailable", code, err)
		}
	}
	if err := ClassifySIWCCode("invalid_grant"); errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("terminal refresh must not look transient: %v", err)
	}
}

func TestClassifySIWCCode_NeverEchoesNonIdentifierText(t *testing.T) {
	err := ClassifySIWCCode("bad code with spaces sk-live-x")
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Contains(err.Error(), "sk-live-x") {
		t.Fatalf("err = %v", err)
	}
}

func TestClassifySIWCHTTP(t *testing.T) {
	cases := []struct {
		info ErrorInfo
		want error
	}{
		{ErrorInfo{Status: 401}, ErrSIWCInvalidUser},
		{ErrorInfo{Status: 401, Code: "subscription_sharing_invalid_user"}, ErrSIWCInvalidUser},
		{ErrorInfo{Status: 403}, iface.ErrLLMPlanUsageDisabled},
		{ErrorInfo{Status: 403, Code: "chatpass_v2_scope_not_authorized"}, ErrSIWCRouteNotSupported},
		{ErrorInfo{Status: 429, Code: "subscription_sharing_usage_limit_exceeded"}, iface.ErrLLMQuotaExhausted},
		{ErrorInfo{Status: 503}, iface.ErrLLMProviderUnavailable},
		{ErrorInfo{Status: 400, Code: "subscription_sharing_unsupported_capability"}, iface.ErrLLMCapabilityMismatch},
	}
	for _, c := range cases {
		if got := ClassifySIWCHTTP(c.info); !errors.Is(got, c.want) {
			t.Errorf("%+v → %v, want %v", c.info, got, c.want)
		}
	}
	if err := ClassifySIWCHTTP(ErrorInfo{Status: http.StatusUnauthorized}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("401 must also wrap ErrLLMProviderUnavailable: %v", err)
	}
}

func TestOAuthErrorCode(t *testing.T) {
	cases := map[string]string{
		`{"error":"invalid_grant","error_description":"refresh token sk-live-x reused"}`: "invalid_grant",
		`{"error":{"code":"subscription_sharing_invalid_user","message":"secret"}}`:      "subscription_sharing_invalid_user",
		`{"error":{"type":"invalid_request_error"}}`:                                     "invalid_request_error",
		`{"error":"not an identifier, has spaces"}`:                                      "",
		`{"detail":"diagnostic text"}`:                                                   "",
		`not json`:                                                                       "",
		`{"error":42}`:                                                                   "",
	}
	for body, want := range cases {
		if got := OAuthErrorCode([]byte(body)); got != want {
			t.Errorf("%s → %q, want %q", body, got, want)
		}
	}
}
