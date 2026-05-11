// tenant_scope.cedar — tier-aware forbid rules.
//
// ADR-0001 reframes Orkestra's tenancy as a two-tier model (internal
// operator tenants vs external client tenants). These policies enforce
// that the platform-reserved system.* actions can only run against an
// internal tenant — a super_admin acting on an external client tenant
// cannot execute operator-only actions like module administration.
//
// forbid rules in Cedar take priority over permits, so these guard rails
// fire even when platform.cedar's wildcard would otherwise permit.

// system.modules.admin must be operator-side.
@id("tenant_scope.system_modules_admin_internal_only")
forbid (
    principal,
    action == Action::"system.modules.admin",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// system.tenants.admin (platform-wide tenant management) is operator-side.
@id("tenant_scope.system_tenants_admin_internal_only")
forbid (
    principal,
    action == Action::"system.tenants.admin",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// system.users.admin is operator-side.
@id("tenant_scope.system_users_admin_internal_only")
forbid (
    principal,
    action == Action::"system.users.admin",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// system.users.mfa_reset is operator-side.
@id("tenant_scope.system_users_mfa_reset_internal_only")
forbid (
    principal,
    action == Action::"system.users.mfa_reset",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// system.users.password_reset is operator-side (admin triggers a
// password-reset email for another user).
@id("tenant_scope.system_users_password_reset_internal_only")
forbid (
    principal,
    action == Action::"system.users.password_reset",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// system.users.email_verify_resend is operator-side (admin resends the
// email-verification message).
@id("tenant_scope.system_users_email_verify_resend_internal_only")
forbid (
    principal,
    action == Action::"system.users.email_verify_resend",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// system.users.oauth_unlink is operator-side (admin unlinks an OAuth
// identity from another user).
@id("tenant_scope.system_users_oauth_unlink_internal_only")
forbid (
    principal,
    action == Action::"system.users.oauth_unlink",
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has kind &&
    resource.kind == "external"
};

// Suspended / archived / purged tenants reject every non-read action. A
// tenant that's been suspended by the operator console should surface as
// read-only until it's reactivated.
@id("tenant_scope.inactive_tenant_denies_mutations")
forbid (
    principal,
    action,
    resource
) when {
    resource is Orkestra::Tenant &&
    resource has status &&
    (resource.status == "suspended" ||
     resource.status == "archived" ||
     resource.status == "purged") &&
    context has action_suffix &&
    context.action_suffix != "read" &&
    context.action_suffix != "view" &&
    context.action_suffix != "self"
};
