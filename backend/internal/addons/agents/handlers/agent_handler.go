package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra/backend/internal/addons/agents/models"
	"github.com/orkestra/backend/internal/addons/agents/services"
)

// AgentHandler handles HTTP requests for agent queries and conversations
type AgentHandler struct {
	agentService services.AgentService
}

// NewAgentHandler creates a new AgentHandler
func NewAgentHandler(agentService services.AgentService) *AgentHandler {
	return &AgentHandler{agentService: agentService}
}

func (h *AgentHandler) Query(ctx context.Context, req *models.AgentQueryRequest) (*models.AgentQueryResponse, error) {
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	userRole, _ := ctxauth.GetSystemRole(ctx)

	resp, err := h.agentService.Query(ctx, req, userUUID, userRole)
	if err != nil {
		return nil, huma.Error500InternalServerError("Agent query failed", err)
	}
	return resp, nil
}

func (h *AgentHandler) CreateConversation(ctx context.Context, req *models.CreateConversationRequest) (*models.CreateConversationResponse, error) {
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	persona := req.Body.Persona
	if persona == "" {
		userRole, _ := ctxauth.GetSystemRole(ctx)
		persona = models.DefaultPersonaForRole(userRole)
	}

	conv, err := h.agentService.CreateConversation(ctx, req.UUID, userUUID, persona)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create conversation", err)
	}
	return &models.CreateConversationResponse{Body: *conv}, nil
}

func (h *AgentHandler) ListConversations(ctx context.Context, req *models.ListConversationsRequest) (*models.ListConversationsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	convs, total, err := h.agentService.ListConversations(ctx, req.UUID, userUUID, req.Limit, req.Offset)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list conversations", err)
	}
	resp := &models.ListConversationsResponse{}
	resp.Body.Conversations = convs
	resp.Body.Total = total
	return resp, nil
}

func (h *AgentHandler) GetConversation(ctx context.Context, req *models.GetConversationRequest) (*models.GetConversationResponse, error) {
	conv, err := h.agentService.GetConversation(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Conversation not found", err)
	}
	return &models.GetConversationResponse{Body: *conv}, nil
}

func (h *AgentHandler) DeleteConversation(ctx context.Context, req *models.DeleteConversationRequest) (*models.DeleteConversationResponse, error) {
	if err := h.agentService.DeleteConversation(ctx, req.UUID); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete conversation", err)
	}
	resp := &models.DeleteConversationResponse{}
	resp.Body.Message = "Conversation deleted successfully"
	return resp, nil
}

func (h *AgentHandler) GetBankInfo(ctx context.Context, req *models.GetBankInfoRequest) (*models.GetBankInfoResponse, error) {
	info, err := h.agentService.GetBankInfo(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get bank info", err)
	}
	resp := &models.GetBankInfoResponse{}
	resp.Body.BankID = info.BankID
	resp.Body.Status = "active"
	return resp, nil
}

func (h *AgentHandler) HealthCheck(ctx context.Context, _ *struct{}) (*models.AgentHealthCheckResponse, error) {
	resp := &models.AgentHealthCheckResponse{}
	if err := h.agentService.HealthCheck(ctx); err != nil {
		resp.Body.Hindsight = "degraded"
	} else {
		resp.Body.Hindsight = "ok"
	}
	return resp, nil
}
