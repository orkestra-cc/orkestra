// Package handlers exposes the Tier-2 self-service billing-profile
// endpoints mounted on the ADR-0003 client API surface.
package handlers

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/clientbilling/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// MeHandler bundles the /v1/me/billing-profile routes. Every handler
// derives the target user from the request JWT — there is no path-level
// override, so a caller can only read or update their own profile.
type MeHandler struct {
	customers *services.CustomerService
}

// NewMeHandler constructs the handler.
func NewMeHandler(customers *services.CustomerService) *MeHandler {
	return &MeHandler{customers: customers}
}

// BillingProfile is the wire shape returned on GET / PUT. Mirrors
// iface.UserBillingProfile minus the Stripe internal id, which is an
// implementation detail clients have no need to see.
type BillingProfile struct {
	LegalName    string `json:"legalName,omitempty"`
	FirstName    string `json:"firstName,omitempty"`
	LastName     string `json:"lastName,omitempty"`
	Email        string `json:"email,omitempty"`
	VATNumber    string `json:"vatNumber,omitempty"`
	FiscalCode   string `json:"fiscalCode,omitempty"`
	Country      string `json:"country,omitempty"`
	AddressLine1 string `json:"addressLine1,omitempty"`
	AddressLine2 string `json:"addressLine2,omitempty"`
	City         string `json:"city,omitempty"`
	PostalCode   string `json:"postalCode,omitempty"`
	Province     string `json:"province,omitempty"`
	IsCompany    bool   `json:"isCompany"`
	HasStripe    bool   `json:"hasStripe" doc:"True once a Stripe customer has been created from this profile"`
}

type GetBillingProfileResponse struct {
	Body BillingProfile
}

// GetBillingProfile returns the caller's billing profile, or an empty
// payload (200) when none has been created yet. Front-end uses the
// difference (all fields blank) to render an onboarding CTA without an
// extra round-trip.
func (h *MeHandler) GetBillingProfile(ctx context.Context, _ *struct{}) (*GetBillingProfileResponse, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	prof, err := h.customers.Get(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	resp := &GetBillingProfileResponse{}
	if prof != nil {
		resp.Body = toWire(prof)
	}
	return resp, nil
}

type PutBillingProfileRequest struct {
	Body BillingProfile
}
type PutBillingProfileResponse struct {
	Body BillingProfile
}

// PutBillingProfile creates or updates the caller's billing profile. The
// caller's user UUID is taken from the JWT — never from the body — so
// there is no path for one client to overwrite another's profile.
func (h *MeHandler) PutBillingProfile(ctx context.Context, in *PutBillingProfileRequest) (*PutBillingProfileResponse, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	prof, err := h.customers.Upsert(ctx, iface.UpsertUserBillingProfileInput{
		UserUUID:     userUUID,
		LegalName:    in.Body.LegalName,
		FirstName:    in.Body.FirstName,
		LastName:     in.Body.LastName,
		Email:        in.Body.Email,
		VATNumber:    in.Body.VATNumber,
		FiscalCode:   in.Body.FiscalCode,
		Country:      in.Body.Country,
		AddressLine1: in.Body.AddressLine1,
		AddressLine2: in.Body.AddressLine2,
		City:         in.Body.City,
		PostalCode:   in.Body.PostalCode,
		Province:     in.Body.Province,
		IsCompany:    in.Body.IsCompany,
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidProfile) {
			return nil, huma.Error400BadRequest("invalid billing profile")
		}
		return nil, err
	}
	return &PutBillingProfileResponse{Body: toWire(prof)}, nil
}

func toWire(p *iface.UserBillingProfile) BillingProfile {
	if p == nil {
		return BillingProfile{}
	}
	return BillingProfile{
		LegalName:    p.LegalName,
		FirstName:    p.FirstName,
		LastName:     p.LastName,
		Email:        p.Email,
		VATNumber:    p.VATNumber,
		FiscalCode:   p.FiscalCode,
		Country:      p.Country,
		AddressLine1: p.AddressLine1,
		AddressLine2: p.AddressLine2,
		City:         p.City,
		PostalCode:   p.PostalCode,
		Province:     p.Province,
		IsCompany:    p.IsCompany,
		HasStripe:    p.StripeCustomerID != "",
	}
}
