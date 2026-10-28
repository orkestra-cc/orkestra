package services

import (
	"context"
	"testing"
	"time"
)

type fakePurger struct {
	calls  int
	cutoff time.Time
}

func (f *fakePurger) PurgeBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.calls++
	f.cutoff = cutoff
	return 7, nil
}

func TestPurgeSearchLog_CutoffIsDaysBeforeNow(t *testing.T) {
	p := &fakePurger{}
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	n, err := PurgeSearchLog(context.Background(), p, DefaultSearchLogRetentionDays, now)
	require.NoError(t, err)
	assert.Equal(t, int64(7), n)
	assert.Equal(t, time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC), p.cutoff)
}

func TestPurgeSearchLog_RefusesNonPositiveDays(t *testing.T) {
	p := &fakePurger{}
	_, err := PurgeSearchLog(context.Background(), p, 0, time.Now())
	if err == nil {
		t.Fatal("want error")
	}
	if p.calls != 0 {
		t.Fatal("purger was called")
	}
}

func TestPurgeSearchLog_RefusesDaysAboveMax(t *testing.T) {
	p := &fakePurger{}
	if _, err := PurgeSearchLog(context.Background(), p, 3651, time.Now()); err == nil || p.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, p.calls)
	}
}
