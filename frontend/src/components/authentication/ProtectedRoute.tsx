import { Navigate, useLocation } from 'react-router';
import { useAuth } from 'hooks/auth/useAuthRTK';
import FalconLoader from 'components/common/FalconLoader';
import { ProtectedRouteProps, PublicRouteProps, WithAuthOptions } from './types';

const ProtectedRoute = ({ 
  children, 
  requireAuth = true, 
  requiredPermissions = [], 
  fallbackUrl = '/login',
  loadingComponent = null 
}: ProtectedRouteProps) => {
  const location = useLocation();
  const {
    isAuthenticated,
    isLoading,
    hasPermission,
    hasAnyPermission,
    permissions
  } = useAuth();

  // Debug logging removed to reduce console noise

  // Show loading state while checking authentication
  if (isLoading) {
    return loadingComponent || <FalconLoader />;
  }

  // Check if authentication is required
  if (requireAuth) {
    // Check if user is authenticated
    if (!isAuthenticated) {
      // Save the attempted location for redirect after login
      return <Navigate 
        to={fallbackUrl} 
        state={{ from: location }} 
        replace 
      />;
    }

    // Check permissions if required
    if (requiredPermissions.length > 0) {
      // Wait for permissions to be loaded before making access decisions
      // If user is authenticated but permissions array is empty, show loading
      const permissionsAreLoading = isAuthenticated && permissions.length === 0 && !isLoading;

      if (permissionsAreLoading) {
        console.log('⏳ Waiting for permissions to load...');
        return loadingComponent || <FalconLoader />;
      }

      const hasRequiredPermission = Array.isArray(requiredPermissions[0])
        ? hasAnyPermission(requiredPermissions.flat()) // If nested array, flatten and check any
        : (requiredPermissions as string[]).every(permission => hasPermission(permission)); // All permissions required

      if (!hasRequiredPermission) {
        // Redirect to 401 (Unauthorized) page with context information
        console.warn('❌ Access denied. Required permissions:', requiredPermissions, 'User permissions:', permissions);
        return <Navigate
          to="/errors/401"
          state={{
            from: location,
            requiredPermissions,
            userPermissions: permissions,
            accessDeniedReason: 'insufficient_permissions'
          }}
          replace
        />;
      }
    }
  }

  // If requireAuth is false, allow access regardless of auth status
  return <>{children}</>;
};

// Convenience component for public routes (redirect to dashboard if authenticated)
export const PublicRoute = ({
  children,
  redirectUrl = '/dashboard/analytics'
}: PublicRouteProps) => {
  const { isAuthenticated } = useAuth();

  if (isAuthenticated) {
    return <Navigate to={redirectUrl} replace />;
  }

  return <>{children}</>;
};

// Higher-order component for protecting routes
export const withAuth = <P extends object>(
  Component: React.ComponentType<P>, 
  options: WithAuthOptions = {}
) => {
  return (props: P) => (
    <ProtectedRoute {...options}>
      <Component {...props} />
    </ProtectedRoute>
  );
};

export default ProtectedRoute;