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
  /** Accessible name for the band's expand/collapse control, e.g.
   *  "Collapse Newsletter". Without it the name is computed from the band's
   *  content, grip and kebab labels included. */
  toggleLabel?: string;
}

/** What a row renders: content right after the grip, meta pinned right. */
export interface RowSlots {
  content: ReactNode;
  meta?: ReactNode;
  /** true ⇒ long content wraps its text BESIDE the meta cluster (the
   *  content group gets a 25% flex-basis floored at its min-content) instead
   *  of pushing the cluster down a line. Default false: the content group is
   *  min-w-0, so `text-truncate` content truncates. */
  contentWraps?: boolean;
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
  /** Group ids and row ids share one DndContext id space: they must be
   *  unique across BOTH sets (UUIDs are fine; small integers in both would
   *  collide). */
  getGroupId: (group: G) => string;
  /** Already ordered. */
  rowsOf: (group: G) => R[];
  /** See getGroupId: unique across groups and rows. */
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
