import { describe, it, expect, vi, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { waitFor } from '@testing-library/react';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';
import { setupApi } from './setupApi';
import { authApi } from './authApi';

// baseQueryWithRetry shows toasts on some error paths; keep them silent and
// inert so a stray call doesn't blow up happy-dom.
vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const seededStore = (accessToken: string | null) =>
  setupStore({
    auth: {
      user: null,
      isAuthenticated: !!accessToken,
      isLoading: false,
      error: null,
      sessionExpiry: null,
      permissions: [],
      preferences: { theme: 'light', language: 'en', notifications: true },
      _isLoggingOut: false,
      accessToken,
      tokenExpiry: accessToken
        ? new Date(Date.now() + 60_000).toISOString()
        : null
    }
  });

const finalizeInput = {
  tenantName: 'Acme',
  tenantSlug: 'acme',
  allowAdditionalInternalTenants: false
};

const finalize200Body = {
  tenantId: 't1',
  tenantName: 'Acme',
  tenantSlug: 'acme',
  mode: 'manual' as const,
  allowAdditionalInternalTenants: true
};

afterEach(async () => {
  // Drain any pending microtask/macrotask queued by the background
  // onQueryStarted continuation before the next test's spies are restored.
  await new Promise(resolve => setTimeout(resolve, 0));
  vi.restoreAllMocks();
});

describe('setupApi.finalizeSetup — ordered session re-mint', () => {
  it('200: invalidation happens only after the forced getSession refetch resolves, and Auth is excluded', async () => {
    let sessionCalls = 0;
    let releaseSession: () => void = () => {};
    const sessionGate = new Promise<void>(resolve => {
      releaseSession = resolve;
    });

    server.use(
      http.post('*/v1/setup/finalize', () =>
        HttpResponse.json(finalize200Body)
      ),
      http.get('*/v1/auth/session', async () => {
        sessionCalls += 1;
        await sessionGate;
        return HttpResponse.json({
          accessToken: 'new-token',
          tokenType: 'Bearer',
          expiresIn: 900,
          user: { id: 'u1', email: 'a@b.com', role: 'administrator' },
          authenticated: true,
          success: true
        });
      })
    );

    const store = seededStore('stale-token');
    const invalidateSpy = vi.spyOn(baseApi.util, 'invalidateTags');
    const getSessionInitiateSpy = vi.spyOn(
      authApi.endpoints.getSession,
      'initiate'
    );

    const pending = store.dispatch(
      setupApi.endpoints.finalizeSetup.initiate(finalizeInput)
    );

    // Session request is in flight but gated — invalidation must not have
    // fired yet, and the stale token must still be in the store.
    await waitFor(() => expect(sessionCalls).toBe(1));
    await new Promise(resolve => setTimeout(resolve, 20));
    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(store.getState().auth.accessToken).toBe('stale-token');

    releaseSession();

    await waitFor(() => expect(invalidateSpy).toHaveBeenCalledTimes(1));
    expect(store.getState().auth.accessToken).toBe('new-token');
    expect(sessionCalls).toBe(1);
    expect(getSessionInitiateSpy).toHaveBeenCalledTimes(1);
    expect(getSessionInitiateSpy).toHaveBeenCalledWith(undefined, {
      forceRefetch: true
    });

    const invalidatedArg = invalidateSpy.mock.calls[0][0] as Array<
      string | { type: string }
    >;
    const flat = invalidatedArg.map(t => (typeof t === 'string' ? t : t.type));
    expect(flat).not.toContain('Auth');
    expect(flat).toEqual(
      expect.arrayContaining(['Setup', 'Membership', 'Org', 'Navigation'])
    );

    const result = await pending;
    expect(result.data).toMatchObject({ tenantId: 't1' });
  });

  it('202: no getSession dispatch and no invalidation — the payload is a success, not an error', async () => {
    server.use(
      http.post('*/v1/setup/finalize', () =>
        HttpResponse.json(
          { state: 'setup.finalization_in_progress' },
          { status: 202, headers: { 'Retry-After': '3' } }
        )
      )
    );

    const store = seededStore('stale-token');
    const invalidateSpy = vi.spyOn(baseApi.util, 'invalidateTags');
    const getSessionInitiateSpy = vi.spyOn(
      authApi.endpoints.getSession,
      'initiate'
    );

    const result = await store.dispatch(
      setupApi.endpoints.finalizeSetup.initiate(finalizeInput)
    );

    expect(result.error).toBeUndefined();
    expect(result.data).toEqual({ state: 'setup.finalization_in_progress' });

    // Give the (absent) background continuation a chance to run so a
    // regression that dispatches unconditionally would be caught.
    await new Promise(resolve => setTimeout(resolve, 20));

    expect(getSessionInitiateSpy).not.toHaveBeenCalled();
    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(store.getState().auth.accessToken).toBe('stale-token');
  });

  it('200 but session refresh resolves without a token: no invalidation, token unchanged, mutation still resolves', async () => {
    server.use(
      http.post('*/v1/setup/finalize', () =>
        HttpResponse.json(finalize200Body)
      ),
      http.get('*/v1/auth/session', () =>
        HttpResponse.json({ authenticated: false, success: true })
      )
    );

    const store = seededStore('stale-token');
    const invalidateSpy = vi.spyOn(baseApi.util, 'invalidateTags');
    const getSessionInitiateSpy = vi.spyOn(
      authApi.endpoints.getSession,
      'initiate'
    );

    const result = await store.dispatch(
      setupApi.endpoints.finalizeSetup.initiate(finalizeInput)
    );

    expect(result.error).toBeUndefined();
    expect(result.data).toMatchObject({ tenantId: 't1' });

    await waitFor(() => expect(getSessionInitiateSpy).toHaveBeenCalledTimes(1));
    await new Promise(resolve => setTimeout(resolve, 20));

    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(store.getState().auth.accessToken).toBe('stale-token');
  });
});

describe('setupApi.createInitialAdmin — Setup invalidation regression pin', () => {
  it('still invalidates Setup and pierces the 300s getSetupStatus cache', async () => {
    let statusCalls = 0;
    server.use(
      http.get('*/v1/setup/status', () => {
        statusCalls += 1;
        return HttpResponse.json({
          setupCompleted: false,
          phase: 'admin_required',
          smtpConfigured: false
        });
      }),
      http.post('*/v1/setup/admin', () =>
        HttpResponse.json({
          success: true,
          accessToken: 'admin-token',
          tokenType: 'Bearer',
          expiresIn: 900,
          user: { id: 'u1', email: 'a@b.com', role: 'administrator' }
        })
      )
    );

    const store = seededStore(null);
    // Keep a live subscription (never unsubscribed) so an invalidation
    // triggers an automatic background refetch rather than just marking the
    // entry stale for a subscriber that never arrives.
    store.dispatch(setupApi.endpoints.getSetupStatus.initiate());
    await waitFor(() => expect(statusCalls).toBe(1));

    await store.dispatch(
      setupApi.endpoints.createInitialAdmin.initiate({
        email: 'a@b.com',
        password: 'Sup3rSecret!1',
        fullName: 'Admin'
      })
    );

    await waitFor(() => expect(statusCalls).toBe(2));
  });
});
