package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

type memOutbox struct {
	queue        []*models.OutboxEvent
	sent, failed []string
	rescheduled  []time.Duration
	tenants      []string // tenant seen by each Mark*/Reschedule call
}

func (m *memOutbox) LeaseDue(_ context.Context, now time.Time, _ time.Duration) (*models.OutboxEvent, error) {
	for _, ev := range m.queue {
		if ev.Status == models.OutboxStatusPending && !ev.NextAttemptAt.After(now) {
			until := now.Add(time.Minute)
			ev.Status, ev.LeaseUntil = models.OutboxStatusSending, &until
			cp := *ev
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memOutbox) find(uuid string) *models.OutboxEvent {
	for _, ev := range m.queue {
		if ev.UUID == uuid {
			return ev
		}
	}
	return nil
}

func (m *memOutbox) tenant(ctx context.Context) {
	id, _ := tenantrepo.CurrentTenantID(ctx)
	m.tenants = append(m.tenants, id)
}

func (m *memOutbox) MarkSent(ctx context.Context, uuid string, _ time.Time) error {
	m.tenant(ctx)
	m.find(uuid).Status = models.OutboxStatusSent
	m.sent = append(m.sent, uuid)
	return nil
}

func (m *memOutbox) Reschedule(ctx context.Context, uuid string, _ time.Time, attempts int, next time.Time) error {
	m.tenant(ctx)
	ev := m.find(uuid)
	ev.Status, ev.Attempts = models.OutboxStatusPending, attempts
	m.rescheduled = append(m.rescheduled, time.Until(next))
	ev.NextAttemptAt = time.Now() // let the test drive the next pass immediately
	return nil
}

func (m *memOutbox) MarkFailed(ctx context.Context, uuid string, _ time.Time, _ int) error {
	m.tenant(ctx)
	m.find(uuid).Status = models.OutboxStatusFailed
	m.failed = append(m.failed, uuid)
	return nil
}

type flakyNotifier struct {
	failures  int
	calls     int
	tenants   []string
	kinds     []string
	deadlines []time.Duration
}

func (n *flakyNotifier) SendBudgetEvent(ctx context.Context, ev models.OutboxEvent) error {
	n.calls++
	if dl, ok := ctx.Deadline(); ok {
		n.deadlines = append(n.deadlines, time.Until(dl))
	}
	id, _ := tenantrepo.CurrentTenantID(ctx)
	n.tenants = append(n.tenants, id)
	n.kinds = append(n.kinds, tenantrepo.CurrentTenantKind(ctx))
	if n.calls <= n.failures {
		return errors.New("smtp down")
	}
	return nil
}

func outboxWith(tenant string) *memOutbox {
	return &memOutbox{queue: []*models.OutboxEvent{{UUID: "e1", TenantID: tenant, Period: "2026-10", Threshold: models.ThresholdWarn, Status: models.OutboxStatusPending, NextAttemptAt: time.Now()}}}
}

func TestOutbox_RetriesWithGrowingBackoffThenSends(t *testing.T) {
	repo := outboxWith("t9")
	n := &flakyNotifier{failures: 2}
	w := NewOutboxWorker(repo, n, slog.Default())
	for i := 0; i < 3; i++ {
		if _, err := w.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n.calls != 3 || len(repo.sent) != 1 || len(repo.rescheduled) != 2 || repo.rescheduled[1] <= repo.rescheduled[0] {
		t.Fatalf("calls=%d sent=%v backoff=%v", n.calls, repo.sent, repo.rescheduled)
	}
	// Ruling 5.4: the notifier and the scoped writes run under the event's
	// own internal tenant, not the ticker's tenant-less context.
	for i, id := range append(n.tenants, repo.tenants...) {
		if id != "t9" {
			t.Fatalf("call %d ran under tenant %q", i, id)
		}
	}
	for _, k := range n.kinds {
		if k != "internal" {
			t.Fatalf("tenant kind = %q", k)
		}
	}
	// Every send has a deadline well inside the lease.
	if len(n.deadlines) != n.calls {
		t.Fatalf("sends without a deadline: %d of %d", n.calls-len(n.deadlines), n.calls)
	}
	for _, d := range n.deadlines {
		if d <= 0 || d > outboxLease/2 {
			t.Fatalf("send deadline %v not within half the lease", d)
		}
	}
}

// A settle write that reports the lease as lost (another worker took the
// event over) is not an error of the pass.
type lostLeaseOutbox struct{ *memOutbox }

func (l lostLeaseOutbox) MarkSent(context.Context, string, time.Time) error {
	return repository.ErrLeaseLost
}

func TestOutbox_LostLeaseIsNotAPassError(t *testing.T) {
	repo := lostLeaseOutbox{outboxWith("t1")}
	w := NewOutboxWorker(repo, &flakyNotifier{}, slog.Default())
	if n, err := w.RunOnce(context.Background()); n != 1 || err != nil {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
}

func TestOutbox_GivesUpAfterMaxAttempts(t *testing.T) {
	repo := outboxWith("t1")
	n := &flakyNotifier{failures: 1 << 30}
	w := NewOutboxWorker(repo, n, slog.Default())
	for i := 0; i < outboxMaxAttempts+5; i++ {
		if _, err := w.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n.calls != outboxMaxAttempts || len(repo.failed) != 1 || len(repo.sent) != 0 {
		t.Fatalf("calls=%d failed=%v sent=%v", n.calls, repo.failed, repo.sent)
	}
}

func TestOutbox_NothingDue(t *testing.T) {
	w := NewOutboxWorker(&memOutbox{}, &flakyNotifier{}, slog.Default())
	if n, err := w.RunOnce(context.Background()); n != 0 || err != nil {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
}
