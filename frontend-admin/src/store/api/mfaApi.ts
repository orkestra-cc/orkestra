import { baseApi } from './baseApi';
import { authApi, type SessionResponse } from './authApi';
import type {
  MfaStatusResponse,
  MfaEnrollBeginResponse,
  MfaEnrollConfirmInput,
  MfaEnrollConfirmResponse,
  MfaVerifyInput,
  MfaVerifyResponse,
  MfaLoginVerifyInput,
  MfaLoginVerifyResponse,
  WebAuthnRegisterBeginResponse,
  WebAuthnRegisterFinishInput,
  WebAuthnRegisterFinishResponse,
  WebAuthnVerifyBeginResponse,
  WebAuthnVerifyFinishInput,
  WebAuthnVerifyFinishResponse,
  WebAuthnLoginBeginInput,
  WebAuthnLoginBeginResponse,
  WebAuthnLoginFinishInput,
  WebAuthnLoginFinishResponse,
  WebAuthnCredentialsListResponse,
} from 'types/mfa';
import { setAccessToken } from '../slices/authSlice';

// Huma v2 lifts the Go handler's `Body` field directly to the top of the
// JSON response — there is no `{body: ...}` wrapper on the wire. Pass the
// parsed payload through as-is.
const unwrap = <T,>(res: unknown): T => res as T;

export const mfaApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Current user's factor status — drives the settings card.
    getMfaStatus: builder.query<MfaStatusResponse, void>({
      query: () => 'v1/auth/operator/me/mfa',
      providesTags: ['MFA'],
      transformResponse: (res: unknown) => unwrap<MfaStatusResponse>(res),
    }),

    // Start enrollment — returns TOTP secret + otpauth:// URI for the QR.
    // The challengeId must be round-tripped to /confirm.
    enrollMfaBegin: builder.mutation<MfaEnrollBeginResponse, void>({
      query: () => ({
        url: 'v1/auth/operator/mfa/enroll/begin',
        method: 'POST',
      }),
      transformResponse: (res: unknown) => unwrap<MfaEnrollBeginResponse>(res),
    }),

    // Confirm enrollment with a TOTP code. Successful response carries the
    // one-shot backup codes that MUST be displayed exactly once.
    enrollMfaConfirm: builder.mutation<MfaEnrollConfirmResponse, MfaEnrollConfirmInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/enroll/confirm',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['MFA', 'User'],
      transformResponse: (res: unknown) => unwrap<MfaEnrollConfirmResponse>(res),
    }),

    // Self-service step-up: verify a TOTP or backup code for the *current*
    // session. Returns a new access token with amr:[..., "otp"] and a fresh
    // last_otp_at. The new token is dispatched into Redux so subsequent
    // requests carry the stepped-up bearer automatically.
    verifyMfa: builder.mutation<MfaVerifyResponse, MfaVerifyInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/verify',
        method: 'POST',
        body,
      }),
      transformResponse: (res: unknown) => unwrap<MfaVerifyResponse>(res),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const { data } = await queryFulfilled;
          if (data?.accessToken && data?.expiresIn) {
            dispatch(setAccessToken({
              accessToken: data.accessToken,
              expiresIn: data.expiresIn,
            }));
          }
        } catch {
          // handled by the mutation consumer
        }
      },
    }),

    // Regenerate backup codes — destroys the existing set and returns
    // a fresh one exactly once. Gated server-side by RequireStepUp(5m);
    // the global StepUpModal handles the 401 replay.
    regenerateBackupCodes: builder.mutation<{ codes: string[] }, void>({
      query: () => ({
        url: 'v1/auth/operator/me/mfa/backup-codes/regenerate',
        method: 'POST',
        body: {},
      }),
      invalidatesTags: ['MFA', 'SelfAuthMethods'],
      transformResponse: (res: unknown) => unwrap<{ codes: string[] }>(res),
    }),

    // Remove the current user's factor. Gated server-side by RequireStepUp —
    // the caller must have verified within the last 5 minutes. The request
    // body is empty; the middleware enforces freshness from JWT claims.
    removeMfa: builder.mutation<{ success: boolean }, void>({
      query: () => ({
        url: 'v1/auth/operator/me/mfa/remove',
        method: 'POST',
        body: {},
      }),
      invalidatesTags: ['MFA', 'User'],
      transformResponse: (res: unknown) => unwrap<{ success: boolean }>(res),
    }),

    // Admin — wipe another user's factor. Gated server-side by
    // RequireStepUp(5m) so the caller must have verified recently. Returns
    // 401 with `code: "step_up_required"` if not; the caller's UI should
    // chain /mfa/verify first to satisfy the gate.
    adminResetUserMfa: builder.mutation<{ success: boolean }, { userId: string }>({
      query: ({ userId }) => ({
        url: `v1/admin/users/${encodeURIComponent(userId)}/mfa/reset`,
        method: 'POST',
      }),
      invalidatesTags: ['User'],
      transformResponse: (res: unknown) => unwrap<{ success: boolean }>(res),
    }),

    // Admin — same surface as adminResetUserMfa but routed at the
    // client-tier admin path so the backend's clientMFAHandler operates
    // against client_users + client_mfa_factors.
    adminResetClientUserMfa: builder.mutation<{ success: boolean }, { userId: string }>({
      query: ({ userId }) => ({
        url: `v1/admin/client-users/${encodeURIComponent(userId)}/mfa/reset`,
        method: 'POST',
      }),
      invalidatesTags: (_r, _e, { userId }) => [
        { type: 'User', id: userId },
        { type: 'User', id: 'CLIENT_LIST' },
      ],
      transformResponse: (res: unknown) => unwrap<{ success: boolean }>(res),
    }),

    // Public — completes a login that was paused with requiresMfa. Unlike
    // /mfa/verify this endpoint has no bearer token yet; the challengeId
    // ties the code back to the paused login so the same user UUID is used.
    loginVerifyMfa: builder.mutation<MfaLoginVerifyResponse, MfaLoginVerifyInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/login/verify',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['User', 'Navigation'],
      transformResponse: (res: unknown) => unwrap<MfaLoginVerifyResponse>(res),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const { data } = await queryFulfilled;
          if (data?.accessToken && data?.expiresIn) {
            dispatch(setAccessToken({
              accessToken: data.accessToken,
              expiresIn: data.expiresIn,
            }));
          }
          // Seed the getSession cache so ProtectedRoute sees an authenticated
          // session immediately on the next render — same pattern as the
          // password login mutation. Without this, useGetSessionQuery returns
          // null after the verify, ProtectedRoute reads isAuthenticated=false
          // and bounces the user back to /login.
          if (data?.user && data?.accessToken) {
            dispatch(
              authApi.util.upsertQueryData('getSession', undefined, {
                accessToken: data.accessToken,
                tokenType: data.tokenType ?? 'Bearer',
                expiresIn: data.expiresIn ?? 0,
                user: data.user,
                success: true,
              } as SessionResponse)
            );
          }
        } catch {
          // handled by the mutation consumer
        }
      },
    }),

    // --- WebAuthn ---
    //
    // Each ceremony is two-step (Begin → browser API call → Finish). The
    // begin endpoints return the W3C JSON the browser needs; the finish
    // endpoints accept the encoded credential JSON returned by the API.

    webAuthnRegisterBegin: builder.mutation<WebAuthnRegisterBeginResponse, void>({
      query: () => ({
        url: 'v1/auth/operator/mfa/webauthn/register/begin',
        method: 'POST',
      }),
      transformResponse: (res: unknown) => unwrap<WebAuthnRegisterBeginResponse>(res),
    }),
    webAuthnRegisterFinish: builder.mutation<WebAuthnRegisterFinishResponse, WebAuthnRegisterFinishInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/webauthn/register/finish',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['MFA'],
      transformResponse: (res: unknown) => unwrap<WebAuthnRegisterFinishResponse>(res),
    }),

    webAuthnList: builder.query<WebAuthnCredentialsListResponse, void>({
      query: () => 'v1/auth/operator/me/mfa/webauthn/credentials',
      providesTags: ['MFA'],
      transformResponse: (res: unknown) => unwrap<WebAuthnCredentialsListResponse>(res),
    }),

    // Step-up gated server-side; if the caller hasn't recently re-verified
    // the global StepUpModal will intercept the 401 and replay this DELETE.
    webAuthnRemove: builder.mutation<{ success: boolean }, { credentialId: string }>({
      query: ({ credentialId }) => ({
        url: `v1/auth/operator/me/mfa/webauthn/credentials/${encodeURIComponent(credentialId)}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['MFA'],
      transformResponse: (res: unknown) => unwrap<{ success: boolean }>(res),
    }),

    webAuthnVerifyBegin: builder.mutation<WebAuthnVerifyBeginResponse, void>({
      query: () => ({
        url: 'v1/auth/operator/mfa/webauthn/verify/begin',
        method: 'POST',
      }),
      transformResponse: (res: unknown) => unwrap<WebAuthnVerifyBeginResponse>(res),
    }),
    webAuthnVerifyFinish: builder.mutation<WebAuthnVerifyFinishResponse, WebAuthnVerifyFinishInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/webauthn/verify/finish',
        method: 'POST',
        body,
      }),
      transformResponse: (res: unknown) => unwrap<WebAuthnVerifyFinishResponse>(res),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const { data } = await queryFulfilled;
          if (data?.accessToken && data?.expiresIn) {
            dispatch(setAccessToken({
              accessToken: data.accessToken,
              expiresIn: data.expiresIn,
            }));
          }
        } catch {
          // handled by the mutation consumer
        }
      },
    }),

    webAuthnLoginBegin: builder.mutation<WebAuthnLoginBeginResponse, WebAuthnLoginBeginInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/webauthn/login/begin',
        method: 'POST',
        body,
      }),
      transformResponse: (res: unknown) => unwrap<WebAuthnLoginBeginResponse>(res),
    }),
    webAuthnLoginFinish: builder.mutation<WebAuthnLoginFinishResponse, WebAuthnLoginFinishInput>({
      query: (body) => ({
        url: 'v1/auth/operator/mfa/webauthn/login/finish',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['User', 'Navigation'],
      transformResponse: (res: unknown) => unwrap<WebAuthnLoginFinishResponse>(res),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          const { data } = await queryFulfilled;
          if (data?.accessToken && data?.expiresIn) {
            dispatch(setAccessToken({
              accessToken: data.accessToken,
              expiresIn: data.expiresIn,
            }));
          }
        } catch {
          // handled by the mutation consumer
        }
      },
    }),
  }),
  overrideExisting: false,
});

export const {
  useGetMfaStatusQuery,
  useEnrollMfaBeginMutation,
  useEnrollMfaConfirmMutation,
  useVerifyMfaMutation,
  useRemoveMfaMutation,
  useRegenerateBackupCodesMutation,
  useLoginVerifyMfaMutation,
  useAdminResetUserMfaMutation,
  useAdminResetClientUserMfaMutation,
  useWebAuthnRegisterBeginMutation,
  useWebAuthnRegisterFinishMutation,
  useWebAuthnListQuery,
  useWebAuthnRemoveMutation,
  useWebAuthnVerifyBeginMutation,
  useWebAuthnVerifyFinishMutation,
  useWebAuthnLoginBeginMutation,
  useWebAuthnLoginFinishMutation,
} = mfaApi;
