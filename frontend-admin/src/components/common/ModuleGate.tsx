import { type ReactNode } from 'react';
import { Navigate } from 'react-router';
import { useGetModulesQuery } from 'store/api/moduleApi';
import { useAppSelector } from 'store/hooks';

interface ModuleGateProps {
  module: string;
  children: ReactNode;
}

/**
 * Gates route rendering based on backend module enabled state.
 *
 * - Admin users: module state is fetched -> disabled modules show 404
 * - Non-admin users: query returns 403 -> isError -> children render
 *   (backend RBAC is the real gate)
 * - Loading state: children render (no flash of 404)
 *
 * Skipped until state.auth.accessToken is set — see useModuleApi.ts for
 * the page-refresh race rationale.
 */
export default function ModuleGate({ module, children }: ModuleGateProps) {
  const hasAccessToken = useAppSelector((s) => !!s.auth.accessToken);
  const { data: modules, isLoading, isError } = useGetModulesQuery(undefined, {
    skip: !hasAccessToken,
  });

  // While loading or on error (non-admin gets 403), allow through
  if (isLoading || isError) return <>{children}</>;

  const moduleConfig = modules?.find((m) => m.moduleName === module);
  if (moduleConfig && !moduleConfig.enabled) {
    return <Navigate to="/errors/404" replace />;
  }

  return <>{children}</>;
}
