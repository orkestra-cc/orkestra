package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
)

var ErrUnsubscribeTokenInvalid = errors.New("notification: unsubscribe token invalid or expired")

// UnsubscribeService manages the creation and consumption of per-email
// unsubscribe tokens that back the mandatory footer link.
type UnsubscribeService interface {
	// IssueToken creates a new unsubscribe token bound to an email address
	// (and optionally a user + category) and returns the raw token string
	// to embed in the unsubscribe URL. context is an opaque producer string
	// (e.g. a campaign/run ref) stored on the token and handed back to a
	// MarketingUnsubscribeSink on consume; pass "" when not needed.
	IssueToken(ctx context.Context, userUUID, address, category, context string) (string, error)

	// ConsumeToken verifies a raw token and returns the stored document.
	// The caller is expected to apply the preference change (or
	// suppression) and then call MarkUsed.
	ConsumeToken(ctx context.Context, raw string) (*models.UnsubscribeTokenDoc, error)

	// MarkUsed flags the token as consumed.
	MarkUsed(ctx context.Context, raw string) error
}

type unsubscribeService struct {
	repo repository.UnsubscribeRepository
	ttl  time.Duration
}

func NewUnsubscribeService(repo repository.UnsubscribeRepository) UnsubscribeService {
	return &unsubscribeService{
		repo: repo,
		ttl:  30 * 24 * time.Hour,
	}
}

func (s *unsubscribeService) IssueToken(ctx context.Context, userUUID, address, category, context string) (string, error) {
	raw, err := generateRandomToken(32)
	if err != nil {
		return "", err
	}
	doc := &models.UnsubscribeTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		TokenHash: hashToken(raw),
		UserUUID:  userUUID,
		Address:   address,
		Category:  category,
		Context:   context,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
	}
	if err := s.repo.Create(ctx, doc); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *unsubscribeService) ConsumeToken(ctx context.Context, raw string) (*models.UnsubscribeTokenDoc, error) {
	if raw == "" {
		return nil, ErrUnsubscribeTokenInvalid
	}
	doc, err := s.repo.GetByHash(ctx, hashToken(raw))
	if err != nil {
		return nil, ErrUnsubscribeTokenInvalid
	}
	if doc.UsedAt != nil {
		return nil, ErrUnsubscribeTokenInvalid
	}
	if time.Now().After(doc.ExpiresAt) {
		return nil, ErrUnsubscribeTokenInvalid
	}
	return doc, nil
}

func (s *unsubscribeService) MarkUsed(ctx context.Context, raw string) error {
	return s.repo.MarkUsed(ctx, hashToken(raw))
}

// generateRandomToken returns a URL-safe random string backed by n bytes.
func generateRandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken returns a hex sha256 of the raw token for database lookup.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
