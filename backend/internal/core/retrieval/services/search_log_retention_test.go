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
	err    error
	// the unchecked sweep: its calls, last cutoff, count and error
	uncheckedCalls  int
	uncheckedCutoff time.Time
	uncheckedN      int64
	uncheckedErr    error
}

func (f *fakePurger) PurgeBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.calls++
	f.cutoff = cutoff
	return 7, f.err
}

func (f *fakePurger) PurgeUncheckedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.uncheckedCalls++
	f.uncheckedCutoff = cutoff
	return f.uncheckedN, f.uncheckedErr
}

// G2: the retention run also sweeps provisional rows older than the grace,
// and the count is the total of both sweeps.
func TestPurgeSearchLog_SweepsUncheckedOlderThanGrace(t *testing.T) {
	p := &fakePurger{uncheckedN: 2}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.FixedZone("x", 3600))
	n, err := PurgeSearchLog(context.Background(), p, DefaultSearchLogRetentionDays, now)
	require.NoError(t, err)
	assert.Equal(t, int64(9), n)
	assert.Equal(t, 1, p.uncheckedCalls)
	assert.Equal(t, time.Date(2026, 10, 26, 10, 50, 0, 0, time.UTC), p.uncheckedCutoff)
	assert.Equal(t, 10*time.Minute, UncheckedSearchLogGrace)
}

// A failed sweep does not stop the other; both errors are returned with the
// total deleted, and the next run retries.
func TestPurgeSearchLog_RunsBothSweepsWhenOneFails(t *testing.T) {
	retErr, uncErr := errors.New("retention failed"), errors.New("sweep failed")
	p := &fakePurger{err: retErr, uncheckedN: 1, uncheckedErr: uncErr}
	n, err := PurgeSearchLog(context.Background(), p, DefaultSearchLogRetentionDays, time.Now())
	assert.Equal(t, int64(8), n)
	if !errors.Is(err, retErr) || !errors.Is(err, uncErr) {
		t.Fatalf("err = %v, want both sweep errors", err)
	}
	assert.Equal(t, 1, p.calls)
	assert.Equal(t, 1, p.uncheckedCalls)
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
	if p.calls != 0 || p.uncheckedCalls != 0 {
		t.Fatal("purger was called")
	}
}

func TestPurgeSearchLog_RefusesDaysAboveMax(t *testing.T) {
	p := &fakePurger{}
	if _, err := PurgeSearchLog(context.Background(), p, 3651, time.Now()); err == nil || p.calls != 0 || p.uncheckedCalls != 0 {
		t.Fatalf("err=%v calls=%d unchecked=%d", err, p.calls, p.uncheckedCalls)
	}
}
