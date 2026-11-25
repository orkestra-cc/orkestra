import { useMemo } from 'react';
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import { useGetModulesQuery } from 'store/api/moduleApi';
import { useAppSelector } from 'store/hooks';
import { collectVisibleModuleNames } from './runtime/ModuleGlobalOverlays';
import { activeModuleNames } from './slotContributions';

/**
 * The names of the modules active for the current user (see
 * activeModuleNames): the admin module list when readable, else the modules
 * the navigation shows. null while neither is known yet. The admin module
 * list is gated on the access token like useModuleApiInjection.
 */
export function useActiveModuleNames(): Set<string> | null {
  const hasAccessToken = useAppSelector(s => !!s.auth.accessToken);
  const {
    data: admin,
    isError,
    error
  } = useGetModulesQuery(undefined, { skip: !hasAccessToken });
  // RTK Query refetches a rejected entry for every new subscriber, and while
  // that refetch is pending isError is false — but the previous error is kept.
  // Reading it keeps a non-admin on the navigation fallback instead of
  // dropping every active module (a flicker) on each page mount.
  const adminFailed = admin === undefined && (isError || error !== undefined);
  const {
    filteredNavigation,
    realms,
    isLoading: navLoading,
    isError: navFailed
  } = useRoleBasedNavigation();

  return useMemo(() => {
    const navVisible =
      navLoading || navFailed
        ? null
        : collectVisibleModuleNames(filteredNavigation, realms);
    return activeModuleNames(admin, adminFailed, navVisible);
  }, [admin, adminFailed, filteredNavigation, realms, navLoading, navFailed]);
}
