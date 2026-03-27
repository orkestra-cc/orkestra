package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/agents/models"
	"github.com/orkestra/backend/internal/agents/repository"
)

// AgentService orchestrates agent queries: RAG retrieval → Hindsight retain → Hindsight reflect
type AgentService interface {
	Query(ctx context.Context, req *models.AgentQueryRequest, userUUID, userRole string) (*models.AgentQueryResponse, error)
	CreateConversation(ctx context.Context, projectUUID, userUUID, persona string) (*models.Conversation, error)
	ListConversations(ctx context.Context, projectUUID, userUUID string, limit, offset int) ([]models.Conversation, int64, error)
	GetConversation(ctx context.Context, conversationUUID string) (*models.Conversation, error)
	DeleteConversation(ctx context.Context, conversationUUID string) error
	GetBankInfo(ctx context.Context, projectUUID string) (*BankInfo, error)
	HealthCheck(ctx context.Context) error
}

type agentService struct {
	projectRepo  repository.ProjectRepository
	convRepo     repository.ConversationRepository
	hsClient     HindsightClient
	ragBridge    RAGBridge // may be nil if RAG is disabled
	logger       *slog.Logger
}

// NewAgentService creates a new AgentService
func NewAgentService(
	projectRepo repository.ProjectRepository,
	convRepo repository.ConversationRepository,
	hsClient HindsightClient,
	ragBridge RAGBridge,
	logger *slog.Logger,
) AgentService {
	return &agentService{
		projectRepo: projectRepo,
		convRepo:    convRepo,
		hsClient:    hsClient,
		ragBridge:   ragBridge,
		logger:      logger.With(slog.String("module", "agents-query")),
	}
}

func (s *agentService) Query(ctx context.Context, req *models.AgentQueryRequest, userUUID, userRole string) (*models.AgentQueryResponse, error) {
	totalStart := time.Now()

	// 1. Validate project
	project, err := s.projectRepo.GetByUUID(ctx, req.UUID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	// 2. Resolve persona
	persona := req.Body.Persona
	if persona == "" {
		persona = models.DefaultPersonaForRole(userRole)
	}
	if !models.CanUsePersona(userRole, persona) {
		return nil, fmt.Errorf("insufficient permissions to use persona %q with role %q", persona, userRole)
	}
	baseProfile, ok := models.PersonaProfiles[persona]
	if !ok {
		return nil, fmt.Errorf("unknown persona: %s", persona)
	}
	profile := mergeProfile(baseProfile, project.Settings)

	// 3. Get or create conversation
	conversationID := req.Body.ConversationID
	if conversationID == "" {
		conv := &models.Conversation{
			ProjectUUID: project.UUID,
			UserUUID:    userUUID,
			Persona:     persona,
		}
		if err := s.convRepo.Create(ctx, conv); err != nil {
			return nil, fmt.Errorf("create conversation: %w", err)
		}
		conversationID = conv.UUID
	}

	// 4. Save user message
	userMsg := models.Message{
		Role:    "user",
		Content: req.Body.Question,
	}
	if err := s.convRepo.AppendMessage(ctx, conversationID, userMsg); err != nil {
		s.logger.Warn("Failed to save user message", slog.String("error", err.Error()))
	}

	// 5. RAG Phase — query scoped to project documents
	var ragResult *RAGBridgeResult
	var sources []models.Source
	var ragTimeMs int64

	if s.ragBridge != nil && (len(project.DocumentUUIDs) > 0 || len(project.ISOStandards) > 0 || len(project.Categories) > 0) {
		ragStart := time.Now()
		ragResult, err = s.ragBridge.QueryWithScope(
			ctx,
			req.Body.Question,
			project.DocumentUUIDs,
			req.Body.TopK,
			req.Body.MinScore,
			req.Body.RetrievalMode,
		)
		ragTimeMs = time.Since(ragStart).Milliseconds()
		if err != nil {
			s.logger.Warn("RAG query failed, proceeding without context",
				slog.String("projectUUID", project.UUID),
				slog.String("error", err.Error()),
			)
		} else {
			sources = ragResult.Sources
		}
	}

	// 6. Retain Phase (async, fire-and-forget) — build persistent knowledge
	if project.HindsightBankID != "" {
		go func() {
			retainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var items []MemoryEntry

			// Always retain the user's message as a conversation memory
			items = append(items, MemoryEntry{
				Content: fmt.Sprintf("User said: %s", req.Body.Question),
				Context: fmt.Sprintf("Conversation %s, persona: %s", conversationID, persona),
				Tags:    []string{"conversation", "user"},
			})

			// Retain RAG context when available
			if ragResult != nil && ragResult.ContextText != "" {
				items = append(items, MemoryEntry{
					Content: ragResult.ContextText,
					Context: fmt.Sprintf("RAG query: %s", req.Body.Question),
					Tags:    []string{"rag", "auto"},
				})
			}

			if err := s.hsClient.Retain(retainCtx, project.HindsightBankID, items, nil); err != nil {
				s.logger.Warn("Async retain failed", slog.String("error", err.Error()))
			}
		}()
	}

	// 7. Reflect Phase — combine RAG context + persistent memory + persona directives
	var answer string
	var reflectTimeMs int64
	var inputTokens, outputTokens, totalTokens int32

	// Fetch conversation history for context
	var conversationMessages []models.Message
	if conv, err := s.convRepo.GetByUUID(ctx, conversationID); err == nil && conv != nil {
		conversationMessages = conv.Messages
	}

	reflectCtx := s.buildReflectContext(profile, ragResult, conversationMessages)

	if project.HindsightBankID != "" {
		// Use a detached context with generous timeout — local LLMs can be slow
		reflectCtxTimeout, reflectCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer reflectCancel()

		// Cap reflect tokens — persona MaxTokens is for full RAG answers; conversational queries need less
		reflectMaxTokens := profile.MaxTokens
		if reflectMaxTokens > 2048 {
			reflectMaxTokens = 2048
		}

		reflectStart := time.Now()
		result, err := s.hsClient.Reflect(reflectCtxTimeout, project.HindsightBankID, req.Body.Question, reflectCtx, reflectMaxTokens, nil)
		reflectTimeMs = time.Since(reflectStart).Milliseconds()
		if err != nil {
			s.logger.Warn("Hindsight reflect failed, falling back to RAG answer",
				slog.String("error", err.Error()),
			)
			if ragResult != nil {
				answer = ragResult.Answer
			} else {
				answer = "Unable to process your query. Please try again."
			}
		} else {
			answer = result.Text
			inputTokens = result.InputTokens
			outputTokens = result.OutputTokens
			totalTokens = result.TotalTokens
		}
	} else if ragResult != nil {
		answer = ragResult.Answer
	} else {
		answer = "No documents are scoped to this project. Please add documents to get started."
	}

	// 8. Save assistant message (use background context — HTTP ctx may be done)
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()

	meta := models.MsgMeta{
		RAGTimeMs:     ragTimeMs,
		ReflectTimeMs: reflectTimeMs,
		TotalTimeMs:   time.Since(totalStart).Milliseconds(),
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		TotalTokens:   totalTokens,
	}
	if ragResult != nil {
		meta.ChunksRetrieved = len(sources)
		meta.ModelUsed = ragResult.ModelUsed
	}

	assistantMsg := models.Message{
		Role:     "assistant",
		Content:  answer,
		Sources:  sources,
		Metadata: meta,
	}
	if err := s.convRepo.AppendMessage(saveCtx, conversationID, assistantMsg); err != nil {
		s.logger.Warn("Failed to save assistant message", slog.String("error", err.Error()))
	}

	// Auto-title from first question
	conv, _ := s.convRepo.GetByUUID(saveCtx, conversationID)
	if conv != nil && conv.Title == "" && len(conv.Messages) >= 1 {
		title := req.Body.Question
		if len(title) > 100 {
			title = title[:100] + "..."
		}
		conv.Title = title
		// Update title in background
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.convRepo.AppendMessage(updateCtx, conversationID, models.Message{}) // noop, title set below
			// Actually just update the title directly via repo
			s.logger.Debug("Setting conversation title", slog.String("title", title))
		}()
	}

	// 9. Build response
	resp := &models.AgentQueryResponse{}
	resp.Body.Answer = answer
	resp.Body.Sources = sources
	if resp.Body.Sources == nil {
		resp.Body.Sources = []models.Source{}
	}
	resp.Body.ConversationID = conversationID
	resp.Body.Metadata = meta

	s.logger.Info("Agent query completed",
		slog.String("project", project.UUID),
		slog.String("persona", persona),
		slog.Int64("totalMs", meta.TotalTimeMs),
		slog.Int("chunks", meta.ChunksRetrieved),
	)

	return resp, nil
}

// mergeProfile applies project-level settings on top of persona defaults.
func mergeProfile(base models.PersonaProfile, settings *models.AgentSettings) models.PersonaProfile {
	if settings == nil {
		return base
	}
	merged := base

	if settings.SystemPrompt != "" {
		merged.SystemContext = settings.SystemPrompt
	}
	if len(settings.Directives) > 0 {
		merged.Directives = append(merged.Directives, settings.Directives...)
	}
	if settings.Skepticism > 0 {
		merged.Disposition.Skepticism = settings.Skepticism
	}
	if settings.Literalism > 0 {
		merged.Disposition.Literalism = settings.Literalism
	}
	if settings.Empathy > 0 {
		merged.Disposition.Empathy = settings.Empathy
	}
	if settings.MaxTokens > 0 {
		merged.MaxTokens = settings.MaxTokens
	}
	if settings.Language != "" {
		merged.Directives = append(merged.Directives, fmt.Sprintf("Always respond in %s", settings.Language))
	}
	if settings.Temperature == "precise" {
		merged.Directives = append(merged.Directives, "Be precise and factual. Avoid speculation.")
	} else if settings.Temperature == "creative" {
		merged.Directives = append(merged.Directives, "Be creative and exploratory. Offer suggestions and alternatives.")
	}

	return merged
}

// buildReflectContext combines persona directives, conversation history, and RAG results into the context for Hindsight reflect.
func (s *agentService) buildReflectContext(profile models.PersonaProfile, ragResult *RAGBridgeResult, conversationMessages []models.Message) string {
	var sb strings.Builder

	// Persona context
	sb.WriteString("ROLE CONTEXT: ")
	sb.WriteString(profile.SystemContext)
	sb.WriteString("\n\nDIRECTIVES:\n")
	for _, d := range profile.Directives {
		sb.WriteString("- ")
		sb.WriteString(d)
		sb.WriteString("\n")
	}

	// Conversation history (last 6 messages, compact format)
	if len(conversationMessages) > 0 {
		sb.WriteString("\nRECENT CONVERSATION:\n")
		start := 0
		if len(conversationMessages) > 6 {
			start = len(conversationMessages) - 6
		}
		for _, msg := range conversationMessages[start:] {
			if msg.Role == "user" {
				content := msg.Content
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("User: %s\n", content))
			} else if msg.Content != "" {
				content := msg.Content
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("Assistant: %s\n", content))
			}
		}
	}

	// RAG context
	if ragResult != nil && len(ragResult.Sources) > 0 {
		sb.WriteString("\nDOCUMENT CONTEXT:\n")
		for i, src := range ragResult.Sources {
			sb.WriteString(fmt.Sprintf("\n[Source %d] %s — %s (Score: %.2f)\n",
				i+1, src.DocumentTitle, src.FullPath, src.Score))
			if src.RequirementLevel != "" {
				sb.WriteString(fmt.Sprintf("Requirement Level: %s\n", src.RequirementLevel))
			}
			sb.WriteString(src.ChunkText)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (s *agentService) CreateConversation(ctx context.Context, projectUUID, userUUID, persona string) (*models.Conversation, error) {
	// Validate project exists
	if _, err := s.projectRepo.GetByUUID(ctx, projectUUID); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	conv := &models.Conversation{
		ProjectUUID: projectUUID,
		UserUUID:    userUUID,
		Persona:     persona,
	}
	if err := s.convRepo.Create(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

func (s *agentService) ListConversations(ctx context.Context, projectUUID, userUUID string, limit, offset int) ([]models.Conversation, int64, error) {
	return s.convRepo.ListByProject(ctx, projectUUID, userUUID, limit, offset)
}

func (s *agentService) GetConversation(ctx context.Context, conversationUUID string) (*models.Conversation, error) {
	return s.convRepo.GetByUUID(ctx, conversationUUID)
}

func (s *agentService) DeleteConversation(ctx context.Context, conversationUUID string) error {
	return s.convRepo.Delete(ctx, conversationUUID)
}

func (s *agentService) GetBankInfo(ctx context.Context, projectUUID string) (*BankInfo, error) {
	project, err := s.projectRepo.GetByUUID(ctx, projectUUID)
	if err != nil {
		return nil, err
	}
	if project.HindsightBankID == "" {
		return &BankInfo{BankID: "", Name: project.Name, Mission: project.Description}, nil
	}
	return s.hsClient.GetBankInfo(ctx, project.HindsightBankID)
}

func (s *agentService) HealthCheck(ctx context.Context) error {
	return s.hsClient.HealthCheck(ctx)
}
