import { useEffect, useMemo, useRef, useState } from 'react';
import { useWatch, type UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import {
  CONFIG_REVISION_STALE,
  moduleApi,
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
import { useAppDispatch } from 'store/hooks';
import {
  buildGroupTree,
  flattenTree,
  unfilledRequiredKeys,
  visibleFields,
  type GroupNode
} from './configModel';
import {
  useModuleConfigForm,
  collectDiff,
  fieldNameOf,
  toSchemaValues,
  EMPTY_CREATES,
  type ConfigFormValues,
  type ConfigValues,
  type PendingCreates
} from './useModuleConfigForm';
import { expandElement, labelKeyOf, rosterOf } from './recordList/expandSchema';
import { useRosterReconciliation } from './recordList/useRosterReconciliation';
import type { RecordListEditingContext } from './recordList/RecordListContext';
import { buildSavePayload } from './recordList/buildSavePayload';
import { translateConfigGroup } from 'helpers/configLabel';

// Module-scope, stable-by-reference fallback for a module with no schema yet
// (still loading, or genuinely declares none). useModuleConfigForm's yup
// resolver memo keys off `schema` by identity — `mod?.configSchema ?? []`
// would mint a brand-new array every render and silently rebuild the whole
// validation schema on every keystroke. Both consumers of this hook
// (ModuleConfigSection and detail/index.tsx) now share this ONE constant
// instead of each keeping their own copy.
const EMPTY_SCHEMA: ConfigField[] = [];

/**
 * The unsaved half of a module's record lists, keyed by field key.
 *
 * A removal is STAGED rather than applied: the element stays on screen,
 * muted, with an Undo, and is only destroyed by the save that carries it.
 * Deleting an element destroys its secrets irreversibly, so the confirmation
 * belongs at Save — where the operator can see everything the save will do —
 * not behind a button that reads like it hides a card.
 */
export interface RecordListEditing extends RecordListEditingContext {
  created: PendingCreates;
  stagedRemovals: PendingCreates;
  /** True when any list has an unsaved membership change. */
  hasMembershipChanges: boolean;
}

export interface ModuleConfigControllerGroupCount {
  key: string;
  label: string;
  count: number;
}

export interface ModuleConfigController {
  schema: ConfigField[];
  groupTree: GroupNode[];
  flatNodes: GroupNode[];
  form: UseFormReturn<ConfigFormValues>;
  defaults: ConfigFormValues;
  /**
   * Schema key → react-hook-form register name (`buildFieldNames`), built
   * once per form by `useModuleConfigForm`. Handed down to every renderer
   * that registers a field, so a dotted key like `email.smtp.host` is never
   * given to RHF verbatim (it would parse it as a path and hide the edit
   * from `dirtyFields` and from the save diff). Every other key on this
   * interface — `visibleKeys`, `dirtyKeys`, `errorKeys`, `perGroup` — is a
   * schema key, matching `GroupNode.fieldKeys`.
   */
  fieldNames: ReadonlyMap<string, string>;
  secretStatus: Record<string, boolean>;
  envLoading: boolean;
  saving: boolean;
  /**
   * Fields of the *whole module* currently visible under their own
   * `dependsOn` — not scoped to any one rail node. Callers intersect this
   * with a specific node's `fieldKeys` to ask "does this panel actually have
   * anything on screen", which `fieldKeys.length` alone cannot answer once a
   * declared-but-conditionally-hidden field is in play (phase 4's OAuth
   * provider credentials before either enable toggle is on).
   */
  visibleKeys: Set<string>;
  dirtyKeys: Set<string>;
  errorKeys: Set<string>;
  dirtyCount: number;
  errorCount: number;
  /**
   * Non-zero groups only, in rail order. Deliberately carries no
   * `onSelect` — unlike `ModuleSaveBar`'s own prop shape, this hook has no
   * way to know how its caller navigates its rail (`ModuleConfigSection`
   * moves its own local `activeKey`; `detail/index.tsx` moves `?section=`).
   * Each caller maps this into `ModuleSaveBar`'s `perGroup`/`errors` shape,
   * adding its own `onSelect`.
   */
  perGroup: ModuleConfigControllerGroupCount[];
  saveBarErrors: ModuleConfigControllerGroupCount[];
  /**
   * Node key → how many of that node's visible required fields are still
   * empty, live against the form. Feeds `ModuleConfigRail`'s `statusFor`
   * so the "to fill" badge tracks edits as the operator types.
   */
  unfilledByGroup: ReadonlyMap<string, number>;
  /**
   * Record-list membership the operator has changed but not saved, and the
   * three intents that move it. Membership travels to the backend as explicit
   * intent — it is never inferred from which keys happen to be in the payload
   * — so it has to be held here rather than derived from form state.
   */
  recordList: RecordListEditing;
  /**
   * Labels of the elements a confirmed save would destroy, or null when no
   * save is waiting on that confirmation. The page renders the modal; the
   * controller decides when one is owed, because it is the only thing that
   * knows what the save will send.
   */
  pendingDeletion: string[] | null;
  /** Proceeds with the save the confirmation was holding. */
  confirmDeletion: () => void;
  /** Abandons it, leaving the removals staged and the form untouched. */
  cancelDeletion: () => void;
  error: string | null;
  success: boolean;
  /**
   * True after a save lost the backend's compare-and-swap
   * (`module.config_revision_stale`). Save stays disabled until a reload has
   * SUCCEEDED: nothing is auto-retried, because a retry would re-send a
   * typed secret and re-decide the change against a state the operator
   * never saw.
   */
  conflict: boolean;
  /**
   * Refetches the environment baseline and re-applies ONLY the operator's
   * dirty fields on top of it — non-secret edits (an intentional clear to
   * '' included) and unsent non-empty secrets — so the diff is recomputed
   * against what the server holds now. Fields the operator never touched
   * adopt the other writer's values. Pending record-list membership —
   * staged removals AND unsaved creates — is discarded on every successful
   * reload, even when this profile's revision did not move (they were
   * decided against a state the 409 says is gone). A failed refetch leaves
   * the conflict latched.
   *
   * It also invalidates the module tag so the parent's module query
   * refetches — an activation is one of the things that causes this
   * conflict, and the `live` badge must not stay stale. A dirty field whose
   * record-list element the other operator removed cannot be re-applied;
   * those entries are counted and reported, never dropped in silence.
   */
  reloadAndReview: () => Promise<void>;
  clearError: () => void;
  onSave: () => Promise<void>;
  handleDiscard: () => void;
}

/**
 * One react-hook-form instance and its surrounding bookkeeping for an
 * entire module's config surface: the env-scoped fetch, the save/discard
 * model (`collectDiff`-based, scoped validation, synchronous secret
 * clearing), and the dirty/error tallies per rail group.
 *
 * Both `ModuleConfigSection` (the config-card-only layout, used when the
 * module doesn't declare enough groups for a full-page rail) and
 * `detail/index.tsx` (the full-page rail layout) consume this hook so there
 * is exactly **one** form per module detail page, fetched once, regardless
 * of which layout renders it. That single-instance property is load-bearing
 * for `detail/index.tsx`'s `useBlocker` registration: react-router supports
 * only one blocker at a time, so the page must be the sole place that calls
 * `useBlocker`, and that registration is only correct if it reads the same
 * dirty state the operator is actually editing — which requires both
 * layouts to share this one instance rather than each creating their own.
 *
 * `mod` may be `undefined` while the caller's own module query is still
 * loading — every hook below runs unconditionally regardless, returning an
 * inert controller (`EMPTY_SCHEMA`, empty tree, zero dirty state) until
 * real data arrives.
 */
export const useModuleConfigController = (
  mod: ModuleConfig | undefined,
  environment: string
): ModuleConfigController => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const {
    data: envConfig,
    isLoading: envLoading,
    refetch: refetchEnv
  } = useGetModuleEnvironmentQuery(
    { name: mod?.moduleName ?? '', environment },
    { skip: !mod || !mod.availableEnvironments?.length }
  );
  const [updateEnv, { isLoading: saving }] =
    useUpdateModuleEnvironmentMutation();

  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [conflict, setConflict] = useState(false);
  /** One dirty field captured by reloadAndReview, re-applied by the re-seed effect. */
  interface DraftEntry {
    name: string;
    value: string;
    secret: boolean;
  }
  // The draft captured by reloadAndReview, consumed by the re-seed effect
  // once the fresh baseline lands. A ref, not state: it must survive the
  // render the refetch triggers without itself causing one. Tagged with the
  // environment it belongs to so a switch mid-reload can never inject it
  // into another profile.
  const pendingDraft = useRef<{
    environment: string;
    entries: DraftEntry[];
  } | null>(null);
  // How many draft entries the last re-seed had to discard. A ref because
  // the re-seed effect and `reloadAndReview`'s own continuation both write
  // `error` and their order is not fixed — whichever runs second reads this
  // and reports the same thing, so the notice cannot be lost to a race.
  const droppedEntries = useRef(0);
  const droppedEditsMessage = (count: number) =>
    t('adminModules.detail.configCard.reloadDroppedEdits', { count });

  const schema = mod?.configSchema ?? EMPTY_SCHEMA;
  // The fetched environment wins once it has loaded; before that (or for a
  // module with no declared environments, which skips the query above) the
  // module-list snapshot is the best available baseline.
  const configSource = envConfig?.configValues ?? mod?.configValues;

  // Record-list membership the operator has changed but not yet saved.
  // Creates reach schema expansion (a new element needs fields to fill in
  // before the save that carries it); staged removals do NOT — the element
  // stays on screen, muted, until Save, so Undo has something to restore.
  const [created, setCreated] = useState<PendingCreates>(EMPTY_CREATES);
  const [stagedRemovals, setStagedRemovals] =
    useState<PendingCreates>(EMPTY_CREATES);
  // Config key → the label typed when that element was created, held until
  // the reconciliation effect below can seed it into the form.
  const [pendingLabels, setPendingLabels] = useState<ConfigValues>({});

  const { form, defaults, fieldNames, expandedSchema } = useModuleConfigForm(
    schema,
    configSource,
    created
  );

  // Re-seed the form whenever the server-known baseline changes — keyed on
  // `configSource`, which IS that baseline: the initial environment fetch
  // resolving, switching environments, or (for a module with no
  // environments) a fresh `mod.configValues` reference from the parent.
  // Keying on `envConfig`/`mod.configValues` separately covered the same
  // triggers plus one that is not a baseline change at all: refreshing the
  // module snapshot while a profile is loaded (what `reloadAndReview` now
  // does for the `live` badge) minted a new `mod.configValues` reference and
  // reset the form under the operator's draft. Still deliberately NOT keyed
  // on `defaults`/`form` — both are recomputed every render, and depending
  // on `defaults` would reset on every keystroke, discarding the very edits
  // the sticky bar exists to accumulate across groups.
  useEffect(() => {
    form.reset(defaults);
    // Pending membership belongs to the baseline that produced it. Carrying a
    // staged removal across an environment switch would arm a destructive
    // save against a profile the operator never looked at.
    setCreated(EMPTY_CREATES);
    setStagedRemovals(EMPTY_CREATES);
    setPendingLabels({});
    // Reload & review: put the operator's DIRTY fields back on top of the
    // fresh baseline. A non-secret edit is re-applied only while it still
    // differs from the new baseline — including an intentional clear to ''
    // when the baseline is non-empty; an edit the other writer already made
    // is no longer a change. A secret's baseline is always '' (never
    // echoed), so a typed secret is always a change.
    const draft = pendingDraft.current;
    pendingDraft.current = null;
    let dropped = 0;
    if (draft && draft.environment === environment) {
      // The roster was rebuilt from the fresh baseline. An edit whose field
      // is no longer registered belongs to an element the other operator
      // removed: `setValue` would write it into a field nothing renders and
      // nothing saves, so the edit would disappear without a word. Counted
      // and reported instead.
      const live = new Set(fieldNames.values());
      for (const { name, value, secret } of draft.entries) {
        if (!live.has(name)) {
          dropped += 1;
          continue;
        }
        if (secret || value !== (defaults[name] ?? '')) {
          form.setValue(name, value, { shouldDirty: true });
        }
      }
    }
    // The save alerts belong to the baseline that produced them. A "save
    // failed" banner from `production` still on screen after switching to
    // `sandbox` reads as a failure against the environment now displayed —
    // and a lingering success tick is just as misleading once the form
    // underneath has been re-seeded from a different source.
    droppedEntries.current = dropped;
    setError(dropped > 0 ? droppedEditsMessage(dropped) : null);
    setSuccess(false);
    // Validate the freshly seeded values once, right here. `mode: 'onChange'`
    // asks react-hook-form to validate a field when *that field* fires a
    // change; it never validates on mount, so a value the backend already
    // stores in violation of its own declared `required`/`min`/`max`/
    // `pattern` would render clean until the operator happened to touch it.
    // Before the form migration these checks were computed inline on every
    // render, so an invalid stored value was red on arrival — this restores
    // that. Deliberately inside the re-seed effect rather than its own
    // mount-only one: a new baseline (environment switch, refetch) brings new
    // values that need the same treatment, and gating on the same deps keeps
    // it from re-running per keystroke.
    void form.trigger();
  }, [configSource]);

  // A roster that moves leaves react-hook-form holding registrations, errors
  // and values for elements that are gone, and holding nothing for ones that
  // have just appeared. Reconciled here, once, against the expanded schema.
  useRosterReconciliation(
    form,
    expandedSchema.map(f => fieldNameOf(fieldNames, f.key)),
    defaults,
    // Re-keyed here, on a render where `fieldNames` already covers the new
    // element — which is exactly why the label is not set at click time.
    Object.fromEntries(
      Object.entries(pendingLabels)
        .filter(([key]) => fieldNames.has(key))
        .map(([key, label]) => [fieldNameOf(fieldNames, key), label])
    )
  );

  const groupTree = useMemo(
    () => buildGroupTree(schema, mod?.configGroups),
    [schema, mod?.configGroups]
  );
  const flatNodes = useMemo(() => flattenTree(groupTree), [groupTree]);

  const secretStatus = envConfig?.secretStatus ?? mod?.secretStatus ?? {};

  // Live values for visibility (dependsOn can reference a field in a
  // different group than the one currently on screen) and for the save
  // bar's cross-group aggregation below.
  const watched = useWatch({ control: form.control }) as ConfigFormValues;
  // Register names in, schema keys out — everything downstream of here
  // (`visibleFields`, `GroupNode.fieldKeys`, the save payload) is keyed by
  // the schema key. Re-keyed over the EXPANDED schema so an element's
  // sub-field values are present for its own `dependsOn` to resolve against.
  const values = toSchemaValues(expandedSchema, watched, fieldNames);
  const { errors, dirtyFields } = form.formState;

  // A record list holds no value of its own, so its dirty/error state is the
  // union of its elements'. Walking the declared schema keeps `dirtyKeys` and
  // `errorKeys` in the same key space as `GroupNode.fieldKeys`, which is what
  // the per-group tallies below intersect against.
  const formNamesOf = (field: ConfigField): string[] => {
    if (field.type !== 'recordList') {
      return [fieldNameOf(fieldNames, field.key)];
    }
    const roster = [
      ...rosterOf(configSource, field.key),
      ...(created[field.key] ?? [])
    ];
    return roster.flatMap(slug =>
      expandElement(field, slug).map(leaf => fieldNameOf(fieldNames, leaf.key))
    );
  };

  // Deliberately NOT useMemo here: react-hook-form mutates its `errors`
  // object in place (same reference across renders even as its content
  // changes), so a memo keyed on `[errors, ...]` silently freezes on the
  // first value it ever saw.
  const visibleKeys = new Set(visibleFields(schema, values).map(f => f.key));
  // Walked schema-first rather than `Object.keys(dirtyFields)`-first: those
  // keys are register names, and intersecting them with `visibleKeys`
  // (schema keys) is what silently emptied both sets for every dotted-key
  // module — `dirtyFields` reported the synthesized `email` branch, which
  // matches no schema key at all.
  // A membership change dirties its record list even though it moves no form
  // field: a removal-only save touches nothing RHF tracks, and without this
  // the save bar would never appear and `useBlocker` would let the operator
  // navigate away from a staged deletion without a word.
  const membershipChanged = (f: ConfigField): boolean =>
    f.type === 'recordList' &&
    ((created[f.key]?.length ?? 0) > 0 ||
      (stagedRemovals[f.key]?.length ?? 0) > 0);

  const dirtyKeys = new Set(
    schema
      .filter(
        f =>
          visibleKeys.has(f.key) &&
          (membershipChanged(f) ||
            formNamesOf(f).some(name => Boolean(dirtyFields[name])))
      )
      .map(f => f.key)
  );
  const errorKeys = new Set(
    schema
      .filter(
        f =>
          visibleKeys.has(f.key) &&
          formNamesOf(f).some(name => Boolean(errors[name]))
      )
      .map(f => f.key)
  );
  const dirtyCount = dirtyKeys.size;
  const errorCount = errorKeys.size;

  const moduleName = mod?.moduleName ?? '';
  // Every field belongs to exactly one node's `fieldKeys`, so summing per
  // node reproduces the total with no double-counting.
  const perGroup = flatNodes
    .map(node => ({
      key: node.key,
      label: translateConfigGroup(t, moduleName, node),
      count: node.fieldKeys.filter(k => dirtyKeys.has(k)).length
    }))
    .filter(g => g.count > 0);

  const saveBarErrors = flatNodes
    .map(node => ({
      key: node.key,
      label: translateConfigGroup(t, moduleName, node),
      count: node.fieldKeys.filter(k => errorKeys.has(k)).length
    }))
    .filter(g => g.count > 0);

  const unfilledKeys = unfilledRequiredKeys(schema, values, secretStatus);
  const unfilledByGroup = new Map<string, number>(
    flatNodes.map(node => [
      node.key,
      node.fieldKeys.filter(k => unfilledKeys.has(k)).length
    ])
  );

  const addTo = (
    prev: PendingCreates,
    field: string,
    slug: string
  ): PendingCreates =>
    (prev[field] ?? []).includes(slug)
      ? prev
      : { ...prev, [field]: [...(prev[field] ?? []), slug] };

  const removeFrom = (
    prev: PendingCreates,
    field: string,
    slug: string
  ): PendingCreates => ({
    ...prev,
    [field]: (prev[field] ?? []).filter(s => s !== slug)
  });

  const recordList: RecordListEditing = {
    created,
    stagedRemovals,
    hasMembershipChanges: Object.values(created)
      .concat(Object.values(stagedRemovals))
      .some(slugs => slugs.length > 0),
    rosterFor: field => [
      ...rosterOf(configSource, field),
      ...(created[field] ?? [])
    ],
    stagedFor: field => stagedRemovals[field] ?? [],
    create: (field, slug, label) => {
      setCreated(prev => addTo(prev, field, slug));
      setPendingLabels(prev => ({ ...prev, [labelKeyOf(field, slug)]: label }));
    },
    stageRemove: (field, slug) => {
      // A slug created in this same session was never stored, so there is
      // nothing for the backend to remove — dropping it from `created` is the
      // whole operation, and staging it would make the request contradict
      // itself (create ∩ remove ≠ ∅ is a 422).
      if ((created[field] ?? []).includes(slug)) {
        setCreated(prev => removeFrom(prev, field, slug));
        return;
      }
      setStagedRemovals(prev => addTo(prev, field, slug));
    },
    undoRemove: (field, slug) =>
      setStagedRemovals(prev => removeFrom(prev, field, slug))
  };

  // Non-null while a save that would destroy elements is waiting for the
  // operator to confirm. Holds the labels, because "delete 2 profiles" is not
  // a question anyone can answer — "delete MailUp SMTP and SES bulk" is.
  const [pendingDeletion, setPendingDeletion] = useState<string[] | null>(null);

  const stagedLabels = (): string[] =>
    Object.entries(stagedRemovals).flatMap(([field, slugs]) =>
      slugs.map(slug => {
        const name = fieldNames.get(labelKeyOf(field, slug));
        const typed = name ? form.getValues(name) : '';
        return typed || slug;
      })
    );

  const onSave = async () => {
    if (!mod) return;
    // Deleting an element destroys its keys — encrypted secrets included —
    // and the backend will not bring them back. Ask here, at the save, rather
    // than behind the Remove button: this is the moment the destruction is
    // actually armed, and the operator can see everything else the save will
    // do at the same time.
    const labels = stagedLabels();
    if (labels.length > 0 && pendingDeletion === null) {
      setPendingDeletion(labels);
      return;
    }
    // Belt and braces: the save bar already disables Save while a conflict is
    // latched, so reaching here means some other path tried to submit against
    // a baseline the operator has not reviewed.
    if (conflict) return;
    setPendingDeletion(null);
    const formValues = form.getValues();
    // Diffed over the EXPANDED schema: an element's sub-fields are the
    // concrete fields that actually hold values, and the record-list
    // container holds none.
    const { config, secrets } = collectDiff(
      expandedSchema,
      formValues,
      defaults,
      fieldNames
    );
    const keysBeingSaved = [...Object.keys(config), ...Object.keys(secrets)];
    // A membership-only save carries no changed keys at all — an element
    // removed without any other edit, or added with every field left at its
    // default. Returning early on an empty key diff would silently do
    // nothing in exactly the case the operator most expects an effect.
    if (keysBeingSaved.length === 0 && !recordList.hasMembershipChanges) return;

    // Validate only the fields actually being sent, not the whole form.
    // The backend itself accepts a stored '' on a required field
    // (`UpdateConfig` writes `configValues[key] || ''`), `buildDefaults`
    // documents it, and `configCompleteness` exists specifically to
    // *report* that state — so a module can legitimately hold an empty
    // required field elsewhere, and blocking every save on whole-form
    // validity would strand an operator editing something unrelated behind
    // a field they can't even see, with no way to unblock themselves short
    // of fixing someone else's incomplete setup.
    //
    // `collectDiff` returns schema keys; `trigger` addresses fields by their
    // register name, so a dotted key handed straight to it would validate
    // nothing at all and vacuously report "valid".
    const valid = await form.trigger(
      keysBeingSaved.map(key => fieldNameOf(fieldNames, key))
    );
    if (!valid) return;

    setError(null);
    setSuccess(false);

    try {
      await updateEnv({
        name: mod.moduleName,
        environment,
        ...buildSavePayload({
          config,
          secrets,
          created,
          stagedRemovals,
          // The revision the environment on screen was fetched at. Absent
          // only before the first fetch resolves, when there is nothing to
          // remove anyway; 0 is a real value, so `??` and not `||`.
          revision: envConfig?.revision ?? 0
        })
      }).unwrap();

      // The membership the save just applied is now the server's, so the
      // pending sets are spent. Cleared before the reset below so the
      // re-render that follows expands the schema against the new roster.
      setCreated(EMPTY_CREATES);
      setStagedRemovals(EMPTY_CREATES);

      // Clears the bar immediately instead of waiting on the invalidated
      // query to refetch. Secret keys are forced back to '' here rather
      // than resetting with the raw submitted values — otherwise the
      // plaintext secret the operator just typed would sit in the password
      // input until the refetch lands, and a save in that window would
      // read it as a fresh (already-saved) edit and re-send it, matching
      // buildDefaults' "a secret always starts empty" rule synchronously
      // instead of waiting on the network.
      // Register names, not schema keys — `resetValues` is a form-values
      // object, so clearing `email.smtp.password` under its schema key would
      // add a dead property and leave the real field holding the plaintext.
      const secretNames = expandedSchema
        .filter(f => f.type === 'secret')
        .map(f => fieldNameOf(fieldNames, f.key));
      const resetValues: ConfigFormValues = { ...formValues };
      for (const name of secretNames) resetValues[name] = '';
      form.reset(resetValues);
      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
    } catch (err: unknown) {
      // Two different 409s land here and the body `code` is what tells them
      // apart. The backend's `detail` is accurate but written for an API
      // client, so each gets a message that names what happened — and only
      // the stale-revision one has an action attached.
      const status =
        err && typeof err === 'object' && 'status' in err
          ? (err as { status?: number }).status
          : undefined;
      const code =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { code?: string } }).data?.code
          : undefined;
      if (code === CONFIG_REVISION_STALE) {
        // The document moved under this save — another operator, or a
        // record-list write. Latch until a reload has succeeded.
        setConflict(true);
        setError(t('adminModules.detail.configCard.revisionConflict'));
        return;
      }
      if (status === 409) {
        // Codeless 409: the record-list roster moved (slug exists / missing).
        setError(t('adminModules.recordList.conflict'));
        return;
      }
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String(
              (err as { data: { detail?: string } }).data?.detail ||
                t('adminModules.detail.configCard.updateFailed')
            )
          : t('adminModules.detail.configCard.updateFailed');
      setError(message);
    }
  };

  const handleDiscard = () => {
    form.reset(defaults);
    // Discard means discard: a staged removal is an unsaved change like any
    // other, and leaving one armed after "Discard" would destroy an element
    // on the next save the operator made for an unrelated reason.
    setCreated(EMPTY_CREATES);
    setStagedRemovals(EMPTY_CREATES);
    setPendingLabels({});
    setPendingDeletion(null);
    setError(null);
    setSuccess(false);
    setConflict(false);
    pendingDraft.current = null;
  };

  // Only fields react-hook-form marks dirty — never the whole form, which
  // would turn the other operator's changes into "local edits" pointing back
  // at the old values. Register names are flat (`buildFieldNames`), so
  // `dirtyFields[name]` is a plain boolean.
  const captureDirtyDraft = (): DraftEntry[] => {
    const values = form.getValues();
    const secretNames = new Set(
      expandedSchema
        .filter(f => f.type === 'secret')
        .map(f => fieldNameOf(fieldNames, f.key))
    );
    // Fields of elements created in this session go with the membership
    // change they belong to: the reload discards pending creates, so their
    // values must not come back as orphan edits.
    const createdLeafNames = new Set(
      schema
        .filter(f => f.type === 'recordList')
        .flatMap(f =>
          (created[f.key] ?? []).flatMap(slug =>
            expandElement(f, slug).map(leaf =>
              fieldNameOf(fieldNames, leaf.key)
            )
          )
        )
    );
    return (
      Object.keys(form.formState.dirtyFields)
        .filter(name => Boolean(form.formState.dirtyFields[name]))
        .filter(name => !createdLeafNames.has(name))
        .map(name => ({
          name,
          value: String(values[name] ?? ''),
          secret: secretNames.has(name)
        }))
        // A secret typed and then cleared is not a change: nothing to re-send.
        .filter(d => !d.secret || d.value !== '')
    );
  };

  const reloadAndReview = async () => {
    const baselineRevision = envConfig?.revision;
    droppedEntries.current = 0;
    pendingDraft.current = { environment, entries: captureDirtyDraft() };
    try {
      const fresh = await refetchEnv().unwrap();
      // Pending membership is discarded on EVERY successful reload, here and
      // not only in the re-seed effect: a staged removal was decided against
      // the state the operator saw, and the 409 says that state is gone —
      // even when this profile's own revision is unchanged (an activation or
      // another profile's write moved only configRevision), in which case
      // the data reference is identical and the effect never runs.
      setCreated(EMPTY_CREATES);
      setStagedRemovals(EMPTY_CREATES);
      setPendingLabels({});
      setPendingDeletion(null);
      // The profile is fresh now, but the module snapshot behind the `live`
      // badge, the runtime status and `activeEnvironment` is a separate
      // query — and an activation is one of the things that produces this
      // very conflict, so it is exactly the stale view the operator must not
      // review against. Invalidating the module tag makes the parent's
      // `useGetModuleQuery` refetch; the environment query's own tag
      // (`${name}-env-${env}`) is untouched, so this adds no second profile
      // request.
      if (mod) {
        dispatch(
          moduleApi.util.invalidateTags([
            { type: 'Module', id: mod.moduleName },
            { type: 'Module', id: 'LIST' }
          ])
        );
      }
      if (fresh.revision === baselineRevision) {
        // Same profile revision ⇒ identical data ⇒ no re-seed. The form
        // still holds the draft; nothing to re-apply.
        pendingDraft.current = null;
      }
      // Otherwise the data reference changes, the re-seed effect runs, and it
      // consumes the draft — whichever of that render and this continuation
      // comes first, the draft is applied exactly once.
      setConflict(false);
      // The re-seed effect may already have run and reported dropped edits;
      // clearing unconditionally here would swallow that notice whenever
      // this continuation happens to resume second.
      setError(
        droppedEntries.current > 0
          ? droppedEditsMessage(droppedEntries.current)
          : null
      );
    } catch {
      pendingDraft.current = null;
      setError(t('adminModules.detail.configCard.reloadFailed'));
      // conflict stays true: Save must not be usable against a baseline the
      // operator never got to review.
    }
  };

  return {
    schema,
    groupTree,
    flatNodes,
    form,
    defaults,
    fieldNames,
    secretStatus,
    envLoading,
    saving,
    visibleKeys,
    dirtyKeys,
    errorKeys,
    dirtyCount,
    errorCount,
    perGroup,
    saveBarErrors,
    unfilledByGroup,
    recordList,
    pendingDeletion,
    confirmDeletion: () => {
      void onSave();
    },
    cancelDeletion: () => setPendingDeletion(null),
    error,
    success,
    conflict,
    reloadAndReview,
    clearError: () => setError(null),
    onSave,
    handleDiscard
  };
};
