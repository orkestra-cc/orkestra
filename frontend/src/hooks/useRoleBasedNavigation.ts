/**
 * Custom hook for getting role-filtered navigation from backend API
 *
 * Navigation items are pre-filtered on the backend for security.
 * This hook fetches the filtered navigation and provides loading/error states.
 */

import { useMemo } from 'react';
import { useGetNavigationQuery } from '../store/api/navigationApi';
import { useAuth } from './auth/useAuthRTK';
import type { RouteGroup, NavItem } from '../store/api/navigationApi';

// Re-export types for convenience
export type { RouteGroup, NavItem };

interface UseRoleBasedNavigationResult {
  /** Filtered navigation groups from backend */
  filteredNavigation: RouteGroup[];
  /** Current user's role */
  userRole: string | null;
  /** Whether user is authenticated */
  isAuthenticated: boolean;
  /** Whether navigation is loading */
  isLoading: boolean;
  /** Whether there was an error loading navigation */
  isError: boolean;
  /** Error details if any */
  error: unknown;
  /** Function to manually refetch navigation */
  refetch: () => void;
}

/**
 * Hook to get role-filtered navigation from backend API
 *
 * Navigation items are pre-filtered on the backend based on user's role.
 * This approach is more secure as role requirements are never exposed to the frontend.
 *
 * @example
 * ```tsx
 * const { filteredNavigation, isLoading, isError } = useRoleBasedNavigation();
 *
 * if (isLoading) return <LoadingSpinner />;
 * if (isError) return <ErrorMessage />;
 *
 * return <Navigation groups={filteredNavigation} />;
 * ```
 */
export const useRoleBasedNavigation = (): UseRoleBasedNavigationResult => {
  const { isAuthenticated } = useAuth();

  // Fetch navigation from backend (skip if not authenticated)
  const {
    data: navigationData,
    isLoading,
    isError,
    error,
    refetch,
  } = useGetNavigationQuery(undefined, {
    skip: !isAuthenticated,
  });

  const result = useMemo((): UseRoleBasedNavigationResult => {
    if (!isAuthenticated || !navigationData) {
      return {
        filteredNavigation: [],
        userRole: null,
        isAuthenticated,
        isLoading,
        isError,
        error,
        refetch,
      };
    }

    return {
      filteredNavigation: navigationData.groups,
      userRole: navigationData.userRole,
      isAuthenticated,
      isLoading,
      isError,
      error,
      refetch,
    };
  }, [isAuthenticated, navigationData, isLoading, isError, error, refetch]);

  return result;
};

/**
 * Hook to check if current user can access a specific route path
 *
 * Note: This checks against the pre-filtered navigation from the backend.
 * For security, the definitive access check should always be on the backend.
 *
 * @param path - The route path to check access for
 * @returns Whether the path is accessible based on filtered navigation
 */
export const useCanAccessRoute = (path: string): boolean => {
  const { filteredNavigation, isLoading } = useRoleBasedNavigation();

  return useMemo(() => {
    if (isLoading) return false;

    const checkPath = (items: NavItem[]): boolean => {
      for (const item of items) {
        if (item.to === path) return true;
        if (item.children && checkPath(item.children)) return true;
      }
      return false;
    };

    for (const group of filteredNavigation) {
      if (checkPath(group.children)) return true;
    }

    return false;
  }, [filteredNavigation, path, isLoading]);
};

export default useRoleBasedNavigation;
