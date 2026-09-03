import { describe, it, expect, beforeEach } from 'vitest';
import { act, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { setUserFromApiResponse, setAccessToken } from 'store/slices/authSlice';
import { setCurrentOrg, stopImpersonation } from 'store/slices/tenantSlice';
import { baseApi, getTenantScopedTags } from 'store/api/baseApi';
import { useTenantBootstrap } from './useTenantBootstrap';

const STORAGE_KEY = 'orkestra.currentOrgId';
const IMPERSONATION_KEY = 'orkestra.impersonatedTenant';

// Side-effect-only hook — mount it through a probe component (same
// pattern as useLanguageSync.test.tsx).
const Probe = () => {
  useTenantBootstrap();
  return null;
};

// Auth state as it looks on a fresh page load: the session check is in
// flight, so we are not authenticated *yet* — but not logged out either.
const loadingAuth = {
  user: null,
  isAuthenticated: false,
  isLoading: true,
  error: null,
  sessionExpiry: null,
  permissions: [],
  preferences: {
    theme: 'light' as const,
    language: 'en',
    notifications: true
  },
  _isLoggingOut: false,
  accessToken: null,
  tokenExpiry: null
};

const user = {
  id: 'u1',
  email: 'a@b.test',
  username: 'a',
  fullName: 'A',
  role: 'administrator',
  isActive: true,
  emailVerified: true,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

// Two memberships: the fallback pick (first owned org) is org-a, so a
// restore that actually honours the stored selection must land on org-b.
const memberships = [
  {
    tenantId: 'org-a',
    name: 'Default',
    slug: 'default',
    plan: 'free',
    kind: 'internal',
    roles: ['org_owner'],
    isOwner: true
  },
  {
    tenantId: 'org-b',
    name: 'Second',
    slug: 'second',
    plan: 'free',
    kind: 'internal',
    roles: ['org_owner'],
    isOwner: false
  }
];

const stubTenantEndpoints = () => {
  server.use(
    http.get('*/v1/tenants', () => HttpResponse.json({ memberships })),
    http.get('*/v1/tenants/:id/authz/me', ({ params }) =>
      // Deliberately constant payload: a refetch must produce a response
      // deep-equal to the cached one so structural sharing kicks in.
      HttpResponse.json({
        tenantId: params.id,
        permissions: ['*'],
        systemRole: 'super_admin'
      })
    ),
    http.get('*/v1/tenants/:id', ({ params }) =>
      HttpResponse.json({ id: params.id, features: [] })
    )
  );
};

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

describe('useTenantBootstrap', () => {
  it('keeps the stored workspace while the session check is still in flight', () => {
    window.localStorage.setItem(STORAGE_KEY, 'org-b');
    window.sessionStorage.setItem(
      IMPERSONATION_KEY,
      JSON.stringify({ tenantId: 't-x', tenantName: 'X' })
    );

    renderWithProviders(<Probe />, { preloadedState: { auth: loadingAuth } });

    // isAuthenticated=false + isLoading=true means "not known yet", not
    // "logged out" — the stored selection must survive the boot window.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('org-b');
    expect(window.sessionStorage.getItem(IMPERSONATION_KEY)).not.toBeNull();
  });

  it('clears tenant state once the auth check concludes unauthenticated', () => {
    window.localStorage.setItem(STORAGE_KEY, 'org-b');

    renderWithProviders(<Probe />, {
      preloadedState: { auth: { ...loadingAuth, isLoading: false } }
    });

    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it('restores the stored workspace after the session check completes', async () => {
    stubTenantEndpoints();
    window.localStorage.setItem(STORAGE_KEY, 'org-b');

    // Mount during the in-flight window (the real refresh sequence), then
    // let the session check conclude authenticated.
    const { store } = renderWithProviders(<Probe />, {
      preloadedState: { auth: loadingAuth }
    });
    act(() => {
      store.dispatch(setUserFromApiResponse(user));
      store.dispatch(setAccessToken({ accessToken: 'tok', expiresIn: 900 }));
    });

    await waitFor(() =>
      expect(store.getState().tenant.currentOrgId).toBe('org-b')
    );
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('org-b');
  });

  // Both bootstrap queries take the org id in the PATH, and the backend's
  // assertTenantScope (tenant/handlers/handler.go, authz/handlers/handler.go)
  // 404s unless that path id equals the tenant the auth middleware resolved
  // for the request. The middleware resolves it from X-Tenant-ID, and
  // baseApi only stamps that header from a membership-VALIDATED
  // `currentOrgId` — deliberately null until GET /v1/tenants lands.
  //
  // Firing these two off an optimistic localStorage hint therefore sends a
  // path id the header cannot yet vouch for: the backend falls back to the
  // token's acting tenant (the platform default), that differs from the
  // operator's selected workspace, and both requests 404. Nothing retries
  // them — the RTK Query cache key is the org id, which does not change when
  // currentOrgId later settles on the same value, and the header is not part
  // of the key — so tenant.permissions and tenant.features stay empty for the
  // whole session. On this fork that silently hid the assistant launcher and
  // the workspace switcher from every non-super_admin operator (super_admins
  // are masked by the client-side '*' merge in useAuthRTK).
  it('never sends a bootstrap request the tenant header cannot vouch for', async () => {
    const scopedCalls: Array<{ path: string; tenant: string | null }> = [];
    server.use(
      http.get('*/v1/tenants', () => HttpResponse.json({ memberships })),
      http.get('*/v1/tenants/:id/authz/me', ({ request, params }) => {
        scopedCalls.push({
          path: `authz/me:${params.id}`,
          tenant: request.headers.get('X-Tenant-ID')
        });
        return HttpResponse.json({
          tenantId: params.id,
          permissions: ['*'],
          systemRole: 'super_admin'
        });
      }),
      http.get('*/v1/tenants/:id', ({ request, params }) => {
        scopedCalls.push({
          path: `org:${params.id}`,
          tenant: request.headers.get('X-Tenant-ID')
        });
        return HttpResponse.json({ id: params.id, features: [] });
      })
    );
    window.localStorage.setItem(STORAGE_KEY, 'org-b');

    const { store } = renderWithProviders(<Probe />, {
      preloadedState: {
        auth: {
          ...loadingAuth,
          user,
          isAuthenticated: true,
          isLoading: false,
          accessToken: 'tok'
        }
      }
    });

    await waitFor(() =>
      expect(store.getState().tenant.permissions).toEqual(['*'])
    );
    await waitFor(() => expect(scopedCalls.length).toBeGreaterThanOrEqual(2));

    // Every scoped call carried the header, and it named the same org as the
    // path — the only combination assertTenantScope answers with a 200.
    expect(scopedCalls.filter(c => c.tenant === null)).toEqual([]);
    expect(scopedCalls.every(c => c.path.endsWith(c.tenant as string))).toBe(
      true
    );
  });

  it('restores permissions after re-picking the current workspace', async () => {
    // The NineDotMenu pick sequence: stopImpersonation + setCurrentOrg +
    // invalidateTags. setCurrentOrg clears org-scoped permissions; the
    // invalidated authz/me refetch returns a payload deep-equal to the
    // cached one, so RTK Query's structural sharing keeps the same object
    // reference — the mirror effect must still re-dispatch it, otherwise
    // permissions stay empty forever and every hasPermission gate (e.g.
    // the NineDotMenu itself for single-membership admins) goes dark
    // until a full page reload.
    stubTenantEndpoints();
    window.localStorage.setItem(STORAGE_KEY, 'org-a');

    const { store } = renderWithProviders(<Probe />, {
      preloadedState: {
        auth: {
          ...loadingAuth,
          user,
          isAuthenticated: true,
          isLoading: false,
          accessToken: 'tok'
        }
      }
    });

    await waitFor(() =>
      expect(store.getState().tenant.permissions).toEqual(['*'])
    );

    act(() => {
      store.dispatch(stopImpersonation());
      store.dispatch(setCurrentOrg('org-a'));
      store.dispatch(baseApi.util.invalidateTags(getTenantScopedTags()));
    });

    await waitFor(() =>
      expect(store.getState().tenant.permissions).toEqual(['*'])
    );
  });
});
