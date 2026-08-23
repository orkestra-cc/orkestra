package setup

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/systeminit"
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

// fakeFinalizationStore is an in-memory systeminit.FinalizationStore for
// exercising Service.Status without a live Mongo connection. Every mutator
// increments mutatorCalls so a test can assert Status performs no writes —
// only Get is a read.
type fakeFinalizationStore struct {
	rec          *systeminit.FinalizationRecord
	getErr       error
	mutatorCalls int
}

func (f *fakeFinalizationStore) Get(_ context.Context) (*systeminit.FinalizationRecord, error) {
	return f.rec, f.getErr
}

func (f *fakeFinalizationStore) InitializeFresh(_ context.Context, _ string) error {
	f.mutatorCalls++
	return nil
}

func (f *fakeFinalizationStore) EnsureRecord(_ context.Context, _ string, _ *systeminit.FinalizationResult) (*systeminit.FinalizationRecord, error) {
	f.mutatorCalls++
	return nil, nil
}

func (f *fakeFinalizationStore) ReserveRequest(_ context.Context, _, _, _, _, _, _ string) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) ClaimStage(_ context.Context, _ string, _ int, _ int64, _ string, _ time.Time) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) RenewLease(_ context.Context, _ string, _ time.Time) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) AdvanceStage(_ context.Context, _ string, _ int, _ int64) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) Complete(_ context.Context, _ string, _ int64, _ systeminit.FinalizationResult) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) ClaimRecovery(_ context.Context, _ string, _ int64, _ string) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) ClaimReconcileLease(_ context.Context, _ int, _ string, _ time.Time) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

func (f *fakeFinalizationStore) FinishReconcile(_ context.Context, _ int, _ string) (bool, error) {
	f.mutatorCalls++
	return false, nil
}

// --- Status: three persistent phases, fail-closed ---

func TestStatus_NoUsers_AdminRequired(t *testing.T) {
	store := &fakeFinalizationStore{}
	svc := NewService(&stubUsers{count: 0}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Phase != PhaseAdminRequired {
		t.Errorf("phase = %q, want %q", st.Phase, PhaseAdminRequired)
	}
	if st.SetupCompleted {
		t.Errorf("no users: expected setupCompleted=false, got true")
	}
	if st.SMTPConfigured {
		t.Errorf("nil configService: expected smtpConfigured=false, got true")
	}
}

func TestStatus_UsersExist_NoRecord_TenantRequired(t *testing.T) {
	store := &fakeFinalizationStore{rec: nil}
	svc := NewService(&stubUsers{count: 1}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Phase != PhaseTenantRequired {
		t.Errorf("phase = %q, want %q", st.Phase, PhaseTenantRequired)
	}
	if st.SetupCompleted {
		t.Errorf("phase=tenant_required: expected setupCompleted=false, got true")
	}
}

func TestStatus_RecordCompleted_Complete(t *testing.T) {
	completedAt := time.Now().UTC()
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{CompletedAt: &completedAt}}
	svc := NewService(&stubUsers{count: 3}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Phase != PhaseComplete {
		t.Errorf("phase = %q, want %q", st.Phase, PhaseComplete)
	}
	if !st.SetupCompleted {
		t.Errorf("phase=complete: expected setupCompleted=true, got false")
	}
}

func TestStatus_RecordPresentIncomplete_TenantRequired(t *testing.T) {
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{Stage: systeminit.StageTenant}}
	svc := NewService(&stubUsers{count: 2}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Phase != PhaseTenantRequired {
		t.Errorf("phase = %q, want %q", st.Phase, PhaseTenantRequired)
	}
	if st.SetupCompleted {
		t.Errorf("record present but incomplete: expected setupCompleted=false, got true")
	}
}

func TestStatus_UserCountError_FailsClosed(t *testing.T) {
	store := &fakeFinalizationStore{}
	svc := NewService(&stubUsers{countErr: errors.New("mongo down")}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err == nil {
		t.Fatalf("expected error when GetUserCount fails, got nil")
	}
	if st != (Status{}) {
		t.Errorf("expected zero-value Status on error, got %+v", st)
	}
	if store.mutatorCalls != 0 {
		t.Errorf("expected no store mutator calls, got %d", store.mutatorCalls)
	}
}

func TestStatus_StoreGetError_FailsClosed(t *testing.T) {
	store := &fakeFinalizationStore{getErr: errors.New("mongo down")}
	svc := NewService(&stubUsers{count: 1}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err == nil {
		t.Fatalf("expected error when store.Get fails, got nil")
	}
	if st != (Status{}) {
		t.Errorf("expected zero-value Status on error, got %+v", st)
	}
}

func TestStatus_SMTPReadFailure_DegradesOnlySMTPFlag(t *testing.T) {
	// A nil configService is the existing "can't read SMTP config" path —
	// isSMTPConfigured's early nil check returns false without touching
	// phase computation at all. This is the deliberate asymmetry: SMTP is
	// non-authoritative and may degrade; the phase must not.
	completedAt := time.Now().UTC()
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{CompletedAt: &completedAt}}
	svc := NewService(&stubUsers{count: 1}, &stubAdmin{}, store, nil, nil)

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.SMTPConfigured {
		t.Errorf("expected smtpConfigured=false on SMTP read failure, got true")
	}
	if st.Phase != PhaseComplete {
		t.Errorf("SMTP failure must not affect phase: got %q, want %q", st.Phase, PhaseComplete)
	}
}

func TestStatus_NoWrites(t *testing.T) {
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{}}
	svc := NewService(&stubUsers{count: 5}, &stubAdmin{}, store, nil, nil)

	if _, err := svc.Status(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.mutatorCalls != 0 {
		t.Errorf("Status must perform no writes; recorded %d mutator calls", store.mutatorCalls)
	}
}

// --- CreateInitialAdmin: unchanged behavior, updated NewService call sites ---

func TestCreateInitialAdmin_EmptyDB_Succeeds(t *testing.T) {
	expected := &authModels.TokenResponse{
		AccessToken: "access-xyz",
		TokenType:   "Bearer",
		ExpiresIn:   900,
		SessionID:   "session-abc",
	}
	admin := &stubAdmin{resp: expected}
	svc := NewService(&stubUsers{count: 0}, admin, &fakeFinalizationStore{}, nil, nil)

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
	svc := NewService(&stubUsers{count: 1}, admin, &fakeFinalizationStore{}, nil, nil)

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
	svc := NewService(&stubUsers{countErr: errors.New("mongo down")}, admin, &fakeFinalizationStore{}, nil, nil)

	_, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1")
	if err == nil {
		t.Fatalf("expected error when GetUserCount fails, got nil")
	}
	if admin.callCount != 0 {
		t.Errorf("expected RegisterInitialAdmin NOT to be called on count error (got %d calls)", admin.callCount)
	}
}
