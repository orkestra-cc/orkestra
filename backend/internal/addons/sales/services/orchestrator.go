package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra-cc/orkestra-addon-sales/config"
	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"github.com/orkestra-cc/orkestra-addon-sales/repository"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// OrchestratorService manages the lifecycle of sales intelligence jobs and skill execution
type OrchestratorService interface {
	RunSkill(ctx context.Context, skillName models.SkillName, url, locale, extraContext, userID string) (*models.SkillResultInternal, error)
	CreateProspectJob(ctx context.Context, url, locale, userID string) (*models.Job, error)
	RunQuickProspect(ctx context.Context, url, locale, userID string) (*models.QuickProspectResult, error)
	GetJob(ctx context.Context, jobID string) (*models.Job, error)
	ListJobs(ctx context.Context, userID, status string, page, pageSize int) ([]models.Job, int64, error)
	CancelJob(ctx context.Context, jobID, userID string) error
	DeleteJob(ctx context.Context, jobID, userID string) error
	RetryJob(ctx context.Context, jobID, userID string) (*models.Job, error)
	RerunFailedAgents(ctx context.Context, jobID, userID string) (*models.Job, error)
}

type orchestratorService struct {
	jobRepo       repository.JobRepository
	reportRepo    repository.ReportRepository
	settingsRepo  repository.SettingsRepository
	batchRepo     repository.BatchRepository
	modelProvider AIModelProvider
	promptLoader  *PromptLoader
	scraper       *Scraper
	agentExecutor *AgentExecutor
	scorer        *Scorer
	enrichment    CompanyEnrichmentService // optional
	reportGen     *ReportGenerator
	cfg           config.SalesConfig
	logger        *slog.Logger

	// Track running jobs for cancellation
	runningJobs   map[string]context.CancelFunc
	runningJobsMu sync.Mutex
}

// NewOrchestrator creates a new OrchestratorService
func NewOrchestrator(
	jobRepo repository.JobRepository,
	reportRepo repository.ReportRepository,
	settingsRepo repository.SettingsRepository,
	batchRepo repository.BatchRepository,
	modelProvider AIModelProvider,
	promptLoader *PromptLoader,
	scraper *Scraper,
	agentExecutor *AgentExecutor,
	scorer *Scorer,
	enrichment CompanyEnrichmentService,
	reportGen *ReportGenerator,
	cfg config.SalesConfig,
	logger *slog.Logger,
) OrchestratorService {
	return &orchestratorService{
		jobRepo:       jobRepo,
		reportRepo:    reportRepo,
		settingsRepo:  settingsRepo,
		batchRepo:     batchRepo,
		modelProvider: modelProvider,
		promptLoader:  promptLoader,
		scraper:       scraper,
		agentExecutor: agentExecutor,
		scorer:        scorer,
		enrichment:    enrichment,
		reportGen:     reportGen,
		cfg:           cfg,
		logger:        logger.With(slog.String("service", "sales-orchestrator")),
		runningJobs:   make(map[string]context.CancelFunc),
	}
}

// userLLMSettings holds resolved LLM provider and user preferences
type userLLMSettings struct {
	llm       iface.LLMProvider
	modelUUID string
	temp      float64
	maxTokens int
	locale    string
	batchMode bool
}

// getLLMForUser resolves the LLM provider based on user settings, falling back to system default
func (s *orchestratorService) getLLMForUser(ctx context.Context, userID string) (*userLLMSettings, error) {
	settings, _ := s.settingsRepo.GetByUser(ctx, userID)

	result := &userLLMSettings{
		locale: s.cfg.DefaultLocale,
	}

	var err error
	if settings != nil && settings.ModelUUID != "" {
		result.llm, err = s.modelProvider.GetLLMProvider(ctx, settings.ModelUUID)
		result.modelUUID = settings.ModelUUID
	} else {
		result.llm, err = s.modelProvider.GetDefaultLLMProvider(ctx)
	}
	if err != nil {
		return nil, err
	}

	if settings != nil {
		if settings.Temperature > 0 {
			result.temp = settings.Temperature
		}
		if settings.MaxTokens > 0 {
			result.maxTokens = settings.MaxTokens
		}
		if settings.Locale != "" {
			result.locale = settings.Locale
		}
		result.batchMode = settings.BatchMode
	}

	return result, nil
}

// ---------- Skill Execution ----------

func (s *orchestratorService) RunSkill(ctx context.Context, skillName models.SkillName, url, locale, extraContext, userID string) (*models.SkillResultInternal, error) {
	if !models.ValidSkills[skillName] {
		return nil, fmt.Errorf("unknown skill: %s", skillName)
	}

	// Use a background context with skill timeout so the LLM call isn't
	// cancelled by client/proxy disconnects (e.g. Cloudflare dropping the connection).
	// Same pattern as runProspectPipeline which uses context.Background().
	skillTimeout := s.cfg.SkillTimeout
	if skillTimeout == 0 {
		skillTimeout = 90 * time.Second
	}
	skillCtx, cancel := context.WithTimeout(context.Background(), skillTimeout)
	defer cancel()

	us, err := s.getLLMForUser(skillCtx, userID)
	if err != nil {
		return nil, fmt.Errorf("get LLM provider: %w", err)
	}
	if locale == "" {
		locale = us.locale
	}

	vars := map[string]any{
		"URL":     url,
		"Context": extraContext,
		"Locale":  localeToLanguage(locale),
	}
	systemPrompt, err := s.promptLoader.LoadSkill(string(skillName), locale, vars)
	if err != nil {
		return nil, fmt.Errorf("load prompt for skill %s: %w", skillName, err)
	}

	userMessage := fmt.Sprintf("Analyze the company at URL: %s", url)
	if extraContext != "" {
		userMessage += fmt.Sprintf("\n\nAdditional context: %s", extraContext)
	}

	start := time.Now()
	var text string
	var inputTokens, outputTokens int

	// Apply user settings with sensible defaults
	temp := us.temp
	if temp == 0 {
		temp = 0.3
	}
	maxTok := us.maxTokens
	if maxTok == 0 {
		maxTok = 4096
	}

	opts := iface.CompletionOptions{
		SystemPrompt: systemPrompt,
		Temperature:  temp,
		MaxTokens:    maxTok,
	}

	if usageProvider, ok := us.llm.(iface.LLMProviderWithUsage); ok {
		result, callErr := usageProvider.CompleteWithUsage(skillCtx, userMessage, opts)
		if callErr != nil {
			return nil, fmt.Errorf("LLM completion for skill %s: %w", skillName, callErr)
		}
		text = result.Text
		inputTokens = result.InputTokens
		outputTokens = result.OutputTokens
	} else {
		text, err = us.llm.Complete(skillCtx, userMessage, opts)
		if err != nil {
			return nil, fmt.Errorf("LLM completion for skill %s: %w", skillName, err)
		}
	}
	latencyMs := time.Since(start).Milliseconds()

	var resultJSON json.RawMessage
	if json.Valid([]byte(text)) {
		resultJSON = json.RawMessage(text)
	} else {
		wrapped, _ := json.Marshal(map[string]string{"response": text})
		resultJSON = json.RawMessage(wrapped)
	}

	s.logger.Info("skill executed",
		slog.String("skill", string(skillName)),
		slog.String("url", url),
		slog.String("model", us.llm.ModelName()),
		slog.Int64("latencyMs", latencyMs),
	)

	return &models.SkillResultInternal{
		Skill:        string(skillName),
		Result:       resultJSON,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    latencyMs,
		ModelUsed:    us.llm.ModelName(),
	}, nil
}

// ---------- Prospect Pipeline ----------

// CreateProspectJob creates an async prospect job and starts the 3-phase pipeline in background
func (s *orchestratorService) CreateProspectJob(ctx context.Context, url, locale, userID string) (*models.Job, error) {
	// Resolve LLM and settings now (before goroutine) so we have access to user context
	us, err := s.getLLMForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get LLM provider: %w", err)
	}
	if locale == "" {
		locale = us.locale
	}

	job := &models.Job{
		UUID:       uuid.New().String(),
		CreatedBy:  userID,
		CompanyURL: url,
		Locale:     locale,
		Status:     models.JobStatusQueued,
		Phases: []models.JobPhase{
			{Name: "discovery", Status: "pending"},
			{Name: "analysis", Status: "pending"},
			{Name: "synthesis", Status: "pending"},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("create prospect job: %w", err)
	}

	// Run pipeline in background with separate timeout
	pipelineCtx, cancel := context.WithTimeout(context.Background(), s.cfg.FullTimeout)

	s.runningJobsMu.Lock()
	s.runningJobs[job.UUID] = cancel
	s.runningJobsMu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			s.runningJobsMu.Lock()
			delete(s.runningJobs, job.UUID)
			s.runningJobsMu.Unlock()
		}()

		s.runProspectPipeline(pipelineCtx, job, us)
	}()

	s.logger.Info("prospect job created", slog.String("jobId", job.UUID), slog.String("url", url))
	return job, nil
}

// RunQuickProspect runs a synchronous, abbreviated prospect analysis
func (s *orchestratorService) RunQuickProspect(ctx context.Context, url, locale, userID string) (*models.QuickProspectResult, error) {
	if locale == "" {
		locale = s.cfg.DefaultLocale
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.QuickTimeout)
	defer cancel()

	// Phase 1: Discovery (scrape)
	scraped, err := s.scraper.ScrapeCompany(url)
	if err != nil {
		return nil, fmt.Errorf("scrape failed: %w", err)
	}

	// Quick mode: run only company-research agent
	us, err := s.getLLMForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get LLM provider: %w", err)
	}

	promptVars := s.buildPromptVars(scraped, nil, locale, url)
	prompt, err := s.promptLoader.LoadAgent("company-research", locale, promptVars)
	if err != nil {
		return nil, fmt.Errorf("load research prompt: %w", err)
	}

	input := &models.AgentInput{
		CompanyURL:  url,
		ScrapedData: scraped,
		Locale:      locale,
	}

	agents := []AgentDef{{
		Name:   models.AgentCompanyResearch,
		Weight: 1.0,
		Prompt: prompt,
	}}

	results := s.agentExecutor.RunParallel(ctx, agents, input, us.llm, nil)

	result := &models.QuickProspectResult{
		CompanyName: scraped.CompanyName,
	}

	if len(results) == 0 || results[0] == nil {
		return nil, fmt.Errorf("agent %s produced no result", models.AgentCompanyResearch)
	}
	r := results[0]
	if r.Error != "" {
		return nil, fmt.Errorf("agent %s failed: %s", r.AgentName, r.Error)
	}

	result.Score = r.Score
	result.Grade = models.Grade(r.Score)
	result.Findings = r.Findings
	result.InputTokens = r.InputTokens
	result.OutputTokens = r.OutputTokens
	result.LatencyMs = r.LatencyMs

	var findings struct {
		MarketPosition string `json:"marketPosition"`
	}
	if json.Unmarshal(r.Findings, &findings) == nil && findings.MarketPosition != "" {
		result.Summary = findings.MarketPosition
	}

	return result, nil
}

// ---------- Job Management ----------

func (s *orchestratorService) GetJob(ctx context.Context, jobID string) (*models.Job, error) {
	return s.jobRepo.GetByUUID(ctx, jobID)
}

func (s *orchestratorService) ListJobs(ctx context.Context, userID, status string, page, pageSize int) ([]models.Job, int64, error) {
	return s.jobRepo.ListByUser(ctx, userID, status, page, pageSize)
}

func (s *orchestratorService) CancelJob(ctx context.Context, jobID, userID string) error {
	job, err := s.jobRepo.GetByUUID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	if job.CreatedBy != userID {
		return fmt.Errorf("not authorized to cancel this job")
	}

	// Cancel the running goroutine
	s.runningJobsMu.Lock()
	if cancelFn, ok := s.runningJobs[jobID]; ok {
		cancelFn()
		delete(s.runningJobs, jobID)
	}
	s.runningJobsMu.Unlock()

	return s.jobRepo.UpdateStatus(ctx, jobID, models.JobStatusCancelled, "cancelled by user")
}

func (s *orchestratorService) DeleteJob(ctx context.Context, jobID, userID string) error {
	job, err := s.jobRepo.GetByUUID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	if job.CreatedBy != userID {
		return fmt.Errorf("not authorized to delete this job")
	}

	// Cancel if still running
	s.runningJobsMu.Lock()
	if cancelFn, ok := s.runningJobs[jobID]; ok {
		cancelFn()
		delete(s.runningJobs, jobID)
	}
	s.runningJobsMu.Unlock()

	// Cascade delete associated report
	s.reportRepo.DeleteByJobUUID(ctx, jobID)

	return s.jobRepo.Delete(ctx, jobID)
}

func (s *orchestratorService) RetryJob(ctx context.Context, jobID, userID string) (*models.Job, error) {
	old, err := s.jobRepo.GetByUUID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	if old.CreatedBy != userID {
		return nil, fmt.Errorf("not authorized to retry this job")
	}
	if old.Status != models.JobStatusFailed && old.Status != models.JobStatusCancelled {
		return nil, fmt.Errorf("only failed or cancelled jobs can be retried (current status: %s)", old.Status)
	}

	return s.CreateProspectJob(ctx, old.CompanyURL, old.Locale, userID)
}

func (s *orchestratorService) RerunFailedAgents(ctx context.Context, jobID, userID string) (*models.Job, error) {
	job, err := s.jobRepo.GetByUUID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	if job.CreatedBy != userID {
		return nil, fmt.Errorf("not authorized")
	}
	if job.Status != models.JobStatusCompleted && job.Status != models.JobStatusFailed && job.Status != models.JobStatusAnalysis {
		return nil, fmt.Errorf("can only re-run agents on completed, failed, or stuck jobs (current: %s)", job.Status)
	}

	// Identify failed agents (errored or score 0 with no real findings)
	failedAgentNames := make(map[models.AgentName]int) // name -> index in agentResults
	for i, r := range job.AgentResults {
		if r == nil {
			continue
		}
		if r.Error != "" || (r.Score == 0 && len(r.Findings) <= 2) {
			failedAgentNames[r.AgentName] = i
		}
	}

	if len(failedAgentNames) == 0 {
		return job, nil // nothing to re-run
	}

	// Get LLM provider
	us, err := s.getLLMForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get LLM provider: %w", err)
	}

	// Re-scrape for prompt template vars
	scraped, err := s.scraper.ScrapeCompany(job.CompanyURL)
	if err != nil {
		return nil, fmt.Errorf("scrape failed: %w", err)
	}

	promptVars := s.buildPromptVars(scraped, nil, job.Locale, job.CompanyURL)

	// Build only the failed agent defs
	allDefs, err := s.buildAgentDefs(job.Locale, promptVars)
	if err != nil {
		return nil, fmt.Errorf("load agent prompts: %w", err)
	}

	var rerunDefs []AgentDef
	for _, d := range allDefs {
		if _, failed := failedAgentNames[d.Name]; failed {
			rerunDefs = append(rerunDefs, d)
		}
	}

	if len(rerunDefs) == 0 {
		return job, nil
	}

	// Update job status to show it's re-running
	s.updatePhase(job, "analysis", "running")
	s.jobRepo.UpdateStatus(context.Background(), job.UUID, models.JobStatusAnalysis, "")

	// Run in background goroutine (not tied to HTTP request context)
	go func() {
		rerunCtx, cancel := context.WithTimeout(context.Background(), s.cfg.FullTimeout)
		defer cancel()

		input := &models.AgentInput{
			CompanyURL:  job.CompanyURL,
			ScrapedData: scraped,
			Locale:      job.Locale,
		}

		newResults := s.agentExecutor.RunParallel(rerunCtx, rerunDefs, input, us.llm, nil)

		// Merge: replace old failed results with new ones
		for j, newR := range newResults {
			if newR == nil {
				continue
			}
			agentName := rerunDefs[j].Name
			if idx, ok := failedAgentNames[agentName]; ok {
				job.AgentResults[idx] = newR
			}
		}

		// Recalculate score
		scoreResult := s.scorer.Calculate(job.AgentResults)
		job.TotalScore = scoreResult.Total
		job.Grade = scoreResult.Grade
		job.ErrorMessage = ""

		// Regenerate report
		if s.reportGen != nil {
			s.reportRepo.DeleteByJobUUID(context.Background(), job.UUID)
			report, genErr := s.reportGen.GenerateFromJob(job)
			if genErr != nil {
				s.logger.Error("failed to regenerate report", slog.String("jobId", job.UUID), slog.String("error", genErr.Error()))
			} else {
				job.ReportUUID = report.UUID
			}
		}

		// Always save final state
		now := time.Now()
		job.Status = models.JobStatusCompleted
		job.CompletedAt = &now
		job.UpdatedAt = now
		s.updatePhase(job, "analysis", "completed")
		s.updatePhase(job, "synthesis", "completed")

		if saveErr := s.jobRepo.UpdateFull(context.Background(), job); saveErr != nil {
			s.logger.Error("failed to save rerun results", slog.String("jobId", job.UUID), slog.String("error", saveErr.Error()))
		}

		s.logger.Info("re-ran failed agents",
			slog.String("jobId", job.UUID),
			slog.Int("rerunCount", len(rerunDefs)),
			slog.Int("newScore", job.TotalScore),
			slog.String("grade", job.Grade),
		)
	}()

	return job, nil
}

// ---------- Three-Phase Pipeline ----------

func (s *orchestratorService) runProspectPipeline(ctx context.Context, job *models.Job, us *userLLMSettings) {
	llm := us.llm
	// Use a separate context for DB writes so they succeed even if the pipeline context times out
	dbCtx := context.Background()

	// Phase 1: Discovery
	s.updatePhase(job, "discovery", "running")
	s.jobRepo.UpdateStatus(dbCtx, job.UUID, models.JobStatusDiscovery, "")

	scraped, err := s.scraper.ScrapeCompany(job.CompanyURL)
	if err != nil {
		s.failJob(dbCtx, job, "discovery", fmt.Sprintf("scrape failed: %v", err))
		return
	}

	// Optional: enrich with Italian business registry
	var enrichmentData *models.CompanyEnrichmentData
	if s.enrichment != nil && job.Locale == "it" && scraped.CompanyName != "" {
		enrichmentData, _ = s.enrichment.EnrichCompany(ctx, scraped.CompanyName, "")
	}

	s.updatePhase(job, "discovery", "completed")

	// Phase 2: Parallel Agent Analysis
	s.updatePhase(job, "analysis", "running")
	s.jobRepo.UpdateStatus(dbCtx, job.UUID, models.JobStatusAnalysis, "")

	promptVars := s.buildPromptVars(scraped, enrichmentData, job.Locale, job.CompanyURL)

	agentDefs, err := s.buildAgentDefs(job.Locale, promptVars)
	if err != nil {
		s.failJob(dbCtx, job, "analysis", fmt.Sprintf("load agent prompts: %v", err))
		return
	}

	input := &models.AgentInput{
		CompanyURL:   job.CompanyURL,
		ScrapedData:  scraped,
		RegistryData: enrichmentData,
		Locale:       job.Locale,
	}

	// Check if batch mode is enabled and provider supports it
	if batchProvider, ok := llm.(iface.BatchLLMProvider); ok && us.batchMode && s.batchRepo != nil {
		s.submitBatchAnalysis(dbCtx, job, batchProvider, us.modelUUID, agentDefs, input)
		return // pipeline continues via batch poller
	}

	results := s.agentExecutor.RunParallel(ctx, agentDefs, input, llm, nil)
	job.AgentResults = results

	s.updatePhase(job, "analysis", "completed")

	// Phase 3: Synthesis
	s.updatePhase(job, "synthesis", "running")
	s.jobRepo.UpdateStatus(dbCtx, job.UUID, models.JobStatusSynthesis, "")

	scoreResult := s.scorer.Calculate(results)
	job.TotalScore = scoreResult.Total
	job.Grade = scoreResult.Grade

	// Generate report
	if s.reportGen != nil {
		report, err := s.reportGen.GenerateFromJob(job)
		if err != nil {
			s.logger.Error("failed to generate report", slog.String("jobId", job.UUID), slog.String("error", err.Error()))
		} else {
			job.ReportUUID = report.UUID
		}
	}

	s.updatePhase(job, "synthesis", "completed")

	// Mark complete
	now := time.Now()
	job.Status = models.JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now

	if err := s.jobRepo.UpdateFull(dbCtx, job); err != nil {
		s.logger.Error("failed to save completed job", slog.String("jobId", job.UUID), slog.String("error", err.Error()))
	}

	s.logger.Info("prospect pipeline completed",
		slog.String("jobId", job.UUID),
		slog.String("url", job.CompanyURL),
		slog.Int("score", job.TotalScore),
		slog.String("grade", job.Grade),
	)
}

// submitBatchAnalysis submits all agent prompts as a single batch and hands off to the poller
func (s *orchestratorService) submitBatchAnalysis(
	ctx context.Context,
	job *models.Job,
	batchProvider iface.BatchLLMProvider,
	modelUUID string,
	agentDefs []AgentDef,
	input *models.AgentInput,
) {
	// Build batch requests from agent defs
	requests := make([]iface.BatchRequest, len(agentDefs))
	requestMap := make(map[string]int, len(agentDefs))

	for i, def := range agentDefs {
		customID := string(def.Name)
		requests[i] = iface.BatchRequest{
			CustomID: customID,
			Prompt:   s.agentExecutor.BuildUserMessage(input),
			Options: iface.CompletionOptions{
				SystemPrompt: def.Prompt,
				Temperature:  0.3,
				MaxTokens:    s.cfg.MaxTokens,
			},
		}
		requestMap[customID] = i
	}

	submission, err := batchProvider.SubmitBatch(ctx, requests)
	if err != nil {
		s.failJob(ctx, job, "analysis", fmt.Sprintf("batch submit failed: %v", err))
		return
	}

	// Persist batch record
	batchJob := &models.BatchJob{
		UUID:       uuid.New().String(),
		JobUUID:    job.UUID,
		ModelUUID:  modelUUID,
		Provider:   submission.Provider,
		BatchID:    submission.BatchID,
		Status:     "submitted",
		RequestMap: requestMap,
	}
	if err := s.batchRepo.Create(ctx, batchJob); err != nil {
		s.failJob(ctx, job, "analysis", fmt.Sprintf("save batch record: %v", err))
		return
	}

	// Update job to batch_pending
	s.jobRepo.UpdateStatus(ctx, job.UUID, models.JobStatusBatchPending, "")

	s.logger.Info("batch submitted",
		slog.String("jobId", job.UUID),
		slog.String("batchId", submission.BatchID),
		slog.String("provider", submission.Provider),
		slog.Int("agents", len(agentDefs)),
	)
}

func (s *orchestratorService) buildAgentDefs(locale string, vars map[string]any) ([]AgentDef, error) {
	type agentMeta struct {
		name   models.AgentName
		file   string
		weight float64
	}
	agents := []agentMeta{
		{models.AgentCompanyResearch, "company-research", 0.25},
		{models.AgentContactFinder, "contact-finder", 0.20},
		{models.AgentOpportunityScoring, "opportunity-scoring", 0.20},
		{models.AgentCompetitiveAnalysis, "competitive-analysis", 0.15},
		{models.AgentOutreachStrategy, "outreach-strategy", 0.20},
	}

	defs := make([]AgentDef, 0, len(agents))
	for _, a := range agents {
		prompt, err := s.promptLoader.LoadAgent(a.file, locale, vars)
		if err != nil {
			return nil, fmt.Errorf("load prompt for %s: %w", a.name, err)
		}
		defs = append(defs, AgentDef{
			Name:   a.name,
			Weight: a.weight,
			Prompt: prompt,
		})
	}
	return defs, nil
}

// localeToLanguage maps ISO locale codes to full language names for prompt clarity.
func localeToLanguage(locale string) string {
	switch locale {
	case "it":
		return "Italian"
	case "en":
		return "English"
	case "de":
		return "German"
	case "fr":
		return "French"
	case "es":
		return "Spanish"
	default:
		return "English"
	}
}

func (s *orchestratorService) buildPromptVars(scraped *models.ScrapedCompanyData, enrichment *models.CompanyEnrichmentData, locale, url string) map[string]any {
	vars := map[string]any{
		"URL":    url,
		"Locale": localeToLanguage(locale),
	}
	if scraped != nil {
		vars["CompanyName"] = scraped.CompanyName
		vars["Industry"] = scraped.Industry
		vars["Description"] = scraped.Description
		vars["RawText"] = scraped.RawText
		vars["TechStack"] = scraped.TechStack
		vars["TeamMembers"] = scraped.TeamMembers
		vars["SocialLinks"] = scraped.SocialLinks
		vars["ContactInfo"] = scraped.ContactInfo
		vars["AboutText"] = scraped.AboutText
	}
	if enrichment != nil {
		vars["RegistryData"] = fmt.Sprintf("Name: %s | Tax: %s | VAT: %s | ATECO: %s (%s) | Employees: %s | Revenue: %s | Founded: %s",
			enrichment.CompanyName, enrichment.TaxCode, enrichment.VATNumber,
			enrichment.AtecoCode, enrichment.AtecoDesc, enrichment.EmployeeRange, enrichment.RevenueRange, enrichment.FoundedYear)
	}
	return vars
}

func (s *orchestratorService) updatePhase(job *models.Job, phaseName, status string) {
	now := time.Now()
	for i, p := range job.Phases {
		if p.Name == phaseName {
			job.Phases[i].Status = status
			if status == "running" {
				job.Phases[i].StartedAt = &now
			} else if status == "completed" || status == "failed" {
				job.Phases[i].CompletedAt = &now
			}
			break
		}
	}
	// Persist phase updates so polling clients see real-time progress
	if err := s.jobRepo.UpdatePhases(context.Background(), job.UUID, job.Phases); err != nil {
		s.logger.Error("failed to persist phase update", slog.String("jobId", job.UUID), slog.String("error", err.Error()))
	}
}

func (s *orchestratorService) failJob(ctx context.Context, job *models.Job, phaseName, errMsg string) {
	s.updatePhase(job, phaseName, "failed")
	for i, p := range job.Phases {
		if p.Status == "pending" {
			job.Phases[i].Status = "skipped"
		}
	}

	s.jobRepo.UpdateStatus(ctx, job.UUID, models.JobStatusFailed, errMsg)
	s.logger.Error("prospect pipeline failed",
		slog.String("jobId", job.UUID),
		slog.String("phase", phaseName),
		slog.String("error", errMsg),
	)
}
