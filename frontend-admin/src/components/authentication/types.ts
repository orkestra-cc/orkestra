/**
 * TypeScript interfaces for Social authentication components
 */

import { ReactNode } from 'react';
import { SocialProvider } from 'utils/socialAuthUtils';

// Social Auth Types
export interface SocialAuthData {
  authenticated: boolean;
  user_id: string;
  email: string;
  full_name: string;
  avatar: string;
  role: string;
  oauth_provider: SocialProvider;
  scopes?: string[];
  expires_on?: string;
}

export interface SocialAuthResponse {
  success: boolean;
  data?: SocialAuthData;
  error?: string;
  message?: string;
}

// Component Props Types
export interface SocialLoginFormProps {
  backendUrl?: string;
  redirectUrl?: string;
  onError?: (error: Error) => void;
}

export interface SocialAuthCallbackProps {
  // This component doesn't take any props directly
}

export interface LoginProps {
  // This component doesn't take any props directly
}

export interface ModalAuthProps {
  // This component doesn't take any props directly (placeholder component)
}

export interface ProtectedRouteProps {
  children: ReactNode;
  requireAuth?: boolean;
  requiredPermissions?: string[] | string[][];
  fallbackUrl?: string;
  loadingComponent?: ReactNode | null;
}

export interface PublicRouteProps {
  children: ReactNode;
  redirectUrl?: string;
}

export interface WithAuthOptions {
  requireAuth?: boolean;
  requiredPermissions?: string[] | string[][];
  fallbackUrl?: string;
  loadingComponent?: ReactNode | null;
}

// Auth Store Types (for useAuthStore hook interface)
export interface AuthStoreState {
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  user: SocialAuthData | null;
  permissions: string[];
}

export interface AuthStoreActions {
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  clearError: () => void;
  hasPermission: (permission: string) => boolean;
  hasAnyPermission: (permissions: string[]) => boolean;
  isSessionValid: () => boolean;
  login: (authData: SocialAuthData) => void;
  logout: () => void;
}

export type AuthStore = AuthStoreState & AuthStoreActions;

// Location State Types (for React Router navigation)
export interface NavigationState {
  from?: {
    pathname: string;
    search?: string;
    hash?: string;
    state?: any;
  };
  authData?: SocialAuthData;
  requiredPermissions?: string[];
}

// Utility Function Types
export interface InitiateSocialLoginParams {
  backendUrl: string;
  provider: SocialProvider;
  redirectUrl?: string;
}

export interface ProcessSocialAuthResponseResult {
  success: boolean;
  error?: string;
}