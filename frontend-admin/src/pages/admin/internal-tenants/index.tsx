import { useTranslation } from 'react-i18next';
import TenantManagementPage from 'pages/admin/tenants';

/**
 * Internal Tenants admin page (Tier-1 operator tenants).
 *
 * Routed at /admin/internal/tenants. Thin wrapper over the shared
 * TenantManagementPage with kind=internal so the Clients console at
 * /admin/clients stays the sole surface for external-tenant management.
 */
const InternalTenantsPage: React.FC = () => {
  const { t } = useTranslation();
  return (
    <TenantManagementPage
      kind="internal"
      detailPathPrefix="/admin/internal/tenants"
      labels={{
        toolbarTitle: t('adminTenants.wrappers.internalToolbarTitle'),
        totalTitle: t('adminTenants.wrappers.internalTotalTitle'),
        activeTitle: t('adminTenants.active'),
        membersTitle: t('adminTenants.wrappers.internalMembersTitle'),
        createLabel: t('adminTenants.wrappers.internalCreateLabel'),
        createTitle: t('adminTenants.wrappers.internalCreateTitle'),
        createSubmitLabel: t('adminTenants.wrappers.internalCreateSubmit'),
        emptyFootnote: t('adminTenants.internalEmptyFootnote')
      }}
    />
  );
};

export default InternalTenantsPage;
