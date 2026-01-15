import { baseApi } from './baseApi';

// Backend OAuth types matching the Go models
export type OAuthProvider = 'google' | 'apple' | 'discord' | 'github';

export interface OAuthLink {
  provider: OAuthProvider;
  providerId: string;
  email: string;
  linkedAt: string; // ISO date string
  isActive: boolean;
  isPrimary: boolean;
  lastUsed?: string; // ISO date string
}

export interface OAuthProviderInfo {
  provider: OAuthProvider;
  providerId: string;
  email: string;
  isPrimary: boolean;
  metadata?: Record<string, any>;
  scopes?: string[];
}

// Backend User response matching Go UserResponse exactly
export interface BackendUser {
  id: string;
  email: string;
  username: string;
  fullName: string;
  avatar?: string;
  role: string;
  oauthLinks?: OAuthLink[];
  oauthProviders?: OAuthProviderInfo[];
  isActive: boolean;
  emailVerified: boolean;
  lastLogin?: string; // ISO date string
  createdAt: string; // ISO date string
  updatedAt: string; // ISO date string
}

export interface LogoutResponse {
  success: boolean;
  message?: string;
}


export interface LoginCredentials {
  email: string;
  password: string;
}

export interface LoginResponse {
  success: boolean;
  user: BackendUser;
  message?: string;
}

export interface SessionResponse {
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  user: BackendUser;
  oauthProviders?: OAuthProviderInfo[];
  success: boolean;
}

// Auth API slice
export const authApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Check authentication status - returns backend user data directly
    getCurrentUser: builder.query<BackendUser | null, void>({
      providesTags: ['Auth', 'User'],
      queryFn: async (_arg, _api, _extraOptions, baseQuery) => {
        const result = await baseQuery('api/v1/auth/me');

        // Handle 401/403 as expected unauthenticated state, not an error
        if (result.error && (result.error.status === 401 || result.error.status === 403)) {
          return { data: null };
        }

        // For other errors, return the error
        if (result.error) {
          return { error: result.error };
        }

        // For successful responses, extract user data from body wrapper
        const responseWrapper = result.data as { body: BackendUser };
        const userData = responseWrapper?.body;
        return { data: userData?.isActive ? userData : null };
      },
      // Check auth status frequently
      keepUnusedDataFor: 30, // 30 seconds
    }),

    // User login
    login: builder.mutation<LoginResponse, LoginCredentials>({
      query: (credentials) => ({
        url: 'api/v1/auth/login',
        method: 'POST',
        body: credentials,
      }),
      // Invalidate navigation to fetch role-filtered menu for new user
      invalidatesTags: ['Auth', 'User', 'Navigation'],
    }),

    // User logout
    logout: builder.mutation<LogoutResponse, void>({
      query: () => ({
        url: 'api/v1/auth/logout',
        method: 'POST',
      }),
      // Clear navigation cache on logout
      invalidatesTags: ['Auth', 'User', 'Navigation'],
      onQueryStarted: async (_, { dispatch, queryFulfilled }) => {
        try {
          // Clear access token immediately
          localStorage.removeItem('access_token');

          // Wait for logout to complete
          await queryFulfilled;

          // Clear all RTK Query cache
          dispatch(baseApi.util.resetApiState());

          // Dispatch logout event for other components
          window.dispatchEvent(new CustomEvent('userLogout'));
        } catch (error) {
          console.error('Logout failed:', error);
        }
      },
    }),


    // OAuth endpoints
    initiateOAuth: builder.mutation<{ redirectUrl: string }, { provider: string }>({
      query: ({ provider }) => ({
        url: `api/v1/auth/oauth/${provider}`,
        method: 'POST',
      }),
    }),

    // OAuth callback handling
    handleOAuthCallback: builder.mutation<LoginResponse, { code: string; state?: string; provider: string }>({
      query: ({ code, state, provider }) => ({
        url: `api/v1/auth/oauth/${provider}/callback`,
        method: 'POST',
        body: { code, state },
      }),
      // Invalidate navigation to fetch role-filtered menu for new user
      invalidatesTags: ['Auth', 'User', 'Navigation'],
    }),

    // Get session after OAuth callback - retrieves access token using refresh token from cookie
    getSession: builder.query<SessionResponse | null, void>({
      providesTags: ['Auth'],
      queryFn: async (_arg, _api, _extraOptions, baseQuery) => {
        const result = await baseQuery('api/v1/auth/session');

        // Handle 401/403 as expected unauthenticated state, not an error
        if (result.error && (result.error.status === 401 || result.error.status === 403)) {
          return { data: null };
        }

        // For other errors, return the error
        if (result.error) {
          return { error: result.error };
        }

        // For successful responses, enhance user data with OAuth providers
        const sessionData = result.data as SessionResponse;
        if (sessionData && sessionData.user && sessionData.oauthProviders) {
          // Add OAuth providers to user data for consistency
          sessionData.user.oauthProviders = sessionData.oauthProviders;
        }

        return { data: sessionData };
      },
      // Keep cache for session management
      keepUnusedDataFor: 60, // 1 minute
    }),
  }),
});

// Export hooks
export const {
  useGetCurrentUserQuery,
  useLoginMutation,
  useLogoutMutation,
  useInitiateOAuthMutation,
  useHandleOAuthCallbackMutation,
  useGetSessionQuery,
  // Lazy query hooks for conditional fetching
  useLazyGetCurrentUserQuery,
  useLazyGetSessionQuery,
} = authApi;

// Export endpoints for manual cache management
export const authApiEndpoints = authApi.endpoints;