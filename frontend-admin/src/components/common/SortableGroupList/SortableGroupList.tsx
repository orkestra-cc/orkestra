// src/components/common/SortableGroupList/SortableGroupList.tsx
// Shared "collapsible group band + sortable rows" list. Owns the dnd-kit
// wiring: one DndContext, a SortableContext over the groups, one per group
// over its rows, and a droppable on every band so a row can land on an empty
// or collapsed group. Drag resolution is the pure resolveDragEnd; pages only
// receive onReorderGroups / onMoveRow. Showcase under
// reference/components/ui/SortableGroupList.tsx.

import { useMemo } from 'react';
import {
  DndContext,
  closestCenter,
  useDroppable,
  type CollisionDetection,
  type DragEndEvent
} from '@dnd-kit/core';
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useGetDndSensor } from 'hooks/ui/useGetDndSensor';
import DragHandle from './DragHandle';
import GroupBand from './GroupBand';
import GroupRow from './GroupRow';
import { droppableTypesFor, resolveDragEnd } from './reorder';
import type { SortableGroupListDnd, SortableGroupListProps } from './types';

interface RowItemProps<G, R> {
  row: R;
  group: G;
  groupId: string;
  rowId: string;
  draggable: boolean;
  renderRow: SortableGroupListProps<G, R>['renderRow'];
  handleLabel?: SortableGroupListDnd<G, R>['handleLabel'];
}

function RowItem<G, R>({
  row,
  group,
  groupId,
  rowId,
  draggable,
  renderRow,
  handleLabel
}: RowItemProps<G, R>) {
  const {
    attributes,
    listeners,
    setNodeRef,
    setActivatorNodeRef,
    transform,
    transition,
    isDragging
  } = useSortable({
    id: rowId,
    data: { type: 'row', groupId },
    disabled: !draggable
  });
  const slots = renderRow(row, group);
  return (
    <GroupRow
      rowId={rowId}
      setNodeRef={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1
      }}
      handle={
        draggable && handleLabel ? (
          <DragHandle
            label={handleLabel.row(row)}
            setActivatorNodeRef={setActivatorNodeRef}
            attributes={attributes}
            listeners={listeners}
          />
        ) : null
      }
      content={slots.content}
      meta={slots.meta}
    />
  );
}

interface GroupSectionProps<G, R> extends SortableGroupListProps<G, R> {
  group: G;
  dndOn: boolean;
}

function GroupSection<G, R>(props: GroupSectionProps<G, R>) {
  const {
    group,
    dndOn,
    getGroupId,
    rowsOf,
    getRowId,
    renderGroup,
    renderRow,
    collapsed,
    onToggle,
    emptyGroup,
    dnd
  } = props;
  const groupId = getGroupId(group);
  const groupDraggable = dndOn && (dnd?.groupDraggable?.(group) ?? true);
  const rowsDraggable = dndOn && (dnd?.rowsDraggable ?? true);

  // The section (band + rows) is the sortable node so a group moves as one;
  // the band alone is the droppable container rows can land on.
  const {
    attributes,
    listeners,
    setNodeRef: setSortableRef,
    setActivatorNodeRef,
    transform,
    transition,
    isDragging
  } = useSortable({
    id: groupId,
    data: { type: 'group' },
    disabled: !groupDraggable
  });
  const { setNodeRef: setDropRef } = useDroppable({
    id: `grp:${groupId}`,
    data: { type: 'container', groupId },
    disabled: !dndOn
  });

  const rows = rowsOf(group);
  const isCollapsed = collapsed.has(groupId);
  const slots = renderGroup(group, {
    collapsed: isCollapsed,
    rowCount: rows.length
  });
  const placeholder =
    rows.length === 0 && emptyGroup ? emptyGroup(group) : null;

  return (
    <div
      ref={setSortableRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1
      }}
      data-group-id={groupId}
    >
      <GroupBand
        collapsed={isCollapsed}
        onToggle={() => onToggle(groupId)}
        dropRef={setDropRef}
        handle={
          groupDraggable && dnd ? (
            <DragHandle
              label={dnd.handleLabel.group(group)}
              setActivatorNodeRef={setActivatorNodeRef}
              attributes={attributes}
              listeners={listeners}
            />
          ) : null
        }
        marker={slots.marker}
        title={slots.title}
        meta={slots.meta}
        actions={slots.actions}
        toggleLabel={slots.toggleLabel}
      />
      {!isCollapsed && (
        <SortableContext
          items={rows.map(getRowId)}
          strategy={verticalListSortingStrategy}
        >
          {rows.length === 0 ? (
            placeholder ? (
              <div className="px-x1 py-3 text-center text-muted fs-11 border-top border-200">
                {placeholder}
              </div>
            ) : null
          ) : (
            rows.map(row => {
              const rowId = getRowId(row);
              return (
                <RowItem
                  key={rowId}
                  row={row}
                  group={group}
                  groupId={groupId}
                  rowId={rowId}
                  draggable={rowsDraggable}
                  renderRow={renderRow}
                  handleLabel={dnd?.handleLabel}
                />
              );
            })
          )}
        </SortableContext>
      )}
    </div>
  );
}

function SortableGroupList<G, R>(props: SortableGroupListProps<G, R>) {
  const { groups, getGroupId, rowsOf, getRowId, dnd, className } = props;
  const sensors = useGetDndSensor();
  const dndOn = !!dnd?.enabled;

  const groupIds = useMemo(() => groups.map(getGroupId), [groups, getGroupId]);
  const rowIdsByGroup = useMemo(() => {
    const map: Record<string, string[]> = {};
    for (const g of groups) map[getGroupId(g)] = rowsOf(g).map(getRowId);
    return map;
  }, [groups, getGroupId, rowsOf, getRowId]);

  // Scope collision candidates by the active drag's type before
  // closestCenter — see droppableTypesFor.
  const collisionDetection: CollisionDetection = args => {
    const wanted = new Set<string>(
      droppableTypesFor(args.active.data.current?.type as string | undefined)
    );
    const scoped = args.droppableContainers.filter(c =>
      wanted.has(c.data.current?.type as string)
    );
    return closestCenter({ ...args, droppableContainers: scoped });
  };

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e;
    const result = resolveDragEnd(
      {
        activeId: String(active.id),
        activeType: active.data.current?.type as string | undefined,
        overId: over ? String(over.id) : null,
        overType: over?.data.current?.type as string | undefined,
        overGroupId: over?.data.current?.groupId as string | undefined
      },
      groupIds,
      rowIdsByGroup
    );
    if (!result) return;
    if (result.kind === 'groups') dnd?.onReorderGroups?.(result.ids);
    else dnd?.onMoveRow?.(result.rowId, result.toGroupId, result.ids);
  };

  const body = (
    <div className={className}>
      {groups.map(group => (
        <GroupSection
          key={getGroupId(group)}
          {...props}
          group={group}
          dndOn={dndOn}
        />
      ))}
    </div>
  );

  // Filtered views render the same tree without a DndContext. The hooks are
  // always called (disabled), so hook order is stable; the subtree IS
  // re-created when `enabled` flips, which is fine because collapse state is
  // controlled by the page.
  if (!dndOn) return body;

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragEnd={onDragEnd}
    >
      <SortableContext items={groupIds} strategy={verticalListSortingStrategy}>
        {body}
      </SortableContext>
    </DndContext>
  );
}

export default SortableGroupList;
