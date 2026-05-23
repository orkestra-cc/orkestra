// Marketing module manifest — routes + lazy API injection. Pages are
// mounted on the operator console, gated by ModuleGate (returns 404
// when the backend module is disabled) and ProtectedRoute (gates on
// the operator's system role).
//
// Sidebar entries live on the BACKEND in marketing/module.go::NavItems().
// The React app fetches them from /v1/navigation at boot — adding a
// route here without the matching NavItem produces a reachable URL
// with no menu pointer.

import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const ContactsList = lazy(() => import('pages/marketing/contacts/list'));
const ContactDetail = lazy(() => import('pages/marketing/contacts/detail'));
const TagsPage = lazy(() => import('pages/marketing/tags'));
const CustomFieldsPage = lazy(() => import('pages/marketing/custom-fields'));
const ImportsPage = lazy(() => import('pages/marketing/imports'));
const ImportWizard = lazy(() => import('pages/marketing/imports/wizard'));
const ScoringPage = lazy(() => import('pages/marketing/scoring'));
const ReviewsPage = lazy(() => import('pages/marketing/reviews'));
const CardTypesPage = lazy(() => import('pages/marketing/card-types'));

const perms: [string[]] = [
  ['super_admin', 'administrator', 'developer', 'manager']
];

const wrap = (node: React.ReactNode, key: string) => (
  <ModuleGate module="marketing">
    <ProtectedRoute requiredPermissions={perms}>
      <Suspense key={key} fallback={<OrkestraLoader />}>
        {node}
      </Suspense>
    </ProtectedRoute>
  </ModuleGate>
);

export const marketingManifest: ModuleManifest = {
  name: 'marketing',
  routes: () => [
    {
      path: 'marketing/contacts',
      element: wrap(<ContactsList />, 'marketing-contacts')
    },
    {
      path: 'marketing/contacts/:id',
      element: wrap(<ContactDetail />, 'marketing-contact-detail')
    },
    {
      path: 'marketing/tags',
      element: wrap(<TagsPage />, 'marketing-tags')
    },
    {
      path: 'marketing/custom-fields',
      element: wrap(<CustomFieldsPage />, 'marketing-custom-fields')
    },
    {
      path: 'marketing/imports',
      element: wrap(<ImportsPage />, 'marketing-imports')
    },
    {
      path: 'marketing/imports/new',
      element: wrap(<ImportWizard />, 'marketing-import-wizard')
    },
    {
      path: 'marketing/scoring',
      element: wrap(<ScoringPage />, 'marketing-scoring')
    },
    {
      path: 'marketing/reviews',
      element: wrap(<ReviewsPage />, 'marketing-reviews')
    },
    {
      path: 'marketing/card-types',
      element: wrap(<CardTypesPage />, 'marketing-card-types')
    }
  ],
  injectApi: () => import('store/api/marketingApi')
};
