package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	erasureAttemptTimeout = 20 * time.Second
	purgeTimeout          = 30 * time.Second
)

// purgeRetryDelays space the tries of the tenant tombstone write.
var purgeRetryDelays = []time.Duration{100 * time.Millisecond, 400 * time.Millisecond}

// ErasureService records erasures (tombstones) and starts their finalization.
// Every eraser writes its tombstone durably BEFORE any row or point is
// deleted: the search pipeline retracts its own log row by re-reading
// tombstones after writing it, so that order leaves no window in which an
// erased subject can reappear. Search-log deletion and engine deletion belong
// to the Finalizer (its inline Attempt runs stage 0); this service makes no
// log call of its own.
type ErasureService struct {
	store   ErasureStore
	fin     *Finalizer
	isOwner func(owner string) bool
	purged  iface.PurgedTenantIDLister // nil: no reconciliation
	audit   *AuditRelay
	now     func() time.Time
	logger  *slog.Logger
}

// NewErasureService builds the service. purged may be nil.
func NewErasureService(store ErasureStore, fin *Finalizer, isOwner func(owner string) bool,
	purged iface.PurgedTenantIDLister, audit *AuditRelay, now func() time.Time, logger *slog.Logger) *ErasureService {
	if audit == nil {
		audit = NewAuditRelay()
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ErasureService{store: store, fin: fin, isOwner: isOwner, purged: purged, audit: audit, now: now, logger: logger}
}

// ValidateOwnerSubject splits "<owner>:<id>" and returns the owner. The owner
// has the corpus owner shape, the whole subject is at most MaxRefBytes bytes,
// the id has no whitespace or control character, and owner "user" is refused
// (platform users are erased only through compliance, ADR-0023 D12).
func ValidateOwnerSubject(subject string) (string, error) {
	invalid := func(why string) (string, error) {
		return "", fmt.Errorf("%w: subject %s", iface.ErrRetrievalInvalidRequest, why)
	}
	if len(subject) > MaxRefBytes {
		return invalid(fmt.Sprintf("is longer than %d bytes", MaxRefBytes))
	}
	owner, id, ok := strings.Cut(subject, ":")
	if !ok || id == "" || !ownerRe.MatchString(owner) {
		return invalid(`must be "<owner>:<id>"`)
	}
	if owner == "user" {
		return invalid("owner user is erased through compliance only")
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalid("id has whitespace or control characters")
		}
	}
	return owner, nil
}

// DeleteBySubject erases a non-user subject in one tenant: it records the
// tombstone, then runs the first finalization pass inline. Only the tombstone
// write can fail the call; a failed pass is logged and the sweep retries it.
func (e *ErasureService) DeleteBySubject(ctx context.Context, tenantID, subject string) error {
	owner, err := ValidateOwnerSubject(subject)
	if err != nil {
		return err
	}
	if tenantID == "" {
		return fmt.Errorf("%w: tenant id is required", iface.ErrRetrievalInvalidRequest)
	}
	if e.isOwner == nil || !e.isOwner(owner) {
		return fmt.Errorf("%w: subject owner is not a registered corpus owner", iface.ErrRetrievalInvalidRequest)
	}
	row, created, err := e.store.EnsureSubject(ctx, tenantID, subject, e.now())
	if err != nil {
		return err
	}
	if created {
		e.audit.EmitSystem(ctx, tenantID, AuditErasureRecorded, "retrieval_erasure", row.UUID, map[string]any{"kind": "subject"})
	}
	e.attempt(ctx, row.UUID)
	return nil
}

// PurgeTenant is the tenant purge hook: it records the tenant tombstone
// (three tries on a context detached from the request), then runs the first
// finalization pass inline. The returned error is the tombstone write's; the
// reconcile loop recovers a tenant whose hook failed.
func (e *ErasureService) PurgeTenant(ctx context.Context, tenantUUID string) error {
	if tenantUUID == "" {
		return fmt.Errorf("%w: tenant id is required", iface.ErrRetrievalInvalidRequest)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), purgeTimeout)
	defer cancel()

	row, created, err := e.ensureTenantWithRetry(ctx, tenantUUID)
	if err != nil {
		return err
	}
	if created {
		e.audit.EmitSystem(ctx, tenantUUID, AuditErasureRecorded, "retrieval_erasure", row.UUID, map[string]any{"kind": "tenant"})
	}
	e.attempt(ctx, row.UUID)
	return nil
}

func (e *ErasureService) ensureTenantWithRetry(ctx context.Context, tenantID string) (*models.Erasure, bool, error) {
	var lastErr error
	for i := 0; i <= len(purgeRetryDelays); i++ {
		row, created, err := e.store.EnsureTenant(ctx, tenantID, e.now())
		if err == nil {
			return row, created, nil
		}
		lastErr = err
		if i == len(purgeRetryDelays) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, false, lastErr
		case <-time.After(purgeRetryDelays[i]):
		}
	}
	return nil, false, lastErr
}

// ReconcilePurgedTenants writes the tombstone of every purged tenant that has
// none (a hook that never ran or failed, or a tenant purged before PR 6). It
// makes no inline pass: the rows are due at once and the sweep takes them. It
// returns the number of tombstones created; the first store error stops it.
func (e *ErasureService) ReconcilePurgedTenants(ctx context.Context) (int, error) {
	if e.purged == nil {
		return 0, nil
	}
	ids, err := e.purged.ListPurgedTenantIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	have, err := e.store.TenantTombstoneIDs(ctx, ids)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if have[id] {
			continue
		}
		row, created, err := e.store.EnsureTenant(ctx, id, e.now())
		if err != nil {
			return n, err
		}
		if created {
			n++
			e.audit.EmitSystem(ctx, id, AuditErasureRecorded, "retrieval_erasure", row.UUID,
				map[string]any{"kind": "tenant", "source": "reconcile"})
		}
	}
	return n, nil
}

// attempt runs the inline first pass; a failure is logged (the row is durable
// and the sweep retries it) and never returned. The log names no subject.
func (e *ErasureService) attempt(ctx context.Context, uuid string) {
	if e.fin == nil {
		return
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), erasureAttemptTimeout)
	defer cancel()
	if _, err := e.fin.Attempt(actx, uuid); err != nil {
		e.logger.WarnContext(ctx, "retrieval: inline erasure pass failed; the sweep will retry",
			slog.String("erasure", uuid), slog.String("reason", failureReason(err)))
	}
}
