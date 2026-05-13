package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/repository"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/metrics"
)

// EntitlementSyncer keeps the capability projection aligned with
// subscription lifecycle changes. Subscription services and the webhook
// reconciler both invoke it on state transitions:
//
//   - activate  (Create, past_due → active on invoice paid, Reactivate)
//     → grant every capability ID listed on the subscription's tier
//   - deactivate (Cancel, failed-charges → suspended)
//     → revoke those same capability IDs
//
// The syncer is intentionally stateless: it reads tier.Capabilities fresh
// on every call so an admin rewriting a service catalog is reflected on the
// next activation without a migration.
//
// AccessProvider may be nil in degraded deployments (e.g. a test rig that
// boots subscriptions without the tenant module). In that case every method
// is a no-op — subscription state still updates, just without the
// entitlement side-effect. Callers must not rely on the syncer for
// correctness of the subscription state machine itself.
type EntitlementSyncer struct {
	services repository.ServiceRepository
	access   iface.AccessProvider
	tenant   iface.TenantProvider
	logger   *slog.Logger
}

// NewEntitlementSyncer constructs a syncer. Pass a nil AccessProvider for
// setups that do not run the tenant module; the syncer degrades to no-ops.
// The TenantProvider is optional and used only for tenant-kind labelling
// in metrics.
func NewEntitlementSyncer(services repository.ServiceRepository, access iface.AccessProvider, tenant iface.TenantProvider, logger *slog.Logger) *EntitlementSyncer {
	return &EntitlementSyncer{services: services, access: access, tenant: tenant, logger: logger}
}

// OnActivate grants every capability listed on the subscription's tier to
// its tenant. Each grant is tagged with the subscription UUID as SourceRef
// so revocations can target the right rows. Expiry is set to the current
// period end when known — entitlements naturally lapse if the next renewal
// doesn't land, closing the window even when webhooks go missing.
//
// Errors on individual grants are logged but do not abort the loop — a
// transient tenant-side failure on one capability should not prevent the
// other capabilities from landing.
func (s *EntitlementSyncer) OnActivate(ctx context.Context, sub *models.Subscription) {
	if s == nil || s.access == nil || sub == nil {
		return
	}
	if sub.TenantUUID == "" {
		return
	}
	svc, err := s.services.GetByUUID(ctx, sub.ServiceUUID)
	if err != nil {
		s.logger.Warn("entitlement syncer: service lookup failed",
			slog.String("subscription", sub.UUID),
			slog.String("service", sub.ServiceUUID),
			slog.String("error", err.Error()),
		)
		return
	}
	tier := svc.FindTier(sub.TierCode)
	if tier == nil || len(tier.Capabilities) == 0 {
		return
	}
	var expires *time.Time
	if !sub.CurrentPeriodEnd.IsZero() {
		t := sub.CurrentPeriodEnd
		expires = &t
	}
	for _, capID := range tier.Capabilities {
		err := s.access.GrantCapability(ctx, iface.GrantCapabilityInput{
			TenantUUID:   sub.TenantUUID,
			CapabilityID: capID,
			Source:       CapabilitySourceSubscription,
			SourceRef:    sub.UUID,
			GrantedBy:    "system",
			ExpiresAt:    expires,
			Metadata: map[string]any{
				"serviceCode": svc.Code,
				"tierCode":    tier.Code,
			},
		})
		if err != nil {
			s.logger.Warn("entitlement syncer: grant failed",
				slog.String("subscription", sub.UUID),
				slog.String("capability", capID),
				slog.String("error", err.Error()),
			)
			continue
		}
		metrics.Default().RecordEntitlementApply(s.tenantLabel(ctx, sub.TenantUUID))
	}
}

// OnDeactivate revokes every capability listed on the subscription's tier
// for its tenant. Revoke is idempotent at the access-provider side —
// revoking a non-active entitlement is a no-op — so double-invocation on
// repeated webhooks is safe.
func (s *EntitlementSyncer) OnDeactivate(ctx context.Context, sub *models.Subscription) {
	if s == nil || s.access == nil || sub == nil {
		return
	}
	if sub.TenantUUID == "" {
		return
	}
	svc, err := s.services.GetByUUID(ctx, sub.ServiceUUID)
	if err != nil {
		s.logger.Warn("entitlement syncer: service lookup failed",
			slog.String("subscription", sub.UUID),
			slog.String("service", sub.ServiceUUID),
			slog.String("error", err.Error()),
		)
		return
	}
	tier := svc.FindTier(sub.TierCode)
	if tier == nil {
		return
	}
	for _, capID := range tier.Capabilities {
		if err := s.access.RevokeCapability(ctx, sub.TenantUUID, capID); err != nil {
			s.logger.Warn("entitlement syncer: revoke failed",
				slog.String("subscription", sub.UUID),
				slog.String("capability", capID),
				slog.String("error", err.Error()),
			)
			continue
		}
		metrics.Default().RecordEntitlementApply(s.tenantLabel(ctx, sub.TenantUUID))
	}
}

// tenantLabel returns a metric label for the tenant — its kind ("internal"
// | "external"). Empty on lookup failure; RecordEntitlementApply ignores
// empty labels.
func (s *EntitlementSyncer) tenantLabel(ctx context.Context, tenantUUID string) string {
	if s == nil || tenantUUID == "" || s.tenant == nil {
		return ""
	}
	t, err := s.tenant.GetTenant(ctx, tenantUUID)
	if err != nil || t == nil {
		return ""
	}
	return t.Kind
}

// CapabilitySourceSubscription mirrors the tenant-side constant without
// importing the tenant services package (would pull a tenant-repository
// dependency into addons).
const CapabilitySourceSubscription = "subscription"
