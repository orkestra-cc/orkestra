import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { complianceApi } from './complianceApi';

// These tests exercise the RTK Query slice's request-building in isolation:
// dispatch an endpoint, let MSW capture the outbound request, and assert the
// URL / query / body the SPA actually sends.

describe('complianceApi', () => {
  it('listAuditEvents forwards actionPrefix + limit as query params', async () => {
    let captured: { prefix: string | null; limit: string | null } | null = null;
    server.use(
      http.get(url('/v1/admin/audit-events'), ({ request }) => {
        const sp = new URL(request.url).searchParams;
        captured = { prefix: sp.get('actionPrefix'), limit: sp.get('limit') };
        return HttpResponse.json({ items: [], total: 0, limit: 50, offset: 0 });
      })
    );

    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.listAuditEvents.initiate({
          actionPrefix: 'auth.',
          limit: 25
        })
      )
      .unwrap();

    expect(captured).toEqual({ prefix: 'auth.', limit: '25' });
  });

  it('listAuditEvents defaults the limit to 50 when unset', async () => {
    let limit: string | null = null;
    server.use(
      http.get(url('/v1/admin/audit-events'), ({ request }) => {
        limit = new URL(request.url).searchParams.get('limit');
        return HttpResponse.json({ items: [], total: 0, limit: 50, offset: 0 });
      })
    );

    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(complianceApi.endpoints.listAuditEvents.initiate())
      .unwrap();

    expect(limit).toBe('50');
  });

  it('executeErasureRequest POSTs the mode body to the per-id execute path', async () => {
    let captured: { path: string; body: unknown } | null = null;
    server.use(
      http.post(
        url('/v1/admin/compliance/erasure-requests/:id/execute'),
        async ({ request }) => {
          captured = {
            path: new URL(request.url).pathname,
            body: await request.json()
          };
          return HttpResponse.json({ purged: {} });
        }
      )
    );

    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.executeErasureRequest.initiate({
          id: 'req-1',
          mode: 'hard_delete'
        })
      )
      .unwrap();

    expect(captured!.path).toBe(
      '/v1/admin/compliance/erasure-requests/req-1/execute'
    );
    expect(captured!.body).toEqual({ mode: 'hard_delete' });
  });

  it('releaseLegalHold DELETEs the per-id path with the release reason', async () => {
    let captured: { path: string; method: string; body: unknown } | null = null;
    server.use(
      http.delete(
        url('/v1/admin/compliance/legal-holds/:id'),
        async ({ request }) => {
          captured = {
            path: new URL(request.url).pathname,
            method: request.method,
            body: await request.json()
          };
          return new HttpResponse(null, { status: 204 });
        }
      )
    );

    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.releaseLegalHold.initiate({
          id: 'hold-9',
          releaseReason: 'case closed'
        })
      )
      .unwrap();

    expect(captured!.path).toBe('/v1/admin/compliance/legal-holds/hold-9');
    expect(captured!.method).toBe('DELETE');
    expect(captured!.body).toEqual({ releaseReason: 'case closed' });
  });
});
