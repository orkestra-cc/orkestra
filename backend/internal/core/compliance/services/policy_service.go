package services

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/retentionclass"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The service is the live resolver of the slog PolicyHandler and of the
// span exporter (main.go swaps it in after InitAll).
var (
	_ iface.CompliancePolicyProvider = (*PolicyService)(nil)
	_ utils.LogPolicyResolver        = (*PolicyService)(nil)
)

const (
	// PolicyRefreshInterval is how often every replica reloads the catalog
	// (spec §2.1); the replica that writes also refreshes at once.
	PolicyRefreshInterval = 5 * time.Second
	refreshWarnInterval   = time.Minute
	refreshTimeout        = 3 * time.Second
)

const (
	EffectiveSourceAssigned = "assigned"
	EffectiveSourcePlatform = "platform"
)

var errNoPlatformPolicy = errors.New("compliance: the policy catalog has no platform policy")

// staticDefaultContent is served before the first snapshot. Package-level so
// its address is stable for the handler's derived-logger cache.
var staticDefaultContent = iface.DefaultLogContentPolicy()

type policySnapshotStore interface {
	ListPolicies(ctx context.Context) ([]models.Policy, error)
	ListAssignments(ctx context.Context) ([]models.PolicyAssignment, error)
}

type policyEntry struct {
	policy  models.Policy
	content *iface.LogContentPolicy
}

// policySnapshot is immutable once stored.
type policySnapshot struct {
	platform    *policyEntry
	byTenant    map[string]*policyEntry
	dangling    map[string]bool // assigned to a policy no longer in the catalog
	assignments map[string]models.PolicyAssignment
	strictest   *iface.LogContentPolicy
	loadedAt    time.Time
}

// EffectivePolicy is what applies to one tenant, for the console and the
// Tier-2 summary. Read-only: it shares maps with the snapshot.
type EffectivePolicy struct {
	TenantID   string                    `json:"tenantId"`
	Source     string                    `json:"source"`
	Policy     models.Policy             `json:"policy"`
	Assignment *models.PolicyAssignment  `json:"assignment,omitempty"`
	Retention  []iface.RetentionDecision `json:"retention"`
}

// PolicyService resolves the live compliance policy from an immutable
// snapshot (spec §2.1). Readers never block: they load one pointer.
type PolicyService struct {
	store  policySnapshotStore
	logger *slog.Logger
	now    func() time.Time
	snap   atomic.Pointer[policySnapshot]
	// startedAt anchors the age reported while no snapshot has ever loaded, so
	// the stale alarm can fire even if Mongo is unreachable from boot.
	startedAt time.Time

	mu         sync.Mutex // serialises Refresh; guards the fields below
	contents   map[string]*iface.LogContentPolicy
	lastWarnAt time.Time
	ageHook    func(seconds float64)
}

func NewPolicyService(store policySnapshotStore, logger *slog.Logger) *PolicyService {
	return &PolicyService{store: store, logger: logger, now: time.Now, startedAt: time.Now(), contents: map[string]*iface.LogContentPolicy{}}
}

// SetSnapshotAgeHook receives the snapshot age after every loop tick (the
// metric in production).
func (s *PolicyService) SetSnapshotAgeHook(f func(seconds float64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ageHook = f
}

// Refresh reloads the catalog. On failure the previous snapshot stays in
// force: a refresh never moves to a less restrictive policy by accident.
// The *LogContentPolicy of a policy keeps its address while its version does
// not change, and so does the strictest policy while its value does not.
func (s *PolicyService) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	policies, err := s.store.ListPolicies(ctx)
	if err != nil {
		return s.refreshFailed(err)
	}
	assignments, err := s.store.ListAssignments(ctx)
	if err != nil {
		return s.refreshFailed(err)
	}
	next := &policySnapshot{
		byTenant:    map[string]*policyEntry{},
		dangling:    map[string]bool{},
		assignments: map[string]models.PolicyAssignment{},
		loadedAt:    s.now(),
	}
	byUUID := make(map[string]*policyEntry, len(policies))
	contents := make(map[string]*iface.LogContentPolicy, len(policies))
	for _, p := range policies {
		key := p.UUID + "@" + strconv.Itoa(p.Version)
		ptr, ok := s.contents[key]
		if !ok {
			c := p.LogContent
			c.PIIKeys = slices.Clone(c.PIIKeys)
			ptr = &c
		}
		contents[key] = ptr
		e := &policyEntry{policy: p, content: ptr}
		byUUID[p.UUID] = e
		if p.IsPlatformDefault {
			next.platform = e
		}
	}
	if next.platform == nil {
		return s.refreshFailed(errNoPlatformPolicy)
	}
	strictest := *next.platform.content
	for _, a := range assignments {
		next.assignments[a.TenantID] = a
		e, ok := byUUID[a.PolicyUUID]
		if !ok {
			next.dangling[a.TenantID] = true
			continue
		}
		next.byTenant[a.TenantID] = e
		strictest = iface.MostRestrictive(strictest, *e.content)
	}
	if prev := s.snap.Load(); prev != nil && reflect.DeepEqual(*prev.strictest, strictest) {
		next.strictest = prev.strictest
	} else {
		next.strictest = &strictest
	}
	s.contents = contents
	s.snap.Store(next)
	return nil
}

// refreshFailed logs at most one warning a minute (spec §10.3). Called with
// s.mu held.
func (s *PolicyService) refreshFailed(err error) error {
	if now := s.now(); now.Sub(s.lastWarnAt) >= refreshWarnInterval {
		s.lastWarnAt = now
		s.logger.Warn("compliance: policy refresh failed, keeping the last snapshot",
			slog.String("error", err.Error()))
	}
	return err
}

// Loop refreshes every PolicyRefreshInterval until ctx or stop ends.
func (s *PolicyService) Loop(ctx context.Context, stop <-chan struct{}) {
	t := time.NewTicker(PolicyRefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			rctx, cancel := context.WithTimeout(ctx, refreshTimeout)
			_ = s.Refresh(rctx) // logged, rate-limited, inside
			cancel()
			s.reportAge()
		}
	}
}

// reportAge feeds the hook. Before the first snapshot has ever loaded it
// reports the time since the service was created, so the gauge keeps growing
// and the policy_snapshot_stale alarm (spec §9, §10.3) fires when Mongo is
// down from boot or the catalog has no platform policy.
func (s *PolicyService) reportAge() {
	s.mu.Lock()
	hook := s.ageHook
	s.mu.Unlock()
	if hook == nil {
		return
	}
	if age, ok := s.SnapshotAge(); ok {
		hook(age.Seconds())
		return
	}
	hook(s.now().Sub(s.startedAt).Seconds())
}

// SnapshotAge is the time since the snapshot in force was loaded; false while
// no snapshot has ever loaded (reportAge substitutes the time since start).
func (s *PolicyService) SnapshotAge() (time.Duration, bool) {
	snap := s.snap.Load()
	if snap == nil {
		return 0, false
	}
	return s.now().Sub(snap.loadedAt), true
}

// LogContentFor implements iface.CompliancePolicyProvider: the assigned
// policy, the platform policy for an unassigned tenant, the strictest policy
// in force for "" (spec D6) and for a tenant whose policy disappeared.
func (s *PolicyService) LogContentFor(tenantID string) *iface.LogContentPolicy {
	snap := s.snap.Load()
	if snap == nil {
		return &staticDefaultContent
	}
	if tenantID == "" || snap.dangling[tenantID] {
		return snap.strictest
	}
	if e, ok := snap.byTenant[tenantID]; ok {
		return e.content
	}
	return snap.platform.content
}

// RetentionFor implements iface.CompliancePolicyProvider. An unknown class
// is treated as privileged_change, never as zero days.
func (s *PolicyService) RetentionFor(tenantID string, class iface.RetentionClass) iface.RetentionDecision {
	if !class.Valid() {
		class = iface.RetentionPrivilegedChange
	}
	snap := s.snap.Load()
	if snap == nil {
		def, _ := retentionclass.Get(class)
		return iface.RetentionDecision{Class: class, Days: def.DefaultDays}
	}
	return retentionFrom(snap, tenantID, class)
}

func retentionFrom(snap *policySnapshot, tenantID string, class iface.RetentionClass) iface.RetentionDecision {
	def, _ := retentionclass.Get(class)
	if e, ok := snap.byTenant[tenantID]; ok && def.TenantSettable {
		if days, ok := e.policy.Retention[class]; ok {
			return iface.RetentionDecision{Class: class, PolicyUUID: e.policy.UUID, PolicyVersion: e.policy.Version, Days: days}
		}
	}
	p := snap.platform.policy
	days, ok := p.Retention[class]
	if !ok {
		days = def.DefaultDays
	}
	return iface.RetentionDecision{Class: class, PolicyUUID: p.UUID, PolicyVersion: p.Version, Days: days}
}

// Effective describes what applies to tenantID; false before the first
// snapshot.
func (s *PolicyService) Effective(tenantID string) (EffectivePolicy, bool) {
	snap := s.snap.Load()
	if snap == nil {
		return EffectivePolicy{}, false
	}
	out := EffectivePolicy{TenantID: tenantID, Source: EffectiveSourcePlatform, Policy: snap.platform.policy}
	if e, ok := snap.byTenant[tenantID]; ok {
		out.Source, out.Policy = EffectiveSourceAssigned, e.policy
	}
	if a, ok := snap.assignments[tenantID]; ok {
		out.Assignment = &a
	}
	for _, class := range iface.AllRetentionClasses() {
		out.Retention = append(out.Retention, retentionFrom(snap, tenantID, class))
	}
	return out, true
}
