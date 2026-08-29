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
import runtimeConfig from 'config/environment';

// Navigation helper - will be set by the auth provider
let navigateToLogin: ((location?: string) => void) | null = null;

export const setNavigateToLogin = (fn: (location?: string) => void) => {
  navigateToLogin = fn;
};

// Endpoints for which a 401 must NOT trigger a silent refresh attempt —
// either because they *are* the refresh/login/logout endpoints (retrying
// would loop) or because a 401 here already means "user is not signed in"
// and the correct UX is to fall through to the caller. ADR-0003 PR-D D-8
// dropped the legacy un-prefixed paths; this dashboard targets the
// operator tier, so all entries are mounted under /v1/auth/operator.
const AUTH_ENDPOINT_PATHS = [
  'v1/auth/operator/login',
  'v1/auth/operator/logout',
  'v1/auth/operator/refresh',
  'v1/auth/operator/refresh-cookie',
  'v1/auth/operator/register',
  'v1/auth/operator/mfa/login/verify'
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
// `retry` is NOT the same answer as `ok: false`. A 503 from the refresh
// endpoint means the backend could not *evaluate* the session — the session
// enforcement path's durable store was unreachable — and ADR-0017 gives that
// its own status precisely so a client does not treat it as a sign-out: an
// outage "would train clients to discard a session that is still perfectly
// valid." Collapsing it into `ok: false`, as this did, logged the user out
// for the exact reason the 503 exists to prevent. A 409
// refresh_rotation_raced carries the same "do not sign out" meaning.
type RefreshResult =
  | { ok: true; accessToken: string; expiresIn: number }
  | { ok: false; retry?: boolean; raced?: boolean };
let inFlightRefresh: Promise<RefreshResult> | null = null;

const REFRESH_LOCK_NAME = 'orkestra:auth-refresh';

// Web Locks is the only cross-tab primitive that releases automatically
// when the holder navigates away or crashes, which a localStorage mutex
// cannot promise. Where it is missing (non-secure context, jsdom under
// test) we fall back to running unguarded: the backend's rotation grace
// window still keeps a lost race from ending the session.
async function withRefreshLock<T>(run: () => Promise<T>): Promise<T> {
  const locks = typeof navigator !== 'undefined' ? navigator.locks : undefined;
  if (!locks?.request) return run();
  // LockGrantedCallback<T> is declared as returning T, so T infers here as
  // Promise<RefreshResult>; the await unwraps both layers.
  return await locks.request(REFRESH_LOCK_NAME, run);
}

async function refreshOnce(baseUrl: string): Promise<RefreshResult> {
  try {
    const res = await fetch(`${baseUrl}/v1/auth/operator/refresh-cookie`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' }
    });
    // 503 session_enforcement_unavailable: transient, keep the token.
    if (res.status === 503) return { ok: false, retry: true } as const;
    // 409 refresh_rotation_raced: a sibling rotated first and the family
    // is intact. Our cookie jar already holds its successor.
    if (res.status === 409) return { ok: false, raced: true } as const;
    if (!res.ok) return { ok: false } as const;
    const body = (await res.json()) as {
      accessToken?: string;
      expiresIn?: number;
    };
    if (!body.accessToken || !body.expiresIn) return { ok: false } as const;
    return {
      ok: true,
      accessToken: body.accessToken,
      expiresIn: body.expiresIn
    } as const;
  } catch {
    return { ok: false } as const;
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

// Base fetch with cookies + Bearer token. Tenant context (X-Tenant-ID) is
// injected by baseQueryWithRetry below, where we have access to the request
// args and can decide whether the endpoint is tenant-scoped.
//
// ADR-0003 PR-C: the operator dashboard targets the operator host
// (`console.*`). The default below uses `console.localhost:3000` so a
// fresh checkout boots against the operator mux directly; setups that
// can't resolve `*.localhost` fall back to the host-mux's dev
// fallthrough by setting VITE_BACKEND_URL=http://localhost:3000.
const baseQuery = fetchBaseQuery({
  baseUrl: runtimeConfig.apiUrl,
  credentials: 'include',
  prepareHeaders: (headers, { getState }) => {
    headers.set('Content-Type', 'application/json');

    const state = getState() as RootState;
    const accessToken = state.auth?.accessToken;

    if (accessToken) {
      const tokenExpiry = state.auth?.tokenExpiry;
      if (tokenExpiry && new Date(tokenExpiry) > new Date()) {
        headers.set('Authorization', `Bearer ${accessToken}`);
      }
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
  // its own, and refreshing ahead of /refresh-cookie would recurse. Any
  // outcome other than `ok` falls through untouched — the request goes out
  // as-is and the 401 branch below stays the single owner of the sign-out
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
        navigateToLogin(window.location.pathname);
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
      const refreshResult = await performRefresh(runtimeConfig.apiUrl);
      if (refreshResult.ok) {
        api.dispatch(
          setAccessToken({
            accessToken: refreshResult.accessToken,
            expiresIn: refreshResult.expiresIn
          })
        );
        result = await baseQuery(args, api, extraOptions);
        if (!result.error || result.error.status !== 401) {
          return result;
        }
        // Retry still returned 401 — fall through to the logout branch.
      } else if (refreshResult.retry) {
        // 503: the server could not evaluate the session, which is not the
        // same as the session being over. Surface the original error and
        // keep the token — the next request will try again. Signing the
        // user out here is the behaviour ADR-0017's 503 exists to prevent.
        return result;
      }
      // Refresh itself failed: drop the stale access token before redirecting.
      api.dispatch(clearAccessToken());
    }

    // If session endpoint returns 401, redirect to login immediately
    if (isSessionEndpoint && navigateToLogin) {
      console.log('🔐 Session endpoint returned 401 - redirecting to login');
      navigateToLogin(window.location.pathname);
      return result; // Return early to avoid showing toast
    }

    // Don't show error toast for auth failures during normal auth checks
    if (!isAuthCheck) {
      toast.error('Session expired. Please log in again.', {
        toastId: 'auth-expired',
        autoClose: 5000
      });
      if (navigateToLogin) {
        navigateToLogin(window.location.pathname);
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
