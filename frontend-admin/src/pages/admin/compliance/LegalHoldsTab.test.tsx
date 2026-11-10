import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { toast } from 'react-toastify';
import LegalHoldsTab from './LegalHoldsTab';

vi.mock('react-toastify', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));
beforeEach(() => vi.clearAllMocks());

const L = '/v1/admin/compliance/legal-holds';
const hold = (over: Record<string, unknown> = {}) => ({
  uuid: 'h1',
  userUuid: 'u-1',
  reason: 'litigation',
  placedBy: 'op',
  placedAt: '2026-06-01T10:00:00Z',
  active: true,
  reviewDue: false,
  expired: false,
  ...over
});
const rowOf = (s: string) =>
  within(screen.getByText(s).closest('tr') as HTMLElement);

describe('LegalHoldsTab', () => {
  it('requires a release reason and sends it', async () => {
    let body: unknown = 'unset';
    server.use(
      http.get(url(L), () => HttpResponse.json({ items: [hold()] })),
      http.delete(url(`${L}/:id`), async ({ request }) => {
        body = await request.json();
        return new HttpResponse(null, { status: 204 });
      })
    );
    renderWithProviders(<LegalHoldsTab />);
    await screen.findByText('u-1');
    await userEvent.click(
      rowOf('u-1').getByRole('button', { name: /release/i })
    );
    const dialog = screen.getByRole('dialog');
    await userEvent.click(
      within(dialog).getByRole('button', { name: /^release$/i })
    );
    expect(body).toBe('unset');
    await userEvent.type(within(dialog).getByRole('textbox'), 'case closed');
    await userEvent.click(
      within(dialog).getByRole('button', { name: /^release$/i })
    );
    await waitFor(() => expect(body).toEqual({ releaseReason: 'case closed' }));
  });

  it('badges a hold whose review is due', async () => {
    server.use(
      http.get(url(L), () =>
        HttpResponse.json({
          items: [hold({ reviewDue: true, reviewAt: '2026-05-01T00:00:00Z' })]
        })
      )
    );
    renderWithProviders(<LegalHoldsTab />);
    await screen.findByText('u-1');
    expect(rowOf('u-1').getByText(/review due/i)).toBeInTheDocument();
  });

  it('asks for released holds only when the toggle is on', async () => {
    const seen: (string | null)[] = [];
    server.use(
      http.get(url(L), ({ request }) => {
        seen.push(new URL(request.url).searchParams.get('includeReleased'));
        return HttpResponse.json({ items: [hold()] });
      })
    );
    renderWithProviders(<LegalHoldsTab />);
    await screen.findByText('u-1');
    await userEvent.click(screen.getByLabelText(/show released/i));
    await waitFor(() => expect(seen).toContain('true'));
    expect(seen[0]).toBeNull();
  });

  it('places a hold with review and expiry dates and maps a past expiry', async () => {
    let body: unknown;
    server.use(
      http.get(url(L), () => HttpResponse.json({ items: [] })),
      http.post(url(L), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(
          { code: 'compliance.legal_hold_expiry_past' },
          { status: 422 }
        );
      })
    );
    renderWithProviders(<LegalHoldsTab />);
    await userEvent.type(
      await screen.findByPlaceholderText(/e\.g\. 7f3c/i),
      'u-2'
    );
    await userEvent.type(
      screen.getByPlaceholderText(/litigation hold reason/i),
      'dispute'
    );
    await userEvent.type(screen.getByLabelText(/review date/i), '2030-01-02');
    await userEvent.type(screen.getByLabelText(/expiry date/i), '2031-01-02');
    await userEvent.click(screen.getByRole('button', { name: /place hold/i }));
    await waitFor(() =>
      expect(body).toEqual({
        userUuid: 'u-2',
        reason: 'dispute',
        reviewAt: new Date(2030, 0, 2).toISOString(),
        // the whole chosen day: next local midnight
        expiresAt: new Date(2031, 0, 3).toISOString()
      })
    );
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'The expiry date must be in the future'
      )
    );
  });

  it('edits dates and clears the expiry', async () => {
    let body: unknown;
    server.use(
      http.get(url(L), () =>
        HttpResponse.json({
          items: [hold({ expiresAt: '2031-01-02T00:00:00Z' })]
        })
      ),
      http.patch(url(`${L}/:id`), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(hold());
      })
    );
    renderWithProviders(<LegalHoldsTab />);
    await screen.findByText('u-1');
    await userEvent.click(rowOf('u-1').getByRole('button', { name: /edit/i }));
    await userEvent.click(
      screen.getByRole('button', { name: /clear the expiry date/i })
    );
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(body).toEqual({ clearExpiresAt: true }));
  });

  it('disables save when nothing changed and shows the stored local day', async () => {
    server.use(
      http.get(url(L), () =>
        HttpResponse.json({
          items: [
            hold({
              reviewAt: new Date(2030, 4, 1).toISOString(),
              // end of 5 June local: stored as the next local midnight
              expiresAt: new Date(2030, 5, 6).toISOString()
            })
          ]
        })
      )
    );
    renderWithProviders(<LegalHoldsTab />);
    await screen.findByText('u-1');
    await userEvent.click(rowOf('u-1').getByRole('button', { name: /edit/i }));
    expect(
      screen.getByLabelText(/^review date$/i, { selector: '#hold-edit-review' })
    ).toHaveValue('2030-05-01');
    expect(
      screen.getByLabelText(/^expiry date$/i, { selector: '#hold-edit-expiry' })
    ).toHaveValue('2030-06-05');
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled();
  });

  it('lists notes and adds one', async () => {
    let body: unknown;
    let notes = [
      { id: 'n1', by: 'op', at: '2026-06-02T10:00:00Z', text: 'first' }
    ];
    server.use(
      http.get(url(L), () => HttpResponse.json({ items: [hold({ notes })] })),
      http.post(url(`${L}/:id/notes`), async ({ request }) => {
        body = await request.json();
        notes = [
          ...notes,
          { id: 'n2', by: 'op', at: '2026-06-03T10:00:00Z', text: 'second' }
        ];
        return HttpResponse.json(hold({ notes }));
      })
    );
    renderWithProviders(<LegalHoldsTab />);
    await screen.findByText('u-1');
    await userEvent.click(rowOf('u-1').getByRole('button', { name: /notes/i }));
    await screen.findByText('first');
    await userEvent.type(screen.getByLabelText(/add a note/i), 'second');
    await userEvent.click(screen.getByRole('button', { name: /add note/i }));
    await waitFor(() => expect(body).toEqual({ text: 'second' }));
    await screen.findByText('second');
  });
});
