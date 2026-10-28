package services

import (
	"context"
	"errors"
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

func TestService_DeleteBySubjectNotImplemented(t *testing.T) {
	err := New(Options{}).DeleteBySubject(context.Background(), "t-1", "user:alice")
	requireIs(t, err, ErrDeleteBySubjectNotImplemented)
	requireIs(t, err, errors.ErrUnsupported)
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
