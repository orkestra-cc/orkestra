package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/agents/models"
	"github.com/orkestra/backend/internal/agents/services"
)

// PersonalAgentHandler handles HTTP requests for per-user personal agents.
type PersonalAgentHandler struct {
	personalService services.PersonalAgentService
}

// NewPersonalAgentHandler creates a new PersonalAgentHandler.
func NewPersonalAgentHandler(personalService services.PersonalAgentService) *PersonalAgentHandler {
	return &PersonalAgentHandler{personalService: personalService}
}

func (h *PersonalAgentHandler) GetOrCreate(ctx context.Context, _ *struct{}) (*models.GetPersonalAgentResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	project, err := h.personalService.GetOrCreatePersonalProject(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get personal agent", err)
	}
	return &models.GetPersonalAgentResponse{Body: *project}, nil
}

func (h *PersonalAgentHandler) Query(ctx context.Context, req *models.PersonalQueryRequest) (*models.PersonalQueryResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	userRole, _ := ctx.Value("userRole").(string)

	input := &services.PersonalQueryInput{
		Question:       req.Body.Question,
		Persona:        req.Body.Persona,
		ConversationID: req.Body.ConversationID,
		TopK:           req.Body.TopK,
		MinScore:       req.Body.MinScore,
		RetrievalMode:  req.Body.RetrievalMode,
	}

	result, err := h.personalService.Query(ctx, input, userUUID, userRole)
	if err != nil {
		return nil, huma.Error500InternalServerError("Personal agent query failed", err)
	}

	resp := &models.PersonalQueryResponse{}
	resp.Body.Answer = result.Body.Answer
	resp.Body.Sources = result.Body.Sources
	resp.Body.ConversationID = result.Body.ConversationID
	resp.Body.Metadata = result.Body.Metadata
	return resp, nil
}

func (h *PersonalAgentHandler) AddDocuments(ctx context.Context, req *models.PersonalAddDocumentsRequest) (*models.PersonalAddDocumentsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	project, err := h.personalService.AddDocuments(ctx, userUUID, req.Body.DocumentUUIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to add documents", err)
	}
	return &models.PersonalAddDocumentsResponse{Body: *project}, nil
}

func (h *PersonalAgentHandler) RemoveDocuments(ctx context.Context, req *models.PersonalRemoveDocumentsRequest) (*models.PersonalRemoveDocumentsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	project, err := h.personalService.RemoveDocuments(ctx, userUUID, req.Body.DocumentUUIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to remove documents", err)
	}
	return &models.PersonalRemoveDocumentsResponse{Body: *project}, nil
}

func (h *PersonalAgentHandler) UpdateSettings(ctx context.Context, req *models.PersonalUpdateSettingsRequest) (*models.PersonalUpdateSettingsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

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

	project, err := h.personalService.UpdateSettings(ctx, userUUID, settings)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to update settings", err)
	}
	return &models.PersonalUpdateSettingsResponse{Body: *project}, nil
}

func (h *PersonalAgentHandler) GetSettings(ctx context.Context, _ *struct{}) (*models.PersonalGetSettingsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	settings, err := h.personalService.GetSettings(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get settings", err)
	}
	resp := &models.PersonalGetSettingsResponse{}
	resp.Body.Settings = settings
	return resp, nil
}

func (h *PersonalAgentHandler) ListConversations(ctx context.Context, req *models.PersonalListConversationsRequest) (*models.PersonalListConversationsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	convs, total, err := h.personalService.ListConversations(ctx, userUUID, req.Limit, req.Offset)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list conversations", err)
	}
	resp := &models.PersonalListConversationsResponse{}
	resp.Body.Conversations = convs
	resp.Body.Total = total
	return resp, nil
}

func (h *PersonalAgentHandler) GetConversation(ctx context.Context, req *models.PersonalGetConversationRequest) (*models.PersonalGetConversationResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	conv, err := h.personalService.GetConversation(ctx, userUUID, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Conversation not found", err)
	}
	return &models.PersonalGetConversationResponse{Body: *conv}, nil
}

func (h *PersonalAgentHandler) DeleteConversation(ctx context.Context, req *models.PersonalDeleteConversationRequest) (*models.PersonalDeleteConversationResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	if err := h.personalService.DeleteConversation(ctx, userUUID, req.UUID); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete conversation", err)
	}
	resp := &models.PersonalDeleteConversationResponse{}
	resp.Body.Message = "Conversation deleted successfully"
	return resp, nil
}
