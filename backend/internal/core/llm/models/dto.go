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

func (c Credential) View() LLMCredentialView {
	return LLMCredentialView{Credential: c, HasSecret: !c.Secret.IsZero()}
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
	Access                    string               `json:"access" enum:"granted,everyone"`
}

func (b LLMModelBody) Input() ModelInput {
	return ModelInput{
		Name: b.Name, Provider: b.Provider, ModelID: b.ModelID, Capabilities: b.Capabilities,
		CredentialRef: b.CredentialRef, Defaults: b.Defaults, BudgetReserveOutputTokens: b.BudgetReserveOutputTokens,
		Purposes: b.Purposes, Access: b.Access,
	}
}

type LLMModelPatchBody struct {
	LLMModelBody
	Status *string `json:"status,omitempty" enum:"active,disabled"`
}

// LLMModelView adds the grant list the admin table shows inline.
type LLMModelView struct {
	Model
	Grants []LLMGrant `json:"grants"`
}

type LLMGrantsPutBody struct {
	UserUUIDs []string `json:"userUuids" doc:"Complete list; replaces the current grants" maxItems:"500"`
}
