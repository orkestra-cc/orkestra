// src/components/common/SortableGroupList/types.ts
import type { ReactNode } from 'react';

/** What a group band renders, in band order: marker · title · meta · actions. */
export interface GroupSlots {
  /** Small leading mark (colour dot, icon). Optional. */
  marker?: ReactNode;
  /** The band's name. Rendered semibold in heading ink and truncated. */
  title: ReactNode;
  /** Secondary facts (count, status pill). Rendered fs-11 text-600. */
  meta?: ReactNode;
  /** Trailing controls (a kebab menu). Clicks never toggle the band. */
  actions?: ReactNode;
}

/** What a row renders: content right after the grip, meta pinned right. */
export interface RowSlots {
  content: ReactNode;
  meta?: ReactNode;
}

export interface SortableGroupListDnd<G, R> {
  /** false ⇒ no DndContext and no handles (e.g. while a filter is active). */
  enabled: boolean;
  /** Per-group opt-out (a fixed trailing bucket). The group stays a drop
   *  target for rows. Default: every group is draggable. */
  groupDraggable?: (group: G) => boolean;
  /** Default true. false ⇒ rows carry no handle and never move. */
  rowsDraggable?: boolean;
  onReorderGroups?: (orderedGroupIds: string[]) => void;
  /** Fires for cross-group moves AND same-group reorders. `orderedRowIds`
   *  is the destination group's full, authoritative order after the drop. */
  onMoveRow?: (
    rowId: string,
    toGroupId: string,
    orderedRowIds: string[]
  ) => void;
  handleLabel: {
    group: (group: G) => string;
    row: (row: R) => string;
  };
}

export interface SortableGroupListProps<G, R> {
  /** Already ordered. */
  groups: G[];
  getGroupId: (group: G) => string;
  /** Already ordered. */
  rowsOf: (group: G) => R[];
  getRowId: (row: R) => string;
  renderGroup: (
    group: G,
    ctx: { collapsed: boolean; rowCount: number }
  ) => GroupSlots;
  renderRow: (row: R, group: G) => RowSlots;
  /** Controlled: pair with `useCollapsedSet`. */
  collapsed: Set<string>;
  onToggle: (groupId: string) => void;
  /** Placeholder for an expanded group with no rows. Return a falsy value
   *  (null, '', false) to render nothing for that group; omit the prop to
   *  render nothing for every group. */
  emptyGroup?: (group: G) => ReactNode;
  dnd?: SortableGroupListDnd<G, R>;
  className?: string;
}
