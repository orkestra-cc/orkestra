package migrator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeStore struct {
	sentinel bool
	// matched/modified counts keyed by collection name
	matched  map[string]int64
	modified map[string]int64
	// renamedOrder records the collections RenameOwnerToTenantUUID was
	// called on, in call order.
	renamedOrder []string
	renameErr    error
	markedFor    []CollectionResult
}

func (f *fakeStore) SentinelExists(context.Context) (bool, error) {
	return f.sentinel, nil
}
func (f *fakeStore) RenameOwnerToTenantUUID(_ context.Context, coll string) (int64, int64, error) {
	if f.renameErr != nil {
		return 0, 0, f.renameErr
	}
	f.renamedOrder = append(f.renamedOrder, coll)
	return f.matched[coll], f.modified[coll], nil
}
func (f *fakeStore) MarkSentinel(_ context.Context, results []CollectionResult) error {
	f.markedFor = results
	return nil
}

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRun_AppliesEveryCollectionAndStampsSentinel(t *testing.T) {
	s := &fakeStore{
		matched:  map[string]int64{"subscriptions_subscriptions": 5, "tenant_entitlements": 3},
		modified: map[string]int64{"subscriptions_subscriptions": 5, "tenant_entitlements": 3},
	}
	m := &Migrator{Store: s, Logger: nopLogger()}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Skipped {
		t.Fatalf("expected not skipped on first run")
	}
	if len(s.renamedOrder) != len(Collections) {
		t.Fatalf("rename calls: %v want %v", s.renamedOrder, Collections)
	}
	for i, coll := range Collections {
		if s.renamedOrder[i] != coll {
			t.Errorf("rename[%d] = %s, want %s", i, s.renamedOrder[i], coll)
		}
	}
	if len(s.markedFor) != len(Collections) {
		t.Fatalf("sentinel stamped with %d results, want %d", len(s.markedFor), len(Collections))
	}
}

func TestRun_SkipWhenSentinelExists(t *testing.T) {
	s := &fakeStore{sentinel: true}
	m := &Migrator{Store: s, Logger: nopLogger()}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sum.Skipped {
		t.Fatalf("expected skipped on second run")
	}
	if len(s.renamedOrder) != 0 {
		t.Fatalf("must not call rename when sentinel present, got %v", s.renamedOrder)
	}
	if s.markedFor != nil {
		t.Fatalf("must not re-stamp sentinel, got %v", s.markedFor)
	}
}

func TestRun_DryRunDoesNotWrite(t *testing.T) {
	s := &fakeStore{}
	m := &Migrator{Store: s, Logger: nopLogger(), DryRun: true}
	sum, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Skipped {
		t.Fatalf("dry-run should still report a fresh run")
	}
	if len(s.renamedOrder) != 0 {
		t.Fatalf("dry-run must not invoke RenameOwnerToTenantUUID, got %v", s.renamedOrder)
	}
	if s.markedFor != nil {
		t.Fatalf("dry-run must not stamp sentinel, got %v", s.markedFor)
	}
	if len(sum.Results) != len(Collections) {
		t.Fatalf("dry-run still reports per-collection rows for the operator preview, got %d", len(sum.Results))
	}
}

func TestRun_FailureSkipsSentinel(t *testing.T) {
	s := &fakeStore{renameErr: errors.New("permission denied")}
	m := &Migrator{Store: s, Logger: nopLogger()}
	_, err := m.Run(context.Background())
	if err == nil {
		t.Fatal("expected rename error to surface")
	}
	if s.markedFor != nil {
		t.Fatal("sentinel must not be stamped on failure")
	}
}
