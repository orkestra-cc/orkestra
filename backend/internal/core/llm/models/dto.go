package models

// Every type here is an HTTP request/response body, so it carries the LLM
// prefix: Huma schema names are package-less and it panics on duplicates
// (auth already owns CredentialView).

// LLMCredentialView is what GET returns: never the secret, only its tail
// (the embedded Credential.SecretLast4).
type LLMCredentialView struct {
	Credential
	HasSecret bool `json:"hasSecret"`
}

// View, like every method of Credential and Model, has a pointer receiver:
// the views embed those structs, and Huma's schema-link transformer
// (reflect.StructOf) cannot rebuild a struct embedding a type whose value
// method set is non-empty; it warns on stderr and drops $schema instead.
func (c *Credential) View() LLMCredentialView {
	return LLMCredentialView{Credential: *c, HasSecret: !c.Secret.IsZero()}
}

type LLMCredentialCreateBody struct {
	Name     string `json:"name" doc:"Display name, unique per organization" maxLength:"64"`
	Provider string `json:"provider" enum:"openai,anthropic,gemini,ollama,openai_compatible,mock"`
	BaseURL  string `json:"baseUrl,omitempty" doc:"Required for ollama and openai_compatible; fixed for cloud providers" maxLength:"512"`
	Secret   string `json:"secret,omitempty" doc:"API key, write-only" maxLength:"4096"`
}

type LLMCredentialPatchBody struct {
	Name    *string `json:"name,omitempty" maxLength:"64"`
	BaseURL *string `json:"baseUrl,omitempty" maxLength:"512"`
	Status  *string `json:"status,omitempty" enum:"active,disabled"`
}

type LLMCredentialRotateBody struct {
	Secret string `json:"secret" doc:"New API key, write-only" minLength:"1" maxLength:"4096"`
}

type LLMModelBody struct {
	Name                      string               `json:"name" maxLength:"64"`
	Provider                  string               `json:"provider" enum:"openai,anthropic,gemini,ollama,openai_compatible,mock"`
	ModelID                   string               `json:"modelId" maxLength:"128"`
	Capabilities              LLMModelCapabilities `json:"capabilities"`
	CredentialRef             LLMCredentialRef     `json:"credentialRef"`
	Defaults                  LLMModelDefaults     `json:"defaults"`
	BudgetReserveOutputTokens *int                 `json:"budgetReserveOutputTokens,omitempty" minimum:"1" maximum:"131072"`
	Purposes                  []LLMModelPurpose    `json:"purposes" minItems:"1" maxItems:"16"`
}

// Input returns the create input. Access is not part of it: a new model is
// always created with access granted and no grants, and only the grants
// route (its own permission and step-up) opens it.
func (b LLMModelBody) Input() ModelInput {
	return ModelInput{
		Name: b.Name, Provider: b.Provider, ModelID: b.ModelID, Capabilities: b.Capabilities,
		CredentialRef: b.CredentialRef, Defaults: b.Defaults, BudgetReserveOutputTokens: b.BudgetReserveOutputTokens,
		Purposes: b.Purposes,
	}
}

// LLMModelPatchBody is a partial update: an absent (nil) field keeps the
// stored value. Struct-valued fields (capabilities, credentialRef, defaults)
// and purposes replace the stored value whole when present. The merged
// model is validated with the create rules. Access is changed only on the
// grants route.
type LLMModelPatchBody struct {
	Name                      *string               `json:"name,omitempty" maxLength:"64"`
	Provider                  *string               `json:"provider,omitempty" enum:"openai,anthropic,gemini,ollama,openai_compatible,mock"`
	ModelID                   *string               `json:"modelId,omitempty" maxLength:"128"`
	Capabilities              *LLMModelCapabilities `json:"capabilities,omitempty"`
	CredentialRef             *LLMCredentialRef     `json:"credentialRef,omitempty"`
	Defaults                  *LLMModelDefaults     `json:"defaults,omitempty"`
	BudgetReserveOutputTokens *int                  `json:"budgetReserveOutputTokens,omitempty" minimum:"1" maximum:"131072"`
	Purposes                  []LLMModelPurpose     `json:"purposes,omitempty" minItems:"1" maxItems:"16"`
	Status                    *string               `json:"status,omitempty" enum:"active,disabled"`
}

// ApplyTo returns in with every provided field of the patch replaced.
func (p LLMModelPatchBody) ApplyTo(in ModelInput) ModelInput {
	if p.Name != nil {
		in.Name = *p.Name
	}
	if p.Provider != nil {
		in.Provider = *p.Provider
	}
	if p.ModelID != nil {
		in.ModelID = *p.ModelID
	}
	if p.Capabilities != nil {
		in.Capabilities = *p.Capabilities
	}
	if p.CredentialRef != nil {
		in.CredentialRef = *p.CredentialRef
	}
	if p.Defaults != nil {
		in.Defaults = *p.Defaults
	}
	if p.BudgetReserveOutputTokens != nil {
		in.BudgetReserveOutputTokens = p.BudgetReserveOutputTokens
	}
	if p.Purposes != nil {
		in.Purposes = p.Purposes
	}
	return in
}

// LLMModelView adds the grant list the admin table shows inline.
type LLMModelView struct {
	Model
	Grants []LLMGrant `json:"grants"`
}

// LLMGrantsPutBody decides who may use a model: access and the complete
// grant list, replaced together. With access everyone the grants are kept
// but dormant (everyone in the org may use the model); they apply again
// once access returns to granted. Every listed user must be a member.
type LLMGrantsPutBody struct {
	Access    string   `json:"access" enum:"granted,everyone" doc:"granted: only the listed users; everyone: every member of the organization"`
	UserUUIDs []string `json:"userUuids" doc:"Complete list; replaces the current grants" maxItems:"500"`
}
