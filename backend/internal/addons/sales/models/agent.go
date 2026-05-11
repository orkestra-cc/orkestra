package models

import (
	"context"
	"encoding/json"

	"github.com/orkestra/backend/internal/shared/iface"
)

// SalesAgent defines a parallel prospect analysis agent
type SalesAgent interface {
	Name() AgentName
	Weight() float64
	Execute(ctx context.Context, input *AgentInput, llm iface.LLMProvider, systemPrompt string) (*AgentResult, error)
}

// AgentInput is the data passed to each agent during parallel analysis
type AgentInput struct {
	CompanyURL   string
	ScrapedData  *ScrapedCompanyData
	RegistryData *CompanyEnrichmentData // nil if not Italian or company module unavailable
	Locale       string
	PriorResults map[AgentName]*AgentResult
}

// ScrapedCompanyData holds structured data extracted from web scraping
type ScrapedCompanyData struct {
	URL           string   `json:"url"`
	CompanyName   string   `json:"companyName"`
	Industry      string   `json:"industry"`
	Description   string   `json:"description"`
	TechStack     []string `json:"techStack"`
	TeamMembers   []string `json:"teamMembers"`
	AboutText     string   `json:"aboutText"`
	ContactInfo   string   `json:"contactInfo"`
	SocialLinks   []string `json:"socialLinks"`
	PageTitles    []string `json:"pageTitles"`
	RawText       string   `json:"rawText"`
}

// CompanyEnrichmentData holds data from the Italian business registry
type CompanyEnrichmentData struct {
	CompanyName   string `json:"companyName"`
	TaxCode       string `json:"taxCode"`
	VATNumber     string `json:"vatNumber"`
	LegalForm     string `json:"legalForm"`
	Address       string `json:"address"`
	Province      string `json:"province"`
	City          string `json:"city"`
	AtecoCode     string `json:"atecoCode"`
	AtecoDesc     string `json:"atecoDescription"`
	EmployeeRange string `json:"employeeRange"`
	RevenueRange  string `json:"revenueRange"`
	FoundedYear   string `json:"foundedYear"`
	Status        string `json:"status"`
}

// AgentResult holds the output from a single agent execution
type AgentResult struct {
	AgentName    AgentName       `json:"agentName"`
	Score        int             `json:"score"`
	Findings     json.RawMessage `json:"findings"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	LatencyMs    int64           `json:"latencyMs"`
	Error        string          `json:"error,omitempty"`
}
