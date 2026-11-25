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

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// countRig is the llm PII producer over real repositories, seeded so that
// every purge step has rows for "u1" and the neighbouring subject "x" keeps
// its own:
//
//	grants    g1 (u1 by admin), g2 (u1 by u1: self-grant), g3 (x by u1)  -> 3 for u1
//	creds     1 created by u1; models 1 created by u1
//	usage     one finalized and one pending row of u1
//	budgets   b1 recipient u1, b2 edited by u1, b3 both                  -> 4 for u1
//	outbox    1 alert delivered to u1 and x; accounts 1
//
// u1 = 3+1+1+2+4+1+1 = 13. "x" holds g3 as grantee, one credential, one usage.
func countRig(t *testing.T) *PIIProducer {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("llm_pii_count_" + uuid.NewString())
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	seed := map[string][]any{
		repository.CollGrants: {
			bson.M{"uuid": "g1", "userUuid": "u1", "grantedBy": "admin"},
			bson.M{"uuid": "g2", "userUuid": "u1", "grantedBy": "u1"},
			bson.M{"uuid": "g3", "userUuid": "x", "grantedBy": "u1"},
		},
		repository.CollCredentials: {bson.M{"uuid": "c1", "createdBy": "u1"}, bson.M{"uuid": "c2", "createdBy": "x"}},
		repository.CollModels:      {bson.M{"uuid": "m1", "createdBy": "u1"}},
		repository.CollUsage: {
			bson.M{"uuid": "e1", "userUuid": "u1", "outcome": "ok"},
			bson.M{"uuid": "e2", "userUuid": "u1", "outcome": models.OutcomePending},
			bson.M{"uuid": "e3", "userUuid": "x", "outcome": "ok"},
		},
		repository.CollBudgets: {
			bson.M{"tenantId": "t1", "notifyExtraUserUuids": bson.A{"u1", "x"}},
			bson.M{"tenantId": "t2", "updatedBy": "u1"},
			bson.M{"tenantId": "t3", "notifyExtraUserUuids": bson.A{"u1"}, "updatedBy": "u1"},
		},
		repository.CollBudgetOutbox: {bson.M{"tenantId": "t1", "deliveredTo": bson.A{"u1", "x"}}},
		repository.CollUserAccounts: {bson.M{"uuid": "a1", "userUuid": "u1"}},
	}
	for c, docs := range seed {
		//tenantscope:allow Test seeds llm collections directly in an isolated test database.
		if _, err := db.Collection(c).InsertMany(ctx, docs); err != nil {
			t.Fatal(err)
		}
	}
	return NewPIIProducer(repository.NewCredentials(db), repository.NewModels(db), repository.NewGrants(db),
		repository.NewUsage(db), repository.NewBudgets(db), repository.NewOutbox(db),
		repository.NewUserAccounts(db), nil, nil)
}

func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	modes := map[string]iface.EraseMode{"hard-delete": iface.EraseHardDelete, "anonymize": iface.EraseAnonymize}
	for name, mode := range modes {
		t.Run(name, func(t *testing.T) {
			p := countRig(t)
			ctx := context.Background()

			got, err := p.CountPersonalData(ctx, "u1")
			if err != nil {
				t.Fatal(err)
			}
			xBefore, err := p.CountPersonalData(ctx, "x")
			if err != nil || xBefore.Rows != 5 {
				t.Fatalf("x rows before = %+v, %v; want 5", xBefore, err)
			}
			xRowsBefore := xBefore.Rows
			res, err := p.PurgePersonalData(ctx, "u1", mode)
			if err != nil {
				t.Fatal(err)
			}
			if got.Rows != 13 {
				t.Fatalf("rows = %d, want 13", got.Rows)
			}
			if want := res.RowsDeleted + res.RowsAnonymized; got.Rows != want {
				t.Fatalf("count %d != purge result %d (%+v)", got.Rows, want, res)
			}
			// Every collection holds a row of u1, so the sets are equal.
			a, b := slices.Clone(got.Collections), slices.Clone(res.Collections)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Fatalf("collections %v != %v", a, b)
			}
			// The neighbour keeps what is its own: g3 as grantee, c2, e3, the
			// recipient entry of budget t1 and the alert it was delivered.
			x, err := p.CountPersonalData(ctx, "x")
			if err != nil {
				t.Fatal(err)
			}
			if x.Rows != xRowsBefore {
				t.Fatalf("x rows after u1's purge = %d, want %d unchanged", x.Rows, xRowsBefore)
			}
			after, err := p.CountPersonalData(ctx, "u1")
			if err != nil || after.Rows != 0 || len(after.Collections) != 0 {
				t.Fatalf("u1 after purge = %+v, %v", after, err)
			}
		})
	}
}

func TestPIIProducer_CountRefusesEmptySubject(t *testing.T) {
	p := countRig(t)
	if _, err := p.CountPersonalData(context.Background(), ""); err == nil {
		t.Fatal("empty subject must be refused")
	}
}
