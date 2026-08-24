package main

import (
	"context"
	"errors"
	"fmt"

	tenantServices "github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/internal/shared/setup"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// setupAuditSink resolves the optional platform audit sink for the setup
// service. Unlike every other setup seam it is NOT required: the sink is
// owned by the compliance module, and a nil sink simply means the two
// finalization audit events are skipped rather than failing the saga.
func setupAuditSink(reg *module.ServiceRegistry) iface.AuditSink {
	sink, ok := module.GetTyped[iface.AuditSink](reg, module.ServiceAuditSink)
	if !ok {
		return nil
	}
	return sink
}

// setupTenantAdapter is the translation layer between the setup
// finalization saga and the tenant service.
//
// It exists for one reason: internal/shared/setup must import no tenant
// package, so it cannot match tenant sentinels (ErrSlugAlreadyInUse,
// ErrProvisioningLocked, ErrSetupTenantConflict, ErrSetupTenantRemediation)
// and must not string-match their text either. cmd/server may legally
// import both, so the classification happens exactly here and crosses into
// setup as a typed *setup.SeamError whose Kind the HTTP error table maps
// to a precise status + code.
//
// Every translation wraps the original error as well (two %w verbs), so
// logs keep the real cause while errors.As still finds the SeamError.
type setupTenantAdapter struct {
	svc *tenantServices.Service
}

func newSetupTenantAdapter(svc *tenantServices.Service) *setupTenantAdapter {
	return &setupTenantAdapter{svc: svc}
}

// EnsureSetupTenant forwards to the tenant seam, including the
// coordinatorAttested flag the saga derives from the coordinator record
// (see setup.SetupTenantEnsurer and the tenant service's contract: the
// restore-a-soft-deleted-reserved-row branch is gated on it).
func (a *setupTenantAdapter) EnsureSetupTenant(ctx context.Context, tenantUUID, ownerUUID, name, slug string, coordinatorAttested bool) error {
	return a.classify(a.svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug, coordinatorAttested))
}

// AssignDefaultTenant forwards to the platform-default assignment. Its one
// classified failure is ErrDefaultAlreadyAssigned: the pointer already
// names a DIFFERENT tenant, which the saga cannot resolve on its own —
// an operator must transfer or clear it, so it surfaces as remediation.
func (a *setupTenantAdapter) AssignDefaultTenant(ctx context.Context, tenantUUID, actorUUID, source string) error {
	return a.classify(a.svc.AssignDefaultTenant(ctx, tenantUUID, actorUUID, source))
}

func (a *setupTenantAdapter) classify(err error) error {
	if err == nil {
		return nil
	}
	var kind setup.SeamErrorKind
	switch {
	case errors.Is(err, tenantServices.ErrSlugAlreadyInUse):
		kind = setup.SeamSlugConflict
	case errors.Is(err, tenantServices.ErrProvisioningLocked):
		kind = setup.SeamProvisioningLocked
	case errors.Is(err, tenantServices.ErrSetupTenantConflict):
		kind = setup.SeamIdentityConflict
	case errors.Is(err, tenantServices.ErrSetupTenantRemediation),
		errors.Is(err, tenantServices.ErrDefaultAlreadyAssigned):
		kind = setup.SeamRemediation
	default:
		// Unclassified failures (infrastructure, KMS, membership, binding)
		// pass through untouched: the setup handler answers 500 without
		// marking setup complete, and the next identical request resumes.
		return err
	}
	return fmt.Errorf("%w: %w", &setup.SeamError{Kind: kind}, err)
}

var _ setup.SetupTenantEnsurer = (*setupTenantAdapter)(nil)
