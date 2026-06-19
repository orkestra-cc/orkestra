import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import classNames from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  visibilityCell,
  type AdminNavItem,
  type TenantKind
} from 'types/navigation';

interface Props {
  item: AdminNavItem;
  roles: string[];
  showRoleMatrix: boolean;
  tenantKind: TenantKind;
  /** When true, drag-to-reorder is suppressed (preview/simulation mode). */
  readOnly?: boolean;
  selected: boolean;
  onSelect: (item: AdminNavItem) => void;
}

const NavigationTreeRow: React.FC<Props> = ({
  item,
  roles,
  showRoleMatrix,
  tenantKind,
  readOnly = false,
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
  } = useSortable({ id: item.itemKey, disabled: readOnly });

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
      style={{ ...style, cursor: readOnly ? 'pointer' : 'grab' }}
      className={classNames(
        'd-flex align-items-center gap-2 py-1 px-2 rounded user-select-none',
        {
          'bg-primary-subtle': selected,
          'opacity-50': !item.moduleEnabled
        }
      )}
      onClick={handleRowClick}
      aria-label={readOnly ? undefined : t('adminNavigation.actions.drag')}
      {...attributes}
      {...listeners}
    >
      {/* Left cluster: grows and clips (min-w-0) so a long name/path
          truncates with an ellipsis instead of wrapping or shoving the
          right-hand module name + role matrix off the card. */}
      <div className="d-flex align-items-center gap-2 flex-grow-1 min-w-0">
        <FontAwesomeIcon
          icon="grip-lines"
          className={classNames(
            'flex-shrink-0',
            readOnly ? 'text-300' : 'text-500'
          )}
          aria-hidden
        />
        <span className="fw-semibold text-900 flex-shrink-0">{item.name}</span>
        {item.path && (
          <code className="small text-muted text-truncate d-none d-lg-inline">
            {item.path}
          </code>
        )}
        {item.overridden && (
          <SubtleBadge bg="warning" className="flex-shrink-0">
            {t('adminNavigation.badges.reordered')}
          </SubtleBadge>
        )}
        {!item.moduleEnabled && (
          <SubtleBadge bg="secondary" className="flex-shrink-0">
            {t('adminNavigation.badges.moduleDisabled')}
          </SubtleBadge>
        )}
        {item.tier && (
          <SubtleBadge
            bg={item.tier === 'internal' ? 'info' : 'success'}
            className="flex-shrink-0"
          >
            {item.tier}
          </SubtleBadge>
        )}
        {item.requiresConfig && (
          <span
            className="flex-shrink-0 d-inline-flex"
            title={
              item.configSatisfied
                ? t('adminNavigation.badges.configGate', {
                    key: item.requiresConfig
                  })
                : t('adminNavigation.badges.configGateOff', {
                    key: item.requiresConfig
                  })
            }
          >
            <SubtleBadge bg={item.configSatisfied ? 'info' : 'warning'}>
              <FontAwesomeIcon
                icon="sliders-h"
                aria-label={item.requiresConfig}
              />
            </SubtleBadge>
          </span>
        )}
      </div>

      {/* Right cluster: module name + role matrix, pinned and never shrunk. */}
      <span className="small text-muted flex-shrink-0">{item.moduleName}</span>

      {showRoleMatrix && (
        <div className="d-flex gap-1 flex-shrink-0">
          {roles.map(role => {
            const cell = visibilityCell(item, role, tenantKind);
            // Tri-state: visible → green; hidden purely because the role rank
            // is too low → muted gray (expected, low-signal); hidden by any
            // OTHER gate (module/config/tier/parent) → amber, because the role
            // WOULD qualify but something else hides it — the actionable case.
            const chipClass = cell.visible
              ? 'bg-success'
              : cell.reason === 'role_below_min'
                ? 'bg-200'
                : 'bg-warning';
            const title = cell.visible
              ? t('adminNavigation.matrix.cellVisible', { role })
              : t('adminNavigation.matrix.cellHidden', {
                  role,
                  reason: t(`adminNavigation.matrix.reason.${cell.reason}`)
                });
            return (
              <span
                key={role}
                data-matrix-chip
                title={title}
                className={classNames(
                  'd-inline-block rounded-circle',
                  chipClass
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
