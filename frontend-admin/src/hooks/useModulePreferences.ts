// Per-module preferences of the signed-in user, over /v1/me/preferences/{module}
// (the user module's store; fields declared by the module with
// module.HasUserPreferences, the same ConfigField vocabulary the admin config
// uses). A module reads its own preferences with this hook; the settings
// page renders every module's declared fields with ModulePreferencesCard.
import { useCallback } from 'react';
import {
  useGetMyPreferencesQuery,
  usePutMyPreferencesMutation,
  type ModulePreferences
} from 'store/api/userApi';

export interface ModulePreferencesState {
  /** Declared fields, for rendering a form. Empty until loaded. */
  fields: ModulePreferences['fields'];
  /** Stored values with the declared defaults filled in. Empty until loaded. */
  values: Record<string, string>;
  /** ISO timestamp of the last save, or undefined when never saved. */
  updatedAt?: string;
  isLoading: boolean;
  isSaving: boolean;
  /** 404 here means the module declares no preferences or is disabled. */
  error: unknown;
  /**
   * Replace the stored values (PUT semantics: send the whole map; an absent
   * field falls back to its declared default). Rejects with the RTK error on
   * failure — a 422 carries every invalid field in its detail.
   */
  save: (values: Record<string, string>) => Promise<ModulePreferences>;
}

export function useModulePreferences(
  moduleName: string
): ModulePreferencesState {
  const { data, isLoading, error } = useGetMyPreferencesQuery(moduleName);
  const [put, { isLoading: isSaving }] = usePutMyPreferencesMutation();

  const save = useCallback(
    (values: Record<string, string>) =>
      put({ module: moduleName, values }).unwrap(),
    [put, moduleName]
  );

  return {
    fields: data?.fields ?? [],
    values: data?.values ?? {},
    updatedAt: data?.updatedAt,
    isLoading,
    isSaving,
    error,
    save
  };
}

export default useModulePreferences;
