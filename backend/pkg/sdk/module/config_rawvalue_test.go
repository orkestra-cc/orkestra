package module

import (
	"context"
	"testing"
)

// TestGetRawValue_PresenceVsAbsence pins the distinction GetValue cannot
// make: an operator-cleared key ("", true) versus a key the document never
// had ("", false). ADR-0017 D1's "clear sessionAbsoluteTTL to disable the
// session cap" exit depends on this — see auth's session_cap.go.
func TestGetRawValue_PresenceVsAbsence(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestConfigService(t)
	seedModuleDoc(t, repo, "rawvalue", map[string]string{
		"present": "hello",
		"cleared": "",
	})

	if v, ok := svc.GetRawValue(ctx, "rawvalue", "present"); !ok || v != "hello" {
		t.Errorf("present key: got (%q, %v), want (%q, true)", v, ok, "hello")
	}
	if v, ok := svc.GetRawValue(ctx, "rawvalue", "cleared"); !ok || v != "" {
		t.Errorf("cleared key: got (%q, %v), want (\"\", true)", v, ok)
	}
	if v, ok := svc.GetRawValue(ctx, "rawvalue", "absent"); ok || v != "" {
		t.Errorf("absent key: got (%q, %v), want (\"\", false)", v, ok)
	}
	if v, ok := svc.GetRawValue(ctx, "no-such-module", "whatever"); ok || v != "" {
		t.Errorf("missing document: got (%q, %v), want (\"\", false)", v, ok)
	}
}

// TestGetRawValue_ActiveEnvironment proves the accessor reads the active
// environment profile, not the legacy top-level ConfigValues, once a
// document has environment profiles — matching GetValue's own precedence.
func TestGetRawValue_ActiveEnvironment(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestConfigService(t)
	seedModuleDocWithEnv(t, repo, "rawvalue-env", "sandbox", map[string]string{
		"cleared": "",
		"present": "sandboxed",
	})
	if err := svc.SetActiveEnvironment(ctx, "rawvalue-env", "sandbox"); err != nil {
		t.Fatalf("SetActiveEnvironment: %v", err)
	}

	if v, ok := svc.GetRawValue(ctx, "rawvalue-env", "present"); !ok || v != "sandboxed" {
		t.Errorf("present key in active env: got (%q, %v), want (%q, true)", v, ok, "sandboxed")
	}
	if v, ok := svc.GetRawValue(ctx, "rawvalue-env", "cleared"); !ok || v != "" {
		t.Errorf("cleared key in active env: got (%q, %v), want (\"\", true)", v, ok)
	}
	if v, ok := svc.GetRawValue(ctx, "rawvalue-env", "absent"); ok || v != "" {
		t.Errorf("absent key in active env: got (%q, %v), want (\"\", false)", v, ok)
	}
}
