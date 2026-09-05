package errcode

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// The 429 envelope is useless to a client that cannot tell when to come
// back, and Huma only copies headers off an error that satisfies
// huma.HeadersError. Pin both halves.
func TestError_SatisfiesHumaHeadersError(t *testing.T) {
	var _ huma.HeadersError = (*Error)(nil)
}

func TestWithHeader_SetsAndAccumulates(t *testing.T) {
	e := TooManyRequests(AuthTooManyAttempts, "Too many failed attempts.").
		WithHeader("Retry-After", "42")

	if e.Status != http.StatusTooManyRequests {
		t.Fatalf("Status = %d, want 429", e.Status)
	}
	if e.Code != "auth.too_many_attempts" {
		t.Fatalf("Code = %q, want auth.too_many_attempts", e.Code)
	}
	got := e.GetHeaders().Get("Retry-After")
	if got != "42" {
		t.Fatalf("Retry-After = %q, want 42", got)
	}
}

// Headers must never reach the JSON body — the envelope is a frozen
// wire contract ({status,title,detail,code}).
func TestHeaders_NotSerialised(t *testing.T) {
	e := TooManyRequests(AuthTooManyAttempts, "d").WithHeader("Retry-After", "1")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "Retry-After") || strings.Contains(string(b), "Headers") {
		t.Fatalf("headers leaked into the body: %s", b)
	}
}

func TestGetHeaders_NilSafeOnUnadornedError(t *testing.T) {
	e := New(http.StatusTooManyRequests, AuthTooManyAttempts, "d")
	if h := e.GetHeaders(); h == nil {
		t.Fatal("GetHeaders must return a non-nil (possibly empty) Header")
	}
}
