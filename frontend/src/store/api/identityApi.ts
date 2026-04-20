import { baseApi } from './baseApi';
import type {
  IdPConfigPayload,
  IdPConfigView,
  ScimTokenRotated,
  ScimTokenStatus,
} from '../../types/identity';

// identityApi wraps the tenant-scoped identity admin endpoints. All
// endpoints depend on X-Tenant-ID to resolve the target tenant — baseApi
// stamps it automatically from the current tenant context, so no special
// handling is needed here.
export const identityApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // --- IdP (OIDC) config ---
    getIdPConfig: builder.query<IdPConfigView | null, void>({
      // 404 is the happy path for "unset" — normalise to null so callers
      // render the empty state without having to interpret the error.
      queryFn: async (_arg, _api, _extra, baseQueryFn) => {
        const res = await baseQueryFn({ url: '/v1/identity/idp', method: 'GET' });
        if (res.error) {
          if (res.error.status === 404) return { data: null };
          return { error: res.error };
        }
        return { data: res.data as IdPConfigView };
      },
      providesTags: [{ type: 'IdentityIdP' as const, id: 'CURRENT' }],
    }),

    putIdPConfig: builder.mutation<IdPConfigView, IdPConfigPayload>({
      query: (body) => ({ url: '/v1/identity/idp', method: 'PUT', body }),
      invalidatesTags: [{ type: 'IdentityIdP' as const, id: 'CURRENT' }],
    }),

    deleteIdPConfig: builder.mutation<{ success: boolean }, void>({
      query: () => ({ url: '/v1/identity/idp', method: 'DELETE' }),
      invalidatesTags: [{ type: 'IdentityIdP' as const, id: 'CURRENT' }],
    }),

    // --- SCIM token ---
    getScimTokenStatus: builder.query<ScimTokenStatus, void>({
      query: () => ({ url: '/v1/identity/scim/token', method: 'GET' }),
      providesTags: [{ type: 'IdentityScim' as const, id: 'CURRENT' }],
    }),

    rotateScimToken: builder.mutation<ScimTokenRotated, void>({
      query: () => ({ url: '/v1/identity/scim/rotate-token', method: 'POST' }),
      invalidatesTags: [{ type: 'IdentityScim' as const, id: 'CURRENT' }],
    }),
  }),
  overrideExisting: false,
});

export const {
  useGetIdPConfigQuery,
  usePutIdPConfigMutation,
  useDeleteIdPConfigMutation,
  useGetScimTokenStatusQuery,
  useRotateScimTokenMutation,
} = identityApi;
