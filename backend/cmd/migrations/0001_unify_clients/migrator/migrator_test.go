package migrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeStore is a minimal in-memory implementation of Store for unit tests.
// Concurrency-safe so the migrator's loop can hand it off across goroutines
// in future variants — today the migrator is single-threaded.
type fakeStore struct {
	mu        sync.Mutex
	rows      []SourceRow
	tenants   map[string]*TenantSnapshot          // tenantUUID -> snapshot
	personal  map[string]string                   // userUUID -> tenantUUID (lookup index)
	memberKey map[string]bool                     // userUUID + "|" + tenantUUID
	owned     map[string]map[string]int           // userUUID -> collection -> rowCount
	pivots    map[string]map[string]string        // tenantUUID -> collection -> "moved"
	sentinel  map[string]bool                     // sourceID
	createIDs []string                            // tenantUUIDs we minted
	patched   map[string]TenantPatch              // tenantUUID -> last patch
	failOn    map[string]error                    // op-name -> error
}

func newFake() *fakeStore {
	return &fakeStore{
		tenants:   map[string]*TenantSnapshot{},
		personal:  map[string]string{},
		memberKey: map[string]bool{},
		owned:     map[string]map[string]int{},
		pivots:    map[string]map[string]string{},
		sentinel:  map[string]bool{},
		patched:   map[string]TenantPatch{},
		failOn:    map[string]error{},
	}
}

func (f *fakeStore) seedRow(r SourceRow) { f.rows = append(f.rows, r) }
func (f *fakeStore) seedOwnerRows(userUUID string, counts map[string]int) {
	f.owned[userUUID] = map[string]int{}
	for k, v := range counts {
		f.owned[userUUID][k] = v
	}
}

func (f *fakeStore) SourceRows(ctx context.Context) ([]SourceRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failOn["SourceRows"]; err != nil {
		return nil, err
	}
	out := make([]SourceRow, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakeStore) SentinelExists(ctx context.Context, sourceID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sentinel[sourceID], nil
}

func (f *fakeStore) FindPersonalTenant(ctx context.Context, userUUID string) (*TenantSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.personal[userUUID]; ok {
		t := *f.tenants[id]
		return &t, nil
	}
	return nil, nil
}

func (f *fakeStore) CreatePersonalTenant(ctx context.Context, userUUID, name string) (*TenantSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failOn["CreatePersonalTenant"]; err != nil {
		return nil, err
	}
	id := "tenant-" + userUUID
	f.tenants[id] = &TenantSnapshot{UUID: id}
	f.personal[userUUID] = id
	f.createIDs = append(f.createIDs, id)
	return &TenantSnapshot{UUID: id}, nil
}

func (f *fakeStore) PatchTenant(ctx context.Context, tenantUUID string, p TenantPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[tenantUUID]
	if !ok {
		return errors.New("tenant not found in fake")
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
	if p.Email != "" {
		t.Email = p.Email
	}
	if p.StripeCustomerID != "" {
		t.StripeCustomerID = p.StripeCustomerID
	}
	if p.AddressLine1 != "" {
		t.AddressLine1 = p.AddressLine1
	}
	if p.AddressLine2 != "" {
		t.AddressLine2 = p.AddressLine2
	}
	if p.City != "" {
		t.City = p.City
	}
	if p.PostalCode != "" {
		t.PostalCode = p.PostalCode
	}
	if p.Province != "" {
		t.Province = p.Province
	}
	if p.Country != "" {
		t.Country = p.Country
	}
	f.patched[tenantUUID] = p
	return nil
}

func (f *fakeStore) EnsureMembership(ctx context.Context, userUUID, tenantUUID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := userUUID + "|" + tenantUUID
	if f.memberKey[k] {
		return false, nil
	}
	f.memberKey[k] = true
	return true, nil
}

func (f *fakeStore) PivotOwner(ctx context.Context, userUUID, tenantUUID string) (PivotCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.owned[userUUID]
	c := PivotCounts{
		Subscriptions:  int64(src["subscriptions"]),
		Invoices:       int64(src["invoices"]),
		Transactions:   int64(src["transactions"]),
		PaymentMethods: int64(src["paymentMethods"]),
		Entitlements:   int64(src["entitlements"]),
	}
	if _, ok := f.pivots[tenantUUID]; !ok {
		f.pivots[tenantUUID] = map[string]string{}
	}
	for k := range src {
		f.pivots[tenantUUID][k] = "moved"
		delete(src, k)
	}
	return c, nil
}

func (f *fakeStore) MarkSentinel(ctx context.Context, sourceID, userUUID, tenantUUID string, p PivotCounts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentinel[sourceID] = true
	return nil
}

// --- tests -----------------------------------------------------------------

func TestRun_CreatesTenantAndPivotsRows(t *testing.T) {
	f := newFake()
	f.seedRow(SourceRow{
		ID:               "row-1",
		UserUUID:         "user-1",
		LegalName:        "Mario Rossi",
		Email:            "mario@example.com",
		StripeCustomerID: "cus_123",
		Country:          "IT",
	})
	f.seedOwnerRows("user-1", map[string]int{
		"subscriptions": 2,
		"invoices":      3,
		"transactions":  5,
	})

	m := &Migrator{Store: f}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Rows != 1 || sum.TenantsCreated != 1 || sum.Memberships != 1 || sum.Patched != 1 {
		t.Fatalf("summary mismatch: %+v", sum)
	}
	if sum.Pivots.Subscriptions != 2 || sum.Pivots.Invoices != 3 || sum.Pivots.Transactions != 5 {
		t.Fatalf("pivot counts wrong: %+v", sum.Pivots)
	}
	if !f.sentinel["row-1"] {
		t.Fatal("expected sentinel for row-1")
	}
}

func TestRun_Idempotent(t *testing.T) {
	f := newFake()
	f.seedRow(SourceRow{ID: "row-1", UserUUID: "user-1", LegalName: "Solo"})
	f.seedOwnerRows("user-1", map[string]int{"subscriptions": 1})

	m := &Migrator{Store: f}
	if _, err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second run sees the sentinel, treats every row as a skip.
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Skipped != 1 || sum.TenantsCreated != 0 || sum.Patched != 0 {
		t.Fatalf("re-run should skip everything, got %+v", sum)
	}
}

func TestRun_DryRunNoMutation(t *testing.T) {
	f := newFake()
	f.seedRow(SourceRow{ID: "row-1", UserUUID: "user-1", LegalName: "Dry"})
	f.seedOwnerRows("user-1", map[string]int{"subscriptions": 1})

	m := &Migrator{Store: f, DryRun: true}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Rows != 1 {
		t.Fatalf("expected 1 row, got %d", sum.Rows)
	}
	// No tenant should have been created in the fake.
	if len(f.createIDs) != 0 {
		t.Fatalf("dry-run wrote a tenant: %v", f.createIDs)
	}
	// No membership rows persisted.
	if len(f.memberKey) != 0 {
		t.Fatalf("dry-run wrote a membership: %v", f.memberKey)
	}
	// No sentinel entries.
	if len(f.sentinel) != 0 {
		t.Fatalf("dry-run wrote a sentinel: %v", f.sentinel)
	}
}

func TestRun_ResumesAfterPartialFailure(t *testing.T) {
	f := newFake()
	f.seedRow(SourceRow{ID: "row-1", UserUUID: "user-1", LegalName: "First"})
	f.seedRow(SourceRow{ID: "row-2", UserUUID: "user-2", LegalName: "Second"})
	f.seedOwnerRows("user-1", map[string]int{})
	f.seedOwnerRows("user-2", map[string]int{})

	// Pre-mark row-1 as completed (simulating a previous partial run).
	f.sentinel["row-1"] = true

	m := &Migrator{Store: f}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Skipped != 1 {
		t.Fatalf("expected 1 skip, got %d", sum.Skipped)
	}
	if sum.TenantsCreated != 1 {
		t.Fatalf("expected 1 fresh tenant for row-2, got %d", sum.TenantsCreated)
	}
	if !f.sentinel["row-2"] {
		t.Fatal("row-2 sentinel not set")
	}
}

func TestRun_ReusesExistingPersonalTenant(t *testing.T) {
	f := newFake()
	// Seed an existing personal tenant for the user — the migrator should
	// reuse it instead of creating a new one.
	f.tenants["existing-1"] = &TenantSnapshot{UUID: "existing-1", LegalName: "Old Name"}
	f.personal["user-1"] = "existing-1"
	f.seedRow(SourceRow{ID: "row-1", UserUUID: "user-1", LegalName: "New Name", VATNumber: "IT123"})
	f.seedOwnerRows("user-1", map[string]int{"subscriptions": 1})

	m := &Migrator{Store: f}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.TenantsCreated != 0 {
		t.Fatalf("expected to reuse existing tenant, got %d created", sum.TenantsCreated)
	}
	if sum.Patched != 1 {
		t.Fatalf("expected one patch, got %d", sum.Patched)
	}
	// LegalName already populated → patch must not overwrite. VATNumber was
	// empty on the tenant → patch fills it in.
	patched := f.patched["existing-1"]
	if patched.LegalName != "" {
		t.Errorf("patch overwrote legal name: %q", patched.LegalName)
	}
	if patched.VATNumber != "IT123" {
		t.Errorf("patch lost VAT: %q", patched.VATNumber)
	}
}

func TestRun_RejectsEmptyUserUUID(t *testing.T) {
	f := newFake()
	f.seedRow(SourceRow{ID: "row-bad", UserUUID: ""})

	m := &Migrator{Store: f}
	_, err := m.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for empty userUUID")
	}
	if !strings.Contains(err.Error(), "empty userUUID") {
		t.Fatalf("error did not mention userUUID: %v", err)
	}
	if len(f.sentinel) != 0 {
		t.Fatal("bad row should not stamp a sentinel")
	}
}

func TestBuildPatch_NeverOverwritesPopulatedFields(t *testing.T) {
	row := SourceRow{
		LegalName:        "row-name",
		VATNumber:        "row-vat",
		StripeCustomerID: "row-stripe",
		Email:            "row@example.com",
	}
	t1 := &TenantSnapshot{LegalName: "tenant-name", VATNumber: "tenant-vat"} // already populated
	p := buildPatch(row, t1)
	if p.LegalName != "" || p.VATNumber != "" {
		t.Errorf("populated fields were overwritten: %+v", p)
	}
	if p.StripeCustomerID != "row-stripe" || p.Email != "row@example.com" {
		t.Errorf("missing fields not propagated: %+v", p)
	}
}

func TestPersonalTenantName(t *testing.T) {
	cases := []struct {
		row  SourceRow
		want string
	}{
		{SourceRow{LegalName: "Acme SRL"}, "Acme SRL"},
		{SourceRow{FirstName: "Mario", LastName: "Rossi"}, "Mario Rossi"},
		{SourceRow{FirstName: "  Lone "}, "Lone"},
		{SourceRow{}, "Personal Workspace"},
	}
	for _, c := range cases {
		got := personalTenantName(c.row)
		if got != c.want {
			t.Errorf("personalTenantName(%+v) = %q, want %q", c.row, got, c.want)
		}
	}
}
