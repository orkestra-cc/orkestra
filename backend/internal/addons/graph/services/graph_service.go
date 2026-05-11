package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/orkestra/backend/internal/addons/graph/models"
	"github.com/orkestra/backend/internal/addons/graph/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// GraphService defines the interface for graph operations
type GraphService interface {
	ExecuteQuery(ctx context.Context, database, cypher string, params map[string]interface{}, readOnly bool) (*iface.QueryResult, error)
	ListDatabases(ctx context.Context) ([]models.DatabaseInfo, error)
	GetSchema(ctx context.Context, database string) (*models.SchemaInfo, error)
	BrowseNodes(ctx context.Context, database string, labels []string, limit, skip int) (*iface.QueryResult, error)
	BrowseRelationships(ctx context.Context, database string, types []string, limit, skip int) (*iface.QueryResult, error)
	GetNodeNeighbors(ctx context.Context, database string, nodeID int64, depth, limit int) (*iface.GraphData, error)
	DeleteNode(ctx context.Context, database string, nodeID int64) (nodesDeleted int, relsDeleted int, err error)
	DeleteRelationship(ctx context.Context, database string, relationshipID int64) error
	HealthCheck(ctx context.Context) error
}

type graphService struct {
	repo   repository.GraphRepository
	logger *slog.Logger
}

// NewGraphService creates a new GraphService
func NewGraphService(repo repository.GraphRepository, logger *slog.Logger) GraphService {
	return &graphService{
		repo:   repo,
		logger: logger.With(slog.String("module", "graph")),
	}
}

// writeKeywords are Cypher keywords that indicate a write operation
var writeKeywords = []string{"CREATE", "MERGE", "DELETE", "DETACH", "SET ", "REMOVE", "DROP", "CALL {"}

func (s *graphService) ExecuteQuery(ctx context.Context, database, cypher string, params map[string]interface{}, readOnly bool) (*iface.QueryResult, error) {
	if strings.TrimSpace(cypher) == "" {
		return nil, fmt.Errorf("cypher query cannot be empty")
	}

	// Determine if the query is a write operation
	isWrite := false
	upperCypher := strings.ToUpper(strings.TrimSpace(cypher))
	for _, keyword := range writeKeywords {
		if strings.Contains(upperCypher, keyword) {
			isWrite = true
			break
		}
	}

	// If readOnly is requested but query contains write keywords, reject
	if readOnly && isWrite {
		return nil, fmt.Errorf("query contains write operations but readOnly mode was requested")
	}

	s.logger.Info("executing query",
		slog.String("database", database),
		slog.Bool("readOnly", readOnly),
		slog.Bool("detectedWrite", isWrite),
	)

	if isWrite {
		return s.repo.ExecuteWrite(ctx, database, cypher, params)
	}
	return s.repo.ExecuteRead(ctx, database, cypher, params)
}

func (s *graphService) ListDatabases(ctx context.Context) ([]models.DatabaseInfo, error) {
	return s.repo.ListDatabases(ctx)
}

func (s *graphService) GetSchema(ctx context.Context, database string) (*models.SchemaInfo, error) {
	return s.repo.GetSchema(ctx, database)
}

func (s *graphService) BrowseNodes(ctx context.Context, database string, labels []string, limit, skip int) (*iface.QueryResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	cypher := "MATCH (n"
	if len(labels) > 0 {
		escapedLabels := make([]string, len(labels))
		for i, l := range labels {
			escapedLabels[i] = fmt.Sprintf("`%s`", l)
		}
		cypher += ":" + strings.Join(escapedLabels, ":")
	}
	cypher += ") RETURN n SKIP $skip LIMIT $limit"

	params := map[string]interface{}{
		"skip":  skip,
		"limit": limit,
	}

	return s.repo.ExecuteRead(ctx, database, cypher, params)
}

func (s *graphService) BrowseRelationships(ctx context.Context, database string, types []string, limit, skip int) (*iface.QueryResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var cypher string
	if len(types) > 0 {
		escapedTypes := make([]string, len(types))
		for i, t := range types {
			escapedTypes[i] = fmt.Sprintf("`%s`", t)
		}
		cypher = fmt.Sprintf("MATCH (a)-[r:%s]->(b) RETURN a, r, b SKIP $skip LIMIT $limit", strings.Join(escapedTypes, "|"))
	} else {
		cypher = "MATCH (a)-[r]->(b) RETURN a, r, b SKIP $skip LIMIT $limit"
	}

	params := map[string]interface{}{
		"skip":  skip,
		"limit": limit,
	}

	return s.repo.ExecuteRead(ctx, database, cypher, params)
}

func (s *graphService) GetNodeNeighbors(ctx context.Context, database string, nodeID int64, depth, limit int) (*iface.GraphData, error) {
	if depth <= 0 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	cypher := fmt.Sprintf(
		"MATCH path = (n)-[*1..%d]-(m) WHERE id(n) = $nodeId RETURN path LIMIT $limit",
		depth,
	)
	params := map[string]interface{}{
		"nodeId": nodeID,
		"limit":  limit,
	}

	result, err := s.repo.ExecuteRead(ctx, database, cypher, params)
	if err != nil {
		return nil, err
	}

	if result.Graph != nil {
		return result.Graph, nil
	}
	return &iface.GraphData{}, nil
}

func (s *graphService) DeleteNode(ctx context.Context, database string, nodeID int64) (int, int, error) {
	cypher := "MATCH (n) WHERE id(n) = $nodeId DETACH DELETE n"
	params := map[string]interface{}{
		"nodeId": nodeID,
	}

	s.logger.Info("deleting node",
		slog.String("database", database),
		slog.Int64("nodeId", nodeID),
	)

	result, err := s.repo.ExecuteWrite(ctx, database, cypher, params)
	if err != nil {
		return 0, 0, err
	}

	if result.Metadata.NodesDeleted == 0 {
		return 0, 0, fmt.Errorf("node with id %d not found", nodeID)
	}

	return result.Metadata.NodesDeleted, result.Metadata.RelationshipsDeleted, nil
}

func (s *graphService) DeleteRelationship(ctx context.Context, database string, relationshipID int64) error {
	cypher := "MATCH ()-[r]->() WHERE id(r) = $relId DELETE r"
	params := map[string]interface{}{
		"relId": relationshipID,
	}

	s.logger.Info("deleting relationship",
		slog.String("database", database),
		slog.Int64("relationshipId", relationshipID),
	)

	result, err := s.repo.ExecuteWrite(ctx, database, cypher, params)
	if err != nil {
		return err
	}

	if result.Metadata.RelationshipsDeleted == 0 {
		return fmt.Errorf("relationship with id %d not found", relationshipID)
	}

	return nil
}

func (s *graphService) HealthCheck(ctx context.Context) error {
	return s.repo.VerifyConnectivity(ctx)
}
