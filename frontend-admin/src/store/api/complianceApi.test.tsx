import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { complianceApi, type PolicyInput } from './complianceApi';

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

  it('listErasureRequests sends status/userUuid/limit/after, omitting the default open status', async () => {
    const seen: string[] = [];
    server.use(
      http.get(url('/v1/admin/compliance/erasure-requests'), ({ request }) => {
        seen.push(new URL(request.url).search);
        return HttpResponse.json({ items: [] });
      })
    );
    const { store } = renderWithProviders(<></>);
    const ep = complianceApi.endpoints.listErasureRequests;
    await store.dispatch(ep.initiate()).unwrap();
    await store
      .dispatch(
        ep.initiate({
          status: 'completed',
          userUuid: 'u-1',
          limit: 20,
          after: 'cur'
        })
      )
      .unwrap();
    expect(seen[0]).toBe('');
    const sp = new URLSearchParams(seen[1]);
    expect(Object.fromEntries(sp)).toEqual({
      status: 'completed',
      userUuid: 'u-1',
      limit: '20',
      after: 'cur'
    });
  });

  it('openErasureRequest and approveErasureRequest POST their bodies', async () => {
    const got: { path: string; body: unknown }[] = [];
    server.use(
      http.post(
        url('/v1/admin/compliance/erasure-requests'),
        async ({ request }) => {
          got.push({
            path: new URL(request.url).pathname,
            body: await request.json()
          });
          return HttpResponse.json({ uuid: 'er-9' }, { status: 201 });
        }
      ),
      http.post(
        url('/v1/admin/compliance/erasure-requests/:id/approve'),
        async ({ request }) => {
          got.push({
            path: new URL(request.url).pathname,
            body: await request.json()
          });
          return HttpResponse.json({ uuid: 'er-9' });
        }
      )
    );
    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.openErasureRequest.initiate({
          userUuid: 'u-1',
          reason: 'why',
          mode: 'anonymize'
        })
      )
      .unwrap();
    await store
      .dispatch(
        complianceApi.endpoints.approveErasureRequest.initiate({
          id: 'er-9',
          mode: 'anonymize',
          note: 'ok'
        })
      )
      .unwrap();
    expect(got).toEqual([
      {
        path: '/v1/admin/compliance/erasure-requests',
        body: { userUuid: 'u-1', reason: 'why', mode: 'anonymize' }
      },
      {
        path: '/v1/admin/compliance/erasure-requests/er-9/approve',
        body: { mode: 'anonymize', note: 'ok' }
      }
    ]);
  });

  it('executeErasureRequest omits the mode when none is given', async () => {
    let body: unknown;
    server.use(
      http.post(
        url('/v1/admin/compliance/erasure-requests/:id/execute'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({ status: 'completed' });
        }
      )
    );
    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.executeErasureRequest.initiate({ id: 'r' })
      )
      .unwrap();
    expect(body).toEqual({});
  });

  it('getErasureReceipt GETs the receipt path', async () => {
    server.use(
      http.get(
        url('/v1/admin/compliance/erasure-requests/:id/receipt'),
        ({ params }) =>
          HttpResponse.json({ requestUuid: params.id, status: 'completed' })
      )
    );
    const { store } = renderWithProviders(<></>);
    const r = await store
      .dispatch(complianceApi.endpoints.getErasureReceipt.initiate('er-1'))
      .unwrap();
    expect(r.requestUuid).toBe('er-1');
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

  it('updatePolicy PUTs the expected version, the policy, the reason and the acknowledgement', async () => {
    let body: unknown = null;
    server.use(
      http.put(
        url('/v1/admin/compliance/policies/p-1'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json(
            { applied: false, warnings: [] },
            { status: 202 }
          );
        }
      )
    );
    const { store } = renderWithProviders(<></>);
    const policy: PolicyInput = {
      name: 'Strict',
      description: '',
      logContent: {
        ipAddress: 'omitted',
        userAgent: 'full',
        subjectIds: 'uuid',
        piiKeys: ['email'],
        scanFreeText: true
      },
      retention: { privileged_change: 400 },
      accountability: {
        role: 'processor',
        ropaRef: 'R',
        assessmentRef: 'D',
        owner: ''
      }
    };
    const res = await store
      .dispatch(
        complianceApi.endpoints.updatePolicy.initiate({
          id: 'p-1',
          expectedVersion: 3,
          policy,
          reason: 'contratto',
          acknowledgeWarnings: true
        })
      )
      .unwrap();
    expect(res.applied).toBe(false);
    expect(body).toEqual({
      expectedVersion: 3,
      policy,
      reason: 'contratto',
      acknowledgeWarnings: true
    });
  });

  it('deletePolicy sends the expected version as a query and the reason in the body', async () => {
    let seen: { version: string | null; body: unknown } | null = null;
    server.use(
      http.delete(
        url('/v1/admin/compliance/policies/p-1'),
        async ({ request }) => {
          seen = {
            version: new URL(request.url).searchParams.get('expectedVersion'),
            body: await request.json()
          };
          return new HttpResponse(null, { status: 204 });
        }
      )
    );
    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.deletePolicy.initiate({
          id: 'p-1',
          expectedVersion: 2,
          reason: 'non più usata'
        })
      )
      .unwrap();
    expect(seen).toEqual({ version: '2', body: { reason: 'non più usata' } });
  });

  it('listChangeRequests filters by status unless all are asked for', async () => {
    const statuses: (string | null)[] = [];
    server.use(
      http.get(url('/v1/admin/compliance/change-requests'), ({ request }) => {
        statuses.push(new URL(request.url).searchParams.get('status'));
        return HttpResponse.json({ items: [] });
      })
    );
    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(complianceApi.endpoints.listChangeRequests.initiate('pending'))
      .unwrap();
    await store
      .dispatch(complianceApi.endpoints.listChangeRequests.initiate('all'))
      .unwrap();
    expect(statuses).toEqual(['pending', null]);
  });

  it('unassignPolicy DELETEs with the reason and the acknowledgement', async () => {
    let body: unknown = null;
    server.use(
      http.delete(
        url('/v1/admin/compliance/policy-assignments/t-1'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({ applied: true, warnings: [] });
        }
      )
    );
    const { store } = renderWithProviders(<></>);
    await store
      .dispatch(
        complianceApi.endpoints.unassignPolicy.initiate({
          tenantId: 't-1',
          reason: 'fine',
          acknowledgeWarnings: true
        })
      )
      .unwrap();
    expect(body).toEqual({ reason: 'fine', acknowledgeWarnings: true });
  });
});
