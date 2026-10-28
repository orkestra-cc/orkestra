// platform.cedar — system-role shortcuts.
//
// These policies grant broad permissions based on the user's platform-level
// system role (stored on User.system_role). They are the Cedar equivalent
// of the role-table shortcuts in authz/services/service.go: super_admin
// gets the wildcard, administrator inherits every registered action,
// developer is environment-gated.
//
// Every policy is annotated with @id so structured shadow-mode logs can
// report the matching policy id. Policy ids follow the scheme
//   <file>.<role_or_rule>.<variant>

// super_admin: wildcard. The canonical "god mode" role; no further checks.
@id("platform.super_admin.wildcard")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "super_admin"
};

// administrator: full access to every declared action. Tier scoping is
// enforced separately by tenant_scope.cedar forbid rules.
@id("platform.administrator.all")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "administrator"
};

// developer: full access in non-production environments (dev, staging).
// Environment is carried on the request Context so one deployed binary
// per environment can produce different developer privilege in a way that
// matches the existing role-seeder logic.
@id("platform.developer.nonprod")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "developer" &&
    context has env && context.env != "production"
};

// developer: production downgrade — read-only system actions only.
// Mirrors authz.Service.GetEffectivePermissions's developer-in-prod
// filter: keys ending in .read, .view, or .self are permitted; all
// other actions fall through to forbid.
@id("platform.developer.prod_readonly")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "developer" &&
    context has env && context.env == "production" &&
    context has action_suffix && (
        context.action_suffix == "read" ||
        context.action_suffix == "view" ||
        context.action_suffix == "self"
    )
};

// --- manager / operator / guest at GLOBAL scope (evaluator rule 2b) ---
//
// A platform system role conveys its SeedSystemRoles permission set only
// when no tenant is in scope (context.scope == "global": the platform user
// routes, RequireSystemPermission). Inside a tenant the same principal is
// an ordinary member and tenant_roles.cedar decides. Platform-reserved
// keys stay with the administrator/developer permits above; Cedar has no
// System flag, so `action_module != "system"` is the proxy — the handful
// of System:true keys under other modules (auth.service_accounts.*,
// notification.template.manage, llm.*.admin) diverge here in shadow logs
// only, and none of them is enforced through Cedar.

// manager: read + create + update, no delete, no admin — global scope.
@id("platform.manager.global_no_delete_no_admin")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "manager" &&
    context has scope && context.scope == "global" &&
    context has action_module && context.action_module != "system" &&
    context has action_suffix &&
    context.action_suffix != "delete" &&
    context.action_suffix != "admin"
};

// operator: read + self-service — global scope.
@id("platform.operator.global_read_self")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "operator" &&
    context has scope && context.scope == "global" &&
    context has action_module && context.action_module != "system" &&
    context has action_suffix && (
        context.action_suffix == "read" ||
        context.action_suffix == "self"
    )
};

// guest: read-only — global scope.
@id("platform.guest.global_read_only")
permit (
    principal,
    action,
    resource
) when {
    principal has system_role && principal.system_role == "guest" &&
    context has scope && context.scope == "global" &&
    context has action_module && context.action_module != "system" &&
    context has action_suffix && context.action_suffix == "read"
};
