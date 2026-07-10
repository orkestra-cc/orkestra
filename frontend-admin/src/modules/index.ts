import type { ModuleManifest } from './types';

/**
 * All optional-module manifests, auto-discovered from `*.tsx` in this folder.
 * Each addon's `<name>.tsx` default-exports its `ModuleManifest`; dropping that
 * file in registers the addon here with NO edit to this shared file, and
 * removing it un-registers it. `*.test.tsx` is excluded so test files are never
 * mistaken for manifests. Mirrors the i18n locale glob in `test/setup.ts`.
 *
 * ADR-0006 collapsed Orkestra to a core-only base, so this discovers **nothing**
 * out of the box — a fork that adds optional modules gets them registered just
 * by adding their `<name>.tsx` files (see `_template/` for the scaffold). This
 * is the frontend half of the addon self-registration seam; the backend half is
 * each addon's `catalog_<name>.go` `init()`.
 */
const mods = import.meta.glob<{ default: ModuleManifest }>(
  ['./*.tsx', '!./*.test.tsx'],
  { eager: true }
);

export const moduleCatalog: Record<string, ModuleManifest> = Object.fromEntries(
  Object.values(mods).map(m => [m.default.name, m.default])
);

export type { ModuleManifest } from './types';
