// Types for the navigation admin surface (Phase 1 + 2 of the navigation
// admin epic). The public sidebar consumer types live in
// store/api/navigationApi.ts; these mirror the backend AdminNavItem /
// AdminNavigationResponse / NavOverride shapes (see
// backend/internal/core/navigation/models/navigation.go and
// .../models/override.go).

export interface AdminNavItem {
  itemKey: string;
  name: string;
  path?: string;
  icon?: string;
  moduleName: string;
  moduleEnabled: boolean;
  realm?: string;
  section?: string;
  group?: string;
  tier?: string;
  minRole?: string;
  active: boolean;
  declaredOrder: number;
  effectiveOrder: number;
  overridden: boolean;
  children?: AdminNavItem[];
}

export interface AdminNavSection {
  label: string;
  items: AdminNavItem[];
}

export interface AdminNavRealm {
  key: string;
  label: string;
  sections: AdminNavSection[];
}

export interface AdminNavigationResponse {
  realms: AdminNavRealm[];
  /** Role hierarchy, highest privilege first. */
  roles: string[];
  /** Tenant-kind values used by Tier filtering. */
  tenantKinds: string[];
  /**
   * Synthetic parentKey clients pass to PATCH /v1/admin/navigation/order
   * to reorder the realm cards. Constant ("__realms__") but echoed by
   * the server so the SPA does not need to hardcode it.
   */
  realmsParentKey: string;
  /** True when a persisted override changed the realm order. */
  realmsOverridden: boolean;
}

export interface NavOverride {
  parentKey: string;
  orderedChildren: string[];
  updatedAt: string;
  updatedBy?: string;
}

export interface PatchOrderBody {
  parentKey: string;
  orderedChildren: string[];
}

/**
 * Synthetic parent key for top-level items inside one (realm, section)
 * bucket. Must match the backend's sectionRootKey shape exactly — see
 * backend/internal/core/navigation/services/override_service.go.
 */
export function sectionRootKey(realm: string, section: string): string {
  const r = realm || 'shared';
  const slug = slugifySection(section || 'Other');
  return `__root.${r}.${slug}`;
}

function slugifySection(s: string): string {
  let out = '';
  let prevHyphen = false;
  for (const ch of s) {
    const c = ch.charCodeAt(0);
    if (c >= 65 && c <= 90) {
      out += String.fromCharCode(c + 32);
      prevHyphen = false;
    } else if ((c >= 97 && c <= 122) || (c >= 48 && c <= 57)) {
      out += ch;
      prevHyphen = false;
    } else if (!prevHyphen && out.length > 0) {
      out += '-';
      prevHyphen = true;
    }
  }
  if (out.endsWith('-')) {
    out = out.slice(0, -1);
  }
  return out.length === 0 ? 'other' : out;
}
