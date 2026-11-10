package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// producerCountTimeout bounds one producer's CountPersonalData in the preview.
const producerCountTimeout = 5 * time.Second

// Fixed texts the preview returns in place of error detail (the detail is only
// logged).
const (
	countFailedText          = "count failed"
	retentionCheckFailedText = "retention check failed"
)

// ProducerCount is one producer's line in the preview. Countable is false for
// a producer that does not implement iface.PIIProducerCounter (Rows and
// Collections are then empty). Rows is that producer's own count: two
// producers can report the same stored rows (the two auth instances share
// device-trust and security-event rows), so the rows of different producers
// must not be added up into an exact total.
type ProducerCount struct {
	Key         string
	Countable   bool
	Rows        int
	Collections []string
	Error       string
}

// CountAll counts the subject's personal data per registered producer, in
// registry order, without deleting anything. Each counter runs under a 5 s
// timeout and a panic or error becomes Error "count failed" on that line only
// (the detail is logged with the producer key, never returned).
func (s *DSRService) CountAll(ctx context.Context, userUUID string) []ProducerCount {
	producers := s.registry.List()
	keys := s.keysFor(producers)
	out := make([]ProducerCount, len(producers))
	timeout := s.countTimeout
	if timeout <= 0 {
		timeout = producerCountTimeout
	}
	var wg sync.WaitGroup
	for i, p := range producers {
		counter, ok := p.(iface.PIIProducerCounter)
		if !ok {
			out[i] = ProducerCount{Key: keys[i]}
			continue
		}
		wg.Add(1)
		go func(i int, subject, key string) {
			defer wg.Done()
			out[i] = s.countOne(ctx, counter, subject, key, userUUID, timeout)
		}(i, p.Subject(), keys[i])
	}
	wg.Wait()
	return out
}

// countOne runs one counter in its own goroutine so a counter that ignores its
// context still releases the caller at the timeout.
func (s *DSRService) countOne(ctx context.Context, c iface.PIIProducerCounter, subject, key, userUUID string, timeout time.Duration) ProducerCount {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		n   iface.PersonalDataCount
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logProducerPanic("count", subject, key, userUUID, r)
				ch <- result{err: errProducerPanicked}
			}
		}()
		n, err := c.CountPersonalData(ctx, userUUID)
		ch <- result{n: n, err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			s.logger.Warn("DSR: count producer failed", slog.String("key", key), slog.String("error", r.err.Error()))
			return ProducerCount{Key: key, Countable: true, Error: countFailedText}
		}
		return ProducerCount{Key: key, Countable: true, Rows: r.n.Rows, Collections: r.n.Collections}
	case <-ctx.Done():
		s.logger.Warn("DSR: count producer timed out", slog.String("key", key), slog.String("error", ctx.Err().Error()))
		return ProducerCount{Key: key, Countable: true, Error: countFailedText}
	}
}

// EvaluateRetention returns the retention obligation that would block a hard
// delete now (nil when none, or when no gate is wired), or an error when the
// gate could not be evaluated.
func (s *DSRService) EvaluateRetention(ctx context.Context, userUUID string) (*RetentionGateError, error) {
	if s.retentionGate == nil {
		return nil, nil
	}
	return s.retentionGate.Evaluate(ctx, userUUID)
}

// DSRPreview is the dry-run of a subject's erasure. Nothing is deleted.
// Producers are per-producer counts, deliberately not summed: the same stored
// rows can be reported by more than one producer.
type DSRPreview struct {
	UserUUID string
	// LegalHolds are the holds that block erasure now (expired ones excluded).
	LegalHolds []models.LegalHold
	// Retention is the obligation blocking a hard delete; nil when none.
	Retention *RetentionGateError
	// RetentionError is set (fixed text) when the gate could not be evaluated;
	// a hard delete is then reported as not allowed, like the real gate.
	RetentionError string
	// OpenRequests are the subject's pending and incomplete erasure requests.
	OpenRequests []models.ErasureRequest
	Producers    []ProducerCount
	// HardDeleteAllowed: no active hold, no retention obligation and the gate
	// evaluated without error.
	HardDeleteAllowed bool
	// AnonymizeAllowed: no active hold.
	AnonymizeAllowed bool
}

type previewDSR interface {
	CountAll(ctx context.Context, userUUID string) []ProducerCount
	EvaluateRetention(ctx context.Context, userUUID string) (*RetentionGateError, error)
	SubjectExists(ctx context.Context, userUUID string) (bool, error)
}

type previewHolds interface {
	ListActive(ctx context.Context, userUUID string) ([]models.LegalHold, error)
}

type previewRequests interface {
	List(ctx context.Context, f repository.ErasureListFilter) ([]models.ErasureRequest, string, error)
}

// DSRPreviewService answers what an erasure of a subject would meet and touch.
type DSRPreviewService struct {
	dsr      previewDSR
	holds    previewHolds
	requests previewRequests
	sink     iface.AuditSink
	logger   *slog.Logger
}

// NewDSRPreviewService builds the preview service. sink may be nil.
func NewDSRPreviewService(dsr previewDSR, holds previewHolds, requests previewRequests, sink iface.AuditSink, logger *slog.Logger) *DSRPreviewService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DSRPreviewService{dsr: dsr, holds: holds, requests: requests, sink: sink, logger: logger}
}

// Preview gathers the legal holds, the retention gate, the open requests and
// the per-producer counts. An unknown subject is ErrSubjectNotFound and nothing
// is audited. A failure to read the holds or the requests fails
// the preview (their state is unknown); a retention or counter failure is
// reported in the preview itself.
func (s *DSRPreviewService) Preview(ctx context.Context, userUUID string) (*DSRPreview, error) {
	userUUID = NormalizeSubjectUUID(userUUID)
	exists, err := s.dsr.SubjectExists(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("look up subject: %w", err)
	}
	if !exists {
		return nil, ErrSubjectNotFound
	}
	holds, err := s.holds.ListActive(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("list active legal holds: %w", err)
	}
	reqs, _, err := s.requests.List(ctx, repository.ErasureListFilter{
		Statuses: []string{models.ErasureRequestPending, models.ErasureRequestIncomplete},
		UserUUID: userUUID,
	})
	if err != nil {
		return nil, fmt.Errorf("list open erasure requests: %w", err)
	}
	out := &DSRPreview{UserUUID: userUUID, LegalHolds: holds, OpenRequests: reqs}
	if out.LegalHolds == nil {
		out.LegalHolds = []models.LegalHold{}
	}
	if out.OpenRequests == nil {
		out.OpenRequests = []models.ErasureRequest{}
	}
	gerr, rerr := s.dsr.EvaluateRetention(ctx, userUUID)
	if rerr != nil {
		s.logger.Warn("DSR preview: retention check failed", slog.String("userUuid", userUUID), slog.String("error", rerr.Error()))
		out.RetentionError = retentionCheckFailedText
	}
	out.Retention = gerr
	out.Producers = s.dsr.CountAll(ctx, userUUID)
	out.AnonymizeAllowed = len(holds) == 0
	out.HardDeleteAllowed = len(holds) == 0 && gerr == nil && rerr == nil
	s.audit(ctx, out)
	return out, nil
}

// audit records that an operator looked at a subject's erasure preview. The
// metadata is counts only, never personal data.
func (s *DSRPreviewService) audit(ctx context.Context, p *DSRPreview) {
	if s.sink == nil {
		return
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	s.sink.Emit(ctx, iface.AuditEvent{
		ActorUserID:  actor,
		ActorType:    "user",
		Action:       "gdpr.dsr.previewed",
		ResourceType: "user",
		ResourceID:   p.UserUUID,
		Outcome:      "success",
		Metadata: map[string]any{
			"producers":    len(p.Producers),
			"legalHolds":   len(p.LegalHolds),
			"openRequests": len(p.OpenRequests),
		},
	})
}
