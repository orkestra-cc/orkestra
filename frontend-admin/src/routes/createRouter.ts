import { createBrowserRouter } from 'react-router';
import { moduleCatalog } from 'modules';
import { buildCoreRoutes } from './coreRoutes';
import { getReferenceRoutes, getLayoutVariantRoutes } from './referenceRoutes';

/**
 * Creates the app router with core + module + reference routes.
 *
 * Module routes are gated by ModuleGate at render time (not at registration).
 * Reference routes are only included in development builds.
 * Layout variant routes (VerticalNav, TopNav, etc.) are appended as top-level routes.
 */
export function createAppRouter() {
  const moduleRoutes = Object.values(moduleCatalog).flatMap((m) => m.routes());
  const referenceRoutes = getReferenceRoutes();
  const layoutVariantRoutes = getLayoutVariantRoutes();

  const routes = [
    ...buildCoreRoutes([...moduleRoutes, ...referenceRoutes]),
    ...layoutVariantRoutes,
  ];

  return createBrowserRouter(routes, {
    basename: import.meta.env.VITE_PUBLIC_URL,
  });
}
