// Package migrator holds the per-row migration logic for the Unified Client
// Aggregate Phase 3 one-shot. Decoupled from the Mongo wiring so the
// transformation can be exercised end-to-end against in-memory fakes.
//
// The migrator walks every clientbilling_customers row and:
//
//  1. resolves a personal Tenant matching
//     (Kind=external, IsCompany=false, SignupChannel=self_serve, OwnerUserUUID=userUUID)
//     creating one in place when none exists;
//  2. copies StripeCustomerID + billing identity (LegalName, VATNumber,
//     FiscalCode, Email, BillingAddress) from the clientbilling row onto
//     the tenant — never overwriting a non-empty tenant value with empty;
//  3. inserts a TenantMembership row for the owner if missing;
//  4. pivots every subscription / invoice / transaction / payment-method /
//     entitlement carrying ownerKind="user" + ownerUUID=userUUID over to
//     ownerKind="tenant" + ownerUUID=newTenantUUID.
//
// Idempotency is enforced via a per-row sentinel in migrations_applied.
// A re-run after partial failure resumes from the first uncompleted row.
//
// IsCompany on the source row is intentionally dropped: the personal-tenant
// predicate fixes IsCompany=false so EnsureTenantForUser can locate the row
// after migration. Operators promote a personal tenant to a company via the
// /admin/clients/:tenantUUID/billing-identity endpoint when needed.
package migrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MigrationName is the sentinel key used in migrations_applied. Hard-coded
// here so the migrator and the Mongo bootstrap stamp the same value.
const MigrationName = "0001_unify_clients"

// SourceRow is the projection of a clientbilling_customers document the
// migrator needs. Field names match the BSON tags so the Mongo loader can
// decode straight into this struct.
type SourceRow struct {
	ID               string `bson:"_id"`
	UserUUID         string `bson:"userUUID"`
	LegalName        string `bson:"legalName,omitempty"`
	FirstName        string `bson:"firstName,omitempty"`
	LastName         string `bson:"lastName,omitempty"`
	Email            string `bson:"email,omitempty"`
	VATNumber        string `bson:"vatNumber,omitempty"`
	FiscalCode       string `bson:"fiscalCode,omitempty"`
	Country          string `bson:"country,omitempty"`
	AddressLine1     string `bson:"addressLine1,omitempty"`
	AddressLine2     string `bson:"addressLine2,omitempty"`
	City             string `bson:"city,omitempty"`
	PostalCode       string `bson:"postalCode,omitempty"`
	Province         string `bson:"province,omitempty"`
	IsCompany        bool   `bson:"isCompany"`
	StripeCustomerID string `bson:"stripeCustomerID,omitempty"`
}

// TenantSnapshot is the subset of tenant fields the migrator inspects when
// deciding whether a copy-into-tenant write needs to happen. The store
// returns these on FindPersonalTenant + CreatePersonalTenant.
type TenantSnapshot struct {
	UUID             string
	LegalName        string
	VATNumber        string
	FiscalCode       string
	Email            string
	StripeCustomerID string
	AddressLine1     string
	AddressLine2     string
	City             string
	PostalCode       string
	Province         string
	Country          string
}

// TenantPatch is the additive update applied on top of an existing tenant.
// Empty strings mean "no change" — we never overwrite a populated tenant
// value with an empty one from clientbilling.
type TenantPatch struct {
	LegalName        string
	VATNumber        string
	FiscalCode       string
	Email            string
	StripeCustomerID string
	AddressLine1     string
	AddressLine2     string
	City             string
	PostalCode       string
	Province         string
	Country          string
}

// IsEmpty reports whether the patch carries no changes.
func (p TenantPatch) IsEmpty() bool {
	return p.LegalName == "" &&
		p.VATNumber == "" &&
		p.FiscalCode == "" &&
		p.Email == "" &&
		p.StripeCustomerID == "" &&
		p.AddressLine1 == "" &&
		p.AddressLine2 == "" &&
		p.City == "" &&
		p.PostalCode == "" &&
		p.Province == "" &&
		p.Country == ""
}

// PivotCounts reports per-collection pivot results so the operator can
// reconcile against pre-migration row counts.
type PivotCounts struct {
	Subscriptions  int64
	Invoices       int64
	Transactions   int64
	PaymentMethods int64
	Entitlements   int64
}

// RowResult summarizes a single source-row's outcome.
type RowResult struct {
	UserUUID          string
	TenantUUID        string
	TenantCreated     bool
	MembershipCreated bool
	Patched           bool
	Pivots            PivotCounts
	Skipped           bool // already in sentinel, no-op
}

// Summary aggregates RowResult across the full run.
type Summary struct {
	Rows           int
	Skipped        int
	TenantsCreated int
	Memberships    int
	Patched        int
	Pivots         PivotCounts
	DurationMS     int64
}

// Store is the cross-collection seam the migrator uses. Concrete Mongo
// implementations live in main.go; tests provide in-memory fakes.
type Store interface {
	// SourceRows streams every clientbilling_customers row in stable order
	// (createdAt asc) so a partial run resumes deterministically.
	SourceRows(ctx context.Context) ([]SourceRow, error)
	// SentinelExists reports whether a row has already been completed.
	SentinelExists(ctx context.Context, sourceID string) (bool, error)
	// FindPersonalTenant returns the tenant matching the
	// (Kind=external, IsCompany=false, SignupChannel=self_serve,
	// OwnerUserUUID=userUUID) predicate, or (nil, nil) when none exists.
	FindPersonalTenant(ctx context.Context, userUUID string) (*TenantSnapshot, error)
	// CreatePersonalTenant inserts a brand-new personal tenant aligned with
	// the predicate (IsCompany=false, SignupChannel=self_serve,
	// Kind=external, Status=active). Also writes the depth=0 self-row in
	// tenant_ancestors. Returns the persisted snapshot.
	CreatePersonalTenant(ctx context.Context, userUUID, name string) (*TenantSnapshot, error)
	// PatchTenant applies the additive update on the existing tenant row.
	PatchTenant(ctx context.Context, tenantUUID string, p TenantPatch) error
	// EnsureMembership inserts a (userUUID, tenantUUID) membership when
	// none exists. Returns true on insert, false when one was already
	// present.
	EnsureMembership(ctx context.Context, userUUID, tenantUUID string) (bool, error)
	// PivotOwner rewrites every (ownerKind="user", ownerUUID=userUUID) row
	// across the five owner-scoped collections to (ownerKind="tenant",
	// ownerUUID=tenantUUID). Returns per-collection counts.
	PivotOwner(ctx context.Context, userUUID, tenantUUID string) (PivotCounts, error)
	// MarkSentinel records successful completion of a source row.
	MarkSentinel(ctx context.Context, sourceID, userUUID, tenantUUID string, p PivotCounts) error
}

// Migrator orchestrates the per-row work. Wire DryRun=true to make every
// write a no-op (sentinels are skipped too) so an operator can inspect
// expected behavior before flipping the switch in prod.
type Migrator struct {
	Store  Store
	Logger *slog.Logger
	DryRun bool
}

// Run iterates every source row and migrates it. Aggregates a Summary so
// operators have a single line to grep in the runbook. A failure on one
// row is logged and surfaced; the loop continues so a single problematic
// row does not block the entire batch — operators rerun the binary after
// fixing the underlying data.
func (m *Migrator) Run(ctx context.Context) (Summary, error) {
	if m.Logger == nil {
		m.Logger = slog.Default()
	}
	start := time.Now()
	rows, err := m.Store.SourceRows(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("load source rows: %w", err)
	}

	var sum Summary
	sum.Rows = len(rows)
	var firstErr error
	for _, row := range rows {
		res, err := m.migrateRow(ctx, row)
		if err != nil {
			m.Logger.Error("row migration failed",
				slog.String("userUUID", row.UserUUID),
				slog.String("sourceID", row.ID),
				slog.String("error", err.Error()))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if res.Skipped {
			sum.Skipped++
			continue
		}
		if res.TenantCreated {
			sum.TenantsCreated++
		}
		if res.MembershipCreated {
			sum.Memberships++
		}
		if res.Patched {
			sum.Patched++
		}
		sum.Pivots.Subscriptions += res.Pivots.Subscriptions
		sum.Pivots.Invoices += res.Pivots.Invoices
		sum.Pivots.Transactions += res.Pivots.Transactions
		sum.Pivots.PaymentMethods += res.Pivots.PaymentMethods
		sum.Pivots.Entitlements += res.Pivots.Entitlements
	}
	sum.DurationMS = time.Since(start).Milliseconds()
	return sum, firstErr
}

func (m *Migrator) migrateRow(ctx context.Context, row SourceRow) (RowResult, error) {
	res := RowResult{UserUUID: row.UserUUID}
	if strings.TrimSpace(row.UserUUID) == "" {
		return res, fmt.Errorf("clientbilling row %s has empty userUUID", row.ID)
	}

	done, err := m.Store.SentinelExists(ctx, row.ID)
	if err != nil {
		return res, fmt.Errorf("sentinel lookup: %w", err)
	}
	if done {
		res.Skipped = true
		m.Logger.Debug("row already migrated, skipping",
			slog.String("userUUID", row.UserUUID),
			slog.String("sourceID", row.ID))
		return res, nil
	}

	tenant, err := m.Store.FindPersonalTenant(ctx, row.UserUUID)
	if err != nil {
		return res, fmt.Errorf("find personal tenant: %w", err)
	}
	if tenant == nil {
		name := personalTenantName(row)
		if m.DryRun {
			res.TenantCreated = true
			res.TenantUUID = "<dry-run-uuid>"
			tenant = &TenantSnapshot{UUID: res.TenantUUID}
			m.Logger.Info("would create personal tenant",
				slog.String("userUUID", row.UserUUID),
				slog.String("name", name))
		} else {
			t, err := m.Store.CreatePersonalTenant(ctx, row.UserUUID, name)
			if err != nil {
				return res, fmt.Errorf("create personal tenant: %w", err)
			}
			tenant = t
			res.TenantCreated = true
			res.TenantUUID = t.UUID
			m.Logger.Info("created personal tenant",
				slog.String("userUUID", row.UserUUID),
				slog.String("tenantUUID", t.UUID))
		}
	} else {
		res.TenantUUID = tenant.UUID
	}

	patch := buildPatch(row, tenant)
	if !patch.IsEmpty() {
		if m.DryRun {
			m.Logger.Info("would patch tenant",
				slog.String("userUUID", row.UserUUID),
				slog.String("tenantUUID", tenant.UUID))
		} else if err := m.Store.PatchTenant(ctx, tenant.UUID, patch); err != nil {
			return res, fmt.Errorf("patch tenant: %w", err)
		}
		res.Patched = true
	}

	if m.DryRun {
		res.MembershipCreated = true
		m.Logger.Info("would ensure membership",
			slog.String("userUUID", row.UserUUID),
			slog.String("tenantUUID", tenant.UUID))
	} else {
		created, err := m.Store.EnsureMembership(ctx, row.UserUUID, tenant.UUID)
		if err != nil {
			return res, fmt.Errorf("ensure membership: %w", err)
		}
		res.MembershipCreated = created
	}

	if m.DryRun {
		// Cannot count pivots without writing; report zeros so the operator
		// understands they are seeing a preview, not real data.
		m.Logger.Info("would pivot owner rows",
			slog.String("userUUID", row.UserUUID),
			slog.String("tenantUUID", tenant.UUID))
	} else {
		counts, err := m.Store.PivotOwner(ctx, row.UserUUID, tenant.UUID)
		if err != nil {
			return res, fmt.Errorf("pivot owner: %w", err)
		}
		res.Pivots = counts
	}

	if !m.DryRun {
		if err := m.Store.MarkSentinel(ctx, row.ID, row.UserUUID, tenant.UUID, res.Pivots); err != nil {
			return res, fmt.Errorf("mark sentinel: %w", err)
		}
	}
	return res, nil
}

// personalTenantName returns the seed Tenant.Name used when creating a
// fresh personal tenant. Prefer the legal/full name from the clientbilling
// row; fall back to a generic placeholder so the row is still reachable
// from the admin UI's tenants list.
func personalTenantName(row SourceRow) string {
	if n := strings.TrimSpace(row.LegalName); n != "" {
		return n
	}
	first := strings.TrimSpace(row.FirstName)
	last := strings.TrimSpace(row.LastName)
	if first != "" || last != "" {
		return strings.TrimSpace(first + " " + last)
	}
	return "Personal Workspace"
}

// buildPatch produces the additive update applied to the resolved tenant.
// Only fields that the source row carries AND the tenant currently lacks
// are propagated. StripeCustomerID is special-cased: clientbilling holds
// the canonical id (Stripe customers were minted from the user-level row),
// so a non-empty source value always wins, but only when the tenant has
// none of its own.
func buildPatch(row SourceRow, t *TenantSnapshot) TenantPatch {
	pick := func(src, dst string) string {
		s := strings.TrimSpace(src)
		if s == "" {
			return ""
		}
		if strings.TrimSpace(dst) != "" {
			return ""
		}
		return s
	}
	return TenantPatch{
		LegalName:        pick(row.LegalName, t.LegalName),
		VATNumber:        pick(row.VATNumber, t.VATNumber),
		FiscalCode:       pick(row.FiscalCode, t.FiscalCode),
		Email:            pick(row.Email, t.Email),
		StripeCustomerID: pick(row.StripeCustomerID, t.StripeCustomerID),
		AddressLine1:     pick(row.AddressLine1, t.AddressLine1),
		AddressLine2:     pick(row.AddressLine2, t.AddressLine2),
		City:             pick(row.City, t.City),
		PostalCode:       pick(row.PostalCode, t.PostalCode),
		Province:         pick(row.Province, t.Province),
		Country:          pick(row.Country, t.Country),
	}
}

// MintTenantUUID is exported so the Mongo Store implementation can stamp a
// fresh UUIDv7 on inserts using the same generator the migrator's tests
// can mock.
func MintTenantUUID() string { return uuid.Must(uuid.NewV7()).String() }
