package sales

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-addon-sales/handlers"
)

func op(id, method, path, summary string) huma.Operation {
	return huma.Operation{
		OperationID: id,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Tags:        []string{"Sales Intelligence"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}
}

// RegisterSkillRoutes registers individual skill endpoints (async submission + poll)
func RegisterSkillRoutes(api huma.API, h *handlers.SkillHandler) {
	// Skill submission (returns taskId immediately)
	huma.Register(api, op("sales-research", http.MethodPost, "/v1/sales/research", "Company research & firmographics"), h.Research)
	huma.Register(api, op("sales-qualify", http.MethodPost, "/v1/sales/qualify", "BANT + MEDDIC lead qualification"), h.Qualify)
	huma.Register(api, op("sales-contacts", http.MethodPost, "/v1/sales/contacts", "Decision maker identification"), h.Contacts)
	huma.Register(api, op("sales-outreach", http.MethodPost, "/v1/sales/outreach", "Generate outreach email sequence"), h.Outreach)
	huma.Register(api, op("sales-followup", http.MethodPost, "/v1/sales/followup", "Generate follow-up sequence"), h.Followup)
	huma.Register(api, op("sales-prep", http.MethodPost, "/v1/sales/prep", "Meeting preparation brief"), h.Prep)
	huma.Register(api, op("sales-proposal", http.MethodPost, "/v1/sales/proposal", "Client proposal generation"), h.Proposal)
	huma.Register(api, op("sales-objections", http.MethodPost, "/v1/sales/objections", "Objection handling playbook"), h.Objections)
	huma.Register(api, op("sales-icp", http.MethodPost, "/v1/sales/icp", "Ideal Customer Profile builder"), h.ICP)
	huma.Register(api, op("sales-competitors", http.MethodPost, "/v1/sales/competitors", "Competitive intelligence"), h.Competitors)

	// Poll for skill result
	huma.Register(api, op("sales-skill-poll", http.MethodGet, "/v1/sales/skills/{taskId}", "Poll skill task result"), h.PollSkillTask)
}

// RegisterProspectRoutes registers prospect analysis endpoints
func RegisterProspectRoutes(api huma.API, h *handlers.ProspectHandler) {
	huma.Register(api, op("sales-prospect", http.MethodPost, "/v1/sales/prospect", "Full async prospect analysis"), h.Prospect)
	huma.Register(api, op("sales-prospect-quick", http.MethodPost, "/v1/sales/prospect/quick", "Quick 60-second prospect snapshot"), h.QuickProspect)
}

// RegisterJobRoutes registers job management endpoints
func RegisterJobRoutes(api huma.API, h *handlers.JobHandler) {
	huma.Register(api, op("sales-list-jobs", http.MethodGet, "/v1/sales/jobs", "List prospect jobs"), h.ListJobs)
	huma.Register(api, op("sales-get-job", http.MethodGet, "/v1/sales/jobs/{uuid}", "Get job status and results"), h.GetJob)
	huma.Register(api, op("sales-cancel-job", http.MethodDelete, "/v1/sales/jobs/{uuid}", "Cancel a running job"), h.DeleteJob)
	huma.Register(api, op("sales-retry-job", http.MethodPost, "/v1/sales/jobs/{uuid}/retry", "Retry a failed job"), h.RetryJob)
	huma.Register(api, op("sales-rerun-job", http.MethodPost, "/v1/sales/jobs/{uuid}/rerun", "Re-run only failed agents"), h.RerunFailedAgents)
}

// RegisterReportRoutes registers report endpoints via Huma
func RegisterReportRoutes(api huma.API, h *handlers.ReportHandler) {
	huma.Register(api, op("sales-list-reports", http.MethodGet, "/v1/sales/reports", "List prospect reports"), h.ListReports)
	huma.Register(api, op("sales-get-report", http.MethodGet, "/v1/sales/reports/{uuid}", "Get report with Markdown content"), h.GetReport)
	huma.Register(api, op("sales-generate-report", http.MethodPost, "/v1/sales/reports/generate/{jobUuid}", "Generate report for a completed job"), h.GenerateReport)
	huma.Register(api, op("sales-delete-report", http.MethodDelete, "/v1/sales/reports/{uuid}", "Delete a report"), h.DeleteReport)
}

// RegisterReportDownloadRoute registers the raw Markdown download route on chi directly
func RegisterReportDownloadRoute(r interface {
	Get(string, http.HandlerFunc)
}, h *handlers.ReportHandler) {
	r.Get("/v1/sales/reports/{uuid}/md", h.DownloadMarkdown)
}

// RegisterPromptRoutes registers prompt management endpoints
func RegisterPromptRoutes(api huma.API, h *handlers.PromptHandler) {
	huma.Register(api, op("sales-list-prompts", http.MethodGet, "/v1/sales/prompts", "List all prompt templates"), h.ListPrompts)
	huma.Register(api, op("sales-get-prompt", http.MethodGet, "/v1/sales/prompts/{uuid}", "Get prompt template"), h.GetPrompt)
	huma.Register(api, op("sales-update-prompt", http.MethodPatch, "/v1/sales/prompts/{uuid}", "Update prompt template content"), h.UpdatePrompt)
	huma.Register(api, op("sales-reset-prompt", http.MethodPost, "/v1/sales/prompts/{uuid}/reset", "Reset prompt to embedded default"), h.ResetPrompt)
}

// RegisterSettingsRoutes registers per-user settings endpoints
func RegisterSettingsRoutes(api huma.API, h *handlers.SettingsHandler) {
	huma.Register(api, op("sales-get-settings", http.MethodGet, "/v1/sales/settings", "Get sales intelligence settings"), h.GetSettings)
	huma.Register(api, op("sales-update-settings", http.MethodPatch, "/v1/sales/settings", "Update sales intelligence settings"), h.UpdateSettings)
}
