package services

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/agents/models"
	"github.com/orkestra/backend/internal/agents/repository"
)

// ProjectService handles project CRUD and Hindsight bank lifecycle
type ProjectService interface {
	CreateProject(ctx context.Context, req *models.CreateProjectRequest, userUUID string) (*models.Project, error)
	GetProject(ctx context.Context, uuid string) (*models.Project, error)
	ListProjects(ctx context.Context, status string) ([]models.Project, error)
	UpdateProject(ctx context.Context, req *models.UpdateProjectRequest) (*models.Project, error)
	DeleteProject(ctx context.Context, uuid string) error
	AddDocuments(ctx context.Context, uuid string, documentUUIDs []string) (*models.Project, error)
	RemoveDocuments(ctx context.Context, uuid string, documentUUIDs []string) (*models.Project, error)
	UpdateFilters(ctx context.Context, uuid string, isoStandards, categories []string) (*models.Project, error)
	UpdateSettings(ctx context.Context, uuid string, settings *models.AgentSettings) (*models.Project, error)
}

type projectService struct {
	repo      repository.ProjectRepository
	hsClient  HindsightClient
	namespace string
	logger    *slog.Logger
}

// NewProjectService creates a new ProjectService
func NewProjectService(repo repository.ProjectRepository, hsClient HindsightClient, namespace string, logger *slog.Logger) ProjectService {
	return &projectService{
		repo:      repo,
		hsClient:  hsClient,
		namespace: namespace,
		logger:    logger.With(slog.String("module", "agents-project")),
	}
}

// bankID generates the Hindsight bank ID for a project
func (s *projectService) bankID(projectUUID string) string {
	return fmt.Sprintf("%s-project-%s", s.namespace, projectUUID)
}

func (s *projectService) CreateProject(ctx context.Context, req *models.CreateProjectRequest, userUUID string) (*models.Project, error) {
	project := &models.Project{
		Name:          req.Body.Name,
		Description:   req.Body.Description,
		DocumentUUIDs: req.Body.DocumentUUIDs,
		ISOStandards:  req.Body.ISOStandards,
		Categories:    req.Body.Categories,
		CreatedBy:     userUUID,
		Status:        models.ProjectStatusActive,
	}

	if err := s.repo.Create(ctx, project); err != nil {
		return nil, err
	}

	// Create Hindsight bank with project description as mission
	project.HindsightBankID = s.bankID(project.UUID)
	if err := s.hsClient.CreateOrUpdateBank(ctx, project.HindsightBankID, project.Name, project.Description, nil); err != nil {
		s.logger.Warn("Failed to create Hindsight bank, project created in degraded mode",
			slog.String("projectUUID", project.UUID),
			slog.String("error", err.Error()),
		)
	}

	// Persist the bank ID
	updated, err := s.repo.Update(ctx, project.UUID, bson.M{"hindsightBankId": project.HindsightBankID})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (s *projectService) GetProject(ctx context.Context, uuid string) (*models.Project, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

func (s *projectService) ListProjects(ctx context.Context, status string) ([]models.Project, error) {
	return s.repo.List(ctx, status)
}

func (s *projectService) UpdateProject(ctx context.Context, req *models.UpdateProjectRequest) (*models.Project, error) {
	update := bson.M{}
	if req.Body.Name != nil {
		update["name"] = *req.Body.Name
	}
	if req.Body.Description != nil {
		update["description"] = *req.Body.Description
	}
	if req.Body.Status != nil {
		update["status"] = *req.Body.Status
	}

	if len(update) == 0 {
		return s.repo.GetByUUID(ctx, req.UUID)
	}

	project, err := s.repo.Update(ctx, req.UUID, update)
	if err != nil {
		return nil, err
	}

	// Sync bank mission if description or name changed
	if req.Body.Description != nil || req.Body.Name != nil {
		if project.HindsightBankID != "" {
			if err := s.hsClient.CreateOrUpdateBank(ctx, project.HindsightBankID, project.Name, project.Description, nil); err != nil {
				s.logger.Warn("Failed to sync Hindsight bank mission",
					slog.String("projectUUID", project.UUID),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	return project, nil
}

func (s *projectService) DeleteProject(ctx context.Context, uuid string) error {
	project, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	if project.IsPersonal {
		return fmt.Errorf("personal agent projects cannot be deleted via project management")
	}

	// Delete Hindsight bank
	if project.HindsightBankID != "" {
		if err := s.hsClient.DeleteBank(ctx, project.HindsightBankID); err != nil {
			s.logger.Warn("Failed to delete Hindsight bank, proceeding with project deletion",
				slog.String("projectUUID", uuid),
				slog.String("bankId", project.HindsightBankID),
				slog.String("error", err.Error()),
			)
		}
	}

	return s.repo.Delete(ctx, uuid)
}

func (s *projectService) AddDocuments(ctx context.Context, uuid string, documentUUIDs []string) (*models.Project, error) {
	project, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	// Deduplicate
	existing := make(map[string]bool, len(project.DocumentUUIDs))
	for _, d := range project.DocumentUUIDs {
		existing[d] = true
	}
	for _, d := range documentUUIDs {
		if !existing[d] {
			project.DocumentUUIDs = append(project.DocumentUUIDs, d)
			existing[d] = true
		}
	}

	return s.repo.Update(ctx, uuid, bson.M{"documentUuids": project.DocumentUUIDs})
}

func (s *projectService) RemoveDocuments(ctx context.Context, uuid string, documentUUIDs []string) (*models.Project, error) {
	project, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	toRemove := make(map[string]bool, len(documentUUIDs))
	for _, d := range documentUUIDs {
		toRemove[d] = true
	}

	filtered := make([]string, 0, len(project.DocumentUUIDs))
	for _, d := range project.DocumentUUIDs {
		if !toRemove[d] {
			filtered = append(filtered, d)
		}
	}

	return s.repo.Update(ctx, uuid, bson.M{"documentUuids": filtered})
}

func (s *projectService) UpdateFilters(ctx context.Context, uuid string, isoStandards, categories []string) (*models.Project, error) {
	update := bson.M{}
	if isoStandards != nil {
		update["isoStandards"] = isoStandards
	}
	if categories != nil {
		update["categories"] = categories
	}
	if len(update) == 0 {
		return s.repo.GetByUUID(ctx, uuid)
	}
	return s.repo.Update(ctx, uuid, update)
}

func (s *projectService) UpdateSettings(ctx context.Context, uuid string, settings *models.AgentSettings) (*models.Project, error) {
	return s.repo.Update(ctx, uuid, bson.M{"settings": settings})
}
