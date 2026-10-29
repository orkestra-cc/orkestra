package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// AdminRuntime is what AdminService needs from *Service.
type AdminRuntime interface {
	Corpus(name string) (iface.RetrievalCorpusSpec, bool)
	Corpora() []iface.RetrievalCorpusSpec
	Engine() (engine.Engine, error)
	CollectionPrefix() string
	Diagnose(ctx context.Context, req iface.RetrievalSearchRequest) (*DiagnosticResult, error)
}

var _ AdminRuntime = (*Service)(nil)
