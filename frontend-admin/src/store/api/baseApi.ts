import {
  createApi,
  fetchBaseQuery,
  BaseQueryFn,
  FetchArgs,
  FetchBaseQueryError
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
type RefreshResult =
  | { ok: true; accessToken: string; expiresIn: number }
  | { ok: false };
let inFlightRefresh: Promise<RefreshResult> | null = null;

async function performRefresh(baseUrl: string): Promise<RefreshResult> {
  if (inFlightRefresh) return inFlightRefresh;
  inFlightRefresh = (async () => {
    try {
      const res = await fetch(`${baseUrl}/v1/auth/operator/refresh-cookie`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' }
      });
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
const baseQueryWithRetry: BaseQueryFn<
  string | FetchArgs,
  unknown,
  FetchBaseQueryError
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

  let result = await baseQuery(args, api, extraOptions);

  // Handle authentication errors
  if (result.error && result.error.status === 401) {
    // Note: No localStorage cleanup needed - using HttpOnly cookies only

    const requestUrl = typeof args === 'string' ? args : args.url;
    const isSessionEndpoint = requestUrl.includes('v1/auth/session');
    const isAuthCheck =
      requestUrl.includes('v1/auth/operator/me') ||
      requestUrl.includes('v1/auth/session');

    // Server-side session revocation (logout, admin-kill, password change)
    // sets `code: "session_revoked"` on the 401 body. Skip the silent-refresh
    // retry in that case — a new access token minted from the same refresh
    // cookie would carry the same revoked sid and just fail again. Clear
    // local state and bounce the user to /login with a specific message.
    const errorData = (result.error as { data?: { code?: string } }).data;
    if (errorData?.code === 'session_revoked') {
      api.dispatch(clearAccessToken());
      if (!isAuthCheck) {
        toast.error('Your session has been revoked. Please sign in again.', {
          toastId: 'session-revoked',
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

    // Billing module tags
    'Customer',
    'Supplier',
    'Company',
    'Invoice',
    'Notification',
    'BillingStats',
    'BusinessRegistry',
    // Company lookup module tags
    'CompanyLookup',
    // Documents module tags
    'DocumentTemplate',
    'GeneratedDocument',
    // Graph database module tags
    'GraphQuery',
    'GraphSchema',
    'VectorIndex',
    // RAG module tags
    'RagModel',
    'RagDocument',
    'RagRelationship',
    // AI Models module tags
    'AIModel',
    // Agents module tags
    'AgentProject',
    'AgentConversation',
    // Personal Agent tags
    'PersonalAgent',
    'PersonalConversation',
    // Admin module management tags
    'Module',
    'ModuleHealth',
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
    // Subscriptions module
    'SubscriptionService',
    'Subscription',
    'SubscriptionInvoice',
    'SubscriptionActivity',
    // Payments module
    'PaymentTransaction',
    'PaymentMethodRec',
    'PaymentWebhookEvent',
    // Compliance module
    'AuditEvent',
    'Soc2Evidence',
    // Identity module
    'IdentityIdP',
    'IdentityScim',
    // Observability — ADR-0005 Phase F runtime log-level mutation
    'LogLevels',
    // Marketing module — Phase 1 contact base + importer surface
    'MarketingOrg',
    'MarketingPerson',
    'MarketingMembership',
    'MarketingTag',
    'MarketingCustomFieldSchema',
    'MarketingImport',
    // Marketing module — Phase 2 activity log + scoring surfaces.
    // ScoreSnapshot tags are keyed two ways: by uuid for single-row
    // reads from the breakdown drawer, and by `person:<uuid>` /
    // `profile:<uuid>` for the per-person and per-profile listings
    // so a profile edit invalidates the right leaderboard rows.
    'MarketingActivity',
    'MarketingScoreProfile',
    'MarketingScoreSnapshot',
    // Marketing module — Phase 3 conflict-review queue. Reviews are
    // queried both by uuid (resolver modal) and by importJobUuid
    // (imports-list deep link), so the slice tags both ways.
    'MarketingConflictReview'
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
    | 'Customer'
    | 'Supplier'
    | 'Company'
    | 'Invoice'
    | 'Notification'
    | 'BillingStats'
    | 'BusinessRegistry'
    | 'CompanyLookup'
    | 'DocumentTemplate'
    | 'GeneratedDocument'
    | 'GraphQuery'
    | 'GraphSchema'
    | 'VectorIndex'
    | 'RagModel'
    | 'RagDocument'
    | 'RagRelationship'
    | 'AIModel'
    | 'AgentProject'
    | 'AgentConversation'
    | 'PersonalAgent'
    | 'PersonalConversation'
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
  'Customer',
  'Supplier',
  'Company',
  'Invoice',
  'Notification',
  'BillingStats',
  'BusinessRegistry',
  'CompanyLookup',
  'DocumentTemplate',
  'GeneratedDocument',
  'GraphQuery',
  'GraphSchema',
  'VectorIndex',
  'RagModel',
  'RagDocument',
  'RagRelationship',
  'AIModel',
  'AgentProject',
  'AgentConversation',
  'PersonalAgent',
  'PersonalConversation',
  'Module',
  'ModuleHealth',
  'Org',
  'Membership',
  'Role',
  'Binding',
  'Permission',
  'EffectivePermissions',
  'AdminOrg',
  'OrgInvite',
  'SubscriptionService',
  'Subscription',
  'SubscriptionInvoice',
  'SubscriptionActivity',
  'PaymentTransaction',
  'PaymentMethodRec',
  'PaymentWebhookEvent',
  'AuditEvent',
  'Soc2Evidence',
  'IdentityIdP',
  'IdentityScim',
  'MarketingOrg',
  'MarketingPerson',
  'MarketingMembership',
  'MarketingTag',
  'MarketingCustomFieldSchema',
  'MarketingImport',
  'MarketingActivity',
  'MarketingScoreProfile',
  'MarketingScoreSnapshot',
  'MarketingConflictReview'
] as const;

export default baseApi;
