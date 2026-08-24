package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Mongo integration tests for the DB-backed services (soc2, retention) ---
//
// Skip unless MONGO_TEST_URI is set, mirroring the repository suite. These
// cover the aggregation/query paths that the pure-unit tests can't reach.

func newServiceTestDB(t *testing.T) (*mongo.Database, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skipf("skipping integration test: set MONGO_TEST_URI to run (e.g. mongodb://admin:<pw>@localhost:27017)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_compliance_svc_" + hex.EncodeToString(suffix))
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	}
	return db, cleanup
}

func insertDoc(t *testing.T, db *mongo.Database, coll string, doc bson.M) {
	t.Helper()
	if _, err := db.Collection(coll).InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("insert into %s: %v", coll, err)
	}
}

// TestSOC2GenerateAggregates seeds the source collections and pins that the
// evidence summary reflects them: privileged head-count, MFA coverage, failed
// logins, and KMS lifecycle.
func TestSOC2GenerateAggregates(t *testing.T) {
	db, cleanup := newServiceTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Two privileged operators (administrator), one ordinary (manager).
	insertDoc(t, db, "operator_users", bson.M{"uuid": "op-1", "role": "administrator"})
	insertDoc(t, db, "operator_users", bson.M{"uuid": "op-2", "role": "administrator"})
	insertDoc(t, db, "operator_users", bson.M{"uuid": "op-3", "role": "manager"})
	// Only op-1 has an MFA factor enrolled.
	insertDoc(t, db, "operator_mfa_factors", bson.M{"userUuid": "op-1", "type": "totp"})
	// One recent failed login.
	insertDoc(t, db, models.AuditEventsCollection, bson.M{
		"uuid": "a-1", "action": "auth.login.failed", "timestamp": time.Now().UTC(),
	})
	insertDoc(t, db, models.AuditEventsCollection, bson.M{
		"uuid": "a-2", "action": "auth.login.succeeded", "timestamp": time.Now().UTC(),
	})
	// One active key, one shredded.
	insertDoc(t, db, models.KMSKeysCollection, bson.M{"uuid": "k-1", "tenantUuid": "t-1", "state": models.KMSStateActive})
	insertDoc(t, db, models.KMSKeysCollection, bson.M{"uuid": "k-2", "tenantUuid": "t-2", "state": models.KMSStatePendingDeletion})

	ev, err := NewSOC2EvidenceService(db).Generate(ctx)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if ev.Summary["privileged_users"] != 2 {
		t.Fatalf("privileged_users = %d; want 2", ev.Summary["privileged_users"])
	}
	if ev.Summary["privileged_with_mfa"] != 1 {
		t.Fatalf("privileged_with_mfa = %d; want 1", ev.Summary["privileged_with_mfa"])
	}
	if ev.Summary["failed_logins_24h"] != 1 {
		t.Fatalf("failed_logins_24h = %d; want 1", ev.Summary["failed_logins_24h"])
	}
	if ev.Summary["kms_keys_active"] != 1 || ev.Summary["kms_keys_shredded"] != 1 {
		t.Fatalf("kms lifecycle wrong: active=%d shredded=%d", ev.Summary["kms_keys_active"], ev.Summary["kms_keys_shredded"])
	}
	if _, ok := ev.Controls["CC6.1_logical_access"]; !ok {
		t.Fatal("expected CC6.1 control in evidence")
	}
}

// TestRetentionPreviewAndRunOnce seeds anonymized tombstones past the window
// and pins that Preview lists them (dry run) and RunOnce reaps them through
// the DSR when enabled.
func TestRetentionPreviewAndRunOnce(t *testing.T) {
	db, cleanup := newServiceTestDB(t)
	defer cleanup()
	ctx := context.Background()

	old := time.Now().UTC().Add(-10 * 365 * 24 * time.Hour) // well past a 5y window
	recent := time.Now().UTC().Add(-24 * time.Hour)
	insertDoc(t, db, "operator_users", bson.M{"uuid": "expired-1", "deletedAt": old})
	insertDoc(t, db, "client_users", bson.M{"uuid": "expired-2", "deletedAt": old})
	insertDoc(t, db, "operator_users", bson.M{"uuid": "fresh-1", "deletedAt": recent})
	insertDoc(t, db, "operator_users", bson.M{"uuid": "live-1"}) // no deletedAt

	// DSR with an empty producer registry: Erase succeeds with nothing to do,
	// so RunOnce counts every expired tombstone it processes.
	dsr := NewDSRService(iface.NewPIIProducerRegistry(), nil, slog.Default())
	cfg := func() RetentionConfig { return RetentionConfig{Enabled: true, Years: 5} }
	svc := NewRetentionService(db, dsr, slog.Default(), cfg)

	ids, cutoff, err := svc.Preview(ctx)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if cutoff.After(time.Now().UTC()) {
		t.Fatalf("cutoff %v should be in the past", cutoff)
	}
	if len(ids) != 2 {
		t.Fatalf("Preview returned %d expired tombstones (%v); want 2 (expired-1, expired-2)", len(ids), ids)
	}

	purged, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if purged != 2 {
		t.Fatalf("RunOnce purged %d; want 2", purged)
	}
}

// TestCreateKeyLiveMongoBarrier races LocalKMS.CreateKey against the real
// unique tenantUuid index the compliance module declares in Collections()
// (module.go) — the in-memory fake in kms_test.go models this index, but
// only a real MongoDB proves the repository's mongo.IsDuplicateKeyError
// translation actually recognizes the driver's E11000 error shape. Two
// goroutines call CreateKey for the same tenant at once, looped enough
// iterations to reliably land both inserts inside the race window: both
// callers must converge on the same keyID, and exactly one document must
// ever land in Mongo for that tenant.
func TestCreateKeyLiveMongoBarrier(t *testing.T) {
	db, cleanup := newServiceTestDB(t)
	defer cleanup()
	ctx := context.Background()

	coll := db.Collection(models.KMSKeysCollection)
	//tenantscope:allow test setup mirrors the production unique index declared in module.go Collections()
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantUuid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create tenantUuid unique index: %v", err)
	}

	repo := repository.NewKMSKeyRepo(db)
	kms := &LocalKMS{repo: repo, masterKey: bytes32(0x5c)}

	const iterations = 40
	for i := 0; i < iterations; i++ {
		tenantUUID := "t-live-race-" + strconv.Itoa(i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		ids := make([]string, 2)
		errs := make([]error, 2)
		wg.Add(2)
		for g := 0; g < 2; g++ {
			go func(g int) {
				defer wg.Done()
				<-start
				ids[g], errs[g] = kms.CreateKey(ctx, tenantUUID)
			}(g)
		}
		close(start)
		wg.Wait()

		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("iteration %d: CreateKey errors: %v, %v", i, errs[0], errs[1])
		}
		if ids[0] == "" || ids[0] != ids[1] {
			t.Fatalf("iteration %d: diverged keys %q != %q", i, ids[0], ids[1])
		}

		//tenantscope:allow test assertion counts rows in one isolated per-run test database
		count, err := coll.CountDocuments(ctx, bson.M{"tenantUuid": tenantUUID})
		if err != nil {
			t.Fatalf("iteration %d: count documents: %v", i, err)
		}
		if count != 1 {
			t.Fatalf("iteration %d: expected exactly one persisted key for tenant, got %d", i, count)
		}
	}
}
