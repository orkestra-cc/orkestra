// Package engine is the engine-neutral contract of the retrieval module's
// vector store. It is consumed only by the retrieval services and never
// imported by other modules; drivers (engine/qdrant) implement Engine and the
// conformance suite in enginetest defines each method's behaviour.
//
// Value representation at the engine boundary:
//
//	keyword   -> string
//	keyword[] -> []string
//	integer   -> int64
//	float     -> float64
//	bool      -> bool
//	datetime  -> int64 Unix milliseconds UTC (Point.Fields) and
//	             float64 milliseconds (Range)
//
// Datetime fields get an integer payload index, so range compilation needs no
// field-type lookup.
//
// Filter.Field is the declared field name, never a payload path: the driver
// maps it into payload namespace f. Candidate.Fields holds JSON-decoded
// values: numbers (integer, float, datetime) come back as float64 and lists
// (keyword[]) as []any of their elements.
package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

type Engine interface {
	Health(ctx context.Context) error
	EnsureCollection(ctx context.Context, spec CollectionSpec) error // idempotent; creates payload and full-text indexes
	DropCollection(ctx context.Context, name string) error
	// Upsert writes points; a point whose stored _rt.sourceRevision is higher
	// than the incoming one is left unchanged (conditional update).
	Upsert(ctx context.Context, collection string, points []Point) error
	// UpdatePayload replaces Runtime and Fields of existing points, keeping
	// their vectors and text. A point is updated only when it exists, its
	// stored _rt.contentHash equals the incoming Runtime.ContentHash and its
	// stored _rt.sourceRevision is not higher than the incoming one; otherwise
	// it is left unchanged. It never creates a point. Dense and Text are ignored.
	UpdatePayload(ctx context.Context, collection string, points []Point) error
	// DeleteOtherRevisions removes the item's points whose revision differs from keep.
	DeleteOtherRevisions(ctx context.Context, collection, tenantID, sourceID string, keep int64) error
	DeleteByFilter(ctx context.Context, collection string, sel Selector) error
	Count(ctx context.Context, collection string, sel Selector) (int64, error)
	// Query returns at most q.Limit candidates ordered by fused Score
	// descending; a query that breaks the rules on Query is refused.
	Query(ctx context.Context, collection string, q Query) ([]Candidate, error)
}

type CollectionSpec struct {
	Name       string
	Dimensions int
	Fields     []iface.RetrievalFilterField
	Analyzer   iface.RetrievalAnalyzer
}

type Runtime struct { // payload namespace _rt
	TenantID         string   `json:"tenantId"`
	SourceID         string   `json:"sourceId"`
	SourceRevision   int64    `json:"sourceRevision"`
	ChunkID          string   `json:"chunkId"`
	Ordinal          int      `json:"ordinal"`
	VisibleTo        []string `json:"visibleTo"`
	Subjects         []string `json:"subjects"`
	ContentHash      string   `json:"contentHash"` // over the indexed text
	IngestionProfile string   `json:"ingestionProfile"`
	CharStart        int      `json:"charStart"`
	CharEnd          int      `json:"charEnd"`
	Page             int      `json:"page,omitempty"`
}

type Point struct {
	ID      string    // PointID(tenantID, corpus, sourceID, ordinal)
	Dense   []float32 // nil: keyword-only write is refused (every point has a vector)
	Text    string    // indexed text; feeds BM25 and the snippet
	Runtime Runtime
	Fields  map[string]any // namespace f
}

// pointNamespace is uuid.NewSHA1(uuid.NameSpaceURL,
// []byte("https://orkestra.cc/retrieval/point-id/v1")), frozen as a literal.
// Changing it re-keys every point of every installation.
var pointNamespace = uuid.MustParse("21fc1e39-ea81-5111-b6fd-a430a1211388")

// PointID is UUIDv5 over "tenantID\x00corpus\x00sourceID\x00ordinal" in a fixed namespace.
func PointID(tenantID, corpus, sourceID string, ordinal int) string {
	name := tenantID + "\x00" + corpus + "\x00" + sourceID + "\x00" + strconv.Itoa(ordinal)
	return uuid.NewSHA1(pointNamespace, []byte(name)).String()
}

// CollectionName builds "<prefix>_<corpus>_g<generation>" (ADR D3). Every
// collection name is built through it.
func CollectionName(prefix, corpus string, generation int) string {
	return fmt.Sprintf("%s_%s_g%d", prefix, corpus, generation)
}

// Selector picks points for DeleteByFilter and Count. All set fields are
// ANDed and at least one must be set. SourceIDs is either nil (no source
// restriction) or a non-empty list of non-empty ids: a driver refuses an
// empty non-nil list or an empty id, so a caller's empty batch never widens
// into the whole tenant. Source ids are tenant-local, so SourceIDs requires
// TenantID: a driver refuses SourceIDs without a tenant rather than match the
// same id in every tenant. A Subject-only selector is tenant-less on purpose:
// it reaches a user's points in every tenant (global erasure).
type Selector struct {
	TenantID  string
	SourceIDs []string
	Subject   string // matches _rt.subjects
}

// IsZero reports whether no selector field is set.
func (s Selector) IsZero() bool {
	return s.TenantID == "" && len(s.SourceIDs) == 0 && s.Subject == ""
}

// Query is one hybrid search. A driver refuses it, before any request, unless:
//
//   - TenantID is set and Principals is non-empty with no empty entry;
//   - at least one arm is present: Dense non-empty, or Keyword not blank (a
//     whitespace-only Keyword means no keyword arm);
//   - each present arm's weight is > 0 (an absent arm's weight is ignored);
//   - Limit is in 1..500;
//   - Filter, when set, is well-formed: a leaf has a Field and exactly one
//     condition (non-empty Match, a Range with a bound, or Exists), a
//     composite has no Field and at least one child.
//
// PerArm is raised to max(PerArm, Limit), which must not exceed 500.
type Query struct {
	TenantID    string   // mandatory; also scopes BM25 statistics
	Principals  []string // mandatory, non-empty; _rt.visibleTo must intersect
	Filter      *Filter  // compiled user filter over namespace f; nil = none
	Dense       []float32
	Keyword     string
	Weights     Weights // RRF weights; an arm with no input is skipped
	PerArm      int
	Limit       int
	Diagnostics bool // fill per-arm scores (admin diagnostics only)
}

type Weights struct{ Dense, Keyword float64 }

type Filter struct { // compiled form, produced only by retrieval/filter
	Must, Should, MustNot []Filter
	Field                 string
	Match                 []any  // eq / in
	Range                 *Range // lt, lte, gt, gte (numbers and datetimes)
	Exists                bool
}

type Range struct{ Lt, Lte, Gt, Gte *float64 }

// Candidate is one fused search hit; Query returns them ordered by Score
// (fused) descending. Fields is JSON-decoded (see the package
// doc: numbers as float64, lists as []any). With Query.Diagnostics, DenseScore
// is the dense arm's cosine similarity and KeywordScore the keyword arm's BM25
// score, each nil when the point is absent from that arm's results.
type Candidate struct {
	PointID      string
	Runtime      Runtime
	Text         string
	Fields       map[string]any
	Score        float32  // fused
	DenseScore   *float32 // only with Diagnostics
	KeywordScore *float32 // only with Diagnostics
}

// ErrUnavailable means the engine could not answer usably: a network
// failure, a timeout, a 5xx, 401/403 (a wrong API key), 429, a response body
// over the size limit or one that cannot be decoded. A cancellation by the
// caller is not an outage: it surfaces as context.Canceled, not
// ErrUnavailable. ErrNotFound means the collection does not exist. Any other
// error is a refusal of the request itself.
var (
	ErrUnavailable = errors.New("engine: unavailable")
	ErrNotFound    = errors.New("engine: collection not found")
)
