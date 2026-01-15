/**
 * Utility functions for role-based access control and hierarchy management
 */

export type UserRole = 'developer' | 'ceo' | 'administrator' | 'manager' | 'operator' | 'guest';

/**
 * Role hierarchy from highest to lowest privilege
 */
export const ROLE_HIERARCHY: UserRole[] = [
  'developer',
  'ceo',
  'administrator',
  'manager',
  'operator',
  'guest'
];

/**
 * Mapping from backend roles to frontend roles
 * Backend uses English role names (developer, ceo, administrator, manager, operator, guest)
 */
export const ROLE_MAPPING: Record<string, UserRole> = {
  'developer': 'developer',
  'ceo': 'ceo',
  'administrator': 'administrator',
  'admin': 'administrator', // alias support
  'manager': 'manager',
  'operator': 'operator',
  'guest': 'guest'
};

/**
 * Normalize a role string to a valid UserRole
 *
 * @param role - Role string from backend or other source
 * @returns Normalized UserRole or null if not recognized
 */
export const normalizeRole = (role: string): UserRole | null => {
  if (!role) return null;
  const normalized = ROLE_MAPPING[role.toLowerCase()];
  return normalized || null;
};

/**
 * Get the privilege level of a role (lower number = higher privilege)
 */
export const getRoleLevel = (role: UserRole): number => {
  const level = ROLE_HIERARCHY.indexOf(role);
  return level === -1 ? Infinity : level;
};

/**
 * Check if a user role meets the minimum required role (hierarchical)
 * Higher roles inherit access from lower roles
 *
 * @param userRole - The user's current role
 * @param requiredRole - The minimum required role
 * @returns true if user role meets or exceeds the requirement
 */
export const hasRoleAccess = (userRole: UserRole, requiredRole: UserRole): boolean => {
  return getRoleLevel(userRole) <= getRoleLevel(requiredRole);
};

/**
 * Check if user has any of the specified roles
 *
 * @param userRole - The user's current role
 * @param allowedRoles - Array of roles that grant access
 * @returns true if user has one of the allowed roles
 */
export const hasAnyRole = (userRole: UserRole, allowedRoles: UserRole[]): boolean => {
  return allowedRoles.some(role => hasRoleAccess(userRole, role));
};

/**
 * Filter roles based on user's current role (hierarchical access)
 * Returns only roles that the user can access
 *
 * @param userRole - The user's current role
 * @param targetRoles - Array of roles to filter
 * @returns Array of accessible roles
 */
export const getAccessibleRoles = (userRole: UserRole, targetRoles: UserRole[]): UserRole[] => {
  return targetRoles.filter(role => hasRoleAccess(userRole, role));
};

/**
 * Get all roles that a user can access (including their own role and below)
 *
 * @param userRole - The user's current role
 * @returns Array of roles the user can access
 */
export const getUserAccessibleRoles = (userRole: UserRole): UserRole[] => {
  const userLevel = getRoleLevel(userRole);
  return ROLE_HIERARCHY.slice(userLevel);
};

/**
 * Check if user is developer (highest privilege)
 *
 * @param userRole - The user's current role
 * @returns true if user is developer
 */
export const isDeveloper = (userRole: UserRole): boolean => {
  return userRole === 'developer';
};

/**
 * Check if user is CEO (second highest privilege)
 *
 * @param userRole - The user's current role
 * @returns true if user is CEO
 */
export const isCEO = (userRole: UserRole): boolean => {
  return userRole === 'ceo';
};

/**
 * Check if user is administrator (third highest privilege)
 *
 * @param userRole - The user's current role
 * @returns true if user is administrator
 */
export const isAdministrator = (userRole: UserRole): boolean => {
  return userRole === 'administrator';
};

/**
 * Check if user is developer or CEO
 *
 * @param userRole - The user's current role
 * @returns true if user is developer or CEO
 */
export const isDeveloperOrCEO = (userRole: UserRole): boolean => {
  return hasRoleAccess(userRole, 'ceo');
};

/**
 * Check if user is CEO or administrator
 *
 * @param userRole - The user's current role
 * @returns true if user is CEO or administrator
 */
export const isCEOOrAdministrator = (userRole: UserRole): boolean => {
  return hasRoleAccess(userRole, 'administrator');
};

/**
 * Check if user is administrator or above
 *
 * @param userRole - The user's current role
 * @returns true if user is administrator or CEO
 */
export const isAdminOrAbove = (userRole: UserRole): boolean => {
  return hasRoleAccess(userRole, 'administrator');
};

/**
 * Check if user is manager or above
 *
 * @param userRole - The user's current role
 * @returns true if user is manager, administrator, or CEO
 */
export const isManagerOrAbove = (userRole: UserRole): boolean => {
  return hasRoleAccess(userRole, 'manager');
};

/**
 * Get user role from various possible sources (for flexibility)
 * Handles different auth data structures
 *
 * @param authData - Authentication data object
 * @returns UserRole or null if not found
 */
export const extractUserRole = (authData: any): UserRole | null => {
  // Try different possible property names for role
  // Priority: direct role field, then nested structures for backward compatibility
  const role = authData?.role ||
               authData?.user_role ||
               authData?.user?.role ||
               authData?.data?.role;

  if (!role) {
    return null;
  }

  // Normalize the role
  const normalizedRole = normalizeRole(role);

  if (normalizedRole) {
    return normalizedRole;
  }

  // Log warning in development for debugging
  if (process.env.NODE_ENV === 'development' && authData) {
    console.warn('🔐 Could not extract valid role from auth data:', {
      available_fields: Object.keys(authData),
      role_value: role,
      expected_roles: ROLE_HIERARCHY,
      supported_mappings: Object.keys(ROLE_MAPPING)
    });
  }

  return null;
};

/**
 * Role-based route access configuration
 */
export const ROUTE_ROLE_CONFIG = {
  // Developer only (highest privilege)
  SYSTEM_DEBUG: ['developer'] as UserRole[],
  API_EXPLORER: ['developer'] as UserRole[],
  DATABASE_ADMIN: ['developer'] as UserRole[],
  LOG_VIEWER: ['developer'] as UserRole[],
  FEATURE_FLAGS: ['developer'] as UserRole[],

  // Administrator and above
  USER_MANAGEMENT: ['administrator'] as UserRole[],
  SYSTEM_SETTINGS: ['administrator'] as UserRole[],
  AUDIT_LOGS: ['administrator'] as UserRole[],
  ADVANCED_ANALYTICS: ['administrator'] as UserRole[],
  BUSINESS_REPORTS: ['administrator'] as UserRole[],

  // Manager and above
  TASK_ASSIGNMENT: ['manager'] as UserRole[],
  TEAM_OVERSIGHT: ['manager'] as UserRole[],
  OPERATIONAL_REPORTS: ['manager'] as UserRole[],

  // All authenticated users
  BASIC_DASHBOARD: ['operator'] as UserRole[],
  PROFILE_MANAGEMENT: ['operator'] as UserRole[],
  TASK_EXECUTION: ['operator'] as UserRole[],
  BASIC_TRACKING: ['operator'] as UserRole[]
};