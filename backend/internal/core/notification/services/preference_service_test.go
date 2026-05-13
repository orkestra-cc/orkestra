package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
)

// fakePrefRepo implements repository.PreferenceRepository for unit tests.
// We never exercise suppression methods here, so they return zero values.
type fakePrefRepo struct {
	prefs   map[string]*models.PreferenceDoc // by userUUID|category|channel
	getErr  error
	upErr   error
	listErr error
	upserts []*models.PreferenceDoc
}

func newFakePrefRepo() *fakePrefRepo {
	return &fakePrefRepo{prefs: map[string]*models.PreferenceDoc{}}
}

func prefKey(user, category, channel string) string {
	return user + "|" + category + "|" + channel
}

func (f *fakePrefRepo) GetPreference(_ context.Context, user, category, channel string) (*models.PreferenceDoc, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.prefs[prefKey(user, category, channel)], nil
}

func (f *fakePrefRepo) ListByUser(_ context.Context, user string) ([]*models.PreferenceDoc, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*models.PreferenceDoc
	for _, p := range f.prefs {
		if p.UserUUID == user {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePrefRepo) UpsertPreference(_ context.Context, doc *models.PreferenceDoc) error {
	if f.upErr != nil {
		return f.upErr
	}
	cp := *doc
	f.upserts = append(f.upserts, &cp)
	f.prefs[prefKey(doc.UserUUID, doc.Category, doc.Channel)] = &cp
	return nil
}

func (f *fakePrefRepo) IsSuppressed(_ context.Context, _ string) (bool, error)           { return false, nil }
func (f *fakePrefRepo) AddSuppression(_ context.Context, _ *models.SuppressionDoc) error { return nil }
func (f *fakePrefRepo) RemoveSuppression(_ context.Context, _ string) error              { return nil }

func TestPreferenceService_CanDeliver_TransactionalAlwaysTrue(t *testing.T) {
	repo := newFakePrefRepo()
	// Even when the user has explicitly opted out of this category, the
	// transactional shortcut must short-circuit before the lookup.
	repo.prefs[prefKey("user-1", models.CategoryAuthVerifyEmail, models.ChannelEmail)] =
		&models.PreferenceDoc{UserUUID: "user-1", Category: models.CategoryAuthVerifyEmail, Channel: models.ChannelEmail, OptedIn: false}
	repo.getErr = errors.New("must not be called")

	svc := NewPreferenceService(repo)
	ok, err := svc.CanDeliver(context.Background(), "user-1", models.CategoryAuthVerifyEmail, models.ChannelEmail, models.TypeTransactional)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("transactional mail must always deliver")
	}
}

func TestPreferenceService_CanDeliver_EmptyUserAllowed(t *testing.T) {
	repo := newFakePrefRepo()
	repo.getErr = errors.New("should not be reached")
	svc := NewPreferenceService(repo)
	ok, err := svc.CanDeliver(context.Background(), "", "marketing.newsletter", models.ChannelEmail, models.TypeMarketing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("missing user identity should default to allow")
	}
}

func TestPreferenceService_CanDeliver_NoPreferenceDefaultsAllow(t *testing.T) {
	svc := NewPreferenceService(newFakePrefRepo())
	ok, err := svc.CanDeliver(context.Background(), "user-1", "marketing.newsletter", models.ChannelEmail, models.TypeMarketing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("missing preference should default to opted-in")
	}
}

func TestPreferenceService_CanDeliver_OptedInExplicit(t *testing.T) {
	repo := newFakePrefRepo()
	repo.prefs[prefKey("user-1", "marketing.newsletter", models.ChannelEmail)] =
		&models.PreferenceDoc{UserUUID: "user-1", Category: "marketing.newsletter", Channel: models.ChannelEmail, OptedIn: true}
	svc := NewPreferenceService(repo)

	ok, err := svc.CanDeliver(context.Background(), "user-1", "marketing.newsletter", models.ChannelEmail, models.TypeMarketing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("explicitly opted-in user should receive marketing mail")
	}
}

func TestPreferenceService_CanDeliver_OptedOut(t *testing.T) {
	repo := newFakePrefRepo()
	repo.prefs[prefKey("user-1", "marketing.newsletter", models.ChannelEmail)] =
		&models.PreferenceDoc{UserUUID: "user-1", Category: "marketing.newsletter", Channel: models.ChannelEmail, OptedIn: false}
	svc := NewPreferenceService(repo)

	ok, err := svc.CanDeliver(context.Background(), "user-1", "marketing.newsletter", models.ChannelEmail, models.TypeMarketing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("opted-out user must not receive marketing mail")
	}
}

func TestPreferenceService_CanDeliver_RepoErrorPropagates(t *testing.T) {
	repo := newFakePrefRepo()
	repo.getErr = errors.New("mongo down")
	svc := NewPreferenceService(repo)

	ok, err := svc.CanDeliver(context.Background(), "user-1", "marketing.newsletter", models.ChannelEmail, models.TypeMarketing)
	if err == nil {
		t.Fatalf("expected error from repo")
	}
	if ok {
		t.Fatalf("error path must default to deny")
	}
}

func TestPreferenceService_Set_UpsertsExpectedDoc(t *testing.T) {
	repo := newFakePrefRepo()
	svc := NewPreferenceService(repo)

	if err := svc.Set(context.Background(), "user-1", "marketing.newsletter", models.ChannelEmail, false); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(repo.upserts) != 1 {
		t.Fatalf("expected one upsert, got %d", len(repo.upserts))
	}
	got := repo.upserts[0]
	if got.UserUUID != "user-1" || got.Category != "marketing.newsletter" || got.Channel != models.ChannelEmail {
		t.Fatalf("upsert doc mismatch: %+v", got)
	}
	if got.OptedIn {
		t.Fatalf("expected OptedIn=false")
	}
}

func TestPreferenceService_Set_PropagatesRepoError(t *testing.T) {
	repo := newFakePrefRepo()
	repo.upErr = errors.New("write failed")
	svc := NewPreferenceService(repo)
	if err := svc.Set(context.Background(), "user-1", "c", models.ChannelEmail, true); err == nil {
		t.Fatalf("expected error from upsert")
	}
}

func TestPreferenceService_List_ReturnsOnlyMatchingUser(t *testing.T) {
	repo := newFakePrefRepo()
	repo.prefs[prefKey("u1", "a", "email")] = &models.PreferenceDoc{UserUUID: "u1", Category: "a", Channel: "email"}
	repo.prefs[prefKey("u1", "b", "email")] = &models.PreferenceDoc{UserUUID: "u1", Category: "b", Channel: "email"}
	repo.prefs[prefKey("u2", "a", "email")] = &models.PreferenceDoc{UserUUID: "u2", Category: "a", Channel: "email"}

	svc := NewPreferenceService(repo)
	got, err := svc.List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 prefs for u1, got %d", len(got))
	}
	for _, p := range got {
		if p.UserUUID != "u1" {
			t.Fatalf("got pref for wrong user: %s", p.UserUUID)
		}
	}
}
