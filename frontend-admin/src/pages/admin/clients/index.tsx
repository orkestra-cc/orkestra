import { useTranslation } from 'react-i18next';
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
const ClientManagementPage: React.FC = () => {
  const { t } = useTranslation();
  return (
    <TenantManagementPage
      kind="external"
      detailPathPrefix="/admin/clients"
      labels={{
        toolbarTitle: t('adminTenants.wrappers.clientsToolbarTitle'),
        totalTitle: t('adminTenants.wrappers.clientsTotalTitle'),
        activeTitle: t('adminTenants.active'),
        membersTitle: t('adminTenants.wrappers.clientsMembersTitle'),
        createLabel: t('adminTenants.wrappers.clientsCreateLabel'),
        createTitle: t('adminTenants.wrappers.clientsCreateTitle'),
        createSubmitLabel: t('adminTenants.wrappers.clientsCreateSubmit'),
        emptyFootnote: t('adminTenants.clientsEmptyFootnote')
      }}
    />
  );
};

export default ClientManagementPage;
