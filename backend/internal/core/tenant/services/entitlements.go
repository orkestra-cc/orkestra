package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// GrantCapability creates an active entitlement for the tenant on the
// capability. If an active entitlement already exists, it is revoked first so
// the "at most one active row per (tenant, capability)" invariant holds. The
// replacement pattern lets subscription upgrades/downgrades land as a pair of
// (revoke old, insert new) rows — the history stays auditable.
//
// Signature matches iface.TenantProvider.GrantCapability so any module
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
// entitlement to the given capability. Implements the
// iface.TenantProvider.HasCapability contract.
func (s *Service) HasCapability(ctx context.Context, tenantUUID, capabilityID string) (bool, error) {
	if tenantUUID == "" || capabilityID == "" {
		return false, nil
	}
	return s.repo.HasActiveEntitlement(ctx, tenantUUID, capabilityID)
}

// ListEntitlements returns the active entitlements held by a tenant.
func (s *Service) ListEntitlements(ctx context.Context, tenantUUID string) ([]models.Entitlement, error) {
	return s.repo.ListActiveByTenant(ctx, tenantUUID)
}

// Capability source constants mirror iface.GrantCapabilityInput.Source
// values expected by GrantCapability. Kept in this file so internal callers
// can reference them without going through iface.
const (
	CapabilitySourceSubscription = string(models.EntitlementSourceSubscription)
	CapabilitySourceGrant        = string(models.EntitlementSourceGrant)
	CapabilitySourceTrial        = string(models.EntitlementSourceTrial)
)
