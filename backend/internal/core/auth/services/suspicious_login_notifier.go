// Package services — suspicious-login notifier.
//
// Section C item #5 of the 2026-04-24 auth roadmap. Consumes the
// RiskAssessment produced by C1 and:
//
//  1. Always records a row in auth_security_events so the user's
//     security history page has something to render.
//  2. When the score lands in the "high" bucket or above (>= 0.5),
//     sends the auth.suspicious_login templated email via the
//     notification module. The email is transactional — preferences
//     and unsubscribes don't suppress it.
//
// The notifier runs fire-and-forget from the login hot path: failures
// log at Warn but never propagate, so a flaky notifier or SMTP
// outage never blocks a legitimate login.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	notifModels "github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// SuspiciousLoginEmailThreshold is the risk-score boundary at which
// the notifier escalates from "record a security event" to "also
// email the user". Matches RiskLevelHigh (0.5) — anything higher is
// more urgent (RiskLevelCritical at 0.7), anything lower is a false
// positive we don't want to spam users about.
const SuspiciousLoginEmailThreshold = 0.5

// SuspiciousLoginNotifier is the narrow interface the login flow
// depends on. One entry point so tests can stub the whole side
// effect; the concrete impl composes SecurityEventService +
// NotificationSender internally.
type SuspiciousLoginNotifier interface {
	OnLogin(ctx context.Context, in SuspiciousLoginInput)
}

// SuspiciousLoginInput bundles the signals the notifier needs to
// decide whether to email + what to stamp on the security event.
// Session and assessment are required; everything else degrades to
// empty-string in the email template.
type SuspiciousLoginInput struct {
	User       *AuthUser
	Session    *SessionSnapshot
	Assessment *models.RiskAssessment
	IPAddress  string
	DeviceName string
	Platform   string
	UserAgent  string
}

// AuthUser is a minimal user projection — email + name + uuid — so
// the notifier doesn't need a full models.User reference. The login
// flow converts whatever representation it has into this shape
// before calling OnLogin.
type AuthUser struct {
	UUID  string
	Email string
	Name  string
}

// SessionSnapshot is the subset of session metadata the notifier
// needs: uuid (for idempotency keys) and createdAt (for the email's
// "when" line).
type SessionSnapshot struct {
	UUID      string
	CreatedAt time.Time
}

// NotifierConfig bundles the dependencies.
type NotifierConfig struct {
	Events       SecurityEventService
	Notifier     iface.NotificationSender
	AppName      string
	SupportEmail string
	FrontendURL  string
	Logger       *slog.Logger
	// Policy is read on every OnLogin call so the admin recipient
	// list / toggle behave live without restarts. Nil falls back to
	// "admin email half disabled".
	Policy *AuthPolicyService
	// EmailThreshold overrides SuspiciousLoginEmailThreshold for
	// tests. Zero or negative falls back to the default.
	EmailThreshold float64
	// Clock is injectable for tests that want a deterministic
	// "LoginAt" rendering in the email body. Nil → time.Now.
	Clock func() time.Time
}

type suspiciousLoginNotifier struct {
	events       SecurityEventService
	notifier     iface.NotificationSender
	appName      string
	supportEmail string
	frontendURL  string
	logger       *slog.Logger
	policy       *AuthPolicyService
	threshold    float64
	clock        func() time.Time
}

// NewSuspiciousLoginNotifier constructs the notifier. A nil
// SecurityEventService disables the event-log half; a nil
// NotificationSender disables the email half. Both halves are
// independent — each fires only when its dependency is wired.
func NewSuspiciousLoginNotifier(cfg NotifierConfig) SuspiciousLoginNotifier {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	threshold := cfg.EmailThreshold
	if threshold <= 0 {
		threshold = SuspiciousLoginEmailThreshold
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	appName := cfg.AppName
	if appName == "" {
		appName = "Orkestra"
	}
	return &suspiciousLoginNotifier{
		events:       cfg.Events,
		notifier:     cfg.Notifier,
		appName:      appName,
		supportEmail: cfg.SupportEmail,
		frontendURL:  cfg.FrontendURL,
		logger:       logger,
		policy:       cfg.Policy,
		threshold:    threshold,
		clock:        clock,
	}
}

func (n *suspiciousLoginNotifier) OnLogin(ctx context.Context, in SuspiciousLoginInput) {
	if in.User == nil || in.User.UUID == "" || in.Session == nil || in.Assessment == nil {
		return
	}

	n.recordSecurityEvent(ctx, in)

	if in.Assessment.Score < n.threshold {
		return
	}
	n.sendEmail(ctx, in)
	// Admin half — gated by notifyAdminOnSuspiciousLogin and a
	// non-empty recipient list. Same threshold as the user email so
	// admins see exactly what the user sees, no more no less. Each
	// recipient gets a distinct idempotency key so a parallel handler
	// invocation can't collapse the fan-out.
	n.sendAdminEmail(ctx, in)
}

// recordSecurityEvent persists a security_events row regardless of
// score. Low-risk logins with no factors produce a minimal "login"
// event the user's security-history page can show as a green
// baseline entry; high-risk ones carry the full factor list in
// metadata for audit. Best-effort — never propagates errors.
func (n *suspiciousLoginNotifier) recordSecurityEvent(ctx context.Context, in SuspiciousLoginInput) {
	if n.events == nil {
		return
	}
	eventType := "login"
	severity := "info"
	if in.Assessment.Score >= n.threshold {
		eventType = "suspicious_login"
		severity = "warning"
	}
	factorNames := make([]string, 0, len(in.Assessment.Factors))
	for _, f := range in.Assessment.Factors {
		if f.Details != nil {
			if name, ok := f.Details["factor"].(string); ok && name != "" {
				factorNames = append(factorNames, name)
			}
		}
	}
	ev := &models.SecurityEvent{
		UserUUID:    in.User.UUID,
		EventType:   eventType,
		Severity:    severity,
		Description: fmt.Sprintf("Login scored %s (%.2f)", in.Assessment.Level, in.Assessment.Score),
		IPAddress:   in.IPAddress,
		UserAgent:   in.UserAgent,
		SessionID:   in.Session.UUID,
		Success:     true,
		RiskScore:   in.Assessment.Score,
		Metadata: map[string]interface{}{
			"deviceName": in.DeviceName,
			"platform":   in.Platform,
			"factors":    factorNames,
			"riskLevel":  in.Assessment.Level,
		},
		Timestamp: n.clock(),
	}
	if err := n.events.RecordEvent(ctx, ev); err != nil {
		n.logger.Warn("suspicious_login: failed to record security event",
			slog.String("user_uuid", in.User.UUID),
			slog.String("session_uuid", in.Session.UUID),
			slog.String("error", err.Error()))
	}
}

// sendEmail dispatches the auth.suspicious_login template through
// NotificationSender. The idempotency key includes the session
// UUID so refresh-induced re-calls (or a parallel handler) don't
// produce duplicate emails for the same login.
func (n *suspiciousLoginNotifier) sendEmail(ctx context.Context, in SuspiciousLoginInput) {
	if n.notifier == nil || !n.notifier.IsConfigured(ctx) {
		return
	}
	if in.User.Email == "" {
		return
	}
	userName := in.User.Name
	if userName == "" {
		userName = strings.TrimSuffix(in.User.Email, "@"+afterAt(in.User.Email))
		if userName == "" {
			userName = "there"
		}
	}
	deviceSummary := in.DeviceName
	if deviceSummary == "" {
		deviceSummary = in.Platform
	}
	if deviceSummary == "" {
		deviceSummary = "Unknown device"
	}
	factorNames := make([]string, 0, len(in.Assessment.Factors))
	for _, f := range in.Assessment.Factors {
		if f.Details != nil {
			if name, ok := f.Details["factor"].(string); ok && name != "" {
				factorNames = append(factorNames, name)
			}
		}
	}
	accountActivityURL := strings.TrimRight(n.frontendURL, "/") + "/user/security?tab=sessions"

	loginAt := in.Session.CreatedAt
	if loginAt.IsZero() {
		loginAt = n.clock()
	}
	vars := map[string]any{
		"AppName":            n.appName,
		"UserName":           userName,
		"LoginAt":            loginAt.UTC().Format("2006-01-02 15:04 UTC"),
		"LoginIP":            in.IPAddress,
		"LoginDevice":        deviceSummary,
		"LoginLocation":      "", // populated by a C4 follow-up once GeoIP stamps SessionDoc.Location
		"RiskLevel":          capitalize(in.Assessment.Level),
		"RiskFactors":        strings.Join(factorNames, ", "),
		"AccountActivityURL": accountActivityURL,
		"SupportEmail":       n.supportEmail,
	}
	req := iface.TemplatedNotificationRequest{
		Channel:  notifModels.ChannelEmail,
		Type:     notifModels.TypeTransactional,
		Category: notifModels.CategoryAuthSuspiciousLogin,
		Recipients: []iface.Recipient{{
			UserUUID: in.User.UUID,
			Address:  in.User.Email,
			Name:     userName,
		}},
		TemplateID:     notifModels.CategoryAuthSuspiciousLogin,
		Data:           vars,
		IdempotencyKey: fmt.Sprintf("suspicious:%s:%s", in.User.UUID, in.Session.UUID),
	}
	if _, err := n.notifier.SendTemplated(ctx, req); err != nil {
		n.logger.Warn("suspicious_login: failed to send email",
			slog.String("user_uuid", in.User.UUID),
			slog.String("session_uuid", in.Session.UUID),
			slog.String("error", err.Error()))
		return
	}
	n.logger.Info("suspicious_login: notified user",
		slog.String("user_uuid", in.User.UUID),
		slog.String("session_uuid", in.Session.UUID),
		slog.Float64("score", in.Assessment.Score),
		slog.String("level", in.Assessment.Level))
}

// sendAdminEmail dispatches CategoryAuthAdminSuspiciousLogin to each
// configured admin recipient when notifyAdminOnSuspiciousLogin is on
// and at least one recipient is configured. Reads the policy live so
// admin edits don't require a restart. Best-effort — failures log and
// continue to the next recipient instead of aborting the fan-out.
func (n *suspiciousLoginNotifier) sendAdminEmail(ctx context.Context, in SuspiciousLoginInput) {
	if n.policy == nil || n.notifier == nil || !n.notifier.IsConfigured(ctx) {
		return
	}
	if !n.policy.NotifyAdminOnSuspiciousLogin(ctx) {
		return
	}
	recipients := n.policy.SuspiciousLoginRecipients(ctx)
	if len(recipients) == 0 {
		return
	}
	deviceSummary := in.DeviceName
	if deviceSummary == "" {
		deviceSummary = in.Platform
	}
	if deviceSummary == "" {
		deviceSummary = "Unknown device"
	}
	factorNames := make([]string, 0, len(in.Assessment.Factors))
	for _, f := range in.Assessment.Factors {
		if f.Details != nil {
			if name, ok := f.Details["factor"].(string); ok && name != "" {
				factorNames = append(factorNames, name)
			}
		}
	}
	loginAt := in.Session.CreatedAt
	if loginAt.IsZero() {
		loginAt = n.clock()
	}
	accountActivityURL := strings.TrimRight(n.frontendURL, "/") + "/user/security?tab=sessions"
	affectedName := in.User.Name
	if affectedName == "" {
		affectedName = in.User.Email
	}
	vars := map[string]any{
		"AppName":            n.appName,
		"AffectedUserName":   affectedName,
		"AffectedUserEmail":  in.User.Email,
		"AffectedUserUUID":   in.User.UUID,
		"LoginAt":            loginAt.UTC().Format("2006-01-02 15:04 UTC"),
		"LoginIP":            in.IPAddress,
		"LoginDevice":        deviceSummary,
		"LoginLocation":      "",
		"RiskLevel":          capitalize(in.Assessment.Level),
		"RiskFactors":        strings.Join(factorNames, ", "),
		"AccountActivityURL": accountActivityURL,
		"SupportEmail":       n.supportEmail,
	}
	for _, addr := range recipients {
		req := iface.TemplatedNotificationRequest{
			Channel:  notifModels.ChannelEmail,
			Type:     notifModels.TypeTransactional,
			Category: notifModels.CategoryAuthAdminSuspiciousLogin,
			Recipients: []iface.Recipient{{
				Address: addr,
				Name:    addr,
			}},
			TemplateID:     notifModels.CategoryAuthAdminSuspiciousLogin,
			Data:           vars,
			IdempotencyKey: fmt.Sprintf("suspicious-admin:%s:%s:%s", in.User.UUID, in.Session.UUID, addr),
		}
		if _, err := n.notifier.SendTemplated(ctx, req); err != nil {
			n.logger.Warn("suspicious_login: failed to send admin email",
				slog.String("recipient", addr),
				slog.String("user_uuid", in.User.UUID),
				slog.String("session_uuid", in.Session.UUID),
				slog.String("error", err.Error()))
			continue
		}
		n.logger.Info("suspicious_login: notified admin",
			slog.String("recipient", addr),
			slog.String("user_uuid", in.User.UUID),
			slog.String("session_uuid", in.Session.UUID),
			slog.Float64("score", in.Assessment.Score))
	}
}

// afterAt returns the substring after the "@" in an email. Used to
// carve a fallback user name out of the local-part when the caller
// didn't supply one. Returns empty on malformed input.
func afterAt(email string) string {
	i := strings.LastIndex(email, "@")
	if i < 0 || i == len(email)-1 {
		return ""
	}
	return email[i+1:]
}

// capitalize returns s with its first rune upper-cased and the rest
// unchanged. Used for the RiskLevel email variable ("high" → "High")
// without pulling in x/text — strings.Title is deprecated.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
