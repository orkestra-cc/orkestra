package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
)

func TestSetPasswordHashIfUnset_AbsentOrEmptyHash(t *testing.T) {
	for _, initial := range []string{"absent", "empty"} {
		t.Run(initial, func(t *testing.T) {
			repo, cleanup := liveUserRepository(t)
			defer cleanup()
			ctx := context.Background()
			old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			doc := bson.M{"uuid": "u-1", "updatedAt": old}
			if initial == "empty" {
				doc["passwordHash"] = ""
			}
			//tenantscope:allow Live repository test seeds one isolated test database directly.
			if _, err := repo.collection.InsertOne(ctx, doc); err != nil {
				t.Fatal(err)
			}
			before := time.Now().Truncate(time.Millisecond)
			if err := repo.SetPasswordHashIfUnset(ctx, "u-1", "first-hash"); err != nil {
				t.Fatal(err)
			}
			u, err := repo.GetByID(ctx, "u-1")
			if err != nil {
				t.Fatal(err)
			}
			if u.PasswordHash != "first-hash" {
				t.Fatalf("hash=%q, want first-hash", u.PasswordHash)
			}
			if u.PasswordUpdatedAt == nil || u.PasswordUpdatedAt.Before(before) || u.UpdatedAt.Before(before) {
				t.Fatalf("timestamps not set: passwordUpdatedAt=%v updatedAt=%v", u.PasswordUpdatedAt, u.UpdatedAt)
			}
			if !u.PasswordUpdatedAt.Equal(u.UpdatedAt) {
				t.Fatal("password and user timestamps differ")
			}
		})
	}
}

func TestSetPasswordHashIfUnset_ExistingHashPreserved(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(ctx, bson.M{
		"uuid": "u-1", "passwordHash": "existing-hash", "passwordUpdatedAt": old, "updatedAt": old,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPasswordHashIfUnset(ctx, "u-1", "replacement-hash"); !errors.Is(err, iface.ErrPasswordAlreadySet) {
		t.Fatalf("got %v, want ErrPasswordAlreadySet", err)
	}
	u, err := repo.GetByID(ctx, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash != "existing-hash" || u.PasswordUpdatedAt == nil || !u.PasswordUpdatedAt.Equal(old) || !u.UpdatedAt.Equal(old) {
		t.Fatalf("existing password state changed: hash=%q passwordUpdatedAt=%v updatedAt=%v", u.PasswordHash, u.PasswordUpdatedAt, u.UpdatedAt)
	}
}

func TestSetPasswordHashIfUnset_MissingOrDeletedUser(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(ctx, bson.M{"uuid": "deleted-user", "deletedAt": time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing-user", "deleted-user"} {
		if err := repo.SetPasswordHashIfUnset(ctx, id, "first-hash"); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("%s: got %v, want ErrUserNotFound", id, err)
		}
	}
}

func TestSetPasswordHashIfUnset_ConcurrentCallers(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(ctx, bson.M{"uuid": "race-user"}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)
	hashes := []string{"first-hash", "second-hash"}
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = repo.SetPasswordHashIfUnset(ctx, "race-user", hashes[i])
		}(i)
	}
	close(start)
	wg.Wait()
	successes, conflicts := 0, 0
	winner := ""
	for i, err := range results {
		switch {
		case err == nil:
			successes++
			winner = hashes[i]
		case errors.Is(err, iface.ErrPasswordAlreadySet):
			conflicts++
		default:
			t.Fatalf("caller %d: unexpected error %v", i, err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
	u, err := repo.GetByID(ctx, "race-user")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash != winner {
		t.Fatalf("hash=%q, want winner %q", u.PasswordHash, winner)
	}
}
