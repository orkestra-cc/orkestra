import { describe, it, expect, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { waitFor } from '@testing-library/react';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { tenantApi } from './tenantApi';
import { completeStepUp, subscribeStepUp } from '../stepUp';

describe('tenantApi.setDefaultTenant', () => {
  it('PUTs /v1/admin/tenants/default with {tenantId} in the body', async () => {
    let capturedMethod = '';
    let capturedBody: unknown = null;
    server.use(
      http.put('*/v1/admin/tenants/default', async ({ request }) => {
        capturedMethod = request.method;
        capturedBody = await request.json();
        return new HttpResponse(null, { status: 204 });
      })
    );

    const store = setupStore();
    const result = await store.dispatch(
      tenantApi.endpoints.setDefaultTenant.initiate({ tenantId: 'tenant-1' })
    );

    expect(result.error).toBeUndefined();
    expect(capturedMethod).toBe('PUT');
    expect(capturedBody).toEqual({ tenantId: 'tenant-1' });
  });

  // Regression pin mirroring setupApi's "createInitialAdmin — Setup
  // invalidation regression pin": keep a live (never-unsubscribed)
  // listAllOrgsAdmin subscription so invalidatesTags triggers an automatic
  // background refetch rather than just marking a stale entry nobody reads.
  it('invalidates the admin tenant list after a successful transfer', async () => {
    let listCalls = 0;
    server.use(
      http.get('*/v1/admin/tenants', () => {
        listCalls += 1;
        return HttpResponse.json({ tenants: [] });
      }),
      http.put(
        '*/v1/admin/tenants/default',
        () => new HttpResponse(null, { status: 204 })
      )
    );

    const store = setupStore();
    store.dispatch(tenantApi.endpoints.listAllOrgsAdmin.initiate());
    await waitFor(() => expect(listCalls).toBe(1));

    await store.dispatch(
      tenantApi.endpoints.setDefaultTenant.initiate({ tenantId: 'tenant-1' })
    );

    await waitFor(() => expect(listCalls).toBe(2));
  });
});

describe('tenantApi.setDefaultTenant — step-up MFA replay', () => {
  // Each test must start with no outstanding pending promise from a
  // previous one — same drain idiom as stepUp.test.ts.
  beforeEach(() => {
    completeStepUp(false);
  });

  // Backend note (task brief): RequireMFA's sendMFARequired emits
  // code:"step_up_required" — the SAME code the existing global
  // baseApi.ts:298-308 branch + StepUpModal already handle for every
  // admin route. This proves the replay goes through THAT shared path
  // (subscribeStepUp/completeStepUp, exactly like stepUp.test.ts) rather
  // than any bespoke retry mechanism invented for this mutation.
  it('replays through the existing step-up interceptor on a 401 step_up_required', async () => {
    let attempts = 0;
    server.use(
      http.put('*/v1/admin/tenants/default', () => {
        attempts += 1;
        if (attempts === 1) {
          return HttpResponse.json(
            { code: 'step_up_required', detail: 'MFA required' },
            { status: 401 }
          );
        }
        return new HttpResponse(null, { status: 204 });
      })
    );

    const openEvents: boolean[] = [];
    const unsub = subscribeStepUp(open => {
      openEvents.push(open);
      if (open) {
        // Defer to the next microtask, standing in for the operator
        // completing MFA in StepUpModal (always after an async verify
        // call in real life). Calling completeStepUp synchronously here
        // would race requestStepUp's own listener-notify loop — it nulls
        // module-level `pending` before requestStepUp's `return pending`
        // runs, same hazard requestStepUp/completeStepUp are written to
        // avoid for real callers, which are never this synchronous.
        queueMicrotask(() => completeStepUp(true));
      }
    });

    const store = setupStore();
    const result = await store.dispatch(
      tenantApi.endpoints.setDefaultTenant.initiate({ tenantId: 'tenant-1' })
    );
    unsub();

    expect(attempts).toBe(2);
    expect(openEvents).toEqual([true, false]);
    expect(result.error).toBeUndefined();
  });

  it('does not replay and surfaces the error when the operator cancels step-up', async () => {
    let attempts = 0;
    server.use(
      http.put('*/v1/admin/tenants/default', () => {
        attempts += 1;
        return HttpResponse.json(
          { code: 'step_up_required', detail: 'MFA required' },
          { status: 401 }
        );
      })
    );

    const unsub = subscribeStepUp(open => {
      if (open) completeStepUp(false);
    });

    const store = setupStore();
    const result = await store.dispatch(
      tenantApi.endpoints.setDefaultTenant.initiate({ tenantId: 'tenant-1' })
    );
    unsub();

    expect(attempts).toBe(1);
    expect(result.error).toBeDefined();
  });
});
