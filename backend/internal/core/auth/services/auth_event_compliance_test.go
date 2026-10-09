package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

func TestAuthEventComplianceAction(t *testing.T) {
	for _, tc := range []struct{ event, action string }{
		{"self_password_added", "auth.password.added"},
		{"admin_password_reset_sent", "auth.password.reset_requested"},
		{"self_mfa_enrolled", "auth.mfa.enrolled"},
		{"self_oauth_unlink", "auth.oauth.unlinked.self"},
		{"unknown_event", ""},
	} {
		t.Run(tc.event, func(t *testing.T) {
			if got := authEventComplianceAction(tc.event); got != tc.action {
				t.Fatalf("action = %q, want %q", got, tc.action)
			}
		})
	}
}

func TestAuthEventComplianceAction_InitialPasswordEmitsOnce(t *testing.T) {
	e := newInitialPasswordEnv(t)
	canonical := &authService{securityEventRepo: e.events}
	canonical.SetAuditSink(e.audit)
	e.svc.SetSecurityEventSink(canonical)
	ctx := ctxauth.WithClientIP(context.Background(), "192.0.2.45")
	password := "new-password-passphrase"
	if err := e.svc.SetInitialPassword(ctx, SetInitialPasswordInput{
		UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: password,
	}); err != nil {
		t.Fatal(err)
	}
	if len(e.events.rows) != 1 || len(e.audit.events) != 1 {
		t.Fatalf("security events = %d, compliance events = %d, want one each", len(e.events.rows), len(e.audit.events))
	}
	event := e.audit.events[0]
	if event.Action != "auth.password.added" || event.ActorUserID != e.user.UUID ||
		event.ResourceType != "user" || event.ResourceID != e.user.UUID ||
		event.ActorType != "user" || event.Outcome != "success" || event.IPAddress != "192.0.2.45" {
		t.Fatalf("unexpected audit identity: %+v", event)
	}
	if event.Metadata["audience"] != "operator" || event.Metadata["currentSessionId"] != "sess-caller" ||
		event.Metadata["otherSessionsRevoked"] != 2 || event.Metadata["teardownComplete"] != true {
		t.Fatalf("unexpected audit metadata: %v", event.Metadata)
	}
	wire, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{password, e.user.PasswordHash, e.user.Email} {
		if strings.Contains(string(wire), secret) {
			t.Fatal("compliance event leaked credentials or email")
		}
	}
}
