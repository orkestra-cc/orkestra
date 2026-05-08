package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/bson"
)

// GrantCapability creates an active entitlement for the tenant on the
// capability. If an active entitlement already exists, it is revoked first
// so the "at most one active row per (tenant, capability)" invariant holds.
// The replacement pattern lets subscription upgrades/downgrades land as a
// pair of (revoke old, insert new) rows — the history stays auditable.
//
// Signature matches iface.AccessProvider.GrantCapability so any module
// holding that interface handle can grant entitlements without importing
// the concrete tenant service.
func (s *Service) GrantCapability(ctx context.Context, in iface.GrantCapabilityInput) error {
	if in.TenantUUID == "" {
		return errors.New("tenant: GrantCapability requires TenantUUID")
	}
	if in.CapabilityID == "" {
		return errors.New("tenant: GrantCapability requires CapabilityID")
	}
	source := models.EntitlementSource(in.Source)
	if !source.Valid() {
		return fmt.Errorf("tenant: GrantCapability invalid source %q", in.Source)
	}
	if source == models.EntitlementSourceTrial && in.ExpiresAt == nil {
		return errors.New("tenant: trial entitlements must set ExpiresAt")
	}

	// Confirm the tenant exists so stale grants don't silently accumulate.
	if _, err := s.repo.GetTenantByUUID(ctx, in.TenantUUID); err != nil {
		return fmt.Errorf("tenant: GrantCapability: %w", err)
	}

	now := time.Now()
	// Revoke any existing active row for the same (tenant, capability).
	// Ignored if none exists (idempotent for first-time grants).
	if err := s.repo.RevokeActiveEntitlement(ctx, in.TenantUUID, in.CapabilityID, now); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("tenant: GrantCapability: revoke existing: %w", err)
	}

	ent := &models.Entitlement{
		UUID:         uuid.NewString(),
		TenantUUID:   in.TenantUUID,
		CapabilityID: in.CapabilityID,
		Source:       source,
		SourceRef:    in.SourceRef,
		GrantedBy:    in.GrantedBy,
		GrantedAt:    now,
		ExpiresAt:    in.ExpiresAt,
		Metadata:     in.Metadata,
	}
	if err := s.repo.CreateEntitlement(ctx, ent); err != nil {
		return fmt.Errorf("tenant: GrantCapability: insert: %w", err)
	}
	return nil
}

// RevokeCapability marks the active entitlement for the (tenant, capability)
// pair as revoked. Returns nil even if no active row exists (idempotent from
// the caller's point of view — e.g. a double webhook delivery).
func (s *Service) RevokeCapability(ctx context.Context, tenantUUID, capabilityID string) error {
	if tenantUUID == "" || capabilityID == "" {
		return errors.New("tenant: RevokeCapability requires TenantUUID and CapabilityID")
	}
	err := s.repo.RevokeActiveEntitlement(ctx, tenantUUID, capabilityID, time.Now())
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	return err
}

// HasCapability reports whether the tenant currently has an active
// entitlement to the given capability. Implements
// iface.AccessProvider.HasCapability.
//
// Internal (operator-side) tenants short-circuit to true: capabilities are
// the monetization seam for external clients, and internal tenants host
// the platform — they don't subscribe.
func (s *Service) HasCapability(ctx context.Context, tenantUUID, capabilityID string) (bool, error) {
	if tenantUUID == "" || capabilityID == "" {
		return false, nil
	}
	if t, err := s.repo.GetTenantByUUID(ctx, tenantUUID); err == nil && t.Kind == models.TenantKindInternal {
		return true, nil
	}
	return s.repo.HasActiveEntitlement(ctx, tenantUUID, capabilityID)
}

// ListEntitlements returns the active entitlements held by a tenant.
func (s *Service) ListEntitlements(ctx context.Context, tenantUUID string) ([]models.Entitlement, error) {
	return s.repo.ListActiveByTenant(ctx, tenantUUID)
}

// ListCapabilityIDs returns the deduplicated capability IDs the tenant
// currently has active entitlements for, in deterministic (sorted) order.
// Thin projection over ListEntitlements so Cedar's principal builder does
// not reason about the entitlement metadata. Implements
// iface.AccessProvider.ListCapabilityIDs.
func (s *Service) ListCapabilityIDs(ctx context.Context, tenantUUID string) ([]string, error) {
	if tenantUUID == "" {
		return nil, nil
	}
	ents, err := s.repo.ListActiveByTenant(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	if len(ents) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(ents))
	ids := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.CapabilityID == "" {
			continue
		}
		if _, dup := seen[e.CapabilityID]; dup {
			continue
		}
		seen[e.CapabilityID] = struct{}{}
		ids = append(ids, e.CapabilityID)
	}
	sort.Strings(ids)
	return ids, nil
}

// Capability source constants mirror iface.GrantCapabilityInput.Source
// values expected by GrantCapability. Kept in this file so internal callers
// can reference them without going through iface.
const (
	CapabilitySourceSubscription = string(models.EntitlementSourceSubscription)
	CapabilitySourceGrant        = string(models.EntitlementSourceGrant)
	CapabilitySourceTrial        = string(models.EntitlementSourceTrial)
)

// ActivateTenant flips a tenant's lifecycle state to `active`. Thin wrapper
// over MarkTenantActive so it can satisfy iface.TenantProvider without the
// caller reaching into the concrete tenant service. Idempotent at the repo
// layer — re-activating an already-active tenant is a no-op write.
func (s *Service) ActivateTenant(ctx context.Context, tenantUUID string) error {
	if tenantUUID == "" {
		return errors.New("tenant: ActivateTenant requires tenantUUID")
	}
	return s.MarkTenantActive(ctx, tenantUUID)
}

// SetTenantStripeCustomerID persists the Stripe customer identifier on the
// tenant row. Called by the subscriptions renewal service after Stripe mints a
// customer on the first charge for an external tenant. Idempotent — applying
// the same value is a no-op write.
func (s *Service) SetTenantStripeCustomerID(ctx context.Context, tenantUUID, stripeCustomerID string) error {
	if tenantUUID == "" {
		return errors.New("tenant: SetTenantStripeCustomerID requires tenantUUID")
	}
	return s.repo.UpdateTenant(ctx, tenantUUID, bson.M{"stripeCustomerID": stripeCustomerID})
}
