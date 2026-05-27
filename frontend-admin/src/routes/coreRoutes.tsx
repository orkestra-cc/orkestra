import { Suspense, lazy } from 'react';
import { Navigate, useParams, type RouteObject } from 'react-router';

// Phase 6 redirect for the previous /admin/external-tenants/:tenantId
// surface. Lives at module scope so it can be referenced from the route
// table without a circular lazy import.
const ExternalTenantToClientsRedirect: React.FC = () => {
  const { tenantId } = useParams<{ tenantId: string }>();
  if (!tenantId) return <Navigate to="/admin/clients" replace />;
  return <Navigate to={`/admin/clients/${tenantId}`} replace />;
};
import App from 'App';
import paths from './paths';
import MainLayout from 'layouts/MainLayout';
import ErrorLayout from 'layouts/ErrorLayout';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import OrkestraLoader from 'components/common/OrkestraLoader';

import Error401 from 'components/errors/Error401';
import Error404 from 'components/errors/Error404';
import Error500 from 'components/errors/Error500';

import Login from 'components/authentication/Login';
import Register from 'components/authentication/Register';
import ForgotPassword from 'components/authentication/ForgotPassword';
import ResetPassword from 'components/authentication/ResetPassword';
import VerifyEmailPage from 'components/authentication/VerifyEmailPage';
import SocialAuthCallback from 'components/authentication/SocialAuthCallback';
import LoginMfaVerify from 'components/authentication/LoginMfaVerify';

const SetupWizard = lazy(() => import('pages/setup/SetupWizard'));
const UserManagement = lazy(() => import('pages/admin/users'));
const ModuleManagement = lazy(() => import('pages/admin/modules'));
const ModuleDetail = lazy(() => import('pages/admin/modules/detail'));
const NavigationAdminPage = lazy(() => import('pages/admin/navigation'));
// ADR-0005 Phase F — observability admin page (runtime log-level mutation).
const LogLevelsPage = lazy(
  () => import('pages/admin/observability/log-levels')
);
const RoleManagement = lazy(() => import('pages/admin/roles'));
const InternalTenants = lazy(() => import('pages/admin/internal-tenants'));
const InternalTenantDetail = lazy(
  () => import('pages/admin/internal-tenants/detail')
);
const ClientManagement = lazy(() => import('pages/admin/clients'));
const ClientDetail = lazy(() => import('pages/admin/clients/detail'));
const AdminUserProfile = lazy(
  () => import('pages/admin/user-profile/AdminUserProfile')
);
const OperatorProfile = lazy(
  () => import('pages/operator/profile/OperatorProfile')
);
const UserDashboard = lazy(() => import('pages/user/dashboard/UserDashboard'));
const UserCalendar = lazy(() => import('pages/user/calendar/UserCalendar'));
const Settings = lazy(() => import('pages/user/settings/Settings'));
const SecurityPage = lazy(() => import('pages/user/security'));

/**
 * Builds the full route tree with core routes + injected module/reference children.
 * Module and reference routes are injected as siblings of the admin/user routes
 * inside the protected MainLayout.
 */
export function buildCoreRoutes(
  additionalChildren: RouteObject[]
): RouteObject[] {
  return [
    {
      element: <App />,
      children: [
        {
          path: 'landing',
          element: <Navigate to="/login" replace />
        },
        {
          path: 'errors',
          element: <ErrorLayout />,
          children: [
            { path: '401', element: <Error401 /> },
            { path: '404', element: <Error404 /> },
            { path: '500', element: <Error500 /> }
          ]
        },
        {
          path: 'setup',
          element: (
            <Suspense key="setup-wizard" fallback={<OrkestraLoader />}>
              <SetupWizard />
            </Suspense>
          )
        },
        { path: 'login', element: <Login /> },
        { path: 'mfa/verify', element: <LoginMfaVerify /> },
        { path: 'register', element: <Register /> },
        { path: 'forgot-password', element: <ForgotPassword /> },
        { path: 'reset-password', element: <ResetPassword /> },
        { path: 'verify-email', element: <VerifyEmailPage /> },
        { path: 'auth/callback', element: <SocialAuthCallback /> },
        {
          path: '/',
          element: (
            <ProtectedRoute>
              <MainLayout />
            </ProtectedRoute>
          ),
          children: [
            {
              index: true,
              element: <Navigate to="/user/dashboard" replace />
            },
            // ── Admin routes (core) ──
            {
              path: 'admin',
              children: [
                {
                  path: 'users',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-userManagement"
                        fallback={<OrkestraLoader />}
                      >
                        <UserManagement />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  path: 'modules',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-modules"
                        fallback={<OrkestraLoader />}
                      >
                        <ModuleManagement />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  path: 'modules/navigation',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[['super_admin', 'administrator']]}
                    >
                      <Suspense
                        key="admin-modules-navigation"
                        fallback={<OrkestraLoader />}
                      >
                        <NavigationAdminPage />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  path: 'modules/:moduleName',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-module-detail"
                        fallback={<OrkestraLoader />}
                      >
                        <ModuleDetail />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  // ADR-0005 Phase F — runtime log-level admin.
                  // Administrator-only by NavItem MinRole; the
                  // ProtectedRoute below is the second gate.
                  path: 'observability/log-levels',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[['super_admin', 'administrator']]}
                    >
                      <Suspense
                        key="admin-observability-log-levels"
                        fallback={<OrkestraLoader />}
                      >
                        <LogLevelsPage />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  path: 'roles',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        [
                          'authz.role.read',
                          'super_admin',
                          'administrator',
                          'developer'
                        ]
                      ]}
                    >
                      <Suspense key="admin-roles" fallback={<OrkestraLoader />}>
                        <RoleManagement />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                // Two-tier split (ADR-0001 Phase 3): legacy /admin/tenants
                // redirects to /admin/clients — most historical traffic here
                // was client-leaning. Operators can deep-link to
                // /admin/internal/tenants for the operator-side view.
                {
                  path: 'tenants',
                  element: <Navigate to="/admin/clients" replace />
                },
                {
                  path: 'internal/tenants',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-internal-tenants"
                        fallback={<OrkestraLoader />}
                      >
                        <InternalTenants />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  path: 'internal/tenants/:tenantId',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-internal-tenant-detail"
                        fallback={<OrkestraLoader />}
                      >
                        <InternalTenantDetail />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  path: 'clients',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-clients"
                        fallback={<OrkestraLoader />}
                      >
                        <ClientManagement />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  // Phase 6 (Unified Client Aggregate): the legacy
                  // /admin/clients/:userId user-detail route and the
                  // /admin/external-tenants/:tenantId tenant-detail route
                  // collapsed into one tenant-centric surface. The page
                  // resolves the param as a tenantUUID first and falls back
                  // to a user → primary-external-tenant lookup so
                  // bookmarks survive.
                  path: 'clients/:tenantUUID',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-client-detail"
                        fallback={<OrkestraLoader />}
                      >
                        <ClientDetail />
                      </Suspense>
                    </ProtectedRoute>
                  )
                },
                {
                  // 301-style redirect — preserves any deep links that
                  // referenced /admin/external-tenants/:tenantId before the
                  // URL merge.
                  path: 'external-tenants/:tenantId',
                  element: <ExternalTenantToClientsRedirect />
                },
                {
                  path: 'user/profile/:userId',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer']
                      ]}
                    >
                      <Suspense
                        key="admin-userProfile"
                        fallback={<OrkestraLoader />}
                      >
                        <AdminUserProfile />
                      </Suspense>
                    </ProtectedRoute>
                  )
                }
              ]
            },
            // ── User / Operator routes (core) ──
            {
              path: 'user',
              children: [
                {
                  path: 'profile',
                  element: (
                    <Suspense
                      key="operator-profile"
                      fallback={<OrkestraLoader />}
                    >
                      <OperatorProfile />
                    </Suspense>
                  )
                },
                {
                  path: 'dashboard',
                  element: (
                    <Suspense
                      key="user-dashboard"
                      fallback={<OrkestraLoader />}
                    >
                      <UserDashboard />
                    </Suspense>
                  )
                },
                {
                  path: 'calendar',
                  element: (
                    <Suspense key="user-calendar" fallback={<OrkestraLoader />}>
                      <UserCalendar />
                    </Suspense>
                  )
                },
                {
                  path: 'settings',
                  element: (
                    <Suspense key="user-settings" fallback={<OrkestraLoader />}>
                      <Settings />
                    </Suspense>
                  )
                },
                {
                  path: 'security',
                  element: (
                    <Suspense key="user-security" fallback={<OrkestraLoader />}>
                      <SecurityPage />
                    </Suspense>
                  )
                }
              ]
            },
            // ── Module + Reference routes (injected) ──
            ...additionalChildren
          ]
        },
        {
          path: '*',
          element: <Navigate to={paths.error404} replace />
        }
      ]
    }
  ];
}
