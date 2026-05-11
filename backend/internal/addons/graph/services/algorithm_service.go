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

// AlgorithmService defines the interface for graph algorithm operations (MAGE)
type AlgorithmService interface {
	RunAlgorithm(ctx context.Context, database string, req models.AlgorithmRequest) (*iface.QueryResult, error)
	ListAlgorithms(ctx context.Context) []models.AlgorithmInfo
}

// mageAlgorithmDef defines how to call a MAGE algorithm
type mageAlgorithmDef struct {
	Procedure string // MAGE procedure name (e.g. "pagerank.get")
	Yield     string // YIELD clause
	IsWrite   bool   // Whether the procedure writes to the graph
}

// mageAlgorithms maps algorithm names to their MAGE procedure definitions
var mageAlgorithms = map[string]mageAlgorithmDef{
	// Centrality
	"pageRank": {
		Procedure: "pagerank.get",
		Yield:     "YIELD node, rank",
	},
	"betweennessCentrality": {
		Procedure: "betweenness_centrality.get",
		Yield:     "YIELD node, betweenness_centrality",
	},
	// Community Detection
	"louvain": {
		Procedure: "community_detection.get",
		Yield:     "YIELD node, community_id",
	},
	"labelPropagation": {
		Procedure: "label_propagation.get",
		Yield:     "YIELD node, label",
	},
	"wcc": {
		Procedure: "weakly_connected_components.get",
		Yield:     "YIELD node, component_id",
	},
}

type algorithmService struct {
	repo   repository.GraphRepository
	logger *slog.Logger
}

// NewAlgorithmService creates a new AlgorithmService
func NewAlgorithmService(repo repository.GraphRepository, logger *slog.Logger) AlgorithmService {
	return &algorithmService{
		repo:   repo,
		logger: logger.With(slog.String("module", "graph-algorithms")),
	}
}

// isMageNotAvailable checks if the error indicates a MAGE procedure is not installed
func isMageNotAvailable(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "ProcedureNotFound") ||
		strings.Contains(err.Error(), "no procedure"))
}

func (s *algorithmService) RunAlgorithm(ctx context.Context, database string, req models.AlgorithmRequest) (*iface.QueryResult, error) {
	if req.Algorithm == "" {
		return nil, fmt.Errorf("algorithm name is required")
	}

	// Look up the MAGE algorithm definition
	algoDef, ok := mageAlgorithms[req.Algorithm]
	if !ok {
		return nil, fmt.Errorf("unknown algorithm: %s", req.Algorithm)
	}

	// Build the MAGE procedure call
	cypher := fmt.Sprintf("CALL %s() %s", algoDef.Procedure, algoDef.Yield)

	s.logger.Info("running MAGE algorithm",
		slog.String("algorithm", req.Algorithm),
		slog.String("procedure", algoDef.Procedure),
	)

	var (
		result *iface.QueryResult
		err    error
	)
	if algoDef.IsWrite {
		result, err = s.repo.ExecuteWrite(ctx, database, cypher, nil)
	} else {
		result, err = s.repo.ExecuteRead(ctx, database, cypher, nil)
	}
	if err != nil {
		if isMageNotAvailable(err) {
			return nil, fmt.Errorf("MAGE algorithm '%s' is not available — ensure the MAGE module is installed in Memgraph", req.Algorithm)
		}
		return nil, fmt.Errorf("algorithm execution failed: %w", err)
	}
	return result, nil
}

func (s *algorithmService) ListAlgorithms(_ context.Context) []models.AlgorithmInfo {
	return []models.AlgorithmInfo{
		// Centrality
		{Name: "pageRank", Category: "centrality", Procedure: "pagerank.get", Description: "Measures the importance of nodes based on incoming relationships"},
		{Name: "betweennessCentrality", Category: "centrality", Procedure: "betweenness_centrality.get", Description: "Measures how often a node lies on shortest paths between other nodes"},
		// Community Detection
		{Name: "louvain", Category: "community", Procedure: "community_detection.get", Description: "Detects communities using modularity optimization (Louvain method)"},
		{Name: "labelPropagation", Category: "community", Procedure: "label_propagation.get", Description: "Detects communities by propagating labels through the network"},
		{Name: "wcc", Category: "community", Procedure: "weakly_connected_components.get", Description: "Finds weakly connected components in the graph"},
	}
}
