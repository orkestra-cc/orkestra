// src/components/common/SortableGroupList/reorder.ts
// Pure drag-resolution helpers for SortableGroupList. Everything here is
// unit-tested without a DOM; the component only translates dnd-kit events
// into these calls.

import { arrayMove } from '@dnd-kit/sortable';

/** dnd-kit `data.type` of every droppable the list registers. */
export type DragKind = 'group' | 'row' | 'container';

/** Moves `activeId` to `overId`'s slot. Returns the SAME array when either
 *  id is absent so callers can detect the no-op by identity. */
export const reorderIds = (
  ids: string[],
  activeId: string,
  overId: string
): string[] => {
  const from = ids.indexOf(activeId);
  const to = ids.indexOf(overId);
  if (from < 0 || to < 0) return ids;
  return arrayMove(ids, from, to);
};

/** The destination group's authoritative ordered row-id list after dropping
 *  `activeId`. A same-group drop onto a row takes that row's slot
 *  (`arrayMove`, so a downward move lands after it, an upward one before);
 *  otherwise `activeId` is deduped out and spliced in at `overRowId`'s index,
 *  or appended when `overRowId` is null (drop on a band / empty group). */
export const computeTargetOrder = (
  idsInTargetGroup: string[],
  activeId: string,
  overRowId: string | null
): string[] => {
  const from = idsInTargetGroup.indexOf(activeId);
  const to = overRowId ? idsInTargetGroup.indexOf(overRowId) : -1;
  if (from >= 0 && to >= 0) return arrayMove(idsInTargetGroup, from, to);
  const base = idsInTargetGroup.filter(id => id !== activeId);
  const idx = overRowId ? base.indexOf(overRowId) : -1;
  const at = idx < 0 ? base.length : idx;
  base.splice(at, 0, activeId);
  return base;
};

/** Droppable-candidate scoping. A group drag must only resolve to group
 *  nodes; a row drag only to rows or band containers. Filtering before
 *  closestCenter keeps resolution deterministic — the unscoped mixed set
 *  (whole-section group rects, band strips, rows) is ambiguous. */
export const droppableTypesFor = (
  activeType: string | undefined
): DragKind[] => (activeType === 'group' ? ['group'] : ['row', 'container']);

export interface DragEndInput {
  activeId: string;
  activeType: string | undefined;
  overId: string | null;
  overType: string | undefined;
  /** `data.groupId` of a row or container target. */
  overGroupId: string | undefined;
}

export type DragEndResult =
  | { kind: 'groups'; ids: string[] }
  | { kind: 'row'; rowId: string; toGroupId: string; ids: string[] }
  | null;

export const resolveDragEnd = (
  input: DragEndInput,
  groupIds: string[],
  rowIdsByGroup: Record<string, string[]>
): DragEndResult => {
  const { activeId, activeType, overId, overType, overGroupId } = input;
  // No target, or a press-and-release with no movement (over resolves to the
  // active node itself): nothing to do.
  if (!overId || activeId === overId) return null;

  if (activeType === 'group') {
    if (overType !== 'group') return null;
    const ids = reorderIds(groupIds, activeId, overId);
    return ids === groupIds ? null : { kind: 'groups', ids };
  }

  if (activeType !== 'row') return null;

  let toGroupId: string;
  let overRowId: string | null;
  if (overType === 'row') {
    toGroupId = overGroupId ?? '';
    overRowId = overId;
  } else if (overType === 'container') {
    toGroupId = overGroupId ?? '';
    overRowId = null;
  } else if (overType === 'group') {
    toGroupId = overId;
    overRowId = null;
  } else {
    return null;
  }
  const ids = computeTargetOrder(
    rowIdsByGroup[toGroupId] ?? [],
    activeId,
    overRowId
  );
  return { kind: 'row', rowId: activeId, toGroupId, ids };
};
