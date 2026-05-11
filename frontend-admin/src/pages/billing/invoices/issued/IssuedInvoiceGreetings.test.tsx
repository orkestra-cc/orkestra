import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  billingStatsHandler,
  capturedRequests,
  emptyBillingStats,
} from 'test/handlers';
import IssuedInvoiceGreetings from './IssuedInvoiceGreetings';

describe('IssuedInvoiceGreetings', () => {
  // Regression guard for the bug where the rollup card silently scoped to
  // the current month (backend default) and read zero whenever no invoice
  // was issued this month, while the table directly below — which has no
  // date filter — showed all-time data. The card must explicitly request
  // an all-time window so the two stay consistent.
  it('requests all-time stats so its counters mirror the un-date-scoped table', async () => {
    server.use(
      billingStatsHandler({
        ...emptyBillingStats,
        issuedTotal: 6,
        issuedDraft: 1,
        issuedSent: 2,
        issuedDelivered: 3,
        issuedAmount: 12345.67,
      }),
    );

    renderWithProviders(<IssuedInvoiceGreetings />);

    // Volume only renders once the stats response lands — proves the
    // request actually fired and the data flowed into the component.
    await screen.findByText(/Volume:/i);

    await waitFor(() => {
      expect(capturedRequests.billingStatsParams).not.toBeNull();
    });
    const params = capturedRequests.billingStatsParams!;
    const fromDate = params.get('fromDate');
    expect(fromDate, 'must pass fromDate, otherwise backend defaults to current month').toBeTruthy();
    // Far enough in the past to cover any realistic invoice history.
    expect(new Date(fromDate!).getUTCFullYear()).toBeLessThanOrEqual(2010);
  });
});
