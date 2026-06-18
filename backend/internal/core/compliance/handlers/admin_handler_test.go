package handlers

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
)

// fakeAuditRepo captures the Filter the handler builds and returns a pinned
// page so the mapping + pagination logic is exercised without MongoDB.
type fakeAuditRepo struct {
	lastFilter repository.Filter
	items      []models.AuditEvent
	total      int64
}

func (f *fakeAuditRepo) List(_ context.Context, fl repository.Filter) ([]models.AuditEvent, int64, error) {
	f.lastFilter = fl
	return f.items, f.total, nil
}

func TestAdminListInvalidSinceIs400(t *testing.T) {
	t.Parallel()
	h := New(&fakeAuditRepo{})
	_, err := h.List(context.Background(), &ListAuditEventsInput{Since: "not-a-date", Limit: 50})
	assertStatus(t, err, 400)
}

func TestAdminListInvalidUntilIs400(t *testing.T) {
	t.Parallel()
	h := New(&fakeAuditRepo{})
	_, err := h.List(context.Background(), &ListAuditEventsInput{Until: "13:00 yesterday", Limit: 50})
	assertStatus(t, err, 400)
}

// TestAdminListMapsFiltersAndPaginates pins the query→Filter mapping, the
// RFC3339 date parse, and the offset/limit echo on the response.
func TestAdminListMapsFiltersAndPaginates(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{
		items: []models.AuditEvent{{UUID: "e-1"}},
		total: 42,
	}
	h := New(repo)

	in := &ListAuditEventsInput{
		TenantID:     "t-1",
		ActionPrefix: "auth.",
		Outcome:      "denied",
		Since:        "2026-01-01T00:00:00Z",
		Until:        "2026-02-01T00:00:00Z",
		Limit:        25,
		Offset:       100,
	}
	out, err := h.List(context.Background(), in)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if repo.lastFilter.TenantID != "t-1" || repo.lastFilter.ActionPrefix != "auth." ||
		repo.lastFilter.Outcome != "denied" {
		t.Fatalf("filter fields not mapped: %+v", repo.lastFilter)
	}
	if repo.lastFilter.Limit != 25 || repo.lastFilter.Skip != 100 {
		t.Fatalf("pagination not mapped: limit=%d skip=%d", repo.lastFilter.Limit, repo.lastFilter.Skip)
	}
	if repo.lastFilter.Since.IsZero() || repo.lastFilter.Until.IsZero() {
		t.Fatalf("date bounds not parsed into filter: %+v", repo.lastFilter)
	}
	if out.Body.Total != 42 || out.Body.Limit != 25 || out.Body.Offset != 100 {
		t.Fatalf("response envelope wrong: %+v", out.Body)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Body.Items))
	}
}
