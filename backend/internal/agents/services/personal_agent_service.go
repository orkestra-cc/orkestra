package services

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/agents/models"
	"github.com/orkestra/backend/internal/agents/repository"
)

// PersonalAgentService manages per-user personal agents backed by auto-provisioned projects.
type PersonalAgentService interface {
	GetOrCreatePersonalProject(ctx context.Context, userUUID string) (*models.Project, error)
	Query(ctx context.Context, req *PersonalQueryInput, userUUID, userRole string) (*models.AgentQueryResponse, error)
	AddDocuments(ctx context.Context, userUUID string, documentUUIDs []string) (*models.Project, error)
	RemoveDocuments(ctx context.Context, userUUID string, documentUUIDs []string) (*models.Project, error)
	UpdateSettings(ctx context.Context, userUUID string, settings *models.AgentSettings) (*models.Project, error)
	GetSettings(ctx context.Context, userUUID string) (*models.AgentSettings, error)
	ListConversations(ctx context.Context, userUUID string, limit, offset int) ([]models.Conversation, int64, error)
	GetConversation(ctx context.Context, userUUID, conversationUUID string) (*models.Conversation, error)
	DeleteConversation(ctx context.Context, userUUID, conversationUUID string) error
}

// PersonalQueryInput holds the parsed query parameters (no path UUID needed).
type PersonalQueryInput struct {
	Question       string
	Persona        string
	ConversationID string
	TopK           int
	MinScore       float64
	RetrievalMode  string
}

type personalAgentService struct {
	projectRepo  repository.ProjectRepository
	agentService AgentService
	hsClient     HindsightClient
	namespace    string
	logger       *slog.Logger
}

// NewPersonalAgentService creates a new PersonalAgentService.
func NewPersonalAgentService(
	projectRepo repository.ProjectRepository,
	agentService AgentService,
	hsClient HindsightClient,
	namespace string,
	logger *slog.Logger,
) PersonalAgentService {
	return &personalAgentService{
		projectRepo:  projectRepo,
		agentService: agentService,
		hsClient:     hsClient,
		namespace:    namespace,
		logger:       logger.With(slog.String("module", "personal-agent")),
	}
}

// personalBankID generates the Hindsight bank ID for a user's personal agent.
func (s *personalAgentService) personalBankID(userUUID string) string {
	return fmt.Sprintf("%s-personal-%s", s.namespace, userUUID)
}

func (s *personalAgentService) GetOrCreatePersonalProject(ctx context.Context, userUUID string) (*models.Project, error) {
	// Try to find existing personal project
	project, err := s.projectRepo.GetPersonalByUserUUID(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("lookup personal project: %w", err)
	}
	if project != nil {
		return project, nil
	}

	// Auto-create personal project
	project = &models.Project{
		Name:             "Personal Agent",
		Description:      "Your personal AI assistant — add documents and start asking questions.",
		IsPersonal:       true,
		PersonalUserUUID: userUUID,
		CreatedBy:        userUUID,
		Status:           models.ProjectStatusActive,
		DocumentUUIDs:    []string{},
	}

	if err := s.projectRepo.Create(ctx, project); err != nil {
		// Race condition: another request may have created it concurrently
		existing, lookupErr := s.projectRepo.GetPersonalByUserUUID(ctx, userUUID)
		if lookupErr != nil || existing == nil {
			return nil, fmt.Errorf("create personal project: %w", err)
		}
		return existing, nil
	}

	// Create Hindsight bank
	bankID := s.personalBankID(userUUID)
	if err := s.hsClient.CreateOrUpdateBank(ctx, bankID, project.Name, project.Description, nil); err != nil {
		s.logger.Warn("Failed to create personal Hindsight bank, project in degraded mode",
			slog.String("userUUID", userUUID),
			slog.String("error", err.Error()),
		)
	}

	// Persist the bank ID
	updated, err := s.projectRepo.Update(ctx, project.UUID, bson.M{"hindsightBankId": bankID})
	if err != nil {
		s.logger.Warn("Failed to persist bank ID", slog.String("error", err.Error()))
		project.HindsightBankID = bankID
		return project, nil
	}

	s.logger.Info("Personal agent created",
		slog.String("userUUID", userUUID),
		slog.String("projectUUID", updated.UUID),
		slog.String("bankID", bankID),
	)

	return updated, nil
}

func (s *personalAgentService) Query(ctx context.Context, req *PersonalQueryInput, userUUID, userRole string) (*models.AgentQueryResponse, error) {
	project, err := s.GetOrCreatePersonalProject(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	// Build a standard AgentQueryRequest to delegate to the existing service
	agentReq := &models.AgentQueryRequest{UUID: project.UUID}
	agentReq.Body.Question = req.Question
	agentReq.Body.Persona = req.Persona
	agentReq.Body.ConversationID = req.ConversationID
	agentReq.Body.TopK = req.TopK
	agentReq.Body.MinScore = req.MinScore
	agentReq.Body.RetrievalMode = req.RetrievalMode

	return s.agentService.Query(ctx, agentReq, userUUID, userRole)
}

func (s *personalAgentService) AddDocuments(ctx context.Context, userUUID string, documentUUIDs []string) (*models.Project, error) {
	project, err := s.GetOrCreatePersonalProject(ctx, userUUID)
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

	return s.projectRepo.Update(ctx, project.UUID, bson.M{"documentUuids": project.DocumentUUIDs})
}

func (s *personalAgentService) RemoveDocuments(ctx context.Context, userUUID string, documentUUIDs []string) (*models.Project, error) {
	project, err := s.GetOrCreatePersonalProject(ctx, userUUID)
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

	return s.projectRepo.Update(ctx, project.UUID, bson.M{"documentUuids": filtered})
}

func (s *personalAgentService) UpdateSettings(ctx context.Context, userUUID string, settings *models.AgentSettings) (*models.Project, error) {
	project, err := s.GetOrCreatePersonalProject(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	// Sync disposition to Hindsight bank if changed
	if project.HindsightBankID != "" && settings != nil {
		disposition := &DispositionConfig{
			Skepticism: settings.Skepticism,
			Literalism: settings.Literalism,
			Empathy:    settings.Empathy,
		}
		if err := s.hsClient.CreateOrUpdateBank(ctx, project.HindsightBankID, project.Name, project.Description, disposition); err != nil {
			s.logger.Warn("Failed to sync disposition to bank", slog.String("error", err.Error()))
		}
	}

	return s.projectRepo.Update(ctx, project.UUID, bson.M{"settings": settings})
}

func (s *personalAgentService) GetSettings(ctx context.Context, userUUID string) (*models.AgentSettings, error) {
	project, err := s.GetOrCreatePersonalProject(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	return project.Settings, nil
}

func (s *personalAgentService) ListConversations(ctx context.Context, userUUID string, limit, offset int) ([]models.Conversation, int64, error) {
	project, err := s.GetOrCreatePersonalProject(ctx, userUUID)
	if err != nil {
		return nil, 0, err
	}
	return s.agentService.ListConversations(ctx, project.UUID, userUUID, limit, offset)
}

func (s *personalAgentService) GetConversation(ctx context.Context, userUUID, conversationUUID string) (*models.Conversation, error) {
	conv, err := s.agentService.GetConversation(ctx, conversationUUID)
	if err != nil {
		return nil, err
	}
	// Verify ownership
	if conv.UserUUID != userUUID {
		return nil, fmt.Errorf("conversation not found: %s", conversationUUID)
	}
	return conv, nil
}

func (s *personalAgentService) DeleteConversation(ctx context.Context, userUUID, conversationUUID string) error {
	conv, err := s.agentService.GetConversation(ctx, conversationUUID)
	if err != nil {
		return err
	}
	// Verify ownership
	if conv.UserUUID != userUUID {
		return fmt.Errorf("conversation not found: %s", conversationUUID)
	}
	return s.agentService.DeleteConversation(ctx, conversationUUID)
}
