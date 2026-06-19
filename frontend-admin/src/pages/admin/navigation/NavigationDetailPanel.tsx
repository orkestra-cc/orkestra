import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import classNames from 'classnames';
import SubtleBadge from 'components/common/SubtleBadge';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  visibilityCell,
  type AdminNavItem,
  type NavVisibilityCell,
  type TenantKind
} from 'types/navigation';

interface Props {
  item: AdminNavItem | null;
  roles: string[];
}

const Row: React.FC<{ label: string; children: React.ReactNode }> = ({
  label,
  children
}) => (
  <div className="d-flex justify-content-between gap-3 small py-1 border-bottom">
    <span className="text-muted">{label}</span>
    <span className="text-end">{children}</span>
  </div>
);

const TENANT_KINDS: TenantKind[] = ['internal', 'external'];

const NavigationDetailPanel: React.FC<Props> = ({ item, roles }) => {
  const { t } = useTranslation();
  if (!item) {
    return (
      <Card className="shadow-none border">
        <Card.Body className="text-muted small">
          {t('adminNavigation.detail.empty')}
        </Card.Body>
      </Card>
    );
  }

  // One visibility cell rendered as a check (visible) or a dash carrying the
  // hidden-reason in its tooltip. Mirrors the tree-row tri-state colouring.
  const Cell: React.FC<{ cell: NavVisibilityCell }> = ({ cell }) => {
    if (cell.visible) {
      return (
        <FontAwesomeIcon
          icon="check"
          className="text-success"
          title={t('adminNavigation.matrix.reason.role_below_min')}
        />
      );
    }
    const amber = cell.reason !== 'role_below_min';
    return (
      <FontAwesomeIcon
        icon={amber ? 'exclamation-triangle' : 'minus'}
        className={classNames(amber ? 'text-warning' : 'text-400')}
        title={t(`adminNavigation.matrix.reason.${cell.reason}`)}
      />
    );
  };

  return (
    <Card className="shadow-none border">
      <Card.Header>
        <h6 className="mb-0">{item.name}</h6>
        {item.path && <code className="small text-muted">{item.path}</code>}
      </Card.Header>
      <Card.Body>
        <Row label={t('adminNavigation.detail.itemKey')}>
          <code>{item.itemKey}</code>
        </Row>
        <Row label={t('adminNavigation.detail.module')}>
          {item.moduleName}
          {!item.moduleEnabled && (
            <SubtleBadge bg="secondary" className="ms-2">
              {t('adminNavigation.badges.moduleDisabled')}
            </SubtleBadge>
          )}
        </Row>
        <Row label={t('adminNavigation.detail.realm')}>{item.realm || '—'}</Row>
        <Row label={t('adminNavigation.detail.section')}>
          {item.section || item.group || '—'}
        </Row>
        <Row label={t('adminNavigation.detail.tier')}>
          {item.tier || t('adminNavigation.detail.tierBoth')}
        </Row>
        <Row label={t('adminNavigation.detail.minRole')}>
          {item.minRole || t('adminNavigation.detail.everyone')}
        </Row>
        {item.requiresConfig && (
          <Row label={t('adminNavigation.detail.configGate')}>
            <code>{item.requiresConfig}</code>
            <SubtleBadge
              bg={item.configSatisfied ? 'success' : 'warning'}
              className="ms-2"
            >
              {item.configSatisfied
                ? t('adminNavigation.detail.configOn')
                : t('adminNavigation.detail.configOff')}
            </SubtleBadge>
          </Row>
        )}
        <Row label={t('adminNavigation.detail.declaredOrder')}>
          #{item.declaredOrder}
        </Row>
        <Row label={t('adminNavigation.detail.effectiveOrder')}>
          #{item.effectiveOrder}
          {item.overridden && (
            <SubtleBadge bg="warning" className="ms-2">
              {t('adminNavigation.badges.reordered')}
            </SubtleBadge>
          )}
        </Row>

        <div className="mt-3">
          <div className="text-700 small fw-semibold">
            {t('adminNavigation.detail.visibility')}
          </div>
          <div className="text-muted mb-2" style={{ fontSize: '0.7rem' }}>
            {t('adminNavigation.detail.visibilityHint')}
          </div>
          <table className="table table-sm align-middle mb-0 small">
            <thead>
              <tr className="text-muted">
                <th className="fw-normal" />
                <th className="fw-normal text-center">
                  {t('adminNavigation.detail.tenantInternal')}
                </th>
                <th className="fw-normal text-center">
                  {t('adminNavigation.detail.tenantExternal')}
                </th>
              </tr>
            </thead>
            <tbody>
              {roles.map(role => (
                <tr key={role}>
                  <td className="text-900">{role}</td>
                  {TENANT_KINDS.map(kind => (
                    <td key={kind} className="text-center">
                      <Cell cell={visibilityCell(item, role, kind)} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {item.requiresConfig && (
          <p className="text-muted mt-2 mb-0" style={{ fontSize: '0.7rem' }}>
            {t('adminNavigation.detail.configHint', {
              module: item.moduleName
            })}
          </p>
        )}
        <p className="text-muted mt-2 mb-0" style={{ fontSize: '0.7rem' }}>
          {t('adminNavigation.detail.perOrgNote')}
        </p>
      </Card.Body>
    </Card>
  );
};

export default NavigationDetailPanel;
