package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestService_ImplementsSearchContracts(t *testing.T) {
	var _ iface.RetrievalSearch = (*Service)(nil)
	var _ iface.RetrievalPrincipalResolver = (*Service)(nil)
}

func TestService_SearchWithoutSearcherIsNotConfigured(t *testing.T) {
	svc := New(Options{})
	res, err := svc.Search(context.Background(), contactsReq())
	requireIs(t, err, iface.ErrRetrievalNotConfigured)
	assert.Nil(t, res)
}

func TestService_DiagnoseWithoutSearcherNotConfigured(t *testing.T) {
	res, err := New(Options{}).Diagnose(context.Background(), contactsReq())
	requireIs(t, err, iface.ErrRetrievalNotConfigured)
	assert.Nil(t, res)
}

func TestService_DiagnoseDelegates(t *testing.T) {
	fx := newSearchFixture(t)
	svc := New(Options{})
	svc.SetSearch(NewSearcher(fx.deps()), nil)
	res, err := svc.Diagnose(fx.ctx, contactsReq())
	require.NoError(t, err)
	assert.Len(t, res.Result.Hits, 3)
	assert.Len(t, res.Scores, 3)
	assert.Equal(t, true, fx.lastQuery().Diagnostics)
	assert.Len(t, fx.log.rows, 1)
}

func TestService_PrincipalsWithoutResolverFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	pc, err := New(Options{}).Principals(fx.ctx)
	requireIs(t, err, iface.ErrRetrievalForbidden)
	assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
}

func TestService_SetSearchDelegates(t *testing.T) {
	fx := newSearchFixture(t)
	svc := New(Options{})
	svc.SetSearch(NewSearcher(fx.deps()), &PrincipalResolver{
		Members: searchMembers{member: true},
		Roles:   searchRoles{ids: []string{"r-1"}},
	})

	res, err := svc.Search(fx.ctx, contactsReq())
	require.NoError(t, err)
	assert.Len(t, res.Hits, 3)
	assert.Len(t, fx.log.rows, 1)

	pc, err := svc.Principals(fx.ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"role:r-1", "tenant:*", "user:u-1"}, pc.Principals)

	// FilterSchema stays PR 3's registry method (P13).
	_, err = svc.FilterSchema("contacts")
	requireIs(t, err, iface.ErrRetrievalUnknownCorpus)
}

// A nil searcher or resolver passed to SetSearch keeps failing closed.
func TestService_SetSearchWithNilsFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	svc := New(Options{})
	svc.SetSearch(nil, nil)
	_, err := svc.Search(fx.ctx, contactsReq())
	requireIs(t, err, iface.ErrRetrievalNotConfigured)
	_, err = svc.Principals(fx.ctx)
	requireIs(t, err, iface.ErrRetrievalForbidden)
}

// Service.Principals crosses the SDK boundary: it maps the resolver's errors
// like search step 3 does, so no contributor text reaches a consumer.
func TestService_PrincipalsMapsResolverErrors(t *testing.T) {
	boom := errors.New("teams: team:secret-team-42 lookup failed")
	cases := map[string]struct {
		resolver *PrincipalResolver
		sentinel error // also in the chain, nil: none
	}{
		"contributor error": {resolver: &PrincipalResolver{
			Members:      searchMembers{member: true},
			Roles:        searchRoles{},
			Contributors: contribs(searchContributor{name: "teams", err: boom}),
		}},
		"cap overflow": {resolver: &PrincipalResolver{
			Members:      searchMembers{member: true},
			Roles:        searchRoles{},
			Contributors: contribs(searchContributor{name: "teams", out: []string{"team:a", "team:b", "team:c"}}),
			Cap:          func() int { return 2 },
		}, sentinel: ErrPrincipalCapExceeded},
		"missing membership checker": {resolver: &PrincipalResolver{Roles: searchRoles{}}},
		"membership error": {resolver: &PrincipalResolver{
			Members: searchMembers{err: boom},
			Roles:   searchRoles{},
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newSearchFixture(t)
			svc := New(Options{})
			svc.SetSearch(NewSearcher(fx.deps()), tc.resolver)
			pc, err := svc.Principals(fx.ctx)
			requireIs(t, err, iface.ErrRetrievalUnavailable)
			if tc.sentinel != nil {
				requireIs(t, err, tc.sentinel)
			}
			if errors.Is(err, boom) {
				t.Fatalf("the resolver's error is in the chain: %v", err)
			}
			if errors.Is(err, iface.ErrRetrievalForbidden) {
				t.Fatalf("err = %v, want unavailable, not forbidden", err)
			}
			if strings.Contains(err.Error(), "secret-team-42") {
				t.Fatalf("a contributor's error text was returned: %v", err)
			}
			assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
		})
	}
}

// Forbidden (no identity in ctx) stays forbidden through Service.Principals,
// and a cancelled caller keeps its context error in the chain.
func TestService_PrincipalsForbiddenAndCanceled(t *testing.T) {
	fx := newSearchFixture(t)
	svc := New(Options{})
	svc.SetSearch(NewSearcher(fx.deps()), &PrincipalResolver{Members: searchMembers{member: true}, Roles: searchRoles{}})
	_, err := svc.Principals(context.Background())
	requireIs(t, err, iface.ErrRetrievalForbidden)

	ctx, cancel := context.WithCancel(fx.ctx)
	cancel()
	svc.SetSearch(NewSearcher(fx.deps()), &PrincipalResolver{
		Members: searchMembers{err: fmt.Errorf("tenant lookup: %w", context.Canceled)},
		Roles:   searchRoles{},
	})
	_, err = svc.Principals(ctx)
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	requireIs(t, err, context.Canceled)
}
