package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type initialPasswordSetterFunc func(context.Context, string, string) error

func (f initialPasswordSetterFunc) SetPasswordHashIfUnset(ctx context.Context, userUUID, hash string) error {
	return f(ctx, userUUID, hash)
}

// Use the existing rotation fake's CAS barrier; only the repository read is
// adapted, preserving the real race semantics and durable family fence.
type enrollmentRaceRepo struct{ *gateRefreshRepo }

func (r *enrollmentRaceRepo) tokensByUser(user string, activeOnly bool) ([]*authModels.RefreshTokenDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var rows []*authModels.RefreshTokenDoc
	for _, row := range r.byHash {
		if row.UserUUID == user && row.ExpiresAt.After(time.Now()) && (!activeOnly || !row.IsRevoked) {
			cp := *row
			rows = append(rows, &cp)
		}
	}
	return rows, nil
}

func (r *enrollmentRaceRepo) GetActiveTokensByUser(_ context.Context, user string) ([]*authModels.RefreshTokenDoc, error) {
	return r.tokensByUser(user, true)
}

func (r *enrollmentRaceRepo) GetUnexpiredTokensByUser(_ context.Context, user string) ([]*authModels.RefreshTokenDoc, error) {
	return r.tokensByUser(user, false)
}

func TestSetInitialPassword_FencesRotationInCASInsertGap(t *testing.T) {
	e := newInitialPasswordEnv(t)
	r := &enrollmentRaceRepo{&gateRefreshRepo{byHash: map[string]*authModels.RefreshTokenDoc{}, compromised: map[string]testFamilyRevocation{}}}
	e.svc.refreshTokenRepo = r
	ctx := context.Background()
	r.seedRefreshDoc(utils.HashRefreshToken("old"), &authModels.RefreshTokenDoc{UUID: "old", UserUUID: e.user.UUID, SessionUUID: "orphan-sid", FamilyID: "other-family", ExpiresAt: time.Now().Add(time.Hour)})
	reached, release := make(chan struct{}), make(chan struct{})
	r.setRotationBarrier(reached, release)
	done := make(chan error, 1)
	go func() {
		done <- r.RotateWithFamily(ctx, utils.HashRefreshToken("old"), &authModels.RefreshTokenDoc{UUID: "successor", Token: "next", UserUUID: e.user.UUID, SessionUUID: "orphan-sid", FamilyID: "other-family", ExpiresAt: time.Now().Add(time.Hour)})
	}()
	<-reached
	if err := e.svc.SetInitialPassword(ctx, SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
		close(release)
		<-done
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, repository.ErrTokenAlreadyRotated) {
		t.Fatalf("successor escaped enrollment fence: rotation error=%v", err)
	}
	row, err := r.GetByTokenAny(ctx, utils.HashRefreshToken("next"))
	if err != nil || row == nil || !row.IsRevoked || row.RevokedReason != "password_added" {
		t.Fatalf("late successor must be unusable: row=%+v error=%v", row, err)
	}
	if !contains(e.revoker.revokedList(), "orphan-sid") {
		t.Fatal("rotating orphan SID must also be denylisted")
	}
}

func TestSetInitialPassword_LiveMongoFencesRotationInCASInsertGap(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI for live enrollment race regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	db := client.Database("auth_enrollment_race_" + uuid.NewString())
	defer db.Drop(context.Background())
	repo := repository.NewOperatorRefreshTokenRepository(db)
	e := newInitialPasswordEnv(t)
	e.svc.refreshTokenRepo = repo
	old := &authModels.RefreshTokenDoc{UUID: "old", Token: "mongo-old", UserUUID: e.user.UUID, SessionUUID: "orphan-sid", FamilyID: "other-family", DeviceID: "other-device", DeviceType: "desktop", Platform: "web", Fingerprint: "test-fingerprint", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.CreateRefreshToken(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateRefreshToken(ctx, &authModels.RefreshTokenDoc{UUID: "caller", Token: "mongo-caller", UserUUID: e.user.UUID, SessionUUID: "sess-caller", FamilyID: "caller-family", DeviceID: "caller-device", DeviceType: "desktop", Platform: "web", Fingerprint: "test-fingerprint", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	reached, release := make(chan struct{}), make(chan struct{})
	monitor := &event.CommandMonitor{Succeeded: func(commandCtx context.Context, command *event.CommandSucceededEvent) {
		if command.CommandName == "update" {
			close(reached)
			select {
			case <-release:
			case <-commandCtx.Done():
			}
		}
	}}
	rotatingClient, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetMonitor(monitor))
	if err != nil {
		t.Fatal(err)
	}
	defer rotatingClient.Disconnect(context.Background())
	rotatingRepo := repository.NewOperatorRefreshTokenRepository(rotatingClient.Database(db.Name()))
	done := make(chan error, 1)
	go func() {
		done <- rotatingRepo.RotateWithFamily(ctx, utils.HashRefreshToken("mongo-old"), &authModels.RefreshTokenDoc{UUID: "next", Token: "mongo-next", UserUUID: e.user.UUID, SessionUUID: "orphan-sid", FamilyID: "other-family", ExpiresAt: time.Now().Add(time.Hour)})
	}()
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("rotation did not reach CAS barrier")
	}
	enrollErr := e.svc.SetInitialPassword(ctx, SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"})
	close(release)
	rotateErr := <-done
	if enrollErr != nil {
		t.Fatal(enrollErr)
	}
	if !errors.Is(rotateErr, repository.ErrTokenAlreadyRotated) {
		t.Fatalf("late successor escaped: %v", rotateErr)
	}
	row, err := repo.GetByTokenAny(ctx, utils.HashRefreshToken("mongo-next"))
	if err != nil || row == nil || !row.IsRevoked || row.RevokedReason != "password_added" {
		t.Fatalf("successor=%+v error=%v", row, err)
	}
	caller, err := repo.GetByToken(ctx, utils.HashRefreshToken("mongo-caller"))
	if err != nil || caller == nil {
		t.Fatalf("caller lost refresh credential: %v", err)
	}
	if !contains(e.revoker.revokedList(), "orphan-sid") || e.events.rows[0].Metadata["teardownComplete"] != true {
		t.Fatal("complete enrollment must denylist rotating orphan SID")
	}
}

func TestSetInitialPassword_PreservesCurrentFamilyAndDeduplicatesHistory(t *testing.T) {
	e := newInitialPasswordEnv(t)
	for _, row := range []struct {
		sid, family string
		revoked     bool
	}{
		{"sess-caller", "caller-family", true},
		{"sess-caller", "caller-family", false},
		{"orphan-sid", "other-family", true},
		{"orphan-sid", "other-family", false},
		{"legacy-sid", "", true},
	} {
		e.refresh.tokens = append(e.refresh.tokens, &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: row.sid, FamilyID: row.family, IsRevoked: row.revoked, ExpiresAt: time.Now().Add(time.Hour)})
	}
	if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.refresh.fencedFamilies, []string{"other-family"}) {
		t.Fatalf("only other family must be fenced once: %v", e.refresh.fencedFamilies)
	}
	if e.refresh.tokens[1].IsRevoked || !e.refresh.tokens[3].IsRevoked {
		t.Fatal("caller family must remain usable and other family must be revoked")
	}
	for _, sid := range []string{"orphan-sid", "legacy-sid"} {
		count := 0
		for _, revoked := range e.revoker.revokedList() {
			if revoked == sid {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("SID %s denylisted %d times", sid, count)
		}
	}
}

func TestSetInitialPassword_FamilyFenceFailureDoesNotStopTeardown(t *testing.T) {
	e := newInitialPasswordEnv(t)
	e.refresh.familyErrors = map[string]error{"failed-family": errors.New("family fence unavailable")}
	for _, family := range []string{"failed-family", "later-family"} {
		e.refresh.tokens = append(e.refresh.tokens, &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: family + "-sid", FamilyID: family, IsRevoked: true, ExpiresAt: time.Now().Add(time.Hour)})
	}
	if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
		t.Fatal(err)
	}
	if e.user.PasswordHash == "" || e.events.rows[0].Metadata["teardownComplete"] != false {
		t.Fatal("successful password creation must report incomplete teardown")
	}
	if !reflect.DeepEqual(e.refresh.fencedFamilies, []string{"failed-family", "later-family"}) || !contains(e.revoker.revokedList(), "failed-family-sid") || !contains(e.revoker.revokedList(), "later-family-sid") || len(e.trust.revokedList()) != 1 {
		t.Fatal("failed family fence must not stop later fences, SID revocation, or trust teardown")
	}
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

func TestSetInitialPassword_RevokesOrphanRefreshCredentialsAndPreservesCaller(t *testing.T) {
	e := newInitialPasswordEnv(t)
	current := &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: "sess-caller", ExpiresAt: time.Now().Add(time.Hour)}
	orphan := &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: "orphan-sid", ExpiresAt: time.Now().Add(time.Hour)}
	duplicate := *orphan
	foreign := &authModels.RefreshTokenDoc{UserUUID: "other-user", SessionUUID: "foreign-sid", ExpiresAt: time.Now().Add(time.Hour)}
	e.refresh.tokens = []*authModels.RefreshTokenDoc{current, orphan, &duplicate, foreign}
	if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
		t.Fatal(err)
	}
	if !orphan.IsRevoked || !duplicate.IsRevoked || orphan.RevokedReason != "password_added" {
		t.Fatal("active orphan refresh credentials survived initial password enrollment")
	}
	if current.IsRevoked || foreign.IsRevoked || contains(e.revoker.revokedList(), "sess-caller") || contains(e.revoker.revokedList(), "foreign-sid") {
		t.Fatal("caller and other user's credentials must survive")
	}
	_, bySession := e.refresh.snapshot()
	for _, calls := range [][]string{bySession, e.revoker.revokedList()} {
		count := 0
		for _, sid := range calls {
			if sid == "orphan-sid" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("orphan SID must be revoked once, calls=%v", calls)
		}
	}
	md := e.events.rows[0].Metadata
	if md["otherSessionsRevoked"] != 2 || md["teardownComplete"] != true {
		t.Fatalf("orphan refresh-only SID must not inflate session count: %v", md)
	}
}

func TestSetInitialPassword_OrphanRefreshTeardownFailureIsIncomplete(t *testing.T) {
	for _, stage := range []string{"list", "refresh", "sid"} {
		t.Run(stage, func(t *testing.T) {
			e := newInitialPasswordEnv(t)
			first := &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: "orphan-one", ExpiresAt: time.Now().Add(time.Hour)}
			second := &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: "orphan-two", ExpiresAt: time.Now().Add(time.Hour)}
			e.refresh.tokens = []*authModels.RefreshTokenDoc{first, second}
			outage := errors.New("injected orphan teardown failure")
			switch stage {
			case "list":
				e.refresh.listErr = outage
			case "refresh":
				e.refresh.sessionErrors = map[string]error{"orphan-one": outage}
			case "sid":
				e.revoker.sidErrors = map[string]error{"orphan-one": outage}
			}
			if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
				t.Fatal("persisted password must still report success", err)
			}
			md := e.events.rows[0].Metadata
			if md["teardownComplete"] != false || md["otherSessionsRevoked"] != 2 {
				t.Fatalf("failure must preserve conservative count and status: %v", md)
			}
			if stage != "list" && (!second.IsRevoked || !contains(e.revoker.revokedList(), "orphan-one") || !contains(e.revoker.revokedList(), "orphan-two")) {
				t.Fatal("failure must not stop SID denial or later orphan refresh teardown")
			}
		})
	}
}

func TestSetInitialPassword_DoesNotRetryAlreadyProcessedSessionSIDs(t *testing.T) {
	e := newInitialPasswordEnv(t)
	e.refresh.tokens = []*authModels.RefreshTokenDoc{{UserUUID: e.user.UUID, SessionUUID: "sess-other", ExpiresAt: time.Now().Add(time.Hour)}}
	e.refresh.sessionErrors = map[string]error{"sess-other": errors.New("refresh outage")}
	if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, CurrentSID: "sess-caller", New: "new-password-passphrase"}); err != nil {
		t.Fatal(err)
	}
	_, bySession := e.refresh.snapshot()
	if len(bySession) != 2 || len(e.revoker.revokedList()) != 2 {
		t.Fatal("already processed session SIDs must not be revoked again")
	}
	md := e.events.rows[0].Metadata
	if md["otherSessionsRevoked"] != 1 || md["teardownComplete"] != false {
		t.Fatalf("failed session teardown must retain its conservative result: %v", md)
	}
}

func TestSetInitialPassword_EmptyCurrentSIDAlsoDeniesOrphanSID(t *testing.T) {
	e := newInitialPasswordEnv(t)
	orphan := &authModels.RefreshTokenDoc{UserUUID: e.user.UUID, SessionUUID: "orphan-sid", ExpiresAt: time.Now().Add(time.Hour)}
	e.refresh.tokens = []*authModels.RefreshTokenDoc{orphan}
	if err := e.svc.SetInitialPassword(context.Background(), SetInitialPasswordInput{UserUUID: e.user.UUID, New: "new-password-passphrase"}); err != nil {
		t.Fatal(err)
	}
	if !orphan.IsRevoked || !contains(e.revoker.revokedList(), "orphan-sid") {
		t.Fatal("empty caller SID must revoke orphan refresh credentials and deny its access-token SID")
	}
	md := e.events.rows[0].Metadata
	if md["otherSessionsRevoked"] != 3 || md["teardownComplete"] != true {
		t.Fatalf("all real sessions must count while orphan SID does not: %v", md)
	}
}
