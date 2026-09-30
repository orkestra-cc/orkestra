package services

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type initialPasswordSetterFunc func(context.Context, string, string) error

func (f initialPasswordSetterFunc) SetPasswordHashIfUnset(ctx context.Context, userUUID, hash string) error {
	return f(ctx, userUUID, hash)
}

type initialPasswordEnv struct {
	*credentialRevocationEnv
	user     *iface.User
	reader   *stubReader
	events   *fakeSecurityEventRepo
	audit    *gateAuditSink
	attempts AttemptCounter
}

func newInitialPasswordEnv(t *testing.T) *initialPasswordEnv {
	t.Helper()
	e := &initialPasswordEnv{
		credentialRevocationEnv: newCredentialRevocationEnv(t),
		user:                    &iface.User{UUID: "oauth-user", Email: "oauth@example.com", IsActive: true, EmailVerified: true},
		reader: &stubReader{values: map[string]string{
			"passwordLoginEnabledAdmin": "true", "passwordLoginEnabledClient": "false",
			"breachedPasswordCheck": "false", "revokeSessionsOnPasswordChange": "false",
		}},
		events:   &fakeSecurityEventRepo{},
		audit:    &gateAuditSink{},
		attempts: NewMemoryAttemptCounter(),
	}
	e.users.seed(e.user)
	for _, sid := range []string{"sess-caller", "sess-other", "sess-third"} {
		e.sessions.seed(&authModels.AuthSessionDoc{UUID: sid, UserUUID: e.user.UUID, IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})
	}
	policy := &AuthPolicyService{cs: e.reader}
	e.pwd.SetPolicy(policy)
	e.svc = NewPasswordAuthService(PasswordAuthConfig{
		UserService: e.users, InitialPasswordSetter: e.users, PasswordService: e.pwd,
		Policy: policy, Audience: PolicyAudienceOperator, AttemptCounter: e.attempts,
		RefreshTokenRepo: e.refresh, AuthSessionRepo: e.sessions, DeviceTrust: e.trust,
		Logger: silentLogger(),
	})
	e.svc.SetSessionRevocation(e.revoker)
	e.svc.SetSecurityEventSink(&authService{securityEventRepo: e.events})
	e.svc.SetAuditSink(e.audit)
	return e
}

func TestSetInitialPassword_RefusesWithoutMutation(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	cases := []struct {
		name  string
		setup func(*initialPasswordEnv)
		want  error
	}{
		{"disabled policy", func(e *initialPasswordEnv) {
			e.reader.values["passwordLoginEnabledAdmin"] = "false"
			e.svc.policy.SetOperatorBreakGlass(true)
			e.users.setGetByIDErr(storageErr)
		}, ErrPasswordLoginDisabled},
		{"unreadable policy", func(e *initialPasswordEnv) { e.reader.rawErr = storageErr }, ErrAuthPolicyUnavailable},
		{"missing policy", func(e *initialPasswordEnv) { e.svc.policy = nil }, ErrAuthPolicyUnavailable},
		{"missing policy document", func(e *initialPasswordEnv) { e.reader.requiredMissing = true }, ErrAuthPolicyUnavailable},
		{"malformed policy", func(e *initialPasswordEnv) { e.reader.values["passwordLoginEnabledAdmin"] = "treu" }, ErrAuthPolicyUnavailable},
		{"configured audience", func(e *initialPasswordEnv) { e.svc.audience = PolicyAudienceClient }, ErrPasswordLoginDisabled},
		{"unknown audience", func(e *initialPasswordEnv) { e.svc.audience = PolicyAudience("service") }, ErrAuthPolicyUnavailable},
		{"inactive", func(e *initialPasswordEnv) { e.user.IsActive = false }, ErrUserInactive},
		{"service principal", func(e *initialPasswordEnv) { e.user.Kind = iface.UserKindService }, ErrUserInactive},
		{"unverified local email", func(e *initialPasswordEnv) { e.user.EmailVerified = false }, ErrEmailNotVerified},
		{"existing password", func(e *initialPasswordEnv) { e.user.PasswordHash = "existing-hash" }, iface.ErrPasswordAlreadySet},
		{"setter missing", func(e *initialPasswordEnv) { e.svc.initialPasswordSetter = nil; e.svc.policy = nil }, ErrInitialPasswordUnavailable},
		{"live password policy", func(e *initialPasswordEnv) { e.reader.values["passwordRequireUpper"] = "true" }, ErrPasswordMissingUpper},
		{"setter race", func(e *initialPasswordEnv) {
			e.svc.initialPasswordSetter = initialPasswordSetterFunc(func(context.Context, string, string) error { return iface.ErrPasswordAlreadySet })
		}, iface.ErrPasswordAlreadySet},
		{"setter outage", func(e *initialPasswordEnv) {
			e.svc.initialPasswordSetter = initialPasswordSetterFunc(func(context.Context, string, string) error { return storageErr })
		}, storageErr},
		{"user read outage", func(e *initialPasswordEnv) { e.users.setGetByIDErr(storageErr) }, storageErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newInitialPasswordEnv(t)
			tc.setup(e)
			before := e.user.PasswordHash
			err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if e.user.PasswordHash != before || e.user.PasswordUpdatedAt != nil {
				t.Fatal("refusal must not write a credential")
			}
			byUser, bySession := e.refresh.snapshot()
			if len(byUser)+len(bySession)+len(e.sessions.terminated)+len(e.revoker.revokedList())+len(e.trust.revokedList()) != 0 {
				t.Fatal("refusal must not revoke credentials")
			}
			if len(e.events.rows) != 0 || len(e.audit.events) != 0 || e.users.failedLoginsCleared(e.user.Email) {
				t.Fatal("refusal must not announce success or clear lockout state")
			}
		})
	}
}

func TestSetInitialPassword_PersistsAndRevokesOtherCredentials(t *testing.T) {
	e := newInitialPasswordEnv(t)
	ctx := ctxauth.WithClientIP(context.Background(), "192.0.2.45")
	password := "new-password-passphrase"
	e.user.FailedLoginCount = 4
	lockedUntil := time.Now().Add(time.Minute)
	e.user.LockedUntil = &lockedUntil
	key := AttemptKeyEmail(PolicyAudienceOperator, e.user.Email)
	if _, err := e.attempts.RecordFailure(ctx, key, Limit{Threshold: 1, Window: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetInitialPassword(ctx, SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: password}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e.user.PasswordHash, "$argon2id$") {
		t.Fatal("stored credential must be Argon2id")
	}
	if ok, err := e.pwd.Verify(password, e.user.PasswordHash); err != nil || !ok {
		t.Fatalf("hash verification: ok=%v error=%v", ok, err)
	}
	if e.user.PasswordUpdatedAt == nil {
		t.Fatal("credential timestamp must be persisted")
	}
	caller, _ := e.sessions.GetByUUID(ctx, "sess-caller")
	if !caller.IsActive || contains(e.revoker.revokedList(), caller.UUID) {
		t.Fatal("caller session must survive")
	}
	byUser, bySession := e.refresh.snapshot()
	if len(byUser) != 0 || len(bySession) != 2 || contains(bySession, "sess-caller") {
		t.Fatalf("refresh revocations byUser=%v bySession=%v", byUser, bySession)
	}
	for _, sid := range []string{"sess-other", "sess-third"} {
		doc, _ := e.sessions.GetByUUID(ctx, sid)
		if doc.IsActive || !contains(bySession, sid) || !contains(e.revoker.revokedList(), sid) {
			t.Fatalf("session %s pathway survived", sid)
		}
	}
	if !reflect.DeepEqual(e.trust.revokedList(), []string{e.user.UUID}) || !reflect.DeepEqual(e.trust.reasons, []string{"password_added"}) {
		t.Fatal("all device trust must be revoked with password_added")
	}
	for _, reason := range e.refresh.reasons {
		if reason != "password_added" {
			t.Fatalf("refresh reason=%s", reason)
		}
	}
	if !reflect.DeepEqual(e.revoker.reasons, []string{"password_added", "password_added"}) {
		t.Fatal("SID revocation must carry password_added")
	}
	if e.user.FailedLoginCount != 0 || e.user.LockedUntil != nil || !e.users.failedLoginsCleared(e.user.Email) {
		t.Fatal("durable lockout state must clear")
	}
	if v, err := e.attempts.Locked(ctx, key, Limit{Threshold: 1, Window: time.Minute}); err != nil || v.Locked {
		t.Fatal("email attempt scope must clear")
	}
	if len(e.events.rows) != 1 {
		t.Fatalf("got %d security events, want one", len(e.events.rows))
	}
	event := e.events.rows[0]
	if event.EventType != "self_password_added" || event.UserUUID != e.user.UUID || event.IPAddress != "192.0.2.45" {
		t.Fatalf("wrong event identity/IP: %+v", event)
	}
	wantMetadata := map[string]interface{}{"audience": "operator", "currentSessionId": "sess-caller", "otherSessionsRevoked": 2, "teardownComplete": true}
	if !reflect.DeepEqual(event.Metadata, wantMetadata) {
		t.Fatalf("metadata=%v, want %v", event.Metadata, wantMetadata)
	}
	wire, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{password, e.user.PasswordHash, e.user.Email} {
		if strings.Contains(string(wire), secret) {
			t.Fatal("event leaked credentials or email")
		}
	}
	if len(e.audit.events) != 0 {
		t.Fatal("password service must not directly emit the compliance event")
	}
}

func TestSetInitialPassword_TeardownFailureStillSucceeds(t *testing.T) {
	e := newInitialPasswordEnv(t)
	e.refresh.bySessionErr = errors.New("refresh store unavailable")
	if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
		t.Fatalf("persistence succeeded, got %v", err)
	}
	if e.user.PasswordHash == "" || len(e.events.rows) != 1 {
		t.Fatal("successful write must still be announced")
	}
	md := e.events.rows[0].Metadata
	if md["teardownComplete"] != false || md["otherSessionsRevoked"] != 0 {
		t.Fatalf("failure must be truthful: %v", md)
	}
	if len(e.sessions.terminated) != 2 || len(e.revoker.revokedList()) != 2 || len(e.trust.revokedList()) != 1 {
		t.Fatal("all later teardown stages must run")
	}
}

func TestSetInitialPassword_MissingTeardownDependencyIsIncomplete(t *testing.T) {
	for _, missing := range []string{"sessions", "refresh", "sid", "trust"} {
		t.Run(missing, func(t *testing.T) {
			e := newInitialPasswordEnv(t)
			switch missing {
			case "sessions":
				e.svc.authSessionRepo = nil
			case "refresh":
				e.svc.refreshTokenRepo = nil
			case "sid":
				e.svc.sessionRevocation = nil
			case "trust":
				e.svc.deviceTrust = nil
			}
			if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
				t.Fatal(err)
			}
			if len(e.events.rows) != 1 || e.events.rows[0].Metadata["teardownComplete"] != false {
				t.Fatal("unavailable required pathway must report incomplete")
			}
		})
	}
}
