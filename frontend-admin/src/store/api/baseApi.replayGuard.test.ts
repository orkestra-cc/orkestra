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
// Neither proof: the 401 goes back to the caller untouched — and, when it
// also carries no code at all, the console rotates ONCE on the way out
// without replaying anything (§8 #14). A live bearer plus a codeless 401 is
// almost always the handler's own verdict, but it is also exactly what a
// JWT signing-key rotation looks like, and against that the console used to
// do nothing at all until the proactive check fired. The rotation costs one
// serialised refresh; the REPLAY is the part that costs a wrong-password
// attempt, and that is what every "attempts stays 1" assertion below pins.
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
const seededStore = async (
  tokenExpiry: string | null,
  accessToken: string | null = 'seed-access-token'
) => {
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
      accessToken,
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
  //
  // The RE-SEND is the whole defect, and `changeAttempts` staying 1 is the
  // assertion that keeps that honest. The rotation beside it is the §8 #14
  // arm doing its job: this 401 is indistinguishable, from the console's
  // side, from the one a signing-key rotation produces, so it rotates once
  // and hands the ORIGINAL 401 straight back.
  it('rotates once but does not replay a wrong-current-password change-password 401', async () => {
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
    // Rotated exactly once — coalesced, and never replayed.
    expect(refreshAttempts).toBe(1);
    // A wrong password is not a dead session: the arm installs the rotated
    // bearer and clears nothing.
    expect(store.getState().auth.accessToken).toBe('fresh-token');
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
  //
  // One honesty note the fixture cannot carry on its own: it stubs a 200
  // refresh, but a sibling tab that really signed out has revoked the
  // refresh cookie, so the production answer here is a 401 → the sign-out
  // path (the case below this one). What this case pins is the MECHANISM —
  // the arm fires once, replays nothing, and hands the caller its 401 — not
  // a claim that a sibling's sign-out leaves the session alive.
  it('rotates once and still passes the codeless 401 through when the store lost its bearer mid-flight', async () => {
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
          { accessToken: 'rotated-token', expiresIn: 900 },
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

    // Sent once. The rotation happens, the RE-SEND does not.
    expect(resourceAttempts).toBe(1);
    expect(refreshAttempts).toBe(1);
    expect((result.error as { status?: number } | undefined)?.status).toBe(401);
    expect(store.getState().auth.accessToken).toBe('rotated-token');
  });

  // The arm's third outcome. A 401 from /refresh-cookie is the refresh
  // cookie itself being refused, which IS the session's own death — not an
  // outage — so the arm falls through to the pre-existing sign-out path
  // rather than returning quietly. Nothing is replayed on the way there:
  // the resource is still only ever sent once.
  it('signs out when the rotation on a codeless verdict 401 is itself refused', async () => {
    let resourceAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', () => {
        resourceAttempts += 1;
        return HttpResponse.json({}, { status: 401 });
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json({}, { status: 401 });
      })
    );

    const store = await seededStore(inMs(60_000));
    const result = await store.dispatch(
      probeApi.endpoints.replayGuardProbe.initiate('/v1/some/resource')
    );

    expect(resourceAttempts).toBe(1);
    expect(refreshAttempts).toBe(1);
    expect((result.error as { status?: number } | undefined)?.status).toBe(401);
    expect(store.getState().auth.accessToken).toBeFalsy();
  });

  // The arm's second outcome. A 503 says the backend could not EVALUATE the
  // session (ADR-0017), and an outage must never be read as a sign-out — the
  // same rule the replay path's own `retry` arm follows. The original 401
  // goes back untouched and the token stays exactly as it was, so the next
  // request gets to try again.
  it('keeps the token when the rotation on a codeless verdict 401 is unavailable', async () => {
    let resourceAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', () => {
        resourceAttempts += 1;
        return HttpResponse.json({}, { status: 401 });
      }),
      http.post('*/refresh-cookie', () => {
        refreshAttempts += 1;
        return HttpResponse.json(
          { code: 'session_enforcement_unavailable' },
          { status: 503 }
        );
      })
    );

    const store = await seededStore(inMs(60_000));
    const result = await store.dispatch(
      probeApi.endpoints.replayGuardProbe.initiate('/v1/some/resource')
    );

    expect(resourceAttempts).toBe(1);
    expect(refreshAttempts).toBe(1);
    expect((result.error as { status?: number } | undefined)?.status).toBe(401);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // ── The rotation the arm triggers is classified by an ALLOWLIST ──────────
  //
  // Only a 401 from /refresh-cookie is the credential being refused. Every
  // other non-2xx says something about the SERVER and nothing about the
  // session, so it keeps the token — which matters most here, on the #14 arm,
  // because the 401 that triggered this rotation is a VERDICT (a mistyped
  // password). Before the allowlist, a wrong password whose rotation happened
  // to meet a rate limit or a 5xx signed the operator out: a wrong-current-
  // password 401 could end a session, which it never could before the arm
  // existed. /refresh-cookie sits under the router's GLOBAL rate limiter, so
  // 429 is reachable on every refresh and a burst of tabs is what trips it.
  it.each([
    ['429 rate-limited', 429],
    ['500 server error', 500]
  ])(
    'keeps the session when the verdict-401 rotation answers %s',
    async (_label, status) => {
      let changeAttempts = 0;
      let refreshAttempts = 0;
      server.use(
        http.post('*/v1/auth/operator/change-password', () => {
          changeAttempts += 1;
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
          return HttpResponse.json({}, { status });
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

      // Still sent once, still not replayed, and still signed in.
      expect(changeAttempts).toBe(1);
      expect(refreshAttempts).toBe(1);
      expect(store.getState().auth.accessToken).toBe('seed-access-token');
    }
  );

  // A 2xx whose body cannot be read is a BROKEN RESPONSE, not a rejection: a
  // captive portal or a proxy error page arrives as 200 text/html, and a
  // connection dropped mid-body rejects the read. Neither has told us
  // anything about the session, so neither may end it. This is the half of
  // the allowlist that lives past the status line.
  it('keeps the session when the verdict-401 rotation answers an unreadable 2xx', async () => {
    let changeAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.post('*/v1/auth/operator/change-password', () => {
        changeAttempts += 1;
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
        // 200, but not JSON — res.json() rejects.
        return HttpResponse.text('<html>captive portal</html>', {
          status: 200
        });
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

    expect(changeAttempts).toBe(1);
    expect(refreshAttempts).toBe(1);
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });

  // A PUBLIC route breaks proof (b)'s reasoning: it runs its handler with no
  // bearer, by design, so "no bearer was sent" proves nothing about whether
  // the handler ran. The passkey login pair is exactly that shape — mounted
  // by WebAuthnHandler.RegisterPublicRoutes, called while the store holds no
  // access token (the login is paused mid-ceremony), and LoginFinish calls
  // IncrementAttempts BEFORE returning its 401, so a replay spends two of
  // MFAMaxAttempts (5) per typo. AUTH_ENDPOINT_PATHS is what keeps proof (b)
  // sound: the public auth routes never reach the branch at all.
  it('does not refresh or replay a failed passkey login assertion', async () => {
    let finishAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.post('*/v1/auth/operator/mfa/webauthn/login/finish', () => {
        finishAttempts += 1;
        return HttpResponse.json(
          {
            title: 'Unauthorized',
            status: 401,
            detail: 'webauthn assertion failed'
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

    // A paused passkey login: password step done, no access token yet.
    const store = await seededStore(null, null);
    await expect(
      store
        .dispatch(
          probeApi.endpoints.replayGuardPost.initiate(
            '/v1/auth/operator/mfa/webauthn/login/finish'
          )
        )
        .unwrap()
    ).rejects.toMatchObject({ status: 401 });

    expect(finishAttempts).toBe(1);
    expect(refreshAttempts).toBe(0);
  });

  // The deliberate widening: proof (b) is "no live bearer went out", which
  // includes "no token in the store at all" and not only "the local expiry
  // had passed". On a PROTECTED route that is sound — RequireAuth rejects a
  // bearer-less request before dispatch — and it is what recovers a request
  // that raced the session bootstrap, or one fired after clearAccessToken()
  // while the refresh cookie is still good. Pinned here so a later narrowing
  // to "a token existed and had expired" cannot pass unnoticed.
  it('refreshes and replays a protected route sent with no token at all', async () => {
    const seenAuth: Array<string | null> = [];
    let resourceAttempts = 0;
    let refreshAttempts = 0;
    server.use(
      http.get('*/v1/some/resource', ({ request }) => {
        resourceAttempts += 1;
        seenAuth.push(request.headers.get('authorization'));
        return resourceAttempts === 1
          ? HttpResponse.json({}, { status: 401 })
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

    const store = await seededStore(null, null);
    const result = await store.dispatch(
      probeApi.endpoints.replayGuardProbe.initiate('/v1/some/resource')
    );

    // No proactive attempt: tokenNeedsRefresh needs a token to want one.
    expect(refreshAttempts).toBe(1);
    expect(resourceAttempts).toBe(2);
    expect(result.data).toEqual({ ok: true });
    expect(seenAuth).toEqual([null, 'Bearer fresh-token']);
    expect(store.getState().auth.accessToken).toBe('fresh-token');
  });
});
