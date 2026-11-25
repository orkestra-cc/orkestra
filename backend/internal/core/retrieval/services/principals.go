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
	// errContributorFailed stands in for a contributor's own error, whose
	// text may name its principals and is therefore never passed on.
	errContributorFailed = errors.New("retrieval: principal contributor error")
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
// deduplicated and sorted ascending. Every error returns a zero context. A
// contributor's error is replaced by errContributorFailed (plus the context
// error when ctx is done); the membership checker's and role lister's errors
// are wrapped. Callers outside the package map the result with
// principalError and never pass its text on.
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
				// Never the contributor's error itself: its text may name its
				// principals. Only the caller's context error stays in the chain.
				if cerr := ctx.Err(); cerr != nil {
					return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principal contributor %q failed: %w: %w", name, errContributorFailed, cerr)
				}
				return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principal contributor %q failed: %w", name, errContributorFailed)
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

// principalError maps a principal resolution error for the module's callers
// (Searcher step 3 and Service.Principals), so both answer identically.
// iface.ErrRetrievalForbidden passes through unchanged. Anything else becomes
// iface.ErrRetrievalUnavailable naming only principalErrorKind; the chain
// keeps the module's own fixed-text sentinels and ctx.Err() when ctx is done,
// never the resolver's error, whose text may name a contributor's principals.
func principalError(ctx context.Context, err error) error {
	if errors.Is(err, iface.ErrRetrievalForbidden) {
		return err
	}
	out := fmt.Errorf("%w: principal resolution failed (%s)", iface.ErrRetrievalUnavailable, principalErrorKind(err))
	for _, keep := range []error{ErrPrincipalCapExceeded, ErrContributorPrincipal, errContributorFailed} {
		if errors.Is(err, keep) {
			out = fmt.Errorf("%w: %w", out, keep)
		}
	}
	if cerr := ctx.Err(); cerr != nil {
		out = fmt.Errorf("%w: %w", out, cerr)
	}
	return out
}

// principalErrorKind classifies a principal resolution error for the log and
// for principalError, without its text.
func principalErrorKind(err error) string {
	switch {
	case errors.Is(err, iface.ErrRetrievalForbidden):
		return "forbidden"
	case errors.Is(err, ErrPrincipalCapExceeded):
		return "cap_exceeded"
	case errors.Is(err, ErrContributorPrincipal):
		return "invalid_contributor_principal"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, errContributorFailed):
		return "contributor_failed"
	}
	return "error"
}
