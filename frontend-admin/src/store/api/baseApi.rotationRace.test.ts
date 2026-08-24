import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';
import { setupApi } from './setupApi';

vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const probeApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    raceProbe: builder.query<unknown, string>({
      query: url => ({ url, method: 'GET' })
    })
  }),
  overrideExisting: true
});

// Same shape as the session-ended suite: setup marked complete so the
// first-install gate cannot be what suppresses the refresh, and a non-null
// starting token so "was it cleared?" is a real question.
const setupSeededStore = async () => {
  const store = setupStore({
    auth: {
      user: null,
      isAuthenticated: true,
      isLoading: false,
      error: null,
      sessionExpiry: null,
      permissions: [],
      preferences: { theme: 'light', language: 'en', notifications: true },
      _isLoggingOut: false,
      accessToken: 'seed-access-token',
      tokenExpiry: new Date(Date.now() + 60_000).toISOString()
    }
  });
  await store.dispatch(
    setupApi.util.upsertQueryData('getSetupStatus', undefined, {
      setupCompleted: true,
      phase: 'complete',
      smtpConfigured: true
    })
  );
  return store;
};

describe('refresh rotation race', () => {
  beforeEach(() => {
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  // performRefresh clears its module-level in-flight promise on a
  // macrotask. Without draining it here the next test reuses THIS test's
  // already-resolved refresh and never hits the network.
  afterEach(async () => {
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  // The backend answers a lost rotation with 409 refresh_rotation_raced:
  // a sibling tab rotated first, the family is intact, and the successor
  // cookie is already in the jar. One retry must land — treating this as a
  // sign-out is exactly the bug that logged operators out of every tab.
  it('retries once on 409 and keeps the session', async () => {
    let refreshAttempts = 0;
    let resourceAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', () => {
        resourceAttempts += 1;
        return resourceAttempts === 1
          ? HttpResponse.json({}, { status: 401 })
          : HttpResponse.json({ ok: true }, { status: 200 });
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return refreshAttempts === 1
          ? HttpResponse.json(
              { code: 'refresh_rotation_raced' },
              { status: 409 }
            )
          : HttpResponse.json(
              { accessToken: 'rotated-token', expiresIn: 900 },
              { status: 200 }
            );
      })
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(refreshAttempts).toBe(2);
    expect(store.getState().auth.accessToken).toBe('rotated-token');
  });

  // A race that survives both attempts is ambiguous, not terminal. Guessing
  // "signed out" is the failure mode this change exists to remove, so the
  // token must survive and the next request gets to try again.
  it('does not sign the user out when the race persists', async () => {
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', () =>
        HttpResponse.json({}, { status: 401 })
      ),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json(
          { code: 'refresh_rotation_raced' },
          { status: 409 }
        );
      })
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(refreshAttempts).toBe(2);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // A 401 that is not a race must still end the session — the retry path
  // must not become a way to ignore a genuinely dead refresh token.
  it('still clears the token when the refresh is rejected', async () => {
    server.use(
      http.get('*/v1/some/resource', () =>
        HttpResponse.json({}, { status: 401 })
      ),
      http.post('*/refresh-cookie', () =>
        HttpResponse.json({}, { status: 401 })
      )
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(store.getState().auth.accessToken).toBeFalsy();
  });
});

describe('cross-tab refresh serialisation', () => {
  afterEach(async () => {
    Reflect.deleteProperty(navigator, 'locks');
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  // A lock that rejects says nothing about the session. performRefresh
  // must still RESOLVE, and must not be the reason someone is signed out.
  it('does not sign the user out when the lock itself fails', async () => {
    Object.defineProperty(navigator, 'locks', {
      value: { request: () => Promise.reject(new Error('lock unavailable')) },
      configurable: true
    });

    server.use(
      http.get('*/v1/some/resource', () =>
        HttpResponse.json({}, { status: 401 })
      )
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // The per-tab in-flight guard says nothing about other tabs, and every
  // tab's access token expires at the same instant. Web Locks is what makes
  // the rotation one-at-a-time for the whole origin.
  it('takes the cross-tab lock when Web Locks is available', async () => {
    const request = vi.fn((_name: string, cb: (lock: unknown) => unknown) =>
      Promise.resolve(cb(null))
    );
    Object.defineProperty(navigator, 'locks', {
      value: { request },
      configurable: true
    });

    server.use(
      http.get('*/v1/some/resource', () =>
        HttpResponse.json({}, { status: 401 })
      ),
      http.post('*/refresh-cookie', () =>
        HttpResponse.json(
          { accessToken: 'locked-token', expiresIn: 900 },
          { status: 200 }
        )
      )
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0][0]).toBe('orkestra:auth-refresh');
  });
});
