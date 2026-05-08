package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/testkit"
)

// stubTxRepo is a minimal TransactionRepository — only List is exercised by
// the self-service endpoints.
type stubTxRepo struct {
	byTenant map[string][]models.Transaction
	calls    []repository.TransactionFilters
}

func newStubTxRepo(rows ...models.Transaction) *stubTxRepo {
	r := &stubTxRepo{byTenant: map[string][]models.Transaction{}}
	for _, t := range rows {
		r.byTenant[t.TenantUUID] = append(r.byTenant[t.TenantUUID], t)
	}
	return r
}

func (r *stubTxRepo) Create(context.Context, *models.Transaction) error { return nil }
func (r *stubTxRepo) GetByUUID(context.Context, string) (*models.Transaction, error) {
	return nil, errors.New("not implemented")
}
func (r *stubTxRepo) GetByProviderTxID(context.Context, models.ProviderName, string) (*models.Transaction, error) {
	return nil, errors.New("not implemented")
}
func (r *stubTxRepo) List(_ context.Context, f repository.TransactionFilters) ([]models.Transaction, error) {
	r.calls = append(r.calls, f)
	rows := r.byTenant[f.TenantUUID]
	out := make([]models.Transaction, 0, len(rows))
	for _, t := range rows {
		if f.SubscriptionUUID != "" && t.SubscriptionUUID != f.SubscriptionUUID {
			continue
		}
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
func (r *stubTxRepo) FindByTenant(context.Context, string) ([]models.Transaction, error) {
	return nil, nil
}
func (r *stubTxRepo) Update(context.Context, *models.Transaction) error { return nil }

// stubPMRepo backs MeListPaymentMethods.
type stubPMRepo struct {
	byTenant map[string][]models.PaymentMethod
}

func newStubPMRepo(rows ...models.PaymentMethod) *stubPMRepo {
	r := &stubPMRepo{byTenant: map[string][]models.PaymentMethod{}}
	for _, pm := range rows {
		r.byTenant[pm.TenantUUID] = append(r.byTenant[pm.TenantUUID], pm)
	}
	return r
}

func (r *stubPMRepo) Create(context.Context, *models.PaymentMethod) error { return nil }
func (r *stubPMRepo) GetByUUID(context.Context, string) (*models.PaymentMethod, error) {
	return nil, errors.New("not implemented")
}
func (r *stubPMRepo) ListByTenant(_ context.Context, tenantUUID string) ([]models.PaymentMethod, error) {
	return r.byTenant[tenantUUID], nil
}
func (r *stubPMRepo) Delete(context.Context, string) error { return nil }

// stubTenantProvider — local copy for this test package.
type stubTenantProvider struct {
	memberships    map[string][]iface.TenantMembership
	personalByUser map[string]*iface.Tenant
	ensureCalls    []string
}

func (s *stubTenantProvider) GetTenant(context.Context, string) (*iface.Tenant, error) {
	return nil, nil
}
func (s *stubTenantProvider) ListUserMemberships(_ context.Context, userUUID string) ([]iface.TenantMembership, error) {
	return s.memberships[userUUID], nil
}
func (s *stubTenantProvider) IsMember(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *stubTenantProvider) ActivateTenant(context.Context, string) error { return nil }
func (s *stubTenantProvider) SetTenantStripeCustomerID(context.Context, string, string) error {
	return nil
}
func (s *stubTenantProvider) EnsureTenantForUser(_ context.Context, userUUID string) (*iface.Tenant, error) {
	s.ensureCalls = append(s.ensureCalls, userUUID)
	if s.personalByUser == nil {
		return nil, nil
	}
	return s.personalByUser[userUUID], nil
}

func tenantTx(uuid, tenantUUID string, status models.TransactionStatus) models.Transaction {
	return models.Transaction{
		UUID:       uuid,
		TenantUUID: tenantUUID,
		Status:     status,
		CreatedAt:  time.Now(),
	}
}

func tenantPM(uuid, tenantUUID string) models.PaymentMethod {
	return models.PaymentMethod{UUID: uuid, TenantUUID: tenantUUID}
}

// newClientHandlerForReads wires a ClientHandler with just the deps the read
// paths touch. payment and planner are not exercised by MeListXxx.
func newClientHandlerForReads(tx repository.TransactionRepository, pm repository.PaymentMethodRepository, tp iface.TenantProvider) *ClientHandler {
	return NewClientHandler(nil, tx, pm, tp, nil)
}

// --- Transactions ---

func TestMeListTransactions_AnonymousReturns401(t *testing.T) {
	h := newClientHandlerForReads(newStubTxRepo(), newStubPMRepo(), &stubTenantProvider{})
	_, err := h.MeListTransactions(context.Background(), &MeListTransactionsRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 401 {
		t.Fatalf("status: %v %T", err, err)
	}
}

func TestMeListTransactions_FansOutAcrossPersonalAndOwnedTenant(t *testing.T) {
	tx := newStubTxRepo(
		tenantTx("tx-p", "tenant-personal-A", models.TxSucceeded),
		tenantTx("tx-tX", "tenant-X", models.TxSucceeded),
		tenantTx("tx-tY", "tenant-Y", models.TxSucceeded), // not owned
	)
	tp := &stubTenantProvider{
		memberships: map[string][]iface.TenantMembership{
			"user-A": {
				{TenantUUID: "tenant-X", IsOwner: true},
				{TenantUUID: "tenant-Y", IsOwner: false},
			},
		},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newClientHandlerForReads(tx, newStubPMRepo(), tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeListTransactions(ctx, &MeListTransactionsRequest{})
	if err != nil {
		t.Fatalf("MeListTransactions: %v", err)
	}
	if resp.Body.Total != 2 {
		t.Fatalf("expected 2 rows (personal + owned tenant), got %d", resp.Body.Total)
	}
	got := map[string]bool{}
	for _, r := range resp.Body.Items {
		got[r.UUID] = true
	}
	if !got["tx-p"] || !got["tx-tX"] || got["tx-tY"] {
		t.Fatalf("fan-out shape: %+v", got)
	}
}

func TestMeListTransactions_FilterUnownedTenantReturnsEmpty(t *testing.T) {
	tx := newStubTxRepo(tenantTx("tx-tZ", "tenant-Z", models.TxSucceeded))
	tp := &stubTenantProvider{
		memberships:    map[string][]iface.TenantMembership{"user-A": {{TenantUUID: "tenant-X", IsOwner: true}}},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newClientHandlerForReads(tx, newStubPMRepo(), tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeListTransactions(ctx, &MeListTransactionsRequest{TenantUUID: "tenant-Z"})
	if err != nil {
		t.Fatalf("MeListTransactions: %v", err)
	}
	if resp.Body.Total != 0 {
		t.Fatalf("expected 0 rows for unowned tenant filter, got %d", resp.Body.Total)
	}
}

func TestMeListTransactions_TenantFilterReturnsScopedRows(t *testing.T) {
	tx := newStubTxRepo(
		tenantTx("tx-p", "tenant-personal-A", models.TxSucceeded),
		tenantTx("tx-tX", "tenant-X", models.TxSucceeded),
	)
	tp := &stubTenantProvider{
		memberships:    map[string][]iface.TenantMembership{"user-A": {{TenantUUID: "tenant-X", IsOwner: true}}},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newClientHandlerForReads(tx, newStubPMRepo(), tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeListTransactions(ctx, &MeListTransactionsRequest{TenantUUID: "tenant-X"})
	if err != nil {
		t.Fatalf("MeListTransactions: %v", err)
	}
	if resp.Body.Total != 1 || resp.Body.Items[0].UUID != "tx-tX" {
		t.Fatalf("tenant filter mismatch: %+v", resp.Body.Items)
	}
}

// --- Payment methods ---

func TestMeListPaymentMethods_FansOutAcrossPersonalAndOwnedTenant(t *testing.T) {
	pm := newStubPMRepo(
		tenantPM("pm-p", "tenant-personal-A"),
		tenantPM("pm-tX", "tenant-X"),
		tenantPM("pm-tY", "tenant-Y"),
	)
	tp := &stubTenantProvider{
		memberships: map[string][]iface.TenantMembership{
			"user-A": {
				{TenantUUID: "tenant-X", IsOwner: true},
				{TenantUUID: "tenant-Y", IsOwner: false},
			},
		},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newClientHandlerForReads(newStubTxRepo(), pm, tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeListPaymentMethods(ctx, &MeListPaymentMethodsRequest{})
	if err != nil {
		t.Fatalf("MeListPaymentMethods: %v", err)
	}
	if resp.Body.Total != 2 {
		t.Fatalf("expected 2 rows, got %d (%+v)", resp.Body.Total, resp.Body.Items)
	}
	got := map[string]bool{}
	for _, r := range resp.Body.Items {
		got[r.UUID] = true
	}
	if !got["pm-p"] || !got["pm-tX"] || got["pm-tY"] {
		t.Fatalf("fan-out shape: %+v", got)
	}
}

func TestMeListPaymentMethods_NilTenantProviderReturns503(t *testing.T) {
	pm := newStubPMRepo(tenantPM("pm-p", "tenant-personal-A"))
	h := newClientHandlerForReads(newStubTxRepo(), pm, nil)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	_, err := h.MeListPaymentMethods(ctx, &MeListPaymentMethodsRequest{})
	if err == nil {
		t.Fatal("expected 503, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 503 {
		t.Fatalf("status: %v %T", err, err)
	}
}
