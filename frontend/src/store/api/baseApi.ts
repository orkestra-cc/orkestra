import { createApi, fetchBaseQuery, BaseQueryFn, FetchArgs, FetchBaseQueryError } from '@reduxjs/toolkit/query/react';
import { toast } from 'react-toastify';
import type { RootState } from '../index';

// Navigation helper - will be set by the auth provider
let navigateToLogin: ((location?: string) => void) | null = null;

export const setNavigateToLogin = (fn: (location?: string) => void) => {
  navigateToLogin = fn;
};

// Enhanced base query with error handling and authentication
const baseQuery = fetchBaseQuery({
  baseUrl: `${import.meta.env.VITE_BACKEND_URL || 'http://localhost:3000'}`,
  credentials: 'include', // Cookie-based authentication only
  prepareHeaders: (headers, { getState }) => {
    // Set standard headers for API requests
    headers.set('Content-Type', 'application/json');

    // Add Bearer token from Redux state if available
    const state = getState() as RootState;
    const accessToken = state.auth?.accessToken;

    if (accessToken) {
      // Check if token is not expired
      const tokenExpiry = state.auth?.tokenExpiry;
      if (tokenExpiry && new Date(tokenExpiry) > new Date()) {
        headers.set('Authorization', `Bearer ${accessToken}`);
      }
    }

    // Note: Also uses HttpOnly cookies for refresh token
    return headers;
  },
});

// Enhanced base query with automatic retry and error handling
const baseQueryWithRetry: BaseQueryFn<
  string | FetchArgs,
  unknown,
  FetchBaseQueryError
> = async (args, api, extraOptions) => {
  let result = await baseQuery(args, api, extraOptions);

  // Handle authentication errors
  if (result.error && result.error.status === 401) {
    // Note: No localStorage cleanup needed - using HttpOnly cookies only

    // Check if this is a session endpoint specifically
    const isSessionEndpoint = typeof args === 'string' && args.includes('api/v1/auth/session');
    const isAuthCheck = typeof args === 'string' && (args.includes('api/v1/auth/v1/me') || args.includes('api/v1/auth/session'));

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
    'Reports',
    // Billing module tags
    'Customer',
    'Supplier',
    'Invoice',
    'Notification',
    'BillingStats',
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
export const invalidateApiTags = (tags: Array<"User" | "Auth" | "Navigation" | "Dashboard" | "Analytics" | "Sales" | "Orders" | "Projects" | "Tasks" | "Events" | "Chat" | "Email" | "Kanban" | "SupportTicket" | "Weather" | "Storage" | "Reports" | "Customer" | "Supplier" | "Invoice" | "Notification" | "BillingStats">) => {
  return baseApi.util.invalidateTags(tags);
};

export default baseApi;