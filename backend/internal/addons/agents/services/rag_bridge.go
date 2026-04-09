package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/addons/agents/models"
	"github.com/orkestra/backend/internal/shared/iface"
)

// RAGBridge provides a scoped interface to the RAG query system for the agents module.
type RAGBridge interface {
	QueryWithScope(ctx context.Context, question string, documentUUIDs []string, topK int, minScore float64, retrievalMode string) (*RAGBridgeResult, error)
}

// RAGBridgeResult holds the output of a scoped RAG query
type RAGBridgeResult struct {
	Sources     []models.Source
	ContextText string
	Answer      string
	TimeMs      int64
	ModelUsed   string
}

type ragBridge struct {
	queryService iface.RAGQueryProvider
	defaultTopK  int
	logger       *slog.Logger
}

// NewRAGBridge creates a RAGBridge wrapping the existing RAG QueryService
func NewRAGBridge(queryService iface.RAGQueryProvider, defaultTopK int, logger *slog.Logger) RAGBridge {
	return &ragBridge{
		queryService: queryService,
		defaultTopK:  defaultTopK,
		logger:       logger.With(slog.String("module", "agents-rag-bridge")),
	}
}

func (b *ragBridge) QueryWithScope(ctx context.Context, question string, documentUUIDs []string, topK int, minScore float64, retrievalMode string) (*RAGBridgeResult, error) {
	if topK <= 0 {
		topK = b.defaultTopK
	}
	if retrievalMode == "" {
		retrievalMode = "graph"
	}

	start := time.Now()

	// Call RAG QueryService with document scoping
	resp, err := b.queryService.Query(ctx, question, topK, minScore, "", "", "", "", retrievalMode, documentUUIDs)
	if err != nil {
		return nil, err
	}

	result := &RAGBridgeResult{
		Answer:    resp.Body.Answer,
		TimeMs:    time.Since(start).Milliseconds(),
		ModelUsed: resp.Body.Metadata.ModelUsed,
	}

	// Convert RAG SourceRef to agents Source
	for _, src := range resp.Body.Sources {
		result.Sources = append(result.Sources, models.Source{
			DocumentUUID:     src.DocumentUUID,
			DocumentTitle:    src.DocumentTitle,
			ChunkText:        src.ChunkText,
			FullPath:         src.FullPath,
			RequirementLevel: src.RequirementLevel,
			Score:            src.Score,
		})
	}

	// Build context text from sources for Hindsight
	result.ContextText = resp.Body.Answer

	return result, nil
}
