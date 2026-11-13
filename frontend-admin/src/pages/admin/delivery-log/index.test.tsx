import { describe, it, expect, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { DeliveryLogEntry } from 'types/notification';
import DeliveryLogPage from './index';

const rows: DeliveryLogEntry[] = [
  {
    id: 'n1',
    channel: 'email',
    type: 'transactional',
    category: 'auth.reset_password',
    recipientAddress: 'blocked@example.test',
    status: 'suppressed',
    error: 'address_invalid',
    createdAt: '2026-11-13T10:00:00Z'
  },
  {
    id: 'n2',
    channel: 'email',
    type: 'marketing',
    category: 'marketing.newsletter',
    recipientAddress: 'optout@example.test',
    status: 'suppressed',
    error: 'marketing_optout',
    createdAt: '2026-11-13T09:00:00Z'
  },
  {
    id: 'n3',
    channel: 'email',
    type: 'marketing',
    category: 'marketing.newsletter',
    recipientAddress: 'broken@example.test',
    status: 'failed',
    senderSlug: 'campagne',
    error: 'sender=campagne err=one_click_unavailable',
    createdAt: '2026-11-13T08:00:00Z'
  },
  {
    id: 'n4',
    channel: 'email',
    type: 'transactional',
    category: 'auth.verify_email',
    recipientAddress: 'ok@example.test',
    status: 'sent',
    createdAt: '2026-11-13T07:00:00Z'
  }
];

let listUrls: URL[];

beforeEach(() => {
  listUrls = [];
  server.use(
    http.get(url('/v1/notifications'), ({ request }) => {
      listUrls.push(new URL(request.url));
      return HttpResponse.json({ items: rows, total: 4 });
    })
  );
});

const render = (entry = '/admin/delivery-log') =>
  renderWithProviders(<DeliveryLogPage />, { routerEntries: [entry] });

const lastQuery = () => listUrls[listUrls.length - 1].searchParams;

describe('DeliveryLogPage', () => {
  it('shows each row with its status and a readable reason', async () => {
    render();
    const blocked = (await screen.findByText('blocked@example.test')).closest(
      'tr'
    )!;
    expect(within(blocked).getByText('Suppressed')).toBeInTheDocument();
    expect(
      within(blocked).getByText('Address marked invalid')
    ).toBeInTheDocument();
    expect(
      within(blocked).getByText('auth.reset_password')
    ).toBeInTheDocument();

    const optout = screen.getByText('optout@example.test').closest('tr')!;
    expect(
      within(optout).getByText('Unsubscribed from marketing')
    ).toBeInTheDocument();

    // A failed send keeps the module's bounded diagnostic as it is.
    const broken = screen.getByText('broken@example.test').closest('tr')!;
    expect(within(broken).getByText('Failed')).toBeInTheDocument();
    expect(
      within(broken).getByText('sender=campagne err=one_click_unavailable')
    ).toBeInTheDocument();

    const ok = screen.getByText('ok@example.test').closest('tr')!;
    expect(within(ok).getByText('Sent')).toBeInTheDocument();
  });

  it('sends the filters of the URL and pages server-side', async () => {
    render(
      '/admin/delivery-log?status=suppressed&type=transactional&category=auth.reset_password&q=blocked&page=3&pageSize=10'
    );
    await waitFor(() => expect(listUrls.length).toBeGreaterThan(0));
    const sp = lastQuery();
    expect(sp.get('status')).toBe('suppressed');
    expect(sp.get('type')).toBe('transactional');
    expect(sp.get('category')).toBe('auth.reset_password');
    expect(sp.get('address')).toBe('blocked');
    expect(sp.get('skip')).toBe('20');
    expect(sp.get('limit')).toBe('10');
  });

  it('ignores a filter value outside the known set', async () => {
    render('/admin/delivery-log?status=bogus&type=nope');
    await waitFor(() => expect(listUrls.length).toBeGreaterThan(0));
    const sp = lastQuery();
    expect(sp.get('status')).toBeNull();
    expect(sp.get('type')).toBeNull();
    expect(sp.get('skip')).toBe('0');
    expect(sp.get('limit')).toBe('25');
  });

  it('filters by status from the select and goes back to page 1', async () => {
    render('/admin/delivery-log?page=2');
    await screen.findByText('blocked@example.test');
    await userEvent.selectOptions(
      screen.getByRole('combobox', { name: 'Status' }),
      'failed'
    );
    await waitFor(() => expect(lastQuery().get('status')).toBe('failed'));
    expect(lastQuery().get('skip')).toBe('0');
  });

  it('says so when no row matches the filters', async () => {
    server.use(
      http.get(url('/v1/notifications'), () =>
        HttpResponse.json({ items: [], total: 0 })
      )
    );
    render('/admin/delivery-log?status=queued');
    expect(
      await screen.findByText('No row matches the filters.')
    ).toBeInTheDocument();
  });
});
