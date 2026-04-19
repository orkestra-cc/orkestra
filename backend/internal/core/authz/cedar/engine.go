// Package cedar wraps cedar-policy/cedar-go so the rest of the backend can
// evaluate authorization policies without knowing about the upstream API.
//
// Phase 1 of the tenancy plan (ADR-0001) adopts Cedar in shadow mode: the
// engine evaluates the same decision the role-table does and emits the
// result as structured telemetry. Enforcement stays on the role table
// until divergence between the two sources settles down.
//
// The engine loads its policies from embedded .cedar files under
// policies/. Adding a new policy is a pure-file change; the engine picks
// it up on next boot.
package cedar

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

//go:embed policies/*.cedar
var policyFS embed.FS

// Namespace is the Cedar namespace every entity type lives under. Kept as
// a constant so callers don't hand-write strings that drift.
const Namespace = "Orkestra"

// Entity types exposed to policies. Matches the .cedar files literally;
// any drift fails at policy-load time.
const (
	EntityUser   = Namespace + "::User"
	EntityTenant = Namespace + "::Tenant"
)

// Decision is the result of an authorization evaluation — a thin wrapper
// around cedar.Decision so callers don't import the upstream package.
type Decision struct {
	Allowed       bool
	MatchedPolicy string // the @id of the first matching policy, empty if forbid or no match
	Reasons       []string
	Errors        []string
}

// Engine is the stateless policy evaluator. Safe for concurrent use.
type Engine struct {
	policies *cedar.PolicySet
	env      string // "development" | "staging" | "production"
}

// Principal is the subject of an authorization request — a User with its
// platform-level system role and the tenant-level roles they hold in the
// acting tenant.
type Principal struct {
	UserUUID    string
	SystemRole  string   // super_admin | administrator | developer | manager | operator | guest
	TenantRoles []string // role names the user holds in the acting tenant
	// Capabilities is the list of capability IDs the acting tenant currently
	// holds an active entitlement for. Populated from
	// TenantProvider.ListCapabilityIDs so Cedar's capability_grants.cedar
	// forbid-unless-entitled rule can reject capability-gated actions when
	// the tenant is unentitled. Empty when no capability context applies.
	Capabilities []string
}

// Request bundles the inputs of an authorization evaluation. Callers that
// need to enable the capability-enforcement pathway (Phase 2 defense in
// depth) use Evaluate with a non-empty RequiredCapability; the simpler
// IsAuthorized wrapper leaves it blank, which keeps shadow-mode semantics
// unchanged.
type Request struct {
	Principal Principal
	Action    string
	Resource  Resource
	// RequiredCapability, when non-empty, is stamped onto the request
	// context as "requires_capability". capability_grants.cedar's
	// forbid-unless-entitled rule fires only when this context key is
	// present, so shadow-mode callers that don't yet wire capability
	// enforcement stay untouched.
	RequiredCapability string
}

// Resource is the target of an authorization request. Today every
// authorization is scoped to a tenant (or no tenant for global routes).
// Phase 2 extends this to Document / Subscription / Capability.
type Resource struct {
	TenantUUID   string
	TenantKind   string // "internal" | "external"
	TenantStatus string // "active" | "suspended" | "archived" | "purged"
}

// New loads the embedded policies, validates them, and returns an engine
// ready for IsAuthorized calls. env is the deployment environment
// ("development" | "staging" | "production") — populated on the request
// Context so policies can branch on it.
func New(env string) (*Engine, error) {
	if env == "" {
		env = "development"
	}
	ps := cedar.NewPolicySet()

	entries, err := fs.ReadDir(policyFS, "policies")
	if err != nil {
		return nil, fmt.Errorf("cedar: read policies: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cedar") {
			continue
		}
		data, err := fs.ReadFile(policyFS, "policies/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("cedar: read %s: %w", entry.Name(), err)
		}
		// Skip files that are intentionally empty placeholders (comments
		// only). Cedar's parser rejects empty input; detect that and skip
		// rather than requiring every stub to carry a dummy policy.
		if !hasPolicyStatement(data) {
			continue
		}
		list, err := cedar.NewPolicyListFromBytes(entry.Name(), data)
		if err != nil {
			return nil, fmt.Errorf("cedar: parse %s: %w", entry.Name(), err)
		}
		for i, p := range list {
			id := types.PolicyID(fmt.Sprintf("%s#%d", entry.Name(), i))
			if anno, ok := p.Annotations()["id"]; ok && anno != "" {
				id = types.PolicyID(string(anno))
			}
			ps.Add(id, p)
		}
	}
	return &Engine{policies: ps, env: env}, nil
}

// hasPolicyStatement reports whether the source text contains something
// other than comments and whitespace. Cedar's parser fails on empty input
// so capability_grants.cedar (intentionally empty until Phase 2) would
// otherwise break boot. A pragmatic line-scan is enough: Cedar doesn't
// have block comments.
func hasPolicyStatement(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		return true
	}
	return false
}

// PolicyCount returns the number of loaded policies. Useful for boot-time
// diagnostics ("Cedar: loaded 9 policies from 4 files").
func (e *Engine) PolicyCount() int {
	return len(e.policies.Map())
}

// IsAuthorized evaluates the policy set against the supplied principal,
// action, and resource. Returns a Decision that carries the matched
// policy id, reasons, and any evaluation errors.
//
// Context carries the environment and a derived action_suffix ("read",
// "update", "admin", …) so policies can dispatch on the naming convention
// without exhaustively listing every action name.
//
// This is a thin wrapper over Evaluate that leaves RequiredCapability
// empty — use Evaluate directly when you want the capability_grants.cedar
// forbid-unless-entitled path to fire.
func (e *Engine) IsAuthorized(p Principal, action string, r Resource) Decision {
	return e.Evaluate(Request{Principal: p, Action: action, Resource: r})
}

// Evaluate is the canonical entry point. It accepts a Request so callers
// can optionally set RequiredCapability (stamped into context for
// defense-in-depth capability enforcement).
func (e *Engine) Evaluate(req Request) Decision {
	p := req.Principal
	r := req.Resource
	action := req.Action

	principal := cedar.NewEntityUID(EntityUser, types.String(p.UserUUID))
	actionUID := cedar.NewEntityUID("Action", types.String(action))
	resourceUID := cedar.NewEntityUID(EntityTenant, types.String(r.TenantUUID))

	entities := cedar.EntityMap{}

	// Principal entity: system_role + tenant_roles + capabilities, each as
	// a Set<String> attribute when non-empty. Absent sets stay absent so
	// policies can use `principal has capabilities` as a presence check.
	principalAttrs := types.RecordMap{}
	if p.SystemRole != "" {
		principalAttrs["system_role"] = types.String(p.SystemRole)
	}
	if len(p.TenantRoles) > 0 {
		rs := make([]types.Value, 0, len(p.TenantRoles))
		for _, role := range p.TenantRoles {
			rs = append(rs, types.String(role))
		}
		principalAttrs["tenant_roles"] = types.NewSet(rs...)
	}
	if len(p.Capabilities) > 0 {
		cs := make([]types.Value, 0, len(p.Capabilities))
		for _, cap := range p.Capabilities {
			cs = append(cs, types.String(cap))
		}
		principalAttrs["capabilities"] = types.NewSet(cs...)
	}
	entities[principal] = types.Entity{
		UID:        principal,
		Attributes: types.NewRecord(principalAttrs),
	}

	// Resource entity (when we have a tenant in scope).
	if r.TenantUUID != "" {
		resourceAttrs := types.RecordMap{}
		if r.TenantKind != "" {
			resourceAttrs["kind"] = types.String(r.TenantKind)
		}
		if r.TenantStatus != "" {
			resourceAttrs["status"] = types.String(r.TenantStatus)
		}
		entities[resourceUID] = types.Entity{
			UID:        resourceUID,
			Attributes: types.NewRecord(resourceAttrs),
		}
	}

	// Context: env + derived action suffix. The suffix is the substring
	// after the last "." — e.g. "tenant.member.invite" → "invite".
	// RequiredCapability (when non-empty) stamps a requires_capability
	// key so capability_grants.cedar can gate the request.
	suffix := action
	if idx := strings.LastIndex(action, "."); idx >= 0 && idx < len(action)-1 {
		suffix = action[idx+1:]
	}
	ctxRec := cedar.RecordMap{
		"env":           types.String(e.env),
		"action_suffix": types.String(suffix),
		"action_key":    types.String(action),
	}
	if req.RequiredCapability != "" {
		ctxRec["requires_capability"] = types.String(req.RequiredCapability)
	}
	reqCtx := cedar.NewRecord(ctxRec)

	cedarReq := cedar.Request{
		Principal: principal,
		Action:    actionUID,
		Resource:  resourceUID,
		Context:   reqCtx,
	}

	ok, diag := cedar.Authorize(e.policies, entities, cedarReq)
	decision := Decision{Allowed: bool(ok)}
	if len(diag.Reasons) > 0 {
		decision.MatchedPolicy = string(diag.Reasons[0].PolicyID)
		decision.Reasons = make([]string, 0, len(diag.Reasons))
		for _, r := range diag.Reasons {
			decision.Reasons = append(decision.Reasons, string(r.PolicyID))
		}
	}
	if len(diag.Errors) > 0 {
		decision.Errors = make([]string, 0, len(diag.Errors))
		for _, e := range diag.Errors {
			decision.Errors = append(decision.Errors, e.String())
		}
	}
	return decision
}
