package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// stubTenantProvider records Grant/Revoke calls for assertion. It is NOT a
// complete TenantProvider — only the capability methods used by the syncer
// are meaningful. The other methods return zero values so compilation
// passes. Shared across tests via t.Helper idioms rather than subpackages.
type stubTenantProvider struct {
	grants  []iface.GrantCapabilityInput
	revokes []stubRevoke
	grantErr error
}

type stubRevoke struct {
	tenantUUID   string
	capabilityID string
}

func (s *stubTenantProvider) GetTenant(context.Context, string) (*iface.Tenant, error) {
	return nil, nil
}
func (s *stubTenantProvider) ListUserMemberships(context.Context, string) ([]iface.TenantMembership, error) {
	return nil, nil
}
func (s *stubTenantProvider) IsMember(context.Context, string, string) (bool, error) { return false, nil }
func (s *stubTenantProvider) HasCapability(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *stubTenantProvider) GrantCapability(_ context.Context, in iface.GrantCapabilityInput) error {
	s.grants = append(s.grants, in)
	return s.grantErr
}
func (s *stubTenantProvider) RevokeCapability(_ context.Context, tenantUUID, capabilityID string) error {
	s.revokes = append(s.revokes, stubRevoke{tenantUUID: tenantUUID, capabilityID: capabilityID})
	return nil
}
func (s *stubTenantProvider) ListCapabilityIDs(context.Context, string) ([]string, error) {
	return nil, nil
}

// stubServiceRepo satisfies repository.ServiceRepository with an in-memory map.
type stubServiceRepo struct {
	byUUID map[string]*models.Service
}

func (s *stubServiceRepo) Create(context.Context, *models.Service) error { return nil }
func (s *stubServiceRepo) GetByUUID(_ context.Context, uuid string) (*models.Service, error) {
	if svc, ok := s.byUUID[uuid]; ok {
		return svc, nil
	}
	return nil, errors.New("not found")
}
func (s *stubServiceRepo) GetByCode(context.Context, string) (*models.Service, error) { return nil, nil }
func (s *stubServiceRepo) List(context.Context, repository.ServiceFilters) ([]models.Service, error) {
	return nil, nil
}
func (s *stubServiceRepo) Update(context.Context, *models.Service) error { return nil }
func (s *stubServiceRepo) Delete(context.Context, string) error          { return nil }

// nopLogger discards output so tests stay quiet.
func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// serviceWithTier builds a Service with a single tier carrying the given
// capability IDs. The tier code is fixed as "std" so tests reference it
// without boilerplate.
func serviceWithTier(uuid string, caps ...string) *models.Service {
	return &models.Service{
		UUID: uuid,
		Code: "svc-" + uuid,
		PricingTiers: []models.PricingTier{
			{Code: "std", Capabilities: caps},
		},
	}
}

func TestSyncer_OnActivateGrantsEveryCapability(t *testing.T) {
	repo := &stubServiceRepo{byUUID: map[string]*models.Service{
		"svc-1": serviceWithTier("svc-1", "rag.query", "agents.run"),
	}}
	tp := &stubTenantProvider{}
	syncer := NewEntitlementSyncer(repo, tp, nopLogger())

	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sub := &models.Subscription{
		UUID:             "sub-1",
		TenantUUID:       "tenant-1",
		ServiceUUID:      "svc-1",
		TierCode:         "std",
		CurrentPeriodEnd: periodEnd,
	}
	syncer.OnActivate(context.Background(), sub)

	if len(tp.grants) != 2 {
		t.Fatalf("want 2 grants, got %d: %+v", len(tp.grants), tp.grants)
	}
	for _, g := range tp.grants {
		if g.TenantUUID != "tenant-1" {
			t.Errorf("grant tenant: %s", g.TenantUUID)
		}
		if g.Source != CapabilitySourceSubscription {
			t.Errorf("grant source: %s", g.Source)
		}
		if g.SourceRef != "sub-1" {
			t.Errorf("grant source ref: %s", g.SourceRef)
		}
		if g.ExpiresAt == nil || !g.ExpiresAt.Equal(periodEnd) {
			t.Errorf("grant expiry: %v want %v", g.ExpiresAt, periodEnd)
		}
	}
}

func TestSyncer_OnActivateSkipsWhenTenantMissing(t *testing.T) {
	// Legacy subscription with only ClientUUID (pre-Phase 3 migration).
	// Syncer must NOT try to grant — ClientUUID is not a valid tenant
	// identifier and a grant would fail on the tenant side.
	repo := &stubServiceRepo{byUUID: map[string]*models.Service{
		"svc-1": serviceWithTier("svc-1", "rag.query"),
	}}
	tp := &stubTenantProvider{}
	syncer := NewEntitlementSyncer(repo, tp, nopLogger())

	sub := &models.Subscription{
		UUID:        "sub-legacy",
		ClientUUID:  "client-1",
		ServiceUUID: "svc-1",
		TierCode:    "std",
	}
	syncer.OnActivate(context.Background(), sub)

	if len(tp.grants) != 0 {
		t.Fatalf("expected zero grants for legacy sub, got %d", len(tp.grants))
	}
}

func TestSyncer_OnActivateNoopWhenTierHasNoCapabilities(t *testing.T) {
	repo := &stubServiceRepo{byUUID: map[string]*models.Service{
		"svc-1": serviceWithTier("svc-1"), // empty caps
	}}
	tp := &stubTenantProvider{}
	syncer := NewEntitlementSyncer(repo, tp, nopLogger())
	syncer.OnActivate(context.Background(), &models.Subscription{
		UUID: "sub-1", TenantUUID: "tenant-1", ServiceUUID: "svc-1", TierCode: "std",
	})
	if len(tp.grants) != 0 {
		t.Fatalf("expected zero grants, got %d", len(tp.grants))
	}
}

func TestSyncer_OnDeactivateRevokesEveryCapability(t *testing.T) {
	repo := &stubServiceRepo{byUUID: map[string]*models.Service{
		"svc-1": serviceWithTier("svc-1", "rag.query", "agents.run"),
	}}
	tp := &stubTenantProvider{}
	syncer := NewEntitlementSyncer(repo, tp, nopLogger())

	syncer.OnDeactivate(context.Background(), &models.Subscription{
		UUID: "sub-1", TenantUUID: "tenant-1", ServiceUUID: "svc-1", TierCode: "std",
	})

	if len(tp.revokes) != 2 {
		t.Fatalf("want 2 revokes, got %d: %+v", len(tp.revokes), tp.revokes)
	}
	if tp.revokes[0].tenantUUID != "tenant-1" {
		t.Errorf("revoke tenant: %s", tp.revokes[0].tenantUUID)
	}
}

func TestSyncer_NilTenantProviderIsNoop(t *testing.T) {
	// Degraded mode: no tenant module registered. Syncer must not panic;
	// subscription lifecycle should continue producing its other side
	// effects unaffected.
	syncer := NewEntitlementSyncer(&stubServiceRepo{}, nil, nopLogger())
	syncer.OnActivate(context.Background(), &models.Subscription{UUID: "sub-1", TenantUUID: "tenant-1"})
	syncer.OnDeactivate(context.Background(), &models.Subscription{UUID: "sub-1", TenantUUID: "tenant-1"})
	// No assertions needed — the test passes if we didn't panic.
}

func TestSyncer_OnActivateContinuesOnIndividualGrantError(t *testing.T) {
	// A transient failure granting one capability must not block the other
	// capabilities in the same tier from landing. The logger warn path is
	// exercised but we only assert call count here.
	repo := &stubServiceRepo{byUUID: map[string]*models.Service{
		"svc-1": serviceWithTier("svc-1", "rag.query", "agents.run"),
	}}
	tp := &stubTenantProvider{grantErr: errors.New("transient")}
	syncer := NewEntitlementSyncer(repo, tp, nopLogger())

	syncer.OnActivate(context.Background(), &models.Subscription{
		UUID: "sub-1", TenantUUID: "tenant-1", ServiceUUID: "svc-1", TierCode: "std",
	})
	if len(tp.grants) != 2 {
		t.Fatalf("syncer must attempt every grant even when earlier ones fail, got %d", len(tp.grants))
	}
}
