package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/orkestra/backend/internal/addons/graph/models"
	"github.com/orkestra/backend/internal/addons/graph/repository"
)

// VectorService defines the interface for vector search operations
type VectorService interface {
	VectorSearch(ctx context.Context, database string, req models.VectorSearchRequest) (*models.QueryResult, error)
	ListVectorIndexes(ctx context.Context, database string) ([]models.VectorIndex, error)
	CreateVectorIndex(ctx context.Context, database string, req models.CreateVectorIndexRequest) error
	DropVectorIndex(ctx context.Context, database, name string) error
}

type vectorService struct {
	repo   repository.GraphRepository
	logger *slog.Logger
}

// NewVectorService creates a new VectorService
func NewVectorService(repo repository.GraphRepository, logger *slog.Logger) VectorService {
	return &vectorService{
		repo:   repo,
		logger: logger.With(slog.String("module", "graph-vector")),
	}
}

func (s *vectorService) VectorSearch(ctx context.Context, database string, req models.VectorSearchRequest) (*models.QueryResult, error) {
	if req.IndexName == "" {
		return nil, fmt.Errorf("index name is required")
	}
	if len(req.QueryVector) == 0 {
		return nil, fmt.Errorf("query vector is required")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	if topK > 1000 {
		topK = 1000
	}

	// Memgraph vector search syntax
	cypher := "CALL vector_search.search($indexName, $topK, $queryVector) YIELD node, similarity RETURN node, similarity"
	params := map[string]interface{}{
		"indexName":   req.IndexName,
		"topK":       topK,
		"queryVector": req.QueryVector,
	}

	s.logger.Info("executing vector search",
		slog.String("index", req.IndexName),
		slog.Int("topK", topK),
		slog.Int("vectorDimensions", len(req.QueryVector)),
	)

	result, err := s.repo.ExecuteRead(ctx, database, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Filter by minimum score if specified
	if req.MinScore > 0 && result.Rows != nil {
		filtered := make([]map[string]interface{}, 0)
		for _, row := range result.Rows {
			if score, ok := row["similarity"].(float64); ok && score >= req.MinScore {
				filtered = append(filtered, row)
			}
		}
		result.Rows = filtered
		result.Metadata.ResultCount = len(filtered)
	}

	return result, nil
}

func (s *vectorService) ListVectorIndexes(ctx context.Context, database string) ([]models.VectorIndex, error) {
	// Memgraph uses SHOW INDEX INFO; filter for vector indexes in Go
	result, err := s.repo.ExecuteRead(ctx, database, "SHOW INDEX INFO", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexes: %w", err)
	}

	var indexes []models.VectorIndex
	for _, row := range result.Rows {
		// Only include vector indexes
		indexType := ""
		if v, ok := row["index type"].(string); ok {
			indexType = v
		}
		if !strings.Contains(strings.ToLower(indexType), "vector") {
			continue
		}

		idx := models.VectorIndex{
			State: "ONLINE",
		}
		if v, ok := row["label"].(string); ok {
			idx.Label = v
		}
		if v, ok := row["property"].(string); ok {
			idx.Property = v
		}
		// Generate a name from label + property
		if idx.Label != "" && idx.Property != "" {
			idx.Name = fmt.Sprintf("%s_%s", idx.Label, idx.Property)
		}
		indexes = append(indexes, idx)
	}

	return indexes, nil
}

func (s *vectorService) CreateVectorIndex(ctx context.Context, database string, req models.CreateVectorIndexRequest) error {
	if req.Name == "" || req.Label == "" || req.Property == "" || req.Dimensions <= 0 {
		return fmt.Errorf("name, label, property, and dimensions are required")
	}

	similarity := req.Similarity
	if similarity == "" {
		similarity = "cos"
	}

	capacity := req.Capacity
	if capacity <= 0 {
		capacity = 10000
	}

	// Memgraph vector index syntax
	cypher := fmt.Sprintf(
		`CREATE VECTOR INDEX %s ON :%s(%s) WITH CONFIG {"dimension": %d, "capacity": %d, "metric": "%s"}`,
		req.Name, req.Label, req.Property, req.Dimensions, capacity, similarity,
	)

	s.logger.Info("creating vector index",
		slog.String("name", req.Name),
		slog.String("label", req.Label),
		slog.String("property", req.Property),
		slog.Int("dimensions", req.Dimensions),
		slog.Int("capacity", capacity),
		slog.String("metric", similarity),
	)

	_, err := s.repo.ExecuteWrite(ctx, database, cypher, nil)
	if err != nil {
		return fmt.Errorf("failed to create vector index: %w", err)
	}
	return nil
}

func (s *vectorService) DropVectorIndex(ctx context.Context, database, name string) error {
	if name == "" {
		return fmt.Errorf("index name is required")
	}

	cypher := fmt.Sprintf("DROP INDEX %s", name)

	s.logger.Info("dropping vector index", slog.String("name", name))

	_, err := s.repo.ExecuteWrite(ctx, database, cypher, nil)
	if err != nil {
		return fmt.Errorf("failed to drop vector index: %w", err)
	}
	return nil
}
