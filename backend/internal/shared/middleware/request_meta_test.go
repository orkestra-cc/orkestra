package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestMeta_StoresUserAgent(t *testing.T) {
	var got string
	h := RequestMeta(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = UserAgentFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodPatch, "/v1/admin/modules/auth", nil)
	req.Header.Set("User-Agent", "Console/2.0")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "Console/2.0" {
		t.Fatalf("UserAgentFromContext = %q", got)
	}
	if UserAgentFromContext(req.Context()) != "" {
		t.Fatal("a context RequestMeta never saw must yield empty")
	}
}
