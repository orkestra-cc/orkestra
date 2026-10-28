package services

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
)

func TestErasureRetryWorkerRunOnceDrainsDueRequests(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	for _, id := range []string{"r-1", "r-2", "r-3"} {
		repo.Put(models.ErasureRequest{
			UUID: id, UserUUID: "u-" + id, Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
			NextAttemptAt: ptrTime(erasureT0.Add(-time.Second)),
			Progress:      []models.ErasureProducerProgress{failed("user")},
		})
	}
	repo.Put(models.ErasureRequest{
		UUID: "r-later", UserUUID: "u-later", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		NextAttemptAt: ptrTime(erasureT0.Add(time.Hour)),
		Progress:      []models.ErasureProducerProgress{failed("user")},
	})
	svc, _ := newErasureSvc(repo, &stubProducer{subject: "user"})
	w := NewErasureRetryWorker(svc, nil)

	n, err := w.RunOnce(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("RunOnce = %d, %v; want 3, nil", n, err)
	}
	for _, id := range []string{"r-1", "r-2", "r-3"} {
		if got := repo.Row(id); got.Status != models.ErasureRequestCompleted {
			t.Fatalf("%s should complete: %+v", id, got)
		}
	}
	if got := repo.Row("r-later"); got.Status != models.ErasureRequestIncomplete {
		t.Fatalf("not-yet-due row must stay untouched: %+v", got)
	}
}

func TestErasureRetryWorkerLoopReturnsOnStop(t *testing.T) {
	t.Parallel()
	svc, _ := newErasureSvc(erasuretest.NewMemRepo())
	w := NewErasureRetryWorker(svc, nil)
	w.interval = 10 * time.Millisecond
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() { w.Loop(context.Background(), stop); close(finished) }()
	time.Sleep(50 * time.Millisecond)
	close(stop)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Loop did not return after stop")
	}
}
