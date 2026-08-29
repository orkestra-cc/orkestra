import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi, PROACTIVE_REFRESH_SKEW_MS } from './baseApi';
import { setupApi } from './setupApi';

vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const probeApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    proactiveProbe: builder.query<unknown, string>({
      query: url => ({ url, method: 'GET' })
    })
  }),
  overrideExisting: true
});

// Same seed shape as the rotation-race suite: setup complete so the
// first-install gate is not what suppresses anything, and a real token
// whose expiry each test places relative to the skew window.
const seededStore = async (tokenExpiry: string) => {
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
      tokenExpiry
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

const inMs = (ms: number) => new Date(Date.now() + ms).toISOString();

// Records every Authorization header the probed endpoints saw, and how
// many times /refresh-cookie was hit. `resourceStatus` lets a test decide
// what the resource answers (200 by default); `expiresIn` is what the
// refresh answers with (900 by default — the 15-minute production TTL).
const arm = (resourceStatus = 200, expiresIn = 900) => {
  const seenAuth: Array<string | null> = [];
  const counters = { refresh: 0, resource: 0 };
  const record = (request: Request) => {
    counters.resource += 1;
    seenAuth.push(request.headers.get('authorization'));
  };
  server.use(
    http.get('*/v1/burst/:n', ({ request }) => {
      record(request);
      return HttpResponse.json({ ok: true }, { status: resourceStatus });
    }),
    http.get('*/v1/auth/session', ({ request }) => {
      record(request);
      return HttpResponse.json({ authenticated: false }, { status: 200 });
    }),
    // The probe issues GETs; msw's `all` lets the POST-only logout route
    // answer them so the test can assert "no pre-refresh" on it too.
    http.all('*/v1/auth/operator/logout', ({ request }) => {
      record(request);
      return HttpResponse.json({ ok: true }, { status: 200 });
    }),
    http.post('*/refresh-cookie', () => {
      counters.refresh += 1;
      return HttpResponse.json(
        { accessToken: 'fresh-token', expiresIn },
        { status: 200 }
      );
    })
  );
  return { seenAuth, counters };
};

describe('proactive refresh', () => {
  beforeEach(() => {
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  // performRefresh clears its in-flight promise on a macrotask; drain it
  // so the next test cannot reuse this test's already-resolved refresh.
  afterEach(async () => {
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  it('rotates first when the token expires inside the skew, and sends the fresh bearer', async () => {
    const { seenAuth, counters } = arm();
    const store = await seededStore(inMs(PROACTIVE_REFRESH_SKEW_MS / 2));

    const result = await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/burst/1')
    );

    expect(result.error).toBeUndefined();
    expect(counters.refresh).toBe(1);
    expect(seenAuth).toEqual(['Bearer fresh-token']);
    expect(store.getState().auth.accessToken).toBe('fresh-token');
  });

  it('coalesces a parallel burst on an already-expired token into one rotation and zero 401s', async () => {
    const { seenAuth, counters } = arm();
    const store = await seededStore(inMs(-1_000));

    const results = await Promise.all(
      [1, 2, 3, 4, 5].map(n =>
        store.dispatch(
          probeApi.endpoints.proactiveProbe.initiate(`/v1/burst/${n}`)
        )
      )
    );

    expect(results.every(r => r.error === undefined)).toBe(true);
    expect(counters.refresh).toBe(1);
    expect(counters.resource).toBe(5);
    expect(seenAuth).toEqual(Array(5).fill('Bearer fresh-token'));
  });

  it('leaves a comfortably valid token alone', async () => {
    const { seenAuth, counters } = arm();
    const store = await seededStore(inMs(10 * 60_000));

    await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/burst/1')
    );

    expect(counters.refresh).toBe(0);
    expect(seenAuth).toEqual(['Bearer seed-access-token']);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // Only the endpoints baseApi already excludes from the 401 retry are
  // exercised here (the session bootstrap mints on its own; the AUTH_ENDPOINT_PATHS
  // entries would recurse). This is not a claim about every /v1/auth/* route.
  it('never pre-refreshes the excluded auth endpoints (session bootstrap, logout)', async () => {
    const { counters } = arm();
    const store = await seededStore(inMs(PROACTIVE_REFRESH_SKEW_MS / 2));

    await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/auth/session')
    );
    await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/auth/operator/logout')
    );

    expect(counters.refresh).toBe(0);
    expect(counters.resource).toBe(2);
  });

  // PROACTIVE_REFRESH_SKEW_MS must stay strictly below the backend's
  // MinAccessTokenTTL (60 s, services/auth_duration_bounds.go). At or above
  // it, a token minted at the floor is already inside the window on arrival
  // and every request rotates again — a refresh loop. Pin both sides: the
  // constant itself, and the behaviour with a floor-length token.
  it('does not loop on a token minted at the backend minimum TTL (60s)', async () => {
    expect(PROACTIVE_REFRESH_SKEW_MS).toBeLessThan(60_000);
    const { seenAuth, counters } = arm(200, 60);
    const store = await seededStore(inMs(-1_000));

    await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/burst/1')
    );
    // performRefresh clears its in-flight promise on a macrotask
    // (baseApi.ts, the setTimeout(0) in performRefresh's finally). Without
    // draining it, a second request that DID decide to refresh would get
    // the already-resolved promise back, the counter would stay at 1, and
    // this test would pass even with a looping skew. Drain first so the
    // second request's "no refresh" is a real decision, not a cache hit.
    await new Promise(resolve => setTimeout(resolve, 0));
    await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/burst/2')
    );

    expect(counters.refresh).toBe(1);
    expect(seenAuth).toEqual(['Bearer fresh-token', 'Bearer fresh-token']);
  });

  it('sends the request anyway when the refresh is unavailable (503) and keeps the token', async () => {
    const { seenAuth, counters } = arm();
    server.use(
      http.post('*/refresh-cookie', () => {
        counters.refresh += 1;
        return HttpResponse.json({}, { status: 503 });
      })
    );
    const store = await seededStore(inMs(PROACTIVE_REFRESH_SKEW_MS / 2));

    const result = await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/burst/1')
    );

    expect(result.error).toBeUndefined();
    expect(counters.refresh).toBe(1);
    // Still valid for 30s — the original bearer goes out.
    expect(seenAuth).toEqual(['Bearer seed-access-token']);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // refreshOnce bounds its fetch with AbortSignal.timeout(REFRESH_FETCH_TIMEOUT_MS)
  // (baseApi.ts). Real timers only: AbortSignal.timeout schedules its own
  // internal timer, not one vi.useFakeTimers() can see (verified against
  // this Node/vitest pin — a fake-timer version of this test silently never
  // fires). The AbortSignal.timeout spy below keeps the real wait short —
  // it does not change the 10s constant, it only makes THIS timer fire fast
  // — so the assertions below exercise the real timeout path in bounded
  // wall-clock time.
  it('sends the request anyway when /refresh-cookie never answers, and keeps the token', async () => {
    const realTimeout = AbortSignal.timeout.bind(AbortSignal);
    const timeoutSpy = vi
      .spyOn(AbortSignal, 'timeout')
      .mockImplementation(() => realTimeout(25));

    const { seenAuth, counters } = arm();
    server.use(
      http.post('*/refresh-cookie', async () => {
        counters.refresh += 1;
        // Never resolves — the connection accepts and never answers, the
        // exact scenario REFRESH_FETCH_TIMEOUT_MS exists to bound.
        await delay('infinite');
        return HttpResponse.json({ accessToken: 'never', expiresIn: 900 });
      })
    );
    const store = await seededStore(inMs(PROACTIVE_REFRESH_SKEW_MS / 2));

    const result = await store.dispatch(
      probeApi.endpoints.proactiveProbe.initiate('/v1/burst/1')
    );

    // The request completes — it is not left hanging behind the refresh.
    expect(result.error).toBeUndefined();
    expect(counters.refresh).toBe(1);
    // Still valid for 30s — falls back to the original bearer, same as 503.
    expect(seenAuth).toEqual(['Bearer seed-access-token']);
    // Not signed out: the token that was in the store before the timed-out
    // refresh is still there afterwards.
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
    expect(store.getState().auth.isAuthenticated).toBe(true);

    timeoutSpy.mockRestore();
  });
});
