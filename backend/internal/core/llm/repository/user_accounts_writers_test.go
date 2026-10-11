package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
)

func tokenSet(tag string, saved time.Time) models.AccountTokenSet {
	return models.AccountTokenSet{
		AccessToken:      models.Envelope{Alg: models.EnvelopeAlgLocal, Ciphertext: "at-" + tag},
		RefreshToken:     models.Envelope{Alg: models.EnvelopeAlgLocal, Ciphertext: "rt-" + tag},
		TokenType:        "Bearer",
		ExpiresAt:        saved.Add(time.Hour),
		RefreshExpiresAt: saved.Add(models.RefreshTokenLifetime),
		SavedAt:          saved,
		Scopes:           []string{"openid", models.ScopePlanUsage},
		PlanUsageGranted: true,
		Status:           models.AccountStatusActive,
	}
}

// The targeted writers touch only their own fields: a last-use stamp or a
// status change racing a refresh can never write an old token set back.
func TestUserAccounts_TargetedWritersKeepTheTokenSet(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC().Truncate(time.Millisecond)
	a := account("a1", "u1", "oaiapp_1", true, now)
	a.SavedAt = now
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	// A refresh lands after a stale copy of the document was read.
	stale, _ := repo.Get(ctx, "a1")
	if err := repo.ReplaceTokens(ctx, "a1", now, tokenSet("new", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	used := now.Add(2 * time.Second)
	if err := repo.SetLastUsed(ctx, stale.UUID, used); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetStatus(ctx, stale.UUID, models.AccountStatusNeedsReauth, "llm.user_account_reauth_required"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken.Ciphertext != "at-new" || got.RefreshToken.Ciphertext != "rt-new" {
		t.Fatalf("token set reverted: %+v", got)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Fatalf("lastUsedAt = %v", got.LastUsedAt)
	}
	if got.Status != models.AccountStatusNeedsReauth || got.LastError != "llm.user_account_reauth_required" {
		t.Fatalf("status = %s / %s", got.Status, got.LastError)
	}
	if got.Label != a.Label || got.ClientID != "oaiapp_1" || !got.Selected {
		t.Fatalf("other fields touched: %+v", got)
	}
	// Clearing the error keeps the status writer usable for recovery.
	if err := repo.SetStatus(ctx, "a1", models.AccountStatusActive, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, "a1"); got.Status != models.AccountStatusActive || got.LastError != "" {
		t.Fatalf("recovered status = %s / %q", got.Status, got.LastError)
	}
	for _, c := range []struct {
		name string
		err  error
	}{
		{"last used, other org", repo.SetLastUsed(ctxFor("t2"), "a1", used)},
		{"status, other org", repo.SetStatus(ctxFor("t2"), "a1", models.AccountStatusRevoked, "")},
		{"last used, unknown", repo.SetLastUsed(ctx, "nope", used)},
		{"status, unknown", repo.SetStatus(ctx, "nope", models.AccountStatusRevoked, "")},
	} {
		if !errors.Is(c.err, ErrNotFound) {
			t.Errorf("%s: err = %v", c.name, c.err)
		}
	}
}

func TestUserAccounts_ReplaceTokensIsFencedOnSavedAt(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	a := account("a1", "u1", "oaiapp_1", true, t0)
	a.SavedAt = t0
	a.Email = "old@example.test"
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(time.Second)
	set := tokenSet("r1", t1)
	set.Email = "new@example.test"
	if err := repo.ReplaceTokens(ctx, "a1", t0, set); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx, "a1")
	if !got.SavedAt.Equal(t1) || got.IDToken.Ciphertext != "id-a1" || got.Email != "new@example.test" || !got.PlanUsageGranted {
		t.Fatalf("after replace: %+v", got)
	}
	// The same previous savedAt again loses: another writer already rotated.
	if err := repo.ReplaceTokens(ctx, "a1", t0, tokenSet("r2", t1.Add(time.Second))); !errors.Is(err, ErrStaleTokenSet) {
		t.Fatalf("stale replace err = %v", err)
	}
	idt := models.Envelope{Alg: models.EnvelopeAlgLocal, Ciphertext: "id-r3"}
	set3 := tokenSet("r3", t1.Add(2*time.Second))
	set3.IDToken = &idt
	set3.PlanUsageGranted = false
	set3.Status = models.AccountStatusPlanDisabled
	set3.EarliestRefreshAt = &t1
	if err := repo.ReplaceTokens(ctx, "a1", t1, set3); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx, "a1")
	if got.IDToken.Ciphertext != "id-r3" || got.Email != "new@example.test" || got.Status != models.AccountStatusPlanDisabled ||
		got.EarliestRefreshAt == nil || !got.EarliestRefreshAt.Equal(t1) {
		t.Fatalf("after second replace: %+v", got)
	}
	if err := repo.ReplaceTokens(ctxFor("t2"), "a1", got.SavedAt, tokenSet("x", t1.Add(time.Hour))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant replace err = %v", err)
	}
	if err := repo.ReplaceTokens(ctx, "nope", got.SavedAt, tokenSet("x", t1.Add(time.Hour))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown replace err = %v", err)
	}
}

// Concurrent writers that read the same set: exactly one wins the CAS.
func TestUserAccounts_ConcurrentReplaceTokensOneWinner(t *testing.T) {
	db := newTestDB(t)
	ensureAccountIndexes(t, db)
	repo := NewUserAccounts(db)
	ctx := ctxFor("t1")
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	a := account("a1", "u1", "oaiapp_1", true, t0)
	a.SavedAt = t0
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		wins, stales int
	)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := repo.ReplaceTokens(ctx, "a1", t0, tokenSet("c", t0.Add(time.Duration(i+1)*time.Millisecond)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrStaleTokenSet):
				stales++
			default:
				t.Errorf("replace: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || stales != 5 {
		t.Fatalf("wins = %d, stales = %d", wins, stales)
	}
}
