import type { ModuleManifest, SlotContribution } from './types';

/** A contribution plus the module that contributes it (its i18n namespace). */
export interface ResolvedSlotContribution extends SlotContribution {
  moduleName: string;
}

interface AdminModule {
  moduleName: string;
  enabled: boolean;
  status: string;
}

/**
 * The active modules as the slot registry sees them. The admin module list
 * answers when the caller may read it (enabled and running, the
 * useModuleApiInjection rule). A non-admin gets 403 there, so the modules that
 * own a navigation item the backend served this user answer instead (the
 * ModuleGlobalOverlays rule; the backend drops a disabled module's items).
 * `null` while neither is known: nothing renders until it is.
 */
export function activeModuleNames(
  admin: AdminModule[] | undefined,
  adminFailed: boolean,
  navVisible: Set<string> | null
): Set<string> | null {
  if (admin) {
    return new Set(
      admin
        .filter(m => m.enabled && m.status === 'running')
        .map(m => m.moduleName)
    );
  }
  return adminFailed ? navVisible : null;
}

/**
 * Collects the contributions to `slotId` of the active modules that the
 * caller may see, sorted by key. A contribution whose key is not prefixed with
 * `<module name>.` is dropped, so two modules can never claim the same key.
 */
export function collectSlotContributions(
  catalog: Record<string, ModuleManifest>,
  slotId: string,
  active: Set<string> | null,
  hasPermission: (permission: string) => boolean
): ResolvedSlotContribution[] {
  if (!active) return [];
  const out: ResolvedSlotContribution[] = [];
  for (const manifest of Object.values(catalog)) {
    if (!active.has(manifest.name)) continue;
    for (const c of manifest.contributions?.[slotId] ?? []) {
      if (!c.key.startsWith(`${manifest.name}.`)) continue;
      if (c.permission && !hasPermission(c.permission)) continue;
      out.push({ ...c, moduleName: manifest.name });
    }
  }
  return out.sort((a, b) => a.key.localeCompare(b.key));
}
