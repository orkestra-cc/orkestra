package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type searchMembers struct {
	member bool
	err    error
}

func (m searchMembers) IsMember(context.Context, string, string) (bool, error) {
	return m.member, m.err
}

type searchRoles struct {
	ids []string
	err error
}

func (r searchRoles) ActiveRoleIDs(context.Context, string, string) ([]string, error) {
	return r.ids, r.err
}

type searchContributor struct {
	name string
	out  []string
	err  error
}

func (c searchContributor) Name() string { return c.name }
func (c searchContributor) Principals(context.Context, string, string) ([]string, error) {
	return c.out, c.err
}

func principalCtx(role string) context.Context {
	return testkit.NewIdentity("u-1", "u1@example.test", role).
		WithTenant("t-1", []string{"org_member"}, true).
		ContextFor(context.Background(), "t-1")
}

func contribs(cs ...searchContributor) func() []iface.RetrievalPrincipalContributor {
	return func() []iface.RetrievalPrincipalContributor {
		out := make([]iface.RetrievalPrincipalContributor, 0, len(cs))
		for _, c := range cs {
			out = append(out, c)
		}
		return out
	}
}

func TestPrincipals_MemberWithRolesAndContributor(t *testing.T) {
	r := &PrincipalResolver{
		Members:      searchMembers{member: true},
		Roles:        searchRoles{ids: []string{"r-2", "r-1"}},
		Contributors: contribs(searchContributor{name: "teams", out: []string{"team:t-9"}}),
	}
	pc, err := r.Principals(principalCtx("operator"))
	require.NoError(t, err)
	assert.Equal(t, []string{"role:r-1", "role:r-2", "team:t-9", "tenant:*", "user:u-1"}, pc.Principals)
	assert.Equal(t, "t-1", pc.TenantID)
	assert.Equal(t, "u-1", pc.UserUUID)
}

func TestPrincipals_NonMemberGetsNoTenantWildcard(t *testing.T) {
	r := &PrincipalResolver{Members: searchMembers{}, Roles: searchRoles{}}
	pc, err := r.Principals(principalCtx("super_admin"))
	require.NoError(t, err)
	assert.Equal(t, []string{"user:u-1"}, pc.Principals)
}

func TestPrincipals_MembershipErrorFailsClosed(t *testing.T) {
	r := &PrincipalResolver{Members: searchMembers{err: errors.New("boom")}, Roles: searchRoles{}}
	pc, err := r.Principals(principalCtx("operator"))
	assert.NotNil(t, err)
	assert.Nil(t, pc.Principals)
}

func TestPrincipals_MissingMembershipCheckerFailsClosed(t *testing.T) {
	r := &PrincipalResolver{Roles: searchRoles{}}
	pc, err := r.Principals(principalCtx("operator"))
	assert.NotNil(t, err)
	assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
}

func TestPrincipals_RoleListerErrorFailsClosed(t *testing.T) {
	r := &PrincipalResolver{Members: searchMembers{member: true}, Roles: searchRoles{err: errors.New("boom")}}
	pc, err := r.Principals(principalCtx("operator"))
	assert.NotNil(t, err)
	assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
}

func TestPrincipals_MissingRoleListerFailsClosed(t *testing.T) {
	r := &PrincipalResolver{Members: searchMembers{member: true}}
	pc, err := r.Principals(principalCtx("operator"))
	assert.NotNil(t, err)
	assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
}

func TestPrincipals_ContributorErrorFailsClosed(t *testing.T) {
	r := &PrincipalResolver{
		Members:      searchMembers{member: true},
		Roles:        searchRoles{},
		Contributors: contribs(searchContributor{name: "teams", err: errors.New("boom")}),
	}
	pc, err := r.Principals(principalCtx("operator"))
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "teams")
	assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
}

func TestPrincipals_ContributorReservedPrefixRefused(t *testing.T) {
	for _, bad := range []string{"tenant:*", "user:u-2", "role:r-9", "", "team", "Team:x", "team:a b"} {
		r := &PrincipalResolver{
			Members:      searchMembers{member: true},
			Roles:        searchRoles{},
			Contributors: contribs(searchContributor{name: "teams", out: []string{bad}}),
		}
		pc, err := r.Principals(principalCtx("operator"))
		if !errors.Is(err, ErrContributorPrincipal) {
			t.Errorf("%q: err = %v, want ErrContributorPrincipal", bad, err)
		}
		assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
	}
}

func TestPrincipals_NilContributorFailsClosed(t *testing.T) {
	r := &PrincipalResolver{
		Members:      searchMembers{member: true},
		Roles:        searchRoles{},
		Contributors: func() []iface.RetrievalPrincipalContributor { return []iface.RetrievalPrincipalContributor{nil} },
	}
	_, err := r.Principals(principalCtx("operator"))
	assert.NotNil(t, err)
}

func TestPrincipals_EmptyRoleIDFailsClosed(t *testing.T) {
	r := &PrincipalResolver{Members: searchMembers{member: true}, Roles: searchRoles{ids: []string{""}}}
	_, err := r.Principals(principalCtx("operator"))
	assert.NotNil(t, err)
}

func TestPrincipals_CapOverflowFailsClosed(t *testing.T) {
	mk := func(cap int) *PrincipalResolver {
		return &PrincipalResolver{
			Members:      searchMembers{member: true},
			Roles:        searchRoles{},
			Contributors: contribs(searchContributor{name: "teams", out: []string{"team:a", "team:b", "team:c"}}),
			Cap:          func() int { return cap },
		}
	}
	pc, err := mk(4).Principals(principalCtx("operator"))
	if !errors.Is(err, ErrPrincipalCapExceeded) {
		t.Fatalf("err = %v, want ErrPrincipalCapExceeded", err)
	}
	assert.Equal(t, iface.RetrievalPrincipalContext{}, pc)
	_, err = mk(5).Principals(principalCtx("operator"))
	assert.NoError(t, err)
}

func TestPrincipals_DuplicatesCountOnce(t *testing.T) {
	r := &PrincipalResolver{
		Members: searchMembers{member: true},
		Roles:   searchRoles{},
		Contributors: contribs(
			searchContributor{name: "a", out: []string{"team:a"}},
			searchContributor{name: "b", out: []string{"team:a"}},
		),
		Cap: func() int { return 3 },
	}
	pc, err := r.Principals(principalCtx("operator"))
	require.NoError(t, err)
	assert.Len(t, pc.Principals, 3)
}

func TestPrincipals_MissingIdentityIsForbidden(t *testing.T) {
	r := &PrincipalResolver{Members: searchMembers{member: true}, Roles: searchRoles{}}
	_, err := r.Principals(context.Background())
	if !errors.Is(err, iface.ErrRetrievalForbidden) {
		t.Fatalf("err = %v, want ErrRetrievalForbidden", err)
	}
}
