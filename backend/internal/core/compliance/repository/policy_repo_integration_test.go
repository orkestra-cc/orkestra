package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ensurePolicyIndexes creates the unique indexes the repository relies on;
// in production the module registry creates them from Collections().
func ensurePolicyIndexes(t *testing.T, db *mongo.Database) {
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

func testPolicy(uuid, name string, platform bool) *models.Policy {
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := models.NewPlatformPolicy(uuid, now)
	p.Name, p.IsPlatformDefault = name, platform
	if !platform {
		p.Sinks = nil
	}
	return &p
}

func TestPolicyRepo_PoliciesAndVersionCAS(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ensurePolicyIndexes(t, db)
	r := NewPolicyRepo(db)
	ctx := context.Background()

	if err := r.InsertPolicy(ctx, testPolicy("plat", "Platform", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.InsertPolicy(ctx, testPolicy("plat2", "Platform bis", true)); !errors.Is(err, ErrPlatformPolicyExists) {
		t.Fatalf("second platform policy: %v", err)
	}
	a := testPolicy("a", "A", false)
	if err := r.InsertPolicy(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := r.InsertPolicy(ctx, testPolicy("a2", "A", false)); !errors.Is(err, ErrPolicyNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
	if taken, _ := r.NameTaken(ctx, "A", "a"); taken {
		t.Fatal("a policy's own name counted as taken")
	}
	if taken, _ := r.NameTaken(ctx, "A", ""); !taken {
		t.Fatal("name A not taken")
	}
	if p, err := r.GetPlatformPolicy(ctx); err != nil || p.UUID != "plat" {
		t.Fatalf("platform = %v, %v", p, err)
	}

	next := *a
	next.Version, next.Description = 2, "changed"
	if err := r.ReplacePolicy(ctx, &next, 7); !errors.Is(err, ErrPolicyVersionConflict) {
		t.Fatalf("stale replace: %v", err)
	}
	if got, _ := r.GetPolicy(ctx, "a"); got.Version != 1 || got.Description == "changed" {
		t.Fatalf("stale replace wrote: %+v", got)
	}
	missing := next
	missing.UUID = "nope"
	if err := r.ReplacePolicy(ctx, &missing, 1); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("replace missing: %v", err)
	}
	if err := r.ReplacePolicy(ctx, &next, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.DeletePolicy(ctx, "a", 1); !errors.Is(err, ErrPolicyVersionConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if err := r.DeletePolicy(ctx, "a", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetPolicy(ctx, "a"); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("deleted policy: %v", err)
	}
	if list, _ := r.ListPolicies(ctx); len(list) != 1 {
		t.Fatalf("policies = %d, want 1", len(list))
	}
}

func TestPolicyRepo_TxnRollsBack(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ensurePolicyIndexes(t, db)
	r := NewPolicyRepo(db)
	ctx := context.Background()
	a := testPolicy("a", "A", false)
	if err := r.InsertPolicy(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Version 2 already exists: the insert fails inside the transaction.
	if err := r.InsertVersion(ctx, &models.PolicyVersion{PolicyUUID: "a", Version: 2}); err != nil {
		t.Fatal(err)
	}
	next := *a
	next.Version = 2
	err := r.WithTxn(ctx, func(ctx context.Context) error {
		if err := r.ReplacePolicy(ctx, &next, 1); err != nil {
			return err
		}
		return r.InsertVersion(ctx, &models.PolicyVersion{PolicyUUID: "a", Version: 2})
	})
	if !errors.Is(err, ErrPolicyVersionConflict) {
		t.Fatalf("WithTxn = %v, want version conflict", err)
	}
	if got, _ := r.GetPolicy(ctx, "a"); got.Version != 1 {
		t.Fatalf("policy version = %d after a rolled-back transaction", got.Version)
	}
}

func TestPolicyRepo_Assignments(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ensurePolicyIndexes(t, db)
	r := NewPolicyRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := r.GetAssignment(ctx, "t1"); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("missing assignment: %v", err)
	}
	for _, pol := range []string{"a", "b"} { // the second upsert replaces the first
		if err := r.UpsertAssignment(ctx, &models.PolicyAssignment{TenantID: "t1", TenantKind: "external", PolicyUUID: pol, AssignedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := r.GetAssignment(ctx, "t1"); got.PolicyUUID != "b" {
		t.Fatalf("assignment = %+v", got)
	}
	if n, _ := r.CountAssignments(ctx, "b"); n != 1 {
		t.Fatalf("count = %d", n)
	}
	if all, _ := r.ListAssignments(ctx); len(all) != 1 {
		t.Fatalf("assignments = %d", len(all))
	}
	if err := r.InsertAssignmentHistory(ctx, &models.PolicyAssignmentHistory{UUID: "h1", TenantID: "t1", Action: models.AssignmentActionAssign, ChangedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteAssignment(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteAssignment(ctx, "t1"); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestPolicyRepo_ChangeRequestCAS(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ensurePolicyIndexes(t, db)
	r := NewPolicyRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, cr := range []models.PolicyChangeRequest{
		{UUID: "old", Kind: models.ChangeCreate, Status: models.ChangeStatusPending, RequestedAt: now.Add(-15 * 24 * time.Hour)},
		{UUID: "new", Kind: models.ChangeCreate, Status: models.ChangeStatusPending, RequestedAt: now},
	} {
		if err := r.InsertChangeRequest(ctx, &cr); err != nil {
			t.Fatal(err)
		}
	}
	if old, _ := r.ListPendingBefore(ctx, now.Add(-14*24*time.Hour)); len(old) != 1 || old[0].UUID != "old" {
		t.Fatalf("pending before = %+v", old)
	}
	// Two concurrent decisions: exactly one wins.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.DecideChangeRequest(ctx, "new", models.ChangeStatusApproved, "op", "ok", now)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrChangeRequestNotPending):
			t.Fatalf("decide: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d decisions applied, want 1", wins)
	}
	if err := r.DecideChangeRequest(ctx, "ghost", models.ChangeStatusRejected, "op", "no", now); !errors.Is(err, ErrChangeRequestNotFound) {
		t.Fatalf("decide missing: %v", err)
	}
	if got, _ := r.GetChangeRequest(ctx, "new"); got.Status != models.ChangeStatusApproved || got.DecidedBy != "op" || got.DecidedAt == nil {
		t.Fatalf("decided request = %+v", got)
	}
	if pending, _ := r.ListChangeRequests(ctx, models.ChangeStatusPending); len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
}
