import { baseApi } from './baseApi';
import type {
  SubscriptionService,
  SubscriptionClient,
  Subscription,
  SubscriptionInvoice,
  ActivityLog,
  ListResponse,
  CreateServiceInput,
  UpdateServiceInput,
  CreateClientInput,
  UpdateClientInput,
  CreateSubscriptionInput,
} from '../../types/subscriptions';

const buildQS = (params: Record<string, unknown>): string => {
  const sp = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') sp.append(k, String(v));
  });
  const qs = sp.toString();
  return qs ? `?${qs}` : '';
};

export const subscriptionsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // --- Services (catalog) ---
    listSubscriptionServices: builder.query<
      ListResponse<SubscriptionService>,
      { active?: string; category?: string } | undefined
    >({
      query: (params) => `/v1/subscriptions/services${params ? buildQS(params) : ''}`,
      providesTags: (result) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({ type: 'SubscriptionService' as const, id: uuid })),
              { type: 'SubscriptionService', id: 'LIST' },
            ]
          : [{ type: 'SubscriptionService', id: 'LIST' }],
    }),
    getSubscriptionService: builder.query<{ body: SubscriptionService }, string>({
      query: (id) => `/v1/subscriptions/services/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'SubscriptionService', id }],
    }),
    createSubscriptionService: builder.mutation<{ body: SubscriptionService }, CreateServiceInput>({
      query: (body) => ({ url: '/v1/subscriptions/services', method: 'POST', body }),
      invalidatesTags: [{ type: 'SubscriptionService', id: 'LIST' }],
    }),
    updateSubscriptionService: builder.mutation<
      { body: SubscriptionService },
      { id: string; patch: UpdateServiceInput }
    >({
      query: ({ id, patch }) => ({ url: `/v1/subscriptions/services/${id}`, method: 'PATCH', body: patch }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'SubscriptionService', id },
        { type: 'SubscriptionService', id: 'LIST' },
      ],
    }),
    deleteSubscriptionService: builder.mutation<void, string>({
      query: (id) => ({ url: `/v1/subscriptions/services/${id}`, method: 'DELETE' }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'SubscriptionService', id },
        { type: 'SubscriptionService', id: 'LIST' },
      ],
    }),

    // --- Clients ---
    listSubscriptionClients: builder.query<
      ListResponse<SubscriptionClient>,
      { status?: string; search?: string } | undefined
    >({
      query: (params) => `/v1/subscriptions/clients${params ? buildQS(params) : ''}`,
      providesTags: (result) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({ type: 'SubscriptionClient' as const, id: uuid })),
              { type: 'SubscriptionClient', id: 'LIST' },
            ]
          : [{ type: 'SubscriptionClient', id: 'LIST' }],
    }),
    getSubscriptionClient: builder.query<{ body: SubscriptionClient }, string>({
      query: (id) => `/v1/subscriptions/clients/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'SubscriptionClient', id }],
    }),
    createSubscriptionClient: builder.mutation<{ body: SubscriptionClient }, CreateClientInput>({
      query: (body) => ({ url: '/v1/subscriptions/clients', method: 'POST', body }),
      invalidatesTags: [{ type: 'SubscriptionClient', id: 'LIST' }],
    }),
    updateSubscriptionClient: builder.mutation<
      { body: SubscriptionClient },
      { id: string; patch: UpdateClientInput }
    >({
      query: ({ id, patch }) => ({ url: `/v1/subscriptions/clients/${id}`, method: 'PATCH', body: patch }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'SubscriptionClient', id },
        { type: 'SubscriptionClient', id: 'LIST' },
      ],
    }),
    archiveSubscriptionClient: builder.mutation<void, string>({
      query: (id) => ({ url: `/v1/subscriptions/clients/${id}`, method: 'DELETE' }),
      invalidatesTags: [{ type: 'SubscriptionClient', id: 'LIST' }],
    }),

    // --- Subscriptions ---
    listSubscriptions: builder.query<
      ListResponse<Subscription>,
      { clientUUID?: string; serviceUUID?: string; status?: string } | undefined
    >({
      query: (params) => `/v1/subscriptions/subscriptions${params ? buildQS(params) : ''}`,
      providesTags: (result) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({ type: 'Subscription' as const, id: uuid })),
              { type: 'Subscription', id: 'LIST' },
            ]
          : [{ type: 'Subscription', id: 'LIST' }],
    }),
    getSubscription: builder.query<{ body: Subscription }, string>({
      query: (id) => `/v1/subscriptions/subscriptions/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'Subscription', id }],
    }),
    createSubscription: builder.mutation<{ body: Subscription }, CreateSubscriptionInput>({
      query: (body) => ({ url: '/v1/subscriptions/subscriptions', method: 'POST', body }),
      invalidatesTags: [{ type: 'Subscription', id: 'LIST' }],
    }),
    cancelSubscription: builder.mutation<
      { body: Subscription },
      { id: string; atPeriodEnd: boolean }
    >({
      query: ({ id, atPeriodEnd }) => ({
        url: `/v1/subscriptions/subscriptions/${id}/cancel`,
        method: 'POST',
        body: { atPeriodEnd },
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'Subscription', id },
        { type: 'Subscription', id: 'LIST' },
        { type: 'SubscriptionActivity', id },
      ],
    }),
    reactivateSubscription: builder.mutation<{ body: Subscription }, string>({
      query: (id) => ({ url: `/v1/subscriptions/subscriptions/${id}/reactivate`, method: 'POST' }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'Subscription', id },
        { type: 'Subscription', id: 'LIST' },
        { type: 'SubscriptionActivity', id },
      ],
    }),
    retryCharge: builder.mutation<{ body: Subscription }, string>({
      query: (id) => ({ url: `/v1/subscriptions/subscriptions/${id}/retry-charge`, method: 'POST' }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'Subscription', id },
        { type: 'SubscriptionInvoice', id },
        { type: 'SubscriptionActivity', id },
      ],
    }),
    listSubscriptionInvoices: builder.query<ListResponse<SubscriptionInvoice>, string>({
      query: (id) => `/v1/subscriptions/subscriptions/${id}/invoices`,
      providesTags: (_r, _e, id) => [{ type: 'SubscriptionInvoice', id }],
    }),
    listSubscriptionActivity: builder.query<
      ListResponse<ActivityLog>,
      { id: string; limit?: number }
    >({
      query: ({ id, limit }) => `/v1/subscriptions/subscriptions/${id}/activity${buildQS({ limit })}`,
      providesTags: (_r, _e, { id }) => [{ type: 'SubscriptionActivity', id }],
    }),
  }),
});

export const {
  useListSubscriptionServicesQuery,
  useGetSubscriptionServiceQuery,
  useCreateSubscriptionServiceMutation,
  useUpdateSubscriptionServiceMutation,
  useDeleteSubscriptionServiceMutation,
  useListSubscriptionClientsQuery,
  useGetSubscriptionClientQuery,
  useCreateSubscriptionClientMutation,
  useUpdateSubscriptionClientMutation,
  useArchiveSubscriptionClientMutation,
  useListSubscriptionsQuery,
  useGetSubscriptionQuery,
  useCreateSubscriptionMutation,
  useCancelSubscriptionMutation,
  useReactivateSubscriptionMutation,
  useRetryChargeMutation,
  useListSubscriptionInvoicesQuery,
  useListSubscriptionActivityQuery,
} = subscriptionsApi;

export default subscriptionsApi;
