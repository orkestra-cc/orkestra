package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/testkit"
)

// stubSubRepo is a minimal SubscriptionRepository that records every List
// filter and replays a fixture set indexed by tenant UUID.
type stubSubRepo struct {
	byTenant map[string][]models.Subscription
	calls    []repository.SubscriptionFilters
}

func newStubSubRepo(rows ...models.Subscription) *stubSubRepo {
	r := &stubSubRepo{byTenant: map[string][]models.Subscription{}}
	for _, s := range rows {
		r.byTenant[s.TenantUUID] = append(r.byTenant[s.TenantUUID], s)
	}
	return r
}

func (r *stubSubRepo) Create(context.Context, *models.Subscription) error { return nil }
func (r *stubSubRepo) GetByUUID(context.Context, string) (*models.Subscription, error) {
	return nil, errors.New("not implemented")
}
func (r *stubSubRepo) List(_ context.Context, f repository.SubscriptionFilters) ([]models.Subscription, error) {
	r.calls = append(r.calls, f)
	if f.TenantUUID == "" {
		out := []models.Subscription{}
		for _, rows := range r.byTenant {
			out = append(out, rows...)
		}
		return out, nil
	}
	rows := r.byTenant[f.TenantUUID]
	out := make([]models.Subscription, 0, len(rows))
	for _, s := range rows {
		if f.Status != "" && s.Status != f.Status {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
func (r *stubSubRepo) FindDue(context.Context, time.Time) ([]models.Subscription, error) {
	return nil, nil
}
func (r *stubSubRepo) FindByTenant(context.Context, string) ([]models.Subscription, error) {
	return nil, nil
}
func (r *stubSubRepo) UpdateStatus(context.Context, string, models.SubStatus) error { return nil }
func (r *stubSubRepo) Update(context.Context, *models.Subscription) error           { return nil }
func (r *stubSubRepo) Delete(context.Context, string) error                         { return nil }

// stubSvcRepoMin satisfies repository.ServiceRepository — MeList does not
// hit any of these methods.
type stubSvcRepoMin struct{}

func (stubSvcRepoMin) Create(context.Context, *models.Service) error              { return nil }
func (stubSvcRepoMin) GetByUUID(context.Context, string) (*models.Service, error) { return nil, nil }
func (stubSvcRepoMin) GetByCode(context.Context, string) (*models.Service, error) { return nil, nil }
func (stubSvcRepoMin) Update(context.Context, *models.Service) error              { return nil }
func (stubSvcRepoMin) Delete(context.Context, string) error                       { return nil }
func (stubSvcRepoMin) List(context.Context, repository.ServiceFilters) ([]models.Service, error) {
	return nil, nil
}

// stubTenantProvider returns a controlled membership set per userUUID and
// records EnsureTenantForUser invocations.
type stubTenantProvider struct {
	memberships    map[string][]iface.TenantMembership
	personalByUser map[string]*iface.Tenant
	ensureCalls    []string
	ensureErr      error
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
	if s.ensureErr != nil {
		return nil, s.ensureErr
	}
	if s.personalByUser == nil {
		return nil, nil
	}
	return s.personalByUser[userUUID], nil
}

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newHandlerForList wires a SubscriptionHandler with just the deps MeList
// touches — no renewal/invoice/activity surface needed.
func newHandlerForList(repo repository.SubscriptionRepository, tp iface.TenantProvider) *SubscriptionHandler {
	subs := services.NewSubscriptionService(repo, stubSvcRepoMin{}, nil, nil, tp, nopLogger())
	return NewSubscriptionHandler(subs, nil, nil, nil, tp)
}

func tenantSub(uuid, tenantUUID string, status models.SubStatus) models.Subscription {
	return models.Subscription{
		UUID:       uuid,
		TenantUUID: tenantUUID,
		Status:     status,
	}
}

func TestMeList_AnonymousReturns401(t *testing.T) {
	h := newHandlerForList(newStubSubRepo(), &stubTenantProvider{})
	_, err := h.MeList(context.Background(), &MeListSubscriptionsRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok {
		t.Fatalf("error type: %T (%v)", err, err)
	}
	if got := se.GetStatus(); got != 401 {
		t.Fatalf("status: %d, want 401", got)
	}
}

func TestMeList_PersonalTenantOnlyReturnsPersonalRows(t *testing.T) {
	repo := newStubSubRepo(
		tenantSub("p-1", "tenant-personal-A", models.SubActive),
		tenantSub("x-1", "tenant-other", models.SubActive),
	)
	tp := &stubTenantProvider{
		memberships:    map[string][]iface.TenantMembership{"user-A": nil},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newHandlerForList(repo, tp)

	id := testkit.NewIdentity("user-A", "a@example.com", "user")
	ctx := id.ContextFor(context.Background(), "-")

	resp, err := h.MeList(ctx, &MeListSubscriptionsRequest{})
	if err != nil {
		t.Fatalf("MeList: %v", err)
	}
	if resp.Body.Total != 1 || resp.Body.Items[0].UUID != "p-1" {
		t.Fatalf("expected only personal-tenant row, got %+v", resp.Body.Items)
	}
}

func TestMeList_OwnedTenantFansIn(t *testing.T) {
	repo := newStubSubRepo(
		tenantSub("p-1", "tenant-personal-A", models.SubActive),
		tenantSub("t-1", "tenant-X", models.SubActive),
		tenantSub("t-2", "tenant-Y", models.SubActive), // not owned (member but not owner)
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
	h := newHandlerForList(repo, tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeList(ctx, &MeListSubscriptionsRequest{})
	if err != nil {
		t.Fatalf("MeList: %v", err)
	}
	if resp.Body.Total != 2 {
		t.Fatalf("expected 2 rows (personal + owned tenant), got %d: %+v", resp.Body.Total, resp.Body.Items)
	}
	got := map[string]bool{}
	for _, s := range resp.Body.Items {
		got[s.UUID] = true
	}
	if !got["p-1"] || !got["t-1"] || got["t-2"] {
		t.Fatalf("unexpected fan-out shape: %+v", got)
	}
}

func TestMeList_FilterUnownedTenantReturnsEmpty(t *testing.T) {
	// Caller asks for a tenant they do not own — the handler must reply
	// with zero rows and not leak existence by erroring out.
	repo := newStubSubRepo(tenantSub("t-1", "tenant-Z", models.SubActive))
	tp := &stubTenantProvider{
		memberships:    map[string][]iface.TenantMembership{"user-A": {{TenantUUID: "tenant-X", IsOwner: true}}},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newHandlerForList(repo, tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeList(ctx, &MeListSubscriptionsRequest{TenantUUID: "tenant-Z"})
	if err != nil {
		t.Fatalf("MeList: %v", err)
	}
	if resp.Body.Total != 0 {
		t.Fatalf("expected 0 rows for unowned-tenant filter, got %d", resp.Body.Total)
	}
}

func TestMeList_StatusFilterPropagates(t *testing.T) {
	repo := newStubSubRepo(
		tenantSub("p-active", "tenant-personal-A", models.SubActive),
		tenantSub("p-cancelled", "tenant-personal-A", models.SubCancelled),
	)
	tp := &stubTenantProvider{
		memberships:    map[string][]iface.TenantMembership{"user-A": nil},
		personalByUser: map[string]*iface.Tenant{"user-A": {UUID: "tenant-personal-A"}},
	}
	h := newHandlerForList(repo, tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	resp, err := h.MeList(ctx, &MeListSubscriptionsRequest{Status: string(models.SubActive)})
	if err != nil {
		t.Fatalf("MeList: %v", err)
	}
	if resp.Body.Total != 1 || resp.Body.Items[0].UUID != "p-active" {
		t.Fatalf("status filter not honored: %+v", resp.Body.Items)
	}
	for _, c := range repo.calls {
		if c.Status != models.SubActive {
			t.Fatalf("repo.List called without status filter: %+v", c)
		}
	}
}

func TestMeList_NilTenantProviderReturns503(t *testing.T) {
	// Post-Unified-Client-Aggregate, the tenant provider is required for
	// every self-service flow because every billable principal is a tenant.
	repo := newStubSubRepo()
	h := newHandlerForList(repo, nil)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	_, err := h.MeList(ctx, &MeListSubscriptionsRequest{})
	if err == nil {
		t.Fatal("expected 503, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 503 {
		t.Fatalf("status: %v %T", err, err)
	}
}

func TestMeList_EnsureTenantForUserErrorPropagates(t *testing.T) {
	repo := newStubSubRepo()
	tp := &stubTenantProvider{
		memberships: map[string][]iface.TenantMembership{"user-A": nil},
		ensureErr:   errors.New("mongo down"),
	}
	h := newHandlerForList(repo, tp)

	ctx := testkit.NewIdentity("user-A", "a@example.com", "user").ContextFor(context.Background(), "-")
	_, err := h.MeList(ctx, &MeListSubscriptionsRequest{})
	if err == nil {
		t.Fatal("expected EnsureTenantForUser failure to surface")
	}
}
