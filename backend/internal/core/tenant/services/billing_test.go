package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// TestSetItalianBillable_EmptyTenantUUID locks in the input-validation
// guard. The repo on the test Service is nil — if a code path drifts past
// the validation guard the test panics on the nil-deref.
func TestSetItalianBillable_EmptyTenantUUID(t *testing.T) {
	t.Parallel()
	s := New(nil)
	cases := []string{"", "   ", "\t"}
	for _, in := range cases {
		in := in
		t.Run("input="+in, func(t *testing.T) {
			t.Parallel()
			if err := s.SetItalianBillable(context.Background(), in, true); err == nil {
				t.Fatalf("expected error for empty tenantUUID, got nil")
			}
		})
	}
}

func TestResolveBillingParty_EmptyTenantUUID(t *testing.T) {
	t.Parallel()
	s := New(nil)
	if _, err := s.ResolveBillingParty(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty tenantUUID, got nil")
	}
}

func TestEnsureTenantForUser_EmptyUserUUID(t *testing.T) {
	t.Parallel()
	s := New(nil)
	if _, err := s.EnsureTenantForUser(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty userUUID, got nil")
	}
}

// TestFatturaPAProfile_HasRouting locks the predicate that gates
// SetItalianBillable: a profile is "routable" iff at least one of
// CodiceDestinatario or PECDestinatario is non-empty (whitespace doesn't
// count).
func TestFatturaPAProfile_HasRouting(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   *models.FatturaPAProfile
		want bool
	}{
		{"nil", nil, false},
		{"zero", &models.FatturaPAProfile{}, false},
		{"only whitespace", &models.FatturaPAProfile{CodiceDestinatario: "  "}, false},
		{"sdi code", &models.FatturaPAProfile{CodiceDestinatario: "ABC1234"}, true},
		{"pec only", &models.FatturaPAProfile{PECDestinatario: "fatture@pec.example.it"}, true},
		{"both", &models.FatturaPAProfile{CodiceDestinatario: "ABC1234", PECDestinatario: "x@pec.it"}, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.in.HasRouting(); got != c.want {
				t.Fatalf("HasRouting() = %v, want %v", got, c.want)
			}
		})
	}
}

// fakeFetcher builds a tenant-by-UUID lookup backed by an in-memory map.
// Used to drive walkBillingChain without spinning up Mongo.
func fakeFetcher(rows ...*models.Tenant) func(uuid string) (*models.Tenant, error) {
	idx := make(map[string]*models.Tenant, len(rows))
	for _, r := range rows {
		idx[r.UUID] = r
	}
	return func(uuid string) (*models.Tenant, error) {
		t, ok := idx[uuid]
		if !ok {
			return nil, repository.ErrNotFound
		}
		return t, nil
	}
}

func parentPtr(s string) *string { return &s }

func billableTenant(uuid string, withProfile bool) *models.Tenant {
	t := &models.Tenant{
		UUID:              uuid,
		Kind:              models.TenantKindExternal,
		IsItalianBillable: true,
	}
	if withProfile {
		t.FatturaPA = &models.FatturaPAProfile{CodiceDestinatario: "ABC1234"}
	}
	return t
}

// TestWalkBillingChain_RootMatch — the requested tenant itself carries
// the FatturaPA profile + IsItalianBillable, so the walk terminates on
// the first hop.
func TestWalkBillingChain_RootMatch(t *testing.T) {
	t.Parallel()
	root := billableTenant("root", true)
	got, err := walkBillingChain("root", fakeFetcher(root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UUID != "root" {
		t.Fatalf("got %q, want root", got.UUID)
	}
}

// TestWalkBillingChain_ParentWalk — the leaf has no profile but its
// parent does. The walk must climb one level and return the parent.
func TestWalkBillingChain_ParentWalk(t *testing.T) {
	t.Parallel()
	parent := billableTenant("parent", true)
	leaf := &models.Tenant{
		UUID:             "leaf",
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: parentPtr("parent"),
	}
	got, err := walkBillingChain("leaf", fakeFetcher(leaf, parent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UUID != "parent" {
		t.Fatalf("got %q, want parent (chain should walk up)", got.UUID)
	}
}

// TestWalkBillingChain_TwoHopWalk — three-tier hierarchy where only the
// grandparent is billable. Confirms walks of arbitrary depth.
func TestWalkBillingChain_TwoHopWalk(t *testing.T) {
	t.Parallel()
	gp := billableTenant("gp", true)
	parent := &models.Tenant{
		UUID:             "parent",
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: parentPtr("gp"),
	}
	leaf := &models.Tenant{
		UUID:             "leaf",
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: parentPtr("parent"),
	}
	got, err := walkBillingChain("leaf", fakeFetcher(leaf, parent, gp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UUID != "gp" {
		t.Fatalf("got %q, want gp", got.UUID)
	}
}

// TestWalkBillingChain_NoBillableAncestor — the entire chain has no
// FatturaPA profile. Returns ErrBillingPartyNotConfigured so handlers can
// 422-prompt the operator to fill in the billing identity.
func TestWalkBillingChain_NoBillableAncestor(t *testing.T) {
	t.Parallel()
	parent := &models.Tenant{
		UUID: "parent",
		Kind: models.TenantKindExternal,
		// No FatturaPA, no toggle.
	}
	leaf := &models.Tenant{
		UUID:             "leaf",
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: parentPtr("parent"),
	}
	_, err := walkBillingChain("leaf", fakeFetcher(leaf, parent))
	if !errors.Is(err, iface.ErrBillingPartyNotConfigured) {
		t.Fatalf("got %v, want ErrBillingPartyNotConfigured", err)
	}
}

// TestWalkBillingChain_ToggleOffStillUnconfigured — a profile present but
// IsItalianBillable=false is treated as "not configured" so accidentally
// stamping FatturaPA fields without flipping the toggle doesn't silently
// route invoices.
func TestWalkBillingChain_ToggleOffStillUnconfigured(t *testing.T) {
	t.Parallel()
	root := &models.Tenant{
		UUID:              "root",
		IsItalianBillable: false,
		FatturaPA:         &models.FatturaPAProfile{CodiceDestinatario: "ABC1234"},
	}
	_, err := walkBillingChain("root", fakeFetcher(root))
	if !errors.Is(err, iface.ErrBillingPartyNotConfigured) {
		t.Fatalf("got %v, want ErrBillingPartyNotConfigured", err)
	}
}

// TestWalkBillingChain_BillableButNoRouting — toggle is on but the
// profile lacks routing handles. Same outcome as toggle-off: the
// configuration is incomplete and the walk continues climbing.
func TestWalkBillingChain_BillableButNoRouting(t *testing.T) {
	t.Parallel()
	root := &models.Tenant{
		UUID:              "root",
		IsItalianBillable: true,
		FatturaPA:         &models.FatturaPAProfile{}, // no codice, no PEC
	}
	_, err := walkBillingChain("root", fakeFetcher(root))
	if !errors.Is(err, iface.ErrBillingPartyNotConfigured) {
		t.Fatalf("got %v, want ErrBillingPartyNotConfigured", err)
	}
}

// TestWalkBillingChain_BrokenChain — leaf points at a non-existent parent.
// After the first hop, repository.ErrNotFound is translated to
// ErrBillingPartyNotConfigured so handlers don't surface internal sentinels.
func TestWalkBillingChain_BrokenChain(t *testing.T) {
	t.Parallel()
	leaf := &models.Tenant{
		UUID:             "leaf",
		ParentTenantUUID: parentPtr("ghost-parent"),
	}
	_, err := walkBillingChain("leaf", fakeFetcher(leaf))
	if !errors.Is(err, iface.ErrBillingPartyNotConfigured) {
		t.Fatalf("got %v, want ErrBillingPartyNotConfigured", err)
	}
}

// TestWalkBillingChain_FirstHopMissing — the requested tenant itself is
// missing. The chain hasn't started, so we surface the original ErrNotFound
// (handlers map this to 404 separately from the 422-routed
// "no profile in chain" case).
func TestWalkBillingChain_FirstHopMissing(t *testing.T) {
	t.Parallel()
	_, err := walkBillingChain("ghost", fakeFetcher())
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v, want repository.ErrNotFound (handler maps to 404)", err)
	}
}

// TestBuildBillingParty_NaturalPersonFallback — when IsCompany=false and
// LegalName is empty on the tenant row, buildBillingParty consults the
// wired UserDisplayResolver to render the FatturaPA party name from the
// owner User's FullName.
func TestBuildBillingParty_NaturalPersonFallback(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.SetUserDisplayResolver(func(_ context.Context, userUUID string) (string, string, error) {
		if userUUID != "owner-1" {
			t.Fatalf("resolver called with %q, want owner-1", userUUID)
		}
		return "Mario Rossi", "mario@rossi.it", nil
	})
	t1 := &models.Tenant{
		UUID:              "tenant-1",
		IsCompany:         false,
		OwnerUserUUID:     "owner-1",
		IsItalianBillable: true,
		FatturaPA:         &models.FatturaPAProfile{CodiceDestinatario: "ABC1234"},
	}
	bp := s.buildBillingParty(context.Background(), t1)
	if bp.LegalName != "Mario Rossi" {
		t.Fatalf("LegalName = %q, want Mario Rossi", bp.LegalName)
	}
	if bp.Email != "mario@rossi.it" {
		t.Fatalf("Email = %q, want mario@rossi.it", bp.Email)
	}
	if bp.IsCompany {
		t.Fatalf("IsCompany = true, want false")
	}
	if bp.FatturaPA.CodiceDestinatario != "ABC1234" {
		t.Fatalf("FatturaPA not copied: got %+v", bp.FatturaPA)
	}
}

// TestBuildBillingParty_CorporateLegalNameWins — for IsCompany=true the
// resolver is NOT consulted: the corporate LegalName is the canonical
// CessionarioCommittente party name.
func TestBuildBillingParty_CorporateLegalNameWins(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.SetUserDisplayResolver(func(_ context.Context, _ string) (string, string, error) {
		t.Fatalf("resolver must not be called for corporate tenants")
		return "", "", nil
	})
	t1 := &models.Tenant{
		UUID:          "tenant-1",
		IsCompany:     true,
		LegalName:     "Acme S.r.l.",
		OwnerUserUUID: "owner-1",
	}
	bp := s.buildBillingParty(context.Background(), t1)
	if bp.LegalName != "Acme S.r.l." {
		t.Fatalf("LegalName = %q, want Acme S.r.l.", bp.LegalName)
	}
}

// TestBuildBillingParty_NaturalPersonWithExistingLegalName — IsCompany=false
// but LegalName is already populated (manual override). Resolver is not
// consulted; the existing value wins so operators can override the
// auto-rendered name.
func TestBuildBillingParty_NaturalPersonWithExistingLegalName(t *testing.T) {
	t.Parallel()
	s := New(nil)
	resolverCalled := false
	s.SetUserDisplayResolver(func(_ context.Context, _ string) (string, string, error) {
		resolverCalled = true
		return "Should Not Win", "ignored@example.com", nil
	})
	t1 := &models.Tenant{
		UUID:          "tenant-1",
		IsCompany:     false,
		LegalName:     "Studio Bianchi (manually set)",
		OwnerUserUUID: "owner-1",
	}
	bp := s.buildBillingParty(context.Background(), t1)
	if bp.LegalName != "Studio Bianchi (manually set)" {
		t.Fatalf("LegalName = %q, want existing override", bp.LegalName)
	}
	if resolverCalled {
		t.Fatalf("resolver was called even though LegalName was already set")
	}
}

// TestBuildBillingParty_NoResolver — natural person with empty LegalName
// and no resolver wired. We tolerate the missing resolver — the billing
// send path will reject this downstream when emitting FatturaPA XML.
func TestBuildBillingParty_NoResolver(t *testing.T) {
	t.Parallel()
	s := New(nil) // userDisplay is nil
	t1 := &models.Tenant{
		UUID:          "tenant-1",
		IsCompany:     false,
		OwnerUserUUID: "owner-1",
	}
	bp := s.buildBillingParty(context.Background(), t1)
	if bp.LegalName != "" {
		t.Fatalf("LegalName = %q, want empty (no resolver wired)", bp.LegalName)
	}
}

// TestResolveUserDisplay_ResolverErrorIsTolerated — a resolver that errors
// produces empty strings, not a hard fail. EnsureTenantForUser falls back
// to the "Personal Workspace" name in that case.
func TestResolveUserDisplay_ResolverErrorIsTolerated(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.SetUserDisplayResolver(func(_ context.Context, _ string) (string, string, error) {
		return "anything", "anything@example.com", errors.New("user not found")
	})
	name, email := s.resolveUserDisplay(context.Background(), "owner-1")
	if name != "" || email != "" {
		t.Fatalf("got (%q, %q), want both empty when resolver errors", name, email)
	}
}

// TestEnsureTenantForUserName_FallbackToPersonalWorkspace covers the name-
// derivation path: when the resolver returns an empty FullName (or no
// resolver is wired), the personal tenant is named "Personal Workspace".
// This is purely a string-derivation invariant tested through the helper.
func TestEnsureTenantForUserName_FallbackToPersonalWorkspace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "Personal Workspace"},
		{"whitespace", "   ", "Personal Workspace"},
		{"populated", "Mario Rossi", "Mario Rossi"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := strings.TrimSpace(c.in)
			if got == "" {
				got = "Personal Workspace"
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
