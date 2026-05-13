package services

import (
	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

// AgentWeights defines the contribution of each agent to the composite score
var AgentWeights = map[models.AgentName]float64{
	models.AgentCompanyResearch:     0.25,
	models.AgentContactFinder:       0.20,
	models.AgentOpportunityScoring:  0.20,
	models.AgentCompetitiveAnalysis: 0.15,
	models.AgentOutreachStrategy:    0.20,
}

// Scorer computes weighted composite prospect scores
type Scorer struct{}

// NewScorer creates a new Scorer
func NewScorer() *Scorer {
	return &Scorer{}
}

// ScoreResult holds the computed composite score and per-agent breakdown
type ScoreResult struct {
	Total    int                   `json:"total"`
	Grade    string                `json:"grade"`
	PerAgent map[string]AgentScore `json:"perAgent"`
}

// AgentScore tracks one agent's contribution
type AgentScore struct {
	RawScore     int     `json:"rawScore"`
	Weight       float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
}

// Calculate computes a weighted composite score from agent results
func (s *Scorer) Calculate(results []*models.AgentResult) ScoreResult {
	sr := ScoreResult{
		PerAgent: make(map[string]AgentScore),
	}

	var weightedSum float64
	var totalWeight float64

	for _, r := range results {
		if r == nil || r.Error != "" {
			continue
		}

		weight, ok := AgentWeights[r.AgentName]
		if !ok {
			weight = 0.20 // default weight for unknown agents
		}

		contribution := float64(r.Score) * weight
		weightedSum += contribution
		totalWeight += weight

		sr.PerAgent[string(r.AgentName)] = AgentScore{
			RawScore:     r.Score,
			Weight:       weight,
			Contribution: contribution,
		}
	}

	// Normalize if not all agents succeeded (scale to full 100)
	if totalWeight > 0 {
		sr.Total = int(weightedSum / totalWeight)
	}

	// Clamp to 0-100
	if sr.Total > 100 {
		sr.Total = 100
	}
	if sr.Total < 0 {
		sr.Total = 0
	}

	sr.Grade = models.Grade(sr.Total)
	return sr
}
