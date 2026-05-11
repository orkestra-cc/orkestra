import { baseApi } from './baseApi';

// OAuth Provider information
export interface UserOAuthProviderInfo {
  provider: string;
  email: string;
  avatar?: string;
}

// User management types based on backend OpenAPI
export interface User {
  id: string;
  email: string;
  username: string;
  fullName: string;
  avatar?: string;
  role: string;
  phone?: string;
  providers: UserOAuthProviderInfo[];
  isActive: boolean;
  emailVerified: boolean;
  lastLogin?: string;
  createdAt: string;
  updatedAt: string;
}

export interface UserListResponse {
  users: User[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface CreateUserInput {
  email: string;
  username: string;
  fullName: string;
  phone: string;
  pin: string;
  role: string;
}

export interface UpdateUserInput {
  email?: string;
  username?: string;
  fullName?: string;
  phone?: string;
  role?: string;
  isActive?: boolean;
  pin?: string;
  avatar?: string;
}

export interface DeleteUserResponse {
  success: boolean;
  message: string;
}


export interface UserMetrics {
  tasksCompleted: number;
  totalTasks: number;
  onTimeDeliveryRate: number;
  performanceRating: number;
  teamCollaboration: number;
  systemUsage: {
    dashboard: number;
    reports: number;
    settings: number;
    helpDesk: number;
  };
  quickStats: {
    loginCount: number;
    onlineTimeHours: number;
    activeTasks: number;
    overdueTasks: number;
  };
}

export interface UserListParams {
  role?: string;
  isActive?: boolean;
  emailVerified?: boolean;
  search?: string;
  page?: number;
  pageSize?: number;
}

// AdminClientUserMembership mirrors the backend's AdminUserMembership —
// the trimmed projection of a tenant_memberships row that the admin
// /admin/clients page renders as a per-row badge column.
export interface AdminClientUserMembership {
  tenantUUID: string;
  tenantName: string;
  tenantSlug?: string;
  tenantKind: string;
  roles?: string[];
  isOwner?: boolean;
}

// AdminClientUserItem is one row of the admin client-users list — a
// client_users record with its tenant memberships joined in. Users with
// no membership return an empty array (self-registered, not yet attached).
//
// `providers` is populated by the single-user GET endpoint and left
// empty by the list endpoint (the list does not show OAuth links).
export interface AdminClientUserItem {
  id: string;
  email: string;
  username?: string;
  fullName?: string;
  avatar?: string;
  role: string;
  isActive: boolean;
  emailVerified: boolean;
  lastLogin?: string;
  createdAt: string;
  memberships: AdminClientUserMembership[];
  providers?: UserOAuthProviderInfo[];
}

// UpdateClientUserAdminInput is the slim PATCH payload for the admin
// client-user mutation endpoint. Only set the fields you intend to
// change — omitted fields stay untouched server-side.
export interface UpdateClientUserAdminInput {
  fullName?: string;
  username?: string;
  email?: string;
  phone?: string;
  role?: string;
  isActive?: boolean;
}

// CreateClientUserAdminInput drives admin-direct creation of a Tier-2
// client user. The new user is pre-verified and active.
export interface CreateClientUserAdminInput {
  email: string;
  fullName: string;
  username?: string;
  phone?: string;
  role: string;
  password: string;
}

// InviteClientUserAdminInput drives the alternate "send an invite email"
// flow — the new user has no password until they redeem the token.
export interface InviteClientUserAdminInput {
  email: string;
  fullName: string;
  username?: string;
  phone?: string;
  role: string;
  inviterName?: string;
}

// AdminTriggerResponse mirrors the no-body confirmation shape used by
// resend-verification, send-password-reset, and resend-invite.
export interface AdminTriggerResponse {
  success: boolean;
  message: string;
}

// --- Admin user auth-methods surface ---
// One-to-one mirror of authModels.AuthMethodsView from the Go backend.
// The card on /admin/user/profile/:userId consumes this as its single
// source of truth for the user's auth state.

export type OAuthProviderName = 'google' | 'apple' | 'github' | 'discord';

export interface AdminAuthWebAuthnCredential {
  credentialId: string; // base64url, no padding
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

export interface AdminAuthMfaFactor {
  type: 'totp' | 'webauthn';
  enrolledAt?: string;
  lastUsedAt?: string;
  backupCodesRemaining?: number;
  credentials?: AdminAuthWebAuthnCredential[];
}

export interface AdminAuthOAuthProvider {
  provider: OAuthProviderName;
  email: string;
  linkedAt: string;
  lastUsedAt?: string;
  isPrimary: boolean;
}

export interface AdminAuthMethods {
  hasUsablePassword: boolean;
  passwordUpdatedAt?: string;
  emailVerified: boolean;
  lastLoginAt?: string;
  mfaRequired: boolean;
  mfaGraceStartedAt?: string;
  mfaGraceExpiresAt?: string;
  mfaFactors: AdminAuthMfaFactor[];
  oauthProviders: AdminAuthOAuthProvider[];
}

export interface AdminClientUserListResponse {
  users: AdminClientUserItem[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface AdminClientUserListParams {
  role?: string;
  isActive?: boolean;
  emailVerified?: boolean;
  search?: string;
  page?: number;
  pageSize?: number;
}

// User management API slice
export const userApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // List users with filtering and pagination
    getUsers: builder.query<UserListResponse, UserListParams | undefined>({
      query: (params) => {
        const searchParams = new URLSearchParams();

        // Add parameters if they exist
        if (params?.role) searchParams.append('role', params.role);
        if (params?.isActive !== undefined) searchParams.append('isActive', String(params.isActive));
        if (params?.emailVerified !== undefined) searchParams.append('emailVerified', String(params.emailVerified));
        if (params?.search) searchParams.append('search', params.search);
        if (params?.page !== undefined) searchParams.append('page', String(params.page));
        if (params?.pageSize !== undefined) searchParams.append('pageSize', String(params.pageSize));

        return {
          url: `/v1/users?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result
          ? [
              ...result.users.map(({ id }) => ({ type: 'User' as const, id })),
              { type: 'User', id: 'LIST' },
            ]
          : [{ type: 'User', id: 'LIST' }],
    }),

    // Get user by ID
    getUserById: builder.query<User, string>({
      query: (id) => `/v1/users/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'User', id }],
    }),

    // Get user by email
    getUserByEmail: builder.query<User, string>({
      query: (email) => `/v1/users/by-email?email=${email}`,
      providesTags: (result) => result ? [{ type: 'User', id: result.id }] : [],
    }),

    // Create new user
    createUser: builder.mutation<User, CreateUserInput>({
      query: (userData) => ({
        url: '/v1/users',
        method: 'POST',
        body: userData,
      }),
      invalidatesTags: [{ type: 'User', id: 'LIST' }],
    }),

    // Update user
    updateUser: builder.mutation<User, { id: string; data: UpdateUserInput }>({
      query: ({ id, data }) => ({
        url: `/v1/users/${id}`,
        method: 'PUT',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'User', id },
        { type: 'User', id: 'LIST' },
      ],
    }),

    // Delete user
    deleteUser: builder.mutation<DeleteUserResponse, string>({
      query: (id) => ({
        url: `/v1/users/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'User', id },
        { type: 'User', id: 'LIST' },
      ],
    }),

    // Get user metrics
    getUserMetrics: builder.query<UserMetrics, string>({
      query: (userId) => `/v1/users/${userId}/metrics`,
      providesTags: (_result, _error, userId) => [
        { type: 'User' as const, id: `metrics-${userId}` }
      ],
    }),

    // Admin — list Tier-2 client users with tenant memberships joined.
    // Powers the /admin/clients page (user-centric view of client_users).
    listClientUsersAdmin: builder.query<AdminClientUserListResponse, AdminClientUserListParams | undefined>({
      query: (params) => {
        const sp = new URLSearchParams();
        if (params?.role) sp.append('role', params.role);
        if (params?.isActive !== undefined) sp.append('isActive', String(params.isActive));
        if (params?.emailVerified !== undefined) sp.append('emailVerified', String(params.emailVerified));
        if (params?.search) sp.append('search', params.search);
        if (params?.page !== undefined) sp.append('page', String(params.page));
        if (params?.pageSize !== undefined) sp.append('pageSize', String(params.pageSize));
        const qs = sp.toString();
        return {
          url: qs ? `/v1/admin/client-users?${qs}` : '/v1/admin/client-users',
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result
          ? [
              ...result.users.map(({ id }) => ({ type: 'User' as const, id })),
              { type: 'User', id: 'CLIENT_LIST' },
            ]
          : [{ type: 'User', id: 'CLIENT_LIST' }],
    }),

    // Admin — single Tier-2 client user with tenant memberships and
    // OAuth providers joined in. Powers /admin/clients/:userId.
    getClientUserAdmin: builder.query<AdminClientUserItem, string>({
      query: (id) => `/v1/admin/client-users/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'User', id }],
    }),

    // Admin — patch profile / role / active status for a client user.
    updateClientUserAdmin: builder.mutation<
      AdminClientUserItem,
      { id: string; data: UpdateClientUserAdminInput }
    >({
      query: ({ id, data }) => ({
        url: `/v1/admin/client-users/${id}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'User', id },
        { type: 'User', id: 'CLIENT_LIST' },
      ],
    }),

    // Admin — soft-delete a client user with email aliasing.
    deleteClientUserAdmin: builder.mutation<{ message: string }, string>({
      query: (id) => ({
        url: `/v1/admin/client-users/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'User', id },
        { type: 'User', id: 'CLIENT_LIST' },
      ],
    }),

    // Admin — create a Tier-2 client user directly (pre-verified).
    createClientUserAdmin: builder.mutation<
      AdminClientUserItem,
      CreateClientUserAdminInput
    >({
      query: (data) => ({
        url: '/v1/admin/client-users',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [{ type: 'User', id: 'CLIENT_LIST' }],
    }),

    // Admin — invite a new Tier-2 client user. Server creates the row
    // with no password and emails an admin_invite token.
    inviteClientUserAdmin: builder.mutation<
      AdminClientUserItem,
      InviteClientUserAdminInput
    >({
      query: (data) => ({
        url: '/v1/admin/client-users/invite',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [{ type: 'User', id: 'CLIENT_LIST' }],
    }),

    // Admin — re-send the invite email for an existing user.
    resendInviteClientUserAdmin: builder.mutation<
      AdminTriggerResponse,
      { id: string; inviterName?: string }
    >({
      query: ({ id, inviterName }) => ({
        url: `/v1/admin/client-users/${id}/invite/resend`,
        method: 'POST',
        body: { inviterName },
      }),
    }),

    // Admin — re-send the email-verification link.
    resendVerificationClientUserAdmin: builder.mutation<AdminTriggerResponse, string>({
      query: (id) => ({
        url: `/v1/admin/client-users/${id}/resend-verification`,
        method: 'POST',
      }),
      invalidatesTags: (_r, _e, id) => [{ type: 'User', id }],
    }),

    // Admin — trigger a password-reset email.
    sendPasswordResetClientUserAdmin: builder.mutation<AdminTriggerResponse, string>({
      query: (id) => ({
        url: `/v1/admin/client-users/${id}/send-password-reset`,
        method: 'POST',
      }),
    }),

    // Admin — aggregate auth state of an operator user. Drives the
    // Authentication Methods card on /admin/user/profile/:userId.
    getUserAuthMethodsAdmin: builder.query<AdminAuthMethods, string>({
      query: (id) => `/v1/admin/users/${id}/auth-methods`,
      providesTags: (_r, _e, id) => [{ type: 'User', id: `auth-methods-${id}` }],
    }),

    // Admin — trigger a password-reset email for an operator user.
    sendPasswordResetUserAdmin: builder.mutation<AdminTriggerResponse, string>({
      query: (id) => ({
        url: `/v1/admin/users/${id}/send-password-reset`,
        method: 'POST',
      }),
      // No tag invalidation: send-password-reset has no read-side
      // effect on the user record; the card stays accurate.
    }),

    // Admin — resend the email-verification message for an operator user.
    resendVerificationUserAdmin: builder.mutation<AdminTriggerResponse, string>({
      query: (id) => ({
        url: `/v1/admin/users/${id}/resend-verification`,
        method: 'POST',
      }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'User', id },
        { type: 'User', id: `auth-methods-${id}` },
      ],
    }),

    // Admin — unlink an OAuth identity from an operator user.
    unlinkOAuthUserAdmin: builder.mutation<{ success: boolean }, { id: string; provider: OAuthProviderName }>({
      query: ({ id, provider }) => ({
        url: `/v1/admin/users/${id}/oauth/${provider}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'User', id },
        { type: 'User', id: `auth-methods-${id}` },
      ],
    }),
  }),
});

// Export hooks for usage in components
export const {
  useGetUsersQuery,
  useGetUserByIdQuery,
  useGetUserByEmailQuery,
  useCreateUserMutation,
  useUpdateUserMutation,
  useDeleteUserMutation,

  useGetUserMetricsQuery,
  useListClientUsersAdminQuery,
  useGetClientUserAdminQuery,
  useUpdateClientUserAdminMutation,
  useDeleteClientUserAdminMutation,
  useCreateClientUserAdminMutation,
  useInviteClientUserAdminMutation,
  useResendInviteClientUserAdminMutation,
  useResendVerificationClientUserAdminMutation,
  useSendPasswordResetClientUserAdminMutation,
  useGetUserAuthMethodsAdminQuery,
  useSendPasswordResetUserAdminMutation,
  useResendVerificationUserAdminMutation,
  useUnlinkOAuthUserAdminMutation,
} = userApi;