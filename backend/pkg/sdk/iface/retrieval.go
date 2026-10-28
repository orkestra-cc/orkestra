// backend/pkg/sdk/iface/retrieval.go
package iface

// Retrieval contract of the core `retrieval` module (ADR-0023): corpus
// declaration, indexing intents, hybrid search and principal resolution.
// One *services.Service registered under module.ServiceRetrieval implements
// RetrievalCorpusRegistry, RetrievalOutbox, RetrievalSearch and
// RetrievalPrincipalResolver; consumers resolve each with
// module.GetTyped[iface.<Interface>](reg, module.ServiceRetrieval).

import (
	"context"
	"errors"
)

type RetrievalLocality string

const (
	RetrievalLocalOnly     RetrievalLocality = "local_only"
	RetrievalHostedAllowed RetrievalLocality = "hosted_allowed"
)

type RetrievalFieldType string

const (
	RetrievalFieldKeyword  RetrievalFieldType = "keyword"
	RetrievalFieldKeywords RetrievalFieldType = "keyword[]"
	RetrievalFieldInteger  RetrievalFieldType = "integer"
	RetrievalFieldFloat    RetrievalFieldType = "float"
	RetrievalFieldBool     RetrievalFieldType = "bool"
	RetrievalFieldDatetime RetrievalFieldType = "datetime" // RFC 3339 in, stored as UTC
)

type RetrievalFilterField struct {
	Name        string             `json:"name"` // ^[a-z][a-zA-Z0-9_]{0,63}$
	Type        RetrievalFieldType `json:"type"`
	Description string             `json:"description"`
}

type RetrievalAnalyzer struct {
	Tokenizer string `json:"tokenizer"` // "word" (default) | "multilingual"
	Language  string `json:"language"`  // snowball language for stemming and stop words; "" = none
	Stemming  bool   `json:"stemming"`
	StopWords bool   `json:"stopWords"`
}

type RetrievalChunking struct {
	Strategy  string `json:"strategy"`  // "none" | "fixed"; "hierarchical" from step 3
	MaxTokens int    `json:"maxTokens"` // fixed only
	Overlap   int    `json:"overlap"`   // fixed only, < MaxTokens
}

type RetrievalEnrichment struct { // delivery step 3; must be nil in step 1
	ChatEndpoint  string `json:"chatEndpoint"`
	PromptVersion string `json:"promptVersion"`
}

type RetrievalIngestionProfile struct {
	EmbedEndpoint string               `json:"embedEndpoint"` // installation endpoint name, kind embed
	Analyzer      RetrievalAnalyzer    `json:"analyzer"`
	Chunking      RetrievalChunking    `json:"chunking"`
	Enrichment    *RetrievalEnrichment `json:"enrichment,omitempty"`
}

type RetrievalFusion struct {
	DenseWeight   float64 `json:"denseWeight"`   // 0 < w <= 10
	KeywordWeight float64 `json:"keywordWeight"` // 0 < w <= 10
	Candidates    int     `json:"candidates"`    // per arm, 1–500
}

type RetrievalRerank struct {
	Endpoint string `json:"endpoint"` // installation endpoint name, kind rerank
	Enabled  bool   `json:"enabled"`
	TopK     int    `json:"topK"` // 1–200
}

type RetrievalCorpusSpec struct {
	Name       string // ^[a-z][a-z0-9_]{0,31}$, unique per installation
	Owner      string // owning module name
	Permission string // org permission the caller must hold
	Filters    []RetrievalFilterField
	Ingestion  RetrievalIngestionProfile
	Locality   RetrievalLocality // "" = local_only
	Fusion     RetrievalFusion   // zero = module defaults
	Rerank     RetrievalRerank
	Source     RetrievalSource
}

type RetrievalItem struct {
	SourceID    string
	Revision    int64 // monotonic per item
	Title       string
	Text        string
	Fields      map[string]any // keys must be declared filter fields
	VisibleTo   []string       // "tenant:*", "role:<roleId>", "team:<teamId>", "user:<uuid>"
	Subjects    []string       // "user:<uuid>" or "<owner>:<id>"
	ContentHash string         // over the source content; the module hashes the indexed text separately
}

type RetrievalLoaded struct {
	SourceID string
	Deleted  bool
	Item     *RetrievalItem // nil when Deleted
}

type RetrievalEnumerated struct {
	SourceID    string
	Revision    int64
	ContentHash string
}

type RetrievalPage struct {
	Items []RetrievalEnumerated
	Next  string // "" at the end
}

type RetrievalPrincipalContext struct {
	TenantID   string
	UserUUID   string
	Principals []string
}

type RetrievalVisibility struct {
	Visible  bool
	Revision int64
}

// RetrievalSource is implemented by the owning module. Every call is scoped
// to the tenant passed in; the ctx carries that tenant too.
type RetrievalSource interface {
	Load(ctx context.Context, tenantID string, ids []string) ([]RetrievalLoaded, error)
	Enumerate(ctx context.Context, tenantID, cursor string, limit int) (RetrievalPage, error)
	// Visible answers for every id; an id missing from the map is "not visible".
	Visible(ctx context.Context, tenantID string, pc RetrievalPrincipalContext, ids []string) (map[string]RetrievalVisibility, error)
}

// RetrievalPrincipalContributor lets a module add principals (for example
// "team:<teamId>") to the caller's principal set at search time.
type RetrievalPrincipalContributor interface {
	Name() string
	Principals(ctx context.Context, tenantID, userUUID string) ([]string, error)
}

// RetrievalCorpusRegistry is how a module declares a corpus and registers
// principal contributors, from its Init.
type RetrievalCorpusRegistry interface {
	RegisterCorpus(spec RetrievalCorpusSpec) error // during any module's Init; closed when retrieval starts (Seal); never contacts the engine
	RegisterPrincipalContributor(c RetrievalPrincipalContributor) error
}

type RetrievalIntent struct {
	Corpus   string
	SourceID string
	Revision int64
}

// RetrievalOutbox records indexing intents transactionally with the source write.
type RetrievalOutbox interface {
	// Enqueue records intents for the tenant in ctx. Call it with the
	// mongo.SessionContext of the transaction that writes the source.
	Enqueue(ctx context.Context, intents ...RetrievalIntent) error
}

type RetrievalOp string

const (
	RetrievalOpEq     RetrievalOp = "eq"
	RetrievalOpIn     RetrievalOp = "in"
	RetrievalOpLt     RetrievalOp = "lt"
	RetrievalOpLte    RetrievalOp = "lte"
	RetrievalOpGt     RetrievalOp = "gt"
	RetrievalOpGte    RetrievalOp = "gte"
	RetrievalOpExists RetrievalOp = "exists"
)

// RetrievalFilter is a node: exactly one of And, Or, Not, or a comparison
// (Field + Op + Value/Values). Limits: depth <= 4, <= 64 nodes, <= 100 Values.
type RetrievalFilter struct {
	And    []RetrievalFilter `json:"and,omitempty"`
	Or     []RetrievalFilter `json:"or,omitempty"`
	Not    *RetrievalFilter  `json:"not,omitempty"`
	Field  string            `json:"field,omitempty"`
	Op     RetrievalOp       `json:"op,omitempty"`
	Value  any               `json:"value,omitempty"`
	Values []any             `json:"values,omitempty"`
}

type RetrievalMode string

const (
	RetrievalModeHybrid  RetrievalMode = "hybrid" // "" means hybrid
	RetrievalModeDense   RetrievalMode = "dense"
	RetrievalModeKeyword RetrievalMode = "keyword"
)

type RetrievalSearchRequest struct {
	Corpus string
	Query  string // 1–2000 runes
	Filter *RetrievalFilter
	Mode   RetrievalMode
	Limit  int              // 1–100, default 10
	Fusion *RetrievalFusion // optional per-request weights within the bounds above
}

type RetrievalProvenance struct {
	SourceID  string `json:"sourceId"`
	ChunkID   string `json:"chunkId"`
	Ordinal   int    `json:"ordinal"`
	Page      int    `json:"page,omitempty"`
	CharStart int    `json:"charStart"`
	CharEnd   int    `json:"charEnd"`
}

type RetrievalHit struct {
	SourceID   string              `json:"sourceId"`
	ChunkID    string              `json:"chunkId"`
	Rank       int                 `json:"rank"`
	Score      float32             `json:"score"`
	Snippet    string              `json:"snippet"`
	Provenance RetrievalProvenance `json:"provenance"`
	Locality   RetrievalLocality   `json:"locality"`
}

type RetrievalSearchResult struct {
	Hits     []RetrievalHit `json:"hits"`
	Degraded bool           `json:"degraded"`
}

// RetrievalSearch is the query side: hybrid search, the filter schema of a
// corpus, and data-subject erasure.
type RetrievalSearch interface {
	Search(ctx context.Context, req RetrievalSearchRequest) (*RetrievalSearchResult, error)
	FilterSchema(corpus string) ([]RetrievalFilterField, error)
	DeleteBySubject(ctx context.Context, tenantID, subject string) error
}

// RetrievalPrincipalResolver resolves the caller's principal set from ctx.
type RetrievalPrincipalResolver interface {
	Principals(ctx context.Context) (RetrievalPrincipalContext, error)
}

var (
	ErrRetrievalNotConfigured     = errors.New("retrieval: the vector engine is not configured")
	ErrRetrievalUnavailable       = errors.New("retrieval: the vector engine is unavailable")
	ErrRetrievalCorpusUnavailable = errors.New("retrieval: the corpus's owning module is not running")
	ErrRetrievalUnknownCorpus     = errors.New("retrieval: unknown corpus")
	ErrRetrievalInvalidSpec       = errors.New("retrieval: invalid corpus declaration")
	ErrRetrievalInvalidFilter     = errors.New("retrieval: invalid filter")
	ErrRetrievalInvalidRequest    = errors.New("retrieval: invalid search request")
	ErrRetrievalForbidden         = errors.New("retrieval: the caller may not search this corpus")
	ErrRetrievalLocality          = errors.New("retrieval: an inference endpoint violates the corpus locality")
	ErrRetrievalVisibilityCheck   = errors.New("retrieval: the source visibility check failed")
)
