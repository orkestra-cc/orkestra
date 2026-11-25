package repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestGetActiveByUUIDs(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run the session repository integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(ctx)
	db := client.Database(fmt.Sprintf("auth_active_sessions_test_%d", time.Now().UnixNano()))
	defer db.Drop(ctx)

	op := NewOperatorAuthSessionRepository(db)
	cl := NewClientAuthSessionRepository(db)

	mk := func(r AuthSessionRepository, uuid string, exp time.Time) {
		if err := r.CreateSession(ctx, &models.AuthSessionDoc{UUID: uuid, UserUUID: "u", DeviceID: "d-" + uuid, ExpiresAt: exp}); err != nil {
			t.Fatalf("create %s: %v", uuid, err)
		}
	}
	future := time.Now().Add(time.Hour)
	mk(op, "active", future)
	mk(op, "inactive", future)
	mk(op, "expired", time.Now().Add(-time.Minute))
	mk(cl, "client-only", future)
	if err := op.TerminateSession(ctx, "inactive"); err != nil {
		t.Fatalf("terminate: %v", err)
	}

	got, err := op.GetActiveByUUIDs(ctx, []string{"active", "inactive", "expired", "client-only", "missing"})
	if err != nil {
		t.Fatalf("GetActiveByUUIDs: %v", err)
	}
	var ids []string
	for _, s := range got {
		ids = append(ids, s.UUID)
	}
	sort.Strings(ids)
	if len(ids) != 1 || ids[0] != "active" {
		t.Fatalf("got %v, want [active]", ids)
	}

	empty, err := op.GetActiveByUUIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty input: %v %v", empty, err)
	}
}
