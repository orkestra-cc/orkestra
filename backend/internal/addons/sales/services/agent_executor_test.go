package services

import (
	"strings"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

func TestJoinStrings(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a, b, c"},
		{[]string{"", "b"}, ", b"}, // empty entries are not filtered out
	}
	for _, c := range cases {
		if got := joinStrings(c.in); got != c.want {
			t.Errorf("joinStrings(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate_AgentExecutor(t *testing.T) {
	// Local helper distinct from billing.notification.truncate.
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestBuildAgentUserMessage_URLOnly(t *testing.T) {
	got := buildAgentUserMessage(&models.AgentInput{CompanyURL: "https://example.com"})
	if !strings.HasPrefix(got, "Analyze the company at: https://example.com\n\n") {
		t.Errorf("missing URL preamble, got:\n%s", got)
	}
}

func TestBuildAgentUserMessage_AllScrapedFields(t *testing.T) {
	input := &models.AgentInput{
		CompanyURL: "https://example.com",
		ScrapedData: &models.ScrapedCompanyData{
			CompanyName: "Acme",
			Industry:    "Technology",
			Description: "Cloud SaaS for ops",
			TechStack:   []string{"Go", "React"},
			TeamMembers: []string{"Alice CEO"},
			ContactInfo: "info@example.com",
			SocialLinks: []string{"https://twitter.com/acme"},
			AboutText:   "About text",
			RawText:     "Body text",
		},
	}
	got := buildAgentUserMessage(input)
	for _, want := range []string{
		"Company Name: Acme",
		"Industry: Technology",
		"Description: Cloud SaaS for ops",
		"Tech Stack: Go, React",
		"Team Members: Alice CEO",
		"Contact Info: info@example.com",
		"Social Links: https://twitter.com/acme",
		"--- About Page ---",
		"About text",
		"--- Website Content ---",
		"Body text",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in message:\n%s", want, got)
		}
	}
}

func TestBuildAgentUserMessage_TruncatesAboutAndRawText(t *testing.T) {
	bigAbout := strings.Repeat("A", 5000) // > 3000 → truncated
	bigRaw := strings.Repeat("B", 10000)  // > 8000 → truncated
	input := &models.AgentInput{
		CompanyURL: "u",
		ScrapedData: &models.ScrapedCompanyData{
			AboutText: bigAbout,
			RawText:   bigRaw,
		},
	}
	got := buildAgentUserMessage(input)
	if !strings.Contains(got, strings.Repeat("A", 3000)+"...") {
		t.Errorf("AboutText not truncated to 3000 chars")
	}
	if !strings.Contains(got, strings.Repeat("B", 8000)+"...") {
		t.Errorf("RawText not truncated to 8000 chars")
	}
}

func TestBuildAgentUserMessage_IncludesRegistryWhenPresent(t *testing.T) {
	input := &models.AgentInput{
		CompanyURL: "u",
		RegistryData: &models.CompanyEnrichmentData{
			CompanyName:   "Acme S.r.l.",
			TaxCode:       "RSSMRA85T10A562S",
			VATNumber:     "02081880490",
			LegalForm:     "SRL",
			Address:       "Via Roma 1",
			City:          "Roma",
			Province:      "RM",
			AtecoCode:     "62.01",
			AtecoDesc:     "Software",
			EmployeeRange: "10-50",
			RevenueRange:  "1-5M",
			FoundedYear:   "2010",
			Status:        "active",
		},
	}
	got := buildAgentUserMessage(input)
	if !strings.Contains(got, "--- Business Registry ---") {
		t.Errorf("missing registry block")
	}
	for _, want := range []string{"Acme S.r.l.", "VAT: 02081880490", "ATECO: 62.01 (Software)", "Founded: 2010"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing registry detail %q in:\n%s", want, got)
		}
	}
}

func TestAgentExecutor_BuildUserMessage_DelegatesToHelper(t *testing.T) {
	exec := NewAgentExecutor(0, 0, discardLogger())
	input := &models.AgentInput{CompanyURL: "https://x"}
	if exec.BuildUserMessage(input) != buildAgentUserMessage(input) {
		t.Errorf("BuildUserMessage should mirror buildAgentUserMessage")
	}
}

func TestNewAgentExecutor_AppliesDefaults(t *testing.T) {
	e := NewAgentExecutor(0, 0, discardLogger())
	if e.maxConcurrency != 5 {
		t.Errorf("default maxConcurrency = %d, want 5", e.maxConcurrency)
	}
	if e.maxTokens != 8192 {
		t.Errorf("default maxTokens = %d, want 8192", e.maxTokens)
	}
	e = NewAgentExecutor(7, 1024, discardLogger())
	if e.maxConcurrency != 7 || e.maxTokens != 1024 {
		t.Errorf("provided values should be preserved, got %d/%d", e.maxConcurrency, e.maxTokens)
	}
}
