import { baseApi } from './baseApi';
import type {
  AdminNavigationResponse,
  NavOverride,
  PatchOrderBody
} from 'types/navigation';

// navigationAdminApi wraps the three operator-only endpoints under
// /v1/admin/navigation. The PATCH/DELETE mutations invalidate BOTH the
// admin snapshot AND the public Navigation tag so the live sidebar
// reflects the new order immediately — operators see what their users
// will see without a refresh.
//
// Note on response shape: Huma v2 serializes the handler's `Body` field
// flat — the on-wire JSON is the body type itself, never wrapped under
// the Go field name. So `AdminNavigationResponse` lands at the root of
// the response (matches `/v1/navigation`'s `NavigationResponse`). Do
// not add a `transformResponse` that unwraps a "navigation" key.

export const navigationAdminApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    getAdminNavigation: builder.query<AdminNavigationResponse, void>({
      query: () => ({ url: '/v1/admin/navigation', method: 'GET' }),
      providesTags: [{ type: 'NavigationAdmin' as const, id: 'TREE' }]
    }),

    patchNavigationOrder: builder.mutation<NavOverride, PatchOrderBody>({
      query: body => ({
        url: '/v1/admin/navigation/order',
        method: 'PATCH',
        body
      }),
      invalidatesTags: [
        { type: 'NavigationAdmin' as const, id: 'TREE' },
        'Navigation'
      ]
    }),

    deleteNavigationOrder: builder.mutation<void, { parentKey: string }>({
      query: ({ parentKey }) => ({
        url: '/v1/admin/navigation/order',
        method: 'DELETE',
        params: { parentKey }
      }),
      invalidatesTags: [
        { type: 'NavigationAdmin' as const, id: 'TREE' },
        'Navigation'
      ]
    })
  })
});

export const {
  useGetAdminNavigationQuery,
  usePatchNavigationOrderMutation,
  useDeleteNavigationOrderMutation
} = navigationAdminApi;
