package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// MembershipChecker is satisfied by iface.TenantProvider.
type MembershipChecker interface {
	IsMember(ctx context.Context, userUUID, tenantUUID string) (bool, error)
}

// DefaultPrincipalCap bounds the caller's principal set when no cap is configured.
const DefaultPrincipalCap = 256

var (
	ErrPrincipalCapExceeded = errors.New("retrieval: the caller's principals exceed the cap")
	ErrContributorPrincipal = errors.New("retrieval: a principal contributor returned an invalid principal")
)

var contributorPrincipalRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}:\S{1,200}$`)

// PrincipalResolver turns the calling user into the principal set that
// filters search (ADR-0023 D10). No platform role gets a bypass, and every
// missing dependency fails closed.
type PrincipalResolver struct {
	Members      MembershipChecker                            // nil: every call fails closed
	Roles        iface.AuthzRoleLister                        // nil: every call fails closed
	Contributors func() []iface.RetrievalPrincipalContributor // nil: none
	Cap          func() int                                   // nil or <= 0: DefaultPrincipalCap
}

var _ iface.RetrievalPrincipalResolver = (*PrincipalResolver)(nil)

// Principals implements iface.RetrievalPrincipalResolver for the user and
// tenant in ctx: user:<uuid>; tenant:* when the user is a member;
// role:<id> per active role; every contributor's output. The result is
// deduplicated and sorted ascending. Every error returns a zero context.
func (r *PrincipalResolver) Principals(ctx context.Context) (iface.RetrievalPrincipalContext, error) {
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	tenantID, _ := ctxauth.GetTenantID(ctx)
	if userUUID == "" || tenantID == "" {
		return iface.RetrievalPrincipalContext{}, iface.ErrRetrievalForbidden
	}
	if r == nil || r.Members == nil {
		return iface.RetrievalPrincipalContext{}, errors.New("retrieval: principals: membership checker is not configured")
	}
	if r.Roles == nil {
		return iface.RetrievalPrincipalContext{}, errors.New("retrieval: principals: role lister is not configured")
	}

	set := map[string]struct{}{"user:" + userUUID: {}}

	member, err := r.Members.IsMember(ctx, userUUID, tenantID)
	if err != nil {
		return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: membership: %w", err)
	}
	if member {
		set["tenant:*"] = struct{}{}
	}

	roleIDs, err := r.Roles.ActiveRoleIDs(ctx, userUUID, tenantID)
	if err != nil {
		return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: roles: %w", err)
	}
	for _, id := range roleIDs {
		p := "role:" + id
		if !contributorPrincipalRE.MatchString(p) {
			return iface.RetrievalPrincipalContext{}, errors.New("retrieval: principals: roles: invalid role id")
		}
		set[p] = struct{}{}
	}

	if r.Contributors != nil {
		for _, c := range r.Contributors() {
			if c == nil {
				return iface.RetrievalPrincipalContext{}, errors.New("retrieval: principals: nil contributor")
			}
			name := c.Name()
			out, err := c.Principals(ctx, tenantID, userUUID)
			if err != nil {
				return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: contributor %q: %w", name, err)
			}
			for _, p := range out {
				if !contributorPrincipalRE.MatchString(p) {
					return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: contributor %q: %w", name, ErrContributorPrincipal)
				}
				switch prefix, _, _ := strings.Cut(p, ":"); prefix {
				case "user", "tenant", "role":
					return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: contributor %q: reserved prefix: %w", name, ErrContributorPrincipal)
				}
				set[p] = struct{}{}
			}
		}
	}

	limit := DefaultPrincipalCap
	if r.Cap != nil {
		if n := r.Cap(); n > 0 {
			limit = n
		}
	}
	if len(set) > limit {
		return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: %d > %d: %w", len(set), limit, ErrPrincipalCapExceeded)
	}

	principals := make([]string, 0, len(set))
	for p := range set {
		principals = append(principals, p)
	}
	sort.Strings(principals)
	return iface.RetrievalPrincipalContext{TenantID: tenantID, UserUUID: userUUID, Principals: principals}, nil
}
