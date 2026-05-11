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
      isCurrent: true,
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
      isCurrent: false,
    },
  ],
  activeCount: 2,
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

  it('revokes a non-current session and removes its row from the list', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/auth/operator/me/sessions'), () => {
        calls++;
        if (calls === 1) return HttpResponse.json(sampleSessions);
        return HttpResponse.json({
          sessions: sampleSessions.sessions.filter((s) => s.isCurrent),
          activeCount: 1,
        });
      }),
      http.delete(url('/v1/auth/operator/me/sessions/s-other'), () =>
        new HttpResponse(null, { status: 204 }),
      ),
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
