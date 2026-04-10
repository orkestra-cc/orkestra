package setup

import (
	"context"
	"errors"
	"testing"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/iface"
)

// stubUsers satisfies iface.UserProvider by embedding a nil interface —
// any method other than the overridden GetUserCount panics. This keeps the
// fake scoped to exactly what the setup service actually calls.
type stubUsers struct {
	iface.UserProvider
	count    int64
	countErr error
}

func (s *stubUsers) GetUserCount(_ context.Context, _ *userModels.UserFilters) (int64, error) {
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

func TestStatus_EmptyDB(t *testing.T) {
	svc := NewService(&stubUsers{count: 0}, &stubAdmin{}, nil, nil)

	st := svc.Status(context.Background())
	if st.SetupCompleted {
		t.Errorf("empty DB: expected setupCompleted=false, got true")
	}
	if st.SMTPConfigured {
		t.Errorf("nil configService: expected smtpConfigured=false, got true")
	}
}

func TestStatus_WithUsers(t *testing.T) {
	svc := NewService(&stubUsers{count: 3}, &stubAdmin{}, nil, nil)

	st := svc.Status(context.Background())
	if !st.SetupCompleted {
		t.Errorf("userCount=3: expected setupCompleted=true, got false")
	}
}

func TestStatus_DBError_FailsOpen(t *testing.T) {
	// A DB error must not lock the operator out of the wizard — the
	// response should report setupCompleted=false so the frontend still
	// offers a path forward.
	svc := NewService(&stubUsers{countErr: errors.New("mongo down")}, &stubAdmin{}, nil, nil)

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
	svc := NewService(&stubUsers{count: 0}, admin, nil, nil)

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

func TestCreateInitialAdmin_NonEmptyDB_Refuses(t *testing.T) {
	admin := &stubAdmin{}
	svc := NewService(&stubUsers{count: 1}, admin, nil, nil)

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
	svc := NewService(&stubUsers{countErr: errors.New("mongo down")}, admin, nil, nil)

	_, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1")
	if err == nil {
		t.Fatalf("expected error when GetUserCount fails, got nil")
	}
	if admin.callCount != 0 {
		t.Errorf("expected RegisterInitialAdmin NOT to be called on count error (got %d calls)", admin.callCount)
	}
}
