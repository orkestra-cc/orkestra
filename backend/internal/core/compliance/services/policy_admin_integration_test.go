package services

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services/policytest"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ensurePolicyIndexesForServices(t *testing.T, db *mongo.Database) {
	t.Helper()
	ctx := context.Background()
	for coll, idx := range map[string][]mongo.IndexModel{
		models.PoliciesCollection: {
			{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "isPlatformDefault", Value: 1}}, Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.M{"isPlatformDefault": true})},
		},
		models.PolicyVersionsCollection: {
			{Keys: bson.D{{Key: "policyUuid", Value: 1}, {Key: "version", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		models.PolicyAssignmentsCollection: {
			{Keys: bson.D{{Key: "tenantId", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		models.PolicyAssignmentHistoryCollection: {{Keys: bson.D{{Key: "changedAt", Value: 1}}}},
		models.PolicyChangeRequestsCollection: {
			{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
	} {
		if _, err := db.Collection(coll).Indexes().CreateMany(ctx, idx); err != nil {
			t.Fatalf("indexes on %s: %v", coll, err)
		}
	}
}

func newMongoAdmin(t *testing.T) (*PolicyAdminService, *repository.PolicyRepository, func()) {
	t.Helper()
	admin, repo, _, cleanup := newMongoAdminWithSink(t)
	return admin, repo, cleanup
}

func newMongoAdminWithSink(t *testing.T) (*PolicyAdminService, *repository.PolicyRepository, *policytest.Sink, func()) {
	t.Helper()
	db, cleanup := newServiceTestDB(t)
	ensurePolicyIndexesForServices(t, db)
	repo := repository.NewPolicyRepo(db)
	logger := slog.New(slog.DiscardHandler)
	svc := NewPolicyService(repo, logger)
	tenants := policytest.Tenants{"t1": {UUID: "t1", Kind: iface.TenantKindExternal, Name: "Clinica"}}
	sink := &policytest.Sink{}
	admin := NewPolicyAdminService(repo, svc, tenants, sink, func(context.Context) bool { return true }, logger)
	if err := admin.EnsurePlatformPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	return admin, repo, sink, cleanup
}

// Replicas booting together on a fresh database all call EnsurePlatformPolicy:
// every call returns nil and exactly one platform policy (one version) exists.
func TestPolicyAdmin_Mongo_ConcurrentBootCreatesOnePlatformPolicy(t *testing.T) {
	db, cleanup := newServiceTestDB(t)
	defer cleanup()
	ensurePolicyIndexesForServices(t, db)
	repo := repository.NewPolicyRepo(db)
	logger := slog.New(slog.DiscardHandler)
	const replicas = 6
	var wg sync.WaitGroup
	errs := make([]error, replicas)
	start := make(chan struct{})
	for i := range replicas {
		admin := NewPolicyAdminService(repo, NewPolicyService(repo, logger), nil, &policytest.Sink{}, func(context.Context) bool { return true }, logger)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = admin.EnsurePlatformPolicy(context.Background())
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d: EnsurePlatformPolicy = %v, want nil", i, err)
		}
	}
	ctx := context.Background()
	ps, err := repo.ListPolicies(ctx)
	if err != nil || len(ps) != 1 || !ps[0].IsPlatformDefault {
		t.Fatalf("policies = %+v, %v; want exactly the platform policy", ps, err)
	}
	if vs, err := repo.ListVersions(ctx, ps[0].UUID); err != nil || len(vs) != 1 {
		t.Fatalf("versions = %+v, %v; want one", vs, err)
	}
}

func TestPolicyAdmin_Mongo_ConcurrentApprovalsApplyOnce(t *testing.T) {
	admin, repo, sink, cleanup := newMongoAdminWithSink(t)
	defer cleanup()
	ctx := context.Background()
	in := tenantInput()
	in.LogContent.IPAddress = iface.IPAddressFull
	res, err := admin.Create(ctx, alice, in, "motivo", true)
	if err != nil || res.ChangeRequest == nil {
		t.Fatalf("Create = %+v, %v", res, err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, approver := range []Actor{bob, {UserID: "carol"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = admin.Approve(ctx, approver, res.ChangeRequest.UUID, "ok")
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, repository.ErrChangeRequestNotPending):
			t.Fatalf("the losing approval = %v, want ErrChangeRequestNotPending (not superseded)", err)
		}
	}
	if wins != 1 {
		t.Fatalf("approvals applied = %d (%v), want 1", wins, errs)
	}
	ps, _ := repo.ListPolicies(ctx)
	if len(ps) != 2 {
		t.Fatalf("policies = %d, want platform + 1", len(ps))
	}
	if cr, err := repo.GetChangeRequest(ctx, res.ChangeRequest.UUID); err != nil || cr.Status != models.ChangeStatusApproved {
		t.Fatalf("request = %+v, %v; want approved", cr, err)
	}
	for _, p := range ps {
		if p.IsPlatformDefault {
			continue
		}
		if vs, _ := repo.ListVersions(ctx, p.UUID); len(vs) != 1 {
			t.Fatalf("versions of the created policy = %d, want 1", len(vs))
		}
	}
	if slices.Contains(sink.Actions(), "compliance.change_request.superseded") {
		t.Fatalf("a superseded event was recorded: %v", sink.Actions())
	}
}

func TestPolicyAdmin_Mongo_UpdateIsAtomic(t *testing.T) {
	admin, repo, cleanup := newMongoAdmin(t)
	defer cleanup()
	ctx := context.Background()
	res, err := admin.Create(ctx, alice, tenantInput(), "motivo", false)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Policy
	// A stray version 2 makes the version insert fail inside the transaction.
	if err := repo.InsertVersion(ctx, &models.PolicyVersion{PolicyUUID: p.UUID, Version: 2}); err != nil {
		t.Fatal(err)
	}
	edit := p.Input()
	edit.Description = "cambiata"
	if _, err := admin.Update(ctx, alice, p.UUID, 1, edit, "motivo", false); !errors.Is(err, repository.ErrPolicyVersionConflict) {
		t.Fatalf("Update = %v, want a version conflict", err)
	}
	if cur, _ := repo.GetPolicy(ctx, p.UUID); cur.Version != 1 || cur.Description == "cambiata" {
		t.Fatalf("policy after the rolled-back update = %+v", cur)
	}
}
