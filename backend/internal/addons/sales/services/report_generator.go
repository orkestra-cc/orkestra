package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/sales/models"
	"github.com/orkestra/backend/internal/addons/sales/repository"
)

// ReportGenerator creates Markdown reports from completed prospect jobs
type ReportGenerator struct {
	repo   repository.ReportRepository
	logger *slog.Logger
}

// NewReportGenerator creates a new ReportGenerator
func NewReportGenerator(repo repository.ReportRepository, logger *slog.Logger) *ReportGenerator {
	return &ReportGenerator{
		repo:   repo,
		logger: logger.With(slog.String("component", "report-generator")),
	}
}

// GenerateFromJob creates a Markdown report from a completed job and stores it
func (g *ReportGenerator) GenerateFromJob(job *models.Job) (*models.Report, error) {
	// Extract company name from company-research findings
	companyName := extractCompanyName(job)

	// Build agent data map for structured storage
	agentData := buildAgentDataMap(job.AgentResults)
	agentDataJSON, _ := json.Marshal(agentData)

	// Generate Markdown content
	md := g.buildMarkdown(job, companyName, agentData)

	report := &models.Report{
		UUID:        uuid.New().String(),
		JobUUID:     job.UUID,
		CreatedBy:   job.CreatedBy,
		CompanyURL:  job.CompanyURL,
		CompanyName: companyName,
		Score:       job.TotalScore,
		Grade:       job.Grade,
		ContentMD:   md,
		AgentData:   json.RawMessage(agentDataJSON),
		CreatedAt:   time.Now(),
	}

	if err := g.repo.Create(dbCtx(), report); err != nil {
		return nil, fmt.Errorf("save report: %w", err)
	}

	g.logger.Info("report generated",
		slog.String("reportId", report.UUID),
		slog.String("jobId", job.UUID),
		slog.String("company", companyName),
		slog.Int("score", report.Score),
	)

	return report, nil
}

func (g *ReportGenerator) buildMarkdown(job *models.Job, companyName string, agentData map[string]any) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Prospect Report: %s\n\n", companyName))
	sb.WriteString(fmt.Sprintf("**URL:** %s  \n", job.CompanyURL))
	sb.WriteString(fmt.Sprintf("**Score:** %d/100 (Grade: %s)  \n", job.TotalScore, job.Grade))
	sb.WriteString(fmt.Sprintf("**Generated:** %s  \n", time.Now().Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("**Locale:** %s  \n\n", job.Locale))
	sb.WriteString("---\n\n")

	// Score summary
	sb.WriteString("## Score Summary\n\n")
	sb.WriteString("| Agent | Score | Status |\n")
	sb.WriteString("|-------|-------|--------|\n")
	for _, r := range job.AgentResults {
		if r == nil {
			continue
		}
		status := "Completed"
		if r.Error != "" {
			status = "Failed"
		}
		sb.WriteString(fmt.Sprintf("| %s | %d/100 | %s |\n", formatAgentName(string(r.AgentName)), r.Score, status))
	}
	sb.WriteString("\n---\n\n")

	// Agent sections
	for _, r := range job.AgentResults {
		if r == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", formatAgentName(string(r.AgentName))))

		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("> **Error:** %s\n\n", r.Error))
			continue
		}

		// Parse findings and render key fields
		var findings map[string]any
		if json.Unmarshal(r.Findings, &findings) == nil {
			renderFindings(&sb, string(r.AgentName), findings)
		} else {
			sb.WriteString("```json\n")
			sb.WriteString(string(r.Findings))
			sb.WriteString("\n```\n")
		}
		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

func renderFindings(sb *strings.Builder, agent string, f map[string]any) {
	switch agent {
	case "company-research":
		writeField(sb, "Industry", f["industry"])
		writeField(sb, "Sub-Industry", f["subIndustry"])
		writeField(sb, "Headquarters", f["headquarters"])
		writeField(sb, "Employee Estimate", f["employeeEstimate"])
		writeField(sb, "Revenue Estimate", f["revenueEstimate"])
		writeField(sb, "Business Model", f["businessModel"])
		writeField(sb, "Target Market", f["targetMarket"])
		writeField(sb, "Market Position", f["marketPosition"])
		writeList(sb, "Tech Stack", f["techStack"])
		writeList(sb, "Growth Signals", f["growthSignals"])
		writeField(sb, "Score Reasoning", f["scoreReasoning"])

	case "contact-finder":
		writeField(sb, "Email Pattern", f["emailPatternDetected"])
		writeField(sb, "Recommended Entry Point", f["recommendedEntryPoint"])
		if contacts, ok := f["contacts"].([]any); ok {
			sb.WriteString("\n### Contacts Found\n\n")
			sb.WriteString("| Name | Title | Department | Decision Maker | Priority |\n")
			sb.WriteString("|------|-------|------------|---------------|----------|\n")
			for _, c := range contacts {
				if cm, ok := c.(map[string]any); ok {
					sb.WriteString(fmt.Sprintf("| %v | %v | %v | %v | %v |\n",
						cm["name"], cm["title"], cm["department"], cm["decisionMaker"], cm["outreachPriority"]))
				}
			}
		}
		writeField(sb, "Score Reasoning", f["scoreReasoning"])

	case "opportunity-scoring":
		if bant, ok := f["bant"].(map[string]any); ok {
			sb.WriteString("\n### BANT Analysis\n\n")
			for _, dim := range []string{"budget", "authority", "need", "timeline"} {
				if d, ok := bant[dim].(map[string]any); ok {
					sb.WriteString(fmt.Sprintf("- **%s** (Score: %v): %v\n", strings.Title(dim), d["score"], d["reasoning"]))
				}
			}
		}
		writeField(sb, "BANT Score", f["bantScore"])
		writeField(sb, "Qualification Level", f["qualificationLevel"])
		if meddic, ok := f["meddic"].(map[string]any); ok {
			sb.WriteString("\n### MEDDIC Analysis\n\n")
			for _, key := range []string{"metrics", "economicBuyer", "decisionCriteria", "decisionProcess", "champion"} {
				writeField(sb, strings.Title(key), meddic[key])
			}
			writeList(sb, "Identified Pain", meddic["identifiedPain"])
		}

	case "competitive-analysis":
		writeField(sb, "Market Saturation", f["marketSaturation"])
		writeField(sb, "Differentiation Level", f["differentiationLevel"])
		writeList(sb, "Competitive Advantages", f["competitiveAdvantages"])
		writeList(sb, "Competitive Weaknesses", f["competitiveWeaknesses"])
		writeList(sb, "Opportunities", f["opportunities"])
		if competitors, ok := f["competitors"].([]any); ok {
			sb.WriteString("\n### Competitors\n\n")
			sb.WriteString("| Name | Similarity | Differentiator |\n")
			sb.WriteString("|------|-----------|----------------|\n")
			for _, c := range competitors {
				if cm, ok := c.(map[string]any); ok {
					sb.WriteString(fmt.Sprintf("| %v | %v | %v |\n", cm["name"], cm["similarity"], cm["differentiator"]))
				}
			}
		}

	case "outreach-strategy":
		writeField(sb, "Recommended Channel", f["recommendedChannel"])
		writeField(sb, "Channel Reasoning", f["channelReasoning"])
		writeList(sb, "Personalization Hooks", f["personalizationHooks"])
		if emails, ok := f["emailSequence"].([]any); ok {
			sb.WriteString("\n### Email Sequence\n\n")
			for _, e := range emails {
				if em, ok := e.(map[string]any); ok {
					sb.WriteString(fmt.Sprintf("#### Step %v (Day +%v) — %v\n\n", em["step"], em["dayOffset"], em["purpose"]))
					sb.WriteString(fmt.Sprintf("**Subject:** %v\n\n", em["subject"]))
					sb.WriteString(fmt.Sprintf("%v\n\n", em["body"]))
				}
			}
		}
		if timing, ok := f["timing"].(map[string]any); ok {
			sb.WriteString(fmt.Sprintf("\n**Best Day:** %v | **Best Time:** %v | **Cadence:** every %v days\n\n", timing["bestDay"], timing["bestTime"], timing["cadenceDays"]))
		}

	default:
		// Fallback: render all fields
		for k, v := range f {
			if k == "score" || k == "scoreReasoning" {
				continue
			}
			writeField(sb, k, v)
		}
		writeField(sb, "Score Reasoning", f["scoreReasoning"])
	}
}

func writeField(sb *strings.Builder, label string, value any) {
	if value == nil || value == "" {
		return
	}
	sb.WriteString(fmt.Sprintf("- **%s:** %v\n", label, value))
}

func writeList(sb *strings.Builder, label string, value any) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return
	}
	sb.WriteString(fmt.Sprintf("\n**%s:**\n", label))
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("- %v\n", item))
	}
}

func formatAgentName(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func extractCompanyName(job *models.Job) string {
	for _, r := range job.AgentResults {
		if r == nil || r.AgentName != models.AgentCompanyResearch {
			continue
		}
		var findings struct {
			CompanyName string `json:"companyName"`
		}
		if json.Unmarshal(r.Findings, &findings) == nil && findings.CompanyName != "" {
			return findings.CompanyName
		}
	}
	// Fallback: extract domain from URL
	url := job.CompanyURL
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "www.")
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}
	return url
}

func buildAgentDataMap(results []*models.AgentResult) map[string]any {
	data := make(map[string]any)
	for _, r := range results {
		if r == nil {
			continue
		}
		entry := map[string]any{
			"score":    r.Score,
			"latencyMs": r.LatencyMs,
		}
		if r.Error != "" {
			entry["error"] = r.Error
		}
		var findings any
		if json.Unmarshal(r.Findings, &findings) == nil {
			entry["findings"] = findings
		}
		data[string(r.AgentName)] = entry
	}
	return data
}

// dbCtx returns a background context for DB writes
func dbCtx() context.Context {
	return context.Background()
}
