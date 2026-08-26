package repository

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
)

// TestCountProvisioningSlots verifies CountProvisioningSlotsByKind counts a
// tenant of the given tier as occupying a provisioning slot when
// deletedAt == nil AND status is one of provisioning/active/suspended — and
// does NOT count it when: the status is archived or purged (even with a
// legacy deletedAt left nil, which is exactly the case the old
// deletedAt-only filter got wrong), the row is soft-deleted despite an
// "active" status, or the tenant belongs to the other tier.
func TestCountProvisioningSlots(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	// Counted: internal, deletedAt nil, status in {provisioning, active, suspended}.
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusProvisioning
	})
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusActive
	})
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusSuspended
	})

	// Not counted: archived/purged free their slot even though deletedAt was
	// never set on these legacy-shaped rows — this is the whole point of the
	// rename away from a deletedAt-only filter.
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusArchived
	})
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusPurged
	})

	// Not counted: soft-deleted despite an "active" status.
	deletedAt := time.Now()
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusActive
		tn.DeletedAt = &deletedAt
	})

	// Not counted: right status, wrong tier.
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindExternal
		tn.Status = models.TenantStatusActive
	})

	got, err := r.CountProvisioningSlotsByKind(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("CountProvisioningSlotsByKind: %v", err)
	}
	if got != 3 {
		t.Fatalf("CountProvisioningSlotsByKind(internal) = %d, want 3", got)
	}
}
