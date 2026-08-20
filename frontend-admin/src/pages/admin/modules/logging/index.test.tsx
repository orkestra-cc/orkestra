import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within
} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Provider } from 'react-redux';
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router';
import { setupStore } from 'test/render';
import { url } from 'test/handlers';
import { server } from 'test/server';
import type { ModuleConfig } from 'store/api/moduleApi';
import type { LogLevelsView } from 'types/observability';
import LoggingModulePage from './index';

const loggingModule: ModuleConfig = {
  moduleName: 'logging',
  displayName: 'Logging',
  description: 'Runtime logging operations',
  category: 'core',
  enabled: true,
  status: 'running',
  needsRestart: false,
  configValues: {},
  secretStatus: {},
  configSchema: [],
  dependsOn: ['auth'],
  providedServices: [],
  requiredServices: [],
  optionalServices: [],
  activeEnvironment: 'production',
  availableEnvironments: ['production'],
  createdAt: '2026-08-20T10:00:00Z',
  updatedAt: '2026-08-20T12:00:00Z'
};

const authModule: ModuleConfig = {
  ...loggingModule,
  moduleName: 'auth',
  displayName: 'Authentication',
  description: 'Authentication',
  dependsOn: []
};

const snapshot = (overrides: Partial<LogLevelsView> = {}): LogLevelsView => ({
  global: 'info',
  modules: [
    { name: 'auth', effective: 'info', hasOverride: false },
    {
      name: 'logging',
      effective: 'debug',
      override: 'debug',
      hasOverride: true
    }
  ],
  diagnostics: [],
  logProvider: {
    available: true,
    grafanaUrl: 'https://grafana.example.test/explore'
  },
  updatedAt: '2026-08-20T12:00:00Z',
  updatedBy: 'operator-1',
  ...overrides
});

let currentSearch = '';

const LocationProbe = () => {
  currentSearch = useLocation().search;
  return null;
};

const stubWorkspace = (view: LogLevelsView = snapshot()) => {
  server.use(
    http.get(url('/v1/admin/modules'), () =>
      HttpResponse.json({ modules: [loggingModule, authModule] })
    ),
    http.get(url('/v1/admin/modules/health'), () =>
      HttpResponse.json({
        modules: [
          { moduleName: 'logging', status: 'healthy' },
          { moduleName: 'auth', status: 'healthy' }
        ],
        checkedAt: '2026-08-20T12:00:00Z'
      })
    ),
    http.get(url('/v1/admin/modules/logging'), () =>
      HttpResponse.json(loggingModule)
    ),
    http.get(url('/v1/admin/observability/log-levels'), () =>
      HttpResponse.json(view)
    ),
    http.get(url('/v1/admin/observability/log-levels/logs'), () =>
      HttpResponse.json({ events: [] })
    )
  );
};

const renderAt = (search = '') => {
  const store = setupStore();
  const router = createMemoryRouter(
    [
      {
        path: '/admin/modules/logging',
        element: (
          <>
            <LocationProbe />
            <LoggingModulePage />
          </>
        )
      },
      { path: '/else', element: <div>Outside workspace</div> }
    ],
    { initialEntries: [`/admin/modules/logging${search}`] }
  );
  return {
    router,
    store,
    ...render(
      <Provider store={store}>
        <RouterProvider router={router} />
      </Provider>
    )
  };
};

describe('LoggingModulePage', () => {
  beforeEach(() => {
    currentSearch = '';
    stubWorkspace();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('opens the overview by default with standard module context and logging summary', async () => {
    renderAt();

    expect(
      await screen.findByRole('heading', { name: 'Logging' })
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Overview' })).toHaveAttribute(
      'aria-current',
      'true'
    );
    expect(screen.getByText('Global level')).toBeInTheDocument();
    expect(screen.getByText('Permanent overrides')).toBeInTheDocument();
    expect(screen.getByText('Active diagnostics')).toBeInTheDocument();
    expect(screen.getByText('Log provider')).toBeInTheDocument();
    expect(screen.getByText('Health')).toBeInTheDocument();
    expect(screen.getByText('Dependencies & Services')).toBeInTheDocument();
  });

  it('opens permanent levels directly from the section URL', async () => {
    renderAt('?section=levels');

    expect(
      await screen.findByRole('heading', { name: 'Permanent log levels' })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Permanent levels' })
    ).toHaveAttribute('aria-current', 'true');
    expect(screen.getByLabelText('Permanent global log level')).toHaveValue(
      'info'
    );
    expect(screen.getByText('auth')).toBeInTheDocument();
  });

  it('normalizes a stale section to overview in the URL', async () => {
    renderAt('?section=removed-panel&trace=abc');

    expect(await screen.findByText('Global level')).toBeInTheDocument();
    await waitFor(() => expect(currentSearch).toContain('section=overview'));
    expect(currentSearch).toContain('trace=abc');
    expect(currentSearch).not.toContain('removed-panel');
  });

  it('normalizes an explicitly empty section to overview', async () => {
    renderAt('?section=&trace=abc');

    expect(await screen.findByText('Global level')).toBeInTheDocument();
    await waitFor(() => expect(currentSearch).toContain('section=overview'));
    expect(currentSearch).toContain('trace=abc');
  });

  it('counts permanent draft edits and discards them back to the server snapshot', async () => {
    const user = userEvent.setup();
    renderAt('?section=levels');

    const authLevel = await screen.findByLabelText(
      'Permanent log level for auth'
    );
    await user.selectOptions(authLevel, 'debug');

    expect(screen.getByText('1 unsaved change')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Discard' }));

    expect(screen.getByLabelText('Permanent log level for auth')).toHaveValue(
      'inherit'
    );
    expect(screen.queryByText('1 unsaved change')).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Apply changes' })
    ).not.toBeInTheDocument();
  });

  it('applies all permanent edits in one guarded batch', async () => {
    const user = userEvent.setup();
    const requests: unknown[] = [];
    server.use(
      http.put(
        url('/v1/admin/observability/log-levels'),
        async ({ request }) => {
          requests.push(await request.json());
          return HttpResponse.json(
            snapshot({
              global: 'warn',
              modules: [
                {
                  name: 'auth',
                  effective: 'debug',
                  override: 'debug',
                  hasOverride: true
                },
                {
                  name: 'logging',
                  effective: 'debug',
                  override: 'debug',
                  hasOverride: true
                }
              ],
              updatedAt: '2026-08-20T12:05:00Z'
            })
          );
        }
      )
    );
    renderAt('?section=levels');

    await user.selectOptions(
      await screen.findByLabelText('Permanent global log level'),
      'warn'
    );
    await user.selectOptions(
      screen.getByLabelText('Permanent log level for auth'),
      'debug'
    );
    await user.click(screen.getByRole('button', { name: 'Apply changes' }));

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0]).toEqual({
      global: 'warn',
      perModule: { auth: 'debug', logging: 'debug' },
      expectedUpdatedAt: '2026-08-20T12:00:00Z'
    });
  });

  it('submits the durable override rather than a diagnostic effective level', async () => {
    const user = userEvent.setup();
    const requests: unknown[] = [];
    stubWorkspace(
      snapshot({
        modules: [
          {
            name: 'auth',
            effective: 'debug',
            override: 'error',
            hasOverride: true
          },
          { name: 'logging', effective: 'info', hasOverride: false }
        ],
        diagnostics: [
          {
            module: 'auth',
            level: 'debug',
            startedAt: '2026-08-20T12:00:00Z',
            startedBy: 'operator-1',
            expiresAt: '2026-08-20T13:00:00Z'
          }
        ]
      })
    );
    server.use(
      http.put(
        url('/v1/admin/observability/log-levels'),
        async ({ request }) => {
          requests.push(await request.json());
          return HttpResponse.json(snapshot());
        }
      )
    );
    renderAt('?section=levels');

    expect(
      await screen.findByLabelText('Permanent log level for auth')
    ).toHaveValue('error');
    await user.selectOptions(
      screen.getByLabelText('Permanent global log level'),
      'warn'
    );
    await user.click(screen.getByRole('button', { name: 'Apply changes' }));

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0]).toEqual({
      global: 'warn',
      perModule: { auth: 'error' },
      expectedUpdatedAt: '2026-08-20T12:00:00Z'
    });
  });

  it('preserves a dirty permanent draft across section changes', async () => {
    const user = userEvent.setup();
    renderAt('?section=levels');

    await user.selectOptions(
      await screen.findByLabelText('Permanent log level for auth'),
      'debug'
    );
    await user.click(screen.getByRole('button', { name: 'Overview' }));
    await user.click(screen.getByRole('button', { name: 'Permanent levels' }));

    expect(screen.getByLabelText('Permanent log level for auth')).toHaveValue(
      'debug'
    );
    expect(screen.getByText('1 unsaved change')).toBeInTheDocument();
  });

  it('offers to reload instead of overwriting after a permanent-edit conflict', async () => {
    const user = userEvent.setup();
    server.use(
      http.put(url('/v1/admin/observability/log-levels'), () =>
        HttpResponse.json(
          {
            status: 409,
            title: 'Conflict',
            detail: 'The log-level snapshot changed'
          },
          { status: 409 }
        )
      )
    );
    renderAt('?section=levels');

    await user.selectOptions(
      await screen.findByLabelText('Permanent global log level'),
      'warn'
    );
    await user.click(screen.getByRole('button', { name: 'Apply changes' }));

    expect(
      await screen.findByText(
        'Another operator changed these levels. Reload the latest snapshot before editing again.'
      )
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Reload latest snapshot' })
    ).toBeInTheDocument();
  });

  it('keeps the draft and conflict recovery visible when a 409 refetches a newer snapshot', async () => {
    const user = userEvent.setup();
    let currentSnapshot = snapshot();
    let getRequests = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels'), () => {
        getRequests += 1;
        return HttpResponse.json(currentSnapshot);
      }),
      http.put(url('/v1/admin/observability/log-levels'), () => {
        currentSnapshot = snapshot({
          global: 'error',
          updatedAt: '2026-08-20T12:05:00Z'
        });
        return HttpResponse.json(
          {
            status: 409,
            title: 'Conflict',
            detail: 'The log-level snapshot changed'
          },
          { status: 409 }
        );
      })
    );
    renderAt('?section=levels');

    await user.selectOptions(
      await screen.findByLabelText('Permanent global log level'),
      'warn'
    );
    await user.click(screen.getByRole('button', { name: 'Apply changes' }));

    expect(
      await screen.findByRole('button', { name: 'Reload latest snapshot' })
    ).toBeInTheDocument();
    await waitFor(() => expect(getRequests).toBeGreaterThan(1));
    expect(screen.getByLabelText('Permanent global log level')).toHaveValue(
      'warn'
    );
    await user.click(screen.getByRole('button', { name: 'Overview' }));
    await user.click(screen.getByRole('button', { name: 'Permanent levels' }));
    expect(
      screen.getByRole('button', { name: 'Reload latest snapshot' })
    ).toBeInTheDocument();
    expect(screen.getByLabelText('Permanent global log level')).toHaveValue(
      'warn'
    );

    await user.click(
      screen.getByRole('button', { name: 'Reload latest snapshot' })
    );
    await waitFor(() =>
      expect(screen.getByLabelText('Permanent global log level')).toHaveValue(
        'error'
      )
    );
    expect(
      screen.queryByRole('button', { name: 'Apply changes' })
    ).not.toBeInTheDocument();
  });

  it('blocks route navigation while permanent edits are unsaved', async () => {
    const user = userEvent.setup();
    const { router } = renderAt('?section=levels');

    await user.selectOptions(
      await screen.findByLabelText('Permanent log level for auth'),
      'debug'
    );
    act(() => {
      void router.navigate('/else');
    });

    expect(
      await screen.findByRole('heading', { name: 'Unsaved permanent changes' })
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin/modules/logging');
    await user.click(screen.getByRole('button', { name: 'Stay in workspace' }));
    expect(
      screen.queryByRole('heading', { name: 'Unsaved permanent changes' })
    ).not.toBeInTheDocument();
  });

  it('starts a diagnostic immediately with the selected duration', async () => {
    const user = userEvent.setup();
    let captured: { path: string; body: unknown } | undefined;
    server.use(
      http.put('*', async ({ request }) => {
        const path = new URL(request.url).pathname;
        if (!path.endsWith('/diagnostic')) return;
        captured = { path, body: await request.json() };
        return HttpResponse.json(snapshot());
      })
    );
    renderAt('?section=diagnostics');

    await user.selectOptions(
      await screen.findByLabelText('Diagnostic module'),
      'auth'
    );
    await user.selectOptions(
      screen.getByLabelText('Diagnostic level'),
      'debug'
    );
    await user.selectOptions(
      screen.getByLabelText('Diagnostic duration'),
      '60'
    );
    await user.click(screen.getByRole('button', { name: 'Start diagnostic' }));

    await waitFor(() => expect(captured).toBeDefined());
    expect(captured).toEqual({
      path: '/v1/admin/observability/log-levels/auth/diagnostic',
      body: { level: 'debug', durationMinutes: 60 }
    });
  });

  it('derives countdowns from server timestamps, flags no-expiry diagnostics, and stops one module', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date('2026-08-20T12:30:00Z'));
    stubWorkspace(
      snapshot({
        diagnostics: [
          {
            module: 'auth',
            level: 'debug',
            startedAt: '2026-08-20T12:00:00Z',
            startedBy: 'operator-1',
            expiresAt: '2026-08-20T12:45:00Z'
          },
          {
            module: 'logging',
            level: 'debug',
            startedAt: '2026-08-20T12:10:00Z',
            startedBy: 'operator-2'
          }
        ]
      })
    );
    let stopped = '';
    server.use(
      http.delete('*', ({ request }) => {
        stopped = new URL(request.url).pathname;
        return HttpResponse.json(snapshot());
      })
    );
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderAt('?section=diagnostics');

    expect(await screen.findByText('15m 0s remaining')).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(1000));
    expect(await screen.findByText('14m 59s remaining')).toBeInTheDocument();
    expect(
      screen.getByText(
        'No expiry — this diagnostic remains active until stopped.'
      )
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole('button', { name: 'Stop diagnostic for logging' })
    );
    await waitFor(() => expect(stopped).toContain('/logging/diagnostic'));
  });

  it('removes an expired diagnostic locally and refetches its snapshot once', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date('2026-08-20T12:30:00Z'));
    const view = snapshot({
      diagnostics: [
        {
          module: 'auth',
          level: 'debug',
          startedAt: '2026-08-20T12:00:00Z',
          startedBy: 'operator-1',
          expiresAt: '2026-08-20T12:30:01Z'
        }
      ]
    });
    let getRequests = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels'), () => {
        getRequests += 1;
        return HttpResponse.json(view);
      })
    );
    renderAt('?section=diagnostics');

    expect(await screen.findByText('0m 1s remaining')).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(1000));
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'Stop diagnostic for auth' })
      ).not.toBeInTheDocument()
    );
    await waitFor(() => expect(getRequests).toBe(2));

    act(() => vi.advanceTimersByTime(5000));
    expect(getRequests).toBe(2);
  });

  it('tracks overlapping diagnostic stops independently', async () => {
    const view = snapshot({
      diagnostics: [
        {
          module: 'auth',
          level: 'debug',
          startedAt: '2026-08-20T12:00:00Z',
          startedBy: 'operator-1'
        },
        {
          module: 'logging',
          level: 'debug',
          startedAt: '2026-08-20T12:00:00Z',
          startedBy: 'operator-1'
        }
      ]
    });
    stubWorkspace(view);
    const resolvers = new Map<string, () => void>();
    server.use(
      http.delete('*', async ({ request }) => {
        const pathSegments = new URL(request.url).pathname.split('/');
        const moduleName = decodeURIComponent(
          pathSegments[pathSegments.length - 2] ?? ''
        );
        await new Promise<void>(resolve => resolvers.set(moduleName, resolve));
        return HttpResponse.json(view);
      })
    );
    renderAt('?section=diagnostics');

    const authStop = await screen.findByRole('button', {
      name: 'Stop diagnostic for auth'
    });
    const loggingStop = screen.getByRole('button', {
      name: 'Stop diagnostic for logging'
    });
    fireEvent.click(authStop);
    fireEvent.click(loggingStop);

    await waitFor(() => {
      expect(authStop).toBeDisabled();
      expect(loggingStop).toBeDisabled();
    });
    act(() => resolvers.get('auth')?.());
    await waitFor(() => expect(authStop).toBeEnabled());
    expect(loggingStop).toBeDisabled();
    act(() => resolvers.get('logging')?.());
  });

  it('keeps level management usable when the log provider is unavailable', async () => {
    let previewRequests = 0;
    stubWorkspace(snapshot({ logProvider: { available: false } }));
    server.use(
      http.get(url('/v1/admin/observability/log-levels/logs'), () => {
        previewRequests += 1;
        return HttpResponse.json({ events: [] });
      })
    );
    renderAt('?section=logs');

    expect(
      await screen.findByText('Log preview is unavailable on this deployment.')
    ).toBeInTheDocument();
    expect(previewRequests).toBe(0);
    expect(
      screen.getByRole('button', { name: 'Permanent levels' })
    ).toBeEnabled();
  });

  it('distinguishes preview errors from empty results', async () => {
    server.use(
      http.get(url('/v1/admin/observability/log-levels/logs'), () =>
        HttpResponse.json(
          { detail: 'Log provider request failed' },
          { status: 502 }
        )
      )
    );
    const { unmount } = renderAt('?section=logs');
    expect(
      await screen.findByText(
        'Log preview failed. Check the provider and try again.'
      )
    ).toBeInTheDocument();

    unmount();
    stubWorkspace();
    renderAt('?section=logs');
    expect(
      await screen.findByText('No log events match these filters.')
    ).toBeInTheDocument();
  });

  it('renders bounded preview results, safe attributes, and the supplied Grafana link', async () => {
    const user = userEvent.setup();
    server.use(
      http.get(url('/v1/admin/observability/log-levels/logs'), () =>
        HttpResponse.json({
          events: [
            {
              timestamp: '2026-08-20T12:29:30Z',
              level: 'warn',
              message: 'Session refresh slowed',
              module: 'auth',
              attributes: { traceId: 'trace-123', requestId: 'request-456' }
            }
          ]
        })
      )
    );
    renderAt('?section=logs');

    const message = await screen.findByText('Session refresh slowed');
    expect(message).toBeInTheDocument();
    expect(
      within(message.closest('tr') as HTMLElement).getByText('warn')
    ).toBeInTheDocument();
    const grafana = screen.getByRole('link', { name: 'Open in Grafana' });
    expect(grafana).toHaveAttribute(
      'href',
      'https://grafana.example.test/explore'
    );

    await user.click(
      screen.getByRole('button', {
        name: 'Show attributes for auth at Aug 20, 2026, 12:29 PM'
      })
    );
    expect(screen.getByText('traceId')).toBeInTheDocument();
    expect(screen.getByText('trace-123')).toBeInTheDocument();
  });

  it('warns that free-text log messages may still contain personal data', async () => {
    renderAt('?section=logs');

    expect(
      await screen.findByText(
        'Structured attributes are minimized and redacted. Free-text messages may still contain personal data; review them before sharing.'
      )
    ).toBeInTheDocument();
  });

  it('announces workspace loading and preview result status', async () => {
    let resolveSnapshot: (() => void) | undefined;
    server.use(
      http.get(url('/v1/admin/observability/log-levels'), async () => {
        await new Promise<void>(resolve => {
          resolveSnapshot = resolve;
        });
        return HttpResponse.json(snapshot());
      })
    );
    const { unmount } = renderAt('?section=logs');

    expect(
      await screen.findByRole('status', { name: 'Loading logging operations' })
    ).toBeInTheDocument();
    act(() => resolveSnapshot?.());
    unmount();

    stubWorkspace();
    renderAt('?section=logs');
    const previewStatus = await screen.findByRole('status', {
      name: 'Log preview status'
    });
    expect(previewStatus).toHaveAttribute('aria-live', 'polite');
    expect(previewStatus).toHaveTextContent(
      'No log events match these filters.'
    );
  });

  it('uses supported theme variants for workspace actions', async () => {
    const user = userEvent.setup();
    stubWorkspace(
      snapshot({
        diagnostics: [
          {
            module: 'auth',
            level: 'debug',
            startedAt: '2026-08-20T12:00:00Z',
            startedBy: 'operator-1'
          }
        ]
      })
    );
    renderAt('?section=levels');

    await user.selectOptions(
      await screen.findByLabelText('Permanent log level for auth'),
      'debug'
    );
    expect(screen.getByRole('button', { name: 'Apply changes' })).toHaveClass(
      'btn-orkestra-primary'
    );

    await user.click(screen.getByRole('button', { name: 'Diagnostics' }));
    expect(
      screen.getByRole('button', { name: 'Start diagnostic' })
    ).toHaveClass('btn-orkestra-primary');
    expect(
      screen.getByRole('button', { name: 'Stop diagnostic for auth' })
    ).toHaveClass('btn-orkestra-danger');

    await user.click(screen.getByRole('button', { name: 'Log preview' }));
    expect(
      screen.getByRole('button', { name: 'Refresh log preview' })
    ).toHaveClass('btn-orkestra-primary');
    expect(screen.getByRole('link', { name: 'Open in Grafana' })).toHaveClass(
      'btn-orkestra-primary'
    );
  });

  it('refreshes the preview every five seconds only after auto-refresh is enabled', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let previewRequests = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels/logs'), () => {
        previewRequests += 1;
        return HttpResponse.json({ events: [] });
      })
    );
    renderAt('?section=logs');

    await waitFor(() => expect(previewRequests).toBe(1));
    act(() => vi.advanceTimersByTime(5000));
    expect(previewRequests).toBe(1);
    fireEvent.click(screen.getByLabelText('Refresh every five seconds'));
    act(() => vi.advanceTimersByTime(5000));
    await waitFor(() => expect(previewRequests).toBe(2));

    const preview = screen
      .getByRole('heading', { name: 'Recent log preview' })
      .closest('.card') as HTMLElement;
    expect(
      within(preview).getByRole('button', { name: 'Refresh log preview' })
    ).toBeInTheDocument();
  });
});
