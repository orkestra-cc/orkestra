package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// AuditActionInternalOpenMigrated is the one-time upgrade event emitted when
// a persisted Tier-1 `open` provisioning mode is rewritten to `manual`. One
// event per installation, not one per read — see
// Module.runReconciliationV1 in ../reconcile.go.
const AuditActionInternalOpenMigrated = "tenant.provisioning.internal_open_migrated"

// OldestOperationalTenant returns the oldest OPERATIONAL tenant of a tier —
// `status == active` and `deletedAt == nil` — with the tenant UUID as a
// deterministic tie-break, or (nil, nil) when the tier holds none.
//
// (nil, nil) is a normal answer, not an error: an installation whose only
// Tier-1 rows are suspended, archived, purged or soft-deleted legitimately
// has no default candidate, and boot reconciliation records that as a
// recovery state rather than failing startup.
func (s *Service) OldestOperationalTenant(ctx context.Context, kind models.TenantKind) (*models.Tenant, error) {
	rows, err := s.repo.ListOperationalByKindOldestFirst(ctx, kind, 1)
	if err != nil {
		return nil, fmt.Errorf("tenant: list operational tenants: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// HasAnyTenant reports whether the installation holds a tenant row of any
// tier in any lifecycle state, soft-deleted rows included. It answers the
// narrow "is this database pristine" question boot reconciliation asks
// before deciding an install has never been set up.
func (s *Service) HasAnyTenant(ctx context.Context) (bool, error) {
	n, err := s.repo.CountAllTenants(ctx)
	if err != nil {
		return false, fmt.Errorf("tenant: count tenants: %w", err)
	}
	return n > 0, nil
}

// TenantDefaultAssigned reports whether the platform default pointer for
// kind=internal exists at all — the RAW pointer, not an operational-filtered
// view. Boot reconciliation must leave an established pointer alone even
// when it currently names a non-operational tenant: replacing it is an
// explicit administrative transfer, never a migration side effect.
func (s *Service) TenantDefaultAssigned(ctx context.Context) (bool, error) {
	_, err := s.repo.GetDefault(ctx, models.TenantKindInternal)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("tenant: read default pointer: %w", err)
	}
	return true, nil
}

// DefaultTenant returns the tenant the platform default pointer names,
// INCLUDING one that is no longer operational, or (nil, nil) when no
// pointer exists or the row it names is gone. Distinct from
// GetDefaultTenant, which is the narrow iface.DefaultTenantProvider surface
// and deliberately withholds a non-operational target from callers that
// would route traffic at it. This one exists so boot reconciliation can
// snapshot the existing default into the coordinator record without
// re-pointing it.
func (s *Service) DefaultTenant(ctx context.Context) (*models.Tenant, error) {
	d, err := s.repo.GetDefault(ctx, models.TenantKindInternal)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tenant: read default pointer: %w", err)
	}
	t, err := s.repo.GetTenantByUUID(ctx, d.TenantUUID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("tenant: read default tenant: %w", err)
	}
	return t, nil
}

// AuditInternalOpenModeMigrated emits the one-time
// tenant.provisioning.internal_open_migrated event. The audit sink lives on
// this service, so the emit does too — the module's boot reconciliation
// decides WHETHER a rewrite happened and calls this exactly once when it
// did.
//
// The actor is unattended by construction: ActorType "system" with an EMPTY
// ActorUserID, because the literal string "system" must never land in an
// actor-UUID field. TenantID is empty because the Tier-1 provisioning mode
// is platform-global configuration, not a per-tenant resource.
func (s *Service) AuditInternalOpenModeMigrated(ctx context.Context) {
	s.emitAudit(ctx, iface.AuditEvent{
		TenantKind:   string(models.TenantKindInternal),
		ActorType:    "system",
		ActorUserID:  "",
		Action:       AuditActionInternalOpenMigrated,
		ResourceType: "module_config",
		ResourceID:   "tenant",
		Metadata: map[string]any{
			"key":  "provisioning.internal.mode",
			"from": models.ProvisioningModeOpen,
			"to":   models.ProvisioningModeManual,
		},
	})
}
