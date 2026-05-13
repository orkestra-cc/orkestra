package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/repository"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

var (
	ErrSubscriptionTierNotFound = errors.New("pricing tier not found on service")
	ErrSubscriptionCancelled    = errors.New("subscription is already cancelled")
	ErrSubscriptionNotPausable  = errors.New("subscription cannot be reactivated from current state")
	ErrSubscriptionServiceCode  = errors.New("service code not found or inactive")
	ErrSubscriptionTenant       = errors.New("tenantUUID is required")
)

// SubscriptionService owns the state machine and period math for a
// Subscription. It does NOT trigger charges — that's RenewalService's job.
//
// Subscriptions are owned by a Tenant aggregate (post Unified Client
// Aggregate refactor). Self-registered clients hold subscriptions on their
// personal tenant; admin-attached business clients hold them on the
// business tenant.
type SubscriptionService struct {
	subs        repository.SubscriptionRepository
	services    repository.ServiceRepository
	activity    *ActivityService
	entitlement *EntitlementSyncer
	// tenants is consulted by callers (e.g. self-serve) for ownership
	// checks. The service itself only needs it to render audit metadata.
	tenants iface.TenantProvider
	logger  *slog.Logger
	// auditSink is wired post-construction by the compliance module so
	// subscription lifecycle events land on the platform audit trail
	// alongside this module's own subscriptions_activity log.
	auditSink iface.AuditSink
}

// SetAuditSink wires the compliance audit sink post-construction.
func (s *SubscriptionService) SetAuditSink(sink iface.AuditSink) { s.auditSink = sink }

// emitAudit forwards to the sink when wired; no-op otherwise. Kept terse
// so emit sites inline cleanly.
func (s *SubscriptionService) emitAudit(ctx context.Context, event iface.AuditEvent) {
	if s.auditSink == nil {
		return
	}
	s.auditSink.Emit(ctx, event)
}

// subscriptionMetadata builds the common audit metadata payload — service
// and tier codes plus the tenant scope.
func subscriptionMetadata(sub *models.Subscription) map[string]any {
	return map[string]any{
		"serviceUUID": sub.ServiceUUID,
		"tierCode":    sub.TierCode,
		"tenantUUID":  sub.TenantUUID,
	}
}

func NewSubscriptionService(
	subs repository.SubscriptionRepository,
	services repository.ServiceRepository,
	activity *ActivityService,
	entitlement *EntitlementSyncer,
	tenants iface.TenantProvider,
	logger *slog.Logger,
) *SubscriptionService {
	return &SubscriptionService{
		subs:        subs,
		services:    services,
		activity:    activity,
		entitlement: entitlement,
		tenants:     tenants,
		logger:      logger,
	}
}

// Create begins a subscription for the given tenant. The first charge is
// handled by the renewal job as soon as NextBillingAt (= now) comes due.
//
// Entitlements are granted immediately on activation so admin- or
// self-service-created subscriptions let the tenant hit gated routes
// without waiting for the first renewal tick; if the first charge later
// fails, the standard past_due → suspended flow revokes them.
func (s *SubscriptionService) Create(ctx context.Context, tenantUUID, serviceUUID, tierCode, actor string) (*models.Subscription, error) {
	if tenantUUID == "" {
		return nil, ErrSubscriptionTenant
	}
	svc, err := s.services.GetByUUID(ctx, serviceUUID)
	if err != nil {
		return nil, fmt.Errorf("load service: %w", err)
	}
	tier := svc.FindTier(tierCode)
	if tier == nil {
		return nil, ErrSubscriptionTierNotFound
	}

	now := time.Now().UTC()
	sub := &models.Subscription{
		UUID:               uuid.NewString(),
		TenantUUID:         tenantUUID,
		ServiceUUID:        svc.UUID,
		TierCode:           tier.Code,
		Status:             models.SubActive,
		StartedAt:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   AdvancePeriod(now, tier.Cycle),
		NextBillingAt:      now, // first charge happens on next renewal tick
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.subs.Create(ctx, sub); err != nil {
		return nil, err
	}
	s.activity.Log(ctx, sub, actor, models.ActivityCreated, "Subscription created", map[string]any{
		"serviceCode": svc.Code,
		"tierCode":    tier.Code,
		"tenantUUID":  tenantUUID,
	})
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     sub.TenantUUID,
		ActorUserID:  actor,
		Action:       "subscription.created",
		ResourceType: "subscription",
		ResourceID:   sub.UUID,
		Metadata:     subscriptionMetadata(sub),
	})
	s.entitlement.OnActivate(ctx, sub)
	return sub, nil
}

// CreateForTenant is the self-service subscribe path. Looks up the service
// by its human-typed code (instead of the internal serviceUUID Create
// accepts) so the SPA can pass a stable SKU identifier. Activation fires
// immediately via the entitlement syncer so the tenant's capability
// projection receives the tier's grants before the first renewal tick.
func (s *SubscriptionService) CreateForTenant(ctx context.Context, tenantUUID, serviceCode, tierCode, actor string) (*models.Subscription, error) {
	if tenantUUID == "" {
		return nil, ErrSubscriptionTenant
	}
	svc, err := s.services.GetByCode(ctx, serviceCode)
	if err != nil || svc == nil || !svc.Active {
		return nil, ErrSubscriptionServiceCode
	}
	tier := svc.FindTier(tierCode)
	if tier == nil {
		return nil, ErrSubscriptionTierNotFound
	}

	now := time.Now().UTC()
	sub := &models.Subscription{
		UUID:               uuid.NewString(),
		TenantUUID:         tenantUUID,
		ServiceUUID:        svc.UUID,
		TierCode:           tier.Code,
		Status:             models.SubActive,
		StartedAt:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   AdvancePeriod(now, tier.Cycle),
		NextBillingAt:      now,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.subs.Create(ctx, sub); err != nil {
		return nil, err
	}
	s.activity.Log(ctx, sub, actor, models.ActivityCreated, "Subscription created (self-service)", map[string]any{
		"serviceCode": svc.Code,
		"tierCode":    tier.Code,
		"tenantUUID":  tenantUUID,
	})
	selfServeMeta := subscriptionMetadata(sub)
	selfServeMeta["selfService"] = true
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     sub.TenantUUID,
		ActorUserID:  actor,
		Action:       "subscription.created",
		ResourceType: "subscription",
		ResourceID:   sub.UUID,
		Metadata:     selfServeMeta,
	})
	s.entitlement.OnActivate(ctx, sub)
	return sub, nil
}

func (s *SubscriptionService) Get(ctx context.Context, uuid string) (*models.Subscription, error) {
	return s.subs.GetByUUID(ctx, uuid)
}

func (s *SubscriptionService) List(ctx context.Context, f repository.SubscriptionFilters) ([]models.Subscription, error) {
	return s.subs.List(ctx, f)
}

// Cancel marks the subscription for cancellation. When `atPeriodEnd` is
// true the subscription keeps running until CurrentPeriodEnd; otherwise it
// becomes Cancelled immediately.
func (s *SubscriptionService) Cancel(ctx context.Context, uuid string, atPeriodEnd bool, actor string) (*models.Subscription, error) {
	sub, err := s.subs.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if sub.Status == models.SubCancelled {
		return nil, ErrSubscriptionCancelled
	}
	now := time.Now().UTC()
	sub.CancelAtPeriodEnd = atPeriodEnd
	if atPeriodEnd {
		sub.EndsAt = &sub.CurrentPeriodEnd
	} else {
		sub.Status = models.SubCancelled
		sub.CancelledAt = &now
		sub.EndsAt = &now
	}
	if err := s.subs.Update(ctx, sub); err != nil {
		return nil, err
	}
	s.activity.Log(ctx, sub, actor, models.ActivityCancelled, "Subscription cancelled", map[string]any{
		"atPeriodEnd": atPeriodEnd,
	})
	cancelMeta := subscriptionMetadata(sub)
	cancelMeta["atPeriodEnd"] = atPeriodEnd
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     sub.TenantUUID,
		ActorUserID:  actor,
		Action:       "subscription.cancelled",
		ResourceType: "subscription",
		ResourceID:   sub.UUID,
		Metadata:     cancelMeta,
	})
	// Immediate cancel revokes capabilities right away; cancel-at-period-end
	// keeps them until the period actually lapses (ExpiresAt on the grant
	// handles the natural expiry). Do nothing on the deferred branch.
	if !atPeriodEnd {
		s.entitlement.OnDeactivate(ctx, sub)
	}
	return sub, nil
}

// Reactivate returns a past_due or suspended subscription to active with a
// fresh period starting now. The next charge happens on the next renewal tick.
func (s *SubscriptionService) Reactivate(ctx context.Context, uuid string, actor string) (*models.Subscription, error) {
	sub, err := s.subs.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if sub.Status != models.SubPastDue && sub.Status != models.SubSuspended {
		return nil, ErrSubscriptionNotPausable
	}
	svc, err := s.services.GetByUUID(ctx, sub.ServiceUUID)
	if err != nil {
		return nil, err
	}
	tier := svc.FindTier(sub.TierCode)
	if tier == nil {
		return nil, ErrSubscriptionTierNotFound
	}
	now := time.Now().UTC()
	sub.Status = models.SubActive
	sub.CurrentPeriodStart = now
	sub.CurrentPeriodEnd = AdvancePeriod(now, tier.Cycle)
	sub.NextBillingAt = now
	sub.FailedChargeCount = 0
	sub.CancelledAt = nil
	sub.EndsAt = nil
	sub.CancelAtPeriodEnd = false
	if err := s.subs.Update(ctx, sub); err != nil {
		return nil, err
	}
	s.activity.Log(ctx, sub, actor, models.ActivityReactivated, "Subscription reactivated", nil)
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     sub.TenantUUID,
		ActorUserID:  actor,
		Action:       "subscription.reactivated",
		ResourceType: "subscription",
		ResourceID:   sub.UUID,
		Metadata:     subscriptionMetadata(sub),
	})
	// Reactivation restores tier capabilities — GrantCapability revokes any
	// stale/revoked row and writes a fresh one so the tenant is back to
	// entitled without needing a separate admin intervention.
	s.entitlement.OnActivate(ctx, sub)
	return sub, nil
}

// AdvancePeriod returns the end of the next billing period for the given cycle.
func AdvancePeriod(from time.Time, cycle models.BillingCycle) time.Time {
	switch cycle {
	case models.CycleMonthly:
		return from.AddDate(0, 1, 0)
	case models.CycleQuarterly:
		return from.AddDate(0, 3, 0)
	case models.CycleAnnual:
		return from.AddDate(1, 0, 0)
	default:
		return from.AddDate(0, 1, 0)
	}
}
