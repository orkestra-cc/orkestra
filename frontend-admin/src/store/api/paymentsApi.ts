import { baseApi } from './baseApi';
import type {
  PaymentTransaction,
  PaymentMethodRec,
  PaymentWebhookEvent,
  PaymentsListResponse,
  RefundInput,
} from '../../types/payments';

const buildQS = (params: Record<string, unknown>): string => {
  const sp = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') sp.append(k, String(v));
  });
  const qs = sp.toString();
  return qs ? `?${qs}` : '';
};

export const paymentsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    listPaymentTransactions: builder.query<
      PaymentsListResponse<PaymentTransaction>,
      { subscriptionUUID?: string; invoiceUUID?: string; tenantUUID?: string; status?: string } | undefined
    >({
      query: (params) => `/v1/payments/transactions${params ? buildQS(params) : ''}`,
      providesTags: (result) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({ type: 'PaymentTransaction' as const, id: uuid })),
              { type: 'PaymentTransaction', id: 'LIST' },
            ]
          : [{ type: 'PaymentTransaction', id: 'LIST' }],
    }),
    getPaymentTransaction: builder.query<{ body: PaymentTransaction }, string>({
      query: (id) => `/v1/payments/transactions/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'PaymentTransaction', id }],
    }),
    refundPaymentTransaction: builder.mutation<
      { body: { providerRefundID: string; status: string } },
      { id: string; input: RefundInput }
    >({
      query: ({ id, input }) => ({
        url: `/v1/payments/transactions/${id}/refund`,
        method: 'POST',
        body: input,
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'PaymentTransaction', id },
        { type: 'PaymentTransaction', id: 'LIST' },
      ],
    }),
    listPaymentMethods: builder.query<PaymentsListResponse<PaymentMethodRec>, string>({
      query: (tenantUUID) => `/v1/payments/methods${buildQS({ tenantUUID })}`,
      providesTags: [{ type: 'PaymentMethodRec', id: 'LIST' }],
    }),
    listPaymentWebhookEvents: builder.query<
      PaymentsListResponse<PaymentWebhookEvent>,
      { provider?: string; limit?: number } | undefined
    >({
      query: (params) => `/v1/payments/webhook-events${params ? buildQS(params) : ''}`,
      providesTags: [{ type: 'PaymentWebhookEvent', id: 'LIST' }],
    }),
  }),
});

export const {
  useListPaymentTransactionsQuery,
  useGetPaymentTransactionQuery,
  useRefundPaymentTransactionMutation,
  useListPaymentMethodsQuery,
  useListPaymentWebhookEventsQuery,
} = paymentsApi;

export default paymentsApi;
