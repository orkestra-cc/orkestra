// src/components/common/SortableGroupList/GroupRow.tsx
// One row under a band. ps-5 (3rem) indents it one level so its grip lines up
// with the band's chevron column; flex-wrap + a min-w-0 grip+content group
// let the meta cluster drop under the content on narrow screens instead of
// the content collapsing to nothing.

import type { CSSProperties, ReactNode } from 'react';
import type { RowSlots } from './types';

interface GroupRowProps extends RowSlots {
  rowId: string;
  handle?: ReactNode;
  setNodeRef?: (el: HTMLElement | null) => void;
  style?: CSSProperties;
}

const GroupRow = ({
  rowId,
  handle,
  setNodeRef,
  style,
  content,
  meta
}: GroupRowProps) => (
  <div
    ref={setNodeRef}
    style={style}
    data-row-id={rowId}
    className="d-flex flex-wrap align-items-center gap-2 ps-5 pe-x1 py-2 border-top border-200 fs-10"
  >
    <div className="d-flex align-items-center gap-2 flex-grow-1 min-w-0">
      {handle}
      {content}
    </div>
    {meta && (
      <div className="d-flex flex-wrap align-items-center gap-2 ms-auto">
        {meta}
      </div>
    )}
  </div>
);

export default GroupRow;
