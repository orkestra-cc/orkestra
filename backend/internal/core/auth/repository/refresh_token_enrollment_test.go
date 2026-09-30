package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestRefreshRepository_ActiveTokensByUserIncludesOrphans(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	ctx := context.Background()
	for _, tc := range []struct {
		raw, user, sid   string
		expired, revoked bool
	}{
		{"caller", "race-user", "caller-sid", false, false},
		{"orphan", "race-user", "orphan-sid", false, false},
		{"foreign", "foreign-user", "foreign-sid", false, false},
		{"expired", "race-user", "expired-sid", true, false},
		{"revoked", "race-user", "revoked-sid", false, true},
	} {
		doc := refreshRaceDoc(tc.raw, "")
		doc.UserUUID, doc.SessionUUID, doc.IsRevoked = tc.user, tc.sid, tc.revoked
		if tc.expired {
			doc.ExpiresAt = time.Now().Add(-time.Hour)
		}
		if err := repo.CreateRefreshToken(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	// No session documents exist in this isolated database, deliberately.
	tokens, err := repo.GetActiveTokensByUser(ctx, "race-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("active user tokens=%d, want caller and orphan only", len(tokens))
	}
	found := map[string]bool{}
	for _, token := range tokens {
		found[token.SessionUUID] = true
	}
	if !found["caller-sid"] || !found["orphan-sid"] {
		t.Fatalf("session anchors must not filter active refresh credentials: %v", found)
	}
	if err := repo.RevokeTokensBySession(ctx, "orphan-sid", "password_added"); err != nil {
		t.Fatal(err)
	}
	if token, err := repo.GetByToken(ctx, utils.HashRefreshToken("orphan")); err != nil || token != nil {
		t.Fatalf("orphan remains usable: token=%v error=%v", token, err)
	}
	if token, err := repo.GetByToken(ctx, utils.HashRefreshToken("caller")); err != nil || token == nil {
		t.Fatalf("caller must remain usable: token=%v error=%v", token, err)
	}
	orphan, err := repo.GetByTokenAny(ctx, utils.HashRefreshToken("orphan"))
	if err != nil || orphan == nil || !orphan.IsRevoked || orphan.RevokedReason != "password_added" || orphan.RevokedAt == nil {
		t.Fatalf("orphan revocation must persist: token=%v error=%v", orphan, err)
	}
}

func TestRefreshRepository_ActiveTokensByUserReportsCursorFailure(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// More than Mongo's default first batch ensures a live server cursor.
	for i := 0; i < 150; i++ {
		if err := repo.CreateRefreshToken(ctx, refreshRaceDoc(fmt.Sprintf("cursor-%d", i), "")); err != nil {
			t.Fatal(err)
		}
	}
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	killResult := make(chan error, 1)
	monitor := &event.CommandMonitor{Succeeded: func(commandCtx context.Context, e *event.CommandSucceededEvent) {
		if e.CommandName != "find" {
			return
		}
		cursorID := e.Reply.Lookup("cursor", "id").Int64()
		if cursorID == 0 {
			killResult <- errors.New("find did not return a server cursor")
			return
		}
		// Kill only this test's cursor via the independent, unmonitored client.
		// This exercises the real driver's getMore failure without a mock.
		var result struct {
			Killed []int64 `bson:"cursorsKilled"`
		}
		err := repo.collection.Database().RunCommand(commandCtx, bson.D{
			{Key: "killCursors", Value: repo.collection.Name()},
			{Key: "cursors", Value: []int64{cursorID}},
		}).Decode(&result)
		if err == nil && (len(result.Killed) != 1 || result.Killed[0] != cursorID) {
			err = errors.New("test cursor was not killed")
		}
		killResult <- err
	}}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetMonitor(monitor))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	monitored := NewOperatorRefreshTokenRepository(client.Database(repo.collection.Database().Name()))
	tokens, err := monitored.GetActiveTokensByUser(ctx, "race-user")
	select {
	case killErr := <-killResult:
		if killErr != nil {
			t.Fatal(killErr)
		}
	case <-ctx.Done():
		t.Fatal("find did not produce a cursor before the test deadline")
	}
	var commandErr mongo.CommandError
	if !errors.As(err, &commandErr) || commandErr.Code != 43 {
		t.Fatalf("partial token list must report CursorNotFound, got %d tokens and error=%v", len(tokens), err)
	}
}
