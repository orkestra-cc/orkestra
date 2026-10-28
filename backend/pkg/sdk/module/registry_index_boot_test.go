package module_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestEnsureCollections_ComplianceRepeatedBootsReportNoIndexDrift provisions the compliance module's
// Collections() on a real MongoDB as the registry does at boot, several times
// (each run is a boot of the same binary), and fails on the registry's
// index-drift ERROR. A partial filter whose keys reach the server in a
// different order on another boot is a different index specification.
func TestEnsureCollections_ComplianceRepeatedBootsReportNoIndexDrift(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run (e.g. mongodb://admin:<pw>@localhost:27017)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_compliance_boot_" + hex.EncodeToString(suffix))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	const boots = 12
	for boot := 1; boot <= boots; boot++ {
		var logs bytes.Buffer
		reg := module.NewModuleRegistry(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
		reg.Register(compliance.NewModule())
		if err := reg.EnsureCollectionsForTest(db); err != nil {
			t.Fatalf("boot %d: ensureCollections: %v", boot, err)
		}
		if out := logs.String(); strings.Contains(out, "Index spec drift") || strings.Contains(out, "Failed to ensure collection") {
			t.Fatalf("boot %d reported an index problem:\n%s", boot, out)
		}
	}

	// The retention index is there, unique and partial on both conditions.
	cur, err := db.Collection(models.ErasureRequestsCollection).Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var idx []bson.M
	if err := cur.All(context.Background(), &idx); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	for _, ix := range idx {
		if ix["name"] == "userUuid_1" && ix["unique"] == true && ix["partialFilterExpression"] != nil {
			return
		}
	}
	t.Fatalf("unique partial userUuid index missing: %+v", idx)
}
