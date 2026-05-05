import { useEffect } from 'react';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  setMemberships,
  setEffectivePermissions,
  setFeatures,
  resetTenantState,
  selectCurrentOrgId
} from 'store/slices/tenantSlice';
import { selectIsAuthenticated } from 'store/slices/authSlice';
import {
  useListMyOrgsQuery,
  useGetEffectivePermissionsQuery,
  useGetOrgQuery
} from 'store/api/tenantApi';

/**
 * useTenantBootstrap fetches the user's tenant memberships and effective
 * permissions for the current tenant after login, and refetches on tenant
 * switch. Drop it into a top-level layout component (e.g. MainLayout) so
 * tenant state is always fresh.
 *
 * Flow:
 *   1. User logs in → authSlice.isAuthenticated goes true
 *   2. GET /v1/tenants → dispatch setMemberships
 *      (the slice auto-picks a default current tenant if none is stored)
 *   3. GET /v1/tenants/{currentOrgId}/authz/me → dispatch setEffectivePermissions
 *   4. GET /v1/tenants/{currentOrgId} → dispatch setFeatures
 */
const STORAGE_KEY = 'orkestra.currentOrgId';

export function useTenantBootstrap() {
  const dispatch = useAppDispatch();
  const isAuthenticated = useAppSelector(selectIsAuthenticated);
  const currentOrgId = useAppSelector(selectCurrentOrgId);
  // Gate on the access token being in Redux, not just isAuthenticated. These
  // queries are tenant-scoped and racing them against the /v1/auth/session
  // cookie rotation trips the backend's family-replay guard. See
  // useModuleApi.ts for the full rationale.
  const hasAccessToken = useAppSelector((s) => !!s.auth.accessToken);
  const gate = isAuthenticated && hasAccessToken;

  // Use stored orgId as an optimistic hint so we can fire all three
  // queries in parallel instead of waiting for memberships to resolve
  // before fetching permissions and org details.
  const storedOrgId = typeof window !== 'undefined'
    ? window.localStorage.getItem(STORAGE_KEY)
    : null;
  const optimisticOrgId = currentOrgId || storedOrgId;

  const { data: membershipsData } = useListMyOrgsQuery(undefined, {
    skip: !gate
  });

  const { data: effective } = useGetEffectivePermissionsQuery(optimisticOrgId as string, {
    skip: !gate || !optimisticOrgId
  });

  const { data: org } = useGetOrgQuery(optimisticOrgId as string, {
    skip: !gate || !optimisticOrgId
  });

  useEffect(() => {
    if (!isAuthenticated) {
      dispatch(resetTenantState());
      return;
    }
    if (membershipsData?.memberships) {
      dispatch(setMemberships(membershipsData.memberships));
    }
  }, [isAuthenticated, membershipsData, dispatch]);

  useEffect(() => {
    if (effective) dispatch(setEffectivePermissions(effective));
  }, [effective, dispatch]);

  useEffect(() => {
    if (org?.features) dispatch(setFeatures(org.features));
  }, [org, dispatch]);
}
