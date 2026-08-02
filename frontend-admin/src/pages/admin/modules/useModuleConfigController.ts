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
  visibleFields,
  type GroupNode
} from './configModel';
import {
  useModuleConfigForm,
  collectDiff,
  type ConfigFormValues
} from './useModuleConfigForm';
import { translateConfigGroup } from 'helpers/configLabel';

// Module-scope, stable-by-reference fallback for a module with no schema yet
// (still loading, or genuinely declares none). useModuleConfigForm's yup
// resolver memo keys off `schema` by identity — `mod?.configSchema ?? []`
// would mint a brand-new array every render and silently rebuild the whole
// validation schema on every keystroke. Both consumers of this hook
// (ModuleConfigSection and detail/index.tsx) now share this ONE constant
// instead of each keeping their own copy.
const EMPTY_SCHEMA: ConfigField[] = [];

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
  secretStatus: Record<string, boolean>;
  envLoading: boolean;
  saving: boolean;
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
  const { form, defaults } = useModuleConfigForm(schema, configSource);

  // Re-seed the form whenever the server-known baseline changes: the
  // initial environment fetch resolving, switching environments, or a
  // fresh `mod.configValues` reference from the parent. Deliberately NOT
  // keyed on `defaults`/`form` — both are recomputed every render, and
  // depending on `defaults` would reset on every keystroke, discarding the
  // very edits the sticky bar exists to accumulate across groups.
  useEffect(() => {
    form.reset(defaults);
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
  const values = useWatch({ control: form.control }) as ConfigFormValues;
  const { errors, dirtyFields } = form.formState;

  // Deliberately NOT useMemo here: react-hook-form mutates its `errors`
  // object in place (same reference across renders even as its content
  // changes), so a memo keyed on `[errors, ...]` silently freezes on the
  // first value it ever saw.
  const visibleKeys = new Set(visibleFields(schema, values).map(f => f.key));
  const dirtyKeys = new Set(
    Object.keys(dirtyFields).filter(key => visibleKeys.has(key))
  );
  const errorKeys = new Set(
    Object.keys(errors).filter(key => visibleKeys.has(key))
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

  const onSave = async () => {
    if (!mod) return;
    const formValues = form.getValues();
    const { config, secrets } = collectDiff(schema, formValues, defaults);
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
    const valid = await form.trigger(keysBeingSaved);
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
      const secretKeys = schema
        .filter(f => f.type === 'secret')
        .map(f => f.key);
      const resetValues: ConfigFormValues = { ...formValues };
      for (const key of secretKeys) resetValues[key] = '';
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
    secretStatus,
    envLoading,
    saving,
    dirtyKeys,
    errorKeys,
    dirtyCount,
    errorCount,
    perGroup,
    saveBarErrors,
    error,
    success,
    clearError: () => setError(null),
    onSave,
    handleDiscard
  };
};
