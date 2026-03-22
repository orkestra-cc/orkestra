package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/graph/models"
	"github.com/orkestra/backend/internal/graph/services"
)

// GraphHandler handles all graph-related HTTP requests
type GraphHandler struct {
	graphService     services.GraphService
	algorithmService services.AlgorithmService
	vectorService    services.VectorService
	graphURI         string
}

// NewGraphHandler creates a new GraphHandler
func NewGraphHandler(graphSvc services.GraphService, algoSvc services.AlgorithmService, vectorSvc services.VectorService, graphURI string) *GraphHandler {
	return &GraphHandler{
		graphService:     graphSvc,
		algorithmService: algoSvc,
		vectorService:    vectorSvc,
		graphURI:         graphURI,
	}
}

// --- Core Endpoints ---

func (h *GraphHandler) HealthCheck(ctx context.Context, req *struct{}) (*models.HealthCheckResponse, error) {
	if err := h.graphService.HealthCheck(ctx); err != nil {
		return nil, huma.Error503ServiceUnavailable("Graph database is not reachable", err)
	}
	resp := &models.HealthCheckResponse{}
	resp.Body.Status = "connected"
	resp.Body.URI = h.graphURI
	return resp, nil
}

func (h *GraphHandler) ListDatabases(ctx context.Context, req *struct{}) (*models.ListDatabasesResponse, error) {
	databases, err := h.graphService.ListDatabases(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list databases", err)
	}
	resp := &models.ListDatabasesResponse{}
	resp.Body.Databases = databases
	return resp, nil
}

func (h *GraphHandler) GetSchema(ctx context.Context, req *models.GetSchemaRequest) (*models.GetSchemaResponse, error) {
	schema, err := h.graphService.GetSchema(ctx, req.Database)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get schema", err)
	}
	return &models.GetSchemaResponse{Body: *schema}, nil
}

func (h *GraphHandler) ExecuteQuery(ctx context.Context, req *models.ExecuteQueryRequest) (*models.ExecuteQueryResponse, error) {
	result, err := h.graphService.ExecuteQuery(ctx, req.Body.Database, req.Body.Cypher, req.Body.Params, req.Body.ReadOnly)
	if err != nil {
		return nil, huma.Error400BadRequest("Query execution failed", err)
	}
	return &models.ExecuteQueryResponse{Body: *result}, nil
}

func (h *GraphHandler) BrowseNodes(ctx context.Context, req *models.BrowseNodesRequest) (*models.BrowseNodesResponse, error) {
	result, err := h.graphService.BrowseNodes(ctx, req.Database, req.Labels, req.Limit, req.Skip)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to browse nodes", err)
	}
	return &models.BrowseNodesResponse{Body: *result}, nil
}

func (h *GraphHandler) BrowseRelationships(ctx context.Context, req *models.BrowseRelationshipsRequest) (*models.BrowseRelationshipsResponse, error) {
	result, err := h.graphService.BrowseRelationships(ctx, req.Database, req.Types, req.Limit, req.Skip)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to browse relationships", err)
	}
	return &models.BrowseRelationshipsResponse{Body: *result}, nil
}

func (h *GraphHandler) GetNodeNeighbors(ctx context.Context, req *models.GetNodeNeighborsRequest) (*models.GetNodeNeighborsResponse, error) {
	graphData, err := h.graphService.GetNodeNeighbors(ctx, req.Database, req.NodeID, req.Depth, req.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get node neighbors", err)
	}
	return &models.GetNodeNeighborsResponse{Body: *graphData}, nil
}

// --- Delete Endpoints ---

func (h *GraphHandler) DeleteNode(ctx context.Context, req *models.DeleteNodeRequest) (*models.DeleteNodeResponse, error) {
	nodesDeleted, relsDeleted, err := h.graphService.DeleteNode(ctx, req.Database, req.NodeID)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to delete node", err)
	}
	resp := &models.DeleteNodeResponse{}
	resp.Body.Message = "Node deleted successfully"
	resp.Body.NodesDeleted = nodesDeleted
	resp.Body.RelationshipsDeleted = relsDeleted
	return resp, nil
}

func (h *GraphHandler) DeleteRelationship(ctx context.Context, req *models.DeleteRelationshipRequest) (*models.DeleteRelationshipResponse, error) {
	if err := h.graphService.DeleteRelationship(ctx, req.Database, req.RelationshipID); err != nil {
		return nil, huma.Error400BadRequest("Failed to delete relationship", err)
	}
	resp := &models.DeleteRelationshipResponse{}
	resp.Body.Message = "Relationship deleted successfully"
	resp.Body.RelationshipsDeleted = 1
	return resp, nil
}

// --- Algorithm Endpoints ---

func (h *GraphHandler) RunAlgorithm(ctx context.Context, req *models.RunAlgorithmRequestDTO) (*models.RunAlgorithmResponse, error) {
	result, err := h.algorithmService.RunAlgorithm(ctx, req.Body.Database, req.Body.AlgorithmRequest)
	if err != nil {
		return nil, huma.Error400BadRequest("Algorithm execution failed", err)
	}
	return &models.RunAlgorithmResponse{Body: *result}, nil
}

func (h *GraphHandler) ListAlgorithms(ctx context.Context, req *struct{}) (*models.ListAlgorithmsResponse, error) {
	algorithms := h.algorithmService.ListAlgorithms(ctx)
	resp := &models.ListAlgorithmsResponse{}
	resp.Body.Algorithms = algorithms
	return resp, nil
}

// --- Vector Endpoints ---

func (h *GraphHandler) VectorSearch(ctx context.Context, req *models.VectorSearchRequestDTO) (*models.VectorSearchResponse, error) {
	result, err := h.vectorService.VectorSearch(ctx, req.Body.Database, req.Body.VectorSearchRequest)
	if err != nil {
		return nil, huma.Error400BadRequest("Vector search failed", err)
	}
	return &models.VectorSearchResponse{Body: *result}, nil
}

func (h *GraphHandler) ListVectorIndexes(ctx context.Context, req *models.ListVectorIndexesRequest) (*models.ListVectorIndexesResponse, error) {
	indexes, err := h.vectorService.ListVectorIndexes(ctx, req.Database)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list vector indexes", err)
	}
	resp := &models.ListVectorIndexesResponse{}
	resp.Body.Indexes = indexes
	return resp, nil
}

func (h *GraphHandler) CreateVectorIndex(ctx context.Context, req *models.CreateVectorIndexRequestDTO) (*models.CreateVectorIndexResponse, error) {
	if err := h.vectorService.CreateVectorIndex(ctx, req.Body.Database, req.Body.CreateVectorIndexRequest); err != nil {
		return nil, huma.Error400BadRequest("Failed to create vector index", err)
	}
	resp := &models.CreateVectorIndexResponse{}
	resp.Body.Message = "Vector index created successfully"
	return resp, nil
}

func (h *GraphHandler) DropVectorIndex(ctx context.Context, req *models.DropVectorIndexRequest) (*models.DropVectorIndexResponse, error) {
	if err := h.vectorService.DropVectorIndex(ctx, req.Database, req.Name); err != nil {
		return nil, huma.Error400BadRequest("Failed to drop vector index", err)
	}
	resp := &models.DropVectorIndexResponse{}
	resp.Body.Message = "Vector index dropped successfully"
	return resp, nil
}
