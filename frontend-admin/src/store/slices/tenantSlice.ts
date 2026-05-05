import { createSelector, createSlice, PayloadAction } from '@reduxjs/toolkit';
import type { RootState } from '../index';

/**
 * Membership entry returned by GET /v1/tenants. Mirrors the backend
 * memberDTO in tenant/handlers/handler.go.
 */
export interface Membership {
  tenantId: string;
  name: string;
  slug: string;
  plan: string;
  kind?: string;
  roles: string[];
  isOwner: boolean;
}

/**
 * Effective-permissions payload returned by
 * GET /v1/tenants/{tenantId}/authz/me.
 */
export interface EffectivePermissions {
  tenantId: string;
  permissions: string[];
  systemRole: string;
}

interface TenantState {
  memberships: Membership[];
  currentOrgId: string | null;
  /** Effective permissions in currentOrgId (union of all role bindings). */
  permissions: string[];
  /** Features enabled on the current org's plan. */
  features: string[];
  systemRole: string;
  loading: boolean;
  error: string | null;
  /**
   * Operator-admin impersonation target. When set, baseApi stamps this as
   * X-Tenant-ID instead of currentOrgId, and the backend honors it only for
   * callers holding system.tenants.admin. Cleared on logout.
   */
  impersonatedTenantId: string | null;
  impersonatedTenantName: string | null;
}

const STORAGE_KEY = 'orkestra.currentOrgId';
const IMPERSONATION_STORAGE_KEY = 'orkestra.impersonatedTenant';

// currentOrgId starts null and is rehydrated from localStorage only after
// memberships load, validated against the fresh list. This prevents a stale
// localStorage value (e.g. after a backend DB wipe) from injecting
// X-Tenant-ID on requests before we know what tenants the user actually has.
// Rehydrate impersonation from sessionStorage only. Using sessionStorage
// (not localStorage) ensures a fresh tab never inherits an impersonation
// session — the admin must opt in explicitly each time.
const rehydrateImpersonation = (): {
  impersonatedTenantId: string | null;
  impersonatedTenantName: string | null;
} => {
  if (typeof window === 'undefined') {
    return { impersonatedTenantId: null, impersonatedTenantName: null };
  }
  try {
    const raw = window.sessionStorage.getItem(IMPERSONATION_STORAGE_KEY);
    if (!raw) return { impersonatedTenantId: null, impersonatedTenantName: null };
    const parsed = JSON.parse(raw) as {
      tenantId?: string;
      tenantName?: string;
    };
    return {
      impersonatedTenantId: parsed.tenantId || null,
      impersonatedTenantName: parsed.tenantName || null
    };
  } catch {
    return { impersonatedTenantId: null, impersonatedTenantName: null };
  }
};

const initialState: TenantState = {
  memberships: [],
  currentOrgId: null,
  permissions: [],
  features: [],
  systemRole: '',
  loading: false,
  error: null,
  ...rehydrateImpersonation()
};

const tenantSlice = createSlice({
  name: 'tenant',
  initialState,
  reducers: {
    setMemberships: (state, action: PayloadAction<Membership[]>) => {
      state.memberships = action.payload;
      // Pick a sensible default: stored selection if still valid, else first
      // owned org, else first membership. We re-read localStorage here (not
      // at slice init) so a stale id from a previous backend DB can't leak
      // through before memberships are known.
      const stored =
        typeof window !== 'undefined' ? window.localStorage.getItem(STORAGE_KEY) : null;
      const valid = action.payload.some((m) => m.tenantId === stored);
      if (valid) {
        state.currentOrgId = stored;
      } else {
        const owned = action.payload.find((m) => m.isOwner);
        state.currentOrgId = owned?.tenantId || action.payload[0]?.tenantId || null;
      }
      if (state.currentOrgId && typeof window !== 'undefined') {
        window.localStorage.setItem(STORAGE_KEY, state.currentOrgId);
      } else if (typeof window !== 'undefined') {
        window.localStorage.removeItem(STORAGE_KEY);
      }
    },

    setCurrentOrg: (state, action: PayloadAction<string>) => {
      const exists = state.memberships.some((m) => m.tenantId === action.payload);
      if (!exists) return;
      state.currentOrgId = action.payload;
      if (typeof window !== 'undefined') {
        window.localStorage.setItem(STORAGE_KEY, action.payload);
      }
      // Permissions are scoped to the org, so clear them on switch.
      state.permissions = [];
      state.features = [];
    },

    setEffectivePermissions: (state, action: PayloadAction<EffectivePermissions>) => {
      if (action.payload.tenantId !== state.currentOrgId) return;
      state.permissions = action.payload.permissions;
      state.systemRole = action.payload.systemRole;
      // Features come from the membership's plan — the memberships list
      // carries the plan name but not the feature set; we get features
      // from the org detail endpoint when needed. For now, leave it empty
      // and rely on RequireEntitlement at the backend.
    },

    setFeatures: (state, action: PayloadAction<string[]>) => {
      state.features = action.payload;
    },

    setLoading: (state, action: PayloadAction<boolean>) => {
      state.loading = action.payload;
    },

    setError: (state, action: PayloadAction<string | null>) => {
      state.error = action.payload;
    },

    resetTenantState: () => {
      if (typeof window !== 'undefined') {
        window.localStorage.removeItem(STORAGE_KEY);
        window.sessionStorage.removeItem(IMPERSONATION_STORAGE_KEY);
      }
      return {
        ...initialState,
        currentOrgId: null,
        impersonatedTenantId: null,
        impersonatedTenantName: null
      };
    },

    /**
     * Begin impersonating a tenant the user is NOT a member of. Gated
     * server-side by system.tenants.admin — the backend rejects the header
     * for everyone else. Stored in sessionStorage so it clears when the
     * tab closes.
     *
     * We intentionally do NOT clear permissions/features here. The caller
     * (AdminTenantSwitcher) follows up with invalidateTags(...) which
     * triggers useGetEffectivePermissionsQuery to refetch against the
     * impersonated target; the resulting useEffect in useTenantBootstrap
     * overwrites this slice with the impersonated permissions. Clearing
     * eagerly would produce a render window where AdminTenantSwitcher's
     * own gate (hasPermission('system.tenants.admin')) returns false,
     * hiding the switcher mid-flow.
     */
    startImpersonation: (
      state,
      action: PayloadAction<{ tenantId: string; tenantName: string }>
    ) => {
      state.impersonatedTenantId = action.payload.tenantId;
      state.impersonatedTenantName = action.payload.tenantName;
      if (typeof window !== 'undefined') {
        window.sessionStorage.setItem(
          IMPERSONATION_STORAGE_KEY,
          JSON.stringify({
            tenantId: action.payload.tenantId,
            tenantName: action.payload.tenantName
          })
        );
      }
    },

    stopImpersonation: (state) => {
      state.impersonatedTenantId = null;
      state.impersonatedTenantName = null;
      // Leave permissions/features intact — see startImpersonation for why.
      // The refetch triggered by invalidateTags in the caller will overwrite
      // them with the admin's real-tenant permissions a moment later.
      if (typeof window !== 'undefined') {
        window.sessionStorage.removeItem(IMPERSONATION_STORAGE_KEY);
      }
    }
  }
});

export const {
  setMemberships,
  setCurrentOrg,
  setEffectivePermissions,
  setFeatures,
  setLoading,
  setError,
  resetTenantState,
  startImpersonation,
  stopImpersonation
} = tenantSlice.actions;

// --- Selectors ---

export const selectTenant = (state: RootState) => state.tenant;
export const selectMemberships = (state: RootState) => state.tenant.memberships;
export const selectCurrentOrgId = (state: RootState) => state.tenant.currentOrgId;
export const selectCurrentMembership = (state: RootState): Membership | null => {
  const id = state.tenant.currentOrgId;
  if (!id) return null;
  return state.tenant.memberships.find((m) => m.tenantId === id) || null;
};
export const selectPermissions = (state: RootState) => state.tenant.permissions;
export const selectFeatures = (state: RootState) => state.tenant.features;
export const selectSystemRole = (state: RootState) => state.tenant.systemRole;
export const selectImpersonation = createSelector(
  (state: RootState) => state.tenant.impersonatedTenantId,
  (state: RootState) => state.tenant.impersonatedTenantName,
  (tenantId, tenantName) => ({ tenantId, tenantName })
);
export const selectIsImpersonating = (state: RootState) =>
  !!state.tenant.impersonatedTenantId;

/** Returns true when the current user holds `permission` in the current org. */
export const selectHasPermission =
  (permission: string) => (state: RootState): boolean => {
    const perms = state.tenant.permissions;
    if (perms.includes('*')) return true;
    return perms.includes(permission);
  };

/** Returns true when the current user has all of the listed permissions. */
export const selectHasAllPermissions =
  (required: string[]) => (state: RootState): boolean => {
    const perms = state.tenant.permissions;
    if (perms.includes('*')) return true;
    return required.every((p) => perms.includes(p));
  };

/** Returns true when the current user has any of the listed permissions. */
export const selectHasAnyPermission =
  (required: string[]) => (state: RootState): boolean => {
    const perms = state.tenant.permissions;
    if (perms.includes('*')) return true;
    return required.some((p) => perms.includes(p));
  };

/** Returns true when the current org's plan includes the given feature. */
export const selectHasFeature =
  (feature: string) => (state: RootState): boolean => {
    const features = state.tenant.features;
    return features.includes('*') || features.includes(feature);
  };

export default tenantSlice.reducer;
