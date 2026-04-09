package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/agents/models"
	"github.com/orkestra/backend/internal/addons/agents/services"
)

// ProjectHandler handles HTTP requests for agent projects
type ProjectHandler struct {
	projectService services.ProjectService
}

// NewProjectHandler creates a new ProjectHandler
func NewProjectHandler(projectService services.ProjectService) *ProjectHandler {
	return &ProjectHandler{projectService: projectService}
}

func (h *ProjectHandler) CreateProject(ctx context.Context, req *models.CreateProjectRequest) (*models.CreateProjectResponse, error) {
	// Extract user UUID from context (set by auth middleware)
	userUUID, _ := ctx.Value("userUUID").(string)

	project, err := h.projectService.CreateProject(ctx, req, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create project", err)
	}
	return &models.CreateProjectResponse{Body: *project}, nil
}

func (h *ProjectHandler) ListProjects(ctx context.Context, req *models.ListProjectsRequest) (*models.ListProjectsResponse, error) {
	projects, err := h.projectService.ListProjects(ctx, req.Status)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list projects", err)
	}
	resp := &models.ListProjectsResponse{}
	resp.Body.Projects = projects
	return resp, nil
}

func (h *ProjectHandler) GetProject(ctx context.Context, req *models.GetProjectRequest) (*models.GetProjectResponse, error) {
	project, err := h.projectService.GetProject(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Project not found", err)
	}
	return &models.GetProjectResponse{Body: *project}, nil
}

func (h *ProjectHandler) UpdateProject(ctx context.Context, req *models.UpdateProjectRequest) (*models.UpdateProjectResponse, error) {
	project, err := h.projectService.UpdateProject(ctx, req)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to update project", err)
	}
	return &models.UpdateProjectResponse{Body: *project}, nil
}

func (h *ProjectHandler) DeleteProject(ctx context.Context, req *models.DeleteProjectRequest) (*models.DeleteProjectResponse, error) {
	if err := h.projectService.DeleteProject(ctx, req.UUID); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete project", err)
	}
	resp := &models.DeleteProjectResponse{}
	resp.Body.Message = "Project deleted successfully"
	return resp, nil
}

func (h *ProjectHandler) AddDocuments(ctx context.Context, req *models.AddDocumentsRequest) (*models.AddDocumentsResponse, error) {
	project, err := h.projectService.AddDocuments(ctx, req.UUID, req.Body.DocumentUUIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to add documents", err)
	}
	return &models.AddDocumentsResponse{Body: *project}, nil
}

func (h *ProjectHandler) RemoveDocuments(ctx context.Context, req *models.RemoveDocumentsRequest) (*models.RemoveDocumentsResponse, error) {
	project, err := h.projectService.RemoveDocuments(ctx, req.UUID, req.Body.DocumentUUIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to remove documents", err)
	}
	return &models.RemoveDocumentsResponse{Body: *project}, nil
}

func (h *ProjectHandler) UpdateFilters(ctx context.Context, req *models.UpdateFiltersRequest) (*models.UpdateFiltersResponse, error) {
	project, err := h.projectService.UpdateFilters(ctx, req.UUID, req.Body.ISOStandards, req.Body.Categories)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to update filters", err)
	}
	return &models.UpdateFiltersResponse{Body: *project}, nil
}

func (h *ProjectHandler) UpdateSettings(ctx context.Context, req *models.UpdateSettingsRequest) (*models.UpdateSettingsResponse, error) {
	settings := &models.AgentSettings{}
	if req.Body.SystemPrompt != nil {
		settings.SystemPrompt = *req.Body.SystemPrompt
	}
	if req.Body.Directives != nil {
		settings.Directives = req.Body.Directives
	}
	if req.Body.Skepticism != nil {
		settings.Skepticism = *req.Body.Skepticism
	}
	if req.Body.Literalism != nil {
		settings.Literalism = *req.Body.Literalism
	}
	if req.Body.Empathy != nil {
		settings.Empathy = *req.Body.Empathy
	}
	if req.Body.MaxTokens != nil {
		settings.MaxTokens = *req.Body.MaxTokens
	}
	if req.Body.Temperature != nil {
		settings.Temperature = *req.Body.Temperature
	}
	if req.Body.Language != nil {
		settings.Language = *req.Body.Language
	}

	project, err := h.projectService.UpdateSettings(ctx, req.UUID, settings)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to update settings", err)
	}
	return &models.UpdateSettingsResponse{Body: *project}, nil
}

func (h *ProjectHandler) GetSettings(ctx context.Context, req *models.GetSettingsRequest) (*models.GetSettingsResponse, error) {
	project, err := h.projectService.GetProject(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Project not found", err)
	}
	resp := &models.GetSettingsResponse{}
	resp.Body.Settings = project.Settings
	return resp, nil
}
