package handlers

// RouteMount controls how the auth handlers' Register*Routes methods bind
// HTTP paths and OpenAPI operation IDs. Multiple mounts on the same
// huma.API would otherwise collide on operation IDs and on chi paths;
// the prefix fields keep each tier's surface unique.
//
// ADR-0003 PR-D ships two audience mounts:
//
//   - OperatorMount → /v1/auth/operator/...  (Tier-1 console.orkestra.com)
//   - ClientMount   → /v1/auth/client/...    (Tier-2 api.orkestra.com)
//
// Handlers themselves are tier-stateless: each Register*Routes call
// reads PathPrefix + OpIDPrefix off the mount and rewrites the operation
// accordingly. Per-tier service binding lives in module.go where the
// tier-specific handler instances are constructed off the matching
// authTierBundle.
type RouteMount struct {
	// PathPrefix is the segment inserted between /v1/auth and the route's
	// suffix (e.g. "/operator" yields /v1/auth/operator/login).
	PathPrefix string
	// OpIDPrefix is prepended to every Huma operation ID so multiple
	// mounts on the same huma.API don't collide.
	OpIDPrefix string
}

// OperatorMount mounts the auth handlers under /v1/auth/operator/...
// for the Tier-1 console audience. Operation IDs are prefixed with
// "operator-" so they don't clash with the client variants.
var OperatorMount = RouteMount{PathPrefix: "/operator", OpIDPrefix: "operator-"}

// ClientMount mounts the auth handlers under /v1/auth/client/... for
// the Tier-2 customer audience. Operation IDs are prefixed with
// "client-".
var ClientMount = RouteMount{PathPrefix: "/client", OpIDPrefix: "client-"}
