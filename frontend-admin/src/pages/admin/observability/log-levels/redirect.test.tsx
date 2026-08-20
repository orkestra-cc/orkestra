import { render, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { buildCoreRoutes } from 'routes/coreRoutes';

vi.mock('components/authentication/ProtectedRoute', () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>
}));

const findRoute = (
  routes: RouteObject[],
  path: string
): RouteObject | undefined => {
  for (const route of routes) {
    if (route.path === path) return route;
    if (route.children) {
      const child = findRoute(route.children, path);
      if (child) return child;
    }
  }
  return undefined;
};

describe('legacy log-level route', () => {
  it('replaces history with the logging workspace permanent levels section', async () => {
    const legacyRoute = findRoute(
      buildCoreRoutes([]),
      'observability/log-levels'
    );
    expect(legacyRoute).toBeDefined();
    const router = createMemoryRouter(
      [
        {
          path: '/admin/observability/log-levels',
          element: legacyRoute?.element
        },
        { path: '/admin/modules/logging', element: <div>Logging workspace</div> }
      ],
      {
        initialEntries: ['/previous', '/admin/observability/log-levels'],
        initialIndex: 1
      }
    );

    render(<RouterProvider router={router} />);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/admin/modules/logging');
      expect(router.state.location.search).toBe('?section=levels');
    });
    expect(router.state.historyAction).toBe('REPLACE');
  });
});
