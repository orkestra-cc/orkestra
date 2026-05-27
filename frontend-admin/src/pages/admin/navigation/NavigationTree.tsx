import { useMemo } from 'react';
import { Button } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import {
  DndContext,
  DragEndEvent,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  useSortable,
  verticalListSortingStrategy
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  usePatchNavigationOrderMutation,
  useDeleteNavigationOrderMutation
} from 'store/api/navigationAdminApi';
import type { AdminNavItem, AdminNavRealm } from 'types/navigation';
import { sectionRootKey } from 'types/navigation';
import NavigationTreeRow from './NavigationTreeRow';

interface Props {
  realms: AdminNavRealm[];
  realmsParentKey: string;
  realmsOverridden: boolean;
  roles: string[];
  showRoleMatrix: boolean;
  moduleFilter: string;
  search: string;
  selectedKey: string | null;
  onSelect: (item: AdminNavItem) => void;
}

// SortableRealm wraps one realm card as a sortable item. Click anywhere
// on the header (apart from the chevron-like badges, of which there are
// none today) doesn't do anything other than drag — realms have no
// detail panel.
const SortableRealm: React.FC<{
  realm: AdminNavRealm;
  children: React.ReactNode;
}> = ({ realm, children }) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging
  } = useSortable({ id: realm.key });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.6 : 1
  };

  return (
    <div ref={setNodeRef} style={style}>
      <div
        className="d-flex align-items-center gap-2 mb-2 py-1 px-2 rounded user-select-none"
        style={{ cursor: 'grab' }}
        {...attributes}
        {...listeners}
      >
        <FontAwesomeIcon
          icon="grip-lines"
          className="text-500 me-1"
          size="lg"
          aria-hidden
        />
        <h6 className="text-uppercase text-700 mb-0">{realm.label}</h6>
      </div>
      {children}
    </div>
  );
};

// itemMatchesFilters returns true when the item itself OR any descendant
// matches the active filter, so a matching grandchild keeps its parents
// visible. Filters are AND-ed.
const itemMatchesFilters = (
  item: AdminNavItem,
  moduleFilter: string,
  search: string
): boolean => {
  const haystack = `${item.name} ${item.path ?? ''}`.toLowerCase();
  const selfMatch =
    (!moduleFilter || item.moduleName === moduleFilter) &&
    (!search || haystack.includes(search.toLowerCase()));
  if (selfMatch) return true;
  if (item.children) {
    return item.children.some(c => itemMatchesFilters(c, moduleFilter, search));
  }
  return false;
};

const NavigationTree: React.FC<Props> = ({
  realms,
  realmsParentKey,
  realmsOverridden,
  roles,
  showRoleMatrix,
  moduleFilter,
  search,
  selectedKey,
  onSelect
}) => {
  const { t } = useTranslation();
  const [patchOrder] = usePatchNavigationOrderMutation();
  const [deleteOrder] = useDeleteNavigationOrderMutation();

  const sensors = useSensors(
    // 5px activation distance — prevents clicks from triggering drags.
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } })
  );

  const filteredRealms = useMemo(() => {
    if (!moduleFilter && !search) return realms;
    return realms
      .map(r => ({
        ...r,
        sections: r.sections
          .map(s => ({
            ...s,
            items: s.items.filter(it =>
              itemMatchesFilters(it, moduleFilter, search)
            )
          }))
          .filter(s => s.items.length > 0)
      }))
      .filter(r => r.sections.length > 0);
  }, [realms, moduleFilter, search]);

  const persistOrder = async (parentKey: string, orderedChildren: string[]) => {
    try {
      await patchOrder({ parentKey, orderedChildren }).unwrap();
      toast.success(t('adminNavigation.toast.orderSaved'));
    } catch {
      toast.error(t('adminNavigation.toast.orderFailed'));
    }
  };

  const clearOverride = async (parentKey: string) => {
    try {
      await deleteOrder({ parentKey }).unwrap();
      toast.success(t('adminNavigation.toast.overrideCleared'));
    } catch {
      toast.error(t('adminNavigation.toast.clearFailed'));
    }
  };

  // Per-parent drag handler. We resolve the parent context via the
  // sibling list passed in, then reorder + PATCH.
  const makeHandleDragEnd =
    (parentKey: string, siblings: AdminNavItem[]) => (e: DragEndEvent) => {
      const { active, over } = e;
      if (!over || active.id === over.id) return;
      const oldIndex = siblings.findIndex(s => s.itemKey === active.id);
      const newIndex = siblings.findIndex(s => s.itemKey === over.id);
      if (oldIndex === -1 || newIndex === -1) return;
      const next = arrayMove(siblings, oldIndex, newIndex);
      persistOrder(
        parentKey,
        next.map(s => s.itemKey)
      );
    };

  const renderSiblings = (
    parentKey: string,
    siblings: AdminNavItem[],
    anyOverridden: boolean
  ) => (
    <>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={makeHandleDragEnd(parentKey, siblings)}
      >
        <SortableContext
          items={siblings.map(s => s.itemKey)}
          strategy={verticalListSortingStrategy}
        >
          <ul className="list-unstyled mb-0">
            {siblings.map(item => (
              <li key={item.itemKey}>
                <NavigationTreeRow
                  item={item}
                  roles={roles}
                  showRoleMatrix={showRoleMatrix}
                  selected={item.itemKey === selectedKey}
                  onSelect={onSelect}
                />
                {item.children && item.children.length > 0 && (
                  <div className="ms-3 border-start ps-2">
                    {renderSiblings(
                      item.itemKey,
                      item.children,
                      item.children.some(c => c.overridden)
                    )}
                  </div>
                )}
              </li>
            ))}
          </ul>
        </SortableContext>
      </DndContext>
      {anyOverridden && (
        <div className="mt-2">
          <Button
            variant="link"
            size="sm"
            className="text-decoration-none p-0"
            onClick={() => clearOverride(parentKey)}
          >
            {t('adminNavigation.actions.resetParent')}
          </Button>
        </div>
      )}
    </>
  );

  if (filteredRealms.length === 0) {
    return (
      <p className="text-muted mb-0 small">
        {t('adminNavigation.emptyFilters')}
      </p>
    );
  }

  // Realm-level drag handler: reorder the realm cards themselves and
  // PATCH with parentKey = realmsParentKey (the "__realms__" sentinel
  // the server echoed). Self-heals if a realm key vanishes.
  const handleRealmDragEnd = (e: DragEndEvent) => {
    const { active, over } = e;
    if (!over || active.id === over.id) return;
    const oldIndex = filteredRealms.findIndex(r => r.key === active.id);
    const newIndex = filteredRealms.findIndex(r => r.key === over.id);
    if (oldIndex === -1 || newIndex === -1) return;
    const next = arrayMove(filteredRealms, oldIndex, newIndex);
    persistOrder(
      realmsParentKey,
      next.map(r => r.key)
    );
  };

  return (
    <div className="d-flex flex-column gap-3">
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleRealmDragEnd}
      >
        <SortableContext
          items={filteredRealms.map(r => r.key)}
          strategy={verticalListSortingStrategy}
        >
          {filteredRealms.map(realm => (
            <SortableRealm key={realm.key} realm={realm}>
              {realm.sections.map(section => {
                const parentKey = sectionRootKey(realm.key, section.label);
                const anyOverridden = section.items.some(it => it.overridden);
                return (
                  <div key={section.label} className="mb-3">
                    <div className="small text-muted mb-1">{section.label}</div>
                    {renderSiblings(parentKey, section.items, anyOverridden)}
                  </div>
                );
              })}
            </SortableRealm>
          ))}
        </SortableContext>
      </DndContext>

      {realmsOverridden && (
        <div>
          <Button
            variant="link"
            size="sm"
            className="text-decoration-none p-0"
            onClick={() => clearOverride(realmsParentKey)}
          >
            {t('adminNavigation.actions.resetRealms')}
          </Button>
        </div>
      )}
    </div>
  );
};

export default NavigationTree;
