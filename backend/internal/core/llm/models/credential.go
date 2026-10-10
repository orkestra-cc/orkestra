package models

import (
	"errors"
	"regexp"
	"time"
)

const (
	ProviderOpenAI           = "openai"
	ProviderAnthropic        = "anthropic"
	ProviderGemini           = "gemini"
	ProviderOllama           = "ollama"
	ProviderOpenAICompatible = "openai_compatible"
	ProviderMock             = "mock"
)

// Providers lists every provider kind the module accepts, in UI order.
var Providers = []string{ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderOllama, ProviderOpenAICompatible, ProviderMock}

func IsProvider(p string) bool {
	for _, k := range Providers {
		if k == p {
			return true
		}
	}
	return false
}

// IsHostedProvider: cloud providers that need the allow_hosted opt-in.
func IsHostedProvider(p string) bool {
	return p == ProviderOpenAI || p == ProviderAnthropic || p == ProviderGemini || p == ProviderOpenAICompatible
}

// FixedBaseURL is non-empty for providers whose endpoint the operator may
// not change.
func FixedBaseURL(p string) string {
	switch p {
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderAnthropic:
		return "https://api.anthropic.com"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com"
	}
	return ""
}

// NeedsSecret: ollama and mock may run without a key.
func NeedsSecret(p string) bool {
	return p != ProviderOllama && p != ProviderMock
}

const (
	CredentialStatusActive   = "active"
	CredentialStatusDisabled = "disabled"
)

// Credential is an org-scoped API key for one provider.
type Credential struct {
	UUID         string     `bson:"uuid" json:"uuid"`
	TenantID     string     `bson:"tenantId" json:"-"`
	Name         string     `bson:"name" json:"name"`
	Provider     string     `bson:"provider" json:"provider"`
	BaseURL      string     `bson:"baseUrl,omitempty" json:"baseUrl,omitempty"`
	Secret       Envelope   `bson:"secret" json:"-"`
	SecretLast4  string     `bson:"secretLast4,omitempty" json:"secretLast4,omitempty"` // display only
	Status       string     `bson:"status" json:"status"`
	LastTestedAt *time.Time `bson:"lastTestedAt,omitempty" json:"lastTestedAt,omitempty"`
	// LastTestStatus is "ok", "error" or "" (never tested).
	LastTestStatus string `bson:"lastTestStatus,omitempty" json:"lastTestStatus,omitempty"`
	// LastTestError is an llm.* code, never provider text.
	LastTestError string    `bson:"lastTestError,omitempty" json:"lastTestError,omitempty"`
	CreatedBy     string    `bson:"createdBy" json:"createdBy"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt" json:"updatedAt"`
}

var nameRE = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,63}$`)

// CredentialInput is the validated shape of create/patch bodies.
type CredentialInput struct {
	Name     string
	Provider string
	BaseURL  string
	Secret   string // plaintext, write-only; empty on patch means "keep"
}

var (
	ErrInvalidName     = errors.New("name must be 1-64 characters of letters, digits, space, dot, underscore or dash")
	ErrInvalidProvider = errors.New("unknown provider")
	ErrBaseURLFixed    = errors.New("baseUrl is fixed for this provider")
	ErrBaseURLRequired = errors.New("baseUrl is required for this provider")
	// ErrBaseURLNotAllowed: the provider has no endpoint to configure (mock).
	ErrBaseURLNotAllowed = errors.New("baseUrl is not accepted for this provider")
	ErrSecretRequired    = errors.New("secret is required for this provider")
)

// ValidateCredentialInput checks shape only; endpoint safety and hosted
// opt-in are service decisions.
func ValidateCredentialInput(in CredentialInput, creating bool) error {
	if !nameRE.MatchString(in.Name) {
		return ErrInvalidName
	}
	if !IsProvider(in.Provider) {
		return ErrInvalidProvider
	}
	if fixed := FixedBaseURL(in.Provider); fixed != "" && in.BaseURL != "" && in.BaseURL != fixed {
		return ErrBaseURLFixed
	}
	if in.Provider == ProviderMock && in.BaseURL != "" {
		return ErrBaseURLNotAllowed
	}
	if FixedBaseURL(in.Provider) == "" && in.Provider != ProviderMock && in.BaseURL == "" {
		return ErrBaseURLRequired
	}
	if creating && NeedsSecret(in.Provider) && in.Secret == "" {
		return ErrSecretRequired
	}
	return nil
}
