package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ensureAccountIndexes mirrors the llm_user_accounts indexes declared in
// module.go (Collections), which the registry creates at boot.
func ensureAccountIndexes(t *testing.T, db *mongo.Database) {
	t.Helper()
	_, err := db.Collection(CollUserAccounts).Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenantId", Value: 1}, {Key: "userUuid", Value: 1}, {Key: "clientId", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "tenantId", Value: 1}, {Key: "userUuid", Value: 1}, {Key: "provider", Value: 1}, {Key: "selected", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"selected": true})},
		{Keys: bson.D{{Key: "tenantId", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "tenantId", Value: 1}, {Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "userUuid", Value: 1}}, Options: options.Index().SetPartialFilterExpression(bson.M{"userUuid": bson.M{"$type": "string"}})},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func account(uuid, user, clientID string, selected bool, linked time.Time) *models.UserAccount {
	return &models.UserAccount{
		UUID: uuid, UserUUID: user, Provider: models.ProviderOpenAI, ClientID: clientID,
		Label: "acc " + uuid, Status: models.AccountStatusActive, Selected: selected,
		LinkedAt: linked, UpdatedAt: linked,
		IDToken:      models.Envelope{Alg: models.EnvelopeAlgLocal, Ciphertext: "id-" + uuid},
		AccessToken:  models.Envelope{Alg: models.EnvelopeAlgLocal, Ciphertext: "at-" + uuid},
		RefreshToken: models.Envelope{Alg: models.EnvelopeAlgLocal, Ciphertext: "rt-" + uuid},
		Scopes:       []string{models.ScopePlanUsage},
	}
}

func TestUserAccounts_OneSelectedPerUserAndProvider(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC()
	if err := repo.Insert(ctx, account("a1", "u1", "oaiapp_1", true, now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, account("a2", "u1", "oaiapp_2", false, now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	// Same client id for the same user: duplicate.
	if err := repo.Insert(ctx, account("a3", "u1", "oaiapp_1", false, now)); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate client id err = %v", err)
	}
	// Second selected row for the same (user, provider): duplicate too.
	if err := repo.Insert(ctx, account("a4", "u1", "oaiapp_4", true, now)); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("second selected err = %v", err)
	}
	if err := repo.Select(ctx, "u1", models.ProviderOpenAI, "a2"); err != nil {
		t.Fatal(err)
	}
	sel, err := repo.GetSelected(ctx, "u1", models.ProviderOpenAI)
	if err != nil || sel.UUID != "a2" {
		t.Fatalf("selected = %+v, %v", sel, err)
	}
	list, err := repo.ListByUser(ctx, "u1")
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	n := 0
	for _, x := range list {
		if x.Selected {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("selected count = %d", n)
	}
	// Other tenant sees nothing.
	if l, err := repo.ListByUser(ctxFor("t2"), "u1"); err != nil || len(l) != 0 {
		t.Fatalf("cross-tenant list = %+v, %v", l, err)
	}
	if _, err := repo.GetSelected(ctxFor("t2"), "u1", models.ProviderOpenAI); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant selected err = %v", err)
	}
}

func TestUserAccounts_SelectUnknownKeepsPreviousSelection(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC()
	if err := repo.Insert(ctx, account("a1", "u1", "oaiapp_1", true, now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, account("b1", "u2", "oaiapp_9", false, now)); err != nil {
		t.Fatal(err)
	}
	// Unknown uuid, and another user's account: not found, nothing deselected.
	for _, uuid := range []string{"nope", "b1"} {
		if err := repo.Select(ctx, "u1", models.ProviderOpenAI, uuid); !errors.Is(err, ErrNotFound) {
			t.Fatalf("select %s err = %v", uuid, err)
		}
	}
	// Another org's account is not selectable either.
	if err := repo.Select(ctxFor("t2"), "u1", models.ProviderOpenAI, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant select err = %v", err)
	}
	sel, err := repo.GetSelected(ctx, "u1", models.ProviderOpenAI)
	if err != nil || sel.UUID != "a1" {
		t.Fatalf("selection lost: %+v, %v", sel, err)
	}
}

func TestUserAccounts_ConcurrentSelectLeavesExactlyOne(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC()
	uuids := []string{"a1", "a2", "a3"}
	for i, u := range uuids {
		if err := repo.Insert(ctx, account(u, "u1", "oaiapp_"+u, i == 0, now)); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, u := range uuids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.Select(ctx, "u1", models.ProviderOpenAI, u); err != nil {
				t.Errorf("select %s: %v", u, err)
			}
		}()
	}
	wg.Wait()
	list, err := repo.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range list {
		if x.Selected {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("selected count after concurrent selects = %d", n)
	}
}

func TestUserAccounts_GetUpdateDeleteRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC().Truncate(time.Millisecond)
	acc := account("a1", "u1", "oaiapp_1", true, now)
	acc.ExpiresAt = now.Add(time.Hour)
	if err := repo.Insert(ctx, acc); err != nil {
		t.Fatal(err)
	}
	if acc.TenantID != "t1" {
		t.Fatalf("tenant not stamped: %q", acc.TenantID)
	}
	got, err := repo.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken.Ciphertext != "rt-a1" || got.IDToken.Ciphertext != "id-a1" || got.AccessToken.Ciphertext != "at-a1" {
		t.Fatalf("envelopes did not round-trip: %+v", got)
	}
	if !got.ExpiresAt.Equal(acc.ExpiresAt) || len(got.Scopes) != 1 {
		t.Fatalf("round trip = %+v", got)
	}
	if _, err := repo.Get(ctxFor("t2"), "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v", err)
	}

	got.Status = models.AccountStatusNeedsReauth
	got.LastError = "llm.user_account_reauth"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.Get(ctx, "a1")
	if again.Status != models.AccountStatusNeedsReauth || again.LastError != "llm.user_account_reauth" {
		t.Fatalf("update lost: %+v", again)
	}
	if err := repo.Update(ctxFor("t2"), got); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update err = %v", err)
	}

	if err := repo.Delete(ctxFor("t2"), "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete err = %v", err)
	}
	if err := repo.Delete(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete err = %v", err)
	}
	if err := repo.Delete(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
}

func TestUserAccounts_ListAdminIsTenantScopedNewestFirst(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.Insert(ctxFor("t1"), account("old", "u1", "c1", true, now.Add(-time.Hour))))
	must(repo.Insert(ctxFor("t1"), account("new", "u2", "c2", true, now)))
	must(repo.Insert(ctxFor("t2"), account("other", "u3", "c3", true, now)))
	list, err := repo.ListAdmin(ctxFor("t1"))
	if err != nil || len(list) != 2 || list[0].UUID != "new" || list[1].UUID != "old" {
		t.Fatalf("admin list = %+v, %v", list, err)
	}
}

// The DSR methods cross every org on purpose: the same person may hold a
// registration in several orgs.
func TestUserAccounts_DSRAcrossTenants(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.Insert(ctxFor("t1"), account("a1", "u1", "c1", true, now)))
	must(repo.Insert(ctxFor("t2"), account("a2", "u1", "c2", true, now)))
	must(repo.Insert(ctxFor("t1"), account("a3", "u2", "c3", true, now)))

	list, err := repo.ListByUserAllTenants(context.Background(), "u1")
	if err != nil || len(list) != 2 {
		t.Fatalf("all-tenant list = %+v, %v", list, err)
	}
	n, err := repo.DeleteByUser(context.Background(), "u1")
	if err != nil || n != 2 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	if l, _ := repo.ListByUserAllTenants(context.Background(), "u1"); len(l) != 0 {
		t.Fatalf("u1 rows left: %+v", l)
	}
	if l, _ := repo.ListByUser(ctxFor("t1"), "u2"); len(l) != 1 {
		t.Fatalf("other subject touched: %+v", l)
	}
	if n, err := repo.DeleteByUser(context.Background(), "u1"); err != nil || n != 0 {
		t.Fatalf("second delete = %d, %v", n, err)
	}
}

// An empty subject id must match nothing: rows with userUuid "" exist in the
// fixture and must survive both calls.
func TestUserAccounts_DSRRefusesEmptySubject(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	now := time.Now().UTC()
	if err := repo.Insert(ctxFor("t1"), account("e1", "", "c1", true, now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctxFor("t2"), account("e2", "", "c2", true, now)); err != nil {
		t.Fatal(err)
	}
	if l, err := repo.ListByUserAllTenants(context.Background(), ""); !errors.Is(err, ErrEmptySubject) || len(l) != 0 {
		t.Fatalf("list empty subject = %+v, %v", l, err)
	}
	if n, err := repo.DeleteByUser(context.Background(), ""); !errors.Is(err, ErrEmptySubject) || n != 0 {
		t.Fatalf("delete empty subject = %d, %v", n, err)
	}
	for _, tenant := range []string{"t1", "t2"} {
		if l, err := repo.ListByUser(ctxFor(tenant), ""); err != nil || len(l) != 1 {
			t.Fatalf("%s empty-subject row gone: %+v, %v", tenant, l, err)
		}
	}
}

func TestInstallation_HostIDIsStable(t *testing.T) {
	db := newTestDB(t)
	repo := NewInstallation(db)
	id1, err := repo.GetOrCreateHostID(context.Background())
	if err != nil || len(id1) != len("urn:uuid:")+36 || id1[:9] != "urn:uuid:" {
		t.Fatalf("id1 = %q, %v", id1, err)
	}
	id2, err := repo.GetOrCreateHostID(context.Background())
	if err != nil || id1 != id2 {
		t.Fatalf("host id changed: %q != %q (%v)", id1, id2, err)
	}
}

func TestInstallation_ConcurrentFirstCallsAgree(t *testing.T) {
	db := newTestDB(t)
	repo := NewInstallation(db)
	const n = 8
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := repo.GetOrCreateHostID(context.Background())
			if err != nil {
				t.Errorf("call %d: %v", i, err)
			}
			ids[i] = id
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("hosts disagree: %v", ids)
		}
	}
}
