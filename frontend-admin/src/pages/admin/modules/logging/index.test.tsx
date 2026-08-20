import {
  act,
  fireEvent,
  screen,
  waitFor,
  within
} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useLocation } from 'react-router';
import { renderWithProviders } from 'test/render';
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
    { name: 'logging', effective: 'debug', hasOverride: true }
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

const renderAt = (search = '') =>
  renderWithProviders(
    <>
      <LocationProbe />
      <LoggingModulePage />
    </>,
    { routerEntries: [`/admin/modules/logging${search}`] }
  );

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
                { name: 'auth', effective: 'debug', hasOverride: true },
                { name: 'logging', effective: 'debug', hasOverride: true }
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
