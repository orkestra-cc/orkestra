import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const SalesProspect = lazy(() => import('pages/sales/prospect'));
const SalesSkill = lazy(() => import('pages/sales/skills'));
const SalesJobs = lazy(() => import('pages/sales/jobs'));
const SalesJobDetail = lazy(() => import('pages/sales/jobs/detail'));
const SalesReports = lazy(() => import('pages/sales/reports'));
const SalesReportDetail = lazy(() => import('pages/sales/reports/detail'));
const SalesSettings = lazy(() => import('pages/sales/settings'));

const salesPerms: [string[]] = [
  ['super_admin', 'administrator', 'developer', 'manager']
];

export const salesManifest: ModuleManifest = {
  name: 'sales',
  routes: () => [
    {
      path: 'sales/prospect',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-prospect" fallback={<OrkestraLoader />}>
              <SalesProspect />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'sales/skills/:skill',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-skill" fallback={<OrkestraLoader />}>
              <SalesSkill />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'sales/jobs',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-jobs" fallback={<OrkestraLoader />}>
              <SalesJobs />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'sales/jobs/:uuid',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-job-detail" fallback={<OrkestraLoader />}>
              <SalesJobDetail />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'sales/reports',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-reports" fallback={<OrkestraLoader />}>
              <SalesReports />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'sales/reports/:uuid',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-report-detail" fallback={<OrkestraLoader />}>
              <SalesReportDetail />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'sales/settings',
      element: (
        <ModuleGate module="sales">
          <ProtectedRoute requiredPermissions={salesPerms}>
            <Suspense key="sales-settings" fallback={<OrkestraLoader />}>
              <SalesSettings />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    }
  ],
  injectApi: () => import('store/api/salesApi')
};
