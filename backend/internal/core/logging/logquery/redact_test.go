package logquery

import (
	"reflect"
	"testing"
)

func TestRedactSensitiveKeysRecursively(t *testing.T) {
	input := map[string]any{
		"Password":       "p@ss",
		"client_secret":  "secret",
		"accessToken":    "token",
		"Authorization":  "Bearer value",
		"session_cookie": "cookie",
		"EMAIL":          "person@example.com",
		"phoneNumber":    "+39 123",
		"postalAddress":  "Via Roma 1",
		"user_id":        "user-1",
		"nested": map[string]any{
			"safe": "kept",
			"items": []any{
				map[string]any{"refresh_token": "nested-token", "trace_id": "trace-nested"},
				map[string]any{"UserId": "nested-user", "duration_ms": float64(7)},
			},
		},
		"trace_id":    "trace-1",
		"span_id":     "span-1",
		"request_id":  "request-1",
		"route":       "/v1/example",
		"duration_ms": float64(12),
	}

	got := Redact(input)
	want := map[string]any{
		"Password":       redactedValue,
		"client_secret":  redactedValue,
		"accessToken":    redactedValue,
		"Authorization":  redactedValue,
		"session_cookie": redactedValue,
		"EMAIL":          redactedValue,
		"phoneNumber":    redactedValue,
		"postalAddress":  redactedValue,
		"user_id":        redactedValue,
		"nested": map[string]any{
			"safe": "kept",
			"items": []any{
				map[string]any{"refresh_token": redactedValue, "trace_id": "trace-nested"},
				map[string]any{"UserId": redactedValue, "duration_ms": float64(7)},
			},
		},
		"trace_id":    "trace-1",
		"span_id":     "span-1",
		"request_id":  "request-1",
		"route":       "/v1/example",
		"duration_ms": float64(12),
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Redact() = %#v, want %#v", got, want)
	}
	if input["Password"] != "p@ss" {
		t.Error("Redact mutated the source map")
	}
}

func TestSensitiveKeyMatchingIsCaseInsensitiveAndSeparatorAgnostic(t *testing.T) {
	for _, key := range []string{
		"PASSWORD_HASH",
		"ClientSecret",
		"id-token",
		"AUTHORIZATION_HEADER",
		"set.cookie",
		"ContactEmail",
		"phone_number",
		"billing-address",
		"UserID",
	} {
		t.Run(key, func(t *testing.T) {
			got := Redact(map[string]any{key: "sensitive"}).(map[string]any)
			if got[key] != redactedValue {
				t.Errorf("key %q value = %#v, want redacted", key, got[key])
			}
		})
	}
}
