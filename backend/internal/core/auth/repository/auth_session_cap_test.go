package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// liveSessionRepository mirrors liveRefreshRepository: same env vars, same
// per-run database, same cleanup. Split rather than generalised because
// the two set up different indexes.
func liveSessionRepository(t *testing.T) (*authSessionRepository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live session repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("mongo.Ping: %v", err)
	}
	db := client.Database("auth_session_cap_" + uuid.NewString())
	repo := NewOperatorAuthSessionRepository(db).(*authSessionRepository)
	return repo, func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
}

func seedActiveSession(t *testing.T, repo *authSessionRepository, uuidStr string) {
	t.Helper()
	err := repo.CreateSession(context.Background(), &models.AuthSessionDoc{
		UUID: uuidStr, UserUUID: "cap-user", DeviceID: "cap-device",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

// Exactly one caller may win the transition. Without the isActive:true
// predicate every concurrent refresh reports itself the winner, and the
// cap security event and metric fire once per racing request instead of
// once per session. ADR-0017 D4.
func TestExpireSessionForMaxAge_NamesExactlyOneWinner(t *testing.T) {
	repo, cleanup := liveSessionRepository(t)
	defer cleanup()
	seedActiveSession(t, repo, "sess-race")

	const racers = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			won, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-race")
			if err != nil {
				t.Errorf("ExpireSessionForMaxAge: %v", err)
				return
			}
			if won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
	sess, err := repo.GetByUUID(context.Background(), "sess-race")
	if err != nil || sess == nil {
		t.Fatalf("GetByUUID: %v", err)
	}
	if sess.IsActive {
		t.Error("every racer must still terminate the session, whether or not it won")
	}
}

// Losing the race is not an error — the caller must still receive the cap
// sentinel, not a 503.
func TestExpireSessionForMaxAge_AlreadyInactiveIsNotAnError(t *testing.T) {
	repo, cleanup := liveSessionRepository(t)
	defer cleanup()
	seedActiveSession(t, repo, "sess-done")
	if _, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-done"); err != nil {
		t.Fatalf("first call: %v", err)
	}

	won, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-done")
	if err != nil {
		t.Fatalf("second call returned an error: %v", err)
	}
	if won {
		t.Error("the second caller must not report itself the winner")
	}
}

// A UUID with no row is the same shape as the losing side of a race.
func TestExpireSessionForMaxAge_UnknownUUIDIsNotAnError(t *testing.T) {
	repo, cleanup := liveSessionRepository(t)
	defer cleanup()
	won, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-absent")
	if err != nil || won {
		t.Fatalf("got (%v, %v), want (false, nil)", won, err)
	}
}
