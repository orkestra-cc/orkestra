package module

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

	if v, ok, err := svc.GetRawValue(ctx, "rawvalue", "present"); err != nil || !ok || v != "hello" {
		t.Errorf("present key: got (%q, %v, %v), want (%q, true, nil)", v, ok, err, "hello")
	}
	if v, ok, err := svc.GetRawValue(ctx, "rawvalue", "cleared"); err != nil || !ok || v != "" {
		t.Errorf("cleared key: got (%q, %v, %v), want (\"\", true, nil)", v, ok, err)
	}
	if v, ok, err := svc.GetRawValue(ctx, "rawvalue", "absent"); err != nil || ok || v != "" {
		t.Errorf("absent key: got (%q, %v, %v), want (\"\", false, nil)", v, ok, err)
	}
	// A module with no document has said nothing about any key — an
	// ABSENCE, and explicitly not an error. Only a failed read is an error.
	if v, ok, err := svc.GetRawValue(ctx, "no-such-module", "whatever"); err != nil || ok || v != "" {
		t.Errorf("missing document: got (%q, %v, %v), want (\"\", false, nil)", v, ok, err)
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

	if v, ok, err := svc.GetRawValue(ctx, "rawvalue-env", "present"); err != nil || !ok || v != "sandboxed" {
		t.Errorf("present key in active env: got (%q, %v, %v), want (%q, true, nil)", v, ok, err, "sandboxed")
	}
	if v, ok, err := svc.GetRawValue(ctx, "rawvalue-env", "cleared"); err != nil || !ok || v != "" {
		t.Errorf("cleared key in active env: got (%q, %v, %v), want (\"\", true, nil)", v, ok, err)
	}
	if v, ok, err := svc.GetRawValue(ctx, "rawvalue-env", "absent"); err != nil || ok || v != "" {
		t.Errorf("absent key in active env: got (%q, %v, %v), want (\"\", false, nil)", v, ok, err)
	}
}

// TestGetRawValue_ReadFailureIsNotAbsence pins the third outcome, and the
// reason the signature carries an error at all.
//
// GetRawValue used to collapse err != nil into ("", false) — the same pair it
// returns for a key the document genuinely does not have. A caller whose
// "absent" branch substitutes a default then applies that default during a
// module_configs outage. auth's SessionAbsoluteTTL is exactly such a caller:
// its absent branch is the 30-day session cap, so a transient read failure
// silently re-armed a cap an operator had deliberately disabled and signed out
// every session older than 30 days that refreshed in that window. ADR-0017.
//
// Hermetic: the client points at a port nothing listens on with a 50ms server
// selection timeout, so FindByName fails fast without a Mongo server.
func TestGetRawValue_ReadFailureIsNotAbsence(t *testing.T) {
	ctx := context.Background()
	client, err := mongo.NewClient(options.Client().
		ApplyURI("mongodb://127.0.0.1:1/").
		SetServerSelectionTimeout(50 * time.Millisecond).
		SetConnectTimeout(50 * time.Millisecond))
	if err != nil {
		t.Fatalf("mongo.NewClient: %v", err)
	}
	// Deliberately NOT connected: every operation fails server selection.
	svc := NewModuleConfigService(
		&ModuleConfigRepository{collection: client.Database("unreachable").Collection(moduleConfigCollection)},
		nil, nil,
	)

	v, ok, err := svc.GetRawValue(ctx, "auth", "sessionAbsoluteTTL")
	if err == nil {
		t.Fatal("a failed read reported no error — a caller cannot then tell it apart from an absent key, and will apply its default")
	}
	if ok {
		t.Errorf("present = true on a failed read; got value %q", v)
	}
}
