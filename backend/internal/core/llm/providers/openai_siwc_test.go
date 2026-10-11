package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// failedEvent is a response.failed event carrying code and a message that
// must never surface.
func failedEvent(code string) string {
	return ev("response.failed", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"`+code+`","message":"sk-live-leak user@example.test"}}}`)
}

// Review Focus 5 (provider half): a plan-route code inside the stream is
// classified by the SIWC map, not by the generic failedInfo (which would
// make subscription_sharing_* a fallbackable 502).
func TestOpenAISIWC_InStreamSubscriptionErrors(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{"subscription_sharing_usage_limit_exceeded", iface.ErrLLMQuotaExhausted},
		{"subscription_sharing_user_not_eligible", iface.ErrLLMPlanUsageDisabled},
		{"subscription_sharing_unsupported_capability", iface.ErrLLMCapabilityMismatch},
		{"subscription_sharing_usage_unavailable", iface.ErrLLMProviderUnavailable},
		{"subscription_sharing_invalid_user", ErrSIWCInvalidUser},
		{"chatpass_v2_scope_not_authorized", ErrSIWCRouteNotSupported},
	}
	for _, c := range cases {
		for _, shape := range []string{"response.failed", "error"} {
			p, _ := newOAI(t, func(w http.ResponseWriter, _ *http.Request) {
				if shape == "error" {
					respSSE(w, ev("error", `{"type":"error","code":"`+c.code+`","message":"sk-live-leak","param":null,"sequence_number":1}`))
					return
				}
				respSSE(w, failedEvent(c.code))
			}, iface.LLMCredentialKindUserAccount)
			_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("u")}, noopSink)
			if !errors.Is(err, c.want) || strings.Contains(err.Error(), "sk-live-leak") {
				t.Errorf("%s/%s → %v, want %v", shape, c.code, err, c.want)
			}
			var f *PlanRouteFailure
			if !errors.As(err, &f) || f.Code != c.code {
				t.Errorf("%s/%s: no plan-route identifiers on %v", shape, c.code, err)
			}
		}
	}
	// The API-key profile keeps the generic mapping.
	p, _ := newOAI(t, func(w http.ResponseWriter, _ *http.Request) {
		respSSE(w, failedEvent("subscription_sharing_usage_limit_exceeded"))
	}, iface.LLMCredentialKindOrg)
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("u")}, noopSink); errors.Is(err, iface.ErrLLMQuotaExhausted) {
		t.Errorf("org profile must not read plan codes: %v", err)
	}
}

// Before the stream, the plan route's 401/403 are never "rotate the API
// key" (ErrUnauthorized → credential_rejected): 401 is the refreshable
// invalid-user, 403 a plan code or plan usage disabled, 503 unavailable.
// The request id is kept for the log; provider text never is.
func TestOpenAISIWC_PreStreamHTTPErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{401, `{"error":{"message":"sk-live-leak"}}`, ErrSIWCInvalidUser},
		{401, `{"error":{"code":"subscription_sharing_invalid_user","message":"sk-live-leak"}}`, ErrSIWCInvalidUser},
		{403, `{"error":{"code":"subscription_sharing_user_not_eligible","message":"sk-live-leak"}}`, iface.ErrLLMPlanUsageDisabled},
		{403, `{"error":{"message":"sk-live-leak"}}`, iface.ErrLLMPlanUsageDisabled},
		{429, `{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"sk-live-leak"}}`, iface.ErrLLMQuotaExhausted},
		{503, `{"error":{"message":"sk-live-leak"}}`, iface.ErrLLMProviderUnavailable},
	}
	for _, c := range cases {
		p, _ := newOAI(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", "req_abc123")
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}, iface.LLMCredentialKindUserAccount)
		_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("u")}, noopSink)
		if !errors.Is(err, c.want) || errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "sk-live-leak") {
			t.Errorf("%d %s → %v, want %v", c.status, c.body, err, c.want)
			continue
		}
		var f *PlanRouteFailure
		if !errors.As(err, &f) || f.Status != c.status || f.RequestID != "req_abc123" {
			t.Errorf("%d: plan-route identifiers = %+v", c.status, f)
		}
	}
}

// The plan route takes developer items and instructions; a system item is
// refused by the adapter too, not only by the gateway's validation.
func TestOpenAISIWC_RefusesSystemItems(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, _ *http.Request) { respSSE(w, completedEvent(1, 1)) }, iface.LLMCredentialKindUserAccount)
	req := iface.ChatRequest{Caller: "t", Messages: []iface.ChatMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}}}
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("system item err = %v", err)
	}
}
