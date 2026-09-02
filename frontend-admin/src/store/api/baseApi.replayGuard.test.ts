import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { clearAccessToken } from 'store/slices/authSlice';
import { baseApi } from './baseApi';
import { setupApi } from './setupApi';

// The reactive 401 branch used to be gated on two path tests and nothing
// else, so EVERY 401 on a non-auth endpoint was refreshed and the original
// request re-sent — whatever the 401 was about. Four console routes answer
// 401 as a verdict on the REQUEST (change-password, /me/password-confirm,
// mfa/verify, mfa/enroll/confirm) and none of them is in
// AUTH_ENDPOINT_PATHS, so each replay cost a second wrong-password attempt,
// a burnt TOTP replay guard, or a consumed MFA attempt.
//
// The gate is the disjunction the client tier uses: refresh and replay only
// on proof the request never reached its handler —
//   (a) the 401 body carries code: "access_token_expired" (RequireAuth is
//       bearer-only, ADR-0020, and rejects an expired bearer BEFORE
//       dispatch), or
//   (b) the request went out with no live bearer at all, by the same
//       predicate prepareHeaders uses to withhold it (RequireAuth rejects
//       that before dispatch too).
// Neither proof: the 401 goes back to the caller untouched.
vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const probeApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    replayGuardProbe: builder.query<unknown, string>({
      query: url => ({ url, method: 'GET' })
    }),
    // A POST, because the replay hazard belongs to mutations: this is the
    // shape of authApi's changePassword call.
    replayGuardPost: builder.mutation<unknown, string>({
      query: url => ({
        url,
        method: 'POST',
        body: { currentPassword: 'wrong-one', newPassword: 'irrelevant' }
      })
    })
  }),
  overrideExisting: true
});

const inMs = (ms: number) => new Date(Date.now() + ms).toISOString();

// Same seed shape as the rotation-race and proactive suites: setup marked
// complete so the first-install gate is not what suppresses a refresh, a
// real token so "was it cleared?" is a question, and the expiry left to the
// caller because it IS proof (b).
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

describe('reactive refresh replay guard', () => {
  beforeEach(() => {
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  // performRefresh clears its module-level in-flight promise on a macrotask;
  // drain it so the next test cannot reuse this test's resolved refresh.
  afterEach(async () => {
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  // THE defect. change-password answers 401 when the CURRENT PASSWORD in the
  // body is wrong (mapPasswordError → huma.Error401Unauthorized, no
  // top-level code), and it is not in AUTH_ENDPOINT_PATHS. Refreshing and
  // re-sending it submits the same wrong password a second time: a second
  // argon2id verify, a second audit-relevant failure, and on its sibling
  // /me/password-confirm a second recordFailed against the lockout budget.
  it('does not refresh or replay a wrong-current-password change-password 401', async () => {
    let changeAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.post('*/v1/auth/operator/change-password', () => {
        changeAttempts += 1;
        // The real body: huma's ErrorModel, no top-level `code`.
        return HttpResponse.json(
          {
            title: 'Unauthorized',
            status: 401,
            detail: 'Invalid email or password'
          },
          { status: 401 }
        );
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json(
          { accessToken: 'fresh-token', expiresIn: 900 },
          { status: 200 }
        );
      })
    );

    const store = await seededStore(inMs(60_000));
    await expect(
      store
        .dispatch(
          probeApi.endpoints.replayGuardPost.initiate(
            '/v1/auth/operator/change-password'
          )
        )
        .unwrap()
    ).rejects.toMatchObject({ status: 401 });

    // Sent once, and the caller's 401 is the server's own answer.
    expect(changeAttempts).toBe(1);
    expect(refreshAttempts).toBe(0);
    // A wrong password is not a dead session: the token survives.
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // Proof (a): the server states it rejected an expired bearer before
  // dispatch. This is the strongest proof available and the only one that
  // covers a token which was live when it left and expired in flight.
  it('refreshes and replays once on a 401 carrying access_token_expired', async () => {
    const seenAuth: Array<string | null> = [];
    let resourceAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', ({ request }) => {
        resourceAttempts += 1;
        seenAuth.push(request.headers.get('authorization'));
        return resourceAttempts === 1
          ? HttpResponse.json({ code: 'access_token_expired' }, { status: 401 })
          : HttpResponse.json({ ok: true }, { status: 200 });
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json(
          { accessToken: 'fresh-token', expiresIn: 900 },
          { status: 200 }
        );
      })
    );

    const store = await seededStore(inMs(60_000));
    const result = await store.dispatch(
      probeApi.endpoints.replayGuardProbe.initiate('/v1/some/resource')
    );

    expect(refreshAttempts).toBe(1);
    expect(resourceAttempts).toBe(2);
    expect(result.data).toEqual({ ok: true });
    expect(seenAuth).toEqual([
      'Bearer seed-access-token',
      'Bearer fresh-token'
    ]);
    expect(store.getState().auth.accessToken).toBe('fresh-token');
  });

  // Proof (b): the recovery ADR-0020 D3 assigns to the reactive path — the
  // proactive rotation itself failed. The seeded token is already expired,
  // so the pre-flight rotation fires first; it gets a 503 (transient, keep
  // the token), prepareHeaders then withholds the dead bearer, and the
  // request arrives with no Authorization at all. RequireAuth's
  // missing-token branch answers a CODELESS 401 — which is why a strict
  // code-only gate would switch this recovery off. The refresh that the 401
  // branch runs finds the outage cleared and the replay carries the new
  // bearer.
  //
  // Note the proactive attempt MUST fail for the reactive path to be
  // reachable here at all: had it succeeded, a live bearer would have gone
  // out and proof (b) would not hold.
  it('refreshes and replays a codeless 401 when the bearer was withheld', async () => {
    const seenAuth: Array<string | null> = [];
    let resourceAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', async ({ request }) => {
        resourceAttempts += 1;
        seenAuth.push(request.headers.get('authorization'));
        // A real timer, so the macrotask on which performRefresh clears its
        // in-flight promise gets to run before the 401 lands. Without it
        // MSW answers inside the same macrotask, the 401 branch joins the
        // proactive attempt's already-resolved 503 instead of starting the
        // second rotation a real round-trip would, and the recovery this
        // test is about never happens. Ordering is deterministic: the
        // setTimeout(0) was scheduled first.
        await delay(5);
        return resourceAttempts === 1
          ? HttpResponse.json({}, { status: 401 })
          : HttpResponse.json({ ok: true }, { status: 200 });
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return refreshAttempts === 1
          ? HttpResponse.json(
              { code: 'session_enforcement_unavailable' },
              { status: 503 }
            )
          : HttpResponse.json(
              { accessToken: 'fresh-token', expiresIn: 900 },
              { status: 200 }
            );
      })
    );

    const store = await seededStore(inMs(-1_000));
    const result = await store.dispatch(
      probeApi.endpoints.replayGuardProbe.initiate('/v1/some/resource')
    );

    // One proactive attempt (503) and one reactive attempt (200).
    expect(refreshAttempts).toBe(2);
    expect(resourceAttempts).toBe(2);
    expect(result.data).toEqual({ ok: true });
    expect(seenAuth).toEqual([null, 'Bearer fresh-token']);
    expect(store.getState().auth.accessToken).toBe('fresh-token');
  });

  // The proof is a statement about the request that WENT OUT, so it is read
  // from the token state captured before the fetch. Here a live bearer is
  // sent and the store loses it while the 401 is on the wire — a sibling tab
  // signing out is the deterministic version of the same race a sibling
  // rotation runs. Reading the store back at 401 time would find no live
  // bearer, conclude proof (b), and replay a request that DID reach its
  // handler; the send-time snapshot is what keeps that from happening.
  it('passes a codeless 401 through when the store lost its bearer mid-flight', async () => {
    let resourceAttempts = 0;
    let refreshAttempts = 0;
    let siblingSignOut: (() => void) | null = null;
    server.use(
      http.get('*/v1/some/resource', () => {
        resourceAttempts += 1;
        // Runs after the request left, so this cannot race the snapshot.
        siblingSignOut?.();
        return HttpResponse.json({}, { status: 401 });
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json(
          { accessToken: 'must-not-be-fetched', expiresIn: 900 },
          { status: 200 }
        );
      })
    );

    const store = await seededStore(inMs(60_000));
    siblingSignOut = () => {
      store.dispatch(clearAccessToken());
    };
    const result = await store.dispatch(
      probeApi.endpoints.replayGuardProbe.initiate('/v1/some/resource')
    );

    expect(resourceAttempts).toBe(1);
    expect(refreshAttempts).toBe(0);
    expect((result.error as { status?: number } | undefined)?.status).toBe(401);
  });
});
