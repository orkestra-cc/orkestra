import React, { createContext, useContext, useEffect, ReactNode } from 'react';
import { useNavigate, useLocation } from 'react-router';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { setNavigateToLogin } from 'store/api/baseApi';
import { useAppDispatch } from 'store/hooks';
import { logout as logoutAction } from 'store/slices/authSlice';

interface AuthProviderProps {
  children: ReactNode;
}

type AuthStoreType = ReturnType<typeof useAuth>;

export const AuthContext = createContext<AuthStoreType | null>(null);

const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const authStore = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const dispatch = useAppDispatch();

  // Setup navigation callback for automatic redirects from API errors
  useEffect(() => {
    const navigateToLoginCallback = (currentPath?: string) => {
      // Clear any auth state
      dispatch(logoutAction());

      // Navigate to login with the current path for redirect after login
      navigate('/login', {
        state: {
          from: currentPath || location.pathname
        },
        replace: true
      });
    };

    setNavigateToLogin(navigateToLoginCallback);

    // Cleanup on unmount
    return () => setNavigateToLogin(() => {});
  }, [dispatch, navigate, location.pathname]);

  // Timeout to prevent infinite loading if backend is unreachable or slow
  useEffect(() => {
    const timeout = setTimeout(() => {
      if (authStore.isLoading) {
        console.warn('🔐 Auth check timeout - enabling login buttons regardless of auth state');
        console.warn('🔐 Current auth store state:', {
          isLoading: authStore.isLoading,
          isAuthenticated: authStore.isAuthenticated,
          error: authStore.error
        });
        // Auth state is now managed automatically by useAuth hook
      }
    }, 3000); // 3 second timeout for auth check (reduced from 5)

    return () => clearTimeout(timeout);
  }, []); // Run only once on mount

  // Auth state logging removed - handled by useAuth hook

  return (
    <AuthContext.Provider value={authStore}>
      {children}
    </AuthContext.Provider>
  );
};

// Hook to use auth context (provides direct access to the store)
export const useAuthContext = (): AuthStoreType => {
  const store = useContext(AuthContext);
  if (!store) {
    throw new Error('useAuthContext must be used within AuthProvider');
  }
  return store;
};

// Convenience hooks for common auth operations
export const useAuthUser = () => {
  const store = useAuthContext();
  return store.user;
};

export const useIsAuthenticated = (): boolean => {
  const store = useAuthContext();
  return store.isAuthenticated;
};

export const useAuthActions = () => {
  const store = useAuthContext();
  return {
    login: store.login,
    logout: store.logout
  };
};

export default AuthProvider;