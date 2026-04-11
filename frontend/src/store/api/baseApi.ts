import { createApi, fetchBaseQuery, BaseQueryFn, FetchArgs, FetchBaseQueryError } from '@reduxjs/toolkit/query/react';
import { toast } from 'react-toastify';
import type { RootState } from '../index';

// Navigation helper - will be set by the auth provider
let navigateToLogin: ((location?: string) => void) | null = null;

export const setNavigateToLogin = (fn: (location?: string) => void) => {
  navigateToLogin = fn;
};

// Endpoints that must NOT carry X-Org-ID because they run before the current
// org is known (login, refresh, org listing, org creation, invite accept),
// or because they are platform-level (module admin, first-install setup)
// and the backend's org-resolution middleware would reject a stray header.
const ORG_AGNOSTIC_PATHS = [
  '/v1/auth/',
  '/v1/orgs',                 // GET list, POST create
  '/v1/orgs/accept-invite',
  '/v1/notifications/preferences',
  '/v1/admin/modules',        // platform-level module admin, not per-org
  '/v1/setup',                // first-install wizard endpoints
];

function isOrgAgnostic(url: string): boolean {
  // Exact-match /v1/orgs (listing/creation) but pass through for /v1/orgs/{orgId}/...
  if (url === '/v1/orgs' || url.startsWith('/v1/orgs?')) return true;
  if (url === '/v1/orgs/accept-invite') return true;
  return ORG_AGNOSTIC_PATHS.some((p) => p !== '/v1/orgs' && url.startsWith(p));
}

// Base fetch with cookies + Bearer token. Tenant context (X-Org-ID) is
// injected by baseQueryWithRetry below, where we have access to the request
// args and can decide whether the endpoint is org-scoped.
const baseQuery = fetchBaseQuery({
  baseUrl: `${import.meta.env.VITE_BACKEND_URL || 'http://localhost:3000'}`,
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
  },
});

// Enhanced base query with automatic retry, error handling, and tenant context.
const baseQueryWithRetry: BaseQueryFn<
  string | FetchArgs,
  unknown,
  FetchBaseQueryError
> = async (args, api, extraOptions) => {
  // Inject X-Org-ID for every tenant-scoped request. The backend validates
  // the header against the caller's JWT memberships and rejects mismatches.
  const state = api.getState() as RootState;
  const currentOrgId = state.tenant?.currentOrgId;
  if (currentOrgId) {
    const url = typeof args === 'string' ? args : args.url;
    if (!isOrgAgnostic(url)) {
      const merged: FetchArgs = typeof args === 'string'
        ? { url: args, headers: { 'X-Org-ID': currentOrgId } }
        : { ...args, headers: { ...(args.headers as Record<string, string> | undefined), 'X-Org-ID': currentOrgId } };
      args = merged;
    }
  }

  let result = await baseQuery(args, api, extraOptions);

  // Handle authentication errors
  if (result.error && result.error.status === 401) {
    // Note: No localStorage cleanup needed - using HttpOnly cookies only

    // Check if this is a session endpoint specifically
    const isSessionEndpoint = typeof args === 'string' && args.includes('v1/auth/session');
    const isAuthCheck = typeof args === 'string' && (args.includes('v1/auth/me') || args.includes('v1/auth/session'));

    // On a fresh install the session endpoint legitimately returns 401
    // because no user exists yet. The SetupGate should be steering the
    // browser to /setup, not /login. Suppress the forced login redirect
    // and the toast while the setup wizard is active or while we have
    // not yet confirmed setupCompleted === true.
    const isOnSetupPath =
      typeof window !== 'undefined' && window.location.pathname.startsWith('/setup');
    const apiState = (api.getState() as { api?: { queries?: Record<string, unknown> } }).api;
    const setupQueryEntry = Object.values(apiState?.queries ?? {}).find((q) => {
      return (q as { endpointName?: string } | null)?.endpointName === 'getSetupStatus';
    }) as { data?: { setupCompleted?: boolean } } | undefined;
    const setupCompleted = setupQueryEntry?.data?.setupCompleted === true;

    if (isOnSetupPath || !setupCompleted) {
      // First-install mode: never interrupt with a login redirect.
      // Return the 401 as-is so callers can still handle it (SetupGate's
      // setup-status query path itself is unauthenticated and returns 200).
      return result;
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
        autoClose: 5000,
      });
    }
  }

  // Skip toast for other 4xx client errors (except 401 which is handled above)
  if (result.error && Number(result.error.status) >= 400 && Number(result.error.status) < 500) {
    // Don't show toasts for client errors (400-499) - these should be handled by the UI
    // This includes 400 Bad Request, 403 Forbidden, 404 Not Found, etc.
    // Note: 401 is already handled above with specific logic
    return result;
  }

  // Handle server errors with user-friendly messages
  if (result.error && Number(result.error.status) >= 500) {
    toast.error('Server error. Please try again later.', {
      toastId: 'server-error',
      autoClose: 5000,
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
    // First-install onboarding
    'Setup',
    // Tenant + authz tags
    'Org',
    'Membership',
    'Role',
    'Binding',
    'Permission',
    'EffectivePermissions',
  ],
  // Keep cache for 5 minutes by default
  keepUnusedDataFor: 300,
  endpoints: () => ({}),
});

// Export hooks and utilities
export const {
  util: {
    getRunningQueriesThunk,
    getRunningMutationsThunk,
    invalidateTags
  }
} = baseApi;

// Helper function to invalidate multiple tags
export const invalidateApiTags = (tags: Array<"User" | "Auth" | "Navigation" | "Dashboard" | "Analytics" | "Sales" | "Orders" | "Projects" | "Tasks" | "Events" | "Chat" | "Email" | "Kanban" | "SupportTicket" | "Weather" | "Storage" | "Customer" | "Supplier" | "Company" | "Invoice" | "Notification" | "BillingStats" | "BusinessRegistry" | "CompanyLookup" | "DocumentTemplate" | "GeneratedDocument" | "GraphQuery" | "GraphSchema" | "VectorIndex" | "RagModel" | "RagDocument" | "RagRelationship" | "AIModel" | "AgentProject" | "AgentConversation" | "PersonalAgent" | "PersonalConversation">) => {
  return baseApi.util.invalidateTags(tags);
};

export default baseApi;