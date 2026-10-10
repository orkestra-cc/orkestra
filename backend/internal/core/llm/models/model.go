package models

import (
	"errors"
	"regexp"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	CredentialKindOrg         = iface.LLMCredentialKindOrg
	CredentialKindUserAccount = iface.LLMCredentialKindUserAccount

	AccessGranted  = "granted"
	AccessEveryone = "everyone"

	ModelStatusActive   = "active"
	ModelStatusDisabled = "disabled"
)

// LLMModelCapabilities is the persisted (and API-facing) capability set.
// It mirrors iface.LLMCapabilities, which is json-only, with explicit bson
// tags; convert with ToIface / CapabilitiesFromIface. The name differs from
// iface.LLMCapabilities because Huma schema names are package-less.
type LLMModelCapabilities struct {
	Chat             bool `bson:"chat" json:"chat"`
	Streaming        bool `bson:"streaming" json:"streaming"`
	Tools            bool `bson:"tools" json:"tools"`
	StructuredOutput bool `bson:"structuredOutput" json:"structuredOutput"`
	Embeddings       bool `bson:"embeddings" json:"embeddings"`
	Dimensions       int  `bson:"dimensions,omitempty" json:"dimensions,omitempty"`
}

// ToIface converts to the consumer-facing contract type.
func (c LLMModelCapabilities) ToIface() iface.LLMCapabilities {
	return iface.LLMCapabilities{
		Chat: c.Chat, Streaming: c.Streaming, Tools: c.Tools,
		StructuredOutput: c.StructuredOutput, Embeddings: c.Embeddings, Dimensions: c.Dimensions,
	}
}

// CapabilitiesFromIface converts from the consumer-facing contract type.
func CapabilitiesFromIface(c iface.LLMCapabilities) LLMModelCapabilities {
	return LLMModelCapabilities{
		Chat: c.Chat, Streaming: c.Streaming, Tools: c.Tools,
		StructuredOutput: c.StructuredOutput, Embeddings: c.Embeddings, Dimensions: c.Dimensions,
	}
}

type LLMCredentialRef struct {
	Kind           string `bson:"kind" json:"kind"`
	CredentialUUID string `bson:"credentialUuid,omitempty" json:"credentialUuid,omitempty"`
}

type LLMModelDefaults struct {
	Temperature     *float64 `bson:"temperature,omitempty" json:"temperature,omitempty" minimum:"0" maximum:"2"`
	MaxOutputTokens *int     `bson:"maxOutputTokens,omitempty" json:"maxOutputTokens,omitempty" minimum:"1" maximum:"131072"`
	Effort          string   `bson:"effort,omitempty" json:"effort,omitempty"`
}

type LLMModelPurpose struct {
	Purpose  string `bson:"purpose" json:"purpose"`
	Priority int    `bson:"priority" json:"priority"` // lower wins
}

// Model is an org-scoped configured model: provider + model id + who pays
// + what it can do + who may use it.
type Model struct {
	UUID                      string               `bson:"uuid" json:"uuid"`
	TenantID                  string               `bson:"tenantId" json:"-"`
	Name                      string               `bson:"name" json:"name"`
	Provider                  string               `bson:"provider" json:"provider"`
	ModelID                   string               `bson:"modelId" json:"modelId"`
	Capabilities              LLMModelCapabilities `bson:"capabilities" json:"capabilities"`
	CredentialRef             LLMCredentialRef     `bson:"credentialRef" json:"credentialRef"`
	Defaults                  LLMModelDefaults     `bson:"defaults" json:"defaults"`
	BudgetReserveOutputTokens *int                 `bson:"budgetReserveOutputTokens,omitempty" json:"budgetReserveOutputTokens,omitempty"`
	Purposes                  []LLMModelPurpose    `bson:"purposes" json:"purposes"`
	Access                    string               `bson:"access" json:"access"`
	Status                    string               `bson:"status" json:"status"`
	CreatedBy                 string               `bson:"createdBy" json:"createdBy"`
	CreatedAt                 time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt                 time.Time            `bson:"updatedAt" json:"updatedAt"`
}

// Info projects the consumer-visible view (pointer receiver: see View).
func (m *Model) Info() iface.LLMModelInfo {
	purposes := make([]string, 0, len(m.Purposes))
	for _, p := range m.Purposes {
		purposes = append(purposes, p.Purpose)
	}
	return iface.LLMModelInfo{
		UUID: m.UUID, Name: m.Name, Provider: m.Provider, ModelID: m.ModelID,
		Capabilities: m.Capabilities.ToIface(), CredentialKind: m.CredentialRef.Kind, Purposes: purposes,
	}
}

// Input returns the editable fields of m, the base a patch applies to.
func (m *Model) Input() ModelInput {
	return ModelInput{
		Name: m.Name, Provider: m.Provider, ModelID: m.ModelID, Capabilities: m.Capabilities,
		CredentialRef: m.CredentialRef, Defaults: m.Defaults, BudgetReserveOutputTokens: m.BudgetReserveOutputTokens,
		Purposes: m.Purposes,
	}
}

// ModelInput is the validated shape of create/patch bodies. It has no
// access: that is set only by CatalogService.ReplaceGrants.
type ModelInput struct {
	Name                      string
	Provider                  string
	ModelID                   string
	Capabilities              LLMModelCapabilities
	CredentialRef             LLMCredentialRef
	Defaults                  LLMModelDefaults
	BudgetReserveOutputTokens *int
	Purposes                  []LLMModelPurpose
}

// MaxOutputTokens bounds both defaults.maxOutputTokens and
// budgetReserveOutputTokens; it matches the module's
// budget_reserve_output_tokens maximum.
const MaxOutputTokens = 131072

var purposeRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var modelIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

var (
	ErrInvalidModelID          = errors.New("modelId must be 1-128 chars of letters, digits, dot, dash, underscore, colon or slash")
	ErrInvalidCredentialRef    = errors.New("credentialRef must be {kind: org, credentialUuid} or {kind: user_account}")
	ErrUserAccountProvider     = errors.New("a user_account model must use provider openai")
	ErrUserAccountCapabilities = errors.New("a user_account model cannot declare embeddings")
	ErrUserAccountDefaults     = errors.New("a user_account model cannot set temperature or maxOutputTokens")
	ErrInvalidPurposes         = errors.New("purposes must be 1-16 unique keys matching ^[a-z][a-z0-9_.-]{0,63}$")
	ErrInvalidAccess           = errors.New("access must be granted or everyone")
	ErrInvalidCapabilities     = errors.New("declare at least chat or embeddings; embeddings require dimensions > 0")
	ErrInvalidEffort           = errors.New("effort must be empty, low, medium or high")
	ErrInvalidReserve          = errors.New("budgetReserveOutputTokens must be 1-131072")
	ErrInvalidTemperature      = errors.New("defaults.temperature must be between 0 and 2")
	ErrInvalidMaxOutputTokens  = errors.New("defaults.maxOutputTokens must be 1-131072")
)

func ValidateModelInput(in ModelInput) error {
	if !nameRE.MatchString(in.Name) {
		return ErrInvalidName
	}
	if !IsProvider(in.Provider) {
		return ErrInvalidProvider
	}
	if !modelIDRE.MatchString(in.ModelID) {
		return ErrInvalidModelID
	}
	switch in.CredentialRef.Kind {
	case CredentialKindOrg:
		if in.CredentialRef.CredentialUUID == "" {
			return ErrInvalidCredentialRef
		}
	case CredentialKindUserAccount:
		if in.CredentialRef.CredentialUUID != "" {
			return ErrInvalidCredentialRef
		}
		if in.Provider != ProviderOpenAI {
			return ErrUserAccountProvider
		}
		if in.Capabilities.Embeddings {
			return ErrUserAccountCapabilities
		}
		if in.Defaults.Temperature != nil || in.Defaults.MaxOutputTokens != nil {
			return ErrUserAccountDefaults
		}
	default:
		return ErrInvalidCredentialRef
	}
	if !in.Capabilities.Chat && !in.Capabilities.Embeddings {
		return ErrInvalidCapabilities
	}
	if in.Capabilities.Embeddings && in.Capabilities.Dimensions <= 0 {
		return ErrInvalidCapabilities
	}
	if t := in.Defaults.Temperature; t != nil && !(*t >= 0 && *t <= 2) { // NaN fails both
		return ErrInvalidTemperature
	}
	if n := in.Defaults.MaxOutputTokens; n != nil && (*n < 1 || *n > MaxOutputTokens) {
		return ErrInvalidMaxOutputTokens
	}
	switch in.Defaults.Effort {
	case "", "low", "medium", "high":
	default:
		return ErrInvalidEffort
	}
	if in.BudgetReserveOutputTokens != nil && (*in.BudgetReserveOutputTokens < 1 || *in.BudgetReserveOutputTokens > MaxOutputTokens) {
		return ErrInvalidReserve
	}
	if len(in.Purposes) == 0 || len(in.Purposes) > 16 {
		return ErrInvalidPurposes
	}
	seen := map[string]bool{}
	for _, p := range in.Purposes {
		if !purposeRE.MatchString(p.Purpose) || seen[p.Purpose] {
			return ErrInvalidPurposes
		}
		seen[p.Purpose] = true
	}
	return nil
}

// ValidateAccess checks the access value the grants route sets.
func ValidateAccess(access string) error {
	if access != AccessGranted && access != AccessEveryone {
		return ErrInvalidAccess
	}
	return nil
}
