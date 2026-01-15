/**
 * API Client with HttpOnly cookie-based authentication
 *
 * This client uses HttpOnly cookies exclusively for authentication,
 * eliminating XSS vulnerabilities from localStorage token storage.
 */

import { useEffect } from 'react';

// Types for our API responses
export interface ApiResponse<T = any> {
  data: T;
  status: number;
  headers: Headers;
  refreshed: boolean; // Indicates if the session was refreshed during this request
}

export interface ApiError extends Error {
  status?: number;
  data?: any;
}

// Configuration
const getBackendUrl = () => (import.meta as any).env?.VITE_BACKEND_URL || 'http://localhost:3000';

// Track if user is logging out to prevent refresh attempts
let isLoggingOut = false;

// Listen for logout events
if (typeof window !== 'undefined') {
  window.addEventListener('userLogout', () => {
    isLoggingOut = true;
    // Reset after a short delay to handle async operations
    setTimeout(() => { isLoggingOut = false; }, 2000);
  }, { passive: true });
}

/**
 * Attempts to refresh the session using HttpOnly cookies
 */
async function refreshSession(): Promise<boolean> {
  // Don't attempt refresh if user is logging out
  if (isLoggingOut) {
    console.log('🚫 [SESSION_REFRESH] Skipping refresh - user is logging out');
    return false;
  }

  try {
    const backendUrl = getBackendUrl();
    console.log('🔄 [SESSION_REFRESH] Attempting to refresh session...');

    const response = await fetch(`${backendUrl}/api/v1/auth/refresh`, {
      method: 'POST',
      credentials: 'include', // Use HttpOnly cookies exclusively
      headers: {
        'Content-Type': 'application/json',
      },
    });

    console.log(`🔄 [SESSION_REFRESH] Refresh response status: ${response.status}`);

    if (response.ok) {
      console.log('✅ [SESSION_REFRESH] Successfully refreshed session');

      // Dispatch custom event for other parts of the app to know about session refresh
      window.dispatchEvent(new CustomEvent('sessionRefreshed'));
      return true;
    } else {
      console.log(`❌ [SESSION_REFRESH] Session refresh failed with status: ${response.status}`);
      // If refresh fails, dispatch session expired event
      window.dispatchEvent(new CustomEvent('sessionExpired'));
      return false;
    }
  } catch (error) {
    console.log('❌ [SESSION_REFRESH] Session refresh error:', error);
    // Dispatch session expired event on network/other errors
    window.dispatchEvent(new CustomEvent('sessionExpired'));
    return false;
  }
}

/**
 * Enhanced fetch wrapper that handles automatic session refresh on 401 responses
 */
export async function apiClient<T = any>(
  endpoint: string,
  options: RequestInit = {}
): Promise<ApiResponse<T>> {
  const backendUrl = getBackendUrl();
  const url = `${backendUrl}${endpoint.startsWith('/') ? endpoint : '/' + endpoint}`;

  console.log(`[API_CLIENT] Request to ${endpoint} using HttpOnly cookie authentication`);

  // Prepare headers - no Authorization header needed, using cookies exclusively
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...options.headers,
  };

  // Always use cookies for authentication - no Bearer tokens
  options.credentials = 'include';
  console.log(`[API_CLIENT] Using HttpOnly cookie authentication exclusively`);

  const makeRequest = async (): Promise<Response> => {
    return await fetch(url, {
      ...options,
      headers,
      credentials: 'include', // Always use cookies
    });
  };

  try {
    let response = await makeRequest();
    let retryAttempted = false;

    // Handle 401 responses with automatic session refresh (unless logging out)
    if (response.status === 401 && !retryAttempted && !isLoggingOut) {
      console.log('🔄 [API_CLIENT] 401 response detected, attempting session refresh...');
      retryAttempted = true;

      const refreshSuccess = await refreshSession();
      if (refreshSuccess) {
        console.log('🔄 [API_CLIENT] Session refreshed, retrying original request...');
        response = await makeRequest();
      } else {
        console.log('❌ [API_CLIENT] Session refresh failed, keeping 401 response');
        // Keep the original 401 response to handle appropriately
      }
    } else if (response.status === 401 && isLoggingOut) {
      console.log('🚫 [API_CLIENT] 401 response during logout - skipping refresh');
    }

    // Check for session refresh headers (cookies are managed automatically by browser)
    let sessionRefreshed = false;
    const sessionRefreshedHeader = response.headers.get('X-Session-Refreshed');

    console.log(`[API_CLIENT] Response status: ${response.status}`);
    console.log(`[API_CLIENT] Session refreshed: ${sessionRefreshedHeader || 'no'}`);

    if (sessionRefreshedHeader === 'true' || sessionRefreshedHeader === '1') {
      console.log('🔄 [API_CLIENT] Session was refreshed during this request');
      sessionRefreshed = true;

      // Dispatch custom event for other parts of the app to know about session refresh
      window.dispatchEvent(new CustomEvent('sessionRefreshed'));
      console.log('🔄 [API_CLIENT] sessionRefreshed event dispatched');
    } else if (!retryAttempted) {
      console.log('[API_CLIENT] No session refresh occurred');
    }

    // Parse response data
    let data: T;
    const contentType = response.headers.get('content-type');

    if (contentType && contentType.includes('application/json')) {
      data = await response.json();
    } else {
      data = (await response.text()) as any;
    }

    // Handle non-2xx responses
    if (!response.ok) {
      const error = new Error(`HTTP ${response.status}: ${response.statusText}`) as ApiError;
      error.status = response.status;
      error.data = data;
      throw error;
    }

    return {
      data,
      status: response.status,
      headers: response.headers,
      refreshed: sessionRefreshed || retryAttempted,
    };

  } catch (error) {
    // Re-throw ApiError instances
    if (error instanceof Error && 'status' in error) {
      throw error;
    }

    // Wrap other errors
    const apiError = new Error(`Network error: ${error instanceof Error ? error.message : 'Unknown error'}`) as ApiError;
    apiError.status = 0; // Network error
    throw apiError;
  }
}

/**
 * Convenience methods for common HTTP verbs
 */
export const api = {
  get: <T = any>(endpoint: string, options?: RequestInit): Promise<ApiResponse<T>> =>
    apiClient<T>(endpoint, { ...options, method: 'GET' }),

  post: <T = any>(endpoint: string, data?: any, options?: RequestInit): Promise<ApiResponse<T>> =>
    apiClient<T>(endpoint, {
      ...options,
      method: 'POST',
      body: data ? JSON.stringify(data) : undefined,
    }),

  put: <T = any>(endpoint: string, data?: any, options?: RequestInit): Promise<ApiResponse<T>> =>
    apiClient<T>(endpoint, {
      ...options,
      method: 'PUT',
      body: data ? JSON.stringify(data) : undefined,
    }),

  patch: <T = any>(endpoint: string, data?: any, options?: RequestInit): Promise<ApiResponse<T>> =>
    apiClient<T>(endpoint, {
      ...options,
      method: 'PATCH',
      body: data ? JSON.stringify(data) : undefined,
    }),

  delete: <T = any>(endpoint: string, options?: RequestInit): Promise<ApiResponse<T>> =>
    apiClient<T>(endpoint, { ...options, method: 'DELETE' }),
};

/**
 * Hook to listen for session refresh and expiration events
 */
export function useSessionListener(
  onSessionRefreshed?: () => void,
  onSessionExpired?: () => void
) {
  useEffect(() => {
    const handleSessionRefresh = () => {
      onSessionRefreshed?.();
    };

    const handleSessionExpired = () => {
      onSessionExpired?.();
    };

    window.addEventListener('sessionRefreshed', handleSessionRefresh, { passive: true });
    window.addEventListener('sessionExpired', handleSessionExpired, { passive: true });

    return () => {
      window.removeEventListener('sessionRefreshed', handleSessionRefresh);
      window.removeEventListener('sessionExpired', handleSessionExpired);
    };
  }, [onSessionRefreshed, onSessionExpired]);
}

/**
 * Legacy compatibility hooks - deprecated, use useSessionListener instead
 */
export function useTokenRefreshListener(
  onTokenRefreshed?: (newToken: string) => void,
  onTokenExpired?: () => void
) {
  console.warn('useTokenRefreshListener is deprecated, use useSessionListener instead');
  return useSessionListener(
    () => onTokenRefreshed?.(''), // Empty string for legacy compatibility
    onTokenExpired
  );
}

export function useTokenRefreshListenerLegacy(callback: (newToken: string) => void) {
  console.warn('useTokenRefreshListenerLegacy is deprecated, use useSessionListener instead');
  return useSessionListener(() => callback(''));
}