import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { url } from 'test/handlers';
import { serviceAccountApi } from './serviceAccountApi';
import type { ServiceAccountWithSecret } from 'types/serviceAccounts';

// serviceAccountApi is a platform-level admin surface (ADR-0014
// client-credentials service accounts) — every request must be
// tenant-agnostic (no X-Tenant-ID) even when the caller has a current org
// selected. Model the store/request harness on baseApi.tenantHeader.test.ts.

const captureRequest = () => {
  const captured: { url: string | null; tenant: string | null } = {
    url: null,
    tenant: null
  };
  server.use(
    http.all('*', ({ request }) => {
      captured.url = request.url;
      captured.tenant = request.headers.get('X-Tenant-ID');
      return HttpResponse.json([]);
    })
  );
  return captured;
};

// A store with a real current org selected — if the endpoints were
// accidentally tenant-scoped, the header would show up here.
const storeWithTenant = () =>
  setupStore({
    tenant: {
      memberships: [],
      currentOrgId: 'real-org-uuid',
      permissions: [],
      features: [],
      systemRole: '',
      loading: false,
      error: null,
      impersonatedTenantId: null,
      impersonatedTenantName: null
    }
  } as never);

describe('serviceAccountApi', () => {
  it('list is tenant agnostic', async () => {
    const captured = captureRequest();
    const store = storeWithTenant();

    await store.dispatch(
      serviceAccountApi.endpoints.listServiceAccounts.initiate()
    );

    expect(captured.url).toContain('/v1/admin/service-accounts');
    expect(captured.tenant).toBeNull();
  });

  it('create returns the one-time secret', async () => {
    const responseBody: ServiceAccountWithSecret = {
      id: 'sa-1',
      name: 'hermes-agent',
      email: 'sa-1@service.local',
      isActive: true,
      activeCredentials: 1,
      createdAt: '2026-08-18T00:00:00Z',
      clientId: 'client-abc',
      clientSecret: 'one-time-secret-value'
    };
    server.use(
      http.post(url('/v1/admin/service-accounts'), () =>
        HttpResponse.json(responseBody)
      )
    );
    const store = storeWithTenant();

    const result = await store
      .dispatch(
        serviceAccountApi.endpoints.createServiceAccount.initiate({
          name: 'hermes-agent'
        })
      )
      .unwrap();

    expect(result.clientSecret).toBe('one-time-secret-value');
  });

  it('revoke hits the credential path', async () => {
    const captured = captureRequest();
    const store = storeWithTenant();

    await store.dispatch(
      serviceAccountApi.endpoints.revokeCredential.initiate({
        id: 'sa-1',
        credentialId: 'cred-1'
      })
    );

    expect(captured.url).toContain(
      '/v1/admin/service-accounts/sa-1/credentials/cred-1'
    );
  });
});
