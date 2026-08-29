import { useEffect } from 'react';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  setMemberships,
  setEffectivePermissions,
  setFeatures,
  resetTenantState,
  selectCurrentOrgId
} from 'store/slices/tenantSlice';
import { selectIsAuthenticated, selectIsLoading } from 'store/slices/authSlice';
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
 *
 * Steps 3 and 4 wait for step 2 on purpose — see the comment on the two
 * queries below.
 */
export function useTenantBootstrap() {
  const dispatch = useAppDispatch();
  const isAuthenticated = useAppSelector(selectIsAuthenticated);
  const isAuthLoading = useAppSelector(selectIsLoading);
  const currentOrgId = useAppSelector(selectCurrentOrgId);
  // Gate on the access token being in Redux, not just isAuthenticated. These
  // queries are tenant-scoped and racing them against the /v1/auth/session
  // cookie rotation trips the backend's family-replay guard. See
  // useModuleApi.ts for the full rationale.
  const hasAccessToken = useAppSelector(s => !!s.auth.accessToken);
  const gate = isAuthenticated && hasAccessToken;

  const { data: membershipsData } = useListMyOrgsQuery(undefined, {
    skip: !gate
  });

  // fulfilledTimeStamp is part of both mirror effects' deps below: a
  // tag-invalidated refetch that returns a payload deep-equal to the cached
  // one keeps the SAME data reference (RTK Query structural sharing), so an
  // effect keyed on `data` alone never refires. setCurrentOrg clears
  // permissions/features on every workspace pick — without re-dispatching
  // after each fulfillment they'd stay empty until a full page reload,
  // unmounting every hasPermission-gated surface (e.g. the NineDotMenu for
  // single-membership admins).
  //
  // Both are keyed on `currentOrgId` — the membership-VALIDATED selection the
  // slice only publishes once GET /v1/tenants has landed — and NOT on the raw
  // localStorage value. They used to read `currentOrgId || storedOrgId` as an
  // optimistic hint, to fire all three in parallel rather than waiting a round
  // trip; that saved the round trip and broke both queries outright.
  //
  // The org id travels in the PATH here, and the backend's assertTenantScope
  // (tenant/handlers/handler.go, authz/handlers/handler.go) 404s unless it
  // equals the tenant the auth middleware resolved for the request — which it
  // takes from X-Tenant-ID. baseApi stamps that header from `currentOrgId`
  // alone, by the same deliberate rule (tenantSlice.ts: a stale stored id must
  // never reach the wire before we know what the user is actually a member of).
  // So the optimistic call sent a path id with no header to vouch for it, the
  // backend fell back to the token's acting tenant — the platform default,
  // which is not necessarily the workspace the operator picked — and both
  // requests 404'd, every time.
  //
  // Nothing recovered from that: the cache key is the org id, and it does not
  // change when currentOrgId later settles on the very value localStorage
  // already held, so no refetch ever fires and tenant.permissions/features
  // stay empty for the whole session.
  const { data: effective, fulfilledTimeStamp: effectiveFulfilledAt } =
    useGetEffectivePermissionsQuery(currentOrgId as string, {
      skip: !gate || !currentOrgId
    });

  const { data: org, fulfilledTimeStamp: orgFulfilledAt } = useGetOrgQuery(
    currentOrgId as string,
    {
      skip: !gate || !currentOrgId
    }
  );

  useEffect(() => {
    if (!isAuthenticated) {
      // On a fresh page load isAuthenticated is false while the session
      // check is still in flight — that means "not known yet", not
      // "logged out". Resetting here would wipe the persisted workspace
      // selection (and any impersonation) on every refresh, so only reset
      // once the auth check has actually concluded unauthenticated.
      if (!isAuthLoading) {
        dispatch(resetTenantState());
      }
      return;
    }
    if (membershipsData?.memberships) {
      dispatch(setMemberships(membershipsData.memberships));
    }
  }, [isAuthenticated, isAuthLoading, membershipsData, dispatch]);

  useEffect(() => {
    if (effective) dispatch(setEffectivePermissions(effective));
  }, [effective, effectiveFulfilledAt, dispatch]);

  useEffect(() => {
    if (org?.features) dispatch(setFeatures(org.features));
  }, [org, orgFulfilledAt, dispatch]);
}
