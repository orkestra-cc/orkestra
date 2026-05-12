import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const CompanyLookup = lazy(() => import('pages/company/lookup'));
const CompanyDetail = lazy(() => import('pages/company/lookup/CompanyDetail'));
const CompanySearch = lazy(() => import('pages/company/search'));

export const companyManifest: ModuleManifest = {
  name: 'company',
  routes: () => [
    {
      path: 'company/lookup',
      element: (
        <ModuleGate module="company">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer', 'manager']
            ]}
          >
            <Suspense key="company-lookup" fallback={<OrkestraLoader />}>
              <CompanyLookup />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'company/lookup/:companyId',
      element: (
        <ModuleGate module="company">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer', 'manager']
            ]}
          >
            <Suspense key="company-detail" fallback={<OrkestraLoader />}>
              <CompanyDetail />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'company/search',
      element: (
        <ModuleGate module="company">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer', 'manager']
            ]}
          >
            <Suspense key="company-search" fallback={<OrkestraLoader />}>
              <CompanySearch />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    }
  ],
  injectApi: () => import('store/api/companyApi')
};
