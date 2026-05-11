import TenantManagementPage from 'pages/admin/tenants';

/**
 * Clients admin page (Tier-2 external tenants).
 *
 * Routed at /admin/clients. The Unified Client Aggregate refactor (Phase 6)
 * collapsed the previous user-centric /admin/clients list and the standalone
 * /admin/external-tenants/:tenantId detail surface into a single
 * tenant-centric /admin/clients[/:tenantUUID] flow — every Tier-2 client is
 * a Tenant{Kind:external}, so the operator console treats the tenant as the
 * primary resource and surfaces members, divisions, subscriptions, payments,
 * and billing identity from there.
 */
const ClientManagementPage: React.FC = () => (
  <TenantManagementPage
    kind="external"
    detailPathPrefix="/admin/clients"
    labels={{
      toolbarTitle: 'Clients',
      totalTitle: 'Clients',
      activeTitle: 'Active',
      membersTitle: 'Client members',
      createLabel: 'New client',
      createTitle: 'Create client',
      createSubmitLabel: 'Create client',
      emptyFootnote: 'All clients are active',
    }}
  />
);

export default ClientManagementPage;
