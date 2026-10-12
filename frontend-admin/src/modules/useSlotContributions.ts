import { useMemo } from 'react';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import { useGetModulesQuery } from 'store/api/moduleApi';
import { useAppSelector } from 'store/hooks';
import { moduleCatalog } from './index';
import { collectVisibleModuleNames } from './runtime/ModuleGlobalOverlays';
import {
  activeModuleNames,
  collectSlotContributions,
  type ResolvedSlotContribution
} from './slotContributions';

/**
 * The contributions to `slotId` the current user should see: active modules
 * only (see activeModuleNames), filtered by each contribution's permission.
 * The admin module list is gated on the access token like
 * useModuleApiInjection.
 */
export function useSlotContributions(
  slotId: string
): ResolvedSlotContribution[] {
  const hasAccessToken = useAppSelector(s => !!s.auth.accessToken);
  const {
    data: admin,
    isError,
    error
  } = useGetModulesQuery(undefined, { skip: !hasAccessToken });
  // RTK Query refetches a rejected entry for every new subscriber, and while
  // that refetch is pending isError is false — but the previous error is kept.
  // Reading it keeps a non-admin on the navigation fallback instead of
  // dropping every contribution (a tab flicker) on each page mount.
  const adminFailed = admin === undefined && (isError || error !== undefined);
  const {
    filteredNavigation,
    realms,
    isLoading: navLoading,
    isError: navFailed
  } = useRoleBasedNavigation();
  const { hasPermission } = useAuth();

  return useMemo(() => {
    const navVisible =
      navLoading || navFailed
        ? null
        : collectVisibleModuleNames(filteredNavigation, realms);
    return collectSlotContributions(
      moduleCatalog,
      slotId,
      activeModuleNames(admin, adminFailed, navVisible),
      hasPermission
    );
  }, [
    admin,
    adminFailed,
    filteredNavigation,
    realms,
    navLoading,
    navFailed,
    hasPermission,
    slotId
  ]);
}
