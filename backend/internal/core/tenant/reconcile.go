package tenant

// Versioned boot reconciliation — the upgrade path an existing installation
// takes the first time it boots this code. See CLAUDE.md#boot-reconciliation
// for the contract and the rule for bumping the version below.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// setupReconciliationVersion is bumped ONLY when the reconciliation work
// itself changes, so an installation that already completed the previous
// version re-runs the new one. v1: Tier-1 `open` → `manual` rewrite +
// platform-default assignment + setup-coordinator creation.
const setupReconciliationVersion = 1

// reconcileLeaseTTL bounds how long a crashed replica can hold the
// reconcile lease before another boot may take it over. It only has to
// outlast the reconciliation itself (a handful of small reads plus at most
// three writes), not any user-facing operation.
const reconcileLeaseTTL = 60 * time.Second

// reconcileWaitInterval is how long a replica that lost the lease race
// waits before re-reading the version. The winner normally finishes in
// well under one interval; the loser then observes the completed version
// and returns.
const reconcileWaitInterval = 2 * time.Second

const provisioningInternalModeKey = "provisioning.internal.mode"

// userCounter is the narrow slice of iface.UserProvider that boot
// reconciliation consumes: whether the installation holds any operator
// users at all. Declared here rather than consumed as the whole
// UserProvider so the dependency this module actually has is the one it
// declares — and so a test does not have to implement forty unrelated
// methods to answer one question.
type userCounter interface {
	GetUserCount(ctx context.Context, filters *iface.UserFilters) (int64, error)
}

// errMissingSetupCoordinator / errMissingUserProvider are Init failures.
// Missing required wiring must fail module initialization loudly rather
// than degrade boot reconciliation into a silent no-op — which a stamped
// version would then hide forever.
var (
	errMissingSetupCoordinator = errors.New("tenant: setup finalization store is not registered (module.ServiceSetupFinalizationStore)")
	errMissingUserProvider     = errors.New("tenant: user provider is not registered (module.ServiceUserService)")
)

// Start runs versioned setup reconciliation.
//
// Start rather than Init because by the time it runs every core module has
// completed Init — the audit sink is wired into the tenant service, module
// configuration and collection indexes exist — while StartAll still runs
// before the HTTP listener and readiness. Tenant is a core module, so a
// failure here aborts backend startup instead of being logged and skipped:
// a replica must never come up serving traffic having silently missed the
// migration.
//
// A legitimate admin_required / tenant_required outcome is persisted state,
// not a startup failure. Only a read or write failure returns an error.
func (m *Module) Start(ctx context.Context) error {
	return m.reconcileSetupState(ctx)
}

// reconcileSetupState elects one replica to apply the current
// reconciliation version and returns once the version is complete — either
// because this replica applied it, or because another one did.
//
// The reconcile lease (reconcileLeaseOwner / reconcileLeaseUntil) is a
// SEPARATE mechanism from the setup saga's per-stage lease (leaseOwner /
// leaseUntil) on the same document. Neither side ever reads or clears the
// other's fields; they coordinate different work over different lifetimes.
func (m *Module) reconcileSetupState(ctx context.Context) error {
	for {
		rec, err := m.finalization.Get(ctx)
		if err != nil {
			return fmt.Errorf("tenant: read setup coordinator: %w", err)
		}
		if rec != nil && rec.ReconciliationVersion >= setupReconciliationVersion {
			return nil
		}

		if rec == nil {
			// Record-absent rule. ClaimReconcileLease is a CAS on an
			// EXISTING document, so a record has to exist before any
			// replica can be elected.
			pristine, err := m.ensureReconcileCoordinator(ctx)
			if err != nil {
				return err
			}
			if pristine {
				// Nothing ran but the config rewrite, which is idempotent
				// and safe for concurrent replicas without coordination.
				// No record is created and NO version is stamped: stamping
				// one for work never performed would make the real
				// migration be skipped forever on the boot where it
				// finally matters. The next boot re-runs this check, which
				// costs one read.
				return nil
			}
			// The record now exists; loop back so exactly one replica
			// performs the reconciliation writes under the reconcile lease.
			continue
		}

		owner := uuid.NewString()
		ok, err := m.finalization.ClaimReconcileLease(ctx, setupReconciliationVersion, owner, time.Now().Add(reconcileLeaseTTL))
		if err != nil {
			return fmt.Errorf("tenant: claim reconcile lease: %w", err)
		}
		if !ok {
			// Another replica is reconciling. Wait for it to publish the
			// version, or for its lease to expire so this replica can take
			// over, then re-check.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(reconcileWaitInterval):
			}
			continue
		}

		pristine, err := m.runReconciliationV1(ctx)
		if err != nil {
			// Startup aborts. The lease expires on its own so the next
			// boot — this replica's or another's — can retry.
			return err
		}
		if pristine {
			return nil
		}
		if _, err := m.finalization.FinishReconcile(ctx, setupReconciliationVersion, owner); err != nil {
			return fmt.Errorf("tenant: finish reconcile: %w", err)
		}
		return nil
	}
}

// ensureReconcileCoordinator resolves the record-absent case, and is the
// one part of reconciliation that necessarily runs WITHOUT the lease —
// there is no document to hold a lease on yet. It is therefore restricted
// to reads plus a single insert-only EnsureRecord, so any number of
// replicas racing through it converge on one record without any of them
// performing a reconciliation write. The writes all happen afterwards,
// under the lease, in runReconciliationV1.
//
// It returns pristine=true for a database with no operator users and no
// tenant rows at all: a brand-new install that has not run setup yet. Only
// the config rewrite applies there, no record is created (the fresh path
// binds one through InitializeFresh once the initial administrator exists),
// and the caller stamps no version.
func (m *Module) ensureReconcileCoordinator(ctx context.Context) (bool, error) {
	pristine, err := m.installationIsPristine(ctx)
	if err != nil {
		return false, err
	}
	if pristine {
		if err := m.applyInternalOpenModeMigration(ctx); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, m.createCoordinatorRecord(ctx)
}

// installationIsPristine reports whether the database holds no operator
// users AND no tenant rows in any lifecycle state. Both halves matter: an
// installation whose only tenants are archived still has a history, and
// classifying it as fresh would skip the migration it needs.
func (m *Module) installationIsPristine(ctx context.Context) (bool, error) {
	userCount, err := m.users.GetUserCount(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("tenant: count operator users for reconciliation: %w", err)
	}
	if userCount > 0 {
		return false, nil
	}
	hasTenant, err := m.svc.HasAnyTenant(ctx)
	if err != nil {
		return false, err
	}
	return !hasTenant, nil
}

// runReconciliationV1 performs the version-1 upgrade work. Every step is
// idempotent, so a replica that took over an expired lease repeats them
// harmlessly.
//
// It returns pristine=true when the installation holds no operator users
// AND no tenant rows at all, so the caller stamps NO version. Reaching that
// under the lease means a record survived while the rest of the database
// was emptied; the check is kept here rather than assumed away because
// stamping a version for work never performed is the one failure mode this
// whole signature exists to prevent.
//
// It never creates, renames, archives or purges a tenant, and never touches
// a membership. It reconciles pointers and configuration only.
func (m *Module) runReconciliationV1(ctx context.Context) (bool, error) {
	// (a) Rewrite every persisted Tier-1 `open` provisioning mode.
	if err := m.applyInternalOpenModeMigration(ctx); err != nil {
		return false, err
	}

	// The config rewrite above is the only step a pristine database needs,
	// and it is safe to have run unconditionally.
	pristine, err := m.installationIsPristine(ctx)
	if err != nil {
		return false, err
	}
	if pristine {
		return true, nil
	}

	// (b) Assign the platform default when none is assigned. An existing
	// pointer is left exactly as it is — moving an established default is
	// an explicit administrative transfer, never a migration side effect.
	assigned, err := m.svc.TenantDefaultAssigned(ctx)
	if err != nil {
		return false, err
	}
	if !assigned {
		winner, err := m.svc.OldestOperationalTenant(ctx, models.TenantKindInternal)
		if err != nil {
			return false, err
		}
		if winner != nil {
			// Empty actor: an unattended migration audits with ActorType
			// "system" and an empty actor UUID, and the pointer row records
			// its automated origin through updateSource while carrying no
			// updatedBy at all.
			if err := m.svc.AssignDefaultTenant(ctx, winner.UUID, "", models.DefaultUpdateSourceMigration); err != nil {
				return false, fmt.Errorf("tenant: assign migration default: %w", err)
			}
		}
	}

	// (c) Create the setup coordinator when it is missing, so the setup
	// phase is coherent. Under the lease a record necessarily exists —
	// reconcileSetupState created one before any replica could be elected,
	// and nothing ever deletes setup_finalization — so this is a defensive
	// re-check that keeps the step's contract true from any entry point.
	// An EXISTING record is left alone whatever state it is in: the
	// finalizer-access recovery rules govern its claim, not this migration.
	rec, err := m.finalization.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("tenant: read setup coordinator: %w", err)
	}
	if rec == nil {
		if err := m.createCoordinatorRecord(ctx); err != nil {
			return false, err
		}
	}
	return false, nil
}

// createCoordinatorRecord inserts the missing setup coordinator with the
// shape the installation's state calls for: a migration record already
// marked complete when a default can be assigned without interaction, or a
// legacy_recovery record with an empty administrator binding when no
// operational Tier-1 tenant exists — one an active super_admin claims
// later. EnsureRecord is insert-only, so concurrent callers converge on
// one record and an existing one is never clobbered.
func (m *Module) createCoordinatorRecord(ctx context.Context) error {
	snapshot, err := m.completedSetupSnapshot(ctx)
	if err != nil {
		return err
	}
	if snapshot == nil {
		if _, err := m.finalization.EnsureRecord(ctx, systeminit.SourceLegacyRecovery, nil); err != nil {
			return fmt.Errorf("tenant: ensure legacy-recovery coordinator: %w", err)
		}
		return nil
	}
	// The mode recorded here is the RESOLVED Tier-1 mode, which already
	// normalises a stored legacy `open` to `manual` — the same value the
	// rewrite persists.
	result := &systeminit.FinalizationResult{
		TenantUUID: snapshot.UUID,
		TenantName: snapshot.Name,
		TenantSlug: snapshot.Slug,
		Mode:       m.svc.ProvisioningMode(ctx, models.TenantKindInternal),
	}
	if _, err := m.finalization.EnsureRecord(ctx, systeminit.SourceMigration, result); err != nil {
		return fmt.Errorf("tenant: ensure migration coordinator: %w", err)
	}
	return nil
}

// completedSetupSnapshot resolves the tenant a migration-sourced,
// already-complete coordinator record should name, or nil when the
// installation has no operational Tier-1 tenant and therefore needs
// interactive recovery.
//
// The default pointer wins when it names an operational tenant. It can
// legitimately name one that is NOT operational (suspended, archived,
// soft-deleted); that installation is not setup-complete through the
// pointer, so the oldest operational tenant answers instead — and when
// there is none, the caller records a recovery state.
func (m *Module) completedSetupSnapshot(ctx context.Context) (*models.Tenant, error) {
	current, err := m.svc.DefaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	if current != nil && current.IsActive() {
		return current, nil
	}
	return m.svc.OldestOperationalTenant(ctx, models.TenantKindInternal)
}

// applyInternalOpenModeMigration is reconciliation step (a): the Tier-1
// `open` → `manual` rewrite plus its one-time audit event. The event is
// emitted only when something was actually rewritten, which is what makes
// it one event per installation rather than one per boot — after the
// rewrite no `open` remains for a later run to find.
func (m *Module) applyInternalOpenModeMigration(ctx context.Context) error {
	rewritten, err := m.migrateInternalOpenMode(ctx)
	if err != nil {
		return err
	}
	if rewritten {
		m.svc.AuditInternalOpenModeMigrated(ctx)
	}
	return nil
}

// migrateInternalOpenMode rewrites the removed Tier-1 `open` value to
// `manual` wherever it is stored — the legacy top-level config AND every
// environment profile, active or not, because switching profiles would
// otherwise re-introduce a value the Tier-1 schema no longer offers. Tier-2
// `open` is a valid, supported mode and is never touched.
//
// It reports whether anything was actually rewritten, which is what makes
// the audit event fire once per installation instead of once per boot. Both
// write paths run the module's own provisioning-policy validator, and
// `manual` always passes it, so the rewrite can never be rejected.
//
// A legacy installation configured as `single` is deliberately NOT
// touched, even when it holds several occupied Tier-1 provisioning slots
// and is therefore blocked from further Tier-1 creation. That is a
// remediation state for an administrator to resolve — archive or delete
// slot occupants until one remains, or explicitly choose `manual` — and
// silently loosening it here would discard operator intent.
func (m *Module) migrateInternalOpenMode(ctx context.Context) (bool, error) {
	if m.configService == nil {
		return false, nil
	}
	doc, err := m.configService.GetConfig(ctx, m.Name())
	if err != nil {
		return false, fmt.Errorf("tenant: read module config for the Tier-1 provisioning-mode migration: %w", err)
	}
	if doc == nil {
		return false, nil
	}

	manual := map[string]string{provisioningInternalModeKey: models.ProvisioningModeManual}
	rewritten := false

	if isLegacyOpenMode(doc.ConfigValues[provisioningInternalModeKey]) {
		if err := m.configService.UpdateConfig(ctx, m.Name(), manual, nil); err != nil {
			return false, fmt.Errorf("tenant: rewrite the legacy Tier-1 provisioning mode: %w", err)
		}
		rewritten = true
	}
	for _, env := range doc.AvailableEnvironments() {
		if !isLegacyOpenMode(doc.Environments[env].ConfigValues[provisioningInternalModeKey]) {
			continue
		}
		if err := m.configService.UpdateEnvironmentConfig(ctx, m.Name(), env, manual, nil); err != nil {
			return false, fmt.Errorf("tenant: rewrite the legacy Tier-1 provisioning mode for an environment profile: %w", err)
		}
		rewritten = true
	}
	return rewritten, nil
}

func isLegacyOpenMode(value string) bool {
	return strings.TrimSpace(value) == models.ProvisioningModeOpen
}
