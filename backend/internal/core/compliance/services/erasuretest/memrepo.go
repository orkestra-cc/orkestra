// Package erasuretest is an in-memory erasure-request repository with the
// lease semantics of repository.ErasureRequestRepository. Never used in
// production.
package erasuretest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
)

// MemRepo keeps erasure requests in memory.
type MemRepo struct {
	mu         sync.Mutex
	rows       map[string]models.ErasureRequest
	failSettle error
}

// NewMemRepo returns an empty repository.
func NewMemRepo() *MemRepo { return &MemRepo{rows: map[string]models.ErasureRequest{}} }

// Put stores req as is, replacing any row with the same UUID (test setup).
func (m *MemRepo) Put(req models.ErasureRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[req.UUID] = clone(req)
}

// Row returns the stored row, or the zero value when absent.
func (m *MemRepo) Row(uuid string) models.ErasureRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.rows[uuid])
}

// FailNextSettle makes the next Settle return err without writing.
func (m *MemRepo) FailNextSettle(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failSettle = err
}

func clone(r models.ErasureRequest) models.ErasureRequest {
	r.Progress = append([]models.ErasureProducerProgress(nil), r.Progress...)
	r.Abandoned = append([]string(nil), r.Abandoned...)
	r.ResolvedAt = cloneTime(r.ResolvedAt)
	r.NextAttemptAt = cloneTime(r.NextAttemptAt)
	r.LeaseUntil = cloneTime(r.LeaseUntil)
	return r
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func leaseFree(r models.ErasureRequest, now time.Time) bool {
	return r.LeaseUntil == nil || r.LeaseUntil.Before(now)
}

// Insert appends a new request.
func (m *MemRepo) Insert(_ context.Context, req *models.ErasureRequest) error {
	m.Put(*req)
	return nil
}

// Get returns one request by UUID.
func (m *MemRepo) Get(_ context.Context, uuid string) (*models.ErasureRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[uuid]
	if !ok {
		return nil, repository.ErrErasureRequestNotFound
	}
	c := clone(r)
	return &c, nil
}

// ListByStatus lists requests in any of statuses (all when none), newest first.
func (m *MemRepo) ListByStatus(_ context.Context, statuses ...string) ([]models.ErasureRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.ErasureRequest
	for _, r := range m.rows {
		if len(statuses) == 0 || contains(statuses, r.Status) {
			out = append(out, clone(r))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.After(out[j].RequestedAt) })
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Resolve transitions a pending request to completed/rejected.
func (m *MemRepo) Resolve(_ context.Context, uuid, status, resolvedBy, mode, note string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[uuid]
	if !ok || r.Status != models.ErasureRequestPending {
		return repository.ErrErasureRequestNotFound
	}
	now := time.Now().UTC()
	r.Status, r.ResolvedAt, r.ResolvedBy, r.Mode, r.ResolutionNote = status, &now, resolvedBy, mode, note
	m.rows[uuid] = r
	return nil
}

// Claim takes the lease on one request; see repository.ErasureRequestRepository.Claim.
func (m *MemRepo) Claim(_ context.Context, uuid, fromStatus, mode, actor string, now, leaseUntil time.Time) (*models.ErasureRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[uuid]
	if !ok || r.Status != fromStatus {
		return nil, repository.ErrErasureRequestNotFound
	}
	if !leaseFree(r, now) {
		return nil, repository.ErrErasureRequestBusy
	}
	lease := leaseUntil.Truncate(time.Millisecond)
	r.LeaseUntil, r.Attempts, r.LastError = &lease, 0, ""
	if fromStatus == models.ErasureRequestPending {
		r.Status, r.Mode, r.ResolvedBy = models.ErasureRequestIncomplete, mode, actor
	}
	m.rows[uuid] = r
	c := clone(r)
	return &c, nil
}

// ClaimDue leases the next incomplete request whose retry is due; nil, nil when none.
func (m *MemRepo) ClaimDue(_ context.Context, now, leaseUntil time.Time) (*models.ErasureRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *models.ErasureRequest
	for _, r := range m.rows {
		if r.Status != models.ErasureRequestIncomplete || r.NextAttemptAt == nil || r.NextAttemptAt.After(now) || !leaseFree(r, now) {
			continue
		}
		if best == nil || r.NextAttemptAt.Before(*best.NextAttemptAt) {
			c := r
			best = &c
		}
	}
	if best == nil {
		return nil, nil
	}
	lease := leaseUntil.Truncate(time.Millisecond)
	best.LeaseUntil = &lease
	m.rows[best.UUID] = *best
	c := clone(*best)
	return &c, nil
}

// Settle records a leased run's outcome, fenced by the lease.
func (m *MemRepo) Settle(_ context.Context, uuid string, leaseUntil time.Time, s repository.ErasureSettlement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failSettle; err != nil {
		m.failSettle = nil
		return err
	}
	r, ok := m.rows[uuid]
	if !ok || r.LeaseUntil == nil || !r.LeaseUntil.Equal(leaseUntil.Truncate(time.Millisecond)) {
		return repository.ErrErasureLeaseLost
	}
	r.Status, r.Progress, r.Attempts = s.Status, append([]models.ErasureProducerProgress(nil), s.Progress...), s.Attempts
	r.NextAttemptAt, r.LastError, r.LeaseUntil = cloneTime(s.NextAttemptAt), s.LastError, nil
	if s.ResolvedAt != nil {
		r.ResolvedAt = cloneTime(s.ResolvedAt)
	}
	m.rows[uuid] = r
	return nil
}

// HasIncomplete reports whether the data subject has an incomplete erasure.
func (m *MemRepo) HasIncomplete(_ context.Context, userUUID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.UserUUID == userUUID && r.Status == models.ErasureRequestIncomplete {
			return true, nil
		}
	}
	return false, nil
}
