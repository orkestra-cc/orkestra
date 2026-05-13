package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"github.com/orkestra-cc/orkestra-addon-sales/services"
)

// SkillHandler handles HTTP requests for individual sales intelligence skills
type SkillHandler struct {
	orchestrator services.OrchestratorService
	store        *services.SkillStore
	logger       *slog.Logger
}

// NewSkillHandler creates a new SkillHandler
func NewSkillHandler(orchestrator services.OrchestratorService, store *services.SkillStore, logger *slog.Logger) *SkillHandler {
	return &SkillHandler{
		orchestrator: orchestrator,
		store:        store,
		logger:       logger.With(slog.String("handler", "sales-skill")),
	}
}

func (h *SkillHandler) submitSkill(ctx context.Context, skillName models.SkillName, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	taskID := uuid.New().String()

	task := &services.SkillTask{
		ID:        taskID,
		Skill:     string(skillName),
		Status:    "running",
		CreatedAt: time.Now(),
	}
	h.store.Put(task)

	// Run in background — detached from HTTP context
	go func() {
		result, err := h.orchestrator.RunSkill(context.Background(), skillName, req.Body.URL, req.Body.Locale, req.Body.Context, userUUID)
		if err != nil {
			h.logger.Error("skill execution failed",
				slog.String("taskId", taskID),
				slog.String("skill", string(skillName)),
				slog.String("url", req.Body.URL),
				slog.String("user", userUUID),
				slog.String("error", err.Error()),
			)
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "completed"
			task.Result = result
		}
		h.store.Put(task)
	}()

	resp := &models.SkillTaskResponse{}
	resp.Body.TaskID = taskID
	return resp, nil
}

// PollSkillTask handles GET /v1/sales/skills/{taskId}
func (h *SkillHandler) PollSkillTask(ctx context.Context, req *models.SkillTaskPollRequest) (*models.SkillTaskPollResponse, error) {
	task := h.store.Get(req.TaskID)
	if task == nil {
		return nil, huma.Error404NotFound("task not found")
	}

	resp := &models.SkillTaskPollResponse{}
	resp.Body.TaskID = task.ID
	resp.Body.Status = task.Status
	resp.Body.Skill = task.Skill
	resp.Body.Error = task.Error

	if task.Result != nil {
		resp.Body.Result = task.Result.Result
		resp.Body.InputTokens = task.Result.InputTokens
		resp.Body.OutputTokens = task.Result.OutputTokens
		resp.Body.LatencyMs = task.Result.LatencyMs
		resp.Body.ModelUsed = task.Result.ModelUsed
	}

	return resp, nil
}

// --- Skill submission endpoints ---

func (h *SkillHandler) Research(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillResearch, req)
}

func (h *SkillHandler) Qualify(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillQualify, req)
}

func (h *SkillHandler) Contacts(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillContacts, req)
}

func (h *SkillHandler) Outreach(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillOutreach, req)
}

func (h *SkillHandler) Followup(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillFollowup, req)
}

func (h *SkillHandler) Prep(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillPrep, req)
}

func (h *SkillHandler) Proposal(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillProposal, req)
}

func (h *SkillHandler) Objections(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillObjections, req)
}

func (h *SkillHandler) ICP(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillICP, req)
}

func (h *SkillHandler) Competitors(ctx context.Context, req *models.SkillRequest) (*models.SkillTaskResponse, error) {
	return h.submitSkill(ctx, models.SkillCompetitors, req)
}
