package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

var (
	ErrSubscriptionTierNotFound = errors.New("pricing tier not found on service")
	ErrSubscriptionCancelled    = errors.New("subscription is already cancelled")
	ErrSubscriptionNotPausable  = errors.New("subscription cannot be reactivated from current state")
	ErrSubscriptionServiceCode  = errors.New("service code not found or inactive")
)

// SubscriptionService owns the state machine and period math for a
// Subscription. It does NOT trigger charges — that's RenewalService's job.
type SubscriptionService struct {
	subs        repository.SubscriptionRepository
	clients     repository.ClientRepository
	services    repository.ServiceRepository
	activity    *ActivityService
	entitlement *EntitlementSyncer
	logger      *slog.Logger
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
// and tier codes plus tenant scope when known.
func subscriptionMetadata(sub *models.Subscription) map[string]any {
	return map[string]any{
		"serviceUUID": sub.ServiceUUID,
		"tierCode":    sub.TierCode,
		"tenantUUID":  sub.TenantUUID,
		"clientUUID":  sub.ClientUUID,
	}
}

func NewSubscriptionService(
	subs repository.SubscriptionRepository,
	clients repository.ClientRepository,
	services repository.ServiceRepository,
	activity *ActivityService,
	entitlement *EntitlementSyncer,
	logger *slog.Logger,
) *SubscriptionService {
	return &SubscriptionService{
		subs:        subs,
		clients:     clients,
		services:    services,
		activity:    activity,
		entitlement: entitlement,
		logger:      logger,
	}
}

// Create begins a subscription. The first charge is handled by the renewal
// job as soon as NextBillingAt (= now) comes due.
func (s *SubscriptionService) Create(ctx context.Context, clientUUID, serviceUUID, tierCode, actor string) (*models.Subscription, error) {
	client, err := s.clients.GetByUUID(ctx, clientUUID)
	if err != nil {
		return nil, fmt.Errorf("load client: %w", err)
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
		ClientUUID:         client.UUID,
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
	})
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     sub.TenantUUID,
		ActorUserID:  actor,
		Action:       "subscription.created",
		ResourceType: "subscription",
		ResourceID:   sub.UUID,
		Metadata:     subscriptionMetadata(sub),
	})
	// Brand-new subscriptions start in Active status so the tenant gets the
	// tier's capability bundle immediately (payment is confirmed on the
	// first renewal tick). The syncer is a no-op when TenantUUID is empty —
	// legacy client-only subs keep working without capability grants.
	s.entitlement.OnActivate(ctx, sub)
	return sub, nil
}

// CreateForTenant is the Phase-3 self-service subscribe path. Unlike
// Create, it takes a TenantUUID (Tier-2 external tenant) and a human-
// typed service/tier code pair — there is no Client row. Activation fires
// immediately via the entitlement syncer so the tenant's capability
// projection receives the tier's grants before the first renewal tick.
//
// This intentionally bypasses the Client entity: under ADR-0001 the
// external Tenant replaces Client, and the self-service path should never
// resurrect the legacy shape. Operator-created subscriptions still flow
// through Create (with a Client row) while the catalog-admin UI exists.
func (s *SubscriptionService) CreateForTenant(ctx context.Context, tenantUUID, serviceCode, tierCode, actor string) (*models.Subscription, error) {
	if tenantUUID == "" {
		return nil, errors.New("subscriptions: CreateForTenant requires tenantUUID")
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
	// Grant the tier's capabilities to the tenant right away so the user
	// can consume gated routes immediately after subscribing. The first
	// charge still happens on the next renewal tick; if that charge fails,
	// the normal past_due → suspended state machine revokes the grants.
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
