import type { ModuleAdminSection, ModuleManifest } from './types';

export const ADMIN_SECTION_KEY = /^[a-z0-9][a-z0-9_-]*$/;
export const ADMIN_SECTION_PREFIX = '__addon:';

/**
 * The manifest's admin sections that can be mounted: well-formed keys only,
 * first occurrence of a duplicate wins. A bad key would make ?section=
 * routing ambiguous and React keys collide, so it is dropped (with a warning
 * in development) rather than rendered.
 */
export const validAdminSections = (
  manifest: ModuleManifest | undefined
): ModuleAdminSection[] => {
  const seen = new Set<string>();
  const valid: ModuleAdminSection[] = [];
  for (const section of manifest?.adminSections ?? []) {
    if (!ADMIN_SECTION_KEY.test(section.key) || seen.has(section.key)) {
      if (import.meta.env.DEV)
        console.warn(
          `Ignoring admin section "${section.key}" of module "${manifest?.name}": invalid or duplicate key.`
        );
      continue;
    }
    seen.add(section.key);
    valid.push(section);
  }
  return valid;
};
