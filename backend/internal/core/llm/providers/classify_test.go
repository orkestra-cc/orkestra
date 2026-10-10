package providers

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func respWith(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"X-Request-Id": {"req_1"}}}
}

func TestReadErrorInfo_ExtractsOnlyAllowlistedFields(t *testing.T) {
	info := ReadErrorInfo(respWith(429, `{"error":{"type":"insufficient_quota","code":"insufficient_quota","param":null,"message":"You exceeded your current quota, see sk-live-SECRET"}}`))
	if info.Status != 429 || info.Type != "insufficient_quota" || info.Code != "insufficient_quota" || info.RequestID != "req_1" {
		t.Fatalf("info = %+v", info)
	}
}

func TestReadErrorInfo_RealProviderShapes(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		info := ReadErrorInfo(respWith(400, `{"error":{"message":"This model's maximum context length is 8192 tokens","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`))
		if info.Type != "invalid_request_error" || info.Code != "context_length_exceeded" || info.Param != "messages" {
			t.Fatalf("info = %+v", info)
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		resp := respWith(529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"},"request_id":"req_011"}`)
		resp.Header = http.Header{"Request-Id": {"req_hdr"}}
		info := ReadErrorInfo(resp)
		if info.Status != 529 || info.Type != "overloaded_error" || info.Code != "" || info.RequestID != "req_hdr" {
			t.Fatalf("info = %+v", info)
		}
	})
	t.Run("gemini numeric code and status", func(t *testing.T) {
		info := ReadErrorInfo(respWith(429, `{"error":{"code":429,"message":"Resource has been exhausted","status":"RESOURCE_EXHAUSTED"}}`))
		if info.Status != 429 || info.Type != "RESOURCE_EXHAUSTED" || info.Code != "" {
			t.Fatalf("info = %+v", info)
		}
	})
	t.Run("gemini array-wrapped (stream) error", func(t *testing.T) {
		info := ReadErrorInfo(respWith(400, `[{"error":{"code":400,"message":"x","status":"INVALID_ARGUMENT"}}]`))
		if info.Type != "INVALID_ARGUMENT" {
			t.Fatalf("info = %+v", info)
		}
	})
}

func TestReadErrorInfo_DropsFieldsThatAreNotIdentifiers(t *testing.T) {
	info := ReadErrorInfo(respWith(400, `{"error":{"type":"Bearer sk-live-SECRET with spaces","code":"`+strings.Repeat("a", 200)+`","param":"line1\nline2"}}`))
	if info.Type != "" || info.Code != "" || info.Param != "" {
		t.Fatalf("free-text must not survive: %+v", info)
	}
}

func TestReadErrorInfo_IgnoresHugeOrNonJSONBodies(t *testing.T) {
	info := ReadErrorInfo(respWith(503, strings.Repeat("x", 70<<10)))
	if info.Status != 503 || info.Type != "" || info.Code != "" {
		t.Fatalf("info = %+v", info)
	}
	info = ReadErrorInfo(respWith(401, "<html>nope</html>"))
	if info.Status != 401 || info.Code != "" {
		t.Fatalf("info = %+v", info)
	}
	// nil body must not panic
	info = ReadErrorInfo(&http.Response{StatusCode: 500, Header: http.Header{}})
	if info.Status != 500 {
		t.Fatalf("info = %+v", info)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		info ErrorInfo
		want error
	}{
		{ErrorInfo{Status: 402}, iface.ErrLLMQuotaExhausted},
		{ErrorInfo{Status: 429, Code: "insufficient_quota"}, iface.ErrLLMQuotaExhausted},
		{ErrorInfo{Status: 429, Type: "billing_hard_limit_reached"}, iface.ErrLLMQuotaExhausted},
		{ErrorInfo{Status: 429}, iface.ErrLLMProviderUnavailable},
		{ErrorInfo{Status: 429, Type: "RESOURCE_EXHAUSTED"}, iface.ErrLLMProviderUnavailable}, // bare Gemini 429 is rate limiting (5.12)
		{ErrorInfo{Status: 500}, iface.ErrLLMProviderUnavailable},
		{ErrorInfo{Status: 503, Type: "overloaded_error"}, iface.ErrLLMProviderUnavailable},
		{ErrorInfo{Status: 401}, ErrUnauthorized},
		{ErrorInfo{Status: 403}, ErrUnauthorized},
		{ErrorInfo{Status: 401}, iface.ErrLLMProviderUnavailable}, // consumers see unavailable
		{ErrorInfo{Status: 403}, iface.ErrLLMProviderUnavailable},
		{ErrorInfo{Status: 400, Code: "context_length_exceeded"}, iface.ErrLLMCapabilityMismatch},
		{ErrorInfo{Status: 400}, iface.ErrLLMInvalidRequest},
		{ErrorInfo{Status: 404}, iface.ErrLLMCapabilityMismatch}, // unknown model id
	}
	for _, c := range cases {
		if got := Classify(c.info); !errors.Is(got, c.want) {
			t.Errorf("Classify(%+v) = %v, want %v", c.info, got, c.want)
		}
	}
}

func TestClassify_UnauthorizedIsNotAQuotaOrInvalidRequest(t *testing.T) {
	got := Classify(ErrorInfo{Status: 401})
	if errors.Is(got, iface.ErrLLMQuotaExhausted) || errors.Is(got, iface.ErrLLMInvalidRequest) {
		t.Fatalf("401 misclassified: %v", got)
	}
}

func TestClassify_MessageCarriesNoBodyText(t *testing.T) {
	got := Classify(ErrorInfo{Status: 429, Code: "insufficient_quota", RequestID: "req_1"}).Error()
	for _, want := range []string{"429", "insufficient_quota", "req_1"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}
