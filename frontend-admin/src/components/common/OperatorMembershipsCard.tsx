// "Gruppi" card: the tenants (groups) the signed-in user belongs to. Shared
// by the operator profile (/user/profile) and the personal dashboard
// (/user/dashboard) — promoted here on second use; its strings still live
// under `operatorProfile.memberships.*`, where they were born. Data comes
// from the same GET /v1/tenants query useTenantBootstrap already runs, so
// this only reads the shared RTK Query cache — no new endpoint. "Tenant" is
// surfaced to the user as "Gruppo" (see the it.json copy).

import { Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import SubtleBadge from 'components/common/SubtleBadge';
import { useListMyOrgsQuery } from 'store/api/tenantApi';
import type { Membership } from 'store/slices/tenantSlice';
import type { BadgeColor } from 'components/common/SubtleBadge';

const kindMeta = (
  kind?: string
): { bg: BadgeColor; key: 'kindInternal' | 'kindExternal' } =>
  kind === 'external'
    ? { bg: 'info', key: 'kindExternal' }
    : { bg: 'primary', key: 'kindInternal' };

const OperatorMembershipsCard: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useListMyOrgsQuery(undefined);
  const memberships: Membership[] = data?.memberships ?? [];

  return (
    <Card>
      {/* No titleTag override: OrkestraCardHeader's h5 default is the panel
          title step (DESIGN.md "Title / h4-h5"), and this was the console's
          only card headed at h6 — beside SectionCard on /user/dashboard the
          two panels sat a full 6px apart on the type ramp. */}
      <OrkestraCardHeader
        light
        title={
          <span className="d-flex align-items-center gap-2 text-700">
            <FontAwesomeIcon icon="layer-group" className="text-primary" />
            {t('operatorProfile.memberships.title')}
          </span>
        }
        endEl={
          memberships.length > 0 ? (
            <SubtleBadge bg="primary" pill>
              {memberships.length}
            </SubtleBadge>
          ) : null
        }
      />
      <Card.Body>
        {isLoading ? (
          <div className="text-center py-3">
            <Spinner animation="border" size="sm" />
          </div>
        ) : memberships.length === 0 ? (
          <p className="text-muted fs-10 mb-0">
            {t('operatorProfile.memberships.empty')}
          </p>
        ) : (
          <div className="d-flex flex-column gap-2">
            {memberships.map(m => {
              const kind = kindMeta(m.kind);
              return (
                <div
                  key={m.tenantId}
                  className="d-flex flex-wrap align-items-center gap-2 border rounded bg-body-tertiary px-3 py-2"
                >
                  <FontAwesomeIcon
                    icon="building"
                    className="text-700"
                    aria-hidden
                  />
                  <span className="fw-semibold text-900">{m.name}</span>
                  <SubtleBadge bg={kind.bg} pill className="fs-11">
                    {t(`operatorProfile.memberships.${kind.key}`)}
                  </SubtleBadge>
                  {m.isOwner && (
                    <SubtleBadge bg="success" pill className="fs-11">
                      {t('operatorProfile.memberships.owner')}
                    </SubtleBadge>
                  )}
                  {m.roles.map(role => (
                    <SubtleBadge
                      key={role}
                      bg="secondary"
                      pill
                      className="fs-11 fw-normal"
                    >
                      {role}
                    </SubtleBadge>
                  ))}
                  <span className="ms-auto text-700 fs-11 d-flex align-items-center gap-2">
                    {t('operatorProfile.memberships.plan', { plan: m.plan })}
                    {m.slug && <code className="text-muted">{m.slug}</code>}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </Card.Body>
    </Card>
  );
};

export default OperatorMembershipsCard;
