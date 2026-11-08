package services

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func piiTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live PII producer tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	db := client.Database("orkestra_sdd_test_pii_" + uuid.NewString())
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

func seedDevice(t *testing.T, db *mongo.Database, tier, user, device, token string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := db.Collection(models.DeviceTokensCollection).InsertOne(context.Background(), models.DeviceToken{
		UUID: uuid.NewString(), Tier: tier, UserUUID: user, DeviceID: device, SessionID: "s",
		Token: token, Platform: "ios", AppVersion: "2.0", Active: true, CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPIIProducer_ExportsPushDevicesMaskedBothTiers(t *testing.T) {
	db := piiTestDB(t)
	seedDevice(t, db, "operator", "u1", "d-op", "ABCDEFGH-secret-operator-token")
	seedDevice(t, db, "client", "u1", "d-cl", "12345678-secret-client-token")
	seedDevice(t, db, "operator", "other", "d-x", "OTHERUSER-token")

	out, err := NewPIIProducer(db).ExportPersonalData(context.Background(), "u1")
	if err != nil || out == nil {
		t.Fatalf("export: %v %v", out, err)
	}
	raw, _ := json.Marshal(out)
	s := string(raw)
	for _, leak := range []string{"secret", "OTHERUSER", "d-x"} {
		if strings.Contains(s, leak) {
			t.Fatalf("export leaks %q: %s", leak, s)
		}
	}
	var got struct {
		PushDevices []map[string]any `json:"pushDevices"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || len(got.PushDevices) != 2 {
		t.Fatalf("pushDevices: %s (%v)", s, err)
	}
	tokens := map[string]string{}
	for _, d := range got.PushDevices {
		tokens[d["deviceId"].(string)] = d["token"].(string)
		for _, k := range []string{"platform", "appVersion", "active", "createdAt", "lastSeenAt"} {
			if _, ok := d[k]; !ok {
				t.Errorf("device missing %s: %v", k, d)
			}
		}
	}
	if tokens["d-op"] != "ABCDEFGH…" || tokens["d-cl"] != "12345678…" {
		t.Fatalf("tokens not masked to 8 chars + ellipsis: %v", tokens)
	}
}

func TestPIIProducer_DevicesAloneAreExported(t *testing.T) {
	db := piiTestDB(t)
	seedDevice(t, db, "client", "u2", "d", "tok")
	out, err := NewPIIProducer(db).ExportPersonalData(context.Background(), "u2")
	if err != nil || out == nil {
		t.Fatalf("a user with only devices still has data: %v %v", out, err)
	}
	if none, _ := NewPIIProducer(db).ExportPersonalData(context.Background(), "nobody"); none != nil {
		t.Fatalf("no data must stay nil, got %v", none)
	}
}

func TestPIIProducer_PurgeDeletesDevicesBothTiersInBothModes(t *testing.T) {
	for _, mode := range []iface.EraseMode{iface.EraseHardDelete, iface.EraseAnonymize} {
		db := piiTestDB(t)
		seedDevice(t, db, "operator", "u1", "a", "t1")
		seedDevice(t, db, "client", "u1", "b", "t2")
		seedDevice(t, db, "operator", "keep", "c", "t3")
		res, err := NewPIIProducer(db).PurgePersonalData(context.Background(), "u1", mode)
		if err != nil {
			t.Fatal(err)
		}
		if res.RowsDeleted != 2 || len(res.Collections) != 1 || res.Collections[0] != models.DeviceTokensCollection {
			t.Fatalf("mode %v: result %+v", mode, res)
		}
		//tenantscope:allow test: counts rows left in a throwaway database
		n, _ := db.Collection(models.DeviceTokensCollection).CountDocuments(context.Background(), bson.M{})
		if n != 1 {
			t.Fatalf("mode %v: %d rows left, want only the other user's", mode, n)
		}
	}
}
