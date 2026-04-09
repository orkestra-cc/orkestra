package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/repository"
)

// RelationshipTypeService manages relationship type configurations.
type RelationshipTypeService interface {
	List(ctx context.Context) ([]models.RelationshipTypeConfig, error)
	Create(ctx context.Context, name, description, fromNode, toNode string, properties []string, categories map[string]bool) (*models.RelationshipTypeConfig, error)
	Update(ctx context.Context, uuid string, desc *string, props *[]string, cats *map[string]bool) (*models.RelationshipTypeConfig, error)
	Delete(ctx context.Context, uuid string) error
	GetActiveForCategory(ctx context.Context, category string) (map[string]bool, error)
}

type relationshipTypeService struct {
	repo   repository.RelationshipTypeRepository
	logger *slog.Logger
}

// NewRelationshipTypeService creates a new RelationshipTypeService.
func NewRelationshipTypeService(repo repository.RelationshipTypeRepository, logger *slog.Logger) RelationshipTypeService {
	return &relationshipTypeService{
		repo:   repo,
		logger: logger.With(slog.String("module", "rag-relationship-types")),
	}
}

func (s *relationshipTypeService) List(ctx context.Context) ([]models.RelationshipTypeConfig, error) {
	return s.repo.List(ctx)
}

func (s *relationshipTypeService) Create(ctx context.Context, name, description, fromNode, toNode string, properties []string, categories map[string]bool) (*models.RelationshipTypeConfig, error) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Check for duplicate name
	if existing, _ := s.repo.GetByName(ctx, name); existing != nil {
		return nil, fmt.Errorf("relationship type %q already exists", name)
	}

	rt := &models.RelationshipTypeConfig{
		Name:        name,
		Description: description,
		FromNode:    fromNode,
		ToNode:      toNode,
		Properties:  properties,
		Categories:  categories,
		IsSystem:    false,
	}

	if err := s.repo.Create(ctx, rt); err != nil {
		return nil, err
	}

	s.logger.Info("relationship type created", slog.String("name", name))
	return rt, nil
}

func (s *relationshipTypeService) Update(ctx context.Context, uuid string, desc *string, props *[]string, cats *map[string]bool) (*models.RelationshipTypeConfig, error) {
	rt, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	// System relationships can have categories toggled but not deleted
	// Name and isSystem cannot be changed via update
	updated, err := s.repo.Update(ctx, uuid, desc, props, cats)
	if err != nil {
		return nil, err
	}

	s.logger.Info("relationship type updated", slog.String("name", rt.Name))
	return updated, nil
}

func (s *relationshipTypeService) Delete(ctx context.Context, uuid string) error {
	rt, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}
	if rt.IsSystem {
		return fmt.Errorf("cannot delete system relationship type %q", rt.Name)
	}

	if err := s.repo.Delete(ctx, uuid); err != nil {
		return err
	}

	s.logger.Info("relationship type deleted", slog.String("name", rt.Name))
	return nil
}

// GetActiveForCategory returns a set of active relationship type names for a given category.
// If category is empty, all non-system types are returned as active.
func (s *relationshipTypeService) GetActiveForCategory(ctx context.Context, category string) (map[string]bool, error) {
	if category == "" {
		category = "generic"
	}

	rels, err := s.repo.ListActiveForCategory(ctx, category)
	if err != nil {
		return nil, err
	}

	active := make(map[string]bool, len(rels))
	for _, r := range rels {
		active[r.Name] = true
	}
	return active, nil
}
