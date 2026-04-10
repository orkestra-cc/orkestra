// Example RTK Query slice for a feature module.
//
// To use: copy this file to `src/store/api/<name>Api.ts` and replace
// `Widget` / `widget` / `widgets` with your module name. Add the cache
// tag types ('Widget', 'WidgetStats') to the `tagTypes` array in
// `src/store/api/baseApi.ts` first, otherwise TypeScript will reject them.
//
// This is the same pattern used by every existing module slice — see
// `src/store/api/companyApi.ts` and `src/store/api/billingApi.ts` for
// real-world examples.

import { baseApi } from '../../../store/api/baseApi';
import type {
  Widget,
  WidgetListParams,
  WidgetListResponse,
  CreateWidgetInput,
  UpdateWidgetInput,
} from './types';

const buildQueryParams = (params: Record<string, unknown>): string => {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      search.append(key, String(value));
    }
  });
  return search.toString();
};

export const widgetsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    listWidgets: builder.query<WidgetListResponse, WidgetListParams | undefined>({
      query: (params) => {
        const qs = params ? buildQueryParams(params) : '';
        return {
          url: `/v1/widgets${qs ? `?${qs}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.widgets
          ? [
              ...result.widgets.map(({ uuid }) => ({ type: 'Widget' as const, id: uuid })),
              { type: 'Widget', id: 'LIST' },
            ]
          : [{ type: 'Widget', id: 'LIST' }],
    }),

    getWidget: builder.query<Widget, string>({
      query: (id) => `/v1/widgets/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Widget', id }],
    }),

    createWidget: builder.mutation<Widget, CreateWidgetInput>({
      query: (body) => ({
        url: '/v1/widgets',
        method: 'POST',
        body,
      }),
      invalidatesTags: [{ type: 'Widget', id: 'LIST' }],
    }),

    updateWidget: builder.mutation<Widget, { id: string; patch: UpdateWidgetInput }>({
      query: ({ id, patch }) => ({
        url: `/v1/widgets/${id}`,
        method: 'PATCH',
        body: patch,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Widget', id },
        { type: 'Widget', id: 'LIST' },
      ],
    }),

    deleteWidget: builder.mutation<void, string>({
      query: (id) => ({
        url: `/v1/widgets/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Widget', id },
        { type: 'Widget', id: 'LIST' },
      ],
    }),
  }),
});

export const {
  useListWidgetsQuery,
  useGetWidgetQuery,
  useCreateWidgetMutation,
  useUpdateWidgetMutation,
  useDeleteWidgetMutation,
} = widgetsApi;
