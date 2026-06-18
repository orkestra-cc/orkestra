import { Link } from 'react-router';
import { Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';
import {
  faLock,
  faLockOpen,
  faUserShield
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import type { BadgeColor } from 'components/common/SubtleBadge';
import {
  useGetProvisioningPolicyQuery,
  type ProvisioningMode
} from 'store/api/tenantApi';

// ProvisioningPolicyCard renders the read-only tenant-creation policy for one
// tier (internal | external) in the ERP KPI style (left accent border + faded
// 3x icon + big value). The mode itself is edited at /admin/modules/tenant;
// this card just surfaces the current state so an operator on the tenant page
// understands why creation may be restricted, with a deep link to change it.
interface ProvisioningPolicyCardProps {
  tier: 'internal' | 'external';
}

const modeMeta: Record<
  ProvisioningMode,
  { icon: IconProp; color: BadgeColor }
> = {
  open: { icon: faLockOpen, color: 'success' },
  manual: { icon: faUserShield, color: 'warning' },
  single: { icon: faLock, color: 'danger' }
};

const ProvisioningPolicyCard = ({ tier }: ProvisioningPolicyCardProps) => {
  const { t } = useTranslation();
  const { data, isLoading } = useGetProvisioningPolicyQuery();

  const mode: ProvisioningMode = data
    ? tier === 'internal'
      ? data.internal
      : data.external
    : 'open';
  const count = data
    ? tier === 'internal'
      ? data.internalCount
      : data.externalCount
    : 0;
  const meta = modeMeta[mode] ?? modeMeta.open;

  return (
    <Card className={`h-100 border-start border-4 border-${meta.color}`}>
      <Card.Body>
        <div className="d-flex align-items-center justify-content-between">
          <div>
            <h6 className="text-muted mb-1">
              {t('adminTenants.provisioning.title')}
            </h6>
            <h3 className="mb-0 fw-bold text-900">
              {isLoading ? (
                <Spinner animation="border" size="sm" />
              ) : (
                t(`adminTenants.provisioning.mode.${mode}`)
              )}
            </h3>
            <small className="text-muted">
              {t('adminTenants.provisioning.activeCount', { count })}
            </small>
          </div>
          <div className={`text-${meta.color}`}>
            <FontAwesomeIcon
              icon={meta.icon}
              size="3x"
              className="opacity-75"
            />
          </div>
        </div>
        <div className="mt-3">
          <Link
            to="/admin/modules/tenant"
            className="fs-10 fw-semibold text-decoration-none"
          >
            {t('adminTenants.provisioning.manage')}
          </Link>
        </div>
      </Card.Body>
    </Card>
  );
};

export default ProvisioningPolicyCard;
