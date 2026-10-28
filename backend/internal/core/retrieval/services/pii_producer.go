package services

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErrErasureEmptySubject refuses a data-subject request with no subject: an
// empty uuid would otherwise build the selector "user:" and widen the delete.
var ErrErasureEmptySubject = errors.New("retrieval: empty data subject")

const (
	piiSubject        = "retrieval"
	collSearchLogName = repository.CollSearchLog
	collIndexName     = "retrieval_index"
	piiAttemptTimeout = 20 * time.Second
)

// ExportedSearch is one search the user ran. Result sources, subjects and
// principals are other people's data and are never exported.
type ExportedSearch struct {
	TenantID    string    `json:"tenantId"`
	Corpus      string    `json:"corpus"`
	Query       string    `json:"query"`
	Filter      any       `json:"filter,omitempty"`
	ResultCount int       `json:"resultCount"`
	At          time.Time `json:"at"`
}

// ExportedAppearance is a search by someone else that returned the user as a
// subject: where and when only (GDPR art. 15(4)).
type ExportedAppearance struct {
	TenantID string    `json:"tenantId"`
	Corpus   string    `json:"corpus"`
	At       time.Time `json:"at"`
}

// PersonalDataExport is the right-of-access projection of the search log.
type PersonalDataExport struct {
	Searches          []ExportedSearch     `json:"searches"`
	AppearedInResults []ExportedAppearance `json:"appearedInResults"`
}

// PIIProducer is retrieval's iface.PIIProducer (ADR-0023 D12). A purge
// records the global user tombstone, which vetoes the user's points in search
// at once, deletes the user's search-log rows and runs the first finalization
// pass inline; it reports Pending until the finalizer marks the tombstone done.
type PIIProducer struct {
	store  ErasureStore
	logs   SearchLogDSRRepo
	fin    *Finalizer
	now    func() time.Time
	logger *slog.Logger
}

var _ iface.PIIProducer = (*PIIProducer)(nil)

func NewPIIProducer(store ErasureStore, logs SearchLogDSRRepo, fin *Finalizer, now func() time.Time, logger *slog.Logger) *PIIProducer {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PIIProducer{store: store, logs: logs, fin: fin, now: now, logger: logger}
}

// Subject is the DSR audit label of this producer.
func (p *PIIProducer) Subject() string { return piiSubject }

// ExportPersonalData returns the searches the user ran and the searches that
// returned them; (nil, nil) when it holds neither.
func (p *PIIProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	if userUUID == "" {
		return nil, ErrErasureEmptySubject
	}
	rows, err := p.logs.ListByUserAllTenants(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	out := &PersonalDataExport{Searches: []ExportedSearch{}, AppearedInResults: []ExportedAppearance{}}
	subject := userSubjectPrefix + userUUID
	for _, row := range rows {
		if row.UserUUID == userUUID {
			out.Searches = append(out.Searches, ExportedSearch{
				TenantID: row.TenantID, Corpus: row.Corpus, Query: row.Query,
				Filter: p.decodeFilter(ctx, row), ResultCount: row.HitCount, At: row.CreatedAt,
			})
		}
		for _, s := range row.Subjects {
			if s == subject {
				out.AppearedInResults = append(out.AppearedInResults,
					ExportedAppearance{TenantID: row.TenantID, Corpus: row.Corpus, At: row.CreatedAt})
				break
			}
		}
	}
	if len(out.Searches) == 0 && len(out.AppearedInResults) == 0 {
		return nil, nil
	}
	return out, nil
}

// decodeFilter reads the stored JSON text keeping numbers exact; a row it
// cannot decode exports without a filter, and the log names no text.
func (p *PIIProducer) decodeFilter(ctx context.Context, row models.SearchLog) any {
	if row.FilterJSON == "" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(row.FilterJSON))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		p.logger.WarnContext(ctx, "retrieval: stored search filter could not be decoded; exported without it",
			slog.String("tenant", row.TenantID), slog.String("corpus", row.Corpus))
		return nil
	}
	return v
}

// PurgePersonalData erases the user under both modes (vectors are personal
// data, ADR-0023 D12). The tombstone is recorded first, so the read veto
// holds even when the engine is down; Pending stays true until the finalizer
// has confirmed the second pass.
func (p *PIIProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	if userUUID == "" {
		return iface.PurgeResult{}, ErrErasureEmptySubject
	}
	e, _, err := p.store.EnsureUser(ctx, userUUID, p.now())
	if err != nil {
		return iface.PurgeResult{}, err
	}
	n, err := p.logs.DeleteByUserAllTenants(ctx, userUUID)
	if err != nil {
		return iface.PurgeResult{}, err
	}
	if e.EffectiveStatus() != models.ErasureStatusDone {
		actx, cancel := context.WithTimeout(ctx, piiAttemptTimeout)
		if _, err := p.fin.Attempt(actx, e.UUID); err != nil {
			p.logger.WarnContext(ctx, "retrieval: inline erasure pass failed; the finalizer retries",
				slog.String("erasure", e.UUID), slog.String("error", err.Error()))
		}
		cancel()
	}
	rows, err := p.store.ListUser(ctx, userUUID)
	if err != nil {
		return iface.PurgeResult{}, err
	}
	pending := false
	for i := range rows {
		if rows[i].EffectiveStatus() != models.ErasureStatusDone {
			pending = true
		}
	}
	return iface.PurgeResult{
		RowsDeleted: int(n),
		Collections: []string{collSearchLogName, collIndexName},
		Pending:     pending,
	}, nil
}
