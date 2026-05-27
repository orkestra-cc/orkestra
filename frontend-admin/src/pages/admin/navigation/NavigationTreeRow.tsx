import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import classNames from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import type { AdminNavItem } from 'types/navigation';

interface Props {
  item: AdminNavItem;
  roles: string[];
  showRoleMatrix: boolean;
  selected: boolean;
  onSelect: (item: AdminNavItem) => void;
}

// roleHierarchy ranks system roles highest privilege first. Mirrors the
// backend's roleRank in navigation services/dynamic_navigation.go and
// the frontend's ROLE_HIERARCHY constant.
const ROLE_RANK: Record<string, number> = {
  super_admin: 6,
  administrator: 5,
  developer: 4,
  manager: 3,
  operator: 2,
  guest: 1
};

const roleSees = (role: string, minRole: string | undefined): boolean => {
  if (!minRole) return true;
  return (ROLE_RANK[role] ?? 0) >= (ROLE_RANK[minRole] ?? 0);
};

const NavigationTreeRow: React.FC<Props> = ({
  item,
  roles,
  showRoleMatrix,
  selected,
  onSelect
}) => {
  const { t } = useTranslation();
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging
  } = useSortable({ id: item.itemKey });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.6 : 1
  };

  // Whole-row drag affordance: spreading {...listeners} on the outer div
  // makes the entire row the drag activator, not just a tiny grip icon.
  // The PointerSensor's 5px activation distance (see NavigationTree.tsx)
  // ensures a click without movement still fires the row's onClick →
  // onSelect, so the detail-panel selection still works. The grip icon
  // stays as a visual cue; do NOT put listeners on it too — duplicate
  // listeners on a child element confuse the sensor.
  const handleRowClick = (e: React.MouseEvent) => {
    // Defensive guard: if dnd-kit later starts emitting a synthetic
    // click after a drop, isDragging is still true at this exact moment
    // for the dragged element. Skip select on that click.
    if (isDragging) return;
    // Ignore clicks bubbling up from the role-matrix chips (purely
    // informational; clicking them shouldn't change the detail panel).
    const target = e.target as HTMLElement;
    if (target.closest('[data-matrix-chip]')) return;
    onSelect(item);
  };

  return (
    <div
      ref={setNodeRef}
      style={{ ...style, cursor: 'grab' }}
      className={classNames(
        'd-flex align-items-center gap-2 py-1 px-2 rounded user-select-none',
        {
          'bg-primary-subtle': selected,
          'opacity-50': !item.moduleEnabled
        }
      )}
      onClick={handleRowClick}
      aria-label={t('adminNavigation.actions.drag')}
      {...attributes}
      {...listeners}
    >
      <FontAwesomeIcon
        icon="grip-lines"
        className="text-500 me-1"
        aria-hidden
      />
      <span className="fw-semibold text-900">{item.name}</span>
      {item.path && <code className="ms-1 small text-muted">{item.path}</code>}
      {item.overridden && (
        <SubtleBadge bg="warning" className="ms-2">
          {t('adminNavigation.badges.reordered')}
        </SubtleBadge>
      )}
      {!item.moduleEnabled && (
        <SubtleBadge bg="secondary" className="ms-2">
          {t('adminNavigation.badges.moduleDisabled')}
        </SubtleBadge>
      )}
      {item.tier && (
        <SubtleBadge
          bg={item.tier === 'internal' ? 'info' : 'success'}
          className="ms-2"
        >
          {item.tier}
        </SubtleBadge>
      )}

      <span className="small text-muted ms-auto">{item.moduleName}</span>

      {showRoleMatrix && (
        <div className="d-flex gap-1 ms-2">
          {roles.map(role => {
            const visible = roleSees(role, item.minRole);
            return (
              <span
                key={role}
                data-matrix-chip
                title={t('adminNavigation.matrix.tooltip', {
                  role,
                  visibility: visible
                    ? t('adminNavigation.matrix.visible')
                    : t('adminNavigation.matrix.hidden')
                })}
                className={classNames(
                  'd-inline-block rounded-circle',
                  visible ? 'bg-success' : 'bg-200'
                )}
                style={{ width: 10, height: 10 }}
              />
            );
          })}
        </div>
      )}
    </div>
  );
};

export default NavigationTreeRow;
