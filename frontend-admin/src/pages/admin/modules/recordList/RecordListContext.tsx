import { createContext, useContext } from 'react';

/**
 * What a `recordList` needs from the page around it, threaded by context
 * rather than by prop.
 *
 * `ModuleConfigFields` sits three layers below the controller that owns this
 * state (page → rail panel → field renderer), and a record list is one field
 * type among eight. Adding six props to every one of those components — each
 * of which would then have to be forwarded by every caller, including a
 * fork's own — to serve a single branch is the wrong trade.
 *
 * A missing provider is a legitimate state, not an error: the standalone
 * field-renderer tests mount `ModuleConfigFields` on its own. Consumers read
 * `undefined` and render the list read-only rather than throwing.
 */
export interface RecordListEditingContext {
  /** Element slugs for one field, in order, including unsaved additions. */
  rosterFor: (field: string) => string[];
  /** Slugs of that field marked for removal at the next save. */
  stagedFor: (field: string) => string[];
  create: (field: string, slug: string, label: string) => void;
  stageRemove: (field: string, slug: string) => void;
  undoRemove: (field: string, slug: string) => void;
}

const RecordListContext = createContext<RecordListEditingContext | undefined>(
  undefined
);

export const RecordListProvider = RecordListContext.Provider;

export const useRecordListEditing = (): RecordListEditingContext | undefined =>
  useContext(RecordListContext);
