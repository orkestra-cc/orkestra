package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
)

// fakeLegalHoldRepo is a hand-rolled stub of legalHoldRepo. Each method
// records its inputs and returns a pinned value so the service logic is
// exercised without MongoDB.
type fakeLegalHoldRepo struct {
	inserted     *models.LegalHold
	insertErr    error
	releaseErr   error
	listResult   []models.LegalHold
	listErr      error
	held         bool
	heldErr      error
	lastRelease  [3]string // uuid, releasedBy, reason
	lastListUUID string
	lastHeldUUID string
}

func (f *fakeLegalHoldRepo) Insert(_ context.Context, h *models.LegalHold) error {
	f.inserted = h
	return f.insertErr
}
func (f *fakeLegalHoldRepo) Release(_ context.Context, uuid, releasedBy, reason string) error {
	f.lastRelease = [3]string{uuid, releasedBy, reason}
	return f.releaseErr
}
func (f *fakeLegalHoldRepo) ListActive(_ context.Context, userUUID string) ([]models.LegalHold, error) {
	f.lastListUUID = userUUID
	return f.listResult, f.listErr
}
func (f *fakeLegalHoldRepo) IsHeld(_ context.Context, userUUID string) (bool, error) {
	f.lastHeldUUID = userUUID
	return f.held, f.heldErr
}

// TestPlacePopulatesActiveHold pins the Place contract: the service stamps a
// UUID + PlacedAt, marks the hold active, and forwards every operator-supplied
// field to the repo verbatim.
func TestPlacePopulatesActiveHold(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{}
	svc := NewLegalHoldService(repo)

	in := PlaceInput{
		UserUUID: "u-1",
		TenantID: "t-1",
		Reason:   "litigation",
		CaseRef:  "CASE-42",
		PlacedBy: "admin-9",
	}
	got, err := svc.Place(context.Background(), in)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if repo.inserted == nil {
		t.Fatal("Place should have inserted a hold")
	}
	if got.UUID == "" {
		t.Fatal("Place should stamp a UUID")
	}
	if !got.Active {
		t.Fatal("a freshly placed hold must be active")
	}
	if got.PlacedAt.IsZero() {
		t.Fatal("Place should stamp PlacedAt")
	}
	if got.UserUUID != "u-1" || got.TenantID != "t-1" || got.Reason != "litigation" ||
		got.CaseRef != "CASE-42" || got.PlacedBy != "admin-9" {
		t.Fatalf("placed hold fields not forwarded verbatim: %+v", got)
	}
}

// TestPlacePropagatesInsertError pins that a repo insert failure surfaces to
// the caller (and no hold object is returned).
func TestPlacePropagatesInsertError(t *testing.T) {
	t.Parallel()

	boom := errors.New("insert failed")
	svc := NewLegalHoldService(&fakeLegalHoldRepo{insertErr: boom})
	got, err := svc.Place(context.Background(), PlaceInput{UserUUID: "u-1", Reason: "x"})
	if !errors.Is(err, boom) {
		t.Fatalf("expected insert error to propagate, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil hold on insert error, got %+v", got)
	}
}

// TestReleaseForwardsArgs pins the pass-through to repo.Release.
func TestReleaseForwardsArgs(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{}
	svc := NewLegalHoldService(repo)
	if err := svc.Release(context.Background(), "h-1", "admin-9", "case closed"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if repo.lastRelease != [3]string{"h-1", "admin-9", "case closed"} {
		t.Fatalf("Release args not forwarded: %+v", repo.lastRelease)
	}
}

// TestListActiveForwardsFilter pins that ListActive passes the subject filter
// through and returns the repo rows unchanged.
func TestListActiveForwardsFilter(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{listResult: []models.LegalHold{{UUID: "h-1"}, {UUID: "h-2"}}}
	svc := NewLegalHoldService(repo)
	got, err := svc.ListActive(context.Background(), "u-7")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if repo.lastListUUID != "u-7" {
		t.Fatalf("ListActive filter not forwarded: %q", repo.lastListUUID)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 holds, got %d", len(got))
	}
}

// TestIsHeldForwards pins the DSR-gate query: IsHeld delegates to the repo and
// returns its verdict for the right subject.
func TestIsHeldForwards(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{held: true}
	svc := NewLegalHoldService(repo)
	held, err := svc.IsHeld(context.Background(), "u-3")
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if !held {
		t.Fatal("IsHeld should report the held subject")
	}
	if repo.lastHeldUUID != "u-3" {
		t.Fatalf("IsHeld subject not forwarded: %q", repo.lastHeldUUID)
	}
}
