package services

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestDefaultActorType pins the inference rule: if the caller omits ActorType,
// fall back to "user" when a userID is present, otherwise "system". This is
// the contract consumers rely on so every emit site doesn't have to set the
// field explicitly.
func TestDefaultActorType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		given  string
		userID string
		want   string
	}{
		{"explicit user is preserved", models.ActorTypeUser, "", models.ActorTypeUser},
		{"explicit system is preserved", models.ActorTypeSystem, "u-1", models.ActorTypeSystem},
		{"explicit anonymous is preserved", models.ActorTypeAnonymous, "", models.ActorTypeAnonymous},
		{"empty with user id infers user", "", "u-1", models.ActorTypeUser},
		{"empty without user id infers system", "", "", models.ActorTypeSystem},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultActorType(tc.given, tc.userID)
			if got != tc.want {
				t.Fatalf("defaultActorType(%q, %q) = %q; want %q",
					tc.given, tc.userID, got, tc.want)
			}
		})
	}
}

// TestDefaultOutcome pins the success-by-default rule. Consumers can emit
// with an empty Outcome for the hot-path successful case; failure paths
// must set it explicitly so the choice is deliberate.
func TestDefaultOutcome(t *testing.T) {
	t.Parallel()

	if got := defaultOutcome(""); got != models.OutcomeSuccess {
		t.Fatalf("empty outcome should default to success; got %q", got)
	}
	if got := defaultOutcome(models.OutcomeFailure); got != models.OutcomeFailure {
		t.Fatalf("explicit failure should be preserved; got %q", got)
	}
	if got := defaultOutcome(models.OutcomeDenied); got != models.OutcomeDenied {
		t.Fatalf("explicit denied should be preserved; got %q", got)
	}
}

// TestEmit_InsertFailureWarnsWithActionResourceOutcome pins the best-effort
// contract's one guarantee: a failed insert is visible in a structured WARN
// that names the event — action, resource type/id, outcome — and never its
// metadata payload.
func TestEmit_InsertFailureWarnsWithActionResourceOutcome(t *testing.T) {
	client, err := mongo.NewClient(options.Client().
		ApplyURI("mongodb://127.0.0.1:1/").
		SetServerSelectionTimeout(50 * time.Millisecond).
		SetConnectTimeout(50 * time.Millisecond))
	if err != nil {
		t.Fatalf("mongo.NewClient: %v", err)
	}
	// Deliberately NOT connected: the insert fails server selection.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := NewSink(repository.New(client.Database("unreachable")), logger)

	sink.Emit(context.Background(), iface.AuditEvent{
		Action: "module.config.updated", ResourceType: "module", ResourceID: "auth",
		Outcome: "failure", TenantID: "t-1",
		Metadata: map[string]any{"keys": []string{"passwordLoginEnabledAdmin"}, "code": "auth.login_method_lockout"},
	})

	out := buf.String()
	for _, want := range []string{"level=WARN", "audit sink insert failed", "action=module.config.updated", "resourceType=module", "resourceId=auth", "outcome=failure"} {
		if !strings.Contains(out, want) {
			t.Errorf("WARN missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "passwordLoginEnabledAdmin") || strings.Contains(out, "login_method_lockout") {
		t.Errorf("WARN must not carry the metadata payload:\n%s", out)
	}
}
