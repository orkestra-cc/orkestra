import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  billingStatsHandler,
  capturedRequests,
  emptyBillingStats,
} from 'test/handlers';
import ReceivedInvoiceGreetings from './ReceivedInvoiceGreetings';

describe('ReceivedInvoiceGreetings', () => {
  // Same regression guard as IssuedInvoiceGreetings.test.tsx — the
  // received-invoices table has no date filter, so the rollup card must
  // request an all-time window or the counters silently read zero
  // outside the current month.
  it('requests all-time stats so its counters mirror the un-date-scoped table', async () => {
    server.use(
      billingStatsHandler({
        ...emptyBillingStats,
        receivedTotal: 5,
        receivedPending: 2,
        receivedAccepted: 2,
        receivedRejected: 1,
        receivedAmount: 9876.54,
      }),
    );

    renderWithProviders(<ReceivedInvoiceGreetings />);

    await screen.findByText(/Volume:/i);

    await waitFor(() => {
      expect(capturedRequests.billingStatsParams).not.toBeNull();
    });
    const params = capturedRequests.billingStatsParams!;
    const fromDate = params.get('fromDate');
    expect(fromDate, 'must pass fromDate, otherwise backend defaults to current month').toBeTruthy();
    expect(new Date(fromDate!).getUTCFullYear()).toBeLessThanOrEqual(2010);
  });
});
