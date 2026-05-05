import { useCallback } from 'react';
import { useAppSelector, useAppDispatch } from '../../store/hooks';
import {
  login,
  logout,
  setLoading,
  setError,
  clearError,
  updateSession,
  updateUser,
  updatePermissions,
  updatePreferences,
  resetAuthState,
  checkSessionExpiry,
  setUserFromApiResponse,
  refreshSession,
  selectAuth,
  selectUser,
  selectIsAuthenticated,
  selectIsLoading,
  selectAuthError,
  selectPermissions,
  selectPreferences,
  selectHasPermission,
  selectHasAnyPermission,
  selectIsSessionValid,
  selectUserDisplayName
} from '../../store/slices/authSlice';

// Import backend user types
import type { BackendUser } from '../../store/api/authApi';

interface UserPreferences {
  theme: 'light' | 'dark';
  language: string;
  notifications: boolean;
}

export const useAuth = () => {
  const dispatch = useAppDispatch();
  const auth = useAppSelector(selectAuth);
  const user = useAppSelector(selectUser);
  const isAuthenticated = useAppSelector(selectIsAuthenticated);
  const isLoading = useAppSelector(selectIsLoading);
  const error = useAppSelector(selectAuthError);
  const permissions = useAppSelector(selectPermissions);
  const preferences = useAppSelector(selectPreferences);
  const isSessionValid = useAppSelector(selectIsSessionValid);
  const userDisplayName = useAppSelector(selectUserDisplayName);

  const loginUser = useCallback((
    userData: BackendUser
  ) => {
    dispatch(login({ userData }));
  }, [dispatch]);

  const logoutUser = useCallback(() => {
    dispatch(logout());
  }, [dispatch]);

  const setAuthLoading = useCallback((loading: boolean) => {
    dispatch(setLoading(loading));
  }, [dispatch]);

  const setAuthError = useCallback((error: string | null) => {
    dispatch(setError(error));
  }, [dispatch]);

  const clearAuthError = useCallback(() => {
    dispatch(clearError());
  }, [dispatch]);

  const updateAuthSession = useCallback(() => {
    dispatch(updateSession());
  }, [dispatch]);

  const updateUserData = useCallback((updates: Partial<BackendUser>) => {
    dispatch(updateUser(updates));
  }, [dispatch]);

  const updateUserPermissions = useCallback((newPermissions: string[]) => {
    dispatch(updatePermissions(newPermissions));
  }, [dispatch]);

  const updateUserPreferences = useCallback((newPreferences: Partial<UserPreferences>) => {
    dispatch(updatePreferences(newPreferences));
  }, [dispatch]);

  const resetAuth = useCallback(() => {
    dispatch(resetAuthState());
  }, [dispatch]);

  const checkSession = useCallback(() => {
    dispatch(checkSessionExpiry());
  }, [dispatch]);

  const refreshUserSession = useCallback(() => {
    return dispatch(refreshSession());
  }, [dispatch]);

  const setUserFromApi = useCallback((authData: BackendUser | null) => {
    dispatch(setUserFromApiResponse(authData));
  }, [dispatch]);

  const hasPermission = useCallback((permission: string) => {
    return permissions.includes(permission);
  }, [permissions]);

  const hasAnyPermission = useCallback((requiredPermissions: string[]) => {
    return requiredPermissions.some(p => permissions.includes(p));
  }, [permissions]);

  return {
    // State
    auth,
    user,
    isAuthenticated,
    isLoading,
    error,
    permissions,
    preferences,
    isSessionValid,
    userDisplayName,

    // Actions
    login: loginUser,
    logout: logoutUser,
    setLoading: setAuthLoading,
    setError: setAuthError,
    clearError: clearAuthError,
    updateSession: updateAuthSession,
    updateUser: updateUserData,
    updatePermissions: updateUserPermissions,
    updatePreferences: updateUserPreferences,
    resetAuthState: resetAuth,
    checkSessionExpiry: checkSession,
    refreshSession: refreshUserSession,
    setUserFromApiResponse: setUserFromApi,

    // Utility functions
    hasPermission,
    hasAnyPermission
  };
};

export const useAuthUser = () => {
  return useAppSelector(selectUser);
};

export const useIsAuthenticated = () => {
  return useAppSelector(selectIsAuthenticated);
};

export const useAuthLoading = () => {
  return useAppSelector(selectIsLoading);
};

export const useAuthError = () => {
  return useAppSelector(selectAuthError);
};

export const useAuthPermissions = () => {
  return useAppSelector(selectPermissions);
};

export const useUserPreferences = () => {
  return useAppSelector(selectPreferences);
};

export const useHasPermission = (permission: string) => {
  return useAppSelector(selectHasPermission(permission));
};

export const useHasAnyPermission = (permissions: string[]) => {
  return useAppSelector(selectHasAnyPermission(permissions));
};

export const useUserDisplayName = () => {
  return useAppSelector(selectUserDisplayName);
};