import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import FalconLoader from 'components/common/FalconLoader';

// Identity is a toggleable addon — if an operator disables the module the
// route 503s via ModuleGate. Role gate is administrator+; the backend
// additionally enforces tenant.update on every endpoint.
const IdentityAdminPage = lazy(() => import('pages/identity'));

export const identityManifest: ModuleManifest = {
  name: 'identity',
  routes: () => [
    {
      path: 'identity',
      element: (
        <ModuleGate module="identity">
          <ProtectedRoute
            requiredPermissions={[['super_admin', 'administrator', 'developer']]}
          >
            <Suspense key="identity-admin" fallback={<FalconLoader />}>
              <IdentityAdminPage />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
  ],
  injectApi: () => import('store/api/identityApi'),
};
