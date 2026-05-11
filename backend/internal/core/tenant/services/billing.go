package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/bson"
)

// UserDisplayResolver looks up the natural-person display name + email for a
// userUUID. Wired by the tenant module's Init from the registered
// ClientUserProvider so this services package stays free of a direct
// dependency on the user models. Returns ("", "", err) when the user is
// missing — callers map that to an empty fallback rather than a hard fail.
type UserDisplayResolver func(ctx context.Context, userUUID string) (fullName, email string, err error)

// SetUserDisplayResolver wires the post-init resolver used by
// EnsureTenantForUser (to seed the new tenant's Name) and by
// ResolveBillingParty (to render the FatturaPA CedentePrestatore party name
// for sole-proprietor tenants where IsCompany=false). Optional — when nil,
// EnsureTenantForUser falls back to a generic name and ResolveBillingParty
// returns whatever LegalName is on the tenant row.
func (s *Service) SetUserDisplayResolver(fn UserDisplayResolver) { s.userDisplay = fn }

// ErrItalianBillableMissingProfile signals that SetItalianBillable was asked
// to flip the toggle on without a FatturaPA profile carrying at least one
// routing handle. Mapped to 422 by the handler so the operator knows to fill
// the billing-identity form before retrying.
var ErrItalianBillableMissingProfile = errors.New("tenant: cannot enable Italian billable mode without a FatturaPA profile (CodiceDestinatario or PECDestinatario required)")

// SetItalianBillable toggles Tenant.IsItalianBillable. When enabling
// (on=true), the tenant must already carry a FatturaPA profile with at least
// one routing handle — otherwise SDI would have nowhere to deliver the
// invoice. Disabling is unconditional. The send-time path enforces the same
// invariant a second time so the toggle on its own is not load-bearing.
func (s *Service) SetItalianBillable(ctx context.Context, tenantUUID string, on bool) error {
	if strings.TrimSpace(tenantUUID) == "" {
		return errors.New("tenant: SetItalianBillable requires tenantUUID")
	}
	t, err := s.repo.GetTenantByUUID(ctx, tenantUUID)
	if err != nil {
		return err
	}
	if on && !t.FatturaPA.HasRouting() {
		return ErrItalianBillableMissingProfile
	}
	if err := s.repo.UpdateTenant(ctx, tenantUUID, bson.M{"isItalianBillable": on}); err != nil {
		return err
	}
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		TenantKind:   string(t.Kind),
		Action:       "tenant.billing.italian_billable_changed",
		ResourceType: "tenant",
		ResourceID:   tenantUUID,
		Metadata:     map[string]any{"isItalianBillable": on},
	})
	return nil
}

// SetBillingIdentityInput is the cross-module write payload for the
// admin-only PATCH /v1/admin/clients/{tenantUUID}/billing-identity. All
// fields are optional pointers; nil means "leave the existing value".
// FatturaPA is wholesale-replaced when non-nil so callers don't have to
// merge sub-document fields field-by-field.
type SetBillingIdentityInput struct {
	IsCompany      *bool
	LegalName      *string
	VATNumber      *string
	FiscalCode     *string
	BillingAddress *models.TenantAddress
	FatturaPA      *models.FatturaPAProfile
}

// SetBillingIdentity persists the billing-identity sub-document of a tenant.
// Used by the operator-admin form on /admin/clients/:tenantUUID. Intentionally
// does NOT toggle IsItalianBillable — that flip lives behind its own
// validating endpoint so the UI affordance and the data model stay aligned.
func (s *Service) SetBillingIdentity(ctx context.Context, tenantUUID string, in SetBillingIdentityInput) error {
	if strings.TrimSpace(tenantUUID) == "" {
		return errors.New("tenant: SetBillingIdentity requires tenantUUID")
	}
	if _, err := s.repo.GetTenantByUUID(ctx, tenantUUID); err != nil {
		return err
	}
	update := bson.M{}
	if in.IsCompany != nil {
		update["isCompany"] = *in.IsCompany
	}
	if in.LegalName != nil {
		update["legalName"] = strings.TrimSpace(*in.LegalName)
	}
	if in.VATNumber != nil {
		update["vatNumber"] = strings.TrimSpace(*in.VATNumber)
	}
	if in.FiscalCode != nil {
		update["fiscalCode"] = strings.TrimSpace(*in.FiscalCode)
	}
	if in.BillingAddress != nil {
		update["billingAddress"] = *in.BillingAddress
	}
	if in.FatturaPA != nil {
		update["fatturaPA"] = *in.FatturaPA
	}
	if len(update) == 0 {
		return nil
	}
	return s.repo.UpdateTenant(ctx, tenantUUID, update)
}

// ResolveBillingParty walks up Tenant.ParentTenantUUID until it finds a
// tenant whose IsItalianBillable=true AND FatturaPA carries a routing handle.
// Subscriptions stay attached to the consuming workspace; invoicing rolls up
// to the legal entity. Returns iface.ErrBillingPartyNotConfigured when the
// chain bottoms out with no match.
//
// For sole-proprietor tenants (IsCompany=false) the LegalName on the
// returned BillingParty falls back to the owner User's FullName via the
// wired UserDisplayResolver. When the resolver is nil (tests, dev path),
// LegalName is whatever the tenant row carries — usually empty for
// natural persons, which the billing send path will reject downstream.
//
// Cycle protection: the loop breaks after walking 32 ancestors. The closure
// table guarantees an acyclic hierarchy, so this is a defense-in-depth
// guard against a future bug introducing a cycle.
func (s *Service) ResolveBillingParty(ctx context.Context, tenantUUID string) (*iface.BillingParty, error) {
	if strings.TrimSpace(tenantUUID) == "" {
		return nil, errors.New("tenant: ResolveBillingParty requires tenantUUID")
	}
	t, err := walkBillingChain(tenantUUID, func(uuid string) (*models.Tenant, error) {
		return s.repo.GetTenantByUUID(ctx, uuid)
	})
	if err != nil {
		return nil, err
	}
	return s.buildBillingParty(ctx, t), nil
}

// walkBillingChain is the parent-walk core of ResolveBillingParty, factored
// out so unit tests can inject a tenant-by-UUID fetcher without touching
// Mongo. Returns the first tenant in the chain that satisfies the
// "billable" predicate (IsItalianBillable=true AND FatturaPA has a routing
// handle), or iface.ErrBillingPartyNotConfigured when the chain bottoms
// out. A repository.ErrNotFound encountered after the first hop is
// translated to ErrBillingPartyNotConfigured — a missing parent means the
// chain is broken, not that a different error happened.
func walkBillingChain(startUUID string, fetch func(uuid string) (*models.Tenant, error)) (*models.Tenant, error) {
	current := startUUID
	for hops := 0; hops < 32; hops++ {
		t, err := fetch(current)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) && hops > 0 {
				return nil, iface.ErrBillingPartyNotConfigured
			}
			return nil, err
		}
		if t.IsItalianBillable && t.FatturaPA.HasRouting() {
			return t, nil
		}
		if t.ParentTenantUUID == nil || strings.TrimSpace(*t.ParentTenantUUID) == "" {
			return nil, iface.ErrBillingPartyNotConfigured
		}
		current = *t.ParentTenantUUID
	}
	return nil, fmt.Errorf("tenant: ResolveBillingParty exceeded 32 ancestor hops starting at %s", startUUID)
}

// buildBillingParty produces the snapshot DTO. For natural-person tenants
// (IsCompany=false) it asks the wired user-display resolver for the owner's
// FullName when LegalName is empty.
func (s *Service) buildBillingParty(ctx context.Context, t *models.Tenant) *iface.BillingParty {
	bp := &iface.BillingParty{
		TenantUUID: t.UUID,
		LegalName:  t.LegalName,
		IsCompany:  t.IsCompany,
		VATNumber:  t.VATNumber,
		FiscalCode: t.FiscalCode,
		Country:    t.BillingAddress.Country,
		Email:      t.PrimaryContact.Email,
		Address: iface.BillingAddress{
			Line1:      t.BillingAddress.Line1,
			Line2:      t.BillingAddress.Line2,
			City:       t.BillingAddress.City,
			Province:   t.BillingAddress.Province,
			PostalCode: t.BillingAddress.PostalCode,
			Country:    t.BillingAddress.Country,
		},
	}
	if t.FatturaPA != nil {
		bp.FatturaPA = iface.FatturaPAProfile{
			CodiceDestinatario: t.FatturaPA.CodiceDestinatario,
			PECDestinatario:    t.FatturaPA.PECDestinatario,
			IsPA:               t.FatturaPA.IsPA,
			CodiceUfficio:      t.FatturaPA.CodiceUfficio,
			RiferimentoAmm:     t.FatturaPA.RiferimentoAmm,
			ConvenzioneNumero:  t.FatturaPA.ConvenzioneNumero,
		}
	}
	if !t.IsCompany && bp.LegalName == "" && s.userDisplay != nil && t.OwnerUserUUID != "" {
		if name, email, err := s.userDisplay(ctx, t.OwnerUserUUID); err == nil {
			if name != "" {
				bp.LegalName = name
			}
			if bp.Email == "" && email != "" {
				bp.Email = email
			}
		}
	}
	return bp
}

// EnsureTenantForUser is the lazy-provisioning seam introduced by Phase 1 of
// the Unified Client Aggregate refactor. It returns the user's existing
// personal tenant — uniquely identified by
// (Kind=external, IsCompany=false, SignupChannel=self_serve, OwnerUserUUID=userUUID)
// — or creates one when none exists. The new tenant's Name is the User's
// FullName via the wired UserDisplayResolver; when no resolver is wired or
// the user lookup fails, the name falls back to "Personal Workspace".
//
// Returned shape is iface.Tenant (the cross-module DTO) so the method
// satisfies iface.TenantProvider. Callers that need the concrete model
// re-fetch via GetTenantModel.
//
// Idempotent at the service level: a second call for the same user returns
// the existing tenant. Two concurrent calls may both pass the lookup and
// both attempt to create — the slug-uniqueness index breaks the tie, the
// loser re-reads the personal-tenant predicate and returns the winner's row.
func (s *Service) EnsureTenantForUser(ctx context.Context, userUUID string) (*iface.Tenant, error) {
	t, err := s.ensureTenantForUserModel(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	return tenantToIface(t), nil
}

// ensureTenantForUserModel is the concrete-model variant of
// EnsureTenantForUser. Kept unexported because callers that need the model
// can use GetTenantModel after EnsureTenantForUser; this helper exists
// solely to keep the main path uncluttered.
func (s *Service) ensureTenantForUserModel(ctx context.Context, userUUID string) (*models.Tenant, error) {
	if strings.TrimSpace(userUUID) == "" {
		return nil, errors.New("tenant: EnsureTenantForUser requires userUUID")
	}
	if existing, err := s.findPersonalTenant(ctx, userUUID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	name, _ := s.resolveUserDisplay(ctx, userUUID)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Personal Workspace"
	}
	slug := slugify(name)
	if slug == "" {
		slug = "personal-" + uuid.Must(uuid.NewV7()).String()[:8]
	}
	// Reserve a unique slug — personal-tenant names collide constantly
	// (every "Mario Rossi" produces the same slug), so we fall through
	// to a userUUID-suffixed variant on first conflict instead of
	// surfacing the slug error to the caller.
	if existing, _ := s.repo.GetTenantBySlug(ctx, slug); existing != nil {
		slug = slug + "-" + userUUID[:8]
	}

	input := models.CreateTenantInput{
		Name: name,
		Slug: slug,
		Kind: models.TenantKindExternal,
	}
	t, err := s.CreateTenant(ctx, userUUID, input)
	if err != nil {
		// Concurrent caller may have raced past us; re-read the predicate.
		if existing, lookupErr := s.findPersonalTenant(ctx, userUUID); lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	// CreateTenant stamped SignupChannel=sales_assisted (the default for
	// kind=external). Personal tenants are self-serve — overwrite both
	// fields so the personal-tenant predicate matches on subsequent
	// EnsureTenantForUser calls.
	if err := s.repo.UpdateTenant(ctx, t.UUID, bson.M{
		"signupChannel": models.SignupChannelSelfServe,
		"isCompany":     false,
	}); err != nil {
		return nil, err
	}
	t.SignupChannel = models.SignupChannelSelfServe
	t.IsCompany = false
	return t, nil
}

// findPersonalTenant returns the tenant matching the canonical personal-tenant
// predicate, or (nil, nil) when none exists. Wraps the repository's
// FindPersonalTenant so callers can branch on "no row" without importing
// the repository's ErrNotFound sentinel.
func (s *Service) findPersonalTenant(ctx context.Context, userUUID string) (*models.Tenant, error) {
	t, err := s.repo.FindPersonalTenant(ctx, userUUID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// resolveUserDisplay calls the wired resolver, returning empty strings when
// the resolver is absent or errors. EnsureTenantForUser tolerates either.
func (s *Service) resolveUserDisplay(ctx context.Context, userUUID string) (fullName, email string) {
	if s.userDisplay == nil {
		return "", ""
	}
	name, addr, err := s.userDisplay(ctx, userUUID)
	if err != nil {
		return "", ""
	}
	return name, addr
}
