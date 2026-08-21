// service_accounts.cedar — explicit coverage for the service-account
// admin surface. Both keys are System-flagged (operator console only),
// so the platform system-role shortcuts already grant them at the
// evaluator; this permit names the non-suffix "manage" action so the
// policycoverage gate sees it and Cedar stays in agreement.
// auth.service_accounts.read needs no policy here — it is suffix-covered
// (action_suffix == "read") by tenant_scope.cedar / role-table shortcuts.

@id("service_accounts.manage.platform_admins")
permit (
    principal,
    action == Action::"auth.service_accounts.manage",
    resource
) when {
    principal has system_role &&
    (principal.system_role == "super_admin" || principal.system_role == "administrator")
};
