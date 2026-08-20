import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { Provider } from 'react-redux';
import { createElement, type ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { server } from 'test/server';
import { setupStore, type TestStore } from 'test/render';
import { url } from 'test/handlers';
import {
  observabilityApi,
  useGetLogPreviewQuery
} from './observabilityApi';
import type {
  LogLevelsView,
  PermanentLogLevelsInput
} from 'types/observability';

const logLevelsView: LogLevelsView = {
  global: 'info',
  modules: [
    { name: 'auth', effective: 'info', hasOverride: false },
    {
      name: 'logging',
      effective: 'debug',
      override: 'error',
      hasOverride: true
    }
  ],
  diagnostics: [
    {
      module: 'logging',
      level: 'debug',
      startedAt: '2026-08-20T12:00:00Z',
      startedBy: 'operator-1'
    }
  ],
  logProvider: {
    available: true,
    grafanaUrl: 'https://grafana.example.test/explore'
  },
  updatedAt: '2026-08-20T12:00:00Z',
  updatedBy: 'operator-1'
};

const wrapperFor = (store: TestStore) => {
  const QueryProvider = ({ children }: { children: ReactNode }) =>
    createElement(Provider, { store, children });
  return QueryProvider;
};

describe('observabilityApi', () => {
  it('keeps a durable override distinct from a diagnostic effective level', () => {
    expect(logLevelsView.modules).toContainEqual({
      name: 'logging',
      effective: 'debug',
      override: 'error',
      hasOverride: true
    });
    expect(logLevelsView.modules[1]?.override).toBe('error');
  });

  it('applies the complete permanent configuration and refreshes the snapshot', async () => {
    const requests: Array<{ method: string; body: unknown }> = [];
    let snapshots = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels'), () => {
        snapshots += 1;
        return HttpResponse.json(logLevelsView);
      }),
      http.put(url('/v1/admin/observability/log-levels'), async ({ request }) => {
        requests.push({
          method: request.method,
          body: await request.json()
        });
        return HttpResponse.json(logLevelsView);
      })
    );
    const store = setupStore();
    const input: PermanentLogLevelsInput = {
      global: 'warn',
      perModule: { auth: 'debug', logging: 'error' },
      expectedUpdatedAt: '2026-08-20T12:00:00Z'
    };

    await store.dispatch(
      observabilityApi.endpoints.getLogLevels.initiate(undefined)
    );
    await store
      .dispatch(
        observabilityApi.endpoints.applyPermanentLogLevels.initiate(input)
      )
      .unwrap();

    await waitFor(() => expect(snapshots).toBe(2));
    expect(requests).toEqual([
      {
        method: 'PUT',
        body: {
          global: 'warn',
          perModule: { auth: 'debug', logging: 'error' },
          expectedUpdatedAt: '2026-08-20T12:00:00Z'
        }
      }
    ]);
  });

  it('starts a diagnostic through an encoded module path and refreshes the snapshot', async () => {
    const captured: { method: string | null; path: string | null; body: unknown } = {
      method: null,
      path: null,
      body: null
    };
    let snapshots = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels'), () => {
        snapshots += 1;
        return HttpResponse.json(logLevelsView);
      }),
      http.put('*', async ({ request }) => {
        captured.method = request.method;
        captured.path = new URL(request.url).pathname;
        captured.body = await request.json();
        return HttpResponse.json(logLevelsView);
      })
    );
    const store = setupStore();

    await store.dispatch(
      observabilityApi.endpoints.getLogLevels.initiate(undefined)
    );
    await store
      .dispatch(
        observabilityApi.endpoints.startDiagnostic.initiate({
          module: 'logging/worker #1',
          level: 'debug',
          durationMinutes: 60
        })
      )
      .unwrap();

    await waitFor(() => expect(snapshots).toBe(2));
    expect(captured).toEqual({
      method: 'PUT',
      path: '/v1/admin/observability/log-levels/logging%2Fworker%20%231/diagnostic',
      body: { level: 'debug', durationMinutes: 60 }
    });
  });

  it('stops a diagnostic through an encoded module path and refreshes the snapshot', async () => {
    const captured: { method: string | null; path: string | null } = {
      method: null,
      path: null
    };
    let snapshots = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels'), () => {
        snapshots += 1;
        return HttpResponse.json(logLevelsView);
      }),
      http.delete('*', ({ request }) => {
        captured.method = request.method;
        captured.path = new URL(request.url).pathname;
        return HttpResponse.json(logLevelsView);
      })
    );
    const store = setupStore();

    await store.dispatch(
      observabilityApi.endpoints.getLogLevels.initiate(undefined)
    );
    await store
      .dispatch(
        observabilityApi.endpoints.stopDiagnostic.initiate({
          module: 'logging/worker #1'
        })
      )
      .unwrap();

    await waitFor(() => expect(snapshots).toBe(2));
    expect(captured).toEqual({
      method: 'DELETE',
      path: '/v1/admin/observability/log-levels/logging%2Fworker%20%231/diagnostic'
    });
  });

  it('serializes all bounded log-preview filters as query parameters', async () => {
    const captured: { method: string | null; params: URLSearchParams | null } = {
      method: null,
      params: null
    };
    server.use(
      http.get(url('/v1/admin/observability/log-levels/logs'), ({ request }) => {
        captured.method = request.method;
        captured.params = new URL(request.url).searchParams;
        return HttpResponse.json({ events: [] });
      })
    );
    const store = setupStore();

    await store
      .dispatch(
        observabilityApi.endpoints.getLogPreview.initiate({
          module: 'auth',
          windowMinutes: 15,
          level: 'warn',
          q: 'request id: abc 123',
          limit: 25
        })
      )
      .unwrap();

    expect(captured.method).toBe('GET');
    expect(captured.params?.get('module')).toBe('auth');
    expect(captured.params?.get('windowMinutes')).toBe('15');
    expect(captured.params?.get('level')).toBe('warn');
    expect(captured.params?.get('q')).toBe('request id: abc 123');
    expect(captured.params?.get('limit')).toBe('25');
  });

  it('skips the preview query when no module is selected', async () => {
    let requests = 0;
    server.use(
      http.get(url('/v1/admin/observability/log-levels/logs'), () => {
        requests += 1;
        return HttpResponse.json({ events: [] });
      })
    );
    const store = setupStore();
    const { result } = renderHook(() => useGetLogPreviewQuery(undefined), {
      wrapper: wrapperFor(store)
    });

    await waitFor(() => expect(result.current.isUninitialized).toBe(true));
    expect(requests).toBe(0);
  });
});
