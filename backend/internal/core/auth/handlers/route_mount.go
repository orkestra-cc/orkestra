package handlers

// RouteMount controls how the auth handlers' Register*Routes methods bind
// HTTP paths and OpenAPI operation IDs. Multiple mounts on the same
// huma.API would otherwise collide on operation IDs and on chi paths;
// the prefix fields keep each tier's surface unique.
//
// ADR-0003 PR-D introduces two parallel mount points alongside the
// pre-cutover legacy paths:
//
//   - LegacyMount   → /v1/auth/...           (pre-cutover; removed in D-8)
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
	// legacy suffix. Examples: "" yields /v1/auth/login; "/operator"
	// yields /v1/auth/operator/login.
	PathPrefix string
	// OpIDPrefix is prepended to every Huma operation ID so multiple
	// mounts on the same huma.API don't collide. Empty for the legacy
	// mount; "operator-" / "client-" for the tier-split mounts.
	OpIDPrefix string
}

// LegacyMount is the pre-PR-D mount: paths under /v1/auth/... with no
// operation-ID prefix. Removed by D-8 once the tier-split paths cover
// every flow.
var LegacyMount = RouteMount{}

// OperatorMount mounts the auth handlers under /v1/auth/operator/...
// for the Tier-1 console audience. Operation IDs are prefixed with
// "operator-" so they don't clash with the legacy or client variants.
var OperatorMount = RouteMount{PathPrefix: "/operator", OpIDPrefix: "operator-"}

// ClientMount mounts the auth handlers under /v1/auth/client/... for
// the Tier-2 customer audience. Operation IDs are prefixed with
// "client-". Wired in PR-D D-5; declared here so D-4 callers can
// statically reference the constant.
var ClientMount = RouteMount{PathPrefix: "/client", OpIDPrefix: "client-"}
