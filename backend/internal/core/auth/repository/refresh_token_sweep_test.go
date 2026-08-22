package repository

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
)

func insertSweepRow(t *testing.T, repo *refreshTokenRepository, uuidStr string, expiresIn time.Duration, revokedAgo time.Duration) {
	t.Helper()
	doc := &models.RefreshTokenDoc{
		UUID: uuidStr, UserUUID: "sweep-user", Token: "hash-" + uuidStr,
		SessionUUID: "sweep-session", DeviceID: "sweep-device",
		IssuedAt: time.Now(), CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(expiresIn),
	}
	if revokedAgo > 0 {
		revokedAt := time.Now().Add(-revokedAgo)
		doc.IsRevoked = true
		doc.RevokedAt = &revokedAt
		doc.RevokedReason = models.RevokeReasonRotated
	}
	if _, err := repo.collection.InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("insert %s: %v", uuidStr, err)
	}
}

func rowExists(t *testing.T, repo *refreshTokenRepository, uuidStr string) bool {
	t.Helper()
	//tenantscope:allow Live repository test inspects one isolated test database directly.
	n, err := repo.collection.CountDocuments(context.Background(), bson.M{"uuid": uuidStr})
	if err != nil {
		t.Fatalf("count %s: %v", uuidStr, err)
	}
	return n == 1
}

// The deletion rule: a row may be deleted only once its token can no
// longer pass temporal validation. Revocation state is irrelevant in both
// directions — an unexpired rotated row must survive, because that row is
// exactly what replay detection matches a reused token against; an
// expired active row may go, because replaying it cannot mint anything.
// This is strictly narrower than the old "one refresh TTL after
// revocation" rule and stays correct across a JWT_REFRESH_TOKEN_EXPIRY
// change between restarts. ADR-0017 D7.
func TestSweep_NeverDeletesAnUnexpiredRow(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()

	insertSweepRow(t, repo, "keep-recently-revoked", 6*24*time.Hour, time.Hour)
	insertSweepRow(t, repo, "keep-long-revoked", 24*time.Hour, 30*24*time.Hour)
	insertSweepRow(t, repo, "delete-expired-active", -time.Hour, 0)
	insertSweepRow(t, repo, "delete-expired-revoked", -time.Hour, 30*24*time.Hour)

	deleted, hasMore, err := repo.CleanupExpiredTokens(context.Background(), SweepBatchLimit)
	if err != nil {
		t.Fatalf("CleanupExpiredTokens: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if hasMore {
		t.Error("hasMore = true with only 2 eligible rows")
	}
	for _, id := range []string{"keep-recently-revoked", "keep-long-revoked"} {
		if !rowExists(t, repo, id) {
			t.Errorf("%s was deleted while its token could still pass temporal validation", id)
		}
	}
	for _, id := range []string{"delete-expired-active", "delete-expired-revoked"} {
		if rowExists(t, repo, id) {
			t.Errorf("%s survived despite being expired", id)
		}
	}
}

// One cycle deletes at most the batch bound and reports hasMore from the
// (limit+1)-th SELECTED row — never from CountDocuments, which on the
// five-minute drain cadence would scan the whole eligible range 288 times
// a day to answer a yes/no question.
func TestSweep_BatchIsBoundedAndReportsHasMore(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()

	// A small limit keeps the test fast while exercising the same code
	// path; SweepBatchLimit itself is a constant, not behaviour.
	const limit = 10
	for i := 0; i < limit+5; i++ {
		insertSweepRow(t, repo, "expired-"+string(rune('a'+i)), -time.Hour, 0)
	}

	deleted, hasMore, err := repo.CleanupExpiredTokens(context.Background(), limit)
	if err != nil {
		t.Fatalf("CleanupExpiredTokens: %v", err)
	}
	if deleted != limit {
		t.Errorf("deleted = %d, want exactly the batch bound %d", deleted, limit)
	}
	if !hasMore {
		t.Error("hasMore = false with 5 rows left; the scheduler would drop to the 6-hour idle cadence mid-drain")
	}

	deleted, hasMore, err = repo.CleanupExpiredTokens(context.Background(), limit)
	if err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if deleted != 5 || hasMore {
		t.Errorf("second batch = (%d, %v), want (5, false)", deleted, hasMore)
	}
}

func TestCountExpiredTokens_CountsOnlyEligibleRows(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	insertSweepRow(t, repo, "live", 24*time.Hour, 0)
	insertSweepRow(t, repo, "expired-1", -time.Hour, 0)
	insertSweepRow(t, repo, "expired-2", -time.Hour, 30*24*time.Hour)

	n, err := repo.CountExpiredTokens(context.Background())
	if err != nil {
		t.Fatalf("CountExpiredTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}
