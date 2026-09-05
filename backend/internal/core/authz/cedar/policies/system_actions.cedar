// system_actions.cedar — platform-reserved actions need a platform role.
//
// Every system.* permission is declared System: true and gated with
// RequireSystemPermission (tenantID == ""). Tenant-scoped role permits in
// tenant_roles.cedar are written for tenant resources and must never be
// the reason a system.* action is allowed: Cedar forbids win over permits,
// so this single rule closes every present and future tenant-role permit.
//
// This is the H-5 fix. Fixing tenant_roles.cedar permit-by-permit would
// have to be repeated for every permit added later; a forbid cannot be
// forgotten.
//
// developer stays in the exempt set: the role table already bounds it to
// read-only in production and platform.developer.prod_readonly mirrors
// that. The forbid is about WHO may hold system actions; the permits are
// about WHICH.
//
// The rule keys on context.action_module — the substring before the first
// "." in the permission key, stamped on every request by the engine —
// while the platform reservation itself is the System: true flag on the
// PermissionSpec. The two coincide exactly for all ten system.* keys in
// the tree today, and every one of them is gated only by
// RequireSystemPermission. A fork that flags a key System: true under a
// different module prefix is not covered here (conservative: no false
// denial); a fork that puts a non-System key under "system." would be.
//
// Landing this rule does NOT flip CEDAR_ENFORCE_ACTIONS: on an unenforced
// action the deny is observable as shadow-mode divergence telemetry and
// changes no decision. Enforcement stays a separate operational step.
@id("system_actions.require_platform_role")
forbid (
    principal,
    action,
    resource
) when {
    context has action_module &&
    context.action_module == "system" &&
    !(principal has system_role &&
      (principal.system_role == "super_admin" ||
       principal.system_role == "administrator" ||
       principal.system_role == "developer"))
};
