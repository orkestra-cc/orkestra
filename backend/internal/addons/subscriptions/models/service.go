package models

import "time"

// PricingTier describes one chargeable cadence of a Service. A Service must
// have at least one tier; a subscription references a tier by Code.
type PricingTier struct {
	Code        string       `bson:"code" json:"code"`
	Cycle       BillingCycle `bson:"cycle" json:"cycle"`
	AmountCents int64        `bson:"amountCents" json:"amountCents"`
	Currency    string       `bson:"currency" json:"currency"`
}

// Service is a sellable item in the catalog (e.g. "N8N Workflow Pro",
// "Managed Postgres — 8GB").
type Service struct {
	UUID          string         `bson:"uuid" json:"uuid"`
	Code          string         `bson:"code" json:"code"`
	Name          string         `bson:"name" json:"name"`
	Category      string         `bson:"category" json:"category"`
	Description   string         `bson:"description" json:"description"`
	Active        bool           `bson:"active" json:"active"`
	PricingTiers  []PricingTier  `bson:"pricingTiers" json:"pricingTiers"`
	SetupFeeCents int64          `bson:"setupFeeCents" json:"setupFeeCents"`
	Metadata      map[string]any `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt     time.Time      `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time      `bson:"updatedAt" json:"updatedAt"`
}

// FindTier returns the matching tier by code, or nil if not present.
func (s *Service) FindTier(code string) *PricingTier {
	for i := range s.PricingTiers {
		if s.PricingTiers[i].Code == code {
			return &s.PricingTiers[i]
		}
	}
	return nil
}
