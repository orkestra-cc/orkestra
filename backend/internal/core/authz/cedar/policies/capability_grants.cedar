// capability_grants.cedar — Phase 2 defense-in-depth.
//
// Capabilities are the first-class unit of what tenants are *entitled to*.
// RequireCapability middleware already gates routes against
// tenant_entitlements (returning 402 on a miss), so Cedar is not the
// primary enforcement path today. This file exists so that when Cedar
// flips from shadow to enforcing (Phase 2 exit), any capability-gated
// request that is missing the entitlement is denied by policy too — a
// belt-and-braces layer that catches misconfigured routes or a middleware
// regression.
//
// Contract:
//
//   1. Callers that want capability enforcement set `RequiredCapability`
//      on cedar.Request. The engine stamps `context.requires_capability`.
//   2. Principals carry a `capabilities` Set<String> populated from
//      TenantProvider.ListCapabilityIDs — the same projection the runtime
//      checks.
//   3. This file emits a forbid rule that fires when requires_capability
//      is set AND the principal's capabilities either do not include it
//      or the set is absent entirely.
//
// Unlike the platform and tenant_role policies, this file's forbid is
// inert for callers that leave RequiredCapability empty (every existing
// shadow-mode call site), so introducing it does not change shadow-mode
// divergence telemetry.

@id("capability_grants.forbid_unless_entitled")
forbid (
    principal,
    action,
    resource
) when {
    context has requires_capability &&
    context.requires_capability != "" &&
    (!(principal has capabilities) ||
     !principal.capabilities.contains(context.requires_capability))
};
