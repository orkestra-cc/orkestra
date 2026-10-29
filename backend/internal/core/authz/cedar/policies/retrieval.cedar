// retrieval.cedar — explicit coverage for the core retrieval admin surface
// (ADR-0023, D15).
//
// retrieval.corpus.read and retrieval.corpus.admin are System keys gated at
// the route with RequireSystemPermission, so the platform system-role
// shortcuts already grant them at the evaluator; these permits exist so
// policycoverage sees each action named and Cedar stays in agreement with
// the catalog.
//
// The corpus mutation key carries the "admin" suffix on purpose:
// abac.cedar's require_mfa_for_admin_suffix forbids it to a
// super_admin/administrator without an enrolled second factor, and the
// authz production filter (D9) never grants a developer an ".admin" key.
// Reads use "read" so a production developer still sees corpus health.
//
// The organization indexing read key (retrieval.tenant.read) is
// suffix-covered by tenant_roles.cedar and platform.cedar
// (action_suffix == "read") and needs no policy here. The tenant mutation
// key, retrieval.tenant.admin (delivery step 2), is not declared yet.
//
// The search log names who searched what and whom the results described, so
// it is limited to the organization's owners and admins. org_member and
// org_viewer are not covered by any permit for it.

@id("retrieval.corpus_read.platform_roles")
permit (
    principal,
    action == Action::"retrieval.corpus.read",
    resource
) when {
    principal has system_role &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator" ||
     principal.system_role == "developer")
};

@id("retrieval.corpus_admin.platform_admins")
permit (
    principal,
    action == Action::"retrieval.corpus.admin",
    resource
) when {
    principal has system_role &&
    (principal.system_role == "super_admin" || principal.system_role == "administrator")
};

@id("retrieval.searchlog_admin.org_admins")
permit (
    principal,
    action == Action::"retrieval.searchlog.admin",
    resource
) when {
    principal has tenant_roles &&
    (principal.tenant_roles.contains("org_owner") ||
     principal.tenant_roles.contains("org_admin")) &&
    resource is Orkestra::Tenant
};
