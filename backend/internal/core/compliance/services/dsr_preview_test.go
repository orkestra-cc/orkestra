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
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// countStub is a stubProducer that also counts.
type countStub struct {
	*stubProducer
	count iface.PersonalDataCount
	err   error
	pnc   any
	block chan struct{} // when set, CountPersonalData ignores ctx and waits here
}

func (c countStub) CountPersonalData(context.Context, string) (iface.PersonalDataCount, error) {
	if c.pnc != nil {
		panic(c.pnc)
	}
	if c.block != nil {
		<-c.block
	}
	return c.count, c.err
}

type fakeHolds struct {
	holds []models.LegalHold
	err   error
}

func (f fakeHolds) ListActive(context.Context, string) ([]models.LegalHold, error) {
	return f.holds, f.err
}

type fakeRequests struct {
	rows []models.ErasureRequest
	err  error
	got  repository.ErasureListFilter
}

func (f *fakeRequests) List(_ context.Context, fl repository.ErasureListFilter) ([]models.ErasureRequest, string, error) {
	f.got = fl
	return f.rows, "", f.err
}

func TestCountAllCountableAndNot(t *testing.T) {
	t.Parallel()
	dsr, _ := newDSRService(
		countStub{stubProducer: &stubProducer{subject: "user"}, count: iface.PersonalDataCount{Rows: 3, Collections: []string{"operator_users"}}},
		&stubProducer{subject: "plain"},
		countStub{stubProducer: &stubProducer{subject: "bad"}, err: errors.New("mongo said secret@example.com")},
	)
	got := dsr.CountAll(context.Background(), "u")
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if g := got[0]; g.Key != "user" || !g.Countable || g.Rows != 3 || len(g.Collections) != 1 || g.Error != "" {
		t.Fatalf("countable = %+v", g)
	}
	if g := got[1]; g.Key != "plain" || g.Countable || g.Rows != 0 || g.Error != "" {
		t.Fatalf("not countable = %+v", g)
	}
	if g := got[2]; !g.Countable || g.Error != "count failed" {
		t.Fatalf("failed = %+v", g)
	}
}

func TestCountAllPanicAndHangDoNotAffectOthers(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	block := make(chan struct{})
	defer close(block)
	reg := iface.NewPIIProducerRegistry()
	reg.Register(countStub{stubProducer: &stubProducer{subject: "ok1"}, count: iface.PersonalDataCount{Rows: 1}})
	reg.Register(countStub{stubProducer: &stubProducer{subject: "panics"}, pnc: "boom"})
	reg.Register(countStub{stubProducer: &stubProducer{subject: "hangs"}, block: block})
	reg.Register(countStub{stubProducer: &stubProducer{subject: "ok2"}, count: iface.PersonalDataCount{Rows: 2}})
	dsr := NewDSRService(reg, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	dsr.countTimeout = 50 * time.Millisecond

	start := time.Now()
	got := dsr.CountAll(context.Background(), "u-1")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v, the hanging counter was not cut off", d)
	}
	if len(got) != 4 {
		t.Fatalf("got %+v", got)
	}
	for _, i := range []int{1, 2} {
		if !got[i].Countable || got[i].Error != "count failed" || got[i].Rows != 0 {
			t.Fatalf("line %d = %+v", i, got[i])
		}
	}
	if got[0].Rows != 1 || got[0].Error != "" || got[3].Rows != 2 || got[3].Error != "" {
		t.Fatalf("others affected: %+v", got)
	}
	out := logs.String()
	if !strings.Contains(out, "panics") || !strings.Contains(out, "boom") || !strings.Contains(out, "hangs") {
		t.Fatalf("log = %s", out)
	}
}

func previewSvc(t *testing.T, holds fakeHolds, reqs *fakeRequests, gate *RetentionGate, producers ...iface.PIIProducer) *DSRPreviewService {
	t.Helper()
	dsr, _ := newDSRService(producers...)
	dsr.SetRetentionGate(gate)
	return NewDSRPreviewService(dsr, holds, reqs, nil, slog.Default())
}

func TestPreviewFlags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	recent := erasureT0.AddDate(-2, 0, 0)

	t.Run("clear", func(t *testing.T) {
		p, err := previewSvc(t, fakeHolds{}, &fakeRequests{}, gateAt(0, &recent)).Preview(ctx, "u")
		if err != nil || !p.HardDeleteAllowed || !p.AnonymizeAllowed || p.Retention != nil || p.LegalHolds == nil || p.OpenRequests == nil {
			t.Fatalf("%+v, %v", p, err)
		}
	})
	t.Run("hold blocks both", func(t *testing.T) {
		p, _ := previewSvc(t, fakeHolds{holds: []models.LegalHold{{UUID: "h"}}}, &fakeRequests{}, gateAt(0, &recent)).Preview(ctx, "u")
		if p.HardDeleteAllowed || p.AnonymizeAllowed || len(p.LegalHolds) != 1 {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("retention blocks only hard delete", func(t *testing.T) {
		p, _ := previewSvc(t, fakeHolds{}, &fakeRequests{}, gateAt(5, &recent)).Preview(ctx, "u")
		if p.HardDeleteAllowed || !p.AnonymizeAllowed || p.Retention == nil || p.Retention.RetainUntil.IsZero() {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("gate error is fail-closed", func(t *testing.T) {
		g := gateAt(5, &recent)
		g.createdAt = func(context.Context, string) (time.Time, bool, error) {
			return time.Time{}, false, errors.New("db down")
		}
		p, err := previewSvc(t, fakeHolds{}, &fakeRequests{}, g).Preview(ctx, "u")
		if err != nil || p.HardDeleteAllowed || !p.AnonymizeAllowed || p.RetentionError != "retention check failed" {
			t.Fatalf("%+v, %v", p, err)
		}
	})
	t.Run("hold lookup error fails the preview", func(t *testing.T) {
		if _, err := previewSvc(t, fakeHolds{err: errors.New("x")}, &fakeRequests{}, nil).Preview(ctx, "u"); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestPreviewListsOpenRequestsOfTheSubject(t *testing.T) {
	t.Parallel()
	reqs := &fakeRequests{rows: []models.ErasureRequest{{UUID: "r1", UserUUID: "u", Status: models.ErasureRequestPending}}}
	p, err := previewSvc(t, fakeHolds{}, reqs, nil, countStub{stubProducer: &stubProducer{subject: "user"}, count: iface.PersonalDataCount{Rows: 2}}).Preview(context.Background(), "u")
	if err != nil || len(p.OpenRequests) != 1 || len(p.Producers) != 1 || p.Producers[0].Rows != 2 {
		t.Fatalf("%+v, %v", p, err)
	}
	if reqs.got.UserUUID != "u" || len(reqs.got.Statuses) != 2 {
		t.Fatalf("filter = %+v", reqs.got)
	}
}

func TestPreviewIsAudited(t *testing.T) {
	t.Parallel()
	dsr, _ := newDSRService(
		countStub{stubProducer: &stubProducer{subject: "user"}, count: iface.PersonalDataCount{Rows: 9}},
		&stubProducer{subject: "plain"},
	)
	sink := &holdSink{}
	svc := NewDSRPreviewService(dsr,
		fakeHolds{holds: []models.LegalHold{{UUID: "h"}}},
		&fakeRequests{rows: []models.ErasureRequest{{UUID: "r1"}, {UUID: "r2"}}},
		sink, slog.Default())
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "op-1")
	if _, err := svc.Preview(ctx, "subj-1"); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %+v", sink.events)
	}
	e := sink.events[0]
	if e.Action != "gdpr.dsr.previewed" || e.ActorUserID != "op-1" || e.ResourceType != "user" || e.ResourceID != "subj-1" {
		t.Fatalf("event = %+v", e)
	}
	if len(e.Metadata) != 3 || e.Metadata["producers"] != 2 || e.Metadata["legalHolds"] != 1 || e.Metadata["openRequests"] != 2 {
		t.Fatalf("metadata = %+v", e.Metadata)
	}
}
