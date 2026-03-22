package services

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	graphRepo "github.com/orkestra/backend/internal/graph/repository"
	"github.com/orkestra/backend/internal/rag/models"
	"github.com/orkestra/backend/internal/rag/repository"
)

// IngestionService manages document ingestion into the knowledge graph
type IngestionService interface {
	IngestDocument(ctx context.Context, title, fileName string, fileData []byte, isoStandard, version string, chunkSize, chunkOverlap int) (*models.RagDocument, error)
	ListDocuments(ctx context.Context, status, isoStandard string) ([]models.RagDocument, error)
	GetDocument(ctx context.Context, uuid string) (*models.RagDocument, error)
	UpdateDocument(ctx context.Context, uuid string, title, isoStandard, version *string) (*models.RagDocument, error)
	GetDocumentChunks(ctx context.Context, uuid string) ([]models.RagChunk, error)
	DeleteDocument(ctx context.Context, uuid string) error
}

type ingestionService struct {
	docRepo        repository.DocumentRepository
	graphRepo      graphRepo.GraphRepository
	modelProvider  AIModelProvider
	extractor      TextExtractor
	defaultChunk   int
	defaultOverlap int
	logger         *slog.Logger
}

// NewIngestionService creates a new IngestionService
func NewIngestionService(
	docRepo repository.DocumentRepository,
	gr graphRepo.GraphRepository,
	modelProvider AIModelProvider,
	extractor TextExtractor,
	defaultChunk, defaultOverlap int,
	logger *slog.Logger,
) IngestionService {
	return &ingestionService{
		docRepo:        docRepo,
		graphRepo:      gr,
		modelProvider:  modelProvider,
		extractor:      extractor,
		defaultChunk:   defaultChunk,
		defaultOverlap: defaultOverlap,
		logger:         logger.With(slog.String("module", "rag-ingestion")),
	}
}

func (s *ingestionService) IngestDocument(ctx context.Context, title, fileName string, fileData []byte, isoStandard, version string, chunkSize, chunkOverlap int) (*models.RagDocument, error) {
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

	// Create document record
	doc := &models.RagDocument{
		UUID:         uuid.New().String(),
		Title:        title,
		FileName:     fileName,
		FileSize:     int64(len(fileData)),
		ISOStandard:  isoStandard,
		Version:      version,
		DocType:      ext,
		Status:       "pending",
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		ModelUUID:    "", // Will be set from provider
	}

	if err := s.docRepo.Create(ctx, doc); err != nil {
		return nil, err
	}

	s.logger.Info("document ingestion started",
		slog.String("uuid", doc.UUID),
		slog.String("title", title),
		slog.String("fileName", fileName),
	)

	// Process in background goroutine
	go s.processDocument(doc.UUID, fileData, ext, chunkSize, chunkOverlap, embProvider)

	return doc, nil
}

func (s *ingestionService) processDocument(docUUID string, fileData []byte, docType string, chunkSize, chunkOverlap int, embProvider interface{ Embed(context.Context, string) ([]float64, error); Dimensions() int; ModelName() string }) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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

	// Step 2: Chunk text
	s.logger.Info("chunking text", slog.String("uuid", docUUID), slog.Int("textLen", len(text)))
	chunks := ChunkText(text, chunkSize, chunkOverlap)
	if len(chunks) == 0 {
		s.failDocument(ctx, docUUID, "no chunks produced from text")
		return
	}

	s.logger.Info("generating embeddings", slog.String("uuid", docUUID), slog.Int("chunks", len(chunks)))

	// Step 3: Generate embeddings
	embeddings := make([][]float64, len(chunks))
	for i, chunk := range chunks {
		emb, err := embProvider.Embed(ctx, chunk.Text)
		if err != nil {
			s.failDocument(ctx, docUUID, fmt.Sprintf("embedding failed for chunk %d: %v", i, err))
			return
		}
		embeddings[i] = emb
	}

	// Step 4: Create graph nodes in Memgraph
	s.logger.Info("creating graph nodes", slog.String("uuid", docUUID))

	// Get document metadata for the graph node
	doc, err := s.docRepo.GetByUUID(ctx, docUUID)
	if err != nil {
		s.failDocument(ctx, docUUID, "failed to retrieve document: "+err.Error())
		return
	}

	// Create document node
	_, err = s.graphRepo.ExecuteWrite(ctx, "", `
		CREATE (d:RagDocument {
			uuid: $uuid,
			title: $title,
			isoStandard: $isoStandard,
			version: $version,
			docType: $docType,
			chunkCount: $chunkCount,
			createdAt: $createdAt
		})
	`, map[string]interface{}{
		"uuid":        docUUID,
		"title":       doc.Title,
		"isoStandard": doc.ISOStandard,
		"version":     doc.Version,
		"docType":     doc.DocType,
		"chunkCount":  len(chunks),
		"createdAt":   doc.CreatedAt.Format(time.RFC3339),
	})
	if err != nil {
		s.failDocument(ctx, docUUID, "failed to create document node: "+err.Error())
		return
	}

	// Create chunk nodes with embeddings and relationships
	for i, chunk := range chunks {
		chunkUUID := uuid.New().String()
		_, err := s.graphRepo.ExecuteWrite(ctx, "", `
			MATCH (d:RagDocument {uuid: $docUuid})
			CREATE (c:RagChunk {
				uuid: $chunkUuid,
				documentUuid: $docUuid,
				text: $text,
				position: $position,
				sectionTitle: $sectionTitle,
				embedding: $embedding
			})
			CREATE (d)-[:HAS_CHUNK]->(c)
		`, map[string]interface{}{
			"docUuid":      docUUID,
			"chunkUuid":    chunkUUID,
			"text":         chunk.Text,
			"position":     chunk.Position,
			"sectionTitle": chunk.SectionTitle,
			"embedding":    embeddings[i],
		})
		if err != nil {
			s.failDocument(ctx, docUUID, fmt.Sprintf("failed to create chunk node %d: %v", i, err))
			return
		}
	}

	// Create NEXT relationships between sequential chunks
	_, err = s.graphRepo.ExecuteWrite(ctx, "", `
		MATCH (d:RagDocument {uuid: $docUuid})-[:HAS_CHUNK]->(c:RagChunk)
		WITH c ORDER BY c.position
		WITH collect(c) AS chunks
		UNWIND range(0, size(chunks)-2) AS i
		WITH chunks[i] AS a, chunks[i+1] AS b
		CREATE (a)-[:NEXT]->(b)
	`, map[string]interface{}{"docUuid": docUUID})
	if err != nil {
		s.logger.Warn("failed to create NEXT relationships", slog.String("error", err.Error()))
	}

	// Step 5: Ensure vector index exists (must use auto-commit for Memgraph)
	dims := embProvider.Dimensions()
	err = s.graphRepo.ExecuteAutoCommit(ctx, "", fmt.Sprintf(
		`CREATE VECTOR INDEX rag_chunk_embedding ON :RagChunk(embedding) WITH CONFIG {"dimension": %d, "capacity": 100000, "metric": "cos"}`,
		dims,
	), nil)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		s.logger.Warn("vector index creation note", slog.String("error", err.Error()))
	}

	// Step 6: Mark as completed
	if err := s.docRepo.UpdateCompleted(ctx, docUUID, len(chunks)); err != nil {
		s.logger.Error("failed to update document status", slog.String("error", err.Error()))
		return
	}

	s.logger.Info("document ingestion completed",
		slog.String("uuid", docUUID),
		slog.Int("chunks", len(chunks)),
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
	// Verify document exists
	if _, err := s.docRepo.GetByUUID(ctx, docUUID); err != nil {
		return nil, err
	}

	result, err := s.graphRepo.ExecuteRead(ctx, "", `
		MATCH (d:RagDocument {uuid: $uuid})-[:HAS_CHUNK]->(c:RagChunk)
		RETURN c.uuid AS uuid, c.documentUuid AS documentUuid, c.text AS text, c.position AS position, c.sectionTitle AS sectionTitle
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
		if v, ok := row["sectionTitle"].(string); ok {
			chunk.SectionTitle = v
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func (s *ingestionService) DeleteDocument(ctx context.Context, docUUID string) error {
	// Delete from Memgraph first
	_, err := s.graphRepo.ExecuteWrite(context.Background(), "", `
		MATCH (d:RagDocument {uuid: $uuid})
		OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c:RagChunk)
		DETACH DELETE c, d
	`, map[string]interface{}{"uuid": docUUID})
	if err != nil {
		s.logger.Warn("failed to delete graph nodes", slog.String("error", err.Error()))
	}

	// Delete from MongoDB
	return s.docRepo.Delete(context.Background(), docUUID)
}
