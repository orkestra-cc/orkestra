package setup

import (
	"context"
	"errors"
	"testing"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// stubUsers satisfies iface.UserProvider by embedding a nil interface —
// any method other than the overridden GetUserCount panics. This keeps the
// fake scoped to exactly what the setup service actually calls.
type stubUsers struct {
	iface.UserProvider
	count    int64
	countErr error
}

func (s *stubUsers) GetUserCount(_ context.Context, _ *iface.UserFilters) (int64, error) {
	return s.count, s.countErr
}

// stubAdmin records the last RegisterInitialAdmin call and returns whatever
// response/error it was configured with.
type stubAdmin struct {
	resp        *authModels.TokenResponse
	err         error
	calledEmail string
	calledName  string
	calledIP    string
	callCount   int
}

func (s *stubAdmin) RegisterInitialAdmin(_ context.Context, email, _, fullName, ip string) (*authModels.TokenResponse, error) {
	s.callCount++
	s.calledEmail = email
	s.calledName = fullName
	s.calledIP = ip
	return s.resp, s.err
}

// stubSeeder records EnsureInternalTenant calls for the bootstrap tests.
type stubSeeder struct {
	calledOwner string
	calledName  string
	callCount   int
	uuid        string
	err         error
}

func (s *stubSeeder) EnsureInternalTenant(_ context.Context, ownerUUID, name string) (string, error) {
	s.callCount++
	s.calledOwner = ownerUUID
	s.calledName = name
	return s.uuid, s.err
}

func TestStatus_EmptyDB(t *testing.T) {
	svc := NewService(&stubUsers{count: 0}, &stubAdmin{}, nil, nil, nil)

	st := svc.Status(context.Background())
	if st.SetupCompleted {
		t.Errorf("empty DB: expected setupCompleted=false, got true")
	}
	if st.SMTPConfigured {
		t.Errorf("nil configService: expected smtpConfigured=false, got true")
	}
}

func TestStatus_WithUsers(t *testing.T) {
	svc := NewService(&stubUsers{count: 3}, &stubAdmin{}, nil, nil, nil)

	st := svc.Status(context.Background())
	if !st.SetupCompleted {
		t.Errorf("userCount=3: expected setupCompleted=true, got false")
	}
}

func TestStatus_DBError_FailsOpen(t *testing.T) {
	// A DB error must not lock the operator out of the wizard — the
	// response should report setupCompleted=false so the frontend still
	// offers a path forward.
	svc := NewService(&stubUsers{countErr: errors.New("mongo down")}, &stubAdmin{}, nil, nil, nil)

	st := svc.Status(context.Background())
	if st.SetupCompleted {
		t.Errorf("DB error: expected setupCompleted=false (fail-open), got true")
	}
}

func TestCreateInitialAdmin_EmptyDB_Succeeds(t *testing.T) {
	expected := &authModels.TokenResponse{
		AccessToken: "access-xyz",
		TokenType:   "Bearer",
		ExpiresIn:   900,
		SessionID:   "session-abc",
	}
	admin := &stubAdmin{resp: expected}
	svc := NewService(&stubUsers{count: 0}, admin, nil, nil, nil)

	tokens, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if tokens != expected {
		t.Errorf("expected the stub's token response to pass through unchanged")
	}
	if admin.callCount != 1 {
		t.Errorf("expected RegisterInitialAdmin to be called exactly once, got %d", admin.callCount)
	}
	if admin.calledEmail != "root@example.com" || admin.calledName != "Root Admin" || admin.calledIP != "10.0.0.1" {
		t.Errorf("arguments not forwarded: got email=%q name=%q ip=%q", admin.calledEmail, admin.calledName, admin.calledIP)
	}
}

func TestCreateInitialAdmin_BootstrapsInternalTenant(t *testing.T) {
	expected := &authModels.TokenResponse{
		AccessToken: "access-xyz",
		User:        &iface.UserManagementResponse{ID: "user-1", Email: "root@acme.com"},
	}
	admin := &stubAdmin{resp: expected}
	seeder := &stubSeeder{uuid: "tenant-1"}
	svc := NewService(&stubUsers{count: 0}, admin, seeder, nil, nil)

	if _, err := svc.CreateInitialAdmin(context.Background(), "root@acme.com", "verysecretpw!", "Root Admin", "10.0.0.1"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if seeder.callCount != 1 {
		t.Fatalf("expected EnsureInternalTenant called once, got %d", seeder.callCount)
	}
	if seeder.calledOwner != "user-1" {
		t.Errorf("owner: got %q, want user-1", seeder.calledOwner)
	}
	if seeder.calledName != "Acme" {
		t.Errorf("name: got %q, want Acme (derived from email domain)", seeder.calledName)
	}
}

func TestCreateInitialAdmin_BootstrapFailure_StillSucceeds(t *testing.T) {
	// The internal-tenant bootstrap is best-effort: a seeder error must not
	// fail admin creation.
	expected := &authModels.TokenResponse{
		AccessToken: "access-xyz",
		User:        &iface.UserManagementResponse{ID: "user-1", Email: "root@acme.com"},
	}
	admin := &stubAdmin{resp: expected}
	seeder := &stubSeeder{err: errors.New("mongo down")}
	svc := NewService(&stubUsers{count: 0}, admin, seeder, nil, nil)

	tokens, err := svc.CreateInitialAdmin(context.Background(), "root@acme.com", "verysecretpw!", "Root Admin", "10.0.0.1")
	if err != nil {
		t.Fatalf("bootstrap failure must not abort admin creation, got error: %v", err)
	}
	if tokens != expected {
		t.Errorf("expected token response to pass through unchanged")
	}
}

func TestInternalTenantNameFromEmail(t *testing.T) {
	cases := map[string]string{
		"root@acme.com":        "Acme",
		"admin@sub.example.io": "Sub",
		"no-domain":            "Internal",
		"trailing@":            "Internal",
		"x@acme":               "Acme",
	}
	for in, want := range cases {
		if got := internalTenantNameFromEmail(in); got != want {
			t.Errorf("internalTenantNameFromEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateInitialAdmin_NonEmptyDB_Refuses(t *testing.T) {
	admin := &stubAdmin{}
	svc := NewService(&stubUsers{count: 1}, admin, nil, nil, nil)

	_, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1")
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("expected ErrAlreadyCompleted, got: %v", err)
	}
	if admin.callCount != 0 {
		t.Errorf("expected RegisterInitialAdmin NOT to be called when setup is already complete (got %d calls)", admin.callCount)
	}
}

func TestCreateInitialAdmin_CountError_Refuses(t *testing.T) {
	// If we can't tell whether users exist, we must NOT create one —
	// blindly writing could duplicate a developer role on a populated DB.
	admin := &stubAdmin{}
	svc := NewService(&stubUsers{countErr: errors.New("mongo down")}, admin, nil, nil, nil)

	_, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1")
	if err == nil {
		t.Fatalf("expected error when GetUserCount fails, got nil")
	}
	if admin.callCount != 0 {
		t.Errorf("expected RegisterInitialAdmin NOT to be called on count error (got %d calls)", admin.callCount)
	}
}
