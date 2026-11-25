package errcode

import (
	"encoding/json"
	"net/http"
	"testing"
)

// FeatureNotConfigured is a ServiceUnavailable on the wire — the opt-in flag
// never reaches the body — and only it carries ExpectedUnavailable.
func TestFeatureNotConfigured_SameWireShapeAndOptIn(t *testing.T) {
	optIn := FeatureNotConfigured("x.feature_not_configured", "The feature is not configured")
	plain := ServiceUnavailable("x.feature_not_configured", "The feature is not configured")

	if optIn.Status != http.StatusServiceUnavailable {
		t.Fatalf("Status = %d, want 503", optIn.Status)
	}
	a, err := json.Marshal(optIn)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("opt-in changed the wire shape:\n opt-in: %s\n plain:  %s", a, b)
	}
	if !optIn.ExpectedUnavailable() {
		t.Error("FeatureNotConfigured must report ExpectedUnavailable")
	}
	for name, e := range map[string]*Error{
		"ServiceUnavailable": plain,
		"jwt_not_configured": ServiceUnavailable(AuthJWTNotConfigured, "keys missing"),
		"New(503)":           New(http.StatusServiceUnavailable, "x.y", "d"),
		"Internal":           Internal("x.y", "d"),
		"nil":                nil,
	} {
		if e.ExpectedUnavailable() {
			t.Errorf("%s must not report ExpectedUnavailable", name)
		}
	}
}
