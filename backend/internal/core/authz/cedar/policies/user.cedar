// user.cedar — explicit coverage for the platform user-management keys.
//
// The user routes (/v1/users, /v1/admin/client-users) are gated per action:
// user.read (every GET), user.create (create/invite), user.update (PUT/PATCH,
// send-password-reset) and user.delete (both DELETEs), evaluated at global
// scope. The suffix clauses in platform.cedar already reach them for the
// platform roles; these permits name the actions so policycoverage sees a
// literal for every key and so Cedar states the matrix in one place:
// administrator/super_admin everything, manager read/create/update,
// operator and guest read. The handlers add the target-tier guard on top
// (a caller never touches a user whose current role outranks their own),
// which no policy here can express.

@id("user.read.platform_roles")
permit (
    principal,
    action == Action::"user.read",
    resource
) when {
    principal has system_role &&
    context has scope && context.scope == "global" &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator" ||
     principal.system_role == "developer" ||
     principal.system_role == "manager" ||
     principal.system_role == "operator" ||
     principal.system_role == "guest")
};

@id("user.write.platform_roles")
permit (
    principal,
    action in [Action::"user.create", Action::"user.update"],
    resource
) when {
    principal has system_role &&
    context has scope && context.scope == "global" &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator" ||
     principal.system_role == "manager")
};

@id("user.delete.platform_roles")
permit (
    principal,
    action == Action::"user.delete",
    resource
) when {
    principal has system_role &&
    context has scope && context.scope == "global" &&
    (principal.system_role == "super_admin" ||
     principal.system_role == "administrator")
};
