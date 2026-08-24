import { useEffect, useMemo, useState } from 'react';
import { useWatch, type UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import {
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
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
  type PendingCreates
} from './useModuleConfigForm';
import { expandElement, labelKeyOf, rosterOf } from './recordList/expandSchema';
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
export interface RecordListEditing {
  created: PendingCreates;
  stagedRemovals: PendingCreates;
  /** Adds a slug to the list and seeds its label. */
  create: (field: string, slug: string, label: string) => void;
  /** Marks a slug for removal at the next save. Reversible until then. */
  stageRemove: (field: string, slug: string) => void;
  /** Takes a slug back off the removal list. */
  undoRemove: (field: string, slug: string) => void;
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
  error: string | null;
  success: boolean;
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

  const { data: envConfig, isLoading: envLoading } =
    useGetModuleEnvironmentQuery(
      { name: mod?.moduleName ?? '', environment },
      { skip: !mod || !mod.availableEnvironments?.length }
    );
  const [updateEnv, { isLoading: saving }] =
    useUpdateModuleEnvironmentMutation();

  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

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

  const { form, defaults, fieldNames, expandedSchema } = useModuleConfigForm(
    schema,
    configSource,
    created
  );

  // Re-seed the form whenever the server-known baseline changes: the
  // initial environment fetch resolving, switching environments, or a
  // fresh `mod.configValues` reference from the parent. Deliberately NOT
  // keyed on `defaults`/`form` — both are recomputed every render, and
  // depending on `defaults` would reset on every keystroke, discarding the
  // very edits the sticky bar exists to accumulate across groups.
  useEffect(() => {
    form.reset(defaults);
    // Pending membership belongs to the baseline that produced it. Carrying a
    // staged removal across an environment switch would arm a destructive
    // save against a profile the operator never looked at.
    setCreated(EMPTY_CREATES);
    setStagedRemovals(EMPTY_CREATES);
    // The save alerts belong to the baseline that produced them. A "save
    // failed" banner from `production` still on screen after switching to
    // `sandbox` reads as a failure against the environment now displayed —
    // and a lingering success tick is just as misleading once the form
    // underneath has been re-seeded from a different source.
    setError(null);
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
  }, [envConfig, mod?.configValues]);

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
  const dirtyKeys = new Set(
    schema
      .filter(
        f =>
          visibleKeys.has(f.key) &&
          formNamesOf(f).some(name => Boolean(dirtyFields[name]))
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
    create: (field, slug, label) => {
      setCreated(prev => addTo(prev, field, slug));
      // Seeded on the next tick: the label field only exists once the roster
      // has grown, and the roster grows from the state set above.
      setTimeout(() => {
        form.setValue(fieldNameOf(fieldNames, labelKeyOf(field, slug)), label, {
          shouldDirty: true
        });
      }, 0);
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

  const onSave = async () => {
    if (!mod) return;
    const formValues = form.getValues();
    const { config, secrets } = collectDiff(
      schema,
      formValues,
      defaults,
      fieldNames
    );
    const keysBeingSaved = [...Object.keys(config), ...Object.keys(secrets)];
    if (keysBeingSaved.length === 0) return;

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
        config: Object.keys(config).length > 0 ? config : undefined,
        secrets: Object.keys(secrets).length > 0 ? secrets : undefined
      }).unwrap();

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
      const secretNames = schema
        .filter(f => f.type === 'secret')
        .map(f => fieldNameOf(fieldNames, f.key));
      const resetValues: ConfigFormValues = { ...formValues };
      for (const name of secretNames) resetValues[name] = '';
      form.reset(resetValues);
      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
    } catch (err: unknown) {
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
    setError(null);
    setSuccess(false);
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
    error,
    success,
    clearError: () => setError(null),
    onSave,
    handleDiscard
  };
};
