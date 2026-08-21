import { describe, it, expect, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';
import { setupApi } from './setupApi';

const probeApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    sessionEndedProbe: builder.query<unknown, string>({
      query: url => ({ url, method: 'GET' })
    })
  }),
  overrideExisting: true
});

const respondWith = (code: string) => {
  let refreshAttempts = 0;
  server.use(
    http.get('*/v1/some/resource', () =>
      HttpResponse.json({ code }, { status: 401 })
    ),
    // If the silent-refresh retry fires, this counts it. It must not.
    http.post('*/refresh*', () => {
      refreshAttempts += 1;
      return HttpResponse.json({}, { status: 200 });
    })
  );
  return () => refreshAttempts;
};

// Builds a store where neither assertion below is trivially true:
//   - setup is marked complete, so the pre-existing "first-install mode"
//     gate in baseApi.ts (isOnSetupPath || !setupCompleted) cannot be
//     what's suppressing the silent refresh. Without this, a fresh store
//     has no getSetupStatus cache entry, setupCompleted reads as false,
//     and that gate alone returns early for ANY 401 — masking whether the
//     session-ended branch ran at all.
//   - accessToken starts non-null, so asserting it's falsy afterwards
//     actually proves clearAccessToken() dispatched rather than the token
//     having simply never been set.
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
      smtpConfigured: true
    })
  );
  return store;
};

describe('baseApi session-ended interception', () => {
  beforeEach(() => {
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  it.each(['session_revoked', 'session_max_age_reached'])(
    'clears state and skips the silent refresh on %s',
    async code => {
      const attempts = respondWith(code);
      const store = await setupSeededStore();
      await store.dispatch(
        probeApi.endpoints.sessionEndedProbe.initiate('/v1/some/resource')
      );
      expect(attempts()).toBe(0);
      expect(store.getState().auth.accessToken).toBeFalsy();
    }
  );
});
