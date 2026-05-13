package migrator

import (
	"context"
	"sync"
	"testing"
)

// fakeStore is a minimal in-memory Store for unit tests.
type fakeStore struct {
	mu              sync.Mutex
	rows            []SourceRow
	tenants         map[string]*TenantSnapshot
	patched         map[string]TenantPatch
	sentinel        map[string]bool
	createdFromRow  []CreateTenantInput
	invoiceBackfill map[string]string // customerUUID -> tenantUUID
}

func newFake() *fakeStore {
	return &fakeStore{
		tenants:         map[string]*TenantSnapshot{},
		patched:         map[string]TenantPatch{},
		sentinel:        map[string]bool{},
		invoiceBackfill: map[string]string{},
	}
}

func (f *fakeStore) seedRow(r SourceRow)          { f.rows = append(f.rows, r) }
func (f *fakeStore) seedTenant(t *TenantSnapshot) { f.tenants[t.UUID] = t }

func (f *fakeStore) SourceRows(ctx context.Context) ([]SourceRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]SourceRow, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakeStore) SentinelExists(ctx context.Context, customerUUID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sentinel[customerUUID], nil
}

func (f *fakeStore) GetTenant(ctx context.Context, tenantUUID string) (*TenantSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tenants[tenantUUID]; ok {
		c := *t
		return &c, nil
	}
	return nil, nil
}

func (f *fakeStore) CreateTenantFromCustomer(ctx context.Context, in CreateTenantInput) (*TenantSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "minted-" + in.Name
	f.tenants[id] = &TenantSnapshot{
		UUID:              id,
		IsCompany:         in.IsCompany,
		LegalName:         in.LegalName,
		VATNumber:         in.VATNumber,
		FiscalCode:        in.FiscalCode,
		Email:             in.Email,
		AddressLine1:      in.Address.Line1,
		City:              in.Address.City,
		PostalCode:        in.Address.PostalCode,
		Province:          in.Address.Province,
		Country:           in.Address.Country,
		IsItalianBillable: in.FatturaPA != nil,
		HasFatturaPA:      in.FatturaPA != nil,
	}
	f.createdFromRow = append(f.createdFromRow, in)
	return f.tenants[id], nil
}

func (f *fakeStore) PatchTenant(ctx context.Context, tenantUUID string, p TenantPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patched[tenantUUID] = p
	t, ok := f.tenants[tenantUUID]
	if !ok {
		return nil
	}
	if p.LegalName != "" {
		t.LegalName = p.LegalName
	}
	if p.VATNumber != "" {
		t.VATNumber = p.VATNumber
	}
	if p.FiscalCode != "" {
		t.FiscalCode = p.FiscalCode
	}
	if p.PromoteToCompany {
		t.IsCompany = true
	}
	if p.SetItalianBillable {
		t.IsItalianBillable = true
	}
	if p.FatturaPA != nil {
		t.HasFatturaPA = true
	}
	return nil
}

func (f *fakeStore) BackfillInvoiceTenant(ctx context.Context, customerUUID, tenantUUID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invoiceBackfill[customerUUID] = tenantUUID
	return 1, nil
}

func (f *fakeStore) MarkSentinel(ctx context.Context, customerUUID, tenantUUID string, invoicesUpdated int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentinel[customerUUID] = true
	return nil
}

func TestMigrate_LinkedTenant_PatchesAndInstallsFatturaPA(t *testing.T) {
	store := newFake()
	store.seedTenant(&TenantSnapshot{
		UUID:      "tenant-1",
		IsCompany: false,
		LegalName: "",
	})
	store.seedRow(SourceRow{
		ID:                 "obj1",
		UUID:               "cust-1",
		TenantUUID:         "tenant-1",
		FiscalIDCountry:    "IT",
		FiscalIDCode:       "IT12345678901",
		IsCompany:          true,
		Denomination:       "Acme S.r.l.",
		Address:            "Via Roma",
		NumeroCivico:       "10",
		City:               "Milano",
		Province:           "MI",
		PostalCode:         "20100",
		Country:            "IT",
		Email:              "billing@acme.it",
		CodiceDestinatario: "ABC1234",
	})
	m := &Migrator{Store: store}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum.TenantsCreated != 0 {
		t.Errorf("TenantsCreated = %d, want 0 (linked tenant should reuse existing row)", sum.TenantsCreated)
	}
	if sum.TenantsPatched != 1 {
		t.Errorf("TenantsPatched = %d, want 1", sum.TenantsPatched)
	}
	patch, ok := store.patched["tenant-1"]
	if !ok {
		t.Fatal("expected tenant-1 to be patched")
	}
	if !patch.PromoteToCompany {
		t.Error("expected PromoteToCompany=true")
	}
	if !patch.SetItalianBillable {
		t.Error("expected SetItalianBillable=true")
	}
	if patch.FatturaPA == nil || patch.FatturaPA.CodiceDestinatario != "ABC1234" {
		t.Errorf("FatturaPA mismatch: %+v", patch.FatturaPA)
	}
	if patch.LegalName != "Acme S.r.l." {
		t.Errorf("LegalName = %q, want Acme S.r.l.", patch.LegalName)
	}
	if patch.VATNumber != "IT12345678901" {
		t.Errorf("VATNumber = %q", patch.VATNumber)
	}
	if patch.AddressLine1 != "Via Roma 10" {
		t.Errorf("AddressLine1 = %q", patch.AddressLine1)
	}
	if got := store.invoiceBackfill["cust-1"]; got != "tenant-1" {
		t.Errorf("invoice backfill = %q, want tenant-1", got)
	}
}

func TestMigrate_NoTenantLink_MintsTenant(t *testing.T) {
	store := newFake()
	store.seedRow(SourceRow{
		ID:                 "obj1",
		UUID:               "cust-1",
		IsCompany:          true,
		Denomination:       "Beta SpA",
		Country:            "IT",
		FiscalIDCountry:    "IT",
		FiscalIDCode:       "IT99999999999",
		CodiceDestinatario: "XYZ7777",
	})
	m := &Migrator{Store: store}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum.TenantsCreated != 1 {
		t.Errorf("TenantsCreated = %d, want 1", sum.TenantsCreated)
	}
	if len(store.createdFromRow) != 1 {
		t.Fatalf("createdFromRow len = %d", len(store.createdFromRow))
	}
	in := store.createdFromRow[0]
	if in.LegalName != "Beta SpA" {
		t.Errorf("LegalName = %q", in.LegalName)
	}
	if in.VATNumber != "IT99999999999" {
		t.Errorf("VATNumber = %q (expected IT vat copied from FiscalIDCode)", in.VATNumber)
	}
	if in.FatturaPA == nil || in.FatturaPA.CodiceDestinatario != "XYZ7777" {
		t.Errorf("FatturaPA mismatch: %+v", in.FatturaPA)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	store := newFake()
	store.seedTenant(&TenantSnapshot{UUID: "tenant-1"})
	store.seedRow(SourceRow{ID: "obj1", UUID: "cust-1", TenantUUID: "tenant-1"})

	m := &Migrator{Store: store}
	if _, err := m.Run(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Second run — should skip via sentinel.
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if sum.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 on second run", sum.Skipped)
	}
	if sum.TenantsPatched != 0 {
		t.Errorf("TenantsPatched = %d, want 0 on idempotent re-run", sum.TenantsPatched)
	}
}

func TestMigrate_DryRun_NoWrites(t *testing.T) {
	store := newFake()
	store.seedRow(SourceRow{
		ID:                 "obj1",
		UUID:               "cust-1",
		IsCompany:          true,
		Denomination:       "Acme",
		CodiceDestinatario: "AAA1111",
	})
	m := &Migrator{Store: store, DryRun: true}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum.TenantsCreated != 1 {
		t.Errorf("TenantsCreated reported = %d, want 1", sum.TenantsCreated)
	}
	if len(store.createdFromRow) != 0 {
		t.Errorf("dry-run wrote %d tenants, expected 0", len(store.createdFromRow))
	}
	if len(store.sentinel) != 0 {
		t.Errorf("dry-run stamped sentinels: %v", store.sentinel)
	}
}

func TestBuildPatch_DoesNotOverwriteNonEmpty(t *testing.T) {
	row := SourceRow{
		Denomination:    "Source Co",
		FiscalIDCode:    "ITSOURCE",
		FiscalIDCountry: "IT",
	}
	tenant := &TenantSnapshot{
		LegalName: "Existing Tenant Name",
		VATNumber: "ITEXISTING",
	}
	p := buildPatch(row, tenant)
	if p.LegalName != "" {
		t.Errorf("LegalName = %q, want empty (tenant already populated)", p.LegalName)
	}
	if p.VATNumber != "" {
		t.Errorf("VATNumber = %q, want empty (tenant already populated)", p.VATNumber)
	}
}
