import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const TransactionsPage = lazy(() => import('pages/payments/transactions'));
const MethodsPage = lazy(() => import('pages/payments/methods'));
const WebhooksPage = lazy(() => import('pages/payments/webhooks'));

const perms: [string[]] = [['super_admin', 'administrator']];

const wrap = (node: React.ReactNode, key: string) => (
  <ModuleGate module="payments">
    <ProtectedRoute requiredPermissions={perms}>
      <Suspense key={key} fallback={<OrkestraLoader />}>
        {node}
      </Suspense>
    </ProtectedRoute>
  </ModuleGate>
);

export const paymentsManifest: ModuleManifest = {
  name: 'payments',
  routes: () => [
    {
      path: 'payments/transactions',
      element: wrap(<TransactionsPage />, 'payments-transactions')
    },
    {
      path: 'payments/methods',
      element: wrap(<MethodsPage />, 'payments-methods')
    },
    {
      path: 'payments/webhooks',
      element: wrap(<WebhooksPage />, 'payments-webhooks')
    }
  ],
  injectApi: () => import('store/api/paymentsApi')
};
