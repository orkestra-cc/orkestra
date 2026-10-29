package services

import (
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// AdminRuntime is what AdminService needs from *Service. Diagnose joins it
// once the diagnostics pipeline exists.
type AdminRuntime interface {
	Corpus(name string) (iface.RetrievalCorpusSpec, bool)
	Corpora() []iface.RetrievalCorpusSpec
	Engine() (engine.Engine, error)
	CollectionPrefix() string
}
