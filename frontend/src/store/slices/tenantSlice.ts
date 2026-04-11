import { createSlice, PayloadAction } from '@reduxjs/toolkit';
import type { RootState } from '../index';

/**
 * Membership entry returned by GET /v1/orgs. Mirrors the backend
 * memberDTO in tenant/handlers/handler.go.
 */
export interface Membership {
  orgId: string;
  name: string;
  slug: string;
  plan: string;
  roles: string[];
  isOwner: boolean;
}

/**
 * Effective-permissions payload returned by
 * GET /v1/orgs/{orgId}/authz/me.
 */
export interface EffectivePermissions {
  orgId: string;
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
}

const STORAGE_KEY = 'orkestra.currentOrgId';

// currentOrgId starts null and is rehydrated from localStorage only after
// memberships load, validated against the fresh list. This prevents a stale
// localStorage value (e.g. after a backend DB wipe) from injecting
// X-Org-ID on requests before we know what orgs the user actually has.
const initialState: TenantState = {
  memberships: [],
  currentOrgId: null,
  permissions: [],
  features: [],
  systemRole: '',
  loading: false,
  error: null
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
      const valid = action.payload.some((m) => m.orgId === stored);
      if (valid) {
        state.currentOrgId = stored;
      } else {
        const owned = action.payload.find((m) => m.isOwner);
        state.currentOrgId = owned?.orgId || action.payload[0]?.orgId || null;
      }
      if (state.currentOrgId && typeof window !== 'undefined') {
        window.localStorage.setItem(STORAGE_KEY, state.currentOrgId);
      } else if (typeof window !== 'undefined') {
        window.localStorage.removeItem(STORAGE_KEY);
      }
    },

    setCurrentOrg: (state, action: PayloadAction<string>) => {
      const exists = state.memberships.some((m) => m.orgId === action.payload);
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
      if (action.payload.orgId !== state.currentOrgId) return;
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
      }
      return { ...initialState, currentOrgId: null };
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
  resetTenantState
} = tenantSlice.actions;

// --- Selectors ---

export const selectTenant = (state: RootState) => state.tenant;
export const selectMemberships = (state: RootState) => state.tenant.memberships;
export const selectCurrentOrgId = (state: RootState) => state.tenant.currentOrgId;
export const selectCurrentMembership = (state: RootState): Membership | null => {
  const id = state.tenant.currentOrgId;
  if (!id) return null;
  return state.tenant.memberships.find((m) => m.orgId === id) || null;
};
export const selectPermissions = (state: RootState) => state.tenant.permissions;
export const selectFeatures = (state: RootState) => state.tenant.features;
export const selectSystemRole = (state: RootState) => state.tenant.systemRole;

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
