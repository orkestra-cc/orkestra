import { render, screen } from '@testing-library/react';
import { type ReactElement } from 'react';
import { Provider } from 'react-redux';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { setupStore } from 'test/render';
import * as staleChunk from 'helpers/staleChunk';
import { buildCoreRoutes } from './coreRoutes';

const Throw = ({ error }: { error: unknown }): ReactElement => {
  throw error;
};

const renderFailingRoute = (error: unknown) => {
  const [rootRoute] = buildCoreRoutes([]);
  const router = createMemoryRouter(
    [
      {
        path: '/',
        errorElement: rootRoute.errorElement,
        children: [{ index: true, element: <Throw error={error} /> }]
      }
    ],
    { initialEntries: ['/'] }
  );
  render(
    <Provider store={setupStore()}>
      <RouterProvider router={router} />
    </Provider>
  );
};

describe('root route error handling', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows a friendly error page whose dashboard button loads the page from the server', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    renderFailingRoute(new Error('boom'));

    expect(
      screen.getByRole('heading', { name: /whoops, something went wrong/i })
    ).toBeInTheDocument();
    // A full page load, not a router navigation: after a deploy the running
    // bundle can no longer load its lazy pages, so a client-side navigation
    // would land on this page again.
    expect(
      screen.getByRole('button', { name: /back to dashboard/i })
    ).toHaveAttribute('href', '/user/dashboard');
  });

  it('reloads the page when a lazy page of a previous deploy is gone', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.spyOn(staleChunk, 'staleChunkReloadAllowed').mockReturnValue(true);
    const reload = vi
      .spyOn(staleChunk, 'reloadForStaleChunk')
      .mockReturnValue(true);

    renderFailingRoute(
      new TypeError(
        'Failed to fetch dynamically imported module: /assets/UserDashboard-old.js'
      )
    );

    expect(reload).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole('heading', { name: /whoops, something went wrong/i })
    ).not.toBeInTheDocument();
  });

  it('shows the error page when it already reloaded moments ago', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.spyOn(staleChunk, 'staleChunkReloadAllowed').mockReturnValue(false);
    const reload = vi.spyOn(staleChunk, 'reloadForStaleChunk');

    renderFailingRoute(new TypeError('Importing a module script failed.'));

    expect(reload).not.toHaveBeenCalled();
    expect(
      screen.getByRole('heading', { name: /whoops, something went wrong/i })
    ).toBeInTheDocument();
  });
});
