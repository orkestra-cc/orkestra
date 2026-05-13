package services

import (
	"context"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

// inMemoryRefreshRepo is a hand-rolled fake that implements the narrow
// subset of RefreshTokenRepository we need for rotation-lineage tests.
// It mirrors the semantics of the real Mongo implementation — the CAS on
// `{isRevoked:false}` is modelled by a quick check under the mutex — so
// the tests reflect real-world behaviour without needing a live database.
type inMemoryRefreshRepo struct {
	mu     sync.Mutex
	byHash map[string]*authModels.RefreshTokenDoc
}

func newInMemoryRefreshRepo() *inMemoryRefreshRepo {
	return &inMemoryRefreshRepo{byHash: map[string]*authModels.RefreshTokenDoc{}}
}

func (r *inMemoryRefreshRepo) insert(tokenHash string, doc *authModels.RefreshTokenDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *doc
	copy.Token = tokenHash
	r.byHash[tokenHash] = &copy
}

func (r *inMemoryRefreshRepo) GetByTokenAny(_ context.Context, tokenHash string) (*authModels.RefreshTokenDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.byHash[tokenHash]; ok {
		c := *d
		return &c, nil
	}
	return nil, nil
}

func (r *inMemoryRefreshRepo) RotateWithFamily(_ context.Context, oldHash string, newDoc *authModels.RefreshTokenDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.byHash[oldHash]
	if !ok || old.IsRevoked {
		return repository.ErrTokenAlreadyRotated
	}
	now := time.Now()
	old.IsRevoked = true
	old.RevokedAt = &now
	old.RevokedReason = authModels.RevokeReasonRotated
	old.SucceededBy = newDoc.UUID

	copy := *newDoc
	// Mirror the real repo: caller-supplied Token is a raw token string
	// that the repo hashes before storing. For the test we accept whatever
	// the caller passes — tests key on the FamilyID / SucceededBy chain,
	// not the specific hash.
	r.byHash[newDoc.Token] = &copy
	return nil
}

func (r *inMemoryRefreshRepo) RevokeFamily(_ context.Context, familyID, reason string) (int64, error) {
	if familyID == "" {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var n int64
	for _, d := range r.byHash {
		if d.FamilyID == familyID && !d.IsRevoked {
			d.IsRevoked = true
			d.RevokedAt = &now
			d.RevokedReason = reason
			n++
		}
	}
	return n, nil
}

func (r *inMemoryRefreshRepo) CountFamilyMembers(_ context.Context, familyID string) (int64, error) {
	if familyID == "" {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, d := range r.byHash {
		if d.FamilyID == familyID {
			n++
		}
	}
	return n, nil
}

// --- Tests ---

func TestRotateWithFamilyHappyPath(t *testing.T) {
	ctx := context.Background()
	repo := newInMemoryRefreshRepo()

	oldHash := "hash-A"
	family := "fam-1"
	repo.insert(oldHash, &authModels.RefreshTokenDoc{
		UUID:      "A",
		UserUUID:  "u-1",
		FamilyID:  family,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	newDoc := &authModels.RefreshTokenDoc{
		UUID:     "B",
		UserUUID: "u-1",
		FamilyID: family,
		Token:    "hash-B",
	}
	if err := repo.RotateWithFamily(ctx, oldHash, newDoc); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	old, _ := repo.GetByTokenAny(ctx, oldHash)
	if !old.IsRevoked || old.RevokedReason != authModels.RevokeReasonRotated {
		t.Fatalf("old not rotated: %+v", old)
	}
	if old.SucceededBy != "B" {
		t.Fatalf("SucceededBy not set: got %q", old.SucceededBy)
	}

	newer, _ := repo.GetByTokenAny(ctx, "hash-B")
	if newer == nil || newer.IsRevoked || newer.FamilyID != family {
		t.Fatalf("new row not inserted correctly: %+v", newer)
	}

	n, _ := repo.CountFamilyMembers(ctx, family)
	if n != 2 {
		t.Fatalf("family count: got %d want 2", n)
	}
}

func TestRotateWithFamilyChained(t *testing.T) {
	ctx := context.Background()
	repo := newInMemoryRefreshRepo()

	family := "fam-chain"
	repo.insert("h-A", &authModels.RefreshTokenDoc{UUID: "A", FamilyID: family, ExpiresAt: time.Now().Add(time.Hour)})

	hashes := []string{"h-A", "h-B", "h-C"}
	for i, h := range hashes {
		nextUUID := string(rune('B' + i))
		next := &authModels.RefreshTokenDoc{UUID: nextUUID, FamilyID: family, Token: "h-" + nextUUID, ExpiresAt: time.Now().Add(time.Hour)}
		if err := repo.RotateWithFamily(ctx, h, next); err != nil {
			t.Fatalf("rotate step %d: %v", i, err)
		}
	}

	n, _ := repo.CountFamilyMembers(ctx, family)
	if n != 4 {
		t.Fatalf("family should have 4 members, got %d", n)
	}
	// Every non-terminal row should have SucceededBy set.
	for _, h := range hashes {
		d, _ := repo.GetByTokenAny(ctx, h)
		if d.SucceededBy == "" {
			t.Fatalf("row %q has no SucceededBy", h)
		}
	}
}

func TestRotateWithFamilyReplayDetected(t *testing.T) {
	ctx := context.Background()
	repo := newInMemoryRefreshRepo()

	family := "fam-replay"
	repo.insert("h-A", &authModels.RefreshTokenDoc{UUID: "A", FamilyID: family, ExpiresAt: time.Now().Add(time.Hour)})

	// First rotation succeeds.
	if err := repo.RotateWithFamily(ctx, "h-A", &authModels.RefreshTokenDoc{UUID: "B", FamilyID: family, Token: "h-B", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("first rotate: %v", err)
	}

	// Replay of A → CAS fails.
	err := repo.RotateWithFamily(ctx, "h-A", &authModels.RefreshTokenDoc{UUID: "B2", FamilyID: family, Token: "h-B2", ExpiresAt: time.Now().Add(time.Hour)})
	if err != repository.ErrTokenAlreadyRotated {
		t.Fatalf("expected ErrTokenAlreadyRotated, got %v", err)
	}

	// In the real code path the caller now calls RevokeFamily — verify
	// that every member is marked revoked with replay reason.
	n, err := repo.RevokeFamily(ctx, family, authModels.RevokeReasonReplayDetected)
	if err != nil {
		t.Fatalf("revoke family: %v", err)
	}
	if n != 1 {
		t.Fatalf("should have revoked exactly 1 still-active row (B), got %d", n)
	}
	b, _ := repo.GetByTokenAny(ctx, "h-B")
	if !b.IsRevoked || b.RevokedReason != authModels.RevokeReasonReplayDetected {
		t.Fatalf("B not revoked with replay reason: %+v", b)
	}
}

func TestRevokeFamilyEmptyIDIsNoop(t *testing.T) {
	// Pre-Block-C rows have FamilyID="". Revoking with empty must not
	// sweep every such row — guard against the catastrophic case where a
	// single stale row triggers wholesale revocation of unrelated sessions.
	ctx := context.Background()
	repo := newInMemoryRefreshRepo()
	repo.insert("h-X", &authModels.RefreshTokenDoc{UUID: "X", FamilyID: "", ExpiresAt: time.Now().Add(time.Hour)})
	repo.insert("h-Y", &authModels.RefreshTokenDoc{UUID: "Y", FamilyID: "", ExpiresAt: time.Now().Add(time.Hour)})

	n, err := repo.RevokeFamily(ctx, "", authModels.RevokeReasonReplayDetected)
	if err != nil {
		t.Fatalf("revoke family: %v", err)
	}
	if n != 0 {
		t.Fatalf("empty-family revoke must be a no-op, revoked %d", n)
	}
	for _, h := range []string{"h-X", "h-Y"} {
		d, _ := repo.GetByTokenAny(ctx, h)
		if d.IsRevoked {
			t.Fatalf("row %q should remain active", h)
		}
	}
}

func TestRevokeFamilySweepsOnlyThatFamily(t *testing.T) {
	ctx := context.Background()
	repo := newInMemoryRefreshRepo()
	repo.insert("h-1a", &authModels.RefreshTokenDoc{UUID: "1a", FamilyID: "fam-1", ExpiresAt: time.Now().Add(time.Hour)})
	repo.insert("h-1b", &authModels.RefreshTokenDoc{UUID: "1b", FamilyID: "fam-1", ExpiresAt: time.Now().Add(time.Hour)})
	repo.insert("h-2a", &authModels.RefreshTokenDoc{UUID: "2a", FamilyID: "fam-2", ExpiresAt: time.Now().Add(time.Hour)})

	n, _ := repo.RevokeFamily(ctx, "fam-1", authModels.RevokeReasonLogout)
	if n != 2 {
		t.Fatalf("should revoke 2 rows, got %d", n)
	}
	two, _ := repo.GetByTokenAny(ctx, "h-2a")
	if two.IsRevoked {
		t.Fatalf("fam-2 must remain untouched")
	}
}
