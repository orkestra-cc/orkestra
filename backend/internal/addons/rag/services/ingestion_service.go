package services

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	aimodelsProviders "github.com/orkestra-cc/orkestra-addon-aimodels/providers"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/repository"
)

// IngestionService manages document ingestion into the knowledge graph
type IngestionService interface {
	IngestDocument(ctx context.Context, title, fileName string, fileData []byte, isoStandard, version, documentCategory string, chunkSize, chunkOverlap int, llmModelUUID string) (*models.RagDocument, error)
	ListDocuments(ctx context.Context, status, isoStandard string) ([]models.RagDocument, error)
	GetDocument(ctx context.Context, uuid string) (*models.RagDocument, error)
	UpdateDocument(ctx context.Context, uuid string, title, isoStandard, version *string) (*models.RagDocument, error)
	GetDocumentChunks(ctx context.Context, uuid string) ([]models.RagChunk, error)
	GetDocumentSections(ctx context.Context, uuid string) ([]models.RagSection, error)
	GetDocumentRelations(ctx context.Context, uuid string) ([]models.RelatedDocSummary, []models.CrossDocLink, error)
	DeleteDocument(ctx context.Context, uuid string) error
	ReprocessDocument(ctx context.Context, uuid string) error
}

type ingestionService struct {
	docRepo             repository.DocumentRepository
	relTypeRepo         repository.RelationshipTypeRepository
	graphRepo           iface.GraphProvider
	modelProvider       AIModelProvider
	extractor           TextExtractor
	defaultChunk        int
	defaultOverlap      int
	similarityThreshold float64
	logger              *slog.Logger
}

// NewIngestionService creates a new IngestionService
func NewIngestionService(
	docRepo repository.DocumentRepository,
	relTypeRepo repository.RelationshipTypeRepository,
	gr iface.GraphProvider,
	modelProvider AIModelProvider,
	extractor TextExtractor,
	defaultChunk, defaultOverlap int,
	logger *slog.Logger,
) IngestionService {
	return &ingestionService{
		docRepo:             docRepo,
		relTypeRepo:         relTypeRepo,
		graphRepo:           gr,
		modelProvider:       modelProvider,
		extractor:           extractor,
		defaultChunk:        defaultChunk,
		defaultOverlap:      defaultOverlap,
		similarityThreshold: 0.85,
		logger:              logger.With(slog.String("module", "rag-ingestion")),
	}
}

func (s *ingestionService) IngestDocument(ctx context.Context, title, fileName string, fileData []byte, isoStandard, version, documentCategory string, chunkSize, chunkOverlap int, llmModelUUID string) (*models.RagDocument, error) {
	if chunkSize <= 0 {
		chunkSize = s.defaultChunk
	}
	if chunkOverlap <= 0 {
		chunkOverlap = s.defaultOverlap
	}

	// Detect doc type from extension
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")
	if ext == "" {
		ext = "txt"
	}

	// Get default embedding model
	embProvider, err := s.modelProvider.GetDefaultEmbeddingProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("no default embedding model configured: %w", err)
	}

	// Resolve LLM model name for contextual enrichment (best-effort)
	llmModelName := ""
	if llmModelUUID != "" {
		if p, err := s.modelProvider.GetLLMProvider(ctx, llmModelUUID); err == nil {
			llmModelName = p.ModelName()
		}
	}
	if llmModelName == "" {
		if p, err := s.modelProvider.GetDefaultLLMProvider(ctx); err == nil {
			llmModelName = p.ModelName()
		}
	}

	// Create document record
	doc := &models.RagDocument{
		UUID:             uuid.New().String(),
		Title:            title,
		FileName:         fileName,
		FileSize:         int64(len(fileData)),
		ISOStandard:      isoStandard,
		Version:          version,
		DocumentCategory: documentCategory,
		DocType:          ext,
		Status:           "pending",
		ChunkSize:        chunkSize,
		ChunkOverlap:     chunkOverlap,
		ModelUUID:        "",
		LLMModelName:     llmModelName,
	}

	if err := s.docRepo.Create(ctx, doc); err != nil {
		return nil, err
	}

	s.logger.Info("document ingestion started",
		slog.String("uuid", doc.UUID),
		slog.String("title", title),
		slog.String("fileName", fileName),
		slog.String("category", documentCategory),
	)

	// Process in background goroutine
	go s.processDocument(doc.UUID, fileData, ext, chunkSize, embProvider, llmModelUUID)

	return doc, nil
}

type embeddingProvider interface {
	Embed(context.Context, string) ([]float64, error)
	Dimensions() int
	ModelName() string
}

func (s *ingestionService) processDocument(docUUID string, fileData []byte, docType string, maxChunkSize int, embProvider embeddingProvider, llmModelUUID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	_ = s.docRepo.UpdateStatus(ctx, docUUID, "processing", "")

	// Step 1: Extract text
	s.logger.Info("extracting text", slog.String("uuid", docUUID))
	text, err := s.extractor.Extract(ctx, fileData, docType)
	if err != nil {
		s.failDocument(ctx, docUUID, "text extraction failed: "+err.Error())
		return
	}

	if strings.TrimSpace(text) == "" {
		s.failDocument(ctx, docUUID, "no text content extracted from document")
		return
	}

	// Step 2: Parse document structure
	s.logger.Info("parsing document structure", slog.String("uuid", docUUID), slog.String("docType", docType), slog.Int("textLen", len(text)))
	var root *StructuralNode
	if docType == "md" {
		root = ParseMarkdownStructure([]byte(text))
	} else {
		root = ParseDocumentStructure(text)
	}

	// Step 3: Chunk using structural boundaries
	minChunkSize := 128
	chunks := ChunkStructured(root, maxChunkSize, minChunkSize)
	if len(chunks) == 0 {
		s.failDocument(ctx, docUUID, "no chunks produced from document")
		return
	}

	s.logger.Info("generating embeddings",
		slog.String("uuid", docUUID),
		slog.Int("chunks", len(chunks)),
		slog.Int("sections", len(CollectSections(root))),
	)

	// Step 3.5: Contextual Retrieval — generate LLM context prefix for each chunk
	// This enriches the embedding with document context without changing the stored text.
	var contextPrefixes []string
	var llmProvider aimodelsProviders.LLMProvider
	// Use specified LLM model, or fall back to default
	if llmModelUUID != "" {
		llmProvider, _ = s.modelProvider.GetLLMProvider(ctx, llmModelUUID)
	}
	if llmProvider == nil {
		llmProvider, _ = s.modelProvider.GetDefaultLLMProvider(ctx)
	}
	if llmProvider != nil {
		doc, _ := s.docRepo.GetByUUID(ctx, docUUID)
		docTitle := ""
		isoStd := ""
		if doc != nil {
			docTitle = doc.Title
			isoStd = doc.ISOStandard
		}
		outline := BuildDocumentOutline(root)
		contextPrefixes = GenerateChunkContexts(ctx, llmProvider, docTitle, isoStd, outline, chunks, s.logger)
	} else {
		s.logger.Info("skipping contextual enrichment (no LLM configured)", slog.String("uuid", docUUID))
	}

	// Step 4: Generate embeddings for all chunks
	// Use contextualized text for embedding (context + chunk) but store raw text in graph.
	embeddings := make([][]float64, len(chunks))
	for i, chunk := range chunks {
		textToEmbed := chunk.Text
		if i < len(contextPrefixes) && contextPrefixes[i] != "" {
			textToEmbed = contextPrefixes[i] + "\n\n" + chunk.Text
		}
		emb, err := embProvider.Embed(ctx, textToEmbed)
		if err != nil {
			s.failDocument(ctx, docUUID, fmt.Sprintf("embedding failed for chunk %d: %v", i, err))
			return
		}
		embeddings[i] = emb
	}

	// Step 5: Create graph nodes
	s.logger.Info("creating graph nodes", slog.String("uuid", docUUID))

	doc, err := s.docRepo.GetByUUID(ctx, docUUID)
	if err != nil {
		s.failDocument(ctx, docUUID, "failed to retrieve document: "+err.Error())
		return
	}

	// Create RagDocument node
	_, err = s.graphRepo.ExecuteWrite(ctx, "", `
		CREATE (d:RagDocument {
			uuid: $uuid,
			title: $title,
			isoStandard: $isoStandard,
			version: $version,
			documentCategory: $category,
			docType: $docType,
			chunkCount: $chunkCount,
			createdAt: $createdAt
		})
	`, map[string]interface{}{
		"uuid":        docUUID,
		"title":       doc.Title,
		"isoStandard": doc.ISOStandard,
		"version":     doc.Version,
		"category":    doc.DocumentCategory,
		"docType":     doc.DocType,
		"chunkCount":  len(chunks),
		"createdAt":   doc.CreatedAt.Format(time.RFC3339),
	})
	if err != nil {
		s.failDocument(ctx, docUUID, "failed to create document node: "+err.Error())
		return
	}

	// Step 6: Create RagSection nodes from the structural tree
	sections := CollectSections(root)
	for _, sec := range sections {
		_, err := s.graphRepo.ExecuteWrite(ctx, "", `
			CREATE (s:RagSection {
				uuid: $uuid,
				documentUuid: $docUuid,
				nodeType: $nodeType,
				numbering: $numbering,
				title: $title,
				depth: $depth,
				fullPath: $fullPath,
				position: $position
			})
		`, map[string]interface{}{
			"uuid":      sec.UUID,
			"docUuid":   docUUID,
			"nodeType":  sec.NodeType,
			"numbering": sec.Numbering,
			"title":     sec.Title,
			"depth":     sec.Depth,
			"fullPath":  BuildFullPath(sec),
			"position":  sec.Position,
		})
		if err != nil {
			s.logger.Warn("failed to create section node", slog.String("error", err.Error()), slog.String("numbering", sec.Numbering))
		}
	}

	// Step 7: Create HAS_SECTION relationships (document → top-level sections)
	for _, sec := range sections {
		if sec.Parent != nil && sec.Parent.NodeType == "document" {
			_, err := s.graphRepo.ExecuteWrite(ctx, "", `
				MATCH (d:RagDocument {uuid: $docUuid})
				MATCH (s:RagSection {uuid: $secUuid})
				CREATE (d)-[:HAS_SECTION]->(s)
			`, map[string]interface{}{"docUuid": docUUID, "secUuid": sec.UUID})
			if err != nil {
				s.logger.Warn("failed to create HAS_SECTION", slog.String("error", err.Error()))
			}
		}
	}

	// Step 8: Create CONTAINS relationships (section → child section)
	for _, sec := range sections {
		if sec.Parent != nil && sec.Parent.NodeType != "document" {
			_, err := s.graphRepo.ExecuteWrite(ctx, "", `
				MATCH (parent:RagSection {uuid: $parentUuid})
				MATCH (child:RagSection {uuid: $childUuid})
				CREATE (parent)-[:CONTAINS]->(child)
			`, map[string]interface{}{"parentUuid": sec.Parent.UUID, "childUuid": sec.UUID})
			if err != nil {
				s.logger.Warn("failed to create section CONTAINS", slog.String("error", err.Error()))
			}
		}
	}

	// Step 9: Create NEXT_SECTION relationships (sequential sections at same depth under same parent)
	sectionsByParent := make(map[string][]*StructuralNode)
	for _, sec := range sections {
		if sec.Parent != nil {
			parentID := sec.Parent.UUID
			sectionsByParent[parentID] = append(sectionsByParent[parentID], sec)
		}
	}
	for _, siblings := range sectionsByParent {
		for i := 0; i < len(siblings)-1; i++ {
			_, err := s.graphRepo.ExecuteWrite(ctx, "", `
				MATCH (a:RagSection {uuid: $aUuid})
				MATCH (b:RagSection {uuid: $bUuid})
				CREATE (a)-[:NEXT_SECTION]->(b)
			`, map[string]interface{}{"aUuid": siblings[i].UUID, "bUuid": siblings[i+1].UUID})
			if err != nil {
				s.logger.Warn("failed to create NEXT_SECTION", slog.String("error", err.Error()))
			}
		}
	}

	// Step 10: Create RagChunk nodes with embeddings
	chunkUUIDs := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkUUID := uuid.New().String()
		chunkUUIDs[i] = chunkUUID

		ctxPrefix := ""
		if i < len(contextPrefixes) {
			ctxPrefix = contextPrefixes[i]
		}

		_, err := s.graphRepo.ExecuteWrite(ctx, "", `
			CREATE (c:RagChunk {
				uuid: $chunkUuid,
				documentUuid: $docUuid,
				text: $text,
				position: $position,
				nodeType: $nodeType,
				numbering: $numbering,
				fullPath: $fullPath,
				requirementLevel: $reqLevel,
				depth: $depth,
				sectionUuid: $sectionUuid,
				contextPrefix: $ctxPrefix,
				embedding: $embedding
			})
		`, map[string]interface{}{
			"chunkUuid":   chunkUUID,
			"docUuid":     docUUID,
			"text":        chunk.Text,
			"position":    chunk.Position,
			"nodeType":    chunk.NodeType,
			"numbering":   chunk.Numbering,
			"fullPath":    chunk.FullPath,
			"reqLevel":    chunk.RequirementLevel,
			"depth":       chunk.Depth,
			"sectionUuid": chunk.SectionUUID,
			"ctxPrefix":   ctxPrefix,
			"embedding":   embeddings[i],
		})
		if err != nil {
			s.failDocument(ctx, docUUID, fmt.Sprintf("failed to create chunk node %d: %v", i, err))
			return
		}
	}

	// Step 11: Create CONTAINS relationships (section → chunk)
	for i, chunk := range chunks {
		if chunk.SectionUUID != "" {
			_, err := s.graphRepo.ExecuteWrite(ctx, "", `
				MATCH (s:RagSection {uuid: $secUuid})
				MATCH (c:RagChunk {uuid: $chunkUuid})
				CREATE (s)-[:CONTAINS]->(c)
			`, map[string]interface{}{"secUuid": chunk.SectionUUID, "chunkUuid": chunkUUIDs[i]})
			if err != nil {
				s.logger.Warn("failed to create section→chunk CONTAINS", slog.String("error", err.Error()))
			}
		}
	}

	// Step 12: Create NEXT relationships between sequential chunks
	for i := 0; i < len(chunkUUIDs)-1; i++ {
		_, err := s.graphRepo.ExecuteWrite(ctx, "", `
			MATCH (a:RagChunk {uuid: $aUuid})
			MATCH (b:RagChunk {uuid: $bUuid})
			CREATE (a)-[:NEXT]->(b)
		`, map[string]interface{}{"aUuid": chunkUUIDs[i], "bUuid": chunkUUIDs[i+1]})
		if err != nil {
			s.logger.Warn("failed to create NEXT relationship", slog.String("error", err.Error()))
		}
	}

	// Load active relationship types for this document's category
	activeRels := make(map[string]bool)
	if s.relTypeRepo != nil {
		category := doc.DocumentCategory
		if category == "" {
			category = "generic"
		}
		if rels, err := s.relTypeRepo.ListActiveForCategory(ctx, category); err == nil {
			for _, r := range rels {
				activeRels[r.Name] = true
			}
			s.logger.Info("loaded active relationship types",
				slog.String("category", category),
				slog.Int("count", len(activeRels)),
			)
		}
	}

	// Step 13: Extract and create definitions from terms sections
	if activeRels["HAS_DEFINITION"] || activeRels["DEFINES"] {
		for _, sec := range sections {
			if sec.NodeType == "terms_section" {
				defs := ExtractDefinitions(sec)
				for _, def := range defs {
					defUUID := uuid.New().String()
					defEmb, embErr := embProvider.Embed(ctx, def.Term+": "+def.Definition)
					if embErr != nil {
						s.logger.Warn("failed to embed definition", slog.String("term", def.Term), slog.String("error", embErr.Error()))
						continue
					}

					_, err := s.graphRepo.ExecuteWrite(ctx, "", `
						MATCH (d:RagDocument {uuid: $docUuid})
						CREATE (def:RagDefinition {
							uuid: $defUuid,
							documentUuid: $docUuid,
							term: $term,
							definition: $definition,
							embedding: $embedding
						})
						CREATE (d)-[:HAS_DEFINITION]->(def)
					`, map[string]interface{}{
						"docUuid":    docUUID,
						"defUuid":    defUUID,
						"term":       def.Term,
						"definition": def.Definition,
						"embedding":  defEmb,
					})
					if err != nil {
						s.logger.Warn("failed to create definition node", slog.String("term", def.Term), slog.String("error", err.Error()))
						continue
					}

					if activeRels["DEFINES"] {
						termLower := strings.ToLower(def.Term)
						for j, chunk := range chunks {
							if strings.Contains(strings.ToLower(chunk.Text), termLower) {
								_, _ = s.graphRepo.ExecuteWrite(ctx, "", `
									MATCH (def:RagDefinition {uuid: $defUuid})
									MATCH (c:RagChunk {uuid: $chunkUuid})
									CREATE (def)-[:DEFINES]->(c)
								`, map[string]interface{}{"defUuid": defUUID, "chunkUuid": chunkUUIDs[j]})
							}
						}
					}
				}
			}
		}
	}

	// Step 14: Create REFERENCES edges from cross-references
	if activeRels["REFERENCES"] {
		refEdges := ResolveInternalReferences(chunks, sections)
		for _, edge := range refEdges {
			_, err := s.graphRepo.ExecuteWrite(ctx, "", `
				MATCH (src:RagChunk {uuid: $srcUuid})
				MATCH (tgt:RagSection {documentUuid: $docUuid, numbering: $numbering})
				CREATE (src)-[:REFERENCES {referenceText: $refText}]->(tgt)
			`, map[string]interface{}{
				"srcUuid":   chunkUUIDs[edge.SourceChunkIdx],
				"docUuid":   docUUID,
				"numbering": edge.TargetNumber,
				"refText":   edge.ReferenceText,
			})
			if err != nil {
				s.logger.Warn("failed to create REFERENCES edge", slog.String("error", err.Error()))
			}
		}
	}

	// Step 15: Compute SIMILAR_TO edges (intra-document)
	if activeRels["SIMILAR_TO"] {
		simEdges := ComputeSimilarityEdges(embeddings, s.similarityThreshold)
		for _, edge := range simEdges {
			_, err := s.graphRepo.ExecuteWrite(ctx, "", `
				MATCH (a:RagChunk {uuid: $aUuid})
				MATCH (b:RagChunk {uuid: $bUuid})
				CREATE (a)-[:SIMILAR_TO {similarity: $sim}]->(b)
			`, map[string]interface{}{
				"aUuid": chunkUUIDs[edge.ChunkIdxA],
				"bUuid": chunkUUIDs[edge.ChunkIdxB],
				"sim":   edge.Similarity,
			})
			if err != nil {
				s.logger.Warn("failed to create SIMILAR_TO edge", slog.String("error", err.Error()))
			}
		}
	}

	// Step 15b: Compute cross-document SIMILAR_TO edges
	// For each new chunk, find similar chunks from OTHER documents via vector search
	if activeRels["SIMILAR_TO"] {
		crossDocStart := time.Now()
		crossDocEdges := 0
		topKCross := 3 // top 3 similar chunks from other docs per chunk

		for i, embedding := range embeddings {
			result, err := s.graphRepo.ExecuteRead(ctx, "", `
				CALL vector_search.search('rag_chunk_embedding', $topK, $queryVector)
				YIELD node, similarity
				WITH node, similarity
				WHERE similarity >= $minSim AND node.documentUuid <> $docUuid
				RETURN node.uuid AS chunkUuid, similarity
				ORDER BY similarity DESC
			`, map[string]interface{}{
				"topK":        topKCross + 5, // fetch extra to account for same-doc filtering
				"queryVector": embedding,
				"minSim":      s.similarityThreshold,
				"docUuid":     docUUID,
			})
			if err != nil {
				continue
			}

			created := 0
			for _, row := range result.Rows {
				if created >= topKCross {
					break
				}
				targetUUID, _ := row["chunkUuid"].(string)
				sim, _ := row["similarity"].(float64)
				if targetUUID == "" {
					continue
				}

				_, err := s.graphRepo.ExecuteWrite(ctx, "", `
					MATCH (a:RagChunk {uuid: $aUuid})
					MATCH (b:RagChunk {uuid: $bUuid})
					CREATE (a)-[:SIMILAR_TO {similarity: $sim, crossDocument: true}]->(b)
				`, map[string]interface{}{
					"aUuid": chunkUUIDs[i],
					"bUuid": targetUUID,
					"sim":   sim,
				})
				if err == nil {
					crossDocEdges++
					created++
				}
			}
		}

		s.logger.Info("cross-document similarity completed",
			slog.Int("edges", crossDocEdges),
			slog.Int("chunks", len(embeddings)),
			slog.Int64("timeMs", time.Since(crossDocStart).Milliseconds()),
		)
	}

	// Step 16: Ensure vector indexes exist
	dims := embProvider.Dimensions()
	err = s.graphRepo.ExecuteAutoCommit(ctx, "", fmt.Sprintf(
		`CREATE VECTOR INDEX rag_chunk_embedding ON :RagChunk(embedding) WITH CONFIG {"dimension": %d, "capacity": 100000, "metric": "cos"}`,
		dims,
	), nil)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		s.logger.Warn("vector index creation note", slog.String("error", err.Error()))
	}

	err = s.graphRepo.ExecuteAutoCommit(ctx, "", fmt.Sprintf(
		`CREATE VECTOR INDEX rag_definition_embedding ON :RagDefinition(embedding) WITH CONFIG {"dimension": %d, "capacity": 50000, "metric": "cos"}`,
		dims,
	), nil)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		s.logger.Warn("definition vector index note", slog.String("error", err.Error()))
	}

	// Create property indexes
	for _, idx := range []string{
		"CREATE INDEX ON :RagSection(documentUuid)",
		"CREATE INDEX ON :RagSection(numbering)",
		"CREATE INDEX ON :RagChunk(documentUuid)",
		"CREATE INDEX ON :RagChunk(requirementLevel)",
		"CREATE INDEX ON :RagChunk(nodeType)",
		"CREATE INDEX ON :RagDefinition(documentUuid)",
		"CREATE INDEX ON :RagDefinition(term)",
	} {
		_ = s.graphRepo.ExecuteAutoCommit(ctx, "", idx, nil)
	}

	// Step 17: Mark as completed
	if err := s.docRepo.UpdateCompleted(ctx, docUUID, len(chunks)); err != nil {
		s.logger.Error("failed to update document status", slog.String("error", err.Error()))
		return
	}

	s.logger.Info("document ingestion completed",
		slog.String("uuid", docUUID),
		slog.Int("chunks", len(chunks)),
		slog.Int("sections", len(sections)),
		slog.Int("activeRels", len(activeRels)),
		slog.Int("dimensions", dims),
	)
}

func (s *ingestionService) failDocument(ctx context.Context, docUUID, errMsg string) {
	s.logger.Error("document ingestion failed", slog.String("uuid", docUUID), slog.String("error", errMsg))
	_ = s.docRepo.UpdateStatus(ctx, docUUID, "failed", errMsg)
}

func (s *ingestionService) ListDocuments(ctx context.Context, status, isoStandard string) ([]models.RagDocument, error) {
	return s.docRepo.List(ctx, status, isoStandard)
}

func (s *ingestionService) GetDocument(ctx context.Context, uuid string) (*models.RagDocument, error) {
	return s.docRepo.GetByUUID(ctx, uuid)
}

func (s *ingestionService) UpdateDocument(ctx context.Context, docUUID string, title, isoStandard, version *string) (*models.RagDocument, error) {
	doc, err := s.docRepo.UpdateMetadata(ctx, docUUID, title, isoStandard, version)
	if err != nil {
		return nil, err
	}

	// Sync updated fields to Memgraph node
	updates := map[string]interface{}{"uuid": docUUID}
	setClauses := []string{}
	if title != nil {
		updates["title"] = *title
		setClauses = append(setClauses, "d.title = $title")
	}
	if isoStandard != nil {
		updates["isoStandard"] = *isoStandard
		setClauses = append(setClauses, "d.isoStandard = $isoStandard")
	}
	if version != nil {
		updates["version"] = *version
		setClauses = append(setClauses, "d.version = $version")
	}

	if len(setClauses) > 0 {
		cypher := fmt.Sprintf("MATCH (d:RagDocument {uuid: $uuid}) SET %s", strings.Join(setClauses, ", "))
		if _, err := s.graphRepo.ExecuteWrite(ctx, "", cypher, updates); err != nil {
			s.logger.Warn("failed to sync document metadata to graph", slog.String("error", err.Error()))
		}
	}

	return doc, nil
}

func (s *ingestionService) GetDocumentChunks(ctx context.Context, docUUID string) ([]models.RagChunk, error) {
	if _, err := s.docRepo.GetByUUID(ctx, docUUID); err != nil {
		return nil, err
	}

	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (c:RagChunk {documentUuid: $uuid})
		RETURN c.uuid AS uuid, c.documentUuid AS documentUuid, c.text AS text,
		       c.position AS position, c.fullPath AS fullPath, c.nodeType AS nodeType,
		       c.numbering AS numbering, c.requirementLevel AS requirementLevel, c.depth AS depth
		ORDER BY c.position
	`, map[string]interface{}{"uuid": docUUID})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chunks: %w", err)
	}

	chunks := make([]models.RagChunk, 0, len(result.Rows))
	for _, row := range result.Rows {
		chunk := models.RagChunk{}
		if v, ok := row["uuid"].(string); ok {
			chunk.UUID = v
		}
		if v, ok := row["documentUuid"].(string); ok {
			chunk.DocumentUUID = v
		}
		if v, ok := row["text"].(string); ok {
			chunk.Text = v
		}
		if v, ok := row["position"].(int64); ok {
			chunk.Position = int(v)
		}
		if v, ok := row["fullPath"].(string); ok {
			chunk.FullPath = v
		}
		if v, ok := row["nodeType"].(string); ok {
			chunk.NodeType = v
		}
		if v, ok := row["numbering"].(string); ok {
			chunk.Numbering = v
		}
		if v, ok := row["requirementLevel"].(string); ok {
			chunk.RequirementLevel = v
		}
		if v, ok := row["depth"].(int64); ok {
			chunk.Depth = int(v)
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func (s *ingestionService) GetDocumentSections(ctx context.Context, docUUID string) ([]models.RagSection, error) {
	if _, err := s.docRepo.GetByUUID(ctx, docUUID); err != nil {
		return nil, err
	}

	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (s:RagSection {documentUuid: $uuid})
		RETURN s.uuid AS uuid, s.documentUuid AS documentUuid, s.nodeType AS nodeType,
		       s.numbering AS numbering, s.title AS title, s.depth AS depth,
		       s.fullPath AS fullPath, s.position AS position
		ORDER BY s.position
	`, map[string]interface{}{"uuid": docUUID})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sections: %w", err)
	}

	sections := make([]models.RagSection, 0, len(result.Rows))
	for _, row := range result.Rows {
		sec := models.RagSection{}
		if v, ok := row["uuid"].(string); ok {
			sec.UUID = v
		}
		if v, ok := row["documentUuid"].(string); ok {
			sec.DocumentUUID = v
		}
		if v, ok := row["nodeType"].(string); ok {
			sec.NodeType = v
		}
		if v, ok := row["numbering"].(string); ok {
			sec.Numbering = v
		}
		if v, ok := row["title"].(string); ok {
			sec.Title = v
		}
		if v, ok := row["depth"].(int64); ok {
			sec.Depth = int(v)
		}
		if v, ok := row["fullPath"].(string); ok {
			sec.FullPath = v
		}
		if v, ok := row["position"].(int64); ok {
			sec.Position = int(v)
		}
		sections = append(sections, sec)
	}

	return sections, nil
}

func (s *ingestionService) DeleteDocument(ctx context.Context, docUUID string) error {
	// Delete all graph nodes for this document
	_, err := s.graphRepo.ExecuteWrite(context.Background(), "", `
		MATCH (d:RagDocument {uuid: $uuid})
		OPTIONAL MATCH (d)-[:HAS_SECTION]->(s:RagSection)
		OPTIONAL MATCH (s)-[:CONTAINS*]->(child)
		OPTIONAL MATCH (d)-[:HAS_DEFINITION]->(def:RagDefinition)
		OPTIONAL MATCH (c:RagChunk {documentUuid: $uuid})
		DETACH DELETE c, child, s, def, d
	`, map[string]interface{}{"uuid": docUUID})
	if err != nil {
		s.logger.Warn("failed to delete graph nodes", slog.String("error", err.Error()))
	}

	return s.docRepo.Delete(context.Background(), docUUID)
}

func (s *ingestionService) ReprocessDocument(ctx context.Context, docUUID string) error {
	doc, err := s.docRepo.GetByUUID(ctx, docUUID)
	if err != nil {
		return fmt.Errorf("document not found: %w", err)
	}

	// Can only reprocess completed or failed documents
	if doc.Status != "completed" && doc.Status != "failed" {
		return fmt.Errorf("cannot reprocess document with status %q", doc.Status)
	}

	// Delete existing graph nodes
	_, err = s.graphRepo.ExecuteWrite(ctx, "", `
		MATCH (d:RagDocument {uuid: $uuid})
		OPTIONAL MATCH (d)-[:HAS_SECTION]->(s:RagSection)
		OPTIONAL MATCH (s)-[:CONTAINS*]->(child)
		OPTIONAL MATCH (d)-[:HAS_DEFINITION]->(def:RagDefinition)
		OPTIONAL MATCH (c:RagChunk {documentUuid: $uuid})
		DETACH DELETE c, child, s, def, d
	`, map[string]interface{}{"uuid": docUUID})
	if err != nil {
		s.logger.Warn("failed to delete old graph nodes for reprocess", slog.String("error", err.Error()))
	}

	// Reset status
	_ = s.docRepo.UpdateStatus(ctx, docUUID, "pending", "")

	// We need the file data — but we don't store it. Return error for now.
	// The user must re-upload the file to reprocess.
	return fmt.Errorf("reprocessing requires re-uploading the file — graph nodes have been cleared, please upload the document again")
}

// GetDocumentRelations returns cross-document SIMILAR_TO relationships for a document.
func (s *ingestionService) GetDocumentRelations(ctx context.Context, docUUID string) ([]models.RelatedDocSummary, []models.CrossDocLink, error) {
	// Query all cross-document SIMILAR_TO edges originating from this document's chunks
	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (src:RagChunk {documentUuid: $docUuid})-[r:SIMILAR_TO]-(tgt:RagChunk)
		WHERE tgt.documentUuid <> $docUuid
		OPTIONAL MATCH (tgtDoc:RagDocument {uuid: tgt.documentUuid})
		RETURN src.uuid AS srcUuid, src.fullPath AS srcPath, src.text AS srcText,
		       tgt.uuid AS tgtUuid, tgt.fullPath AS tgtPath, tgt.text AS tgtText,
		       tgt.documentUuid AS tgtDocUuid, tgtDoc.title AS tgtDocTitle,
		       tgtDoc.isoStandard AS tgtIsoStandard, r.similarity AS similarity
		ORDER BY similarity DESC
	`, map[string]interface{}{"docUuid": docUUID})
	if err != nil {
		return nil, nil, fmt.Errorf("query cross-doc relations: %w", err)
	}

	// Build links and aggregate per-document summaries
	var links []models.CrossDocLink
	docStats := make(map[string]*models.RelatedDocSummary)

	for _, row := range result.Rows {
		srcText, _ := row["srcText"].(string)
		tgtText, _ := row["tgtText"].(string)

		// Truncate text for readability
		if len(srcText) > 200 {
			srcText = srcText[:200] + "..."
		}
		if len(tgtText) > 200 {
			tgtText = tgtText[:200] + "..."
		}

		link := models.CrossDocLink{
			SourceChunkUUID: strVal(row, "srcUuid"),
			SourceFullPath:  strVal(row, "srcPath"),
			SourceText:      srcText,
			TargetChunkUUID: strVal(row, "tgtUuid"),
			TargetFullPath:  strVal(row, "tgtPath"),
			TargetText:      tgtText,
			TargetDocUUID:   strVal(row, "tgtDocUuid"),
			TargetDocTitle:  strVal(row, "tgtDocTitle"),
		}
		if v, ok := row["similarity"].(float64); ok {
			link.Similarity = v
		}
		links = append(links, link)

		// Aggregate per target document
		tgtDocUUID := link.TargetDocUUID
		if _, exists := docStats[tgtDocUUID]; !exists {
			docStats[tgtDocUUID] = &models.RelatedDocSummary{
				DocumentUUID:  tgtDocUUID,
				DocumentTitle: link.TargetDocTitle,
				ISOStandard:   strVal(row, "tgtIsoStandard"),
			}
		}
		ds := docStats[tgtDocUUID]
		ds.LinkCount++
		ds.AvgSimilarity += link.Similarity
		if link.Similarity > ds.MaxSimilarity {
			ds.MaxSimilarity = link.Similarity
		}
	}

	// Finalize averages
	var summaries []models.RelatedDocSummary
	for _, ds := range docStats {
		if ds.LinkCount > 0 {
			ds.AvgSimilarity /= float64(ds.LinkCount)
		}
		summaries = append(summaries, *ds)
	}

	return summaries, links, nil
}

func strVal(row map[string]interface{}, key string) string {
	if v, ok := row[key].(string); ok {
		return v
	}
	return ""
}
