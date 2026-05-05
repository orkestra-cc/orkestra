import { Suspense, lazy } from 'react';
import { Navigate, type RouteObject } from 'react-router';
import App from 'App';
import paths from './paths';
import MainLayout from 'layouts/MainLayout';
import ErrorLayout from 'layouts/ErrorLayout';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import FalconLoader from 'components/common/FalconLoader';

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
const RoleManagement = lazy(() => import('pages/admin/roles'));
const InternalTenants = lazy(() => import('pages/admin/internal-tenants'));
const InternalTenantDetail = lazy(
  () => import('pages/admin/internal-tenants/detail'),
);
const ClientManagement = lazy(() => import('pages/admin/clients'));
const ClientDetail = lazy(() => import('pages/admin/clients/detail'));
const AdminUserProfile = lazy(
  () => import('pages/admin/user-profile/AdminUserProfile')
);
const OperatorProfile = lazy(
  () => import('pages/operator/profile/OperatorProfile')
);
const UserDashboard = lazy(
  () => import('pages/user/dashboard/UserDashboard')
);
const UserCalendar = lazy(
  () => import('pages/user/calendar/UserCalendar')
);
const Settings = lazy(() => import('pages/user/settings/Settings'));

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
          element: <Navigate to="/login" replace />,
        },
        {
          path: 'errors',
          element: <ErrorLayout />,
          children: [
            { path: '401', element: <Error401 /> },
            { path: '404', element: <Error404 /> },
            { path: '500', element: <Error500 /> },
          ],
        },
        {
          path: 'setup',
          element: (
            <Suspense key="setup-wizard" fallback={<FalconLoader />}>
              <SetupWizard />
            </Suspense>
          ),
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
              element: <Navigate to="/user/dashboard" replace />,
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
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-userManagement"
                        fallback={<FalconLoader />}
                      >
                        <UserManagement />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                {
                  path: 'modules',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-modules"
                        fallback={<FalconLoader />}
                      >
                        <ModuleManagement />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                {
                  path: 'modules/:moduleName',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-module-detail"
                        fallback={<FalconLoader />}
                      >
                        <ModuleDetail />
                      </Suspense>
                    </ProtectedRoute>
                  ),
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
                          'developer',
                        ],
                      ]}
                    >
                      <Suspense
                        key="admin-roles"
                        fallback={<FalconLoader />}
                      >
                        <RoleManagement />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                // Two-tier split (ADR-0001 Phase 3): legacy /admin/tenants
                // redirects to /admin/clients — most historical traffic here
                // was client-leaning. Operators can deep-link to
                // /admin/internal/tenants for the operator-side view.
                {
                  path: 'tenants',
                  element: <Navigate to="/admin/clients" replace />,
                },
                {
                  path: 'internal/tenants',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-internal-tenants"
                        fallback={<FalconLoader />}
                      >
                        <InternalTenants />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                {
                  path: 'internal/tenants/:tenantId',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-internal-tenant-detail"
                        fallback={<FalconLoader />}
                      >
                        <InternalTenantDetail />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                {
                  path: 'clients',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-clients"
                        fallback={<FalconLoader />}
                      >
                        <ClientManagement />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                {
                  path: 'clients/:clientId',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-client-detail"
                        fallback={<FalconLoader />}
                      >
                        <ClientDetail />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
                {
                  path: 'user/profile/:userId',
                  element: (
                    <ProtectedRoute
                      requiredPermissions={[
                        ['super_admin', 'administrator', 'developer'],
                      ]}
                    >
                      <Suspense
                        key="admin-userProfile"
                        fallback={<FalconLoader />}
                      >
                        <AdminUserProfile />
                      </Suspense>
                    </ProtectedRoute>
                  ),
                },
              ],
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
                      fallback={<FalconLoader />}
                    >
                      <OperatorProfile />
                    </Suspense>
                  ),
                },
                {
                  path: 'dashboard',
                  element: (
                    <Suspense
                      key="user-dashboard"
                      fallback={<FalconLoader />}
                    >
                      <UserDashboard />
                    </Suspense>
                  ),
                },
                {
                  path: 'calendar',
                  element: (
                    <Suspense
                      key="user-calendar"
                      fallback={<FalconLoader />}
                    >
                      <UserCalendar />
                    </Suspense>
                  ),
                },
                {
                  path: 'settings',
                  element: (
                    <Suspense
                      key="user-settings"
                      fallback={<FalconLoader />}
                    >
                      <Settings />
                    </Suspense>
                  ),
                },
              ],
            },
            // ── Module + Reference routes (injected) ──
            ...additionalChildren,
          ],
        },
        {
          path: '*',
          element: <Navigate to={paths.error404} replace />,
        },
      ],
    },
  ];
}
