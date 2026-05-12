import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const BillingDashboard = lazy(() => import('pages/billing/dashboard'));
const SupplierManagement = lazy(() => import('pages/billing/suppliers'));
const IssuedInvoices = lazy(() => import('pages/billing/invoices/issued'));
const ReceivedInvoices = lazy(() => import('pages/billing/invoices/received'));
const SDINotifications = lazy(() => import('pages/billing/notifications'));
const NewIssuedInvoice = lazy(
  () => import('pages/billing/invoices/issued/NewIssuedInvoice')
);
const IssuedInvoiceDetail = lazy(
  () => import('pages/billing/invoices/issued/IssuedInvoiceDetail')
);
const ReceivedInvoiceDetail = lazy(
  () => import('pages/billing/invoices/received/ReceivedInvoiceDetail')
);
const CompanyManagement = lazy(() => import('pages/billing/companies'));
const DocumentTemplates = lazy(() => import('pages/documents/templates'));

export const billingManifest: ModuleManifest = {
  name: 'billing',
  routes: () => [
    {
      path: 'billing/dashboard',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense key="billing-dashboard" fallback={<OrkestraLoader />}>
              <BillingDashboard />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/suppliers',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense key="billing-suppliers" fallback={<OrkestraLoader />}>
              <SupplierManagement />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/invoices/issued/new',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense
              key="billing-invoices-issued-new"
              fallback={<OrkestraLoader />}
            >
              <NewIssuedInvoice />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/invoices/issued/:invoiceId',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense
              key="billing-invoices-issued-detail"
              fallback={<OrkestraLoader />}
            >
              <IssuedInvoiceDetail />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/invoices/issued',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense key="billing-invoices-issued" fallback={<OrkestraLoader />}>
              <IssuedInvoices />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/invoices/received',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense
              key="billing-invoices-received"
              fallback={<OrkestraLoader />}
            >
              <ReceivedInvoices />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/invoices/received/:invoiceId',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense
              key="billing-invoices-received-detail"
              fallback={<OrkestraLoader />}
            >
              <ReceivedInvoiceDetail />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/notifications',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense key="billing-notifications" fallback={<OrkestraLoader />}>
              <SDINotifications />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'billing/companies',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer']
            ]}
          >
            <Suspense key="billing-companies" fallback={<OrkestraLoader />}>
              <CompanyManagement />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'documents/templates',
      element: (
        <ModuleGate module="billing">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer', 'manager']
            ]}
          >
            <Suspense key="document-templates" fallback={<OrkestraLoader />}>
              <DocumentTemplates />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    }
  ],
  injectApi: () =>
    Promise.all([
      import('store/api/billingApi'),
      import('store/api/documentsApi')
    ])
};
