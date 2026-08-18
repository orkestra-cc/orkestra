package services

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMFAChallengeBeginAndConsume(t *testing.T) {
	store := NewMemoryOAuthStateStore()
	svc := NewMFAChallengeService(store)

	ch, err := svc.Begin(context.Background(), "u-1", MFAPurposeEnroll, "SECRETBASE32")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if ch.ID == "" || ch.UserUUID != "u-1" || ch.PendingSecret != "SECRETBASE32" {
		t.Fatalf("unexpected challenge payload: %+v", ch)
	}

	got, err := svc.Consume(context.Background(), ch.ID)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got.UserUUID != "u-1" {
		t.Fatalf("consumed wrong challenge: %+v", got)
	}

	// Second consume fails because the first one deleted the record.
	if _, err := svc.Consume(context.Background(), ch.ID); err != ErrMFAChallengeNotFound {
		t.Fatalf("expected ErrMFAChallengeNotFound, got %v", err)
	}
}

func TestMFAChallengeIncrementAttemptsCapsOut(t *testing.T) {
	store := NewMemoryOAuthStateStore()
	svc := NewMFAChallengeService(store)

	ch, err := svc.Begin(context.Background(), "u-2", MFAPurposeLogin, "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := 1; i <= MFAMaxAttempts; i++ {
		if _, err := svc.IncrementAttempts(context.Background(), ch.ID); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}
	// The MFAMaxAttempts-th increment should have deleted the challenge.
	if _, err := svc.Peek(context.Background(), ch.ID); err != ErrMFAChallengeNotFound {
		t.Fatalf("challenge not deleted after cap: %v", err)
	}
}

func TestMFAChallengeRequiresUserUUID(t *testing.T) {
	svc := NewMFAChallengeService(NewMemoryOAuthStateStore())
	if _, err := svc.Begin(context.Background(), "", MFAPurposeEnroll, ""); err == nil {
		t.Fatalf("expected error for empty userUUID")
	}
}

func TestMFAChallengeConsume_AllowsExactlyOneConcurrentWinner(t *testing.T) {
	svc := NewMFAChallengeService(NewMemoryOAuthStateStore())
	ch, err := svc.Begin(context.Background(), "u-concurrent", MFAPurposeLogin, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	const callers = 32
	start := make(chan struct{})
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.Consume(context.Background(), ch.ID); err == nil {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := winners.Load(); got != 1 {
		t.Fatalf("successful consumes = %d, want exactly 1", got)
	}
}

type deleteFailingOAuthStateStore struct {
	mu     sync.Mutex
	states map[string][]byte
}

func (s *deleteFailingOAuthStateStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[key] = append([]byte(nil), value...)
	return nil
}

func (s *deleteFailingOAuthStateStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.states[key]
	if !ok {
		return nil, ErrMFAChallengeNotFound
	}
	return append([]byte(nil), v...), nil
}

func (s *deleteFailingOAuthStateStore) Delete(context.Context, string) error {
	return errors.New("delete unavailable")
}

func (s *deleteFailingOAuthStateStore) DeleteByPattern(context.Context, string) error { return nil }
func (s *deleteFailingOAuthStateStore) Incr(context.Context, string, time.Duration) (int64, error) {
	return 1, nil
}

func (s *deleteFailingOAuthStateStore) Take(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.states[key]
	if !ok {
		return nil, ErrMFAChallengeNotFound
	}
	delete(s.states, key)
	return append([]byte(nil), v...), nil
}

func TestMFAChallengeConsume_DoesNotDependOnBestEffortDelete(t *testing.T) {
	store := &deleteFailingOAuthStateStore{states: map[string][]byte{}}
	svc := NewMFAChallengeService(store)
	ch, err := svc.Begin(context.Background(), "u-delete-failure", MFAPurposeLogin, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := svc.Consume(context.Background(), ch.ID); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	if _, err := svc.Consume(context.Background(), ch.ID); !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Fatalf("second Consume = %v, want ErrMFAChallengeNotFound", err)
	}
}
