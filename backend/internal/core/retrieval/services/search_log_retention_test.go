package services

import (
	"context"
	"errors"
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

// fakeSweeper records the unchecked sweep's cutoff and answers n, err.
type fakeSweeper struct {
	calls  int
	cutoff time.Time
	n      int64
	err    error
}

func (f *fakeSweeper) PurgeUncheckedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.calls++
	f.cutoff = cutoff
	return f.n, f.err
}

// G2: the sweep deletes unchecked rows older than the grace (10 minutes).
func TestSweepUncheckedSearchLog_CutoffIsGraceBeforeNow(t *testing.T) {
	s := &fakeSweeper{n: 2}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.FixedZone("x", 3600))
	n, err := SweepUncheckedSearchLog(context.Background(), s, now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	assert.Equal(t, 1, s.calls)
	assert.Equal(t, time.Date(2026, 10, 26, 10, 50, 0, 0, time.UTC), s.cutoff)
	assert.Equal(t, 10*time.Minute, UncheckedSearchLogGrace)
}

// A failed sweep returns the partial count with the error.
func TestSweepUncheckedSearchLog_ReturnsPartialCountWithError(t *testing.T) {
	boom := errors.New("sweep failed")
	s := &fakeSweeper{n: 3, err: boom}
	n, err := SweepUncheckedSearchLog(context.Background(), s, time.Now())
	assert.Equal(t, int64(3), n)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
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
