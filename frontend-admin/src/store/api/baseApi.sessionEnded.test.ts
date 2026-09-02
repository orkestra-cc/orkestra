import { describe, it, expect, beforeEach, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';
import { setupApi } from './setupApi';

// The toast is the only observable difference between "suppressed" and
// "shown", so it has to be spied rather than inferred.
const toastError = vi.hoisted(() => vi.fn());
vi.mock('react-toastify', () => ({
  toast: { error: toastError, success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

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
      phase: 'complete',
      smtpConfigured: true
    })
  );
  return store;
};

describe('baseApi session-ended interception', () => {
  beforeEach(() => {
    toastError.mockClear();
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

describe('session_max_age_reached reaches the user', () => {
  beforeEach(() => {
    toastError.mockClear();
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  // `isAuthCheck` exists to keep an anonymous visitor from being told their
  // session expired on a cold load of /me or /v1/auth/session. A server-side
  // TERMINATION is a different event: session_max_age_reached is only ever
  // emitted for a session that existed and was ended by policy, and
  // /v1/auth/session is one of the two endpoints that emit it — so the
  // suppression made the message unreachable on that path.
  //
  // ADR-0017 D4 gives the cap its own wording because "'revoked' is
  // inaccurate for a session that simply reached its maximum age, and the
  // distinction matters to whoever reads the support ticket". A message
  // nobody sees cannot carry that distinction.
  it('shows the toast on an auth-check URL, which normally suppresses it', async () => {
    server.use(
      http.get('*/v1/auth/session', () =>
        HttpResponse.json({ code: 'session_max_age_reached' }, { status: 401 })
      )
    );
    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.sessionEndedProbe.initiate('/v1/auth/session')
    );

    expect(toastError).toHaveBeenCalledTimes(1);
    expect(String(toastError.mock.calls[0][0])).toMatch(/maximum age/i);
  });

  // The counter-test that keeps the exemption narrow: the generic revoked
  // code on the SAME auth-check URL must stay suppressed, or the fix is just
  // "always toast" wearing a specific name.
  it('still suppresses session_revoked on an auth-check URL', async () => {
    server.use(
      http.get('*/v1/auth/session', () =>
        HttpResponse.json({ code: 'session_revoked' }, { status: 401 })
      )
    );
    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.sessionEndedProbe.initiate('/v1/auth/session')
    );

    expect(toastError).not.toHaveBeenCalled();
  });
});

// Both tests below make the resource answer `code: "access_token_expired"`.
// The reactive refresh only fires on proof the request never reached its
// handler (baseApi.ts's `handlerNeverRan`): that code, or a request sent
// with no live bearer. Their previous `{ detail: 'expired' }` body carries
// neither, so against the live seeded token the 401 branch would now return
// early and no refresh would be attempted at all — one test would fail
// outright and the other would pass without exercising anything. The code is
// the honest fixture here: these are 401s on a protected resource whose
// bearer the server rejected as expired, which is exactly what §4.10 emits.
describe('503 from the refresh endpoint is not a sign-out', () => {
  beforeEach(async () => {
    toastError.mockClear();
    setupStore().dispatch(baseApi.util.resetApiState());
    // performRefresh coalesces concurrent 401s onto a module-scoped
    // in-flight promise and clears it on a macrotask tick, so two refresh
    // tests in a row would otherwise share the FIRST one's result and the
    // second test would assert nothing. Yield a macrotask to drain it.
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  // ADR-0017 gives session enforcement its own 503 precisely so a client
  // does not discard a live session when the server merely could not
  // evaluate it: an outage "would train clients to discard a session that is
  // still perfectly valid." performRefresh collapsed every !res.ok into
  // ok:false, so the 503 logged the user out for the exact reason it exists
  // to prevent.
  it('keeps the access token when the refresh endpoint answers 503', async () => {
    server.use(
      http.get('*/v1/some/resource', () =>
        HttpResponse.json({ code: 'access_token_expired' }, { status: 401 })
      ),
      http.post('*/v1/auth/operator/refresh-cookie', () =>
        HttpResponse.json(
          { code: 'session_enforcement_unavailable' },
          { status: 503 }
        )
      )
    );
    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.sessionEndedProbe.initiate('/v1/some/resource')
    );

    expect(store.getState().auth.accessToken).toBe('seed-access-token');
    expect(toastError).not.toHaveBeenCalled();
  });

  // Counter-test: a 401 from the refresh endpoint IS a sign-out, so the
  // branch above must key on the status and not disable the logout path.
  it('still clears the access token when the refresh endpoint answers 401', async () => {
    server.use(
      http.get('*/v1/some/resource', () =>
        HttpResponse.json({ code: 'access_token_expired' }, { status: 401 })
      ),
      http.post('*/v1/auth/operator/refresh-cookie', () =>
        HttpResponse.json({ detail: 'no cookie' }, { status: 401 })
      )
    );
    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.sessionEndedProbe.initiate('/v1/some/resource')
    );

    expect(store.getState().auth.accessToken).toBeFalsy();
  });
});
