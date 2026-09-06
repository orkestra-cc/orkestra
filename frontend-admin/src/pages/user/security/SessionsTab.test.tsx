import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { mySessionsHandler, url } from 'test/handlers';
import SessionsTab from './SessionsTab';

const sampleSessions = {
  sessions: [
    {
      sessionId: 's-current',
      deviceId: 'd-cur',
      deviceName: 'Current Device',
      deviceType: 'web',
      platform: 'Chrome / macOS',
      ipAddress: '10.0.0.1',
      lastActivity: '2026-05-10T10:00:00Z',
      createdAt: '2026-05-10T09:00:00Z',
      expiresAt: '2026-06-10T00:00:00Z',
      isCurrent: true
    },
    {
      sessionId: 's-other',
      deviceId: 'd-other',
      deviceName: 'iPhone',
      deviceType: 'mobile',
      platform: 'iOS Safari',
      ipAddress: '10.0.0.2',
      lastActivity: '2026-05-09T15:00:00Z',
      createdAt: '2026-05-09T14:00:00Z',
      expiresAt: '2026-06-09T00:00:00Z',
      isCurrent: false
    }
  ],
  activeCount: 2
};

describe('SessionsTab', () => {
  it('renders the active sessions and badges the current row', async () => {
    server.use(mySessionsHandler(sampleSessions));
    renderWithProviders(<SessionsTab />);

    expect(await screen.findByText(/current device/i)).toBeInTheDocument();
    expect(screen.getByText(/iphone/i)).toBeInTheDocument();
    expect(screen.getAllByText(/current/i).length).toBeGreaterThan(0);
  });

  it('disables revoke on the current session row', async () => {
    server.use(mySessionsHandler(sampleSessions));
    renderWithProviders(<SessionsTab />);

    await screen.findByText(/current device/i);

    // Two revoke buttons, one per row. The current row's button is disabled.
    const revokeButtons = screen.getAllByRole('button', { name: /^revoke$/i });
    expect(revokeButtons).toHaveLength(2);
    const currentRow = screen.getByText(/current device/i).closest('tr')!;
    const otherRow = screen.getByText(/iphone/i).closest('tr')!;
    expect(currentRow.querySelector('button')).toBeDisabled();
    expect(otherRow.querySelector('button')).not.toBeDisabled();
  });

  // The pane moved from raw <table> markup to the console's AdvanceTable
  // shell, so the search box is new behaviour worth pinning: sessions grow
  // without bound and filtering is the only way through a long list.
  it('filters rows through the AdvanceTable search box', async () => {
    server.use(mySessionsHandler(sampleSessions));
    renderWithProviders(<SessionsTab />);
    const user = userEvent.setup();

    await screen.findByText(/current device/i);
    await user.type(screen.getByPlaceholderText(/search sessions/i), 'iPhone');

    await waitFor(() => {
      expect(screen.queryByText(/current device/i)).not.toBeInTheDocument();
    });
    expect(screen.getByText(/iphone/i)).toBeInTheDocument();
  });

  // The global filter matches cell VALUES, not rendered text, so a date column
  // accessored on the raw ISO string is searchable only by a string the
  // operator never sees — on staging, typing the "10:0" printed in the cell
  // matched nothing while the UTC "08:0" behind it returned that very row.
  //
  // Search on the MONTH NAME, not the time: the runner is UTC, where a
  // rendered "10:00" is byte-identical to the ISO "10:00" behind it and the
  // bug is invisible. A month name appears in the rendered cell and never in
  // an ISO string, in any locale or zone.
  it('matches the date text the operator can actually see', async () => {
    server.use(
      mySessionsHandler({
        sessions: [
          // BOTH date cells of a row must share a month: the global filter
          // spans every cell, so an August row still holding a May
          // "Last active" would match a search for "May".
          {
            ...sampleSessions.sessions[0],
            lastActivity: '2026-05-10T10:00:00Z',
            createdAt: '2026-05-10T09:00:00Z'
          },
          {
            ...sampleSessions.sessions[1],
            lastActivity: '2026-08-09T15:00:00Z',
            createdAt: '2026-08-09T14:00:00Z'
          }
        ],
        activeCount: 2
      })
    );
    renderWithProviders(<SessionsTab />);
    const user = userEvent.setup();

    const currentRow = (await screen.findByText(/current device/i)).closest(
      'tr'
    )!;
    // 4th cell = "Started". Take whatever month token this locale renders.
    const month = currentRow
      .querySelectorAll('td')[3]
      .textContent!.match(/[A-Za-z]{3,}/)![0];

    await user.type(screen.getByPlaceholderText(/search sessions/i), month);

    await waitFor(() => {
      expect(screen.queryByText(/iphone/i)).not.toBeInTheDocument();
    });
    expect(screen.getByText(/current device/i)).toBeInTheDocument();
  });

  it('sorts the date column chronologically, not lexicographically', async () => {
    // Sep 2026 is OLDER than Jan 2027 but sorts AFTER it as text ("J" < "S"),
    // so the two orders disagree and a formatted-string comparator flips them.
    // (A Dec/Jan pair would not: there the two orders happen to agree, and the
    // test would pass with the comparator removed.)
    server.use(
      mySessionsHandler({
        sessions: [
          {
            ...sampleSessions.sessions[0],
            sessionId: 's-sep',
            deviceName: 'Older Box',
            isCurrent: false,
            createdAt: '2026-09-05T10:00:00Z'
          },
          {
            ...sampleSessions.sessions[1],
            sessionId: 's-jan',
            deviceName: 'Newer Box',
            createdAt: '2027-01-05T10:00:00Z'
          }
        ],
        activeCount: 2
      })
    );
    renderWithProviders(<SessionsTab />);
    const user = userEvent.setup();

    await screen.findByText(/older box/i);
    await user.click(screen.getByText(/^started$/i));

    await waitFor(() => {
      const names = [...document.querySelectorAll('tbody tr')].map(r =>
        r.querySelector('td')!.textContent!.trim()
      );
      expect(names).toEqual(['Older Box', 'Newer Box']);
    });
  });

  it('revokes a non-current session and removes its row from the list', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/auth/operator/me/sessions'), () => {
        calls++;
        if (calls === 1) return HttpResponse.json(sampleSessions);
        return HttpResponse.json({
          sessions: sampleSessions.sessions.filter(s => s.isCurrent),
          activeCount: 1
        });
      }),
      http.delete(
        url('/v1/auth/operator/me/sessions/s-other'),
        () => new HttpResponse(null, { status: 204 })
      )
    );

    renderWithProviders(<SessionsTab />);
    const user = userEvent.setup();

    await screen.findByText(/iphone/i);
    const otherRow = screen.getByText(/iphone/i).closest('tr')!;
    await user.click(otherRow.querySelector('button')!);

    await waitFor(() => {
      expect(screen.queryByText(/iphone/i)).not.toBeInTheDocument();
    });
    expect(screen.getByText(/current device/i)).toBeInTheDocument();
  });
});
