// abac.cedar — attribute-based access control forbids.
//
// Section B item #4 of the 2026-04-24 auth roadmap (Commit C). Uses the
// ABAC attributes plumbed by Commits A–B:
//
//   principal.mfa_enrolled  : Bool (always stamped by the engine; honours
//                             the MFA epoch, so a marker whose factor the
//                             user has since removed reads as false)
//   principal.amr           : Set<String> (optional) — the RAW claim, a
//                             record of how the session authenticated. It
//                             is NOT epoch-checked and it still lists a
//                             removed factor's marker, so a policy that
//                             grants on it grants on authority the
//                             platform has disowned. For MFA authority use
//                             principal.mfa_enrolled.
//   resource.status         : String (threaded from real tenant status)
//   context.hour_utc        : Long
//   context.weekday         : String
//   context.ip_bucket       : String ("loopback" | "private" | "public" | "unknown")
//
// These are shadow-only at landing — CEDAR_ENFORCE_ACTIONS still controls
// which actions are load-bearing. Each new forbid here emits divergence
// telemetry for its @id so operators can measure blast radius before
// flipping the enforce flag on the covered actions.

// Require a verified second factor on every admin-suffix action when the
// caller holds a privileged platform role. Blocks a pwd-only or oauth-only
// session from reaching a .admin endpoint even when the role table would
// allow (super_admin wildcard, administrator.all). Tenant-level roles
// (org_owner / org_admin / manager) are not covered here — their
// .admin reach is narrow and the existing MFA-grace enforcement in the
// auth module already gates their login when a factor is required.
//
// The engine stamps principal.mfa_enrolled unconditionally (false when
// the session has no second factor), so no `has` guard is needed here;
// if the engine is ever refactored to conditional stamping the ABAC
// tests will catch the regression.
@id("abac.require_mfa_for_admin_suffix")
forbid (
    principal,
    action,
    resource
) when {
    principal has system_role &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator") &&
    context has action_suffix &&
    context.action_suffix == "admin" &&
    principal.mfa_enrolled == false
};

// The platform user-admin keys (user.read/create/update/delete) replaced
// system.users.admin as the gate of the /v1/users and
// /v1/admin/client-users routes. These two forbids carry over the
// restrictions that gate had here, so a deployment that moves its
// CEDAR_ENFORCE_ACTIONS entry from system.users.admin to the user.* keys
// keeps them: the user keys have no .admin suffix (so the MFA forbid
// above never fired on them) and are not system.* literals (so the
// public-IP forbid below did not name them).
//
// User management without a second factor: refused to the platform roles
// that can write users (super_admin, administrator, manager) on every
// user-admin key, reads included, as system.users.admin was.
@id("abac.require_mfa_for_user_admin")
forbid (
    principal,
    action,
    resource
) when {
    (action == Action::"user.read" ||
     action == Action::"user.create" ||
     action == Action::"user.update" ||
     action == Action::"user.delete") &&
    principal has system_role &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator" ||
     principal.system_role == "manager") &&
    principal.mfa_enrolled == false
};

// User management from a public IP in production: refused like the
// system.* actions in abac.deny_system_actions_from_public_ip_in_prod.
@id("abac.deny_user_admin_from_public_ip_in_prod")
forbid (
    principal,
    action,
    resource
) when {
    (action == Action::"user.read" ||
     action == Action::"user.create" ||
     action == Action::"user.update" ||
     action == Action::"user.delete") &&
    context has env && context.env == "production" &&
    context has ip_bucket && context.ip_bucket == "public"
};

// Deny platform-reserved system.* actions when the call originates from
// a public IP and the deployment is production. The four actions listed
// are the same ones named in tenant_scope.cedar's tier-aware forbids —
// keeping the enumeration narrow means we don't accidentally broaden
// Cedar's reach over actions that aren't already covered there.
//
// loopback + private buckets (office VPN, internal service mesh) stay
// permitted; unknown buckets (background jobs, AI sidecar S2S calls
// without an HTTP request) also pass because those paths don't carry a
// client IP and shouldn't be mistaken for a leaked credential. Env is
// bounded to "production" so dev/staging operators can still exercise
// admin endpoints from the public internet for smoke tests.
@id("abac.deny_system_actions_from_public_ip_in_prod")
forbid (
    principal,
    action,
    resource
) when {
    (action == Action::"system.modules.admin" ||
     action == Action::"system.tenants.admin" ||
     action == Action::"system.users.admin" ||
     action == Action::"system.users.mfa_reset" ||
     action == Action::"system.users.password_reset" ||
     action == Action::"system.users.email_verify_resend" ||
     action == Action::"system.users.oauth_unlink") &&
    context has env && context.env == "production" &&
    context has ip_bucket && context.ip_bucket == "public"
};

// A PIN session (principal.amr contains "pin") is a
// sign-in with a 4–8 digit secret. Whatever the role table says, it never
// reaches a destructive or administrative action, nor a platform-reserved
// system.* one. Defence in depth: the auth module already refuses PIN for
// privileged roles and every credential change from a PIN session. The
// marker survives refreshes (the auth module re-stamps it from the
// refresh row), so the forbid cannot be shed by refreshing.
@id("abac.pin_session_no_destructive")
forbid (
    principal,
    action,
    resource
) when {
    principal has amr && principal.amr.contains("pin") &&
    context has action_suffix &&
    (context.action_suffix == "delete" ||
     context.action_suffix == "admin" ||
     context.action_suffix == "manage")
};

@id("abac.pin_session_no_system")
forbid (
    principal,
    action,
    resource
) when {
    principal has amr && principal.amr.contains("pin") &&
    context has action_module && context.action_module == "system"
};
