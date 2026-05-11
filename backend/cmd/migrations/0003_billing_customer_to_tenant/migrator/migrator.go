// Package migrator holds the per-row migration logic for the Unified Client
// Aggregate Phase 5 one-shot. Decoupled from the Mongo wiring so the
// transformation can be exercised end-to-end against in-memory fakes.
//
// The migrator walks every billing_customers row and:
//
//  1. resolves the tenant the customer is bound to:
//     - tenantUUID set → load the existing tenant
//     - tenantUUID empty → mint a new admin-flagged external tenant from the
//       customer row (memberCount=0; operators attach members via the admin
//       UI later)
//  2. copies the customer's billing identity onto the tenant (LegalName,
//     IsCompany, VATNumber, FiscalCode, billing address) — additive only:
//     never overwrites a non-empty tenant value with empty
//  3. populates Tenant.FatturaPA from the customer's SDI routing fields
//     (CodiceDestinatario, PECDestinatario, IsPA, CodiceUfficio,
//     RiferimentoAmm, ConvenzioneNumero) and flips IsItalianBillable=true so
//     the new BillingTenantProvider.ResolveBillingParty path picks the row up
//  4. backfills billing_invoices.tenantUUID from the per-row customerID →
//     customer.tenantUUID lookup so the rewired invoice send path can resolve
//     a CessionarioCommittente from the tenant after Phase 5 deletes the
//     billing.Customer Go surface.
//
// Idempotency is enforced via a per-row sentinel in migrations_applied.
// A re-run after partial failure resumes from the first uncompleted row.
//
// The plan keeps the billing_customers + clientbilling_customers Mongo
// collections themselves around for one phase as a safety belt — the source
// data is still readable for forensics if a regression slips through. A
// follow-up migration drops the collections after invoice-send is verified
// working in production.
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
const MigrationName = "0003_billing_customer_to_tenant"

// SourceRow is the projection of a billing_customers document the migrator
// needs. Field names match the BSON tags so the Mongo loader can decode
// straight into this struct.
type SourceRow struct {
	ID              string `bson:"_id"`
	UUID            string `bson:"uuid"`
	TenantUUID      string `bson:"tenantUUID,omitempty"`
	FiscalIDCountry string `bson:"fiscalIdCountry,omitempty"`
	FiscalIDCode    string `bson:"fiscalIdCode,omitempty"`
	CodiceFiscale   string `bson:"codiceFiscale,omitempty"`

	IsCompany    bool   `bson:"isCompany"`
	Denomination string `bson:"denomination,omitempty"`
	Name         string `bson:"name,omitempty"`
	Surname      string `bson:"surname,omitempty"`

	Address      string `bson:"address,omitempty"`
	NumeroCivico string `bson:"numeroCivico,omitempty"`
	City         string `bson:"city,omitempty"`
	Province     string `bson:"province,omitempty"`
	PostalCode   string `bson:"postalCode,omitempty"`
	Country      string `bson:"country,omitempty"`

	Email string `bson:"email,omitempty"`
	PEC   string `bson:"pec,omitempty"`
	Phone string `bson:"phone,omitempty"`

	CodiceDestinatario string `bson:"codiceDestinatario,omitempty"`
	PECDestinatario    string `bson:"pecDestinatario,omitempty"`
	IsPA               bool   `bson:"isPA,omitempty"`
	CodiceUfficio      string `bson:"codiceUfficio,omitempty"`
	RiferimentoAmm     string `bson:"riferimentoAmm,omitempty"`
	ConvenzioneNumero  string `bson:"convenzioneNumero,omitempty"`
}

// TenantSnapshot is the subset of tenant fields the migrator inspects when
// deciding whether a copy-into-tenant write needs to happen.
type TenantSnapshot struct {
	UUID              string
	IsCompany         bool
	IsItalianBillable bool
	LegalName         string
	VATNumber         string
	FiscalCode        string
	Email             string
	AddressLine1      string
	City              string
	PostalCode        string
	Province          string
	Country           string
	HasFatturaPA      bool
}

// TenantPatch is the additive update applied on top of an existing tenant.
// Empty strings on the identity fields mean "no change" — we never overwrite
// a populated value with empty. SetFatturaPA is always honoured so a
// migration re-run still installs missing routing fields if the customer
// row carried them.
type TenantPatch struct {
	LegalName    string
	VATNumber    string
	FiscalCode   string
	Email        string
	AddressLine1 string
	City         string
	PostalCode   string
	Province     string
	Country      string

	// PromoteToCompany flips IsCompany false→true when the source customer
	// row is a corporate entity. The reverse direction is never applied —
	// once a tenant is marked as a company, an empty source IsCompany does
	// not flip it back.
	PromoteToCompany bool

	// FatturaPA carries the SDI routing snapshot. Always populated when the
	// source row has at least one routing handle (codice or PEC) so a
	// re-run still installs missing fields.
	FatturaPA *FatturaPAProfile

	// SetItalianBillable marks the tenant as billable-via-FatturaPA. Set
	// when the source customer row has at least one routing handle.
	SetItalianBillable bool
}

// FatturaPAProfile mirrors tenant.models.FatturaPAProfile for the migrator
// boundary. The Mongo store layer is responsible for translating these into
// the tenant document's BSON sub-document.
type FatturaPAProfile struct {
	CodiceDestinatario string
	PECDestinatario    string
	IsPA               bool
	CodiceUfficio      string
	RiferimentoAmm     string
	ConvenzioneNumero  string
}

// IsEmpty reports whether the patch carries no changes.
func (p TenantPatch) IsEmpty() bool {
	return p.LegalName == "" &&
		p.VATNumber == "" &&
		p.FiscalCode == "" &&
		p.Email == "" &&
		p.AddressLine1 == "" &&
		p.City == "" &&
		p.PostalCode == "" &&
		p.Province == "" &&
		p.Country == "" &&
		!p.PromoteToCompany &&
		p.FatturaPA == nil &&
		!p.SetItalianBillable
}

// RowResult summarizes a single source-row's outcome.
type RowResult struct {
	CustomerUUID    string
	TenantUUID      string
	TenantCreated   bool
	TenantPatched   bool
	InvoicesUpdated int64
	Skipped         bool
}

// Summary aggregates RowResult across the full run.
type Summary struct {
	Rows            int
	Skipped         int
	TenantsCreated  int
	TenantsPatched  int
	InvoicesUpdated int64
	DurationMS      int64
}

// Store is the cross-collection seam the migrator uses. Concrete Mongo
// implementations live in main.go; tests provide in-memory fakes.
type Store interface {
	// SourceRows streams every billing_customers row in stable order
	// (createdAt asc) so a partial run resumes deterministically.
	SourceRows(ctx context.Context) ([]SourceRow, error)
	// SentinelExists reports whether a row has already been completed.
	SentinelExists(ctx context.Context, customerUUID string) (bool, error)
	// GetTenant returns the tenant snapshot keyed by UUID, or (nil, nil)
	// when none exists.
	GetTenant(ctx context.Context, tenantUUID string) (*TenantSnapshot, error)
	// CreateTenantFromCustomer inserts a brand-new admin-flagged external
	// tenant pre-filled from the customer row. The migrator hands a fully
	// resolved name + IsCompany flag so the store stays mechanical.
	CreateTenantFromCustomer(ctx context.Context, in CreateTenantInput) (*TenantSnapshot, error)
	// PatchTenant applies the additive update on the existing tenant row.
	PatchTenant(ctx context.Context, tenantUUID string, p TenantPatch) error
	// BackfillInvoiceTenant sets billing_invoices.tenantUUID on every row
	// where customerId equals customerUUID and tenantUUID is currently
	// empty. Returns the number of invoices touched.
	BackfillInvoiceTenant(ctx context.Context, customerUUID, tenantUUID string) (int64, error)
	// MarkSentinel records successful completion of a source row.
	MarkSentinel(ctx context.Context, customerUUID, tenantUUID string, invoicesUpdated int64) error
}

// CreateTenantInput is the shape the store receives when materializing a
// brand-new tenant from a customer row. The migrator pre-resolves Name and
// IsCompany so the store layer stays free of business logic.
type CreateTenantInput struct {
	Name       string
	LegalName  string
	IsCompany  bool
	VATNumber  string
	FiscalCode string
	Email      string
	Phone      string
	Address    Address
	FatturaPA  *FatturaPAProfile
}

// Address is the migrator-side projection of the tenant's BillingAddress
// sub-document.
type Address struct {
	Line1      string
	City       string
	Province   string
	PostalCode string
	Country    string
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
// operators have a single line to grep in the runbook. A failure on one row
// is logged and surfaced; the loop continues so a single problematic row
// does not block the entire batch — operators rerun the binary after
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
				slog.String("customerUUID", row.UUID),
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
		if res.TenantPatched {
			sum.TenantsPatched++
		}
		sum.InvoicesUpdated += res.InvoicesUpdated
	}
	sum.DurationMS = time.Since(start).Milliseconds()
	return sum, firstErr
}

func (m *Migrator) migrateRow(ctx context.Context, row SourceRow) (RowResult, error) {
	res := RowResult{CustomerUUID: row.UUID}
	if strings.TrimSpace(row.UUID) == "" {
		return res, fmt.Errorf("billing_customers row %s has empty uuid", row.ID)
	}

	done, err := m.Store.SentinelExists(ctx, row.UUID)
	if err != nil {
		return res, fmt.Errorf("sentinel lookup: %w", err)
	}
	if done {
		res.Skipped = true
		m.Logger.Debug("row already migrated, skipping",
			slog.String("customerUUID", row.UUID),
			slog.String("sourceID", row.ID))
		return res, nil
	}

	var tenant *TenantSnapshot
	if strings.TrimSpace(row.TenantUUID) != "" {
		t, err := m.Store.GetTenant(ctx, row.TenantUUID)
		if err != nil {
			return res, fmt.Errorf("get tenant %s: %w", row.TenantUUID, err)
		}
		if t == nil {
			return res, fmt.Errorf("customer %s links tenantUUID %s but tenant row not found", row.UUID, row.TenantUUID)
		}
		tenant = t
	} else {
		// Customer carries no tenant link — admin-only invoice recipient.
		// Mint a fresh external tenant from the row so the unified-client
		// model can address it. The new tenant has no members; operators
		// can attach a user via the admin UI when needed.
		in := buildCreateInput(row)
		if m.DryRun {
			res.TenantCreated = true
			res.TenantUUID = "<dry-run-uuid>"
			tenant = &TenantSnapshot{UUID: res.TenantUUID, IsCompany: in.IsCompany}
			m.Logger.Info("would create tenant from customer",
				slog.String("customerUUID", row.UUID),
				slog.String("name", in.Name))
		} else {
			t, err := m.Store.CreateTenantFromCustomer(ctx, in)
			if err != nil {
				return res, fmt.Errorf("create tenant from customer: %w", err)
			}
			tenant = t
			res.TenantCreated = true
			res.TenantUUID = t.UUID
			m.Logger.Info("created tenant from customer",
				slog.String("customerUUID", row.UUID),
				slog.String("tenantUUID", t.UUID))
		}
	}
	res.TenantUUID = tenant.UUID

	patch := buildPatch(row, tenant)
	if !patch.IsEmpty() {
		if m.DryRun {
			m.Logger.Info("would patch tenant",
				slog.String("customerUUID", row.UUID),
				slog.String("tenantUUID", tenant.UUID))
		} else if err := m.Store.PatchTenant(ctx, tenant.UUID, patch); err != nil {
			return res, fmt.Errorf("patch tenant: %w", err)
		}
		res.TenantPatched = true
	}

	if m.DryRun {
		m.Logger.Info("would backfill invoice tenant",
			slog.String("customerUUID", row.UUID),
			slog.String("tenantUUID", tenant.UUID))
	} else {
		n, err := m.Store.BackfillInvoiceTenant(ctx, row.UUID, tenant.UUID)
		if err != nil {
			return res, fmt.Errorf("backfill invoice tenant: %w", err)
		}
		res.InvoicesUpdated = n
	}

	if !m.DryRun {
		if err := m.Store.MarkSentinel(ctx, row.UUID, tenant.UUID, res.InvoicesUpdated); err != nil {
			return res, fmt.Errorf("mark sentinel: %w", err)
		}
	}
	return res, nil
}

// buildCreateInput materializes the CreateTenantInput for a customer row
// that has no tenantUUID link. Name falls back gracefully so the new tenant
// is still reachable from the admin UI.
func buildCreateInput(row SourceRow) CreateTenantInput {
	name := tenantNameFromCustomer(row)
	in := CreateTenantInput{
		Name:       name,
		LegalName:  strings.TrimSpace(row.Denomination),
		IsCompany:  row.IsCompany,
		VATNumber:  pickVAT(row),
		FiscalCode: strings.TrimSpace(row.CodiceFiscale),
		Email:      strings.TrimSpace(row.Email),
		Phone:      strings.TrimSpace(row.Phone),
		Address: Address{
			Line1:      buildAddressLine(row),
			City:       strings.TrimSpace(row.City),
			Province:   strings.TrimSpace(row.Province),
			PostalCode: strings.TrimSpace(row.PostalCode),
			Country:    strings.ToUpper(strings.TrimSpace(row.Country)),
		},
	}
	if fp := buildFatturaPA(row); fp != nil {
		in.FatturaPA = fp
	}
	return in
}

// tenantNameFromCustomer returns the seed Tenant.Name. Companies use the
// Denomination; natural persons use "Name Surname"; fall back to a generic
// placeholder when both are empty.
func tenantNameFromCustomer(row SourceRow) string {
	if n := strings.TrimSpace(row.Denomination); n != "" {
		return n
	}
	first := strings.TrimSpace(row.Name)
	last := strings.TrimSpace(row.Surname)
	if first != "" || last != "" {
		return strings.TrimSpace(first + " " + last)
	}
	if id := strings.TrimSpace(row.FiscalIDCode); id != "" {
		return "Customer " + id
	}
	return "Imported Customer"
}

// pickVAT returns the customer's FiscalIDCode only when the country code is
// the canonical Italian P.IVA marker — non-IT codes go onto FiscalCode in
// the tenant aggregate so the FatturaPA path doesn't try to use a non-IT
// identifier as a VAT number.
func pickVAT(row SourceRow) string {
	id := strings.TrimSpace(row.FiscalIDCode)
	if id == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(row.FiscalIDCountry), "IT") {
		return id
	}
	return ""
}

// buildAddressLine joins the legacy single-line address with the optional
// civic number. The unified-clients tenant address is multi-line; the
// customer model carried a single line plus a separate numeroCivico field.
func buildAddressLine(row SourceRow) string {
	addr := strings.TrimSpace(row.Address)
	num := strings.TrimSpace(row.NumeroCivico)
	switch {
	case addr == "" && num == "":
		return ""
	case num == "":
		return addr
	case addr == "":
		return num
	default:
		return addr + " " + num
	}
}

// buildFatturaPA returns a profile when the customer row has at least one
// SDI routing handle. Returns nil otherwise — a customer without
// CodiceDestinatario or PECDestinatario can't drive FatturaPA delivery, so
// the tenant should not be marked as billable on its behalf.
func buildFatturaPA(row SourceRow) *FatturaPAProfile {
	codice := strings.TrimSpace(row.CodiceDestinatario)
	pec := strings.TrimSpace(row.PECDestinatario)
	if codice == "" && pec == "" {
		return nil
	}
	return &FatturaPAProfile{
		CodiceDestinatario: codice,
		PECDestinatario:    pec,
		IsPA:               row.IsPA,
		CodiceUfficio:      strings.TrimSpace(row.CodiceUfficio),
		RiferimentoAmm:     strings.TrimSpace(row.RiferimentoAmm),
		ConvenzioneNumero:  strings.TrimSpace(row.ConvenzioneNumero),
	}
}

// buildPatch produces the additive update applied to the resolved tenant.
// Identity fields are propagated only when the source has them AND the
// tenant lacks them. FatturaPA + IsItalianBillable are propagated when the
// source has a routing handle, regardless of the tenant's prior state, so
// a re-run heals partially-migrated rows.
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
	patch := TenantPatch{
		LegalName:    pick(row.Denomination, t.LegalName),
		VATNumber:    pick(pickVAT(row), t.VATNumber),
		FiscalCode:   pick(row.CodiceFiscale, t.FiscalCode),
		Email:        pick(row.Email, t.Email),
		AddressLine1: pick(buildAddressLine(row), t.AddressLine1),
		City:         pick(row.City, t.City),
		PostalCode:   pick(row.PostalCode, t.PostalCode),
		Province:     pick(row.Province, t.Province),
		Country:      pick(strings.ToUpper(row.Country), t.Country),
	}
	if row.IsCompany && !t.IsCompany {
		patch.PromoteToCompany = true
	}
	if fp := buildFatturaPA(row); fp != nil {
		patch.FatturaPA = fp
		patch.SetItalianBillable = true
	}
	return patch
}

// MintTenantUUID is exported so the Mongo Store implementation can stamp a
// fresh UUIDv7 on inserts using the same generator the migrator's tests
// can mock.
func MintTenantUUID() string { return uuid.Must(uuid.NewV7()).String() }
