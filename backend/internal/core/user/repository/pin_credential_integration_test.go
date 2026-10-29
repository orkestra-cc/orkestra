package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func seedPINUser(t *testing.T, repo *mongoUserRepository, doc bson.M) {
	t.Helper()
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
}

func TestGetForPINAuth_ExactUsernameLiveRowsOnly(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	seedPINUser(t, repo, bson.M{"uuid": "u-live", "username": "mario.rossi", "pinHash": "h"})
	seedPINUser(t, repo, bson.M{"uuid": "u-deleted", "username": "luigi", "deletedAt": time.Now()})

	u, err := repo.GetForPINAuth(ctx, "mario.rossi")
	if err != nil {
		t.Fatal(err)
	}
	if u.UUID != "u-live" || u.PINHash != "h" {
		t.Fatalf("got uuid=%q pinHash=%q", u.UUID, u.PINHash)
	}
	for _, name := range []string{"luigi", "Mario.Rossi", "mario", ""} {
		if _, err := repo.GetForPINAuth(ctx, name); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("username %q: got %v, want ErrUserNotFound", name, err)
		}
	}
}

func TestGetForPINAuth_DuplicateUsernameIsAmbiguous(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	seedPINUser(t, repo, bson.M{"uuid": "u-1", "username": "shared"})
	seedPINUser(t, repo, bson.M{"uuid": "u-2", "username": "shared"})

	if _, err := repo.GetForPINAuth(ctx, "shared"); !errors.Is(err, ErrUsernameAmbiguous) {
		t.Fatalf("got %v, want ErrUsernameAmbiguous", err)
	}
	n, err := repo.CountByUsername(ctx, "shared")
	if err != nil || n != 2 {
		t.Fatalf("count=%d err=%v, want 2", n, err)
	}
}

func TestSetPINHash_StampsAndClearsLock(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	lock := time.Now().Add(time.Hour)
	seedPINUser(t, repo, bson.M{"uuid": "u-1", "pinFailedCount": 4, "pinLockedUntil": lock})

	before := time.Now().Truncate(time.Millisecond)
	if err := repo.SetPINHash(ctx, "u-1", "pin-hash"); err != nil {
		t.Fatal(err)
	}
	u, err := repo.GetByID(ctx, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.PINHash != "pin-hash" || u.PINUpdatedAt == nil || u.PINUpdatedAt.Before(before) {
		t.Fatalf("hash=%q updatedAt=%v", u.PINHash, u.PINUpdatedAt)
	}
	if u.PINFailedCount != 0 || u.PINLockedUntil != nil {
		t.Fatalf("counter/lock not cleared: count=%d lock=%v", u.PINFailedCount, u.PINLockedUntil)
	}
}

func TestPINWrites_MissingOrDeletedUser(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	seedPINUser(t, repo, bson.M{"uuid": "deleted", "deletedAt": time.Now()})
	for _, id := range []string{"deleted", "missing"} {
		if err := repo.SetPINHash(ctx, id, "h"); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("SetPINHash(%s): got %v", id, err)
		}
		if err := repo.ClearPIN(ctx, id); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("ClearPIN(%s): got %v", id, err)
		}
	}
}

func TestPINFailures_CountLockAndClear(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	seedPINUser(t, repo, bson.M{"uuid": "u-1", "pinHash": "h", "failedLoginCount": 2})

	if err := repo.RecordFailedPIN(ctx, "u-1", nil); err != nil {
		t.Fatal(err)
	}
	lock := time.Now().Add(15 * time.Minute).Truncate(time.Millisecond)
	if err := repo.RecordFailedPIN(ctx, "u-1", &lock); err != nil {
		t.Fatal(err)
	}
	u, _ := repo.GetByID(ctx, "u-1")
	if u.PINFailedCount != 2 || u.PINLockedUntil == nil || !u.PINLockedUntil.Equal(lock) {
		t.Fatalf("count=%d lock=%v", u.PINFailedCount, u.PINLockedUntil)
	}
	if u.FailedLoginCount != 2 || u.LockedUntil != nil {
		t.Fatalf("password counters touched: count=%d lock=%v", u.FailedLoginCount, u.LockedUntil)
	}

	if err := repo.ClearFailedPINs(ctx, "u-1"); err != nil {
		t.Fatal(err)
	}
	u, _ = repo.GetByID(ctx, "u-1")
	if u.PINFailedCount != 0 || u.PINLockedUntil != nil || u.PINHash != "h" {
		t.Fatalf("after clear: count=%d lock=%v hash=%q", u.PINFailedCount, u.PINLockedUntil, u.PINHash)
	}

	if err := repo.ClearPIN(ctx, "u-1"); err != nil {
		t.Fatal(err)
	}
	u, _ = repo.GetByID(ctx, "u-1")
	if u.PINHash != "" || u.PINUpdatedAt != nil {
		t.Fatalf("after ClearPIN: hash=%q updatedAt=%v", u.PINHash, u.PINUpdatedAt)
	}
}
