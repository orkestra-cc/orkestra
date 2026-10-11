package models

import "time"

const (
	AccountStatusActive       = "active"
	AccountStatusPlanDisabled = "plan_disabled"
	AccountStatusNeedsReauth  = "needs_reauth"
	AccountStatusRevoked      = "revoked"

	// ScopePlanUsage is the OpenAI scope that authorizes plan-billed
	// inference; a valid identity without it is linked but plan_disabled.
	ScopePlanUsage = "chatgpt.tokens.use.direct"
)

// RefreshTokenLifetime is the documented lifetime of a SIWC refresh token.
const RefreshTokenLifetime = 30 * 24 * time.Hour

// UserAccount is one Sign in with ChatGPT registration of one user in one
// org. A user may hold several (one per ChatGPT account/workspace, each
// with its own issued client id); exactly one is Selected per
// (tenant, user, provider).
//
// It is a storage model and never an HTTP body (it carries the sealed
// token envelopes, the client id and the OIDC subject): handlers answer
// with the LLM-prefixed view DTOs in dto.go instead, hence no json tags.
// The three token fields are independent envelopes, each sealed under its
// own field name in the AAD.
type UserAccount struct {
	UUID              string     `bson:"uuid"`
	TenantID          string     `bson:"tenantId"`
	UserUUID          string     `bson:"userUuid"`
	Provider          string     `bson:"provider"` // "openai"
	Label             string     `bson:"label"`
	Issuer            string     `bson:"issuer"`
	Subject           string     `bson:"subject"`
	Email             string     `bson:"email,omitempty"`
	ClientID          string     `bson:"clientId"`
	ExtAgentHostID    string     `bson:"extAgentHostId"`
	IDToken           Envelope   `bson:"idToken"`
	AccessToken       Envelope   `bson:"accessToken"`
	RefreshToken      Envelope   `bson:"refreshToken"`
	TokenType         string     `bson:"tokenType"`
	ExpiresAt         time.Time  `bson:"expiresAt"`
	RefreshExpiresAt  time.Time  `bson:"refreshExpiresAt"`
	EarliestRefreshAt *time.Time `bson:"earliestRefreshAt,omitempty"`
	SavedAt           time.Time  `bson:"savedAt"`
	Scopes            []string   `bson:"scopes"`
	PlanUsageGranted  bool       `bson:"planUsageGranted"`
	Selected          bool       `bson:"selected"`
	Status            string     `bson:"status"`
	LinkedAt          time.Time  `bson:"linkedAt"`
	LastUsedAt        *time.Time `bson:"lastUsedAt,omitempty"`
	LastError         string     `bson:"lastError,omitempty"` // llm.* code, never provider text
	UpdatedAt         time.Time  `bson:"updatedAt"`
}
