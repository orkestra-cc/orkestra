import { Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  selectCurrentMembership,
  selectImpersonation,
  startImpersonation,
  stopImpersonation
} from 'store/slices/tenantSlice';
import { baseApi, TENANT_SCOPED_TAGS } from 'store/api/baseApi';
import type { Org } from 'store/api/tenantApi';

interface Props {
  org: Org;
}

/**
 * Per-tenant "Impersonate" affordance on /admin/clients/:tenantUUID. Same
 * gate + dispatch as the impersonation panel in NineDotMenu
 * (system.tenants.admin holders acting in an internal tenant), but
 * contextual to the tenant the operator is already looking at — saves the
 * round-trip through the navbar dropdown's search.
 *
 * The backend's tryImpersonationBypass enforces the actual permission. If
 * the target is a personal tenant (IsCompany=false + signupChannel=
 * self_serve) the next request returns 401 step_up_required and the
 * existing MFA modal handles the prompt — no extra UX needed here.
 */
const ImpersonateButton: React.FC<Props> = ({ org }) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const { hasPermission } = useAuth();
  const currentMembership = useAppSelector(selectCurrentMembership);
  const { tenantId: impersonatedId } = useAppSelector(selectImpersonation);

  const canImpersonate =
    hasPermission('system.tenants.admin') &&
    currentMembership?.kind === 'internal';

  if (!canImpersonate) return null;

  const isThisTenant = impersonatedId === org.id;

  const onImpersonate = () => {
    dispatch(startImpersonation({ tenantId: org.id, tenantName: org.name }));
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
    toast.info(t('adminClients.impersonate.toastStarted', { name: org.name }));
  };

  const onStop = () => {
    dispatch(stopImpersonation());
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
    toast.info(t('adminClients.impersonate.toastStopped'));
  };

  if (isThisTenant) {
    return (
      <Button variant="warning" size="sm" onClick={onStop}>
        <FontAwesomeIcon icon="user-shield" className="me-2" />
        {t('adminClients.impersonate.stop')}
      </Button>
    );
  }

  return (
    <Button variant="outline-warning" size="sm" onClick={onImpersonate}>
      <FontAwesomeIcon icon="user-shield" className="me-2" />
      {t('adminClients.impersonate.start')}
    </Button>
  );
};

export default ImpersonateButton;
