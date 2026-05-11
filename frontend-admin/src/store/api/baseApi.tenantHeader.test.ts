import { describe, it, expect, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';

// Test-only endpoint that hits any URL string. Uses baseQuery, so the
// X-Tenant-ID injection logic in baseApi.ts runs for every call.
const probeApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    tenantHeaderProbe: builder.query<{ ok: true }, string>({
      query: (url) => ({ url, method: 'GET' }),
    }),
  }),
  overrideExisting: true,
});

const captureHeaders = () => {
  const captured: { url: string | null; tenant: string | null } = {
    url: null,
    tenant: null,
  };
  server.use(
    http.all('*', ({ request }) => {
      captured.url = request.url;
      captured.tenant = request.headers.get('X-Tenant-ID');
      return HttpResponse.json({ ok: true });
    }),
  );
  return captured;
};

const fire = async (
  preloadedTenant: Partial<{
    currentOrgId: string | null;
    impersonatedTenantId: string | null;
    impersonatedTenantName: string | null;
  }>,
  url: string,
) => {
  const store = setupStore({
    tenant: {
      memberships: [],
      currentOrgId: null,
      permissions: [],
      features: [],
      systemRole: '',
      loading: false,
      error: null,
      impersonatedTenantId: null,
      impersonatedTenantName: null,
      ...preloadedTenant,
    },
  } as never);
  await store.dispatch(probeApi.endpoints.tenantHeaderProbe.initiate(url));
};

describe('baseApi X-Tenant-ID injection', () => {
  beforeEach(() => {
    // Hard reset RTK Query cache so each test fires a fresh request
    // instead of returning a cached response from a previous test.
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  it('stamps currentOrgId on tenant-scoped requests', async () => {
    const captured = captureHeaders();
    await fire({ currentOrgId: 'real-org-uuid' }, '/v1/billing/stats');
    expect(captured.tenant).toBe('real-org-uuid');
  });

  it('lets impersonatedTenantId override currentOrgId — security-critical', async () => {
    // Bug class: an admin starts impersonation but the header still carries
    // their real currentOrgId, so writes hit the wrong tenant. The backend
    // gates the header for non-admins, but the SPA must send the right one.
    const captured = captureHeaders();
    await fire(
      {
        currentOrgId: 'admin-real-org',
        impersonatedTenantId: 'target-tenant',
        impersonatedTenantName: 'Acme Corp',
      },
      '/v1/billing/stats',
    );
    expect(captured.tenant).toBe('target-tenant');
  });

  it('omits the header when neither currentOrgId nor impersonation is set', async () => {
    const captured = captureHeaders();
    await fire({}, '/v1/billing/stats');
    expect(captured.tenant).toBeNull();
  });

  // Tenant-agnostic endpoints: sending X-Tenant-ID would either be wrong
  // (auth/setup happen before tenant resolution) or get rejected by the
  // backend (platform-level admin endpoints). The SPA must NOT stamp it.
  it.each([
    '/v1/auth/operator/login',
    '/v1/auth/operator/refresh-cookie',
    '/v1/tenants',
    '/v1/tenants/accept-invite',
    '/v1/admin/modules',
    '/v1/admin/tenants',
    '/v1/admin/audit-events',
    '/v1/admin/compliance/soc2/evidence',
    '/v1/me/dsr/exports',
    '/v1/setup/status',
    '/v1/notifications/preferences',
  ])('suppresses the header for tenant-agnostic path %s', async (path) => {
    const captured = captureHeaders();
    await fire({ currentOrgId: 'real-org-uuid' }, path);
    expect(captured.tenant, `expected no X-Tenant-ID for ${path}`).toBeNull();
  });

  // /v1/tenants is exact-match agnostic (listing/creation), but per-tenant
  // sub-paths (/v1/tenants/{id}/...) must still carry the header.
  it('still stamps header on /v1/tenants/{id}/sub-paths', async () => {
    const captured = captureHeaders();
    await fire({ currentOrgId: 'real-org-uuid' }, '/v1/tenants/abc/authz/me');
    expect(captured.tenant).toBe('real-org-uuid');
  });
});
