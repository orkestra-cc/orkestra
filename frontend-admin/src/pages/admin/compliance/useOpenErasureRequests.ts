import {
  complianceApi,
  useListErasureRequestsQuery,
  type ErasureRequest
} from 'store/api/complianceApi';

// The backend retry worker ticks every minute and changes incomplete requests
// without any browser mutation, so nothing would invalidate the cache.
export const ERASURE_REFRESH_MS = 30_000;

// erasureRefreshInterval keeps the refresh bounded: it polls only while some
// incomplete request still has an automatic retry scheduled (nextAttemptAt).
// A pending request waits for an operator and an exhausted one for a retry, so
// neither is changed by the worker.
export const erasureRefreshInterval = (items?: ErasureRequest[]): number =>
  items?.some(r => r.status === 'incomplete' && r.nextAttemptAt)
    ? ERASURE_REFRESH_MS
    : 0;

// useOpenErasureRequests is the erasure-request list query for both callers
// on the compliance page (KPI card and tab). It reads the cached list without
// subscribing to derive the interval, then subscribes with it, so RTK Query
// owns the polling lifecycle and stops it once nothing is scheduled.
export const useOpenErasureRequests = () => {
  const { data } = complianceApi.endpoints.listErasureRequests.useQueryState();
  return useListErasureRequestsQuery(undefined, {
    pollingInterval: erasureRefreshInterval(data?.items)
  });
};
