import { baseApi } from './baseApi';
import { setAccessToken } from '../slices/authSlice';

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

export interface PasswordLoginResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  user: BackendUser;
}

export interface RegisterInput {
  email: string;
  password: string;
  fullName: string;
}

export interface RegisterResponse {
  success: boolean;
  userUuid: string;
  message: string;
  requiresVerification: boolean;
}

export interface SimpleMessageResponse {
  success: boolean;
  message: string;
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
  authenticated?: boolean;
  success: boolean;
}

// Auth API slice
export const authApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Check authentication status - returns backend user data directly
    getCurrentUser: builder.query<BackendUser | null, void>({
      providesTags: ['Auth', 'User'],
      queryFn: async (_arg, _api, _extraOptions, baseQuery) => {
        const result = await baseQuery('v1/auth/me');

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

    // Email/password login — returns access token + user
    login: builder.mutation<PasswordLoginResponse, LoginCredentials>({
      query: (credentials) => ({
        url: 'v1/auth/login',
        method: 'POST',
        body: credentials,
      }),
      // Intentionally does NOT invalidate 'Auth'. Invalidating 'Auth' would
      // trigger useGetSessionQuery to immediately refetch /v1/auth/session,
      // which rotates the refresh cookie a SECOND time (login already set a
      // fresh one). That post-login rotation races the cookie application
      // in the browser and trips the family-replay guard. The login response
      // body already contains accessToken + user, so we dispatch them
      // directly from the login callback in useAuthRTK — no session refetch
      // is needed to establish the authenticated state. We still invalidate
      // User + Navigation so role-dependent queries refetch.
      invalidatesTags: ['User', 'Navigation'],
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const result = await queryFulfilled;
          if (result.data?.accessToken && result.data?.expiresIn) {
            dispatch(
              setAccessToken({
                accessToken: result.data.accessToken,
                expiresIn: result.data.expiresIn,
              })
            );
          }
          // Seed the session cache with the login response so subsequent
          // useGetSessionQuery subscribers see authenticated state without
          // another round-trip (which would rotate the cookie again).
          if (result.data?.user) {
            dispatch(
              authApi.util.upsertQueryData('getSession', undefined, {
                accessToken: result.data.accessToken,
                tokenType: result.data.tokenType,
                expiresIn: result.data.expiresIn,
                user: result.data.user,
                success: true,
              } as SessionResponse)
            );
          }
        } catch {
          // login failed — nothing to seed, error surfaces via mutation result
        }
      },
    }),

    // Self-service registration with email/password
    register: builder.mutation<RegisterResponse, RegisterInput>({
      query: (input) => ({
        url: 'v1/auth/register',
        method: 'POST',
        body: input,
      }),
    }),

    // Verify email address with token from link
    verifyEmail: builder.mutation<SimpleMessageResponse, { token: string }>({
      query: (body) => ({
        url: 'v1/auth/verify-email',
        method: 'POST',
        body,
      }),
    }),

    // Resend the verification email
    resendVerification: builder.mutation<SimpleMessageResponse, { email: string }>({
      query: (body) => ({
        url: 'v1/auth/verify-email/resend',
        method: 'POST',
        body,
      }),
    }),

    // Request password reset email
    forgotPassword: builder.mutation<SimpleMessageResponse, { email: string }>({
      query: (body) => ({
        url: 'v1/auth/forgot-password',
        method: 'POST',
        body,
      }),
    }),

    // Consume a password reset token and set a new password
    resetPassword: builder.mutation<SimpleMessageResponse, { token: string; newPassword: string }>({
      query: (body) => ({
        url: 'v1/auth/reset-password',
        method: 'POST',
        body,
      }),
    }),

    // Change password while authenticated
    changePassword: builder.mutation<SimpleMessageResponse, { currentPassword: string; newPassword: string }>({
      query: (body) => ({
        url: 'v1/auth/change-password',
        method: 'POST',
        body,
      }),
    }),

    // User logout
    logout: builder.mutation<LogoutResponse, void>({
      query: () => ({
        url: 'v1/auth/logout',
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
        url: `v1/auth/oauth/${provider}`,
        method: 'POST',
      }),
    }),

    // OAuth callback handling
    handleOAuthCallback: builder.mutation<LoginResponse, { code: string; state?: string; provider: string }>({
      query: ({ code, state, provider }) => ({
        url: `v1/auth/oauth/${provider}/callback`,
        method: 'POST',
        body: { code, state },
      }),
      // Invalidate navigation to fetch role-filtered menu for new user
      invalidatesTags: ['Auth', 'User', 'Navigation'],
    }),

    // Get session after OAuth callback - retrieves access token using refresh token from cookie
    getSession: builder.query<SessionResponse | null, void>({
      providesTags: ['Auth'],
      queryFn: async (_arg, api, _extraOptions, baseQuery) => {
        const result = await baseQuery('v1/auth/session');

        // Handle 401/403 as expected unauthenticated state, not an error.
        // 401 now means "cookie present but refresh rejected" (expired,
        // revoked, replay) — the bootstrap "no cookie" case returns 200
        // with authenticated:false, handled below.
        if (result.error && (result.error.status === 401 || result.error.status === 403)) {
          return { data: null };
        }

        // For other errors, return the error
        if (result.error) {
          return { error: result.error };
        }

        // For successful responses, enhance user data with OAuth providers
        const sessionData = result.data as SessionResponse;

        // Backend returns 200 + authenticated:false when no refresh cookie
        // is present (fresh browser, post-logout). Surface this as a null
        // session so subscribers see the same unauthenticated state the
        // 401 path produced before.
        if (sessionData && sessionData.authenticated === false) {
          return { data: null };
        }

        if (sessionData && sessionData.user && sessionData.oauthProviders) {
          // Add OAuth providers to user data for consistency
          sessionData.user.oauthProviders = sessionData.oauthProviders;
        }

        // Dispatch setAccessToken BEFORE returning so dependent queries
        // (useListMyOrgsQuery, useGetNavigationQuery, etc.) that unskip
        // the moment isAuthenticated flips true include the Authorization
        // header in their very first request. Doing it here — rather than
        // in a useEffect in useAuthRTK that runs after render — avoids a
        // page-load race: without the token in Redux, those queries fire
        // with no auth header, the backend's inline refresh-cookie
        // rotation races across the concurrent middleware invocations, and
        // the CAS-loss branch trips the family-replay guard. That revokes
        // the entire session and bounces the user to /login on every
        // page refresh.
        if (sessionData && sessionData.accessToken) {
          api.dispatch(
            setAccessToken({
              accessToken: sessionData.accessToken,
              expiresIn: sessionData.expiresIn,
            })
          );
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
  useRegisterMutation,
  useVerifyEmailMutation,
  useResendVerificationMutation,
  useForgotPasswordMutation,
  useResetPasswordMutation,
  useChangePasswordMutation,
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