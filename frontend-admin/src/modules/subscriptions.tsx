import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import FalconLoader from 'components/common/FalconLoader';

const ServicesPage = lazy(() => import('pages/subscriptions/services'));
const SubscriptionsPage = lazy(() => import('pages/subscriptions/subscriptions'));
const SubscriptionDetailPage = lazy(() => import('pages/subscriptions/subscriptions/detail'));

const perms: [string[]] = [['super_admin', 'administrator']];

const wrap = (node: React.ReactNode, key: string) => (
  <ModuleGate module="subscriptions">
    <ProtectedRoute requiredPermissions={perms}>
      <Suspense key={key} fallback={<FalconLoader />}>{node}</Suspense>
    </ProtectedRoute>
  </ModuleGate>
);

export const subscriptionsManifest: ModuleManifest = {
  name: 'subscriptions',
  routes: () => [
    { path: 'subscriptions/services', element: wrap(<ServicesPage />, 'subscriptions-services') },
    { path: 'subscriptions/subscriptions', element: wrap(<SubscriptionsPage />, 'subscriptions-subscriptions') },
    {
      path: 'subscriptions/subscriptions/:id',
      element: wrap(<SubscriptionDetailPage />, 'subscriptions-detail'),
    },
  ],
  injectApi: () => import('store/api/subscriptionsApi'),
};
