package handlers

import (
	"context"
	"testing"

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
func (f *fakeHoldRepo) Release(_ context.Context, _, _, _ string) error     { return f.releaseErr }
func (f *fakeHoldRepo) ListActive(_ context.Context, _ string) ([]models.LegalHold, error) {
	return f.listResult, nil
}
func (f *fakeHoldRepo) IsHeld(_ context.Context, _ string) (bool, error) { return false, nil }

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
