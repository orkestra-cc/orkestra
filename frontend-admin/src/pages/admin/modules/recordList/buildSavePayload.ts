import type { RecordListMutation } from 'store/api/moduleApi';
import type { ConfigValues, PendingCreates } from '../useModuleConfigForm';

export interface SavePayloadInput {
  config: ConfigValues;
  secrets: ConfigValues;
  created: PendingCreates;
  stagedRemovals: PendingCreates;
  /** The revision the displayed environment was fetched at. */
  revision: number;
}

export interface SavePayload {
  config?: ConfigValues;
  secrets?: ConfigValues;
  recordLists?: RecordListMutation[];
  revision?: number;
}

/**
 * Assembles the environment PATCH body.
 *
 * Membership travels as **intent**, never inferred from which keys happen to
 * be present: the backend applies `create` and `remove` against the roster it
 * has stored, so a request that merely wrote an element's keys would leave
 * that element out of the roster entirely, and one that merely stopped
 * writing them would leave the element in place with stale values. When
 * nothing joined or left, the block is omitted and the request behaves
 * exactly as it did before record lists existed.
 *
 * The revision rides along **only when something is being removed**. It is a
 * compare-and-swap the backend refuses on mismatch, and a removal is the one
 * operation that must not be replayed against state the operator did not see
 * — it destroys keys, secrets included. An add carries no revision on
 * purpose: two operators each adding an element is a compatible outcome the
 * backend retries into, and guarding it would turn that into a needless
 * conflict. A revision of 0 is a real expectation (a document written before
 * record lists existed has none), so it is sent as 0 rather than dropped.
 */
export const buildSavePayload = (input: SavePayloadInput): SavePayload => {
  const { config, secrets, created, stagedRemovals, revision } = input;

  const payload: SavePayload = {
    config: Object.keys(config).length > 0 ? config : undefined,
    secrets: Object.keys(secrets).length > 0 ? secrets : undefined
  };

  const fields = [
    ...new Set([...Object.keys(created), ...Object.keys(stagedRemovals)])
  ].filter(
    field =>
      (created[field]?.length ?? 0) > 0 ||
      (stagedRemovals[field]?.length ?? 0) > 0
  );
  if (fields.length === 0) return payload;

  payload.recordLists = fields.map(field => ({
    field,
    create: created[field] ?? [],
    remove: stagedRemovals[field] ?? []
  }));
  if (payload.recordLists.some(m => m.remove.length > 0)) {
    payload.revision = revision;
  }
  return payload;
};
