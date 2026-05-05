import { useEffect, useRef } from 'react';
import { moduleCatalog } from 'modules';
import { useGetModulesQuery } from 'store/api/moduleApi';
import { useAppSelector } from 'store/hooks';

/**
 * Injects RTK Query API slices for all enabled modules.
 * Call once at app root (e.g. App.tsx).
 *
 * For admin users: only injects slices for modules the backend reports as
 * enabled/running. For non-admin users: the query fails with 403, so this
 * hook is a no-op — pages import their API slices directly as a fallback.
 *
 * Gated on state.auth.accessToken so this query never fires before the
 * session endpoint has minted one. Firing unauthenticated races with
 * /v1/auth/session at the backend middleware — both inline-rotate the
 * refresh cookie, the CAS-loss branch trips the family-replay guard, and
 * the whole session is revoked on page refresh.
 */
export function useModuleApiInjection() {
  const hasAccessToken = useAppSelector((s) => !!s.auth.accessToken);
  const { data: modules } = useGetModulesQuery(undefined, { skip: !hasAccessToken });
  const injectedRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    if (!modules) return;

    const enabled = modules
      .filter((m) => m.enabled && m.status === 'running')
      .map((m) => m.moduleName);

    for (const name of enabled) {
      if (injectedRef.current.has(name)) continue;
      const manifest = moduleCatalog[name];
      if (manifest?.injectApi) {
        manifest.injectApi();
        injectedRef.current.add(name);
      }
    }
  }, [modules]);
}
