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
  userUuid: '11111111-1111-4111-8111-111111111111',
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
  it('rejects a malformed subject with a field error and loads nothing', async () => {
    let calls = 0;
    server.use(
      http.get(url(`${P}/preview`), () => {
        calls++;
        return HttpResponse.json(preview());
      })
    );
    render('/?tab=subject');
    await userEvent.type(
      screen.getByLabelText(/subject useruuid/i),
      'not-a-uuid'
    );
    await userEvent.click(screen.getByRole('button', { name: /^load$/i }));
    expect(await screen.findByText(/valid user uuid/i)).toBeInTheDocument();
    expect(calls).toBe(0);
    expect(screen.getByTestId('loc').textContent).not.toContain('userUuid');
  });

  it('explains each hold, marking an expired one as no longer blocking', async () => {
    server.use(
      http.get(url(`${P}/preview`), () =>
        HttpResponse.json(
          preview({
            legalHolds: [
              {
                uuid: 'h1',
                reason: 'live hold',
                caseRef: 'CASE-7',
                expiresAt: '2999-01-01T00:00:00Z',
                reviewDue: true,
                expired: false,
                active: true
              },
              {
                uuid: 'h2',
                reason: 'old hold',
                expired: true,
                reviewDue: false,
                active: true
              }
            ]
          })
        )
      ),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: [] }))
    );
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
    const live = await screen.findByText(/live hold/);
    expect(live.textContent).toMatch(/CASE-7/);
    expect(within(live).getByText(/review due/i)).toBeInTheDocument();
    const old = screen.getByText(/old hold/).closest('li')!;
    expect(within(old).getByText(/no longer blocking/i)).toBeInTheDocument();
    expect(
      within(live.closest('li')!).queryByText(/no longer blocking/i)
    ).toBeNull();
  });

  it('makes no request without a subject and writes the subject to the URL on load', async () => {
    let previewCalls = 0;
    server.use(
      http.get(url(`${P}/preview`), ({ request }) => {
        previewCalls++;
        expect(new URL(request.url).searchParams.get('userUuid')).toBe(
          '99999999-9999-4999-8999-999999999999'
        );
        return HttpResponse.json(
          preview({ userUuid: '99999999-9999-4999-8999-999999999999' })
        );
      }),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: [] }))
    );
    render('/?tab=subject');
    expect(previewCalls).toBe(0);
    await userEvent.type(
      screen.getByLabelText(/subject useruuid/i),
      ' 99999999-9999-4999-8999-999999999999 '
    );
    await userEvent.click(screen.getByRole('button', { name: /^load$/i }));
    await screen.findByText('auth');
    expect(screen.getByTestId('loc').textContent).toContain(
      'userUuid=99999999-9999-4999-8999-999999999999'
    );
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
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
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
            userUuid: '11111111-1111-4111-8111-111111111111',
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
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
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
        userUuid: '11111111-1111-4111-8111-111111111111',
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
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
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

  it('downloads through the route then follows the URL with a synthetic link', async () => {
    const clicked: HTMLAnchorElement[] = [];
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(function (this: HTMLAnchorElement) {
        clicked.push(this);
      });
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () =>
        HttpResponse.json({
          items: [
            {
              uuid: 'x1',
              userUuid: '11111111-1111-4111-8111-111111111111',
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
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
    await userEvent.click(
      await screen.findByRole('button', { name: /download/i })
    );
    await waitFor(() => expect(click).toHaveBeenCalled());
    expect(clicked[0].href).toBe('https://signed.example/x');
    expect(clicked[0].target).toBe('_blank');
    expect(clicked[0].rel).toBe('noopener');
  });

  it('disables the download of an export past its expiry', async () => {
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () =>
        HttpResponse.json({
          items: [
            {
              uuid: 'x1',
              userUuid: '11111111-1111-4111-8111-111111111111',
              format: 'json',
              requestedBy: 'op',
              reason: 'r',
              createdAt: '2020-01-01T10:00:00Z',
              expiresAt: '2020-02-01T10:00:00Z',
              sizeBytes: 5
            }
          ]
        })
      )
    );
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
    expect(
      await screen.findByRole('button', { name: /download/i })
    ).toBeDisabled();
    expect(screen.getByText(/expired/i)).toBeInTheDocument();
  });

  it('lower-cases a typed subject before loading it', async () => {
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () => HttpResponse.json({ items: [] }))
    );
    render('/?tab=subject');
    await userEvent.type(
      screen.getByLabelText(/subject useruuid/i),
      '11111111-AAAA-4111-8111-111111111111'
    );
    await userEvent.click(screen.getByRole('button', { name: /^load$/i }));
    await waitFor(() =>
      expect(screen.getByTestId('loc').textContent).toContain(
        'userUuid=11111111-aaaa-4111-8111-111111111111'
      )
    );
  });

  it('shows the expired message on a 410 download', async () => {
    const clicked: HTMLAnchorElement[] = [];
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(function (this: HTMLAnchorElement) {
        clicked.push(this);
      });
    server.use(
      http.get(url(`${P}/preview`), () => HttpResponse.json(preview())),
      http.get(url(`${P}/exports`), () =>
        HttpResponse.json({
          items: [
            {
              uuid: 'x1',
              userUuid: '11111111-1111-4111-8111-111111111111',
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
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
    await userEvent.click(
      await screen.findByRole('button', { name: /download/i })
    );
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('This export has expired')
    );
    expect(click).not.toHaveBeenCalled();
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
    render('/?tab=subject&userUuid=11111111-1111-4111-8111-111111111111');
    await screen.findByText('auth');
    await userEvent.click(
      screen.getByRole('button', { name: /open erasure request/i })
    );
    expect(
      screen.getByLabelText(/subject useruuid/i, {
        selector: '#open-erasure-user'
      })
    ).toHaveValue('11111111-1111-4111-8111-111111111111');
    await userEvent.type(
      screen.getByLabelText(/^reason$/i, { selector: 'textarea' }),
      'art 17'
    );
    await userEvent.click(
      screen.getByRole('button', { name: /open request/i })
    );
    await waitFor(() =>
      expect(body).toEqual({
        userUuid: '11111111-1111-4111-8111-111111111111',
        mode: 'hard_delete',
        reason: 'art 17'
      })
    );
  });
});
