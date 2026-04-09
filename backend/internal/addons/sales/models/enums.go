package models

// JobStatus represents the lifecycle state of a sales intelligence job
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusDiscovery JobStatus = "discovery"
	JobStatusAnalysis  JobStatus = "analysis"
	JobStatusSynthesis JobStatus = "synthesis"
	JobStatusCompleted JobStatus = "completed"
	JobStatusBatchPending JobStatus = "batch_pending"
	JobStatusFailed      JobStatus = "failed"
	JobStatusCancelled   JobStatus = "cancelled"
)

// SkillName identifies a sales intelligence skill
type SkillName string

const (
	SkillResearch    SkillName = "research"
	SkillQualify     SkillName = "qualify"
	SkillContacts    SkillName = "contacts"
	SkillOutreach    SkillName = "outreach"
	SkillFollowup    SkillName = "followup"
	SkillPrep        SkillName = "prep"
	SkillProposal    SkillName = "proposal"
	SkillObjections  SkillName = "objections"
	SkillICP         SkillName = "icp"
	SkillCompetitors SkillName = "competitors"
)

// ValidSkills is the set of valid skill names
var ValidSkills = map[SkillName]bool{
	SkillResearch:    true,
	SkillQualify:     true,
	SkillContacts:    true,
	SkillOutreach:    true,
	SkillFollowup:    true,
	SkillPrep:        true,
	SkillProposal:    true,
	SkillObjections:  true,
	SkillICP:         true,
	SkillCompetitors: true,
}

// AgentName identifies one of the 5 parallel prospect analysis agents
type AgentName string

const (
	AgentCompanyResearch    AgentName = "company-research"
	AgentContactFinder      AgentName = "contact-finder"
	AgentOpportunityScoring AgentName = "opportunity-scoring"
	AgentCompetitiveAnalysis AgentName = "competitive-analysis"
	AgentOutreachStrategy   AgentName = "outreach-strategy"
)

// Grade maps a prospect score (0-100) to a letter grade
func Grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}
