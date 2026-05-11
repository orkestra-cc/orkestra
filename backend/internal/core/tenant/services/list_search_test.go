package services

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
)

// fakeAdminListRepo is the test seam for listAllTenantsFiltered — it records
// which dispatch path was taken (search vs plain list) so the routing
// decisions can be asserted without spinning up Mongo.
type fakeAdminListRepo struct {
	listCalls   []repository.TenantListFilter
	searchCalls []repository.TenantListFilter
	countCalls  [][]string

	listResult   []models.Tenant
	listErr      error
	searchResult []repository.TenantSearchResult
	searchErr    error
	countResult  map[string]int
	countErr     error
}

func (f *fakeAdminListRepo) ListTenants(_ context.Context, fl repository.TenantListFilter) ([]models.Tenant, error) {
	f.listCalls = append(f.listCalls, fl)
	return f.listResult, f.listErr
}

func (f *fakeAdminListRepo) SearchTenantsByQ(_ context.Context, fl repository.TenantListFilter) ([]repository.TenantSearchResult, error) {
	f.searchCalls = append(f.searchCalls, fl)
	return f.searchResult, f.searchErr
}

func (f *fakeAdminListRepo) CountMembersByTenants(_ context.Context, ids []string) (map[string]int, error) {
	cp := make([]string, len(ids))
	copy(cp, ids)
	f.countCalls = append(f.countCalls, cp)
	return f.countResult, f.countErr
}

// TestListAllTenantsFiltered_EmptyQ_RoutesToList locks the dispatch contract:
// no Q means the legacy ListTenants + CountMembersByTenants path runs and
// SearchTenantsByQ is never invoked.
func TestListAllTenantsFiltered_EmptyQ_RoutesToList(t *testing.T) {
	t.Parallel()
	repo := &fakeAdminListRepo{
		listResult:  []models.Tenant{{UUID: "t-1", Name: "Acme"}},
		countResult: map[string]int{"t-1": 3},
	}
	views, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{Kind: models.TenantKindExternal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.searchCalls) != 0 {
		t.Fatalf("SearchTenantsByQ called %d times; want 0", len(repo.searchCalls))
	}
	if len(repo.listCalls) != 1 {
		t.Fatalf("ListTenants called %d times; want 1", len(repo.listCalls))
	}
	if len(repo.countCalls) != 1 || !reflect.DeepEqual(repo.countCalls[0], []string{"t-1"}) {
		t.Fatalf("CountMembersByTenants called with %v; want [[t-1]]", repo.countCalls)
	}
	if len(views) != 1 || views[0].MemberCount != 3 || views[0].MatchedMembers != nil {
		t.Fatalf("unexpected view shape: %+v", views)
	}
}

// TestListAllTenantsFiltered_WhitespaceQ_RoutesToList — whitespace-only Q is
// equivalent to no Q. Locks the strings.TrimSpace guard so a literal-space
// query doesn't trigger an empty-regex aggregation that would match every
// row trivially.
func TestListAllTenantsFiltered_WhitespaceQ_RoutesToList(t *testing.T) {
	t.Parallel()
	for _, q := range []string{" ", "\t", "  \n  ", ""} {
		q := q
		t.Run("q="+q, func(t *testing.T) {
			t.Parallel()
			repo := &fakeAdminListRepo{listResult: nil, countResult: map[string]int{}}
			views, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{Q: q})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.searchCalls) != 0 {
				t.Fatalf("SearchTenantsByQ should not be called for whitespace Q=%q", q)
			}
			if len(repo.listCalls) != 1 {
				t.Fatalf("ListTenants call count = %d, want 1", len(repo.listCalls))
			}
			if views == nil {
				t.Fatalf("nil view slice; want empty []TenantAdminView{}")
			}
		})
	}
}

// TestListAllTenantsFiltered_NonEmptyQ_RoutesToSearch locks the
// search-path dispatch: the search aggregation runs, results are projected
// 1:1 into TenantAdminView with MemberCount + MatchedMembers preserved, and
// CountMembersByTenants is NOT additionally consulted (the search path
// already returns a count).
func TestListAllTenantsFiltered_NonEmptyQ_RoutesToSearch(t *testing.T) {
	t.Parallel()
	matched := []repository.MemberMatch{
		{UserUUID: "u-1", Email: "alice@example.com", FullName: "Alice Rossi"},
	}
	repo := &fakeAdminListRepo{
		searchResult: []repository.TenantSearchResult{
			{
				Tenant:         models.Tenant{UUID: "t-1", Name: "Acme"},
				MemberCount:    7,
				MatchedMembers: matched,
			},
		},
	}
	views, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{Q: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.listCalls) != 0 {
		t.Fatalf("ListTenants should not be called when Q is set; got %d calls", len(repo.listCalls))
	}
	if len(repo.countCalls) != 0 {
		t.Fatalf("CountMembersByTenants should not be called on the search path; got %d calls", len(repo.countCalls))
	}
	if len(repo.searchCalls) != 1 {
		t.Fatalf("SearchTenantsByQ call count = %d, want 1", len(repo.searchCalls))
	}
	if repo.searchCalls[0].Q != "alice" {
		t.Fatalf("filter.Q = %q, want %q", repo.searchCalls[0].Q, "alice")
	}
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	if views[0].MemberCount != 7 {
		t.Fatalf("MemberCount = %d, want 7 (search-path count, not list-path count)", views[0].MemberCount)
	}
	if !reflect.DeepEqual(views[0].MatchedMembers, matched) {
		t.Fatalf("MatchedMembers = %+v, want %+v", views[0].MatchedMembers, matched)
	}
}

// TestListAllTenantsFiltered_ListPath_EmptyResult locks the early-return:
// when the list path returns zero tenants, we DON'T call
// CountMembersByTenants (no UUIDs to count) and we DO return a non-nil
// empty slice (so JSON serialization gives [] not null).
func TestListAllTenantsFiltered_ListPath_EmptyResult(t *testing.T) {
	t.Parallel()
	repo := &fakeAdminListRepo{listResult: []models.Tenant{}}
	views, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.countCalls) != 0 {
		t.Fatalf("CountMembersByTenants should not be called for empty result; got %d", len(repo.countCalls))
	}
	if views == nil {
		t.Fatalf("returned nil; want non-nil empty slice")
	}
	if len(views) != 0 {
		t.Fatalf("got %d views, want 0", len(views))
	}
}

// TestListAllTenantsFiltered_ListPath_AttachesCounts verifies the list path
// joins ListTenants output with CountMembersByTenants by UUID.
func TestListAllTenantsFiltered_ListPath_AttachesCounts(t *testing.T) {
	t.Parallel()
	repo := &fakeAdminListRepo{
		listResult: []models.Tenant{
			{UUID: "t-a", Name: "Alpha"},
			{UUID: "t-b", Name: "Beta"},
			{UUID: "t-c", Name: "Gamma"},
		},
		countResult: map[string]int{"t-a": 1, "t-c": 9},
	}
	views, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("got %d views, want 3", len(views))
	}
	want := map[string]int{"t-a": 1, "t-b": 0, "t-c": 9}
	for _, v := range views {
		if got := v.MemberCount; got != want[v.Tenant.UUID] {
			t.Fatalf("tenant %s: MemberCount = %d, want %d", v.Tenant.UUID, got, want[v.Tenant.UUID])
		}
	}
}

// TestListAllTenantsFiltered_PropagatesErrors locks error handling on both
// dispatch paths so a Mongo failure surfaces as-is rather than being
// silently swallowed.
func TestListAllTenantsFiltered_PropagatesErrors(t *testing.T) {
	t.Parallel()
	t.Run("search error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("aggregation exploded")
		repo := &fakeAdminListRepo{searchErr: boom}
		_, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{Q: "x"})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
	t.Run("list error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("find exploded")
		repo := &fakeAdminListRepo{listErr: boom}
		_, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
	t.Run("count error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("count exploded")
		repo := &fakeAdminListRepo{
			listResult: []models.Tenant{{UUID: "t-1"}},
			countErr:   boom,
		}
		_, err := listAllTenantsFiltered(context.Background(), repo, repository.TenantListFilter{})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
}
