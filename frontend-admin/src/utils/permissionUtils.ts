/**
 * Permission-based access helpers. These replace the old role-hierarchy
 * utilities in roleUtils.ts — Orkestra now computes effective permissions
 * on the backend (via the authz module) and returns them per-org, so the
 * frontend should check for specific permission keys, not role names.
 *
 * Use these with `useAppSelector(selectPermissions)` from the tenant slice:
 *
 *     const perms = useAppSelector(selectPermissions);
 *     if (hasPermission(perms, 'billing.invoice.create')) { ... }
 *
 * Or through the dedicated selectors in tenantSlice:
 *
 *     const canCreate = useAppSelector(selectHasPermission('billing.invoice.create'));
 */

/** True when the user holds `required`. The `*` wildcard grants everything. */
export const hasPermission = (permissions: string[], required: string): boolean => {
  if (!permissions || permissions.length === 0) return false;
  return permissions.includes('*') || permissions.includes(required);
};

/** True when the user holds every permission in `required`. */
export const hasAllPermissions = (permissions: string[], required: string[]): boolean => {
  if (permissions.includes('*')) return true;
  return required.every((p) => permissions.includes(p));
};

/** True when the user holds any permission in `required`. */
export const hasAnyPermission = (permissions: string[], required: string[]): boolean => {
  if (permissions.includes('*')) return true;
  return required.some((p) => permissions.includes(p));
};

/**
 * Groups a list of permission keys by their module prefix, e.g.
 * ["billing.invoice.create", "billing.customer.manage", "rag.query"]
 * becomes { billing: [...], rag: [...] }. Useful for rendering the
 * role editor where permissions are organized by module.
 */
export const groupPermissionsByModule = (permissions: string[]): Record<string, string[]> => {
  const out: Record<string, string[]> = {};
  for (const p of permissions) {
    const dot = p.indexOf('.');
    const module = dot >= 0 ? p.slice(0, dot) : 'other';
    if (!out[module]) out[module] = [];
    out[module].push(p);
  }
  return out;
};

/**
 * Returns true when the given feature is included in the tenant's plan.
 * The `*` wildcard grants all features (enterprise plan default).
 */
export const hasFeature = (features: string[], feature: string): boolean => {
  if (!features) return false;
  return features.includes('*') || features.includes(feature);
};
