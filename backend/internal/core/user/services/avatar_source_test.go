package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/user/models"
)

// TestSetAvatarSource exercises the source-validation guard: OAuth
// sources require the matching provider to be active on the user;
// invalid sources are rejected before touching the repo; the previous
// object key is returned so the caller can GC it.
func TestSetAvatarSource(t *testing.T) {
	t.Parallel()

	newSvc := func(seed *models.User) (UserService, *fakeUserRepo) {
		repo := newFakeUserRepo()
		if seed != nil {
			repo.seed(seed)
		}
		return NewUserService(repo, &fakeOAuthProviderRepo{}), repo
	}

	t.Run("initials clears object key and returns previous", func(t *testing.T) {
		t.Parallel()
		svc, repo := newSvc(&models.User{
			UUID:            "u-1",
			Email:           "u@example.com",
			Role:            "operator",
			IsActive:        true,
			AvatarSource:    models.AvatarSourceUploaded,
			AvatarObjectKey: "avatars/operator/u-1/old.png",
		})
		prev, err := svc.SetAvatarSource(context.Background(), "u-1", models.AvatarSourceInitials, "")
		if err != nil {
			t.Fatalf("SetAvatarSource: %v", err)
		}
		if prev != "avatars/operator/u-1/old.png" {
			t.Fatalf("previous = %q, want avatars/operator/u-1/old.png", prev)
		}
		got, _ := repo.GetByID(context.Background(), "u-1")
		if got.AvatarSource != models.AvatarSourceInitials {
			t.Fatalf("source = %q, want initials", got.AvatarSource)
		}
		if got.AvatarObjectKey != "" {
			t.Fatalf("objectKey = %q, want empty", got.AvatarObjectKey)
		}
	})

	t.Run("oauth source requires active link", func(t *testing.T) {
		t.Parallel()
		svc, _ := newSvc(&models.User{
			UUID:     "u-2",
			Email:    "u@example.com",
			Role:     "operator",
			IsActive: true,
			// No OAuth links → request must be rejected.
		})
		_, err := svc.SetAvatarSource(context.Background(), "u-2", models.AvatarSourceOAuthGoogle, "")
		if !errors.Is(err, ErrOAuthProviderNotLinked) {
			t.Fatalf("err = %v, want ErrOAuthProviderNotLinked", err)
		}
	})

	t.Run("oauth source accepts active link", func(t *testing.T) {
		t.Parallel()
		svc, _ := newSvc(&models.User{
			UUID:     "u-3",
			Email:    "u@example.com",
			Role:     "operator",
			IsActive: true,
			OAuthLinks: []models.OAuthLink{
				{Provider: models.OAuthProviderGoogle, IsActive: true},
			},
		})
		_, err := svc.SetAvatarSource(context.Background(), "u-3", models.AvatarSourceOAuthGoogle, "")
		if err != nil {
			t.Fatalf("SetAvatarSource: %v", err)
		}
	})

	t.Run("oauth source rejects inactive link", func(t *testing.T) {
		t.Parallel()
		svc, _ := newSvc(&models.User{
			UUID:     "u-4",
			Email:    "u@example.com",
			Role:     "operator",
			IsActive: true,
			OAuthLinks: []models.OAuthLink{
				{Provider: models.OAuthProviderGoogle, IsActive: false},
			},
		})
		_, err := svc.SetAvatarSource(context.Background(), "u-4", models.AvatarSourceOAuthGoogle, "")
		if !errors.Is(err, ErrOAuthProviderNotLinked) {
			t.Fatalf("err = %v, want ErrOAuthProviderNotLinked", err)
		}
	})

	t.Run("invalid source rejected without touching repo", func(t *testing.T) {
		t.Parallel()
		svc, _ := newSvc(&models.User{UUID: "u-5", Email: "u@example.com", Role: "operator", IsActive: true})
		_, err := svc.SetAvatarSource(context.Background(), "u-5", "junk", "")
		if !errors.Is(err, ErrInvalidAvatarSource) {
			t.Fatalf("err = %v, want ErrInvalidAvatarSource", err)
		}
	})

	t.Run("uploaded with object key writes both fields", func(t *testing.T) {
		t.Parallel()
		svc, repo := newSvc(&models.User{
			UUID:     "u-6",
			Email:    "u@example.com",
			Role:     "operator",
			IsActive: true,
		})
		prev, err := svc.SetAvatarSource(context.Background(), "u-6", models.AvatarSourceUploaded, "avatars/operator/u-6/new.png")
		if err != nil {
			t.Fatalf("SetAvatarSource: %v", err)
		}
		if prev != "" {
			t.Fatalf("previous = %q, want empty", prev)
		}
		got, _ := repo.GetByID(context.Background(), "u-6")
		if got.AvatarSource != models.AvatarSourceUploaded {
			t.Fatalf("source = %q, want uploaded", got.AvatarSource)
		}
		if got.AvatarObjectKey != "avatars/operator/u-6/new.png" {
			t.Fatalf("objectKey = %q", got.AvatarObjectKey)
		}
	})

	t.Run("missing user surfaces ErrUserNotFound", func(t *testing.T) {
		t.Parallel()
		svc, _ := newSvc(nil)
		_, err := svc.SetAvatarSource(context.Background(), "missing", models.AvatarSourceInitials, "")
		if !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("err = %v, want ErrUserNotFound", err)
		}
	})
}
