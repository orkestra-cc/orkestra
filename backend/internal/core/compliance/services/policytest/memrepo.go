// Package policytest holds in-memory doubles for the compliance policy
// services and handlers tests: a repository with the same semantics as
// repository.PolicyRepository (without rollback: the Mongo integration tests
// cover atomicity), a tenant lookup and a recording audit sink. Never used
// in production.
package policytest

import (
	"context"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
)

type MemRepo struct {
	mu          sync.Mutex
	policies    map[string]models.Policy
	versions    []models.PolicyVersion
	assignments map[string]models.PolicyAssignment
	history     []models.PolicyAssignmentHistory
	requests    map[string]models.PolicyChangeRequest
	listErr     error
}

func NewMemRepo() *MemRepo {
	return &MemRepo{
		policies:    map[string]models.Policy{},
		assignments: map[string]models.PolicyAssignment{},
		requests:    map[string]models.PolicyChangeRequest{},
	}
}

// SetListErr makes ListPolicies and ListAssignments fail (a Mongo outage
// seen by the PolicyService refresh); nil restores them.
func (m *MemRepo) SetListErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listErr = err
}

func clonePolicy(p models.Policy) models.Policy {
	p.LogContent.PIIKeys = slices.Clone(p.LogContent.PIIKeys)
	p.Retention = maps.Clone(p.Retention)
	p.Sinks = p.Sinks.Clone()
	return p
}

func (m *MemRepo) WithTxn(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (m *MemRepo) InsertPolicy(_ context.Context, p *models.Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, q := range m.policies {
		if q.Name == p.Name {
			return repository.ErrPolicyNameTaken
		}
		if q.IsPlatformDefault && p.IsPlatformDefault {
			return repository.ErrPlatformPolicyExists
		}
	}
	m.policies[p.UUID] = clonePolicy(*p)
	return nil
}

func (m *MemRepo) GetPolicy(_ context.Context, uuid string) (*models.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.policies[uuid]
	if !ok {
		return nil, repository.ErrPolicyNotFound
	}
	c := clonePolicy(p)
	return &c, nil
}

func (m *MemRepo) GetPlatformPolicy(_ context.Context) (*models.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.policies {
		if p.IsPlatformDefault {
			c := clonePolicy(p)
			return &c, nil
		}
	}
	return nil, repository.ErrPolicyNotFound
}

func (m *MemRepo) ListPolicies(_ context.Context) ([]models.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]models.Policy, 0, len(m.policies))
	for _, p := range m.policies {
		out = append(out, clonePolicy(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemRepo) NameTaken(_ context.Context, name, exceptUUID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.policies {
		if p.Name == name && p.UUID != exceptUUID {
			return true, nil
		}
	}
	return false, nil
}

func (m *MemRepo) ReplacePolicy(_ context.Context, p *models.Policy, expectedVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[p.UUID]
	if !ok {
		return repository.ErrPolicyNotFound
	}
	if cur.Version != expectedVersion || cur.IsPlatformDefault != p.IsPlatformDefault {
		return repository.ErrPolicyVersionConflict
	}
	for _, q := range m.policies {
		if q.UUID != p.UUID && q.Name == p.Name {
			return repository.ErrPolicyNameTaken
		}
	}
	m.policies[p.UUID] = clonePolicy(*p)
	return nil
}

func (m *MemRepo) DeletePolicy(_ context.Context, uuid string, expectedVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[uuid]
	if !ok {
		return repository.ErrPolicyNotFound
	}
	if cur.Version != expectedVersion || cur.IsPlatformDefault {
		return repository.ErrPolicyVersionConflict
	}
	delete(m.policies, uuid)
	return nil
}

func (m *MemRepo) InsertVersion(_ context.Context, v *models.PolicyVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.versions {
		if x.PolicyUUID == v.PolicyUUID && x.Version == v.Version {
			return repository.ErrPolicyVersionConflict
		}
	}
	c := *v
	c.Snapshot = clonePolicy(v.Snapshot)
	m.versions = append(m.versions, c)
	return nil
}

func (m *MemRepo) ListVersions(_ context.Context, policyUUID string) ([]models.PolicyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.PolicyVersion{}
	for _, v := range m.versions {
		if v.PolicyUUID == policyUUID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (m *MemRepo) GetAssignment(_ context.Context, tenantID string) (*models.PolicyAssignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.assignments[tenantID]
	if !ok {
		return nil, repository.ErrAssignmentNotFound
	}
	return &a, nil
}

func (m *MemRepo) ListAssignments(_ context.Context) ([]models.PolicyAssignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]models.PolicyAssignment, 0, len(m.assignments))
	for _, a := range m.assignments {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TenantID < out[j].TenantID })
	return out, nil
}

func (m *MemRepo) CountAssignments(_ context.Context, policyUUID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, a := range m.assignments {
		if a.PolicyUUID == policyUUID {
			n++
		}
	}
	return n, nil
}

func (m *MemRepo) UpsertAssignment(_ context.Context, a *models.PolicyAssignment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assignments[a.TenantID] = *a
	return nil
}

func (m *MemRepo) DeleteAssignment(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.assignments[tenantID]; !ok {
		return repository.ErrAssignmentNotFound
	}
	delete(m.assignments, tenantID)
	return nil
}

func (m *MemRepo) InsertAssignmentHistory(_ context.Context, h *models.PolicyAssignmentHistory) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, *h)
	return nil
}

func (m *MemRepo) InsertChangeRequest(_ context.Context, cr *models.PolicyChangeRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[cr.UUID] = *cr
	return nil
}

func (m *MemRepo) GetChangeRequest(_ context.Context, uuid string) (*models.PolicyChangeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cr, ok := m.requests[uuid]
	if !ok {
		return nil, repository.ErrChangeRequestNotFound
	}
	return &cr, nil
}

func (m *MemRepo) ListChangeRequests(_ context.Context, status string) ([]models.PolicyChangeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.PolicyChangeRequest{}
	for _, cr := range m.requests {
		if status == "" || cr.Status == status {
			out = append(out, cr)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.After(out[j].RequestedAt) })
	return out, nil
}

func (m *MemRepo) DecideChangeRequest(_ context.Context, uuid, status, decidedBy, note string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cr, ok := m.requests[uuid]
	if !ok {
		return repository.ErrChangeRequestNotFound
	}
	if cr.Status != models.ChangeStatusPending {
		return repository.ErrChangeRequestNotPending
	}
	cr.Status, cr.DecidedBy, cr.DecisionNote = status, decidedBy, note
	cr.DecidedAt = &at
	m.requests[uuid] = cr
	return nil
}

func (m *MemRepo) ListPendingBefore(_ context.Context, before time.Time) ([]models.PolicyChangeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.PolicyChangeRequest{}
	for _, cr := range m.requests {
		if cr.Status == models.ChangeStatusPending && cr.RequestedAt.Before(before) {
			out = append(out, cr)
		}
	}
	return out, nil
}

// Versions, History and Requests expose the evidence rows to tests.
func (m *MemRepo) Versions() []models.PolicyVersion {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.versions)
}

func (m *MemRepo) History() []models.PolicyAssignmentHistory {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.history)
}

func (m *MemRepo) Requests() []models.PolicyChangeRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.PolicyChangeRequest, 0, len(m.requests))
	for _, cr := range m.requests {
		out = append(out, cr)
	}
	return out
}
