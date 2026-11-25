package services

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// operatorProducerFixture wires the operator-tier producer on real
// repositories and seeds, in each of its seven collections, rows for "u1"
// (two in the first, one in the others) and for "other".
func operatorProducerFixture(t *testing.T) iface.PIIProducer {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live auth PII producer tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("auth_pii_count_" + uuid.NewString())
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	collections := []string{
		authModels.OperatorRefreshTokensCollection, authModels.OperatorSessionsCollection,
		authModels.OperatorEmailTokensCollection, authModels.OperatorMFAFactorsCollection,
		authModels.SecurityEventsCollection, authModels.DeviceTrustCollection, authModels.OperatorPINDevicesCollection,
	}
	for i, c := range collections {
		docs := []any{bson.M{"userUuid": "u1"}, bson.M{"userUuid": "other"}}
		if i == 0 {
			docs = append(docs, bson.M{"userUuid": "u1"})
		}
		//tenantscope:allow Test seeds auth collections directly in an isolated test database.
		if _, err := db.Collection(c).InsertMany(ctx, docs); err != nil {
			t.Fatal(err)
		}
	}
	events, err := NewSecurityEventService(db)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPIIProducer(PIIProducerKeyOperator,
		repository.NewOperatorRefreshTokenRepository(db), repository.NewOperatorAuthSessionRepository(db),
		repository.NewOperatorEmailTokenRepository(db), repository.NewOperatorMFAFactorRepository(db),
		events, repository.NewDeviceTrustRepository(db),
		authModels.OperatorRefreshTokensCollection, authModels.OperatorSessionsCollection,
		authModels.OperatorEmailTokensCollection, authModels.OperatorMFAFactorsCollection)
	p.(PINDeviceAware).SetPINDevices(repository.NewOperatorPINDeviceRepository(db), authModels.OperatorPINDevicesCollection)
	return p
}

func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	p := operatorProducerFixture(t)
	counter, ok := p.(iface.PIIProducerCounter)
	if !ok {
		t.Fatal("auth producer does not implement PIIProducerCounter")
	}
	ctx := context.Background()

	got, err := counter.CountPersonalData(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows != 8 {
		t.Fatalf("rows = %d, want 8 (two refresh tokens + one in each of six others)", got.Rows)
	}
	res, err := p.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	if err != nil {
		t.Fatal(err)
	}
	if want := res.RowsDeleted + res.RowsAnonymized; got.Rows != want {
		t.Fatalf("count %d != purge result %d", got.Rows, want)
	}
	a, b := slices.Clone(got.Collections), slices.Clone(res.Collections)
	slices.Sort(a)
	slices.Sort(b)
	if !slices.Equal(a, b) {
		t.Fatalf("collections %v != %v", a, b)
	}
	other, err := counter.CountPersonalData(ctx, "other")
	if err != nil || other.Rows != 7 {
		t.Fatalf("other subject = %+v, %v; want 7 untouched", other, err)
	}
	after, err := counter.CountPersonalData(ctx, "u1")
	if err != nil || after.Rows != 0 || len(after.Collections) != 0 {
		t.Fatalf("after purge = %+v, %v", after, err)
	}
}

// A repository that cannot count must fail the count, not under-report it.
func TestPIIProducer_CountRefusesRepositoryWithoutCounter(t *testing.T) {
	p := NewPIIProducer("auth.test", &gateRefreshRepo{}, nil, nil, nil, nil, nil, "rt", "", "", "")
	if _, err := p.(iface.PIIProducerCounter).CountPersonalData(context.Background(), "u1"); err == nil {
		t.Fatal("expected an error for a repository without CountByUser")
	}
}
