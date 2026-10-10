// llm.cedar — explicit coverage for the core llm module (ADR-0022).
//
// Every System llm.* key below is gated at the route with
// RequireSystemPermission, so the platform system-role shortcuts already
// grant them at the evaluator; these permits exist so policycoverage sees
// each action named and Cedar stays in agreement with the catalog.
//
// Mutations carry the "admin" suffix on purpose: abac.cedar's
// require_mfa_for_admin_suffix forbids them to a super_admin/administrator
// without an enrolled second factor, and the authz production filter
// (D9) never grants a developer an ".admin" key. Reads use "read" so a
// production developer still sees configuration.
//
// llm.models.self is suffix-covered by tenant_roles.cedar and platform.cedar
// (action_suffix == "self") and needs no policy here.
//
// Later llm PRs add their own keys (usage, budgets, accounts) together with
// the routes that use them; name them here when they are declared.

@id("llm.admin_read.platform_roles")
permit (
    principal,
    action == Action::"llm.admin.read",
    resource
) when {
    principal has system_role &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator" ||
     principal.system_role == "developer")
};

@id("llm.admin_mutations.platform_admins")
permit (
    principal,
    action in [
        Action::"llm.credentials.admin",
        Action::"llm.models.admin",
        Action::"llm.grants.admin"
    ],
    resource
) when {
    principal has system_role &&
    (principal.system_role == "super_admin" || principal.system_role == "administrator")
};
