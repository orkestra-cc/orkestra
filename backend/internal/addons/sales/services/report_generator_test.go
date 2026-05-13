package services

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFormatAgentName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"company-research", "Company Research"},
		{"opportunity-scoring", "Opportunity Scoring"},
		{"foo", "Foo"},
		{"", ""},
		{"a-b-c", "A B C"},
	}
	for _, c := range cases {
		if got := formatAgentName(c.in); got != c.want {
			t.Errorf("formatAgentName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractCompanyName_FromCompanyResearchFindings(t *testing.T) {
	findings, _ := json.Marshal(map[string]string{"companyName": "Acme S.r.l."})
	job := &models.Job{
		CompanyURL: "https://acme.example.com/about",
		AgentResults: []*models.AgentResult{
			{AgentName: models.AgentCompanyResearch, Findings: findings},
		},
	}
	if got := extractCompanyName(job); got != "Acme S.r.l." {
		t.Errorf("got %q, want Acme S.r.l.", got)
	}
}

func TestExtractCompanyName_FallsBackToURLHost(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://example.com/about", "example.com"},
		{"http://example.com", "example.com"},
		{"www.example.com/path", "example.com"},
		{"https://www.example.com", "example.com"},
	}
	for _, c := range cases {
		job := &models.Job{CompanyURL: c.url}
		if got := extractCompanyName(job); got != c.want {
			t.Errorf("extractCompanyName(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestExtractCompanyName_SkipsNilOrUnnamedResults(t *testing.T) {
	job := &models.Job{
		CompanyURL: "https://example.com",
		AgentResults: []*models.AgentResult{
			nil,
			{AgentName: models.AgentContactFinder, Findings: json.RawMessage(`{"companyName":"Wrong"}`)},
			{AgentName: models.AgentCompanyResearch, Findings: json.RawMessage(`{"companyName":""}`)},
		},
	}
	// The company-research findings carry an empty companyName → fall back to URL host.
	if got := extractCompanyName(job); got != "example.com" {
		t.Errorf("got %q, want example.com", got)
	}
}

func TestBuildAgentDataMap(t *testing.T) {
	results := []*models.AgentResult{
		nil,
		{AgentName: models.AgentCompanyResearch, Score: 80, LatencyMs: 1200, Findings: json.RawMessage(`{"industry":"SaaS"}`)},
		{AgentName: models.AgentContactFinder, Score: 0, Error: "timeout"},
		// Invalid JSON findings — should still produce an entry without findings field.
		{AgentName: models.AgentName("misc"), Score: 50, Findings: json.RawMessage(`not json`)},
	}
	got := buildAgentDataMap(results)
	if _, ok := got["company-research"]; !ok {
		t.Fatal("company-research entry missing")
	}
	if entry, ok := got["company-research"].(map[string]any); !ok || entry["score"] != 80 {
		t.Errorf("company-research entry malformed: %+v", got["company-research"])
	}
	contactEntry, _ := got["contact-finder"].(map[string]any)
	if contactEntry["error"] != "timeout" {
		t.Errorf("error not propagated, got %+v", contactEntry)
	}
	miscEntry, _ := got["misc"].(map[string]any)
	if _, hasFindings := miscEntry["findings"]; hasFindings {
		t.Errorf("invalid JSON should NOT yield a findings field, got %+v", miscEntry)
	}
}

func TestWriteField(t *testing.T) {
	var sb strings.Builder
	writeField(&sb, "Industry", "SaaS")
	if !strings.Contains(sb.String(), "- **Industry:** SaaS\n") {
		t.Errorf("writeField output unexpected: %q", sb.String())
	}

	// Nil and empty-string values are skipped.
	sb.Reset()
	writeField(&sb, "X", nil)
	writeField(&sb, "X", "")
	if sb.Len() != 0 {
		t.Errorf("writeField with nil/empty should write nothing, got %q", sb.String())
	}
}

func TestWriteList(t *testing.T) {
	var sb strings.Builder
	writeList(&sb, "Tech Stack", []any{"Go", "React"})
	out := sb.String()
	if !strings.Contains(out, "**Tech Stack:**") || !strings.Contains(out, "- Go\n") || !strings.Contains(out, "- React\n") {
		t.Errorf("writeList output: %q", out)
	}

	// Non-slice or empty → skipped.
	sb.Reset()
	writeList(&sb, "X", "not-a-slice")
	writeList(&sb, "X", []any{})
	writeList(&sb, "X", nil)
	if sb.Len() != 0 {
		t.Errorf("writeList should be no-op for non-list/empty, got %q", sb.String())
	}
}

func TestRenderFindings_CompanyResearch(t *testing.T) {
	var sb strings.Builder
	renderFindings(&sb, "company-research", map[string]any{
		"industry":       "SaaS",
		"techStack":      []any{"Go", "React"},
		"scoreReasoning": "Strong fit",
	})
	out := sb.String()
	if !strings.Contains(out, "**Industry:** SaaS") {
		t.Errorf("expected Industry field: %q", out)
	}
	if !strings.Contains(out, "**Tech Stack:**") || !strings.Contains(out, "- Go") {
		t.Errorf("expected tech stack list: %q", out)
	}
	if !strings.Contains(out, "**Score Reasoning:** Strong fit") {
		t.Errorf("expected score reasoning: %q", out)
	}
}

func TestRenderFindings_DefaultFallback(t *testing.T) {
	var sb strings.Builder
	renderFindings(&sb, "unknown-agent", map[string]any{
		"score":          90,        // explicitly excluded
		"scoreReasoning": "ok",      // rendered separately
		"customField":    "myValue", // generic render
	})
	out := sb.String()
	if strings.Contains(out, "score:") {
		t.Errorf("default branch must not render the 'score' field, got %q", out)
	}
	if !strings.Contains(out, "myValue") {
		t.Errorf("default branch must render custom fields, got %q", out)
	}
	if !strings.Contains(out, "**Score Reasoning:** ok") {
		t.Errorf("default branch must render score reasoning at end, got %q", out)
	}
}

func TestReportGenerator_buildMarkdown_TopLineHeaders(t *testing.T) {
	job := &models.Job{
		UUID:       "j-1",
		CompanyURL: "https://example.com",
		TotalScore: 73,
		Grade:      "B",
		Locale:     "en",
		AgentResults: []*models.AgentResult{
			{AgentName: models.AgentCompanyResearch, Score: 80, Findings: json.RawMessage(`{"industry":"SaaS"}`)},
			{AgentName: models.AgentContactFinder, Score: 0, Error: "timeout"},
		},
	}
	g := &ReportGenerator{logger: discardLogger().With(slog.String("c", "test"))}
	md := g.buildMarkdown(job, "Acme", map[string]any{})

	for _, want := range []string{
		"# Prospect Report: Acme",
		"**URL:** https://example.com",
		"**Score:** 73/100 (Grade: B)",
		"**Locale:** en",
		"## Score Summary",
		"| Company Research | 80/100 | Completed |",
		"| Contact Finder | 0/100 | Failed |",
		"## Company Research",
		"## Contact Finder",
		"> **Error:** timeout",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestReportGenerator_buildMarkdown_FallsBackToRawJSONForUnparsableFindings(t *testing.T) {
	job := &models.Job{
		UUID:       "j-1",
		CompanyURL: "https://example.com",
		AgentResults: []*models.AgentResult{
			{AgentName: models.AgentCompanyResearch, Score: 80, Findings: json.RawMessage(`not-json`)},
		},
	}
	g := &ReportGenerator{logger: discardLogger().With(slog.String("c", "test"))}
	md := g.buildMarkdown(job, "Acme", map[string]any{})
	if !strings.Contains(md, "```json") || !strings.Contains(md, "not-json") {
		t.Errorf("expected raw JSON fallback block in:\n%s", md)
	}
}

func TestDbCtx_ReturnsBackgroundContext(t *testing.T) {
	if dbCtx() == nil {
		t.Fatal("dbCtx returned nil")
	}
}
