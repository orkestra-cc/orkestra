// Package qdrant is the Qdrant driver of the retrieval engine contract
// (ADR-0023): REST over net/http, Qdrant >= 1.20. The conformance suite in
// enginetest defines its behaviour.
package qdrant

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

type Config struct {
	URL              string
	APIKey           string
	Timeout          time.Duration // per call; 0 = 10s; the caller's earlier deadline wins
	MaxResponseBytes int64         // 0 = 16 MiB
	HTTPClient       *http.Client  // nil = own client (dial 5s, TLS handshake 5s)
}

type Driver struct {
	cfg Config
}

var _ engine.Engine = (*Driver)(nil)

// errNotImplemented is the skeleton's answer until Tasks 6–7 land.
var errNotImplemented = errors.New("qdrant: not implemented")

func New(cfg Config) (*Driver, error) {
	return &Driver{cfg: cfg}, nil
}

func (d *Driver) Health(ctx context.Context) error { return errNotImplemented }

func (d *Driver) EnsureCollection(ctx context.Context, spec engine.CollectionSpec) error {
	return errNotImplemented
}

func (d *Driver) DropCollection(ctx context.Context, name string) error { return errNotImplemented }

func (d *Driver) Upsert(ctx context.Context, collection string, points []engine.Point) error {
	return errNotImplemented
}

func (d *Driver) DeleteOtherRevisions(ctx context.Context, collection, tenantID, sourceID string, keep int64) error {
	return errNotImplemented
}

func (d *Driver) DeleteByFilter(ctx context.Context, collection string, sel engine.Selector) error {
	return errNotImplemented
}

func (d *Driver) Count(ctx context.Context, collection string, sel engine.Selector) (int64, error) {
	return 0, errNotImplemented
}

func (d *Driver) Query(ctx context.Context, collection string, q engine.Query) ([]engine.Candidate, error) {
	return nil, errNotImplemented
}
