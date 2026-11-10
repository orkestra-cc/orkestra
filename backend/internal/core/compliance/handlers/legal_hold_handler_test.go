package handlers

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/testkit"
)

// fakeHoldRepo structurally satisfies the services legal-hold repo seam.
type fakeHoldRepo struct {
	listResult []models.LegalHold
	releaseErr error
}

func (f *fakeHoldRepo) Insert(_ context.Context, _ *models.LegalHold) error { return nil }
func (f *fakeHoldRepo) Release(_ context.Context, _, _, _ string, _ time.Time) error {
	return f.releaseErr
}
func (f *fakeHoldRepo) ListActive(_ context.Context, _ string, _ time.Time) ([]models.LegalHold, error) {
	return f.listResult, nil
}
func (f *fakeHoldRepo) List(_ context.Context, _ string, _ bool, _ time.Time) ([]models.LegalHold, error) {
	return f.listResult, nil
}
func (f *fakeHoldRepo) IsHeld(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (f *fakeHoldRepo) Get(_ context.Context, _ string) (*models.LegalHold, error) {
	return &models.LegalHold{UUID: "h-1"}, nil
}
func (f *fakeHoldRepo) Update(_ context.Context, id string, _ bson.M, _ []string, _ time.Time) (*models.LegalHold, error) {
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	return &models.LegalHold{UUID: id, Active: true}, nil
}
func (f *fakeHoldRepo) AddNote(_ context.Context, id string, n models.LegalHoldNote, _ time.Time) (*models.LegalHold, error) {
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	return &models.LegalHold{UUID: id, Active: true, Notes: []models.LegalHoldNote{n}}, nil
}
func (f *fakeHoldRepo) ExpireDue(_ context.Context, _ time.Time, _ int) ([]models.LegalHold, error) {
	return nil, nil
}

func newLegalHoldHandler(repo *fakeHoldRepo) *LegalHoldHandler {
	return NewLegalHoldHandler(services.NewLegalHoldService(repo))
}

func adminTenantCtx(actor, tenantID string) context.Context {
	return testkit.NewIdentity(actor, actor+"@example.com", "administrator").
		WithTenant(tenantID, []string{"administrator"}, true).
		ContextFor(context.Background(), tenantID)
}

func TestLegalHoldListReturnsItems(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{listResult: []models.LegalHold{{UUID: "h-1"}, {UUID: "h-2"}}})
	out, err := h.List(context.Background(), &ListLegalHoldsInput{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("expected 2 holds, got %d", len(out.Body.Items))
	}
}

func TestLegalHoldPlaceValidation(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	in := &PlaceLegalHoldInput{}
	in.Body.UserUUID = ""
	in.Body.Reason = ""
	_, err := h.Place(adminTenantCtx("admin-1", "t-1"), in)
	assertStatus(t, err, 422)
}

// TestLegalHoldPlaceForwardsActorAndTenant pins that the handler pulls the
// actor + tenant from the auth context onto the placed hold.
func TestLegalHoldPlaceForwardsActorAndTenant(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	in := &PlaceLegalHoldInput{}
	in.Body.UserUUID = "subject-9"
	in.Body.Reason = "litigation"
	out, err := h.Place(adminTenantCtx("admin-1", "t-1"), in)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if out.Body.PlacedBy != "admin-1" {
		t.Fatalf("PlacedBy = %q; want admin-1 (from auth ctx)", out.Body.PlacedBy)
	}
	if out.Body.TenantID != "t-1" {
		t.Fatalf("TenantID = %q; want t-1 (from auth ctx)", out.Body.TenantID)
	}
	if out.Body.UserUUID != "subject-9" || !out.Body.Active {
		t.Fatalf("placed hold wrong: %+v", out.Body)
	}
}

func TestLegalHoldReleaseNotFoundIs404(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{releaseErr: repository.ErrLegalHoldNotFound})
	in := &ReleaseLegalHoldInput{ID: "missing"}
	in.Body.ReleaseReason = "closed"
	_, err := h.Release(adminTenantCtx("admin-1", "t-1"), in)
	assertStatus(t, err, 404)
}

func TestLegalHoldReleaseSuccess(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	in := &ReleaseLegalHoldInput{ID: "h-1"}
	in.Body.ReleaseReason = "case closed"
	if _, err := h.Release(adminTenantCtx("admin-1", "t-1"), in); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestLegalHoldReleaseBlankReasonIs422(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	_, err := h.Release(adminTenantCtx("admin-1", "t-1"), &ReleaseLegalHoldInput{ID: "h-1"})
	assertStatus(t, err, 422)
}

func TestLegalHoldPlacePastExpiryIs422(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	in := &PlaceLegalHoldInput{}
	in.Body.UserUUID = "u"
	in.Body.Reason = "r"
	past := time.Now().Add(-time.Hour)
	in.Body.ExpiresAt = &past
	_, err := h.Place(adminTenantCtx("admin-1", "t-1"), in)
	assertStatus(t, err, 422)
}

func TestLegalHoldUpdate(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	ctx := adminTenantCtx("admin-1", "t-1")
	_, err := h.Update(ctx, &UpdateLegalHoldInput{ID: "h-1"})
	assertStatus(t, err, 422) // nothing to change

	in := &UpdateLegalHoldInput{ID: "h-1"}
	soon := time.Now().Add(time.Hour)
	in.Body.ExpiresAt = &soon
	out, err := h.Update(ctx, in)
	if err != nil || out.Body.UUID != "h-1" {
		t.Fatalf("Update = %+v, %v", out, err)
	}

	nf := newLegalHoldHandler(&fakeHoldRepo{releaseErr: repository.ErrLegalHoldNotFound})
	_, err = nf.Update(ctx, in)
	assertStatus(t, err, 404)
}

func TestLegalHoldAddNote(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{})
	ctx := adminTenantCtx("admin-1", "t-1")
	_, err := h.AddNote(ctx, &AddLegalHoldNoteInput{ID: "h-1"})
	assertStatus(t, err, 422)
	in := &AddLegalHoldNoteInput{ID: "h-1"}
	in.Body.Text = "counsel asked to keep"
	out, err := h.AddNote(ctx, in)
	if err != nil || len(out.Body.Notes) != 1 || out.Body.Notes[0].By != "admin-1" {
		t.Fatalf("AddNote = %+v, %v", out, err)
	}
}

// A lapsed hold (repo reports not found for Update/AddNote) surfaces as 404.
func TestLegalHoldLapsedUpdateAndNoteAre404(t *testing.T) {
	t.Parallel()
	h := newLegalHoldHandler(&fakeHoldRepo{releaseErr: repository.ErrLegalHoldNotFound})
	ctx := adminTenantCtx("admin-1", "t-1")
	in := &UpdateLegalHoldInput{ID: "h-1"}
	in.Body.ClearExpiresAt = true
	_, err := h.Update(ctx, in)
	assertStatus(t, err, 404)
	n := &AddLegalHoldNoteInput{ID: "h-1"}
	n.Body.Text = "late"
	_, err = h.AddNote(ctx, n)
	assertStatus(t, err, 404)
}
