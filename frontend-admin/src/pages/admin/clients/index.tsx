import TenantManagementPage from 'pages/admin/tenants';

/**
 * Client Management page (Tier-2 external tenants).
 *
 * Routed at /admin/clients. Thin wrapper over the shared
 * TenantManagementPage with kind=external + rootsOnly so nested sub-tenants
 * (divisions) don't clutter the root client list — they surface through the
 * parent's Divisions tab in Phase 4.
 */
const ClientManagementPage: React.FC = () => (
  <TenantManagementPage
    kind="external"
    rootsOnly
    detailPathPrefix="/admin/clients"
    labels={{
      toolbarTitle: 'Client Management',
      totalTitle: 'Clients',
      activeTitle: 'Active clients',
      membersTitle: 'Client members',
      createLabel: 'New client',
      createTitle: 'Create client',
      createSubmitLabel: 'Create client',
      emptyFootnote: 'All clients are active',
    }}
  />
);

export default ClientManagementPage;
