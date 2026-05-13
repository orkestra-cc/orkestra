package services

import (
	"context"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

// piiProducer implements iface.PIIProducer for the auth module. It
// aggregates the PII-bearing repositories (refresh tokens, sessions,
// email tokens, MFA factors, security events) behind a single subject so
// DSR export produces one coherent payload and erasure wipes everything
// in one pass.
//
// Hash fields (tokenHash, passwordHash) are intentionally excluded from
// the exported payload — they are server secrets the user cannot
// meaningfully consume. Everything that *is* PII (IPs, device
// fingerprints, timestamps, user agents, backup-code counts) is
// included.
type piiProducer struct {
	refresh     repository.RefreshTokenRepository
	sessions    repository.AuthSessionRepository
	emails      repository.EmailTokenRepository
	mfa         repository.MFAFactorRepository
	events      SecurityEventService
	deviceTrust repository.DeviceTrustRepository

	// Collection name labels for the DSR audit row. Captured at
	// construction so the producer can report tier-correct names
	// (operator_refresh_tokens vs client_refresh_tokens, etc.) without
	// reaching into the repository. Empty falls back to a generic label.
	refreshCollection  string
	sessionsCollection string
	emailsCollection   string
	mfaCollection      string
}

// NewPIIProducer builds the auth PII producer for a single tier. Pass
// the tier's collection-name constants (operator_* or client_*) so the
// DSR audit row labels are accurate.
func NewPIIProducer(
	refresh repository.RefreshTokenRepository,
	sessions repository.AuthSessionRepository,
	emails repository.EmailTokenRepository,
	mfa repository.MFAFactorRepository,
	events SecurityEventService,
	deviceTrust repository.DeviceTrustRepository,
	refreshCollection, sessionsCollection, emailsCollection, mfaCollection string,
) iface.PIIProducer {
	return &piiProducer{
		refresh:            refresh,
		sessions:           sessions,
		emails:             emails,
		mfa:                mfa,
		events:             events,
		deviceTrust:        deviceTrust,
		refreshCollection:  refreshCollection,
		sessionsCollection: sessionsCollection,
		emailsCollection:   emailsCollection,
		mfaCollection:      mfaCollection,
	}
}

// Subject is the stable bundle-key identifier for this producer.
func (p *piiProducer) Subject() string { return "auth" }

// ExportPersonalData returns every auth-owned record tied to userUUID.
// Token hashes are scrubbed (users can't do anything with them and they
// are not PII in the GDPR sense); timestamps, IPs, and device info are
// included since those are the personal-identifier surface.
func (p *piiProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	out := map[string]any{}

	if p.refresh != nil {
		tokens, err := p.refresh.GetActiveTokensByUser(ctx, userUUID)
		if err == nil {
			out["refreshTokens"] = scrubRefreshTokens(tokens)
		}
	}
	if p.sessions != nil {
		sessions, err := p.sessions.GetActiveSessionsByUser(ctx, userUUID)
		if err == nil {
			out["sessions"] = scrubSessions(sessions)
		}
	}
	if p.events != nil {
		// Cap the event export at 1000 rows so a noisy attacker account
		// doesn't blow up the export payload. If a subject needs more,
		// they can request the audit trail separately.
		events, err := p.events.GetUserEvents(ctx, userUUID, 1000)
		if err == nil {
			out["securityEvents"] = scrubSecurityEvents(events)
		}
	}
	if p.mfa != nil {
		// Report factor presence and type without exposing the encrypted
		// secret. The user already knows they've enrolled; the secret is
		// only useful server-side. WebAuthn credentials are reported as
		// per-credential metadata so the user can recognise their devices
		// in the export — public keys and counters carry no PII risk.
		factors := []map[string]any{}
		if totp, err := p.mfa.FindByUserAndType(ctx, userUUID, authModels.MFAFactorTOTP); err == nil && totp != nil {
			factors = append(factors, map[string]any{
				"type":                 string(totp.Type),
				"createdAt":            totp.CreatedAt,
				"lastUsedAt":           totp.LastUsedAt,
				"backupCodesRemaining": len(totp.BackupCodesHashed),
			})
		}
		if wa, err := p.mfa.FindByUserAndType(ctx, userUUID, authModels.MFAFactorWebAuthn); err == nil && wa != nil {
			creds := make([]map[string]any, 0, len(wa.WebAuthnCredentials))
			for _, c := range wa.WebAuthnCredentials {
				creds = append(creds, map[string]any{
					"name":       c.Name,
					"createdAt":  c.CreatedAt,
					"lastUsedAt": c.LastUsedAt,
					"transports": c.Transports,
				})
			}
			factors = append(factors, map[string]any{
				"type":        string(wa.Type),
				"createdAt":   wa.CreatedAt,
				"lastUsedAt":  wa.LastUsedAt,
				"credentials": creds,
			})
		}
		if len(factors) > 0 {
			out["mfaFactors"] = factors
		}
	}

	if p.deviceTrust != nil {
		grants, err := p.deviceTrust.ListActiveByUser(ctx, userUUID)
		if err == nil && len(grants) > 0 {
			out["deviceTrust"] = scrubDeviceTrust(grants)
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// PurgePersonalData wipes every auth-owned PII collection for the user.
// Collections reported back match the actual Mongo names so the DSR
// audit row is legible to auditors. The refresh-token reaper's
// replay-detection window (~7 days) tolerates deletions here —
// subsequent reuse of a now-deleted family would fail with
// "token not found" rather than triggering a family revocation.
//
// Tier note: this producer is registered per-tier in module.go (the
// canonical instance is operator-tier). The collection labels reflect
// the tier's storage so an operator-tier purge reports
// operator_refresh_tokens, etc.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string) (iface.PurgeResult, error) {
	var rows int64
	collections := make([]string, 0, 5)

	if p.refresh != nil {
		n, err := p.refresh.DeleteAllByUser(ctx, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		if n > 0 {
			rows += n
			collections = append(collections, p.refreshCollection)
		}
	}
	if p.sessions != nil {
		n, err := p.sessions.DeleteAllByUser(ctx, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		if n > 0 {
			rows += n
			collections = append(collections, p.sessionsCollection)
		}
	}
	if p.emails != nil {
		n, err := p.emails.DeleteAllByUser(ctx, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		if n > 0 {
			rows += n
			collections = append(collections, p.emailsCollection)
		}
	}
	if p.mfa != nil {
		n, err := p.mfa.DeleteAllByUser(ctx, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		if n > 0 {
			rows += n
			collections = append(collections, p.mfaCollection)
		}
	}
	if p.events != nil {
		n, err := p.events.DeleteAllByUser(ctx, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		if n > 0 {
			rows += n
			collections = append(collections, authModels.SecurityEventsCollection)
		}
	}
	if p.deviceTrust != nil {
		n, err := p.deviceTrust.DeleteAllByUser(ctx, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		if n > 0 {
			rows += n
			collections = append(collections, authModels.DeviceTrustCollection)
		}
	}

	return iface.PurgeResult{RowsDeleted: int(rows), Collections: collections}, nil
}

// --- internal scrubbers ---

func scrubRefreshTokens(tokens []*authModels.RefreshTokenDoc) []map[string]any {
	out := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, map[string]any{
			"uuid":      t.UUID,
			"deviceId":  t.DeviceID,
			"ipAddress": t.IPAddress,
			"userAgent": t.UserAgent,
			"createdAt": t.CreatedAt,
			"expiresAt": t.ExpiresAt,
		})
	}
	return out
}

func scrubSessions(sessions []*authModels.AuthSessionDoc) []map[string]any {
	out := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		m := map[string]any{
			"uuid":         s.UUID,
			"deviceId":     s.DeviceID,
			"lastActivity": s.LastActivity,
			"createdAt":    s.CreatedAt,
			"isActive":     s.IsActive,
		}
		m["deviceName"] = s.DeviceInfo.DeviceName
		m["platform"] = s.DeviceInfo.Platform
		out = append(out, m)
	}
	return out
}

func scrubSecurityEvents(events []*authModels.SecurityEvent) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{
			"eventType": e.EventType,
			"ipAddress": e.IPAddress,
			"riskScore": e.RiskScore,
			"timestamp": e.Timestamp,
		})
	}
	return out
}

func scrubDeviceTrust(grants []*authModels.DeviceTrustDoc) []map[string]any {
	out := make([]map[string]any, 0, len(grants))
	for _, g := range grants {
		out = append(out, map[string]any{
			"deviceId":     g.DeviceID,
			"deviceName":   g.DeviceName,
			"platform":     g.Platform,
			"ipAddress":    g.IPAddress,
			"userAgent":    g.UserAgent,
			"trustedAt":    g.TrustedAt,
			"trustedUntil": g.TrustedUntil,
			"lastUsedAt":   g.LastUsedAt,
			"grantedAmr":   g.GrantedAMR,
		})
	}
	return out
}

// compile-time assertion: iface.PIIProducer is satisfied by piiProducer.
var _ iface.PIIProducer = (*piiProducer)(nil)
