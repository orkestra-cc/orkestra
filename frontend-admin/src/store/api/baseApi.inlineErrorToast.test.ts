import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { toast } from 'react-toastify';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';
import { setupApi } from './setupApi';

// The generic "Server error" toast fires for every 5xx unless the endpoint
// explicitly opts into rendering its errors inline with
// extraOptions.suppressErrorToast. The error still reaches the caller.
vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const probeApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    defaultErrorProbe: builder.query<unknown, string>({
      query: url => ({ url, method: 'GET' })
    }),
    inlineErrorProbe: builder.query<unknown, string>({
      extraOptions: { suppressErrorToast: true },
      query: url => ({ url, method: 'GET' })
    })
  }),
  overrideExisting: true
});

const callWith503 = async (inline: boolean) => {
  server.use(
    http.get('*/v1/probe/server-error', () =>
      HttpResponse.json(
        { code: 'something.else', detail: 'x' },
        { status: 503 }
      )
    )
  );
  const store = setupStore();
  // Setup marked complete so the first-install gate is not in play.
  await store.dispatch(
    setupApi.util.upsertQueryData('getSetupStatus', undefined, {
      setupCompleted: true,
      phase: 'complete',
      smtpConfigured: true
    })
  );
  const endpoint = inline
    ? probeApi.endpoints.inlineErrorProbe
    : probeApi.endpoints.defaultErrorProbe;
  const request = store.dispatch(endpoint.initiate('/v1/probe/server-error'));
  expect((await request).error).toMatchObject({ status: 503 });
  request.unsubscribe();
};

describe('5xx toast and inline error handlers', () => {
  beforeEach(() => {
    vi.mocked(toast.error).mockClear();
  });

  it('toasts once for a 5xx by default', async () => {
    await callWith503(false);
    expect(toast.error).toHaveBeenCalledTimes(1);
  });

  it('keeps a server error available to an inline handler without a toast', async () => {
    await callWith503(true);
    expect(toast.error).not.toHaveBeenCalled();
  });
});
