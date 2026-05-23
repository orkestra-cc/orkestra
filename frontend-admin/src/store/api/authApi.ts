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
  // BCP-47 language tag. Backend backfills 'en' for accounts that
  // predate the field; useLanguageSync drives i18n.changeLanguage off
  // this on login.
  language?: string;
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
  // accessToken/tokenType/expiresIn are absent on MFA partial responses.
  accessToken?: string;
  tokenType?: string;
  expiresIn?: number;
  user?: BackendUser;
  // Populated only when the account has an enrolled second factor. The
  // caller must POST challengeId+code to /v1/auth/operator/mfa/login/verify to
  // complete the flow — no session cookies are set until then.
  requiresMfa?: boolean;
  mfaToken?: string;
  // True when the user has at least one enrolled passkey alongside (or
  // instead of) TOTP. Drives the "Use a passkey" button on /mfa/verify.
  webauthnAvailable?: boolean;
  // Populated when the account's role requires MFA but none is enrolled.
  // The caller receives a full token (grace window) but must enroll before
  // mfaGraceExpiresAt.
  mfaEnrollmentRequired?: boolean;
  mfaGraceExpiresAt?: string | null;
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

// Public auth-policy slice — exposes the admin-managed flags the
// unauthenticated login + signup pages need before the user types
// anything. Audience is implicit in the route prefix.
export interface AuthPolicy {
  registrationEnabled: boolean;
  loginEnabled: boolean;
  passwordMinLength: number;
}

// --- Self-service security center ---
// Mirrors authModels.AuthMethodsView and SessionInfo from the Go
// backend. Reused by /user/security so the page can drive every tab
// from a single fetch.

export interface SelfAuthWebAuthnCredential {
  credentialId: string; // base64url, no padding
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

export interface SelfAuthMfaFactor {
  type: 'totp' | 'webauthn';
  enrolledAt?: string;
  lastUsedAt?: string;
  backupCodesRemaining?: number;
  credentials?: SelfAuthWebAuthnCredential[];
}

export interface SelfAuthOAuthProvider {
  provider: OAuthProvider;
  email: string;
  linkedAt: string;
  lastUsedAt?: string;
  isPrimary: boolean;
}

export interface SelfAuthMethods {
  hasUsablePassword: boolean;
  passwordUpdatedAt?: string;
  emailVerified: boolean;
  lastLoginAt?: string;
  mfaRequired: boolean;
  mfaGraceStartedAt?: string;
  mfaGraceExpiresAt?: string;
  mfaFactors: SelfAuthMfaFactor[];
  oauthProviders: SelfAuthOAuthProvider[];
}

export interface SelfSessionInfo {
  sessionId: string;
  deviceId: string;
  deviceName: string;
  deviceType: string;
  platform: string;
  ipAddress: string;
  lastActivity: string;
  createdAt: string;
  expiresAt: string;
  isCurrent: boolean;
  riskScore?: number;
}

export interface SelfSessionsResponse {
  sessions: SelfSessionInfo[];
  activeCount: number;
  maxSessions?: number;
  currentDevice?: string;
}

// Auth API slice
export const authApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    // Public auth policy — read by the login + register pages before
    // the user types anything so a maintenance kill switch hides the
    // CTA instead of making the user discover it via a 403.
    getAuthPolicy: builder.query<AuthPolicy, void>({
      queryFn: async (_arg, _api, _extraOptions, baseQuery) => {
        const result = await baseQuery('v1/auth/operator/policy');
        if (result.error) {
          // Network failure / 404 → assume "everything enabled" so a
          // misconfigured deployment doesn't block legitimate users.
          // The backend re-validates on submit anyway.
          return {
            data: {
              registrationEnabled: true,
              loginEnabled: true,
              passwordMinLength: 10
            }
          };
        }
        const wrapper = result.data as AuthPolicy;
        return { data: wrapper };
      },
      keepUnusedDataFor: 30
    }),

    // Check authentication status - returns backend user data directly
    getCurrentUser: builder.query<BackendUser | null, void>({
      providesTags: ['Auth', 'User'],
      queryFn: async (_arg, _api, _extraOptions, baseQuery) => {
        const result = await baseQuery('v1/auth/operator/me');

        // Handle 401/403 as expected unauthenticated state, not an error
        if (
          result.error &&
          (result.error.status === 401 || result.error.status === 403)
        ) {
          return { data: null };
        }

        // For other errors, return the error
        if (result.error) {
          return { error: result.error };
        }

        // Huma serializes the Body field's value as the response body
        // directly — there is no outer { body: ... } wrapper.
        const userData = result.data as BackendUser | undefined;
        return { data: userData?.isActive ? userData : null };
      },
      // Check auth status frequently
      keepUnusedDataFor: 30 // 30 seconds
    }),

    // Self-service preference patch — currently only `language`. Backend
    // mirrors GET /me on success so the SPA can replace its cached user
    // doc with the response. We seed both authApi caches (`getCurrentUser`
    // + `getSession`) so subscribers re-render against the new value
    // without an extra round-trip; the failure path is the caller's
    // problem (LanguageSettings reverts i18n + cookie + toasts).
    updateCurrentUser: builder.mutation<BackendUser, { language?: string }>({
      query: body => ({
        url: 'v1/auth/operator/me',
        method: 'PATCH',
        body
      }),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const { data: user } = await queryFulfilled;
          dispatch(
            authApi.util.updateQueryData('getCurrentUser', undefined, draft =>
              draft ? Object.assign(draft, user) : user
            )
          );
          dispatch(
            authApi.util.updateQueryData('getSession', undefined, draft => {
              if (draft && draft.user) {
                Object.assign(draft.user, user);
              }
            })
          );
        } catch {
          // Surfaced to the caller via the mutation result.
        }
      }
    }),

    // Email/password login — returns access token + user
    login: builder.mutation<PasswordLoginResponse, LoginCredentials>({
      query: credentials => ({
        url: 'v1/auth/operator/login',
        method: 'POST',
        body: credentials
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
          // MFA partial response: no tokens minted, caller routes to
          // /mfa/verify with the challenge id. Don't seed auth state.
          if (result.data?.requiresMfa) {
            return;
          }
          if (result.data?.accessToken && result.data?.expiresIn) {
            dispatch(
              setAccessToken({
                accessToken: result.data.accessToken,
                expiresIn: result.data.expiresIn
              })
            );
          }
          // Seed the session cache with the login response so subsequent
          // useGetSessionQuery subscribers see authenticated state without
          // another round-trip (which would rotate the cookie again).
          if (result.data?.user && result.data?.accessToken) {
            dispatch(
              authApi.util.upsertQueryData('getSession', undefined, {
                accessToken: result.data.accessToken,
                tokenType: result.data.tokenType ?? 'Bearer',
                expiresIn: result.data.expiresIn ?? 0,
                user: result.data.user,
                success: true
              } as SessionResponse)
            );
          }
        } catch {
          // login failed — nothing to seed, error surfaces via mutation result
        }
      }
    }),

    // Self-service registration with email/password
    register: builder.mutation<RegisterResponse, RegisterInput>({
      query: input => ({
        url: 'v1/auth/operator/register',
        method: 'POST',
        body: input
      })
    }),

    // Verify email address with token from link
    verifyEmail: builder.mutation<SimpleMessageResponse, { token: string }>({
      query: body => ({
        url: 'v1/auth/operator/verify-email',
        method: 'POST',
        body
      })
    }),

    // Resend the verification email
    resendVerification: builder.mutation<
      SimpleMessageResponse,
      { email: string }
    >({
      query: body => ({
        url: 'v1/auth/operator/verify-email/resend',
        method: 'POST',
        body
      })
    }),

    // Request password reset email
    forgotPassword: builder.mutation<SimpleMessageResponse, { email: string }>({
      query: body => ({
        url: 'v1/auth/operator/forgot-password',
        method: 'POST',
        body
      })
    }),

    // Consume a password reset token and set a new password
    resetPassword: builder.mutation<
      SimpleMessageResponse,
      { token: string; newPassword: string }
    >({
      query: body => ({
        url: 'v1/auth/operator/reset-password',
        method: 'POST',
        body
      })
    }),

    // Change password while authenticated
    changePassword: builder.mutation<
      SimpleMessageResponse,
      { currentPassword: string; newPassword: string }
    >({
      query: body => ({
        url: 'v1/auth/operator/change-password',
        method: 'POST',
        body
      })
    }),

    // Password reconfirm — the step-up bypass for users with no MFA
    // factor enrolled. Backend mints an access token with
    // amr += "reauth" + last_otp_at = now so the next destructive
    // request passes RequireStepUp. The fresh token is dispatched into
    // Redux on success so the in-flight replay carries it.
    confirmPassword: builder.mutation<
      {
        success: boolean;
        accessToken: string;
        tokenType: string;
        expiresIn: number;
      },
      { password: string }
    >({
      query: body => ({
        url: 'v1/auth/operator/me/password-confirm',
        method: 'POST',
        body
      }),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const { data } = await queryFulfilled;
          if (data?.accessToken && data?.expiresIn) {
            dispatch(
              setAccessToken({
                accessToken: data.accessToken,
                expiresIn: data.expiresIn
              })
            );
          }
        } catch {
          // handled by the mutation consumer
        }
      }
    }),

    // User logout
    logout: builder.mutation<LogoutResponse, void>({
      query: () => ({
        url: 'v1/auth/operator/logout',
        method: 'POST'
      }),
      // Clear navigation cache on logout
      invalidatesTags: ['Auth', 'User', 'Navigation'],
      onQueryStarted: async (_, { dispatch, queryFulfilled }) => {
        try {
          // Wait for logout to complete
          await queryFulfilled;

          // Clear all RTK Query cache
          dispatch(baseApi.util.resetApiState());

          // Dispatch logout event for other components
          window.dispatchEvent(new CustomEvent('userLogout'));
        } catch (error) {
          console.error('Logout failed:', error);
        }
      }
    }),

    // OAuth start — operator tier. Backend signature is POST
    // /v1/auth/operator/oauth/login with `{provider}` in the body.
    initiateOAuth: builder.mutation<
      { redirectUrl: string },
      { provider: string }
    >({
      query: ({ provider }) => ({
        url: 'v1/auth/operator/oauth/login',
        method: 'POST',
        body: { provider }
      })
    }),

    // OAuth callback — single shared endpoint per provider, dispatched
    // server-side to the correct tier via the signed-state JWT.
    handleOAuthCallback: builder.mutation<
      LoginResponse,
      { code: string; state?: string; provider: string }
    >({
      query: ({ code, state, provider }) => ({
        url: `v1/auth/oauth/${provider}/callback`,
        method: 'POST',
        body: { code, state }
      }),
      // Invalidate navigation to fetch role-filtered menu for new user
      invalidatesTags: ['Auth', 'User', 'Navigation']
    }),

    // --- Self-service security center ---

    // Aggregate auth state of the **current** user. Read-only; used
    // by the page header on /user/security and by individual tabs
    // that need a quick is-it-set check.
    getSelfAuthMethods: builder.query<SelfAuthMethods, void>({
      query: () => 'v1/auth/operator/me/auth-methods',
      providesTags: ['SelfAuthMethods']
    }),

    // Active sessions for the current user. The IsCurrent flag is
    // stamped server-side from the JWT sid so the row the request is
    // coming from is highlighted.
    getMySessions: builder.query<SelfSessionsResponse, void>({
      query: () => 'v1/auth/operator/me/sessions',
      providesTags: ['Sessions']
    }),

    // Self-service unlink — gated server-side by RequireStepUp(5m).
    // The global StepUpModal intercepts the 401 and replays.
    unlinkOauthSelf: builder.mutation<
      { success: boolean },
      { provider: OAuthProvider }
    >({
      query: ({ provider }) => ({
        url: `v1/auth/operator/me/oauth/${provider}`,
        method: 'DELETE'
      }),
      invalidatesTags: ['SelfAuthMethods', 'User']
    }),

    // Self-service link — start the OAuth flow that binds a new
    // sign-in provider to the current account. Same step-up gate as
    // unlink (it adds a credential). Returns the IdP redirect URL;
    // the caller is responsible for `window.location.assign(authUrl)`.
    initiateOauthLinkSelf: builder.mutation<
      { authUrl: string; state: string },
      { provider: OAuthProvider }
    >({
      query: ({ provider }) => ({
        url: `v1/auth/operator/me/oauth/link/${provider}`,
        method: 'POST'
      })
    }),

    // Revoke one session. Server returns 409 cannot_revoke_current
    // when the session matches the JWT sid; the UI disables the
    // revoke button for the current row, so this is defensive.
    revokeSession: builder.mutation<void, { sessionId: string }>({
      query: ({ sessionId }) => ({
        url: `v1/auth/operator/me/sessions/${encodeURIComponent(sessionId)}`,
        method: 'DELETE'
      }),
      invalidatesTags: ['Sessions']
    }),

    // Revoke every session except the calling one. The current
    // session is left alive so the response can complete.
    revokeAllSessions: builder.mutation<{ revoked: number }, void>({
      query: () => ({
        url: 'v1/auth/operator/me/sessions',
        method: 'DELETE'
      }),
      invalidatesTags: ['Sessions']
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
        if (
          result.error &&
          (result.error.status === 401 || result.error.status === 403)
        ) {
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
              expiresIn: sessionData.expiresIn
            })
          );
        }

        return { data: sessionData };
      },
      // Keep cache for session management
      keepUnusedDataFor: 60 // 1 minute
    })
  })
});

// Export hooks
export const {
  useGetAuthPolicyQuery,
  useGetCurrentUserQuery,
  useUpdateCurrentUserMutation,
  useLoginMutation,
  useRegisterMutation,
  useVerifyEmailMutation,
  useResendVerificationMutation,
  useForgotPasswordMutation,
  useResetPasswordMutation,
  useChangePasswordMutation,
  useConfirmPasswordMutation,
  useLogoutMutation,
  useInitiateOAuthMutation,
  useHandleOAuthCallbackMutation,
  useGetSessionQuery,
  // Lazy query hooks for conditional fetching
  useLazyGetCurrentUserQuery,
  useLazyGetSessionQuery,
  // Self-service security center
  useGetSelfAuthMethodsQuery,
  useGetMySessionsQuery,
  useUnlinkOauthSelfMutation,
  useInitiateOauthLinkSelfMutation,
  useRevokeSessionMutation,
  useRevokeAllSessionsMutation
} = authApi;

// Export endpoints for manual cache management
export const authApiEndpoints = authApi.endpoints;
