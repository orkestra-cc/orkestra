package services

import (
	"testing"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

func TestScorer_Calculate_AllAgents(t *testing.T) {
	s := NewScorer()
	results := []*models.AgentResult{
		{AgentName: models.AgentCompanyResearch, Score: 80},
		{AgentName: models.AgentContactFinder, Score: 60},
		{AgentName: models.AgentOpportunityScoring, Score: 70},
		{AgentName: models.AgentCompetitiveAnalysis, Score: 50},
		{AgentName: models.AgentOutreachStrategy, Score: 90},
	}
	// weighted = 80*0.25 + 60*0.20 + 70*0.20 + 50*0.15 + 90*0.20 = 20 + 12 + 14 + 7.5 + 18 = 71.5
	// totalWeight = 1.0, normalized = 71.5 → int(71.5) = 71
	got := s.Calculate(results)
	if got.Total != 71 {
		t.Fatalf("Total = %d, want 71", got.Total)
	}
	if got.Grade != models.Grade(71) {
		t.Fatalf("Grade = %q, want %q", got.Grade, models.Grade(71))
	}
	if len(got.PerAgent) != 5 {
		t.Fatalf("PerAgent should hold 5 entries, got %d", len(got.PerAgent))
	}
	if got.PerAgent["company-research"].Weight != 0.25 {
		t.Errorf("company-research weight = %v", got.PerAgent["company-research"].Weight)
	}
	if got.PerAgent["company-research"].Contribution != 20 {
		t.Errorf("company-research contribution = %v, want 20", got.PerAgent["company-research"].Contribution)
	}
}

func TestScorer_Calculate_NormalizesPartialSuccess(t *testing.T) {
	s := NewScorer()
	// Only one agent (weight 0.25) succeeds with score 80 → normalized to full 100 → 80.
	results := []*models.AgentResult{
		{AgentName: models.AgentCompanyResearch, Score: 80},
		{AgentName: models.AgentContactFinder, Error: "timeout"},
		nil,
	}
	got := s.Calculate(results)
	if got.Total != 80 {
		t.Fatalf("Total = %d, want 80 (normalized)", got.Total)
	}
	if _, ok := got.PerAgent["contact-finder"]; ok {
		t.Errorf("failed agent should not appear in PerAgent")
	}
	if len(got.PerAgent) != 1 {
		t.Errorf("PerAgent len = %d, want 1", len(got.PerAgent))
	}
}

func TestScorer_Calculate_UnknownAgentDefaultsWeight(t *testing.T) {
	s := NewScorer()
	results := []*models.AgentResult{
		{AgentName: models.AgentName("custom-agent"), Score: 50},
	}
	got := s.Calculate(results)
	if got.PerAgent["custom-agent"].Weight != 0.20 {
		t.Errorf("unknown agent should get default weight 0.20, got %v", got.PerAgent["custom-agent"].Weight)
	}
	if got.Total != 50 {
		t.Errorf("Total = %d, want 50", got.Total)
	}
}

func TestScorer_Calculate_EmptyAndAllFailed(t *testing.T) {
	s := NewScorer()
	if got := s.Calculate(nil); got.Total != 0 || got.Grade != "F" {
		t.Errorf("empty results: Total=%d Grade=%q", got.Total, got.Grade)
	}
	results := []*models.AgentResult{
		{AgentName: models.AgentCompanyResearch, Error: "boom"},
		{AgentName: models.AgentContactFinder, Error: "boom"},
	}
	got := s.Calculate(results)
	if got.Total != 0 || got.Grade != "F" {
		t.Errorf("all-failed: Total=%d Grade=%q", got.Total, got.Grade)
	}
	if len(got.PerAgent) != 0 {
		t.Errorf("PerAgent should be empty when every agent errored, got %d", len(got.PerAgent))
	}
}

func TestScorer_Calculate_ClampsToRange(t *testing.T) {
	s := NewScorer()
	// Score above 100 is propagated but clamped by Calculate.
	results := []*models.AgentResult{
		{AgentName: models.AgentCompanyResearch, Score: 200},
	}
	if got := s.Calculate(results); got.Total != 100 {
		t.Errorf("Total = %d, want 100 (clamped)", got.Total)
	}
	results = []*models.AgentResult{
		{AgentName: models.AgentCompanyResearch, Score: -50},
	}
	if got := s.Calculate(results); got.Total != 0 {
		t.Errorf("Total = %d, want 0 (clamped)", got.Total)
	}
}
