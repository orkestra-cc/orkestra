/**
 * Utility functions for role-based access control and hierarchy management.
 *
 * The six system roles, from most to least privileged:
 *   super_admin   — full power, wildcard permission, assigns every role
 *   administrator — all org permissions, cannot elevate peers to admin
 *   developer     — technical power user, cannot manage admin/super_admin
 *   manager       — read/create/update, no delete, no admin
 *   operator      — read + self-service
 *   guest         — read-only
 */

export type UserRole =
  | 'super_admin'
  | 'administrator'
  | 'developer'
  | 'manager'
  | 'operator'
  | 'guest';

/**
 * Role hierarchy from highest to lowest privilege.
 */
export const ROLE_HIERARCHY: UserRole[] = [
  'super_admin',
  'administrator',
  'developer',
  'manager',
  'operator',
  'guest',
];

/**
 * Mapping from backend role strings to the frontend UserRole enum.
 * `admin` is kept as an alias for `administrator` because some legacy UI
 * code passes the short form.
 */
export const ROLE_MAPPING: Record<string, UserRole> = {
  super_admin: 'super_admin',
  administrator: 'administrator',
  admin: 'administrator',
  developer: 'developer',
  manager: 'manager',
  operator: 'operator',
  guest: 'guest',
};

/**
 * Normalize a role string to a valid UserRole, returning null if the input
 * doesn't match any known role name.
 */
export const normalizeRole = (role: string): UserRole | null => {
  if (!role) return null;
  return ROLE_MAPPING[role.toLowerCase()] ?? null;
};

/**
 * Get the privilege level of a role (lower number = higher privilege).
 */
export const getRoleLevel = (role: UserRole): number => {
  const level = ROLE_HIERARCHY.indexOf(role);
  return level === -1 ? Infinity : level;
};

/**
 * Check whether a user's role meets a minimum required role. Higher roles
 * inherit access from lower roles.
 */
export const hasRoleAccess = (userRole: UserRole, requiredRole: UserRole): boolean => {
  return getRoleLevel(userRole) <= getRoleLevel(requiredRole);
};

/**
 * Check whether a user's role matches any of the specified roles.
 */
export const hasAnyRole = (userRole: UserRole, allowedRoles: UserRole[]): boolean => {
  return allowedRoles.some((role) => hasRoleAccess(userRole, role));
};

/**
 * Filter a list of roles down to the ones the user can access.
 */
export const getAccessibleRoles = (userRole: UserRole, targetRoles: UserRole[]): UserRole[] => {
  return targetRoles.filter((role) => hasRoleAccess(userRole, role));
};

/**
 * Return all roles at or below the user's level — including their own.
 */
export const getUserAccessibleRoles = (userRole: UserRole): UserRole[] => {
  const userLevel = getRoleLevel(userRole);
  return ROLE_HIERARCHY.slice(userLevel);
};

export const isSuperAdmin = (userRole: UserRole): boolean => userRole === 'super_admin';

export const isAdministrator = (userRole: UserRole): boolean => userRole === 'administrator';

export const isDeveloper = (userRole: UserRole): boolean => userRole === 'developer';

export const isAdminOrAbove = (userRole: UserRole): boolean =>
  hasRoleAccess(userRole, 'administrator');

export const isDeveloperOrAbove = (userRole: UserRole): boolean =>
  hasRoleAccess(userRole, 'developer');

export const isManagerOrAbove = (userRole: UserRole): boolean =>
  hasRoleAccess(userRole, 'manager');

/**
 * Extract a user role from various auth data shapes. Returns null when no
 * recognizable role is present.
 */
export const extractUserRole = (authData: any): UserRole | null => {
  const role =
    authData?.role ||
    authData?.user_role ||
    authData?.user?.role ||
    authData?.data?.role;

  if (!role) {
    return null;
  }

  const normalizedRole = normalizeRole(role);
  if (normalizedRole) {
    return normalizedRole;
  }

  if (process.env.NODE_ENV === 'development' && authData) {
    console.warn('Could not extract valid role from auth data', {
      available_fields: Object.keys(authData),
      role_value: role,
      expected_roles: ROLE_HIERARCHY,
      supported_mappings: Object.keys(ROLE_MAPPING),
    });
  }

  return null;
};

/**
 * Route-based access configuration. Each entry declares the minimum role
 * required for a given feature area; anything above that level inherits
 * access via `hasRoleAccess`.
 */
export const ROUTE_ROLE_CONFIG = {
  // super_admin only
  SYSTEM_DEBUG: ['super_admin'] as UserRole[],
  DATABASE_ADMIN: ['super_admin'] as UserRole[],
  FEATURE_FLAGS: ['super_admin'] as UserRole[],

  // developer and above (technical tools)
  API_EXPLORER: ['developer'] as UserRole[],
  LOG_VIEWER: ['developer'] as UserRole[],

  // administrator and above
  USER_MANAGEMENT: ['administrator'] as UserRole[],
  SYSTEM_SETTINGS: ['administrator'] as UserRole[],
  AUDIT_LOGS: ['administrator'] as UserRole[],
  ADVANCED_ANALYTICS: ['administrator'] as UserRole[],
  BUSINESS_REPORTS: ['administrator'] as UserRole[],

  // manager and above
  TASK_ASSIGNMENT: ['manager'] as UserRole[],
  TEAM_OVERSIGHT: ['manager'] as UserRole[],
  OPERATIONAL_REPORTS: ['manager'] as UserRole[],

  // all authenticated users
  BASIC_DASHBOARD: ['operator'] as UserRole[],
  PROFILE_MANAGEMENT: ['operator'] as UserRole[],
  TASK_EXECUTION: ['operator'] as UserRole[],
  BASIC_TRACKING: ['operator'] as UserRole[],
};
