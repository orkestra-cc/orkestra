// Package services hosts the concrete AuditSink that satisfies
// iface.AuditSink and wraps the audit-event repository.
package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/compliance/models"
	"github.com/orkestra/backend/internal/addons/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// AuditSink persists audit events through the repository. Emit is
// intentionally fire-and-forget: downstream failures are logged but never
// surfaced to the caller because auditing must not break the hot path.
type AuditSink struct {
	repo   *repository.AuditEventRepository
	logger *slog.Logger
}

// NewSink constructs a sink bound to repo. Logger is used for the internal
// error reports Emit emits when the insert fails.
func NewSink(repo *repository.AuditEventRepository, logger *slog.Logger) *AuditSink {
	return &AuditSink{repo: repo, logger: logger}
}

// Emit records a single audit event. The sink stamps UUID and Timestamp
// (UTC) when the caller left them zero, so consumers can populate only the
// semantic fields and trust the sink for identity and clock. Every emit is
// wrapped in a 2-second timeout so an unhealthy Mongo can't stall the
// calling request.
func (s *AuditSink) Emit(ctx context.Context, in iface.AuditEvent) {
	event := &models.AuditEvent{
		UUID:         uuid.NewString(),
		TenantID:     in.TenantID,
		TenantKind:   in.TenantKind,
		ActorUserID:  in.ActorUserID,
		ActorEmail:   in.ActorEmail,
		ActorType:    defaultActorType(in.ActorType, in.ActorUserID),
		Action:       in.Action,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Outcome:      defaultOutcome(in.Outcome),
		IPAddress:    in.IPAddress,
		UserAgent:    in.UserAgent,
		Metadata:     in.Metadata,
		Timestamp:    time.Now().UTC(),
	}

	writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Deliberately use a detached context so a cancelled request context
	// does not abort the insert — audit writes must survive caller cancel.
	_ = ctx
	if err := s.repo.Insert(writeCtx, event); err != nil {
		s.logger.Warn("audit sink insert failed",
			slog.String("action", event.Action),
			slog.String("tenantId", event.TenantID),
			slog.String("error", err.Error()),
		)
	}
}

// Repo exposes the underlying repository for the admin handler. Kept on the
// sink so consumers of the module only need one object.
func (s *AuditSink) Repo() *repository.AuditEventRepository { return s.repo }

func defaultActorType(given, userID string) string {
	if given != "" {
		return given
	}
	if userID != "" {
		return models.ActorTypeUser
	}
	return models.ActorTypeSystem
}

func defaultOutcome(given string) string {
	if given == "" {
		return models.OutcomeSuccess
	}
	return given
}
