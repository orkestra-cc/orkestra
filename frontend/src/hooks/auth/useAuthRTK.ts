import { useCallback, useEffect, useMemo, useState } from 'react';
import { toast } from 'react-toastify';
import { useAppSelector, useAppDispatch } from 'store/hooks';
import {
  useGetSessionQuery,
  useLoginMutation,
  useLogoutMutation,
  type BackendUser,
  type LoginCredentials
} from 'store/api/authApi';
import {
  setUserFromApiResponse,
  setAccessToken,
  logout as logoutAction,
  selectAuth,
  selectPreferences
} from 'store/slices/authSlice';
import { selectPermissions as selectTenantPermissions } from 'store/slices/tenantSlice';

/**
 * Enhanced auth hook using RTK Query for server state and Redux for client state
 * This replaces the TanStack Query-based useAuth hook
 */
export const useAuth = () => {
  const dispatch = useAppDispatch();


  // Redux selectors for client-side auth state. Permissions now live on
  // the tenant slice (computed per-org by the authz module) rather than
  // on the auth slice, since they are org-scoped.
  const auth = useAppSelector(selectAuth);
  const tenantPermissions = useAppSelector(selectTenantPermissions);
  const preferences = useAppSelector(selectPreferences);

  // Check if logout is in progress (dynamic check)
  const [skipSessionQuery, setSkipSessionQuery] = useState(() => {
    const logoutFlag = sessionStorage.getItem('logout_in_progress');
    if (logoutFlag) {
      console.log('🧹 Found stale logout_in_progress flag, clearing it');
      sessionStorage.removeItem('logout_in_progress');
      return false; // Don't skip after clearing
    }
    return false; // Never skip on normal initialization
  });

  // RTK Query hooks for server state - use session endpoint for initialization
  const {
    data: sessionData,
    isLoading: isAuthLoading,
    error: authError,
    refetch: refetchAuthStatus
  } = useGetSessionQuery(undefined, {
    // Only skip during active logout operations
    skip: skipSessionQuery,
    // No polling - session will be refreshed on demand when needed
  });

  // Extract user data from session response
  const currentUser = sessionData?.user || null;

  // Effective permissions shown to ProtectedRoute and hasPermission.
  // We merge the org-scoped tenant permissions with the user's global
  // system role so that:
  //   1) legacy role-name guards like [['developer','administrator']]
  //      keep working immediately after login, before any org is selected,
  //   2) the array is never empty for an authenticated user — which
  //      unblocks ProtectedRoute's permissionsAreLoading spinner for
  //      accounts that don't yet have an organization membership,
  //   3) real permission keys from the authz evaluator still take effect
  //      once useTenantBootstrap resolves an orgId.
  const permissions = useMemo(() => {
    const merged = [...tenantPermissions];
    const systemRole = currentUser?.role;
    if (systemRole && !merged.includes(systemRole)) {
      merged.push(systemRole);
    }
    return merged;
  }, [tenantPermissions, currentUser?.role]);

  // Debug logging in development (throttled to reduce noise)
  useEffect(() => {
    if (process.env.NODE_ENV === 'development') {
      const logoutInProgress = sessionStorage.getItem('logout_in_progress');
      const debugInfo = {
        sessionData: sessionData === undefined ? 'undefined' : sessionData === null ? 'null' : 'has_data',
        currentUser: currentUser ? 'has_user' : 'no_user',
        isAuthLoading,
        authError: authError ? 'has_error' : 'no_error',
        logoutInProgress: logoutInProgress ? 'true' : 'false',
        skipSessionQuery,
        querySkipped: skipSessionQuery
      };

      // Only log on significant state changes
      const isSignificantChange = !isAuthLoading && (sessionData !== undefined);
      if (isSignificantChange) {
        console.log('🔍 useAuth State:', debugInfo);
      }
    }
  }, [isAuthLoading, sessionData, currentUser, authError]);

  // Profile functionality removed - using currentUser data only
  const userProfile = null;
  const isProfileLoading = false;
  const profileError = null;
  const refetchUserProfile = () => Promise.resolve();

  // Mutations
  const [loginMutation, { isLoading: isLogging }] = useLoginMutation();
  const [logoutMutation, { isLoading: isLoggingOut }] = useLogoutMutation();
  // Profile update functionality removed
  const isUpdatingProfile = false;

  // Sync RTK Query session data with Redux state
  useEffect(() => {
    // Only update when query is not loading (has completed)
    if (!isAuthLoading) {
      if (sessionData) {
        // Set user data from session response
        dispatch(setUserFromApiResponse(sessionData.user));

        // Set access token from session response
        if (sessionData.accessToken) {
          dispatch(setAccessToken({
            accessToken: sessionData.accessToken,
            expiresIn: sessionData.expiresIn
          }));
        }
      } else if (sessionData === null) {
        // Explicitly null means unauthenticated
        dispatch(setUserFromApiResponse(null));
      }
    }
  }, [sessionData, isAuthLoading, dispatch]);

  // Login function
  const login = useCallback(async (credentials: LoginCredentials) => {
    try {
      const result = await loginMutation(credentials).unwrap();

      // Sync successful login with Redux state
      dispatch(setUserFromApiResponse(result.user));

      toast.success('Login successful!', {
        toastId: 'login-success',
        autoClose: 3000,
      });

      return { success: true, user: result.user };
    } catch (error: any) {
      toast.error(error?.data?.message || 'Login failed. Please try again.', {
        toastId: 'login-error',
        autoClose: 5000,
      });
      throw error;
    }
  }, [loginMutation, dispatch]);

  // Logout function
  const logout = useCallback(async () => {
    try {
      // Skip session queries during logout
      setSkipSessionQuery(true);

      await logoutMutation().unwrap();

      // Redux state is cleared in the mutation's onQueryStarted
      toast.success('Logged out successfully', {
        toastId: 'logout-success',
        autoClose: 3000,
      });

      // Re-enable session queries after logout
      setSkipSessionQuery(false);

      return { success: true };
    } catch (error: any) {
      console.error('Logout failed:', error);

      // Even if logout fails server-side, clear client state
      localStorage.removeItem('access_token');
      dispatch(logoutAction());

      // Re-enable session queries even if logout failed
      setSkipSessionQuery(false);

      toast.error('Logout failed. Please try again.', {
        toastId: 'logout-error',
        autoClose: 5000,
      });

      return { success: false, error };
    }
  }, [logoutMutation, dispatch, setSkipSessionQuery]);

  // Update user profile - removed (profile endpoints no longer exist)
  const updateProfile = useCallback(async (_updates: any) => {
    toast.error('Profile update functionality has been removed', {
      toastId: 'profile-update-removed',
      autoClose: 5000,
    });
    throw new Error('Profile update functionality has been removed');
  }, []);

  // Permission helpers. The `*` wildcard grants all permissions and is
  // issued to users with the developer system role.
  const hasPermission = useCallback((permission: string) => {
    if (permissions.includes('*')) return true;
    return permissions.includes(permission);
  }, [permissions]);

  const hasAnyPermission = useCallback((requiredPermissions: string[]) => {
    if (permissions.includes('*')) return true;
    return requiredPermissions.some(p => permissions.includes(p));
  }, [permissions]);

  // Current user with enhanced data from Redux state
  const enrichedUser = useCallback(() => {
    if (currentUser) {
      return {
        ...currentUser,
        permissions,
        preferences,
        // Legacy compatibility fields
        displayName: currentUser.fullName,
        name: currentUser.fullName,
        userId: currentUser.id
      };
    }
    return null;
  }, [currentUser, permissions, preferences]);

  // Loading states
  const isLoading = isAuthLoading || isProfileLoading || isLogging || isLoggingOut;

  // Error handling
  const error = authError || profileError;

  return {
    // State
    auth,
    user: enrichedUser(),
    userProfile,
    currentUser,
    isAuthenticated: !!currentUser?.isActive,
    isLoading,
    error,
    permissions,
    preferences,

    // Loading states
    isAuthLoading,
    isProfileLoading,
    isLogging,
    isLoggingOut,
    isUpdatingProfile,

    // Actions
    login,
    logout,
    updateProfile,
    refetchAuthStatus,
    refetchUserProfile,

    // Utility functions
    hasPermission,
    hasAnyPermission,

    // Legacy compatibility
    setUserFromApiResponse: (data: BackendUser | null) => dispatch(setUserFromApiResponse(data)),
    clearError: () => {}, // Handled by RTK Query automatically
  };
};

// Simplified hooks for specific use cases
export const useCurrentUser = () => {
  const { user, isLoading, error } = useAuth();
  return {
    user,
    isLoading,
    error,
    isAuthenticated: !!user,
  };
};

export const useAuthStatus = () => {
  const { currentUser, isAuthLoading, error, refetchAuthStatus } = useAuth();
  return {
    data: currentUser,
    isLoading: isAuthLoading,
    error: error,
    refetch: refetchAuthStatus,
  };
};

export const useLogout = () => {
  const { logout, isLoggingOut } = useAuth();
  return {
    mutate: logout,
    isLoading: isLoggingOut,
  };
};