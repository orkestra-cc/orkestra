package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"github.com/orkestra-cc/orkestra-addon-sales/repository"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// BatchPoller polls pending batch jobs and completes the prospect pipeline when results arrive
type BatchPoller struct {
	batchRepo     repository.BatchRepository
	jobRepo       repository.JobRepository
	reportRepo    repository.ReportRepository
	modelProvider AIModelProvider
	scorer        *Scorer
	reportGen     *ReportGenerator
	interval      time.Duration
	logger        *slog.Logger
}

// NewBatchPoller creates a new BatchPoller
func NewBatchPoller(
	batchRepo repository.BatchRepository,
	jobRepo repository.JobRepository,
	reportRepo repository.ReportRepository,
	modelProvider AIModelProvider,
	scorer *Scorer,
	reportGen *ReportGenerator,
	interval time.Duration,
	logger *slog.Logger,
) *BatchPoller {
	return &BatchPoller{
		batchRepo:     batchRepo,
		jobRepo:       jobRepo,
		reportRepo:    reportRepo,
		modelProvider: modelProvider,
		scorer:        scorer,
		reportGen:     reportGen,
		interval:      interval,
		logger:        logger.With(slog.String("service", "batch-poller")),
	}
}

// Start begins the background polling loop
func (p *BatchPoller) Start() {
	p.logger.Info("batch poller started", slog.Duration("interval", p.interval))
	go p.run()
}

func (p *BatchPoller) run() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("batch poller panic recovered", slog.Any("panic", r))
				}
			}()
			p.pollPending()
		}()
	}
}

func (p *BatchPoller) pollPending() {
	ctx := context.Background()
	pending, err := p.batchRepo.ListPending(ctx)
	if err != nil {
		p.logger.Error("failed to list pending batches", slog.String("error", err.Error()))
		return
	}

	if len(pending) > 0 {
		p.logger.Info("polling pending batches", slog.Int("count", len(pending)))
	}

	for _, batch := range pending {
		p.checkBatch(ctx, &batch)
	}
}

func (p *BatchPoller) checkBatch(ctx context.Context, batch *models.BatchJob) {
	// Reconstruct the LLM provider from stored model UUID
	llm, err := p.modelProvider.GetLLMProvider(ctx, batch.ModelUUID)
	if err != nil {
		p.logger.Error("failed to get LLM provider for batch",
			slog.String("batchUuid", batch.UUID),
			slog.String("modelUuid", batch.ModelUUID),
			slog.String("error", err.Error()),
		)
		p.failBatch(ctx, batch, "LLM provider unavailable: "+err.Error())
		return
	}

	batchProvider, ok := llm.(iface.BatchLLMProvider)
	if !ok {
		p.failBatch(ctx, batch, "LLM provider no longer supports batch")
		return
	}

	status, err := batchProvider.PollBatch(ctx, batch.BatchID)
	if err != nil {
		p.logger.Error("failed to poll batch",
			slog.String("batchUuid", batch.UUID),
			slog.String("batchId", batch.BatchID),
			slog.String("error", err.Error()),
		)
		return // transient error, retry next tick
	}

	switch status.State {
	case "completed":
		p.completeBatch(ctx, batch, status.Results)
	case "failed":
		p.failBatch(ctx, batch, status.Error)
	case "processing":
		p.logger.Info("batch still processing",
			slog.String("batchUuid", batch.UUID),
			slog.String("batchId", batch.BatchID),
			slog.String("provider", batch.Provider),
		)
		if batch.Status == "submitted" {
			p.batchRepo.UpdateStatus(ctx, batch.UUID, "processing", "")
		}
	}
}

func (p *BatchPoller) completeBatch(ctx context.Context, batch *models.BatchJob, results []iface.BatchResult) {
	// Save raw results to batch record
	entries := make([]models.BatchResultEntry, len(results))
	for i, r := range results {
		entries[i] = models.BatchResultEntry{
			CustomID:     r.CustomID,
			Text:         r.Text,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			Error:        r.Error,
		}
	}
	if err := p.batchRepo.UpdateResults(ctx, batch.UUID, entries); err != nil {
		p.logger.Error("failed to save batch results", slog.String("batchUuid", batch.UUID), slog.String("error", err.Error()))
	}

	// Load the parent job
	job, err := p.jobRepo.GetByUUID(ctx, batch.JobUUID)
	if err != nil {
		p.logger.Error("failed to load job for batch completion",
			slog.String("batchUuid", batch.UUID),
			slog.String("jobUuid", batch.JobUUID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Map batch results → AgentResult array
	agentResults := make([]*models.AgentResult, len(batch.RequestMap))
	for _, r := range results {
		idx, ok := batch.RequestMap[r.CustomID]
		if !ok {
			continue
		}

		ar := &models.AgentResult{
			AgentName:    models.AgentName(r.CustomID),
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
		}

		if r.Error != "" {
			ar.Error = r.Error
		} else {
			// Parse JSON findings
			if json.Valid([]byte(r.Text)) {
				ar.Findings = json.RawMessage(r.Text)
			} else {
				wrapped, _ := json.Marshal(map[string]string{"response": r.Text})
				ar.Findings = json.RawMessage(wrapped)
			}
			// Extract score from findings
			var parsed struct {
				Score int `json:"score"`
			}
			json.Unmarshal(ar.Findings, &parsed)
			ar.Score = parsed.Score
		}

		if idx < len(agentResults) {
			agentResults[idx] = ar
		}
	}

	job.AgentResults = agentResults

	// Synthesis: score + grade + report
	scoreResult := p.scorer.Calculate(agentResults)
	job.TotalScore = scoreResult.Total
	job.Grade = scoreResult.Grade

	if p.reportGen != nil {
		p.reportRepo.DeleteByJobUUID(ctx, job.UUID)
		report, genErr := p.reportGen.GenerateFromJob(job)
		if genErr != nil {
			p.logger.Error("failed to generate report", slog.String("jobId", job.UUID), slog.String("error", genErr.Error()))
		} else {
			job.ReportUUID = report.UUID
		}
	}

	// Complete job
	now := time.Now()
	job.Status = models.JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now

	// Update phases
	for i, ph := range job.Phases {
		if ph.Name == "analysis" && ph.Status != "completed" {
			job.Phases[i].Status = "completed"
			job.Phases[i].CompletedAt = &now
		}
		if ph.Name == "synthesis" {
			job.Phases[i].Status = "completed"
			job.Phases[i].StartedAt = &now
			job.Phases[i].CompletedAt = &now
		}
	}

	if err := p.jobRepo.UpdateFull(ctx, job); err != nil {
		p.logger.Error("failed to save completed job", slog.String("jobId", job.UUID), slog.String("error", err.Error()))
		return
	}

	p.logger.Info("batch completed",
		slog.String("batchUuid", batch.UUID),
		slog.String("jobId", job.UUID),
		slog.Int("score", job.TotalScore),
		slog.String("grade", job.Grade),
	)
}

func (p *BatchPoller) failBatch(ctx context.Context, batch *models.BatchJob, errMsg string) {
	p.batchRepo.UpdateStatus(ctx, batch.UUID, "failed", errMsg)
	p.jobRepo.UpdateStatus(ctx, batch.JobUUID, models.JobStatusFailed, "batch failed: "+errMsg)
	p.logger.Error("batch failed",
		slog.String("batchUuid", batch.UUID),
		slog.String("jobUuid", batch.JobUUID),
		slog.String("error", errMsg),
	)
}
