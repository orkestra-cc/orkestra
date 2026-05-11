package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
)

type fakeUnsubRepo struct {
	docs      map[string]*models.UnsubscribeTokenDoc // by tokenHash
	createErr error
	getErr    error
	markErr   error
	createN   int
	markN     int
}

func newFakeUnsubRepo() *fakeUnsubRepo {
	return &fakeUnsubRepo{docs: map[string]*models.UnsubscribeTokenDoc{}}
}

func (f *fakeUnsubRepo) Create(_ context.Context, doc *models.UnsubscribeTokenDoc) error {
	f.createN++
	if f.createErr != nil {
		return f.createErr
	}
	cp := *doc
	f.docs[doc.TokenHash] = &cp
	return nil
}

func (f *fakeUnsubRepo) GetByHash(_ context.Context, hash string) (*models.UnsubscribeTokenDoc, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	doc, ok := f.docs[hash]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return doc, nil
}

func (f *fakeUnsubRepo) MarkUsed(_ context.Context, hash string) error {
	f.markN++
	if f.markErr != nil {
		return f.markErr
	}
	doc, ok := f.docs[hash]
	if !ok {
		return repository.ErrNotFound
	}
	now := time.Now()
	doc.UsedAt = &now
	return nil
}

func TestUnsubscribeService_IssueToken_StoresHashAndReturnsRaw(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)

	raw, err := svc.IssueToken(context.Background(), "user-1", "alice@example.com", models.CategoryAuthVerifyEmail)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if raw == "" {
		t.Fatalf("expected raw token, got empty string")
	}
	if repo.createN != 1 {
		t.Fatalf("expected one Create call, got %d", repo.createN)
	}
	stored := repo.docs[hashToken(raw)]
	if stored == nil {
		t.Fatalf("stored doc not found under hashed key")
	}
	if stored.UserUUID != "user-1" || stored.Address != "alice@example.com" || stored.Category != models.CategoryAuthVerifyEmail {
		t.Fatalf("stored doc mismatch: %+v", stored)
	}
	if stored.UUID == "" {
		t.Fatalf("expected UUID on stored doc")
	}
	// Sanity: raw is never persisted in plaintext.
	for _, d := range repo.docs {
		if d.TokenHash == raw {
			t.Fatalf("token hash matched raw token — should be hashed")
		}
	}
	// Default TTL is 30 days; expiresAt is in the future and within bounds.
	if !stored.ExpiresAt.After(time.Now()) {
		t.Fatalf("ExpiresAt should be in the future, got %v", stored.ExpiresAt)
	}
	if stored.ExpiresAt.Sub(stored.CreatedAt) < 29*24*time.Hour {
		t.Fatalf("expected ~30-day TTL, got %v", stored.ExpiresAt.Sub(stored.CreatedAt))
	}
}

func TestUnsubscribeService_IssueToken_PropagatesRepoError(t *testing.T) {
	repo := newFakeUnsubRepo()
	repo.createErr = errors.New("boom")
	svc := NewUnsubscribeService(repo)

	raw, err := svc.IssueToken(context.Background(), "", "addr@example.com", "")
	if err == nil {
		t.Fatalf("expected error from Create")
	}
	if raw != "" {
		t.Fatalf("expected empty raw on error, got %q", raw)
	}
}

func TestUnsubscribeService_IssueToken_TokensAreUnique(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)

	seen := map[string]struct{}{}
	for i := 0; i < 20; i++ {
		raw, err := svc.IssueToken(context.Background(), "u", "a@example.com", "c")
		if err != nil {
			t.Fatalf("IssueToken: %v", err)
		}
		if _, dup := seen[raw]; dup {
			t.Fatalf("duplicate token issued: %s", raw)
		}
		seen[raw] = struct{}{}
	}
}

func TestUnsubscribeService_ConsumeToken_Empty(t *testing.T) {
	svc := NewUnsubscribeService(newFakeUnsubRepo())
	_, err := svc.ConsumeToken(context.Background(), "")
	if !errors.Is(err, ErrUnsubscribeTokenInvalid) {
		t.Fatalf("expected ErrUnsubscribeTokenInvalid, got %v", err)
	}
}

func TestUnsubscribeService_ConsumeToken_NotFound(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)
	_, err := svc.ConsumeToken(context.Background(), "no-such-token")
	if !errors.Is(err, ErrUnsubscribeTokenInvalid) {
		t.Fatalf("expected ErrUnsubscribeTokenInvalid for missing token, got %v", err)
	}
}

func TestUnsubscribeService_ConsumeToken_AlreadyUsed(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)
	raw, err := svc.IssueToken(context.Background(), "u", "a@example.com", "c")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	used := time.Now()
	repo.docs[hashToken(raw)].UsedAt = &used

	_, err = svc.ConsumeToken(context.Background(), raw)
	if !errors.Is(err, ErrUnsubscribeTokenInvalid) {
		t.Fatalf("expected ErrUnsubscribeTokenInvalid on used token, got %v", err)
	}
}

func TestUnsubscribeService_ConsumeToken_Expired(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)
	raw, err := svc.IssueToken(context.Background(), "u", "a@example.com", "c")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	repo.docs[hashToken(raw)].ExpiresAt = time.Now().Add(-1 * time.Minute)

	_, err = svc.ConsumeToken(context.Background(), raw)
	if !errors.Is(err, ErrUnsubscribeTokenInvalid) {
		t.Fatalf("expected ErrUnsubscribeTokenInvalid on expired token, got %v", err)
	}
}

func TestUnsubscribeService_ConsumeToken_HappyPath(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)
	raw, err := svc.IssueToken(context.Background(), "user-7", "z@example.com", models.CategoryAuthVerifyEmail)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	doc, err := svc.ConsumeToken(context.Background(), raw)
	if err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}
	if doc.UserUUID != "user-7" {
		t.Fatalf("doc UserUUID = %q, want user-7", doc.UserUUID)
	}
	if doc.Category != models.CategoryAuthVerifyEmail {
		t.Fatalf("doc Category = %q, want %q", doc.Category, models.CategoryAuthVerifyEmail)
	}
}

func TestUnsubscribeService_MarkUsed_StampsTimestamp(t *testing.T) {
	repo := newFakeUnsubRepo()
	svc := NewUnsubscribeService(repo)
	raw, err := svc.IssueToken(context.Background(), "u", "a@example.com", "c")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if err := svc.MarkUsed(context.Background(), raw); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	stored := repo.docs[hashToken(raw)]
	if stored.UsedAt == nil {
		t.Fatalf("expected UsedAt to be set")
	}
}

func TestUnsubscribeService_MarkUsed_PropagatesRepoError(t *testing.T) {
	repo := newFakeUnsubRepo()
	repo.markErr = errors.New("write failed")
	svc := NewUnsubscribeService(repo)
	if err := svc.MarkUsed(context.Background(), "anything"); err == nil {
		t.Fatalf("expected error from MarkUsed")
	}
}

func TestHashToken_DeterministicAndHex(t *testing.T) {
	h1 := hashToken("hello")
	h2 := hashToken("hello")
	if h1 != h2 {
		t.Fatalf("hashToken should be deterministic, got %q vs %q", h1, h2)
	}
	if len(h1) != 64 { // sha256 hex
		t.Fatalf("expected 64-char hex digest, got %d (%q)", len(h1), h1)
	}
	for _, c := range h1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex character %q in digest %q", c, h1)
		}
	}
	if hashToken("hello") == hashToken("world") {
		t.Fatalf("different inputs should produce different digests")
	}
}

func TestGenerateRandomToken_Length(t *testing.T) {
	// 32 random bytes → base64-RawURLEncoding → 43 chars.
	raw, err := generateRandomToken(32)
	if err != nil {
		t.Fatalf("generateRandomToken: %v", err)
	}
	if len(raw) != 43 {
		t.Fatalf("expected 43-char base64 token, got %d (%q)", len(raw), raw)
	}
}
