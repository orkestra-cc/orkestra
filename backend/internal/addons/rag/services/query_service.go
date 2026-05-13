package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	aimodelsProviders "github.com/orkestra/backend/internal/addons/aimodels/providers"
)

const ragSystemPrompt = `You are an expert assistant for ISO standards, laws, and regulatory compliance. Answer the question based ONLY on the provided context.
If the context does not contain enough information to answer, say so clearly.
Always cite the source document, section path, and requirement level when referencing specific provisions.
Be precise and actionable in your responses.
/no_think`

const (
	// scopeOverFetchMultiplier increases the vector search candidate pool when
	// post-filtering (by document, requirement level, node type, or ISO standard)
	// is active, compensating for global top-K not including scoped documents.
	scopeOverFetchMultiplier = 5
	maxOverFetchTopK         = 200
)

// StreamResult holds the pre-LLM data and token channel for streaming queries.
//
// Cancel must be invoked once the caller is done draining TokenChan (typically
// `defer result.Cancel()`) so the LLM context's timer is released. It is
// always non-nil — a no-op when no LLM stream was started.
type StreamResult struct {
	Sources   []iface.SourceRef
	PreMeta   iface.QueryMeta
	TokenChan <-chan aimodelsProviders.StreamChunk
	Cancel    context.CancelFunc
	ModelName string
	StartTime time.Time // total query start for final metadata
	LLMStart  time.Time // LLM start for llmTimeMs
}

// QueryService handles RAG query execution
type QueryService interface {
	Query(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*iface.RAGQueryResponse, error)
	QueryStream(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*StreamResult, error)
}

type queryService struct {
	graphRepo     iface.GraphProvider
	modelProvider AIModelProvider
	defaultTopK   int
	logger        *slog.Logger
}

// NewQueryService creates a new QueryService
func NewQueryService(gr iface.GraphProvider, modelProvider AIModelProvider, defaultTopK int, logger *slog.Logger) QueryService {
	return &queryService{
		graphRepo:     gr,
		modelProvider: modelProvider,
		defaultTopK:   defaultTopK,
		logger:        logger.With(slog.String("module", "rag-query")),
	}
}

// preparedContext holds the results of embedding + vector search + context expansion
type preparedContext struct {
	sources      []iface.SourceRef
	contextStr   string
	prompt       string
	embTimeMs    int64
	searchTimeMs int64
	llmProvider  aimodelsProviders.LLMProvider
}

// prepareContext performs embedding, vector search, context expansion, and LLM provider resolution.
// documentUUIDs optionally restricts results to chunks from specific documents (nil = no filter).
func (s *queryService) prepareContext(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*preparedContext, error) {
	if topK <= 0 {
		topK = s.defaultTopK
	}
	if minScore <= 0 {
		minScore = 0.3
	}
	if retrievalMode == "" {
		retrievalMode = "vector"
	}

	// Step 1: Embed the question
	embStart := time.Now()
	embProvider, err := s.modelProvider.GetDefaultEmbeddingProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("no embedding model: %w", err)
	}

	queryVector, err := embProvider.Embed(ctx, question)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	embTimeMs := time.Since(embStart).Milliseconds()

	// Step 2: Vector search with optional filters
	searchStart := time.Now()

	// Build WHERE clauses
	whereClauses := []string{"similarity >= $minScore"}
	params := map[string]interface{}{
		"queryVector": queryVector,
		"minScore":    minScore,
	}
	if requirementLevel != "" {
		whereClauses = append(whereClauses, "node.requirementLevel = $reqLevel")
		params["reqLevel"] = requirementLevel
	}
	if nodeType != "" {
		whereClauses = append(whereClauses, "node.nodeType = $nodeType")
		params["nodeType"] = nodeType
	}
	if len(documentUUIDs) > 0 {
		whereClauses = append(whereClauses, "node.documentUuid IN $documentUuids")
		params["documentUuids"] = documentUUIDs
	}

	// Over-fetch when post-filtering is active so scoped documents aren't missed.
	// Document filtering needs the full candidate set in Go for balanced selection.
	hasPostFilter := len(documentUUIDs) > 0 || requirementLevel != "" || nodeType != "" || isoStandard != ""
	searchTopK := topK
	limit := topK
	if hasPostFilter {
		searchTopK = topK * scopeOverFetchMultiplier
		if searchTopK > maxOverFetchTopK {
			searchTopK = maxOverFetchTopK
		}
		// Return full over-fetched set — Go handles final truncation + balancing
		limit = searchTopK
	}
	params["topK"] = searchTopK
	params["limit"] = limit

	searchCypher := fmt.Sprintf(`
		CALL vector_search.search('rag_chunk_embedding', $topK, $queryVector)
		YIELD node, similarity
		WITH node, similarity
		WHERE %s
		RETURN node.uuid AS chunkUuid, node.text AS text,
		       node.fullPath AS fullPath, node.nodeType AS nodeType,
		       node.requirementLevel AS requirementLevel,
		       node.position AS position,
		       node.documentUuid AS documentUuid, similarity
		ORDER BY similarity DESC
		LIMIT $limit
	`, strings.Join(whereClauses, " AND "))

	searchResult, err := s.graphRepo.ExecuteRead(ctx, "", searchCypher, params)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	searchTimeMs := time.Since(searchStart).Milliseconds()

	s.logger.Debug("vector search completed",
		slog.Int("searchTopK", searchTopK),
		slog.Int("requestedTopK", topK),
		slog.Int("rawResults", len(searchResult.Rows)),
		slog.Bool("hasDocumentScope", len(documentUUIDs) > 0),
		slog.Bool("hasISOFilter", isoStandard != ""),
	)

	// Build source refs with document metadata
	var sources []iface.SourceRef
	var contextParts []string

	for _, row := range searchResult.Rows {
		src := iface.SourceRef{}
		if v, ok := row["chunkUuid"].(string); ok {
			src.ChunkUUID = v
		}
		if v, ok := row["text"].(string); ok {
			src.ChunkText = v
		}
		if v, ok := row["fullPath"].(string); ok {
			src.FullPath = v
		}
		if v, ok := row["nodeType"].(string); ok {
			src.NodeType = v
		}
		if v, ok := row["requirementLevel"].(string); ok {
			src.RequirementLevel = v
		}
		if v, ok := row["position"].(int64); ok {
			src.Position = int(v)
		}
		if v, ok := row["documentUuid"].(string); ok {
			src.DocumentUUID = v
		}
		if v, ok := row["similarity"].(float64); ok {
			src.Score = v
		}

		// Filter by ISO standard if specified
		docInfo := s.getDocumentInfo(ctx, src.DocumentUUID)
		if docInfo != nil {
			src.DocumentTitle = docInfo.title
			src.ISOStandard = docInfo.isoStandard
		}
		if isoStandard != "" && src.ISOStandard != isoStandard {
			continue
		}

		sources = append(sources, src)

		// Build context based on retrieval mode
		var expandedText string
		switch retrievalMode {
		case "graph", "hybrid":
			expandedText = s.expandContextGraph(ctx, src.ChunkUUID, src.ChunkText)
		default:
			expandedText = s.expandContext(ctx, src.ChunkUUID, src.ChunkText)
		}

		path := src.FullPath
		if path == "" {
			path = "General"
		}

		// Build richer context entry
		var entry strings.Builder
		entry.WriteString(fmt.Sprintf("Source: %s (%s)\nPath: %s\n", src.DocumentTitle, src.ISOStandard, path))
		if src.RequirementLevel != "" {
			entry.WriteString(fmt.Sprintf("Requirement Level: %s\n", src.RequirementLevel))
		}
		entry.WriteString(expandedText)

		contextParts = append(contextParts, entry.String())
	}

	// Balanced per-document selection: ensure each scoped document is represented
	if len(documentUUIDs) > 1 && len(sources) > topK {
		// Group source indices by document (order preserved = similarity order)
		docIndices := make(map[string][]int)
		for i, src := range sources {
			docIndices[src.DocumentUUID] = append(docIndices[src.DocumentUUID], i)
		}

		perDoc := (topK + len(docIndices) - 1) / len(docIndices) // ceil
		selected := make(map[int]bool)

		// Phase 1: guarantee each document up to perDoc slots
		for _, indices := range docIndices {
			for j := 0; j < perDoc && j < len(indices); j++ {
				selected[indices[j]] = true
			}
		}
		// Phase 2: fill remaining with best unselected (global similarity order)
		for i := range sources {
			if len(selected) >= topK {
				break
			}
			if !selected[i] {
				selected[i] = true
			}
		}

		// Rebuild in original similarity order
		var balSources []iface.SourceRef
		var balParts []string
		for i := range sources {
			if !selected[i] {
				continue
			}
			balSources = append(balSources, sources[i])
			balParts = append(balParts, contextParts[i])
			if len(balSources) >= topK {
				break
			}
		}
		sources = balSources
		contextParts = balParts
	} else if len(sources) > topK {
		sources = sources[:topK]
		contextParts = contextParts[:topK]
	}

	// Resolve LLM provider
	var llmProvider aimodelsProviders.LLMProvider
	if llmOverrideUUID != "" {
		llmProvider, _ = s.modelProvider.GetLLMProvider(ctx, llmOverrideUUID)
	}
	if llmProvider == nil {
		llmProvider, err = s.modelProvider.GetDefaultLLMProvider(ctx)
		if err != nil {
			return nil, fmt.Errorf("no LLM model: %w", err)
		}
	}

	contextStr := strings.Join(contextParts, "\n---\n")
	prompt := fmt.Sprintf("CONTEXT:\n%s\n\nQUESTION: %s\n\nANSWER:", contextStr, question)

	return &preparedContext{
		sources:      sources,
		contextStr:   contextStr,
		prompt:       prompt,
		embTimeMs:    embTimeMs,
		searchTimeMs: searchTimeMs,
		llmProvider:  llmProvider,
	}, nil
}

func (s *queryService) Query(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*iface.RAGQueryResponse, error) {
	totalStart := time.Now()

	pc, err := s.prepareContext(ctx, question, topK, minScore, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode, documentUUIDs)
	if err != nil {
		return nil, err
	}

	if len(pc.sources) == 0 {
		resp := &iface.RAGQueryResponse{}
		resp.Body.Answer = "No relevant information found in the knowledge base for your question."
		resp.Body.Sources = []iface.SourceRef{}
		resp.Body.Metadata = iface.QueryMeta{
			EmbeddingTimeMs: pc.embTimeMs,
			SearchTimeMs:    pc.searchTimeMs,
			TotalTimeMs:     time.Since(totalStart).Milliseconds(),
		}
		return resp, nil
	}

	// Generate answer with LLM
	llmStart := time.Now()
	llmCtx, llmCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer llmCancel()

	answer, err := pc.llmProvider.Complete(llmCtx, pc.prompt, aimodelsProviders.CompletionOptions{
		SystemPrompt: ragSystemPrompt,
		Temperature:  0.1,
		MaxTokens:    2048,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}
	llmTimeMs := time.Since(llmStart).Milliseconds()

	resp := &iface.RAGQueryResponse{}
	resp.Body.Answer = answer
	resp.Body.Sources = pc.sources
	resp.Body.Metadata = iface.QueryMeta{
		EmbeddingTimeMs: pc.embTimeMs,
		SearchTimeMs:    pc.searchTimeMs,
		LLMTimeMs:       llmTimeMs,
		TotalTimeMs:     time.Since(totalStart).Milliseconds(),
		ChunksRetrieved: len(pc.sources),
		ModelUsed:       pc.llmProvider.ModelName(),
	}

	s.logger.Info("RAG query completed",
		slog.Int64("totalMs", resp.Body.Metadata.TotalTimeMs),
		slog.Int("chunks", len(pc.sources)),
	)

	return resp, nil
}

func (s *queryService) QueryStream(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*StreamResult, error) {
	totalStart := time.Now()

	pc, err := s.prepareContext(ctx, question, topK, minScore, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode, documentUUIDs)
	if err != nil {
		return nil, err
	}

	if len(pc.sources) == 0 {
		return &StreamResult{
			Sources: []iface.SourceRef{},
			PreMeta: iface.QueryMeta{
				EmbeddingTimeMs: pc.embTimeMs,
				SearchTimeMs:    pc.searchTimeMs,
				TotalTimeMs:     time.Since(totalStart).Milliseconds(),
			},
			TokenChan: nil,
			Cancel:    func() {},
		}, nil
	}

	// Start streaming LLM generation. Caller must invoke result.Cancel() when
	// done draining TokenChan to release the timer.
	llmCtx, llmCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	llmStart := time.Now()

	tokenChan, err := pc.llmProvider.StreamComplete(llmCtx, pc.prompt, aimodelsProviders.CompletionOptions{
		SystemPrompt: ragSystemPrompt,
		Temperature:  0.1,
		MaxTokens:    2048,
	})
	if err != nil {
		llmCancel()
		return nil, fmt.Errorf("LLM stream failed: %w", err)
	}

	return &StreamResult{
		Sources: pc.sources,
		PreMeta: iface.QueryMeta{
			EmbeddingTimeMs: pc.embTimeMs,
			SearchTimeMs:    pc.searchTimeMs,
			ChunksRetrieved: len(pc.sources),
			ModelUsed:       pc.llmProvider.ModelName(),
		},
		TokenChan: tokenChan,
		Cancel:    llmCancel,
		ModelName: pc.llmProvider.ModelName(),
		StartTime: totalStart,
		LLMStart:  llmStart,
	}, nil
}

type docInfo struct {
	title       string
	isoStandard string
}

func (s *queryService) getDocumentInfo(ctx context.Context, docUUID string) *docInfo {
	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (d:RagDocument {uuid: $uuid})
		RETURN d.title AS title, d.isoStandard AS isoStandard
	`, map[string]interface{}{"uuid": docUUID})
	if err != nil || len(result.Rows) == 0 {
		return nil
	}
	info := &docInfo{}
	if v, ok := result.Rows[0]["title"].(string); ok {
		info.title = v
	}
	if v, ok := result.Rows[0]["isoStandard"].(string); ok {
		info.isoStandard = v
	}
	return info
}

// expandContext fetches prev/next chunks via NEXT edges (basic mode).
func (s *queryService) expandContext(ctx context.Context, chunkUUID, chunkText string) string {
	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (c:RagChunk {uuid: $uuid})
		OPTIONAL MATCH (prev:RagChunk)-[:NEXT]->(c)
		OPTIONAL MATCH (c)-[:NEXT]->(next:RagChunk)
		RETURN prev.text AS prevText, next.text AS nextText
	`, map[string]interface{}{"uuid": chunkUUID})

	if err != nil || len(result.Rows) == 0 {
		return chunkText
	}

	var parts []string
	if v, ok := result.Rows[0]["prevText"].(string); ok && v != "" {
		parts = append(parts, v)
	}
	parts = append(parts, chunkText)
	if v, ok := result.Rows[0]["nextText"].(string); ok && v != "" {
		parts = append(parts, v)
	}
	return strings.Join(parts, "\n\n")
}

// expandContextGraph performs full graph-aware context expansion:
// section hierarchy, sibling chunks, cross-references, definitions,
// and similar chunks from other documents.
func (s *queryService) expandContextGraph(ctx context.Context, chunkUUID, chunkText string) string {
	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (c:RagChunk {uuid: $uuid})
		OPTIONAL MATCH (sec:RagSection)-[:CONTAINS]->(c)
		OPTIONAL MATCH (parent:RagSection)-[:CONTAINS]->(sec)
		OPTIONAL MATCH (prev:RagChunk)-[:NEXT]->(c)
		OPTIONAL MATCH (c)-[:NEXT]->(next:RagChunk)
		OPTIONAL MATCH (c)-[:REFERENCES]->(refSec:RagSection)
		OPTIONAL MATCH (def:RagDefinition)-[:DEFINES]->(c)
		RETURN sec.title AS sectionTitle, sec.fullPath AS sectionPath,
		       parent.title AS parentTitle,
		       prev.text AS prevText, next.text AS nextText,
		       collect(DISTINCT refSec.fullPath) AS referencedPaths,
		       collect(DISTINCT {term: def.term, definition: def.definition}) AS definitions
	`, map[string]interface{}{"uuid": chunkUUID})

	if err != nil || len(result.Rows) == 0 {
		return chunkText
	}

	row := result.Rows[0]
	var sb strings.Builder

	// Main chunk text with neighbors
	if v, ok := row["prevText"].(string); ok && v != "" {
		sb.WriteString(v)
		sb.WriteString("\n\n")
	}
	sb.WriteString(chunkText)
	if v, ok := row["nextText"].(string); ok && v != "" {
		sb.WriteString("\n\n")
		sb.WriteString(v)
	}

	// Cross-references
	if refs, ok := row["referencedPaths"].([]interface{}); ok && len(refs) > 0 {
		sb.WriteString("\n\nReferenced sections:")
		for _, ref := range refs {
			if path, ok := ref.(string); ok && path != "" {
				sb.WriteString("\n  - ")
				sb.WriteString(path)
			}
		}
	}

	// Definitions
	if defs, ok := row["definitions"].([]interface{}); ok && len(defs) > 0 {
		sb.WriteString("\n\nDefinitions:")
		for _, d := range defs {
			if dm, ok := d.(map[string]interface{}); ok {
				term, _ := dm["term"].(string)
				def, _ := dm["definition"].(string)
				if term != "" && def != "" {
					sb.WriteString(fmt.Sprintf("\n  - %s: %s", term, def))
				}
			}
		}
	}

	// Cross-document similar chunks via SIMILAR_TO edges
	simResult, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (c:RagChunk {uuid: $uuid})-[r:SIMILAR_TO]-(related:RagChunk)
		WHERE related.documentUuid <> c.documentUuid
		WITH related, r.similarity AS sim
		ORDER BY sim DESC
		LIMIT 3
		OPTIONAL MATCH (doc:RagDocument {uuid: related.documentUuid})
		RETURN related.text AS text, related.fullPath AS fullPath,
		       doc.title AS docTitle, sim
	`, map[string]interface{}{"uuid": chunkUUID})

	if err == nil && len(simResult.Rows) > 0 {
		sb.WriteString("\n\nRelated content from other documents:")
		for _, r := range simResult.Rows {
			docTitle, _ := r["docTitle"].(string)
			fullPath, _ := r["fullPath"].(string)
			text, _ := r["text"].(string)
			sim, _ := r["sim"].(float64)
			if text != "" {
				sb.WriteString(fmt.Sprintf("\n\n[%s — %s (%.0f%% similar)]:\n%s",
					docTitle, fullPath, sim*100, text))
			}
		}
	}

	return sb.String()
}
