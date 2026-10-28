package iface

import (
	"context"
	"errors"
)

// Installation-level inference (ADR-0023 D4): endpoints declared by the
// operator in RETRIEVAL_INFERENCE_ENDPOINTS, served only to the retrieval
// module. LLMGateway is unchanged; the registered gateway also implements
// the three interfaces below (assert them on the resolved LLMGateway).

type InferenceKind string

const (
	InferenceKindEmbed  InferenceKind = "embed"
	InferenceKindRerank InferenceKind = "rerank"
	InferenceKindChat   InferenceKind = "chat" // parsed and listed; served from delivery step 3
)

type InferenceBoundary string

const (
	InferenceBoundaryLocal  InferenceBoundary = "local"
	InferenceBoundaryHosted InferenceBoundary = "hosted"
)

type EmbedInputKind string

const (
	EmbedInputQuery    EmbedInputKind = "query"
	EmbedInputDocument EmbedInputKind = "document"
)

// InferenceProfile is the immutable identity of what an endpoint computes.
// ID = "ip_" + hex(sha256(canonical JSON of Kind, ModelID, Revision, Pooling,
// Normalize, Truncate, QueryPrefix, DocumentPrefix)); the endpoint name and
// URL are not part of it, so moving a model to another host keeps its ID.
type InferenceProfile struct {
	ID             string            `json:"id"`
	Endpoint       string            `json:"endpoint"`
	Kind           InferenceKind     `json:"kind"`
	Boundary       InferenceBoundary `json:"boundary"`
	ModelID        string            `json:"modelId"`
	Revision       string            `json:"revision"`
	Dimensions     int               `json:"dimensions,omitempty"` // embed: measured at verification
	MaxInputTokens int               `json:"maxInputTokens,omitempty"`
	Pooling        string            `json:"pooling,omitempty"`
	Normalize      bool              `json:"normalize"`
	Truncate       bool              `json:"truncate"`
	QueryPrefix    string            `json:"queryPrefix,omitempty"`
	DocumentPrefix string            `json:"documentPrefix,omitempty"`
}

type LLMInferenceProfiles interface {
	// InferenceProfile returns the profile the named endpoint serves, verified
	// against what the endpoint reports. Unknown name: ErrLLMNotConfigured;
	// endpoint reachable but serving another model or revision:
	// ErrLLMProfileMismatch; unreachable: ErrLLMProviderUnavailable.
	InferenceProfile(ctx context.Context, endpoint string) (*InferenceProfile, error)
}

type PinnedEmbedRequest struct {
	Caller     string         // must be "retrieval"
	OnBehalfOf string         // org billed; must equal the tenant in ctx
	Profile    string         // InferenceProfile.ID; refused if the endpoint now serves another
	Kind       EmbedInputKind // selects QueryPrefix or DocumentPrefix
	Inputs     []string
}

type LLMPinnedEmbedder interface {
	EmbedWithProfile(ctx context.Context, req PinnedEmbedRequest) (*EmbedResult, error)
}

type RerankRequest struct {
	Caller     string
	OnBehalfOf string
	Profile    string
	Query      string
	Documents  []string
	TopN       int // 0 = all
}

type RerankScore struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
}

type RerankResult struct {
	Results []RerankScore `json:"results"` // ordered by Score descending
	Profile string        `json:"profile"`
	Usage   LLMUsage      `json:"usage"`
}

type LLMReranker interface {
	Rerank(ctx context.Context, req RerankRequest) (*RerankResult, error)
}

var ErrLLMProfileMismatch = errors.New("llm: the endpoint no longer serves this inference profile")
