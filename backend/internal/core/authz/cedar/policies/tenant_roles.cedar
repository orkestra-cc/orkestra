// tenant_roles.cedar — tenant-scoped role permits.
//
// Two generations of permits live here. The "legacy tenant role" permits
// (manager, operator, guest, administrator-as-tenant-role) date from
// before the org-role split (Section B item #3 of the auth roadmap,
// 2026-04-24). The "org role" permits (org_owner, org_admin, org_member,
// org_billing, org_viewer) are the post-split tenant-scoped role catalog.
// Both groups are kept active simultaneously: existing dev-tenant
// memberships still carry the legacy role names in
// TenantMembership.Roles, and ripping the legacy permits would dirty
// shadow-mode divergence telemetry without user-visible benefit. A
// follow-up cleanup commit can remove the legacy block once memberships
// are migrated to org_* names.
//
// Principal attribute used here: tenant_roles — a Set<String> listing
// the role names the user holds in the acting tenant. For a check about
// the caller in the request tenant it comes from iface.TenantMembership.Roles
// (the request context); for a check about another user, or another tenant,
// from that user's bindings there, catalog roles only (authz
// services.boundRoleNames).

// ----- Org-role permits (post-split) ---------------------------------

// org_owner — every action on this tenant. The org-side mirror of the
// platform "administrator" shortcut, but bounded to one tenant. Tier
// scoping is enforced separately by tenant_scope.cedar's forbid rules:
// even an org_owner cannot trigger a system.* action against an external
// tenant.
@id("tenant_roles.org_owner.all_in_tenant")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("org_owner") &&
    resource is Orkestra::Tenant
};

// org_admin — every action except deletes. Mirrors the manager pattern
// (read + create + update without removal authority).
@id("tenant_roles.org_admin.all_except_delete")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("org_admin") &&
    resource is Orkestra::Tenant &&
    context has action_suffix &&
    context.action_suffix != "delete"
};

// org_member — read across the tenant plus self/own scopes for
// personal resources. Suffix list mirrors the Go-side org_member
// predicate in services.SeedSystemRoles.
@id("tenant_roles.org_member.read_self_own")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("org_member") &&
    resource is Orkestra::Tenant &&
    context has action_suffix &&
    (context.action_suffix == "read" ||
     context.action_suffix == "view" ||
     context.action_suffix == "self" ||
     context.action_suffix == "own")
};

// org_billing — every action whose action_module is one of the three
// finance-surface modules. action_module is the substring before the
// first "." in the permission key (e.g. "billing.invoice.read" →
// "billing"), stamped on the request context by the engine.
@id("tenant_roles.org_billing.finance_modules")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("org_billing") &&
    resource is Orkestra::Tenant &&
    context has action_module &&
    (context.action_module == "billing" ||
     context.action_module == "payments" ||
     context.action_module == "subscriptions")
};

// org_viewer — read-only across the tenant.
@id("tenant_roles.org_viewer.read_only")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("org_viewer") &&
    resource is Orkestra::Tenant &&
    context has action_suffix &&
    (context.action_suffix == "read" || context.action_suffix == "view")
};

// ----- Legacy tenant-role permits (pre-split, deprecated) ------------
//
// Kept active until existing memberships are migrated from
// "administrator"/"manager"/"operator"/"guest" to org_* names. After
// that, this block can be removed without behavior change.

// A tenant member with the "manager" role gets read + create + update
// actions on their tenant. Delete and admin actions are not granted.
// Matches the manager filter in authz/services/service.go::SeedSystemRoles.
@id("tenant_roles.manager.read_write_noop_delete")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("manager") &&
    resource is Orkestra::Tenant &&
    context has action_suffix &&
    context.action_suffix != "delete" &&
    context.action_suffix != "admin"
};

// Operator: read-only + self-service actions on their tenant.
@id("tenant_roles.operator.read_self")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("operator") &&
    resource is Orkestra::Tenant &&
    context has action_suffix &&
    (context.action_suffix == "read" ||
     context.action_suffix == "view" ||
     context.action_suffix == "self")
};

// Guest: read-only on their tenant.
@id("tenant_roles.guest.read_only")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("guest") &&
    resource is Orkestra::Tenant &&
    context has action_suffix &&
    (context.action_suffix == "read" || context.action_suffix == "view")
};

// Holding "administrator" as a tenant-level role (legacy shape where
// bindings name "administrator" inside a tenant) grants every action
// on that tenant — still subject to tenant_scope.cedar forbid rules.
@id("tenant_roles.administrator.all_in_tenant")
permit (
    principal,
    action,
    resource
) when {
    principal has tenant_roles &&
    principal.tenant_roles.contains("administrator") &&
    resource is Orkestra::Tenant
};
