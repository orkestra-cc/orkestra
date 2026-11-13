import { baseApi } from './baseApi';
import type {
  DeliveryLogArgs,
  DeliveryLogPage
} from '../../types/notification';

// notificationLogApi reads the delivery log of the notification module:
// GET /v1/notifications, newest first, one page per request with the total
// the filters match. Gated by notification.log.read (a system permission).
// Read-only: there is nothing to invalidate.
export const notificationLogApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    listDeliveryLog: builder.query<DeliveryLogPage, DeliveryLogArgs>({
      query: ({ status, type, category, address, skip, limit }) => ({
        url: '/v1/notifications',
        method: 'GET',
        params: {
          status: status || undefined,
          type: type || undefined,
          category: category || undefined,
          address: address || undefined,
          skip,
          limit
        }
      }),
      transformResponse: (r: Partial<DeliveryLogPage>) => ({
        items: r.items ?? [],
        total: r.total ?? 0
      })
    })
  })
});

export const { useListDeliveryLogQuery } = notificationLogApi;
