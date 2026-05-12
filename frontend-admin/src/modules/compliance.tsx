import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import OrkestraLoader from 'components/common/OrkestraLoader';

// Compliance is a core-like addon — always enabled in the backend so SOC2
// auditors see uninterrupted coverage. It therefore skips the usual
// ModuleGate; the ProtectedRoute + the backend's `system.compliance.audit.read`
// permission together are the access controls.
const AuditEventsPage = lazy(() => import('pages/admin/audit-events'));
const Soc2EvidencePage = lazy(() => import('pages/admin/compliance/soc2'));
const UserPrivacyPage = lazy(() => import('pages/user/privacy'));

export const complianceManifest: ModuleManifest = {
  name: 'compliance',
  routes: () => [
    {
      path: 'admin/audit-events',
      element: (
        <ProtectedRoute
          requiredPermissions={[['super_admin', 'administrator', 'developer']]}
        >
          <Suspense key="admin-audit-events" fallback={<OrkestraLoader />}>
            <AuditEventsPage />
          </Suspense>
        </ProtectedRoute>
      )
    },
    {
      path: 'admin/compliance/soc2',
      element: (
        <ProtectedRoute
          requiredPermissions={[['super_admin', 'administrator', 'developer']]}
        >
          <Suspense key="admin-compliance-soc2" fallback={<OrkestraLoader />}>
            <Soc2EvidencePage />
          </Suspense>
        </ProtectedRoute>
      )
    },
    {
      // GDPR DSR self-service — any authenticated user can export / erase
      // their own data. No role gate beyond auth; the backend enforces
      // userUUID-scoped access via the JWT.
      path: 'user/privacy',
      element: (
        <ProtectedRoute>
          <Suspense key="user-privacy" fallback={<OrkestraLoader />}>
            <UserPrivacyPage />
          </Suspense>
        </ProtectedRoute>
      )
    }
  ],
  injectApi: () => import('store/api/complianceApi')
};
