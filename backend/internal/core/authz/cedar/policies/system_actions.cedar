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
// PermissionSpec. Those two are NOT the same set, today, in this tree.
// Fifteen keys are declared System: true. Ten carry the "system." prefix
// and are covered by this rule. The other five are not:
//
//   auth.service_accounts.read     gated by RequireSystemPermission
//   auth.service_accounts.manage   gated by RequireSystemPermission
//   notification.log.read          gated by RequireSystemPermission
//   notification.template.manage   declared, gates no route
//   notification.test              declared, gates no route
//
// The first three are platform-reserved and live, and this forbid does
// not reach them. What closes them is D24, not this rule:
// RequireSystemPermission checks with an empty tenantID, and
// shadowEvaluate no longer stamps the caller's tenant_roles on a check
// with no tenant — so no tenant-role permit can fire on them in the
// first place. The last two are a pre-existing catalog artefact,
// unrelated to PR C.
//
// Keying this rule on the System: true flag instead of the module prefix
// would require the flag on the request context (a context.action_is_system
// stamped by shadowEvaluate). That is a change to D23 itself and is
// recorded as a follow-up for the spec owner — do not reach for it here.
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
