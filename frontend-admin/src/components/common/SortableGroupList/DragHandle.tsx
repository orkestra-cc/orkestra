// src/components/common/SortableGroupList/DragHandle.tsx
// The one element that carries dnd-kit listeners. A real <button> so it takes
// the theme focus ring; text-600 (not text-400) because a control owes 3:1 on
// the row it sits in. Its click never bubbles into the band toggle.

import type { useSortable } from '@dnd-kit/sortable';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faGripLines } from '@fortawesome/free-solid-svg-icons';

type Sortable = ReturnType<typeof useSortable>;

interface DragHandleProps {
  label: string;
  setActivatorNodeRef: Sortable['setActivatorNodeRef'];
  attributes: Sortable['attributes'];
  listeners: Sortable['listeners'];
}

const DragHandle = ({
  label,
  setActivatorNodeRef,
  attributes,
  listeners
}: DragHandleProps) => (
  <button
    type="button"
    ref={setActivatorNodeRef}
    className="btn btn-link btn-sm p-0 lh-1 text-600 flex-shrink-0"
    style={{ cursor: 'grab', touchAction: 'none' }}
    aria-label={label}
    onClick={e => e.stopPropagation()}
    {...attributes}
    {...listeners}
  >
    <FontAwesomeIcon icon={faGripLines} aria-hidden />
  </button>
);

export default DragHandle;
