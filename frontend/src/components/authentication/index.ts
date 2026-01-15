// Authentication Components - Barrel Exports
export { default as SocialLoginForm } from './SocialLoginForm';
export { default as ProtectedRoute, PublicRoute, withAuth } from './ProtectedRoute';
export { default as SocialAuthCallback } from './SocialAuthCallback';

// Authentication Layout Components
export { default as Login } from './Login';
export { default as ModalAuth } from './modal/ModalAuth';

// Export TypeScript types for external usage
export type {
  SocialAuthData,
  SocialAuthResponse,
  SocialLoginFormProps,
  SocialAuthCallbackProps,
  LoginProps,
  ModalAuthProps,
  ProtectedRouteProps,
  PublicRouteProps,
  WithAuthOptions,
  AuthStoreState,
  AuthStoreActions,
  AuthStore,
  NavigationState,
  InitiateSocialLoginParams,
  ProcessSocialAuthResponseResult
} from './types';