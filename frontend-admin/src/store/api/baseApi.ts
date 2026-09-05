import {
  createApi,
  fetchBaseQuery,
  BaseQueryFn,
  FetchArgs,
  FetchBaseQueryError,
  FetchBaseQueryMeta
} from '@reduxjs/toolkit/query/react';
import { toast } from 'react-toastify';
import type { RootState } from '../index';
import { setAccessToken, clearAccessToken } from '../slices/authSlice';
import { requestStepUp } from '../stepUp';
import { requestPasswordConfirm } from '../passwordConfirm';
import { DEFAULT_POST_LOGIN, sanitizeReturnTo } from 'utils/returnTo';
import runtimeConfig from 'config/environment';

// Navigation helper - will be set by the auth provider
let navigateToLogin: ((location?: string) => void) | null = null;

export const setNavigateToLogin = (fn: (location?: string) => void) => {
  navigateToLogin = fn;
};

// The page the operator is on, as the login flow needs to receive it: pathname
// AND search. One helper because all four navigateToLogin call sites below owe
// the same answer, and three of them used to pass the pathname alone. That was
// invisible for as long as AuthProvider stored `state.from` as a string and
// locationToReturnTo dropped it — every one of these redirects landed on
// DEFAULT_POST_LOGIN regardless. Now that the callback stores a Location and
// the deep link survives, the difference is user-visible: a revoked session
// would return to /admin/modules having silently lost ?tab=addons.
//
// The hash is deliberately absent. It never reaches the server, react-router
// does not use it for routing in this SPA, and sanitizeReturnTo's checks are
// written against path + query — carrying it would add a value no consumer
// reads and one more shape for the sanitiser to reason about.
const currentPath = (): string =>
  window.location.pathname + window.location.search;

// Endpoints for which a 401 must NOT trigger a silent refresh attempt —
// either because they *are* the refresh/login/logout endpoints (retrying
// would loop) or because a 401 here already means "user is not signed in"
// and the correct UX is to fall through to the caller. ADR-0003 PR-D D-8
// dropped the legacy un-prefixed paths; this dashboard targets the
// operator tier, so all entries are mounted under /v1/auth/operator.
//
// This list is load-bearing for the 401 branch's proof (b) below, and not
// only for loop avoidance: proof (b) reasons that a bearer-less request was
// rejected by RequireAuth before dispatch, which holds for PROTECTED routes
// and says nothing about the PUBLIC ones — those run their handler with no
// bearer at all, by design, and some of them answer 401 as a verdict on the
// request. Every public route this SPA calls that can answer 401 therefore
// belongs here. The passkey login pair is the one that was missed: it is
// mounted by WebAuthnHandler.RegisterPublicRoutes, the store holds no access
// token during a paused login, and LoginFinish calls IncrementAttempts
// BEFORE returning its 401 — so a replay spends two of MFAMaxAttempts (5)
// per typo. Its TOTP twin (mfa/login/verify) was already here. One entry
// covers both halves of the ceremony: the match is a substring test.
const AUTH_ENDPOINT_PATHS = [
  'v1/auth/operator/login',
  'v1/auth/operator/logout',
  'v1/auth/operator/refresh',
  'v1/auth/operator/refresh-cookie',
  'v1/auth/operator/register',
  'v1/auth/operator/mfa/login/verify',
  'v1/auth/operator/mfa/webauthn/login/' // begin + finish
];

function isAuthEndpoint(url: string): boolean {
  return AUTH_ENDPOINT_PATHS.some(p => url.includes(p));
}

// Shared in-flight refresh promise. Any 401 arriving while a refresh is
// already in progress awaits the same promise instead of firing N parallel
// refresh requests that would rotate the refresh token N times and trip
// the backend's family-replay guard.
//
// This guard is per-TAB — a module-level variable in one JS context. It
// says nothing about the other tabs the operator has open, and those are
// the ones that hurt: every tab shares a login, so their access tokens
// expire at the same instant, each takes its own 401, and each posts the
// same refresh cookie. Exactly one wins the backend's rotation CAS; the
// losers used to be answered with refresh_token_replay, which revokes the
// whole family — the winner's brand-new token included — and forced a full
// re-login roughly once per access-token lifetime. REFRESH_LOCK_NAME
// serialises the rotation across tabs so only one is ever in flight for
// the origin; the others then rotate in turn, each with the cookie its
// predecessor left behind.
//
// `retry` is NOT the same answer as a bare `ok: false`, and which one an
// attempt produces is decided by an ALLOWLIST in refreshOnce below: a bare
// `ok: false` means the refresh cookie itself was REFUSED (a 401, and only a
// 401), and it is the one outcome that ends the session. `retry` is
// everything else — a 503, a 429, any other 4xx or 5xx, a transport failure,
// the timeout, a twice-raced 409, and a 2xx whose body is unreadable or
// carries no token. ADR-0017 gives the 503 its own status precisely so a
// client does not treat it as a sign-out: an outage "would train clients to
// discard a session that is still perfectly valid." That reasoning is not
// specific to 503 — it holds for every answer that describes the server
// rather than the credential, which is why the rule is an allowlist and not
// a list of transient statuses to remember to extend.
type RefreshResult =
  | { ok: true; accessToken: string; expiresIn: number }
  | { ok: false; retry?: boolean; raced?: boolean };
let inFlightRefresh: Promise<RefreshResult> | null = null;

const REFRESH_LOCK_NAME = 'orkestra:auth-refresh';

// How long refreshOnce waits for /refresh-cookie to answer before treating
// the attempt as failed rather than letting the caller hang. Since #317's
// proactive rotation (PROACTIVE_REFRESH_SKEW_MS below) this fetch's await
// sits on the OUTBOUND path of every request whose token is inside the skew
// window, so a connection that accepts and never answers no longer just
// delays an eventual error — it stalls the request itself, indefinitely.
// 10s is generous for a same-origin POST under normal load, and short
// enough that a stalled connection doesn't hold up requests for longer than
// an operator will wait before assuming the app is broken. See the `catch`
// below for why timing out must NOT be treated as a negative answer.
export const REFRESH_FETCH_TIMEOUT_MS = 10_000;

// The bound refreshOnce actually applies. Production never changes it; the
// setter below is the ONLY writer, and it exists so the timeout tests can
// exercise the real abort path in 25 ms of wall clock.
let refreshFetchTimeoutMs: number = REFRESH_FETCH_TIMEOUT_MS;

// TEST-ONLY, and not part of this module's production surface — nothing
// outside `baseApi.*.test.ts` may call it. It replaces the two
// `vi.spyOn(AbortSignal, 'timeout')` monkey-patches those suites used to
// carry: an AbortController's timer is an ordinary `setTimeout`, so nothing
// needs patching, but fake timers are still not an option here —
// performRefresh schedules its own `setTimeout(…, 0)` and every suite drains
// it on a real timer in `afterEach`, which a file-wide `vi.useFakeTimers()`
// would hang. Called with no argument it restores the production value,
// which is what those `afterEach` hooks do.
export function __setRefreshTimeoutForTests(
  ms: number = REFRESH_FETCH_TIMEOUT_MS
): void {
  refreshFetchTimeoutMs = ms;
}

// Web Locks is the only cross-tab primitive that releases automatically
// when the holder navigates away or crashes, which a localStorage mutex
// cannot promise. Where it is missing (non-secure context, jsdom under
// test) we fall back to running unguarded: the backend's rotation grace
// window still keeps a lost race from ending the session.
//
// NOT bounded with a timeout signal (unlike refreshOnce's fetch below): Web
// Locks only supports that via the 3-argument overload
// `request(name, { signal }, callback)`, and the transitive bound is
// refreshOnce's own abort, which now covers the body read too. The
// two-argument shape here is pinned by `takes the cross-tab lock when Web
// Locks is available` in baseApi.rotationRace.test.ts — it asserts arity 2
// and that the second argument is the run callback, because a 3-arg call
// would otherwise bind that mock's `cb` to the options object and have the
// resulting throw swallowed by performRefresh's `.catch`.
async function withRefreshLock<T>(run: () => Promise<T>): Promise<T> {
  const locks = typeof navigator !== 'undefined' ? navigator.locks : undefined;
  if (!locks?.request) return run();
  // LockGrantedCallback<T> is declared as returning T, so T infers here as
  // Promise<RefreshResult>; the await unwraps both layers.
  return await locks.request(REFRESH_LOCK_NAME, run);
}

// models.TokenResponse, as far as this module cares about it.
type RefreshBody = { accessToken?: string; expiresIn?: number };

// A promise that rejects when the signal aborts and never resolves otherwise.
// `fetch` resolves on HEADERS, and the body stream of a mocked or proxied
// response does not always observe the request's abort signal, so the body
// read is raced against the signal EXPLICITLY. The transitive bound on the
// Web Lock must not depend on the platform propagating an abort into the
// body: a server that sends headers and then stalls would otherwise hold the
// lock — and the in-flight promise every other tab is awaiting — for as long
// as it stalls. Nothing inspects the rejection value; it only unblocks the
// race. The listener is per-attempt (a fresh controller each time) and
// `once`, so nothing accumulates.
function rejectOnAbort(signal: AbortSignal): Promise<never> {
  return new Promise<never>((_, reject) => {
    const fail = () => reject(new Error('refresh aborted'));
    if (signal.aborted) {
      fail();
      return;
    }
    signal.addEventListener('abort', fail, { once: true });
  });
}

async function refreshOnce(baseUrl: string): Promise<RefreshResult> {
  // AbortController + setTimeout, NOT AbortSignal.timeout: the latter runs on
  // an internal timer nothing in the test suite can shorten without patching
  // a platform object, and it cannot be cancelled either, so every refresh
  // left a live 10s timer behind it.
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), refreshFetchTimeoutMs);
  try {
    const res = await fetch(`${baseUrl}/v1/auth/operator/refresh-cookie`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      signal: ctrl.signal
    });
    // The outcome rule is an ALLOWLIST, and the order below is the rule.
    //
    // 401 is the ONLY status that means "the credential I presented was
    // refused" — the refresh cookie is gone, revoked, or was never valid, and
    // that is the session's own death. Everything else that is not a usable
    // 2xx says something about the SERVER and nothing about the session.
    //
    // A denylist is what this used to be, and it was wrong in a way no test
    // caught: only 503 and 409 were transient, so 429, 408, 500, 502 and 504
    // all fell through to the bare `{ ok: false }` the 401 branch reads as a
    // real rejection. /refresh-cookie sits under the router's GLOBAL rate
    // limiter, so 429 is reachable on every refresh and a burst of tabs is
    // exactly what trips it — and once the #14 arm started rotating on
    // verdict 401s, a mistyped password whose rotation met a rate limit could
    // end a session that a mistyped password could never end before.
    if (res.status === 401) return { ok: false } as const;
    // 409 refresh_rotation_raced: a sibling rotated first and the family
    // is intact. Our cookie jar already holds its successor.
    if (res.status === 409) return { ok: false, raced: true } as const;
    // 503 session_enforcement_unavailable, 429, 408, any other 4xx or 5xx.
    if (!res.ok) return { ok: false, retry: true } as const;
    // The parse is settled into a DISCRIMINATED result before the race — a
    // wrapper on success, `null` on any parse failure — so that whichever
    // promise loses cannot surface later as an unhandled rejection AND an
    // unreadable body stays distinguishable from an empty one. Racing it
    // against the signal is what makes the timer bound the READ as well as
    // the headers.
    const parsed: Promise<{ body: RefreshBody } | null> = res.json().then(
      (body: RefreshBody) => ({ body }),
      () => null
    );
    const raced = await Promise.race([parsed, rejectOnAbort(ctrl.signal)]);
    // Two ways to have no answer, and neither is a rejection. The abort may
    // have fired (the racers can then settle in either order, so the signal
    // is read directly rather than inferred from who won); or the body was
    // unreadable — a connection dropped mid-read, an empty 200, a captive
    // portal's HTML, a proxy error page served as 200. Both throw into the
    // `catch` below and come back transient. No timer can fire between the
    // await and this line — a macrotask cannot preempt a running microtask —
    // so a body that arrived in time is never discarded here.
    if (ctrl.signal.aborted || raced === null) {
      throw new Error('refresh answer not readable');
    }
    const body = raced.body;
    // A 2xx with no token is a BROKEN RESPONSE, which is the reason not to
    // act on it: it has told us nothing about the session either.
    if (!body.accessToken || !body.expiresIn)
      return { ok: false, retry: true } as const;
    return {
      ok: true,
      accessToken: body.accessToken,
      expiresIn: body.expiresIn
    } as const;
  } catch {
    // Nothing here distinguishes WHY the attempt threw — the abort firing
    // (on the headers OR on the body read), an unreadable body, a DNS
    // failure, the tab going offline, a rejected promise from the network
    // stack. What matters is that we never got an answer at all. The naive
    // "just add a timeout" fix lands here and falls through to a bare
    // `{ ok: false }`, which the 401 branch in baseQueryWithRetry reads as a
    // REAL negative answer and signs the user out (clearAccessToken +
    // navigateToLogin) — turning "the network is slow" into "you are logged
    // out", a worse bug than the hang it replaces. No answer is not the same
    // as "no", so this returns the transient outcome, exactly as every
    // non-401 status above does. Do not "simplify" this back to a bare
    // `{ ok: false }` — that is the one outcome reserved for a refused
    // credential.
    return { ok: false, retry: true } as const;
  } finally {
    clearTimeout(timer);
  }
}

async function performRefresh(baseUrl: string): Promise<RefreshResult> {
  if (inFlightRefresh) return inFlightRefresh;
  inFlightRefresh = (async () => {
    try {
      // refreshOnce never throws, but the lock itself can (an aborted or
      // rejected request). Keep the original contract that performRefresh
      // always RESOLVES: a lock failure says nothing about the session, so
      // it must not be reported as a sign-out.
      return await withRefreshLock(async () => {
        const first = await refreshOnce(baseUrl);
        if (!first.ok && first.raced) {
          // Someone rotated between our 401 and our turn at the lock.
          // Exactly one retry: the successor cookie is already in the jar,
          // so a second attempt lands. A race that survives two attempts
          // is reported as transient rather than as a sign-out — the
          // session is far more likely alive than gone, and guessing
          // "gone" is the failure this whole change exists to remove.
          const second = await refreshOnce(baseUrl);
          if (!second.ok && second.raced)
            return { ok: false, retry: true } as const;
          return second;
        }
        return first;
      }).catch(() => ({ ok: false, retry: true }) as const);
    } finally {
      // Clear after the current microtask so concurrent awaiters all see
      // the same result, but a future 401 can kick off a fresh attempt.
      setTimeout(() => {
        inFlightRefresh = null;
      }, 0);
    }
  })();
  return inFlightRefresh;
}

// Access tokens are rotated BEFORE they expire, not after. The backend's
// RequireAuth is bearer-only (ADR-0020, #317): a request that arrives with
// an expired — or, per prepareHeaders, withheld — bearer is a plain 401,
// and the only way back to a valid token is /refresh-cookie. Doing that
// rotation ahead of any request whose token is inside this window means
// expiry almost never lands on a burst of parallel requests; when it does,
// every request in the burst awaits the one in-flight performRefresh and
// goes out with the fresh bearer instead of taking a 401 each.
//
// INVARIANT: strictly below the backend's MinAccessTokenTTL (60 s,
// backend/internal/core/auth/services/auth_duration_bounds.go). At or above
// the floor, a token minted at the minimum TTL is already inside this
// window the moment it arrives, so every request would rotate again — a
// refresh loop. 30 s leaves a floor-length token half its life of quiet.
// Pinned by baseApi.proactiveRefresh.test.ts ("does not loop …").
export const PROACTIVE_REFRESH_SKEW_MS = 30_000;

function tokenNeedsRefresh(state: RootState): boolean {
  const accessToken = state.auth?.accessToken;
  const tokenExpiry = state.auth?.tokenExpiry;
  if (!accessToken || !tokenExpiry) return false;
  return (
    new Date(tokenExpiry).getTime() - Date.now() < PROACTIVE_REFRESH_SKEW_MS
  );
}

// Endpoints that must NOT carry X-Tenant-ID because they run before the
// current tenant is known (login, refresh, tenant listing, tenant creation,
// invite accept), or because they are platform-level (module admin,
// first-install setup) and the backend's tenant-resolution middleware would
// reject a stray header.
const TENANT_AGNOSTIC_PATHS = [
  '/v1/auth/',
  '/v1/tenants', // GET list, POST create
  '/v1/tenants/accept-invite',
  '/v1/notifications/preferences',
  '/v1/admin/modules', // platform-level module admin, not per-tenant
  '/v1/admin/service-accounts', // platform-level admin surface, not per-tenant
  '/v1/admin/tenants', // platform-level tenant admin, not per-tenant
  '/v1/admin/audit-events', // platform-level audit read, not per-tenant
  '/v1/admin/compliance', // platform-level compliance (SOC2 evidence, …)
  '/v1/me/dsr', // DSR endpoints operate on the caller's own subject
  '/v1/setup' // first-install wizard endpoints
];

function isTenantAgnostic(url: string): boolean {
  // Exact-match /v1/tenants (listing/creation) but pass through for
  // /v1/tenants/{tenantId}/...
  if (url === '/v1/tenants' || url.startsWith('/v1/tenants?')) return true;
  if (url === '/v1/tenants/accept-invite') return true;
  return TENANT_AGNOSTIC_PATHS.some(
    p => p !== '/v1/tenants' && url.startsWith(p)
  );
}

// The one predicate that decides whether a request carries a bearer, and
// therefore the one that says whether it could have reached its handler.
// prepareHeaders below withholds the Authorization header once the console's
// own recorded expiry has passed — RequireAuth is bearer-only (ADR-0020), so
// a token we already know is dead buys nothing — and the 401 branch in
// baseQueryWithRetry has to know exactly that fact to tell a rejected-before-
// dispatch 401 from an endpoint's own verdict. Two copies of the rule could
// drift apart and turn the replay guard into a fiction, so there is one
// function and both call sites use it. Distinct from tokenNeedsRefresh
// above: that one asks "will it expire soon" (with a skew), this one asks
// "is it alive right now" (no margin), which is the only question
// prepareHeaders and the 401 branch ever ask.
//
// null also covers "no token at all", deliberately: a request that carries
// no bearer is rejected by RequireAuth before dispatch exactly as an expired
// one is, so on a PROTECTED route the handler provably never ran either way.
// That argument stops at the route: a PUBLIC route runs its handler without
// any bearer, so "no bearer sent" proves nothing there. Those routes are
// excluded from the 401 branch by AUTH_ENDPOINT_PATHS above, which is what
// keeps this reading of null sound — the two are one mechanism, not two.
function liveBearer(state: RootState): string | null {
  const accessToken = state.auth?.accessToken;
  const tokenExpiry = state.auth?.tokenExpiry;
  if (!accessToken || !tokenExpiry) return null;
  return new Date(tokenExpiry) > new Date() ? accessToken : null;
}

// Base fetch with cookies + Bearer token. Tenant context (X-Tenant-ID) is
// injected by baseQueryWithRetry below, where we have access to the request
// args and can decide whether the endpoint is tenant-scoped.
//
// ADR-0003 PR-C: the operator dashboard targets the operator host
// (`console.*` in staging/prod). In development it targets `localhost:3000`,
// which the host mux's dev fallthrough serves off the operator mux — the
// console's own origin is `localhost:8080`, and this request carries
// `credentials: 'include'` against a `SameSite=Lax` refresh cookie, so the
// two must be one site (spec §8 follow-up #13). Put the console on
// `console.localhost` end to end if you prefer, but never one of each.
const baseQuery = fetchBaseQuery({
  baseUrl: runtimeConfig.apiUrl,
  credentials: 'include',
  prepareHeaders: (headers, { getState }) => {
    headers.set('Content-Type', 'application/json');

    const bearer = liveBearer(getState() as RootState);
    if (bearer) {
      headers.set('Authorization', `Bearer ${bearer}`);
    }

    return headers;
  }
});

// Enhanced base query with automatic retry, error handling, and tenant context.
// The explicit 5th (Meta) generic matters: BaseQueryFn defaults it to {},
// which is what every endpoint's transformResponse/transformErrorResponse
// would see as the `meta` type otherwise — even though the underlying
// fetchBaseQuery call below always resolves a real FetchBaseQueryMeta
// (request/response). Declaring it lets endpoints (e.g. setupApi's
// getSetupStatus) read response headers via `meta.response.headers`.
const baseQueryWithRetry: BaseQueryFn<
  string | FetchArgs,
  unknown,
  FetchBaseQueryError,
  object,
  FetchBaseQueryMeta
> = async (args, api, extraOptions) => {
  // Inject X-Tenant-ID for every tenant-scoped request. Impersonation (set
  // by NineDotMenu / ImpersonateButton for system.tenants.admin holders)
  // takes precedence over the user's own currentOrgId — the backend
  // middleware honors the header only for admin callers and 403s everyone
  // else.
  const state = api.getState() as RootState;
  const effectiveTenantId =
    state.tenant?.impersonatedTenantId ?? state.tenant?.currentOrgId;
  if (effectiveTenantId) {
    const url = typeof args === 'string' ? args : args.url;
    if (!isTenantAgnostic(url)) {
      const merged: FetchArgs =
        typeof args === 'string'
          ? { url: args, headers: { 'X-Tenant-ID': effectiveTenantId } }
          : {
              ...args,
              headers: {
                ...(args.headers as Record<string, string> | undefined),
                'X-Tenant-ID': effectiveTenantId
              }
            };
      args = merged;
    }
  }

  // Proactive rotation (see PROACTIVE_REFRESH_SKEW_MS). Auth endpoints are
  // excluded for the same reason they skip the 401 retry: /session mints on
  // its own, and refreshing ahead of /refresh-cookie is redundant by
  // construction — refreshOnce hits that endpoint with a raw fetch,
  // bypassing this function entirely, so there is no call stack to recurse
  // into. The exclusion stays as defence-in-depth, in case a future refresh
  // path is ever routed through baseQueryWithRetry instead. Any outcome
  // other than `ok` falls through untouched — the request goes out as-is
  // and the 401 branch below stays the single owner of the sign-out
  // decision (a dead session costs one extra refresh-cookie round-trip).
  const preflightUrl = typeof args === 'string' ? args : args.url;
  if (
    !isAuthEndpoint(preflightUrl) &&
    !preflightUrl.includes('v1/auth/session') &&
    tokenNeedsRefresh(api.getState() as RootState)
  ) {
    const refreshed = await performRefresh(runtimeConfig.apiUrl);
    if (refreshed.ok) {
      api.dispatch(
        setAccessToken({
          accessToken: refreshed.accessToken,
          expiresIn: refreshed.expiresIn
        })
      );
    }
  }

  // Captured BEFORE the fetch, because the 401 branch below asks a question
  // about the request that actually went out: did it carry a live bearer?
  // Reading the store back after the 401 answers a different question — a
  // sibling tab may have rotated or signed out in the meantime, and either
  // way the answer would no longer describe the request the server saw.
  // This is the same state prepareHeaders reads (no await between here and
  // the fetch), so the two cannot disagree.
  const sentBearer = liveBearer(api.getState() as RootState);

  let result = await baseQuery(args, api, extraOptions);

  // Handle authentication errors
  if (result.error && result.error.status === 401) {
    // Note: No localStorage cleanup needed - using HttpOnly cookies only

    const requestUrl = typeof args === 'string' ? args : args.url;
    const isSessionEndpoint = requestUrl.includes('v1/auth/session');
    const isAuthCheck =
      requestUrl.includes('v1/auth/operator/me') ||
      requestUrl.includes('v1/auth/session');

    // Server-side session termination sets a code on the 401 body. Skip the
    // silent-refresh retry in both cases — a new access token minted from
    // the same refresh cookie would carry the same dead sid and just fail
    // again. The two codes share the logic and differ only in the message:
    // "revoked" is inaccurate for a session that simply reached its maximum
    // age, and the distinction matters to whoever reads the support ticket.
    const errorData = (result.error as { data?: { code?: string } }).data;
    const sessionEndedMessages: Record<string, string> = {
      session_revoked: 'Your session has been revoked. Please sign in again.',
      session_max_age_reached:
        'Your session reached its maximum age. Please sign in again.'
    };
    const sessionEndedMessage = errorData?.code
      ? sessionEndedMessages[errorData.code]
      : undefined;
    if (sessionEndedMessage) {
      api.dispatch(clearAccessToken());
      // `isAuthCheck` suppresses the toast because a 401 from /me or
      // /v1/auth/session on a cold load usually means "never signed in", and
      // telling an anonymous visitor their session expired is noise.
      //
      // A server-side TERMINATION is a different event and must not inherit
      // that suppression. `session_max_age_reached` in particular is only
      // ever emitted for a session that existed and was ended by policy —
      // /v1/auth/session is one of the two endpoints that emit it, so
      // suppressing it there made the message unreachable on that path.
      // ADR-0017 D4 gives the cap its own wording specifically so the user
      // (and whoever reads their support ticket) is told what happened.
      const isServerSideTermination =
        errorData!.code === 'session_max_age_reached';
      if (!isAuthCheck || isServerSideTermination) {
        toast.error(sessionEndedMessage, {
          toastId: errorData!.code,
          autoClose: 5000
        });
      }
      if (navigateToLogin) {
        navigateToLogin(currentPath());
      }
      return result;
    }

    // Step-up MFA required. Pause the original request, open the global
    // StepUpModal via requestStepUp(), and replay once the user completes
    // /v1/auth/operator/mfa/verify — the mutation dispatches a refreshed access
    // token into Redux so the replay carries fresh AMR + last_otp_at.
    // Auth endpoints themselves are excluded so we don't recurse on
    // /mfa/verify's own 401s.
    if (
      result.error.status === 401 &&
      errorData?.code === 'step_up_required' &&
      !isAuthEndpoint(requestUrl)
    ) {
      const verified = await requestStepUp();
      if (verified) {
        return await baseQuery(args, api, extraOptions);
      }
      return result;
    }

    // Password reconfirm required — the no-MFA-factor fallback path of
    // RequireStepUp. The backend emits this 401 when the user has no
    // TOTP / passkey enrolled and the policy doesn't require them to;
    // asking for an MFA code in StepUpModal would be a dead-end. We
    // open PasswordConfirmModal instead, which posts to
    // /me/password-confirm and replays the original request with the
    // amr=[…,"reauth"] bearer dispatched by the mutation.
    if (
      result.error.status === 401 &&
      errorData?.code === 'password_confirm_required' &&
      !isAuthEndpoint(requestUrl)
    ) {
      const verified = await requestPasswordConfirm();
      if (verified) {
        return await baseQuery(args, api, extraOptions);
      }
      return result;
    }

    // Reauthentication required — the no-factor branch of the backend's
    // RequireEnrolmentProof gate (spec §4.2 D14), emitted when a session is
    // too old to add a *first* second factor.
    //
    // It is the third gate answer and the only one with no modal: a step-up
    // needs a factor the caller does not have, and a password reconfirm is
    // wrong for both an OAuth-only account (no password to reconfirm) and an
    // MFA-obligated one inside its grace window (the reconfirm endpoint
    // refuses those outright). A fresh sign-in is the one answer every
    // population can give, so clear the session and send the operator to the
    // login form with the page they were on.
    //
    // No `!isAuthEndpoint(requestUrl)` guard, unlike the two branches above.
    // Theirs is not politeness: the modal they open calls an auth route, so
    // that route's own 401 would re-open the modal. This branch opens
    // nothing, issues no request and replays nothing — there is no loop to
    // avoid, and an auth route that ever answered this code would want the
    // same redirect anyway.
    //
    // The path is sanitised before it leaves. `window.location` is
    // attacker-influenceable within the origin (history.pushState keeps the
    // origin but not the shape), so without this the branch would hand a
    // crafted destination to the login flow on every stale enrolment
    // attempt. A rejected path degrades to DEFAULT_POST_LOGIN rather than to
    // `undefined`, which AuthProvider would fill back in from
    // `location.pathname` — the very value that was just rejected.
    if (errorData?.code === 'reauthentication_required') {
      api.dispatch(clearAccessToken());
      if (navigateToLogin) {
        navigateToLogin(sanitizeReturnTo(currentPath()) ?? DEFAULT_POST_LOGIN);
      }
      return result;
    }

    // On a fresh install the session endpoint legitimately returns 401
    // because no user exists yet. The SetupGate should be steering the
    // browser to /setup, not /login. Suppress the forced login redirect
    // and the toast while the setup wizard is active or while we have
    // not yet confirmed setupCompleted === true.
    const isOnSetupPath =
      typeof window !== 'undefined' &&
      window.location.pathname.startsWith('/setup');
    const apiState = (
      api.getState() as { api?: { queries?: Record<string, unknown> } }
    ).api;
    const setupQueryEntry = Object.values(apiState?.queries ?? {}).find(q => {
      return (
        (q as { endpointName?: string } | null)?.endpointName ===
        'getSetupStatus'
      );
    }) as { data?: { setupCompleted?: boolean } } | undefined;
    const setupCompleted = setupQueryEntry?.data?.setupCompleted === true;

    if (isOnSetupPath || !setupCompleted) {
      // First-install mode: never interrupt with a login redirect.
      // Return the 401 as-is so callers can still handle it (SetupGate's
      // setup-status query path itself is unauthenticated and returns 200).
      return result;
    }

    // Silent refresh: if the failing call was a normal protected endpoint,
    // try to rotate the refresh cookie and retry once. The refresh cookie
    // is HttpOnly and its TTL is independent of the access-token TTL, so
    // the user stays signed in for as long as the refresh token is valid
    // instead of being kicked out every access-token window.
    if (!isAuthEndpoint(requestUrl) && !isSessionEndpoint) {
      // …but ONLY on proof the request never reached its handler, because
      // the retry re-sends it and a request that ran once may have consumed
      // something. Four console routes answer 401 as a verdict on the
      // REQUEST and none of them is in AUTH_ENDPOINT_PATHS (that
      // hand-maintained allowlist is what failed open here): a wrong
      // current password on change-password, or on /me/password-confirm,
      // where the replay double-counts the lockout budget because the
      // service records the failure under both the IP and the email key; a
      // wrong code on mfa/verify or mfa/enroll/confirm, which burns the TOTP
      // replay guard, consumes a backup code, or spends one of five
      // enrolment attempts.
      //
      // Two independent proofs, either sufficient on its own:
      //  (a) the server says it rejected an EXPIRED bearer before dispatch
      //      (access_token_expired — the strongest proof, and the only one
      //      that covers a token which was live when it left and expired in
      //      flight);
      //  (b) no live bearer went out at all, by prepareHeaders' own
      //      predicate. On a protected route RequireAuth rejects that
      //      before dispatch too — and only the protected ones reach here,
      //      because AUTH_ENDPOINT_PATHS excludes the public auth routes,
      //      which run their handler bearer-less by design. This is the
      //      fallback ADR-0020 D3 assigns to this path — the proactive
      //      rotation failed, so the dead bearer was withheld — and it is
      //      what keeps the console recovering against a backend that does
      //      not yet send (a): a missing-bearer 401 is codeless.
      //
      // Neither proof: no replay. The request may have run, so re-sending it
      // is the hazard this whole gate exists to remove — and a mistyped
      // password is not a dead session, so there is no sign-out either.
      const handlerNeverRan =
        errorData?.code === 'access_token_expired' || sentBearer === null;

      // …but "no replay" is not the same as "do nothing", and for ONE input
      // in this shape doing nothing is wrong. A live bearer was sent and the
      // 401 carries no top-level code at all: almost always the handler's own
      // verdict, and also exactly what a JWT signing-key rotation (or a
      // restart with new key material) produces — every unexpired bearer then
      // validates as plain "invalid" and RequireAuth answers a CODELESS 401
      // (shared/middleware/auth.go). Against that the console used to do
      // nothing whatsoever: no refresh, no toast, no sign-out, every request
      // failing silently until the proactive check fired at
      // `expiry − PROACTIVE_REFRESH_SKEW_MS` — up to TTL − 30 s, ≈14.5 min at
      // the 15-minute default. So rotate ONCE and return the ORIGINAL 401
      // untouched: the next request carries the fresh bearer, which collapses
      // the window to a single request, and a genuinely dead session reaches
      // the sign-out branch below instead of failing quietly for a quarter of
      // an hour. The cost is one serialised rotation per verdict 401 — a
      // wrong password now rotates the refresh cookie.
      //
      // ⚠️ That cost was originally written down as "harmless: the family is
      // untouched, and performRefresh coalesces, so a burst costs one
      // rotation and not one each". Staging falsified the second half on
      // 2026-09-04: performRefresh SERIALISES rotations, it does not collapse
      // them, so 26 mistyped MFA codes in 13 seconds produced 26 codeless
      // 401s, 44 `409 refresh_rotation_raced` answers, 8 rotations, and then
      // reuse detection killed the session. The per-rotation claim is true
      // and beside the point — the races are the harm. The real fix was on
      // the backend: every verdict 401 now carries a code
      // (backend/internal/shared/errcode/codes.go), so those routes take the
      // coded branch above and never reach this one. This arm remains for
      // what it was written for, the signing-key rotation.
      //
      // CODELESS, not "anything but access_token_expired". A 401 that names
      // itself has been explained by the server, and a token minted from the
      // same cookie cannot change the answer — `audience_mismatch` is the
      // live example, emitted by RequireAudience, unhandled by every branch
      // ahead of this one, and identical after any rotation.
      const rotateWithoutReplay =
        !handlerNeverRan && errorData?.code === undefined;
      if (!handlerNeverRan && !rotateWithoutReplay) {
        return result;
      }
      const refreshResult = await performRefresh(runtimeConfig.apiUrl);
      if (refreshResult.ok) {
        api.dispatch(
          setAccessToken({
            accessToken: refreshResult.accessToken,
            expiresIn: refreshResult.expiresIn
          })
        );
        if (rotateWithoutReplay) {
          // The state is updated and that is the whole mitigation. The
          // request that earned this 401 is never sent twice.
          return result;
        }
        result = await baseQuery(args, api, extraOptions);
        if (!result.error || result.error.status !== 401) {
          return result;
        }
        // Retry still returned 401 — fall through to the logout branch.
      } else if (refreshResult.retry) {
        // 503, a 409 that survived its retry, or no answer at all: the server
        // could not evaluate the session, which is not the same as the
        // session being over. Surface the original error and keep the token —
        // the next request will try again. Signing the user out here is the
        // behaviour ADR-0017's 503 exists to prevent. (There is no `raced`
        // arm to write: performRefresh retries a 409 once inside the lock and
        // converts a second one to `retry` before returning, so `raced` never
        // escapes it.)
        return result;
      }
      // A bare `{ ok: false }` — the refresh cookie itself was refused, which
      // IS the session's own death. Both arms above share this exit: drop the
      // stale access token before redirecting.
      api.dispatch(clearAccessToken());
    }

    // If session endpoint returns 401, redirect to login immediately
    if (isSessionEndpoint && navigateToLogin) {
      console.log('🔐 Session endpoint returned 401 - redirecting to login');
      navigateToLogin(currentPath());
      return result; // Return early to avoid showing toast
    }

    // Don't show error toast for auth failures during normal auth checks
    if (!isAuthCheck) {
      toast.error('Session expired. Please log in again.', {
        toastId: 'auth-expired',
        autoClose: 5000
      });
      if (navigateToLogin) {
        navigateToLogin(currentPath());
      }
    }
  }

  // Skip toast for other 4xx client errors (except 401 which is handled above)
  if (
    result.error &&
    Number(result.error.status) >= 400 &&
    Number(result.error.status) < 500
  ) {
    // Don't show toasts for client errors (400-499) - these should be handled by the UI
    // This includes 400 Bad Request, 403 Forbidden, 404 Not Found, etc.
    // Note: 401 is already handled above with specific logic
    return result;
  }

  // Handle server errors with user-friendly messages
  if (result.error && Number(result.error.status) >= 500) {
    toast.error('Server error. Please try again later.', {
      toastId: 'server-error',
      autoClose: 5000
    });
  }

  return result;
};

// Main API slice that other API slices will extend
export const baseApi = createApi({
  reducerPath: 'api',
  baseQuery: baseQueryWithRetry,
  // Global tag types for cache invalidation
  tagTypes: [
    'User',
    'Auth',
    'Navigation',
    'Dashboard',
    'Analytics',
    'Sales',
    'Orders',
    'Projects',
    'Tasks',
    'Events',
    'Chat',
    'Email',
    'Kanban',
    'SupportTicket',
    'Weather',
    'Storage',

    // Admin module management tags
    'Module',
    'ModuleHealth',
    // Service accounts — platform-level admin (ADR-0014 client-credentials)
    'ServiceAccount',
    // First-install onboarding
    'Setup',
    // MFA factors + backup codes
    'MFA',
    // Self-service security center
    'SelfAuthMethods',
    'Sessions',
    'TrustedDevices',
    // Tenant + authz tags
    'Org',
    'Membership',
    'Role',
    'Binding',
    'Permission',
    'EffectivePermissions',
    // Platform-admin tenant management
    'AdminOrg',
    'OrgInvite',
    // Observability — ADR-0005 Phase F runtime log-level mutation
    'LogLevels',
    // Navigation admin — full unfiltered tree + per-parent ordering
    // overrides surfaced at /admin/modules/navigation.
    'NavigationAdmin',
    // OAuth provider availability — the live list /v1/auth/{tier}/providers
    // returns for the unauthenticated login page. Invalidated implicitly by
    // RTK Query's 30s default cache + manual invalidation when the
    // /admin/modules/auth admin tab saves the OAuth Providers toggles.
    'OAuthProviders',
    // Compliance — audit trail + GDPR DSR (ADR-0009)
    'AuditEvent',
    'ErasureRequest',
    'LegalHold'
  ],
  // Keep cache for 5 minutes by default
  keepUnusedDataFor: 300,
  endpoints: () => ({})
});

// Export hooks and utilities
export const {
  util: { getRunningQueriesThunk, getRunningMutationsThunk, invalidateTags }
} = baseApi;

// Helper function to invalidate multiple tags
export const invalidateApiTags = (
  tags: Array<
    | 'User'
    | 'Auth'
    | 'Navigation'
    | 'Dashboard'
    | 'Analytics'
    | 'Sales'
    | 'Orders'
    | 'Projects'
    | 'Tasks'
    | 'Events'
    | 'Chat'
    | 'Email'
    | 'Kanban'
    | 'SupportTicket'
    | 'Weather'
    | 'Storage'
  >
) => {
  return baseApi.util.invalidateTags(tags);
};

// Every cache tag except Auth and Setup. Used by the tenant-impersonation
// switcher + banner to purge per-tenant cached data without blowing away
// the session (Auth) or first-install (Setup) entries. Nuking the session
// cache via baseApi.util.resetApiState() causes a render where sessionData
// is undefined and ProtectedRoute bounces the user to /login before the
// session query has a chance to refetch — see the bug fixed alongside this
// constant.
export const TENANT_SCOPED_TAGS = [
  'User',
  'Navigation',
  'Dashboard',
  'Analytics',
  'Sales',
  'Orders',
  'Projects',
  'Tasks',
  'Events',
  'Chat',
  'Email',
  'Kanban',
  'SupportTicket',
  'Weather',
  'Storage',
  'Module',
  'ModuleHealth',
  'Org',
  'Membership',
  'Role',
  'Binding',
  'Permission',
  'EffectivePermissions',
  'AdminOrg',
  'OrgInvite'
] as const;

// Addon-owned tenant-scoped tags. An addon declares its RTK Query tags in its
// OWN slice (via `baseApi.enhanceEndpoints`) and registers them here so the
// tenant switcher invalidates them on tenant change WITHOUT editing this shared
// file per addon (the addon self-registration seam). Registration fires when
// the addon's slice module is imported. The core-only base registers nothing;
// a fork's slices populate this at import time.
const addonTenantScopedTags = new Set<string>();

export const registerTenantScopedTags = (tags: readonly string[]): void => {
  for (const t of tags) addonTenantScopedTags.add(t);
};

// The exact argument type `baseApi.util.invalidateTags` expects. Addon tags are
// intentionally NOT in baseApi's static TagType union (they live on each addon's
// enhanced api), so we assert the runtime-safe shape here once — RTK ignores
// unknown tags at runtime, and this keeps every call site clean and typed.
type InvalidateArg = Parameters<typeof baseApi.util.invalidateTags>[0];

export const getTenantScopedTags = (): InvalidateArg =>
  [...TENANT_SCOPED_TAGS, ...addonTenantScopedTags] as unknown as InvalidateArg;

export default baseApi;
