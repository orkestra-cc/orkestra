package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// stubNotifier satisfies iface.NotificationSender. Records the last
// SendTemplated request for assertions; Send is panics since the
// suspicious-login path is templated-only.
type stubNotifier struct {
	configured bool
	sends      []iface.TemplatedNotificationRequest
	sendErr    error
}

func (s *stubNotifier) IsConfigured(_ context.Context) bool { return s.configured }
func (s *stubNotifier) Send(context.Context, iface.NotificationRequest) (*iface.NotificationResult, error) {
	panic("Send should not be used by suspicious_login_notifier")
}
func (s *stubNotifier) SendTemplated(_ context.Context, req iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	if s.sendErr != nil {
		return nil, s.sendErr
	}
	s.sends = append(s.sends, req)
	return &iface.NotificationResult{Status: "sent"}, nil
}

// stubEvents satisfies SecurityEventService. Records events; other
// methods panic so accidental use surfaces loudly.
type stubEvents struct {
	recorded []*models.SecurityEvent
	recErr   error
}

func (s *stubEvents) RecordEvent(_ context.Context, ev *models.SecurityEvent) error {
	if s.recErr != nil {
		return s.recErr
	}
	s.recorded = append(s.recorded, ev)
	return nil
}
func (s *stubEvents) GetUserEvents(context.Context, string, int) ([]*models.SecurityEvent, error) {
	panic("unexpected GetUserEvents")
}
func (s *stubEvents) GetEventsByType(context.Context, string, int) ([]*models.SecurityEvent, error) {
	panic("unexpected GetEventsByType")
}
func (s *stubEvents) GetSuspiciousEvents(context.Context, int) ([]*models.SecurityEvent, error) {
	panic("unexpected GetSuspiciousEvents")
}
func (s *stubEvents) MarkEventResolved(context.Context, primitive.ObjectID, string) error {
	panic("unexpected MarkEventResolved")
}
func (s *stubEvents) AnalyzeSecurityTrends(context.Context, string, int) (*SecurityTrendAnalysis, error) {
	panic("unexpected AnalyzeSecurityTrends")
}
func (s *stubEvents) GetFailedLoginAttempts(context.Context, string, int) (int, error) {
	panic("unexpected GetFailedLoginAttempts")
}
func (s *stubEvents) GetSuspiciousActivityScore(context.Context, string) (float64, error) {
	panic("unexpected GetSuspiciousActivityScore")
}
func (s *stubEvents) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("unexpected DeleteAllByUser")
}

// compile-time guard that the stub really satisfies the interface.
var _ SecurityEventService = (*stubEvents)(nil)

func newTestNotifier(t *testing.T, events SecurityEventService, notifier iface.NotificationSender) *suspiciousLoginNotifier {
	t.Helper()
	svc := NewSuspiciousLoginNotifier(NotifierConfig{
		Events:       events,
		Notifier:     notifier,
		AppName:      "Orkestra",
		SupportEmail: "support@example.com",
		FrontendURL:  "https://app.example.com",
	}).(*suspiciousLoginNotifier)
	svc.clock = func() time.Time { return time.Date(2026, 4, 25, 15, 30, 0, 0, time.UTC) }
	return svc
}

func sampleInput() SuspiciousLoginInput {
	return SuspiciousLoginInput{
		User:    &AuthUser{UUID: "u1", Email: "user@example.com", Name: "Alice"},
		Session: &SessionSnapshot{UUID: "s1", CreatedAt: time.Date(2026, 4, 25, 15, 30, 0, 0, time.UTC)},
		Assessment: &models.RiskAssessment{
			Score:   0.6,
			Level:   "high",
			Factors: []models.RiskFactor{{Details: map[string]interface{}{"factor": "new_ip"}}, {Details: map[string]interface{}{"factor": "rapid_ip_change"}}},
		},
		IPAddress:  "203.0.113.5",
		Platform:   "web",
		DeviceName: "Chrome on macOS",
		UserAgent:  "Mozilla/5.0",
	}
}

func TestSuspiciousLogin_LowScoreRecordsLoginEventWithoutEmail(t *testing.T) {
	events := &stubEvents{}
	notifier := &stubNotifier{configured: true}
	svc := newTestNotifier(t, events, notifier)
	in := sampleInput()
	in.Assessment.Score = 0.1
	in.Assessment.Level = "low"

	svc.OnLogin(context.Background(), in)

	if len(events.recorded) != 1 {
		t.Fatalf("expected 1 event recorded, got %d", len(events.recorded))
	}
	if events.recorded[0].EventType != "login" {
		t.Errorf("low-score eventType = %q, want login", events.recorded[0].EventType)
	}
	if events.recorded[0].Severity != "info" {
		t.Errorf("low-score severity = %q, want info", events.recorded[0].Severity)
	}
	if len(notifier.sends) != 0 {
		t.Errorf("low-score must not email, got %d sends", len(notifier.sends))
	}
}

func TestSuspiciousLogin_HighScoreRecordsAndEmails(t *testing.T) {
	events := &stubEvents{}
	notifier := &stubNotifier{configured: true}
	svc := newTestNotifier(t, events, notifier)

	svc.OnLogin(context.Background(), sampleInput())

	if len(events.recorded) != 1 {
		t.Fatalf("expected 1 event recorded, got %d", len(events.recorded))
	}
	ev := events.recorded[0]
	if ev.EventType != "suspicious_login" {
		t.Errorf("high-score eventType = %q, want suspicious_login", ev.EventType)
	}
	if ev.Severity != "warning" {
		t.Errorf("high-score severity = %q, want warning", ev.Severity)
	}
	if ev.SessionID != "s1" || ev.UserUUID != "u1" || ev.IPAddress != "203.0.113.5" {
		t.Errorf("event missing identity fields: %+v", ev)
	}
	if len(notifier.sends) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(notifier.sends))
	}
	req := notifier.sends[0]
	if req.TemplateID != "auth.suspicious_login" {
		t.Errorf("template = %q, want auth.suspicious_login", req.TemplateID)
	}
	if req.Type != "transactional" {
		t.Errorf("type = %q, want transactional (preferences must not suppress security mail)", req.Type)
	}
	if req.IdempotencyKey != "suspicious:u1:s1" {
		t.Errorf("idempotency key = %q, want suspicious:u1:s1", req.IdempotencyKey)
	}
	if len(req.Recipients) != 1 || req.Recipients[0].Address != "user@example.com" {
		t.Errorf("recipient = %+v", req.Recipients)
	}
	if req.Data["RiskLevel"] == nil || req.Data["RiskFactors"] == nil {
		t.Errorf("data missing risk fields: %+v", req.Data)
	}
	if got, _ := req.Data["LoginIP"].(string); got != "203.0.113.5" {
		t.Errorf("LoginIP = %q, want 203.0.113.5", got)
	}
	if got, _ := req.Data["AccountActivityURL"].(string); got != "https://app.example.com/account/security" {
		t.Errorf("AccountActivityURL = %q", got)
	}
}

func TestSuspiciousLogin_NotifierNotConfiguredSkipsEmail(t *testing.T) {
	events := &stubEvents{}
	notifier := &stubNotifier{configured: false}
	svc := newTestNotifier(t, events, notifier)

	svc.OnLogin(context.Background(), sampleInput())

	if len(events.recorded) != 1 {
		t.Errorf("event must still record when notifier is unconfigured, got %d", len(events.recorded))
	}
	if len(notifier.sends) != 0 {
		t.Errorf("IsConfigured=false must skip email, got %d sends", len(notifier.sends))
	}
}

func TestSuspiciousLogin_NilNotifierSkipsEmail(t *testing.T) {
	events := &stubEvents{}
	svc := newTestNotifier(t, events, nil)

	svc.OnLogin(context.Background(), sampleInput())

	if len(events.recorded) != 1 {
		t.Errorf("event must still record with nil notifier, got %d", len(events.recorded))
	}
}

func TestSuspiciousLogin_NilEventsSkipsRecord(t *testing.T) {
	notifier := &stubNotifier{configured: true}
	svc := newTestNotifier(t, nil, notifier)

	svc.OnLogin(context.Background(), sampleInput())

	// Email still fires even without event recording — each half is
	// independent.
	if len(notifier.sends) != 1 {
		t.Errorf("expected 1 email sent, got %d", len(notifier.sends))
	}
}

func TestSuspiciousLogin_MissingUserShortCircuits(t *testing.T) {
	events := &stubEvents{}
	notifier := &stubNotifier{configured: true}
	svc := newTestNotifier(t, events, notifier)

	in := sampleInput()
	in.User = nil
	svc.OnLogin(context.Background(), in)

	if len(events.recorded) != 0 || len(notifier.sends) != 0 {
		t.Errorf("nil user must short-circuit: events=%d sends=%d", len(events.recorded), len(notifier.sends))
	}
}

func TestSuspiciousLogin_EmailErrorDoesNotPanic(t *testing.T) {
	events := &stubEvents{}
	notifier := &stubNotifier{configured: true, sendErr: errors.New("smtp down")}
	svc := newTestNotifier(t, events, notifier)

	// Must not panic, must not propagate, must still record the event.
	svc.OnLogin(context.Background(), sampleInput())

	if len(events.recorded) != 1 {
		t.Errorf("event must still record when email errors, got %d", len(events.recorded))
	}
}

func TestSuspiciousLogin_EventErrorDoesNotBlockEmail(t *testing.T) {
	events := &stubEvents{recErr: errors.New("mongo down")}
	notifier := &stubNotifier{configured: true}
	svc := newTestNotifier(t, events, notifier)

	svc.OnLogin(context.Background(), sampleInput())

	// Event failed; email still goes through — each half is independent.
	if len(notifier.sends) != 1 {
		t.Errorf("expected 1 email despite event error, got %d", len(notifier.sends))
	}
}

func TestSuspiciousLogin_ThresholdOverride(t *testing.T) {
	events := &stubEvents{}
	notifier := &stubNotifier{configured: true}
	svc := NewSuspiciousLoginNotifier(NotifierConfig{
		Events:         events,
		Notifier:       notifier,
		EmailThreshold: 0.8, // tighter than default 0.5
	}).(*suspiciousLoginNotifier)

	in := sampleInput() // score 0.6
	svc.OnLogin(context.Background(), in)

	if len(events.recorded) != 1 || events.recorded[0].EventType != "login" {
		t.Errorf("custom threshold: 0.6 < 0.8 → event should be 'login' (info), got %+v", events.recorded)
	}
	if len(notifier.sends) != 0 {
		t.Errorf("custom threshold: 0.6 < 0.8 → no email, got %d sends", len(notifier.sends))
	}
}
