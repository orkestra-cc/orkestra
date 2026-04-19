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
)

var (
	ErrSubscriptionTierNotFound = errors.New("pricing tier not found on service")
	ErrSubscriptionCancelled    = errors.New("subscription is already cancelled")
	ErrSubscriptionNotPausable  = errors.New("subscription cannot be reactivated from current state")
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
	// Brand-new subscriptions start in Active status so the tenant gets the
	// tier's capability bundle immediately (payment is confirmed on the
	// first renewal tick). The syncer is a no-op when TenantUUID is empty —
	// legacy client-only subs keep working without capability grants.
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
