import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import type { AdminNavItem } from 'types/navigation';

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
          <div className="text-muted small mb-1">
            {t('adminNavigation.detail.visibleTo')}
          </div>
          <div className="d-flex flex-wrap gap-1">
            {roles.map(role => {
              const visible = roleSees(role, item.minRole);
              return (
                <SubtleBadge key={role} bg={visible ? 'success' : 'secondary'}>
                  {role}
                </SubtleBadge>
              );
            })}
          </div>
        </div>
      </Card.Body>
    </Card>
  );
};

const ROLE_RANK: Record<string, number> = {
  super_admin: 6,
  administrator: 5,
  developer: 4,
  manager: 3,
  operator: 2,
  guest: 1
};
const roleSees = (role: string, minRole?: string) =>
  !minRole || (ROLE_RANK[role] ?? 0) >= (ROLE_RANK[minRole] ?? 0);

export default NavigationDetailPanel;
