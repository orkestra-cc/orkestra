package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// reportingStub is a stubProducer that also reports a retention obligation.
type reportingStub struct {
	*stubProducer
	ob  *iface.RetentionObligation
	err error
}

func (r reportingStub) RetainUntil(context.Context, string) (*iface.RetentionObligation, error) {
	return r.ob, r.err
}

func gateAt(years int, created *time.Time, producers ...iface.PIIProducer) *RetentionGate {
	reg := iface.NewPIIProducerRegistry()
	for _, p := range producers {
		reg.Register(p)
	}
	g := NewRetentionGate(nil, reg, func() int { return years })
	g.now = func() time.Time { return erasureT0 }
	g.createdAt = func(context.Context, string) (time.Time, bool, error) {
		if created == nil {
			return time.Time{}, false, nil
		}
		return *created, true, nil
	}
	return g
}

func TestRetentionGateEvaluate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	twoYearsAgo := erasureT0.AddDate(-2, 0, 0)
	longAgo := erasureT0.AddDate(-10, 0, 0)

	t.Run("off", func(t *testing.T) {
		got, err := gateAt(0, &twoYearsAgo, &stubProducer{subject: "user"}).Evaluate(ctx, "u")
		if err != nil || got != nil {
			t.Fatalf("got %+v, %v; want nil", got, err)
		}
	})
	t.Run("floor active", func(t *testing.T) {
		got, err := gateAt(5, &twoYearsAgo).Evaluate(ctx, "u")
		if err != nil || got == nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		if want := twoYearsAgo.AddDate(5, 0, 0); !got.RetainUntil.Equal(want) {
			t.Fatalf("RetainUntil = %v; want %v", got.RetainUntil, want)
		}
		if len(got.Reasons) != 1 || got.Reasons[0] != "platform_minimum" {
			t.Fatalf("reasons = %v", got.Reasons)
		}
	})
	t.Run("floor past", func(t *testing.T) {
		got, err := gateAt(5, &longAgo).Evaluate(ctx, "u")
		if err != nil || got != nil {
			t.Fatalf("got %+v, %v; want nil", got, err)
		}
	})
	t.Run("no user row has no floor", func(t *testing.T) {
		got, err := gateAt(5, nil).Evaluate(ctx, "u")
		if err != nil || got != nil {
			t.Fatalf("got %+v, %v; want nil", got, err)
		}
	})
	t.Run("reporter later than floor wins and both reasons listed", func(t *testing.T) {
		later := erasureT0.AddDate(8, 0, 0)
		rep := reportingStub{stubProducer: &stubProducer{subject: "billing"}, ob: &iface.RetentionObligation{Until: later, Reason: "fiscal_records"}}
		got, err := gateAt(5, &twoYearsAgo, rep).Evaluate(ctx, "u")
		if err != nil || got == nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		if !got.RetainUntil.Equal(later) {
			t.Fatalf("RetainUntil = %v; want %v", got.RetainUntil, later)
		}
		if len(got.Reasons) != 2 {
			t.Fatalf("reasons = %v; want both", got.Reasons)
		}
	})
	t.Run("expired reporter obligation ignored", func(t *testing.T) {
		rep := reportingStub{stubProducer: &stubProducer{subject: "billing"}, ob: &iface.RetentionObligation{Until: erasureT0, Reason: "fiscal_records"}}
		got, err := gateAt(0, nil, rep).Evaluate(ctx, "u")
		if err != nil || got != nil {
			t.Fatalf("got %+v, %v; want nil", got, err)
		}
	})
	t.Run("reporter error", func(t *testing.T) {
		boom := errors.New("boom")
		rep := reportingStub{stubProducer: &stubProducer{subject: "billing"}, err: boom}
		got, err := gateAt(0, nil, rep).Evaluate(ctx, "u")
		if !errors.Is(err, boom) || got != nil {
			t.Fatalf("got %+v, %v; want the reporter error", got, err)
		}
	})
}

func gatedSvc(repo *erasuretest.MemRepo, g *RetentionGate, producers ...iface.PIIProducer) (*ErasureRequestService, *DSRService) {
	svc, dsr := newErasureSvc(repo, producers...)
	dsr.SetRetentionGate(g)
	return svc, dsr
}

func TestCheckRetention(t *testing.T) {
	t.Parallel()
	created := erasureT0.AddDate(-2, 0, 0)
	dsr, _ := newDSRService()
	dsr.SetRetentionGate(gateAt(5, &created))
	ctx := context.Background()

	if err := dsr.CheckRetention(ctx, "u", iface.EraseAnonymize); err != nil {
		t.Fatalf("anonymize must pass: %v", err)
	}
	err := dsr.CheckRetention(ctx, "u", iface.EraseHardDelete)
	var gerr *RetentionGateError
	if !errors.Is(err, ErrRetentionMinimum) || !errors.As(err, &gerr) {
		t.Fatalf("hard delete: %v", err)
	}
	boom := errors.New("boom")
	dsr.SetRetentionGate(gateAt(0, nil, reportingStub{stubProducer: &stubProducer{subject: "x"}, err: boom}))
	err = dsr.CheckRetention(ctx, "u", iface.EraseHardDelete)
	if !errors.Is(err, ErrRetentionCheckUnavailable) || errors.Is(err, ErrRetentionMinimum) {
		t.Fatalf("reporter failure must fail closed as unavailable: %v", err)
	}
}

func TestExecuteHardDeleteBeforeFloorIsRefused(t *testing.T) {
	t.Parallel()
	created := erasureT0.AddDate(-2, 0, 0)
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: erasureT0})
	user := &stubProducer{subject: "user"}
	svc, _ := gatedSvc(repo, gateAt(5, &created), user)

	_, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	var gerr *RetentionGateError
	if !errors.As(err, &gerr) || !gerr.RetainUntil.Equal(created.AddDate(5, 0, 0)) {
		t.Fatalf("err = %v", err)
	}
	if user.purgeCalls != 0 || repo.Row("r-1").Status != models.ErasureRequestPending {
		t.Fatalf("nothing may run: purge=%d row=%+v", user.purgeCalls, repo.Row("r-1"))
	}

	// Anonymize is not gated.
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseAnonymize, "admin-1"); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	if user.purgeCalls != 1 {
		t.Fatalf("anonymize should run the producer, calls=%d", user.purgeCalls)
	}
}

func TestExecuteLegalHoldIsCheckedBeforeRetention(t *testing.T) {
	t.Parallel()
	created := erasureT0.AddDate(-2, 0, 0)
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending})
	svc, dsr := gatedSvc(repo, gateAt(5, &created), &stubProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "a"); !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("err = %v; want legal hold first", err)
	}
}

func TestEraseNowHardDeleteBeforeFloorRecordsNothing(t *testing.T) {
	t.Parallel()
	created := erasureT0.AddDate(-2, 0, 0)
	repo := erasuretest.NewMemRepo()
	user := &stubProducer{subject: "user"}
	svc, _ := gatedSvc(repo, gateAt(5, &created), user)

	_, err := svc.EraseNow(context.Background(), "u-1", "t-1", "self_service", "u-1", iface.EraseHardDelete)
	if !errors.Is(err, ErrRetentionMinimum) {
		t.Fatalf("err = %v", err)
	}
	if rows, _ := repo.ListByStatus(context.Background()); len(rows) != 0 || user.purgeCalls != 0 {
		t.Fatalf("no row, no producer: rows=%+v purge=%d", rows, user.purgeCalls)
	}
	if _, err := svc.EraseNow(context.Background(), "u-1", "t-1", "retention", "x", iface.EraseAnonymize); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
}

func TestEraseNowReporterFailureFailsClosed(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	user := &stubProducer{subject: "user"}
	rep := reportingStub{stubProducer: &stubProducer{subject: "billing"}, err: errors.New("down")}
	svc, _ := gatedSvc(repo, gateAt(0, nil, rep), user)
	_, err := svc.EraseNow(context.Background(), "u-1", "", "self_service", "u-1", iface.EraseHardDelete)
	if !errors.Is(err, ErrRetentionCheckUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if rows, _ := repo.ListByStatus(context.Background()); len(rows) != 0 || user.purgeCalls != 0 {
		t.Fatalf("nothing may be recorded or run")
	}
}

func TestRetryDueHardDeleteBeforeFloorSettlesRetentionMinimum(t *testing.T) {
	t.Parallel()
	created := erasureT0.AddDate(-2, 0, 0)
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		Attempts: 1, NextAttemptAt: ptrTime(erasureT0.Add(-time.Second)),
		Progress: []models.ErasureProducerProgress{failed("user")},
	})
	user := &stubProducer{subject: "user"}
	svc, _ := gatedSvc(repo, gateAt(5, &created), user)

	if ok, err := svc.RetryDue(context.Background()); !ok || err != nil {
		t.Fatalf("RetryDue = %v, %v", ok, err)
	}
	row := repo.Row("r-1")
	if user.purgeCalls != 0 || row.Status != models.ErasureRequestIncomplete || row.LastError != models.ErasureLastErrorRetention ||
		row.Attempts != 2 || row.NextAttemptAt == nil || !row.NextAttemptAt.Equal(erasureT0.Add(2*time.Minute)) {
		t.Fatalf("row wrong: %+v purge=%d", row, user.purgeCalls)
	}
}

func TestExecuteIncompleteUsesStoredModeForTheGate(t *testing.T) {
	t.Parallel()
	created := erasureT0.AddDate(-2, 0, 0)
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Mode: "anonymize",
		Progress: []models.ErasureProducerProgress{failed("user")}})
	user := &stubProducer{subject: "user"}
	svc, _ := gatedSvc(repo, gateAt(5, &created), user)
	// The caller asks for hard_delete, but the stored mode anonymize applies.
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "a"); err != nil {
		t.Fatalf("stored anonymize must not be gated: %v", err)
	}
}

type panickingReporter struct{ *stubProducer }

func (panickingReporter) RetainUntil(context.Context, string) (*iface.RetentionObligation, error) {
	panic("kaboom")
}

func TestPanickingReporterFailsClosedAndIsLogged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	g := gateAt(0, nil, panickingReporter{&stubProducer{subject: "billing"}})
	g.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	dsr, _ := newDSRService()
	dsr.SetRetentionGate(g)

	err := dsr.CheckRetention(context.Background(), "u-1", iface.EraseHardDelete)
	if !errors.Is(err, ErrRetentionCheckUnavailable) {
		t.Fatalf("err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"level=ERROR", "key=billing", "userUuid=u-1", "kaboom", "stack="} {
		if !strings.Contains(out, want) {
			t.Fatalf("log lacks %q: %s", want, out)
		}
	}
}

type hangingReporter struct{ *stubProducer }

func (hangingReporter) RetainUntil(ctx context.Context, _ string) (*iface.RetentionObligation, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestReporterTimeoutFailsClosed(t *testing.T) {
	t.Parallel()
	g := gateAt(0, nil, hangingReporter{&stubProducer{subject: "slow"}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := g.Evaluate(ctx, "u"); err == nil {
		t.Fatal("a reporter that does not answer must fail the evaluation")
	}
}

func TestRetentionFloorLeapDay(t *testing.T) {
	t.Parallel()
	created := time.Date(2024, 2, 29, 10, 0, 0, 0, time.UTC)
	g := gateAt(1, &created)
	g.now = func() time.Time { return time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC) }
	got, err := g.Evaluate(context.Background(), "u")
	if err != nil || got == nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Go normalises 2025-02-29 to 2025-03-01: the floor never ends before a full year.
	if want := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC); !got.RetainUntil.Equal(want) {
		t.Fatalf("RetainUntil = %v; want %v", got.RetainUntil, want)
	}
}
