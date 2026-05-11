import { baseApi } from './baseApi';

// One trust grant the user holds. Mirrors the backend's
// trustedDevicePublic shape exactly. The backend includes only
// non-expired, non-revoked grants in the list.
export interface TrustedDevice {
  uuid: string;
  deviceId: string;
  deviceName?: string;
  platform?: string;
  ipAddress?: string;
  grantedAmr?: string;
  trustedAt: string; // ISO date
  trustedUntil: string; // ISO date — 30d default
  lastUsedAt?: string;
}

export interface TrustedDevicesListResponse {
  devices: TrustedDevice[];
}

// Huma v2 lifts the Go handler's Body field directly to the top-level
// JSON, so no `{body: ...}` wrapper handling needed.

export const deviceTrustApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // List active trust grants for the current user.
    listTrustedDevices: builder.query<TrustedDevicesListResponse, void>({
      query: () => 'v1/auth/operator/me/devices/trust',
      providesTags: ['TrustedDevices'],
    }),

    // Drop trust for one device. Idempotent — returns 204 even when
    // the device wasn't trusted, so the UI can call this from a
    // confirmation modal without first checking the list.
    revokeTrustedDevice: builder.mutation<void, { deviceId: string }>({
      query: ({ deviceId }) => ({
        url: `v1/auth/operator/me/devices/trust/${encodeURIComponent(deviceId)}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['TrustedDevices'],
    }),

    // Drop every active trust grant. Same idempotency as revoke-one.
    revokeAllTrustedDevices: builder.mutation<void, void>({
      query: () => ({
        url: 'v1/auth/operator/me/devices/trust',
        method: 'DELETE',
      }),
      invalidatesTags: ['TrustedDevices'],
    }),
  }),
  overrideExisting: false,
});

export const {
  useListTrustedDevicesQuery,
  useRevokeTrustedDeviceMutation,
  useRevokeAllTrustedDevicesMutation,
} = deviceTrustApi;
