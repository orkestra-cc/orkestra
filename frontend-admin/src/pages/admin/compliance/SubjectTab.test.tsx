import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useLocation } from 'react-router';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { toast } from 'react-toastify';
import SubjectTab from './SubjectTab';

vi.mock('react-toastify', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => ({ currentUser: { id: 'op-me' } })
}));
beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

const P = '/v1/admin/compliance/dsr';
const preview = (over: Record<string, unknown> = {}) => ({
  userUuid: 'u-1',
  legalHolds: [],
  retention: null,
  openRequests: [],
  producers: [
    { key: 'auth', countable: true, rows: 3, collections: ['users'] },
    { key: 'legacy', countable: false, rows: 0, collections: null },
    {
      key: 'crm',
      countable: true,
      rows: 0,
      collections: ['contacts'],
      error: 'count failed'
    }
  ],
  hardDeleteAllowed: true,
  anonymizeAllowed: true,
  ...over
});
const Loc = () => <span data-testid="loc">{useLocation().search}</span>;
const render = (entry: string) =>
  renderWithProviders(
    <>
      <SubjectTab />
      <Loc />
    </>,
    { routerEntries: [entry] }
  );

describe('SubjectTab', () => {
  it('makes no request without a subject and writes the subject to the URL on load', async () => {
    let previewCalls = 0;
    server.use(
      http.get(url(`${P}/preview`), ({ request }) => {
        previewCalls++;
        expect(new URL(request.url).searchParams.get('userUuid')).toBe('u-9');
        return HttpResponse.json(preview({ userUuid: 'u-9' }));
      }),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: [] }))
    );
    render('/?tab=subject');
    expect(previewCalls).toBe(0);
    await userEvent.type(screen.getByLabelText(/subject useruuid/i), ' u-9 ');
    await userEvent.click(screen.getByRole('button', { name: /^load$/i }));
    await screen.findByText('auth');
    expect(screen.getByTestId('loc').textContent).toContain('userUuid=u-9');
    expect(screen.getByTestId('loc').textContent).toContain('tab=subject');
  });

  it('renders flags, retention, producers and never sums rows', async () => {
    server.use(
      http.get(url(`${P}/preview`), () =>
        HttpResponse.json(
          preview({
            hardDeleteAllowed: false,
            legalHolds: [{ uuid: 'h1', reason: 'litigation X', active: true }],
            retention: {
              retainUntil: '2030-01-01T00:00:00Z',
              reasons: ['minimum_retention']
            }
          })
        )
      ),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: null }))
    );
    render('/?tab=subject&userUuid=u-1');
    await screen.findByText('auth');
    expect(screen.getByText(/hard delete blocked/i)).toBeInTheDocument();
    expect(screen.getByText(/anonymize allowed/i)).toBeInTheDocument();
    expect(screen.getByText(/litigation X/)).toBeInTheDocument();
    expect(screen.getByText(/keep until/i)).toBeInTheDocument();
    const legacy = within(screen.getByText('legacy').closest('tr')!);
    expect(legacy.getByText(/not countable/i)).toBeInTheDocument();
    expect(screen.getByText('count failed')).toBeInTheDocument();
    expect(screen.getByText(/not added into a total/i)).toBeInTheDocument();
    expect(screen.queryByText(/^total/i)).not.toBeInTheDocument();
  });

  it('creates an export with a required reason, then lists it', async () => {
    let body: unknown;
    let items: Record<string, unknown>[] = [];
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items })),
      http.post(url(`${P}/exports`), async ({ request }) => {
        body = await request.json();
        items = [
          {
            uuid: 'x1',
            userUuid: 'u-1',
            format: 'csv',
            requestedBy: 'op-me',
            reason: 'subject asked',
            createdAt: '2026-06-01T10:00:00Z',
            expiresAt: '2999-01-01T00:00:00Z',
            sizeBytes: 10
          }
        ];
        return HttpResponse.json(items[0]);
      })
    );
    render('/?tab=subject&userUuid=u-1');
    await screen.findByText('auth');
    await userEvent.click(screen.getByRole('button', { name: /export csv/i }));
    await userEvent.click(
      screen.getByRole('button', { name: /create export/i })
    );
    expect(body).toBeUndefined(); // empty reason refused client-side
    await userEvent.type(screen.getByLabelText(/reason/i), 'subject asked');
    await userEvent.click(
      screen.getByRole('button', { name: /create export/i })
    );
    await waitFor(() =>
      expect(body).toEqual({
        userUuid: 'u-1',
        format: 'csv',
        reason: 'subject asked'
      })
    );
    await screen.findByText('subject asked');
    expect(toast.success).toHaveBeenCalled();
  });

  it('maps a storage outage to its message and keeps the modal open', async () => {
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: [] })),
      http.post(url(`${P}/exports`), () =>
        HttpResponse.json(
          { code: 'compliance.export_storage_unavailable' },
          { status: 503 }
        )
      )
    );
    render('/?tab=subject&userUuid=u-1');
    await screen.findByText('auth');
    await userEvent.click(screen.getByRole('button', { name: /export json/i }));
    await userEvent.type(screen.getByLabelText(/reason/i), 'why');
    await userEvent.click(
      screen.getByRole('button', { name: /create export/i })
    );
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'Export storage is unavailable; try again later'
      )
    );
    expect(
      screen.getByRole('button', { name: /create export/i })
    ).toBeVisible();
  });

  it('downloads through the route then opens the URL without opener', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () =>
        HttpResponse.json({
          items: [
            {
              uuid: 'x1',
              userUuid: 'u-1',
              format: 'json',
              requestedBy: 'op',
              reason: 'r',
              createdAt: '2026-06-01T10:00:00Z',
              expiresAt: '2999-01-01T00:00:00Z',
              sizeBytes: 5
            }
          ]
        })
      ),
      http.get(url(`${P}/exports/x1/download`), () =>
        HttpResponse.json({ url: 'https://signed.example/x', expiresIn: 60 })
      )
    );
    render('/?tab=subject&userUuid=u-1');
    await userEvent.click(
      await screen.findByRole('button', { name: /download/i })
    );
    await waitFor(() =>
      expect(open).toHaveBeenCalledWith(
        'https://signed.example/x',
        '_blank',
        'noopener'
      )
    );
  });

  it('shows the expired message on a 410 download', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () =>
        HttpResponse.json({
          items: [
            {
              uuid: 'x1',
              userUuid: 'u-1',
              format: 'json',
              requestedBy: 'op',
              reason: 'r',
              createdAt: '2026-06-01T10:00:00Z',
              expiresAt: '2999-01-01T00:00:00Z',
              sizeBytes: 5
            }
          ]
        })
      ),
      http.get(url(`${P}/exports/x1/download`), () =>
        HttpResponse.json(
          { code: 'compliance.export_expired' },
          { status: 410 }
        )
      )
    );
    render('/?tab=subject&userUuid=u-1');
    await userEvent.click(
      await screen.findByRole('button', { name: /download/i })
    );
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('This export has expired')
    );
    expect(open).not.toHaveBeenCalled();
  });

  it('opens an erasure request prefilled with the subject', async () => {
    let body: unknown;
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: [] })),
      http.post(
        url('/v1/admin/compliance/erasure-requests'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({ uuid: 'er-1' });
        }
      )
    );
    render('/?tab=subject&userUuid=u-1');
    await screen.findByText('auth');
    await userEvent.click(
      screen.getByRole('button', { name: /open erasure request/i })
    );
    expect(
      screen.getByLabelText(/subject useruuid/i, {
        selector: '#open-erasure-user'
      })
    ).toHaveValue('u-1');
    await userEvent.type(
      screen.getByLabelText(/^reason$/i, { selector: 'textarea' }),
      'art 17'
    );
    await userEvent.click(
      screen.getByRole('button', { name: /open request/i })
    );
    await waitFor(() =>
      expect(body).toEqual({
        userUuid: 'u-1',
        mode: 'hard_delete',
        reason: 'art 17'
      })
    );
  });
});
