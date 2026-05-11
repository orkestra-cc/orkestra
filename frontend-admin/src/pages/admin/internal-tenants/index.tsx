import TenantManagementPage from 'pages/admin/tenants';

/**
 * Internal Tenants admin page (Tier-1 operator tenants).
 *
 * Routed at /admin/internal/tenants. Thin wrapper over the shared
 * TenantManagementPage with kind=internal so the Clients console at
 * /admin/clients stays the sole surface for external-tenant management.
 */
const InternalTenantsPage: React.FC = () => (
  <TenantManagementPage
    kind="internal"
    detailPathPrefix="/admin/internal/tenants"
    labels={{
      toolbarTitle: 'Internal Tenants',
      totalTitle: 'Internal tenants',
      activeTitle: 'Active',
      membersTitle: 'Internal members',
      createLabel: 'New internal tenant',
      createTitle: 'Create internal tenant',
      createSubmitLabel: 'Create tenant',
      emptyFootnote: 'All internal tenants are active',
    }}
  />
);

export default InternalTenantsPage;
