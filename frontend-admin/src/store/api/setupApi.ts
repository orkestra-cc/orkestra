import { baseApi } from './baseApi';
import type { BackendUser } from './authApi';

/**
 * Drives the first-install onboarding wizard. Both endpoints live at
 * /v1/setup/* on the backend and are unauthenticated — they are gated by
 * the "no users exist yet" invariant enforced server-side.
 */

export interface SetupStatus {
  setupCompleted: boolean;
  smtpConfigured: boolean;
}

export interface CreateAdminInput {
  email: string;
  password: string;
  fullName: string;
}

export interface CreateAdminResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  user: BackendUser;
}

export const setupApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Lightweight status probe. Used by SetupGate on app boot and again after
    // the wizard completes so the gate can stop redirecting.
    getSetupStatus: builder.query<SetupStatus, void>({
      query: () => '/v1/setup/status',
      providesTags: ['Setup'],
      // Cache for longer than the default — the underlying state only flips
      // once per deployment. The wizard explicitly invalidates on success.
      keepUnusedDataFor: 300,
    }),

    // Create the first administrator. Returns a full login response so the
    // caller can dispatch the standard auth slice `login` action and the
    // remaining wizard steps run authenticated.
    createInitialAdmin: builder.mutation<CreateAdminResponse, CreateAdminInput>({
      query: (body) => ({
        url: '/v1/setup/admin',
        method: 'POST',
        body,
      }),
      // A successful admin creation flips setupCompleted to true; invalidate
      // the status cache so any subscribed SetupGate re-checks immediately.
      invalidatesTags: ['Setup', 'Auth', 'User', 'Navigation'],
    }),
  }),
});

export const {
  useGetSetupStatusQuery,
  useCreateInitialAdminMutation,
} = setupApi;
