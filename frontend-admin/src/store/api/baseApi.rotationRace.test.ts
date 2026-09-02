import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi, __setRefreshTimeoutForTests } from './baseApi';
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

// Every 401 the probed resource answers below carries
// `code: "access_token_expired"`. The reactive refresh only fires on proof
// the request never reached its handler (baseApi.ts's `handlerNeverRan`):
// that code, or a request that went out with no live bearer. These tests are
// about what performRefresh does once the branch has been ENTERED, so the
// fixture has to supply one of the two proofs — with the codeless `{}` body
// they used to send and the live seeded token, the branch now returns the
// 401 early and there is no rotation left to race. The other proof (seeding
// an already-expired token) would additionally trip the PROACTIVE rotation
// and rotate the seeded token before the 401 exists, which is a different
// test. /refresh-cookie's own 401 (the "refresh is rejected" case) keeps its
// bare body: that one is the refresh endpoint rejecting the cookie, not the
// resource rejecting a bearer.
const expired401 = () =>
  HttpResponse.json({ code: 'access_token_expired' }, { status: 401 });

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
  // already-resolved refresh and never hits the network. The refresh
  // timeout is restored here too, so a failed assertion in the test that
  // shortens it cannot leak a 25 ms bound into the tests after it.
  afterEach(async () => {
    __setRefreshTimeoutForTests();
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
          ? expired401()
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
      http.get('*/v1/some/resource', () => expired401()),
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
      http.get('*/v1/some/resource', () => expired401()),
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

  // The same allowlist on the PROOF path (this suite's 401s all carry
  // access_token_expired, so the reactive branch refreshes and replays).
  // /refresh-cookie sits under the router's global rate limiter, so a burst
  // of tabs crossing the TTL together is exactly what trips a 429 — and a
  // rate limit is the server declining to answer, not the session ending.
  // Before the classifier became an allowlist this signed the operator out.
  it('does not sign the user out when the refresh is rate-limited', async () => {
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', () => expired401()),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json({ detail: 'slow down' }, { status: 429 });
      })
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(refreshAttempts).toBe(1);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // This is the trap #317's follow-up brief calls out: refreshOnce's catch
  // used to return a bare `{ ok: false }`, and the 401 branch above treats
  // that as a REAL negative answer — clearAccessToken + navigateToLogin.
  // An aborted/timed-out fetch throws into that same catch, so a naive
  // timeout on its own would turn "the network is slow" into "you are
  // signed out". Prove the fix: a /refresh-cookie that never answers must
  // NOT clear the token, must NOT navigate to login, and must surface the
  // original 401 as a transient (`retry: true`) outcome — the same shape as
  // the 503/409 cases above, not the "still clears the token" case right
  // above this one. The timeout is driven through the test-only setter, not
  // by patching a platform object; see the proactive suite for why fake
  // timers are not an option here.
  it('does not sign the user out when /refresh-cookie never answers', async () => {
    __setRefreshTimeoutForTests(25);

    server.use(
      http.get('*/v1/some/resource', () => expired401()),
      http.post('*/refresh-cookie', async () => {
        // Never resolves — the connection accepts and never answers.
        await delay('infinite');
        return HttpResponse.json({ accessToken: 'never', expiresIn: 900 });
      })
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    // NOT signed out: the seeded token survives the timed-out refresh.
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });
});

describe('cross-tab refresh serialisation', () => {
  afterEach(async () => {
    Reflect.deleteProperty(navigator, 'locks');
    __setRefreshTimeoutForTests();
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  // A lock that rejects says nothing about the session. performRefresh
  // must still RESOLVE, and must not be the reason someone is signed out.
  it('does not sign the user out when the lock itself fails', async () => {
    Object.defineProperty(navigator, 'locks', {
      value: { request: () => Promise.reject(new Error('lock unavailable')) },
      configurable: true
    });

    server.use(http.get('*/v1/some/resource', () => expired401()));

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // The per-tab in-flight guard says nothing about other tabs, and every
  // tab's access token expires at the same instant. Web Locks is what makes
  // the rotation one-at-a-time for the whole origin.
  //
  // The ARITY is part of what this pins, not incidental. Web Locks offers a
  // three-argument overload — request(name, { signal }, callback) — and a
  // switch to it would bind `cb` below to the options object; `cb(null)`
  // would throw, performRefresh's own `.catch` would swallow the throw, and
  // a mock that only checked the call count and the lock name would stay
  // green while exercising nothing. Hence the explicit shape check in the
  // mock and the two assertions on the recorded call.
  it('takes the cross-tab lock when Web Locks is available', async () => {
    const request = vi.fn((_name: string, cb: unknown) => {
      if (typeof cb !== 'function') {
        throw new Error(
          'withRefreshLock switched to the 3-arg overload; update this mock'
        );
      }
      return Promise.resolve((cb as (lock: unknown) => unknown)(null));
    });
    Object.defineProperty(navigator, 'locks', {
      value: { request },
      configurable: true
    });

    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', () => expired401()),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json(
          { accessToken: 'locked-token', expiresIn: 900 },
          { status: 200 }
        );
      })
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(request).toHaveBeenCalledTimes(1);
    // The mock's own throw is swallowed by performRefresh's `.catch`, so
    // THIS is the assertion that actually fails when the call site changes
    // shape — the guard inside the mock only makes the reason legible.
    expect(request.mock.calls[0]).toHaveLength(2);
    expect(request.mock.calls[0][0]).toBe('orkestra:auth-refresh');
    expect(typeof request.mock.calls[0][1]).toBe('function');
    // And the run callback really ran inside the lock: a 3-arg call would
    // throw before reaching the network and leave this at 0.
    expect(refreshAttempts).toBe(1);
  });

  // `fetch` resolves on HEADERS. A timer cleared straight after that await
  // therefore bounds almost nothing: a server that sends headers and then
  // stalls the body holds the cross-tab lock — and with it every other tab's
  // rotation — for as long as it stalls. refreshOnce races the body read
  // against the abort for exactly this reason, which is also why clearTimeout
  // lives in the `finally` and nowhere else. A stalled read is "no answer",
  // so the outcome is transient and the token survives.
  it('releases the cross-tab lock when the headers arrive and the body stalls', async () => {
    __setRefreshTimeoutForTests(25);
    let lockReleased = false;
    const request = vi.fn(async (_name: string, cb: unknown) => {
      const held = await (cb as (lock: unknown) => Promise<unknown>)(null);
      lockReleased = true;
      return held;
    });
    Object.defineProperty(navigator, 'locks', {
      value: { request },
      configurable: true
    });

    server.use(
      http.get('*/v1/some/resource', () => expired401()),
      http.post('*/refresh-cookie', () => {
        // Headers land immediately; the body never arrives and the stream is
        // never closed. This is the shape AbortSignal.timeout on the fetch
        // alone could not express at all.
        const stalledBody = new ReadableStream({
          start() {
            /* never enqueue, never close */
          }
        });
        return new HttpResponse(stalledBody, {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        });
      })
    );

    const store = await setupSeededStore();
    await store.dispatch(
      probeApi.endpoints.raceProbe.initiate('/v1/some/resource')
    );

    expect(request).toHaveBeenCalledTimes(1);
    expect(lockReleased).toBe(true);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });
});
