// src/components/common/SortableGroupList/GroupBand.tsx
// The collapsible group header. Presentational: the optional drag handle and
// the droppable ref are handed in by SortableGroupList. Order: handle ·
// chevron · marker · title · meta · actions. The whole band toggles on click /
// Enter / Space; the actions slot stops propagation so a kebab never toggles.
// `data-group-id` lives on the section wrapper in SortableGroupList.tsx, not
// here — this band can be a drop target for a bare group with no wrapper of
// its own to key off, so only one element in the tree carries the id.

import type { MouseEvent, ReactNode } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faChevronDown,
  faChevronRight
} from '@fortawesome/free-solid-svg-icons';
import type { GroupSlots } from './types';

interface GroupBandProps extends GroupSlots {
  collapsed: boolean;
  onToggle: () => void;
  handle?: ReactNode;
  dropRef?: (el: HTMLElement | null) => void;
}

const stop = (e: MouseEvent) => e.stopPropagation();

const GroupBand = ({
  collapsed,
  onToggle,
  handle,
  dropRef,
  marker,
  title,
  meta,
  actions,
  toggleLabel
}: GroupBandProps) => (
  <div
    ref={dropRef}
    role="button"
    tabIndex={0}
    aria-expanded={!collapsed}
    aria-label={toggleLabel}
    onClick={onToggle}
    onKeyDown={e => {
      // Only a keypress on the band itself toggles: the grip and the kebab
      // live inside the band, and dnd-kit's keyboard sensor picks a group
      // up on Space/Enter without stopping propagation.
      if (e.target !== e.currentTarget) return;
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        onToggle();
      }
    }}
    className="d-flex align-items-center gap-2 px-x1 py-2 bg-body-tertiary fs-10 border-top border-200"
  >
    {handle}
    <FontAwesomeIcon
      icon={collapsed ? faChevronRight : faChevronDown}
      className="text-600 flex-shrink-0"
      aria-hidden
    />
    {marker}
    <span className="fw-semibold text-900 flex-grow-1 text-truncate">
      {title}
    </span>
    {meta && (
      <span className="d-flex align-items-center gap-2 fs-11 text-600 flex-shrink-0">
        {meta}
      </span>
    )}
    {actions && (
      <span role="presentation" className="flex-shrink-0" onClick={stop}>
        {actions}
      </span>
    )}
  </div>
);

export default GroupBand;
