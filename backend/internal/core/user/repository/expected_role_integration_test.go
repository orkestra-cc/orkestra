package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
)

// seedRoleUser inserts one live user with the given role.
func seedRoleUser(t *testing.T, repo *mongoUserRepository, uuid, role string) {
	t.Helper()
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(context.Background(), bson.M{
		"uuid":     uuid,
		"email":    uuid + "@example.com",
		"role":     role,
		"isActive": true,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestUpdate_ExpectedRoleHolds(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	seedRoleUser(t, repo, "u-1", "operator")

	ctx := WithExpectedRole(context.Background(), "operator")
	u, err := repo.Update(ctx, "u-1", &iface.UpdateUserInput{FullName: "Renamed"})
	if err != nil {
		t.Fatalf("Update with the current role as precondition: %v", err)
	}
	if u.FullName != "Renamed" {
		t.Fatalf("fullName = %q, want Renamed", u.FullName)
	}
}

// A promotion landing between the guard's read and the write: the write
// must not apply to the user the guard did not check.
func TestUpdate_ExpectedRoleChangedIsRefused(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	seedRoleUser(t, repo, "u-1", "administrator")

	ctx := WithExpectedRole(context.Background(), "operator")
	if _, err := repo.Update(ctx, "u-1", &iface.UpdateUserInput{FullName: "Renamed"}); !errors.Is(err, ErrRoleChanged) {
		t.Fatalf("Update = %v, want ErrRoleChanged", err)
	}
	u, err := repo.GetByID(context.Background(), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.FullName == "Renamed" {
		t.Fatal("the refused update was written")
	}
}

func TestUpdate_ExpectedRoleOnDeletedUserIsNotFound(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(context.Background(), bson.M{
		"uuid": "gone", "role": "operator", "deletedAt": time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	ctx := WithExpectedRole(context.Background(), "operator")
	if _, err := repo.Update(ctx, "gone", &iface.UpdateUserInput{FullName: "x"}); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("Update = %v, want ErrUserNotFound", err)
	}
}

func TestSoftDeleteAndAliasEmail_ExpectedRole(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	seedRoleUser(t, repo, "promoted", "administrator")
	seedRoleUser(t, repo, "same", "operator")

	if err := repo.SoftDeleteAndAliasEmail(WithExpectedRole(context.Background(), "operator"), "promoted"); !errors.Is(err, ErrRoleChanged) {
		t.Fatalf("SoftDeleteAndAliasEmail on a re-roled user = %v, want ErrRoleChanged", err)
	}
	if _, err := repo.GetByID(context.Background(), "promoted"); err != nil {
		t.Fatalf("the refused delete removed the user: %v", err)
	}
	if err := repo.SoftDeleteAndAliasEmail(WithExpectedRole(context.Background(), "operator"), "same"); err != nil {
		t.Fatalf("SoftDeleteAndAliasEmail with the current role: %v", err)
	}
	if _, err := repo.GetByID(context.Background(), "same"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetByID after delete = %v, want ErrUserNotFound", err)
	}
}
