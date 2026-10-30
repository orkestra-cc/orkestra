import { baseApi } from './baseApi';

// PIN sign-in. The console is the operator surface,
// so the self routes are the operator tier's; the admin routes address
// either tier's users from here, as the MFA reset does.

export interface PinDevice {
  id: string;
  deviceId: string;
  deviceName?: string;
  platform?: string;
  via: 'admin' | 'self';
  createdAt: string;
  lastUsedAt?: string;
}

export interface PinMethodView {
  available: boolean;
  set: boolean;
  updatedAt?: string;
  lockedUntil?: string;
  length: number;
  devices: PinDevice[];
  requiresTrustedDevice: boolean;
  // Administrator view only: the caller may change this PIN (user.update).
  canManage?: boolean;
}

export interface PinEnrollmentCode {
  code: string;
  expiresAt: string;
}

export type PinTier = 'operator' | 'client';

const adminBase = (tier: PinTier, id: string) =>
  `v1/admin/${tier === 'client' ? 'client-users' : 'users'}/${encodeURIComponent(id)}/auth-methods/pin`;

const tag = (id: string) => ({ type: 'Pin' as const, id });

export interface AdminPinTarget {
  id: string;
  tier: PinTier;
}

export const pinApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    // --- own PIN ---
    getSelfPin: builder.query<PinMethodView, void>({
      query: () => 'v1/auth/operator/me/pin',
      providesTags: [tag('self')]
    }),
    setSelfPin: builder.mutation<void, { pin: string }>({
      query: body => ({ url: 'v1/auth/operator/me/pin', method: 'PUT', body }),
      invalidatesTags: [tag('self'), 'SelfAuthMethods']
    }),
    removeSelfPin: builder.mutation<void, void>({
      query: () => ({ url: 'v1/auth/operator/me/pin', method: 'DELETE' }),
      invalidatesTags: [tag('self'), 'SelfAuthMethods']
    }),
    issueSelfPinEnrollmentCode: builder.mutation<PinEnrollmentCode, void>({
      query: () => ({
        url: 'v1/auth/operator/me/pin/enrollment-code',
        method: 'POST'
      })
    }),
    revokeSelfPinDevice: builder.mutation<void, { deviceId: string }>({
      query: ({ deviceId }) => ({
        url: `v1/auth/operator/me/pin/devices/${encodeURIComponent(deviceId)}`,
        method: 'DELETE'
      }),
      invalidatesTags: [tag('self')]
    }),

    // --- another user's PIN (admin) ---
    getUserPinAdmin: builder.query<PinMethodView, AdminPinTarget>({
      query: ({ id, tier }) => adminBase(tier, id),
      providesTags: (_r, _e, { id }) => [tag(id)]
    }),
    setUserPinAdmin: builder.mutation<void, AdminPinTarget & { pin: string }>({
      query: ({ id, tier, pin }) => ({
        url: adminBase(tier, id),
        method: 'PUT',
        body: { pin }
      }),
      invalidatesTags: (_r, _e, { id }) => [
        tag(id),
        { type: 'User', id: `auth-methods-${id}` }
      ]
    }),
    removeUserPinAdmin: builder.mutation<void, AdminPinTarget>({
      query: ({ id, tier }) => ({ url: adminBase(tier, id), method: 'DELETE' }),
      invalidatesTags: (_r, _e, { id }) => [
        tag(id),
        { type: 'User', id: `auth-methods-${id}` }
      ]
    }),
    issueUserPinEnrollmentCodeAdmin: builder.mutation<
      PinEnrollmentCode,
      AdminPinTarget
    >({
      query: ({ id, tier }) => ({
        url: `${adminBase(tier, id)}/enrollment-code`,
        method: 'POST'
      })
    }),
    revokeUserPinDeviceAdmin: builder.mutation<
      void,
      AdminPinTarget & { deviceId: string }
    >({
      query: ({ id, tier, deviceId }) => ({
        url: `${adminBase(tier, id)}/devices/${encodeURIComponent(deviceId)}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, { id }) => [tag(id)]
    })
  })
});

export const {
  useGetSelfPinQuery,
  useSetSelfPinMutation,
  useRemoveSelfPinMutation,
  useIssueSelfPinEnrollmentCodeMutation,
  useRevokeSelfPinDeviceMutation,
  useGetUserPinAdminQuery,
  useSetUserPinAdminMutation,
  useRemoveUserPinAdminMutation,
  useIssueUserPinEnrollmentCodeAdminMutation,
  useRevokeUserPinDeviceAdminMutation
} = pinApi;

// pinErrorKey maps the backend's stable PIN codes to i18n keys under
// pinMethod.errors; anything else falls back to the server's detail.
export function pinErrorKey(err: unknown): string | null {
  const code = (err as { data?: { code?: string } })?.data?.code;
  switch (code) {
    case 'auth.pin_invalid':
    case 'auth.pin_too_weak':
    case 'auth.pin_username_required':
    case 'auth.pin_username_not_unique':
    case 'auth.pin_not_allowed':
    case 'auth.pin_not_set':
    case 'auth.pin_session_forbidden':
    case 'auth.pin_device_not_found':
    case 'user.target_tier_forbidden':
      return `pinMethod.errors.${code.replace('.', '_')}`;
    default:
      return null;
  }
}
