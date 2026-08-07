import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  url,
  mySessionsHandler,
  emptySessions,
  trustedDevicesHandler,
  emptyTrustedDevices
} from 'test/handlers';
import UserDashboard from './UserDashboard';

// The dashboard is DEFAULT_POST_LOGIN — the first page every operator sees —
// so this guards the page actually mounting against real API shapes, not
// just the module compiling (a green typecheck has not meant a booting app
// here before).

const sessionUser = {
  id: 'user-1',
  email: 'op@example.com',
  username: 'op',
  fullName: 'Ada Lovelace',
  role: 'administrator',
  isActive: true,
  emailVerified: true,
  lastLogin: '2026-08-07T09:00:00Z',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

const stubAll = (mfa: {
  status: string;
  requiresMfa: boolean;
  backupCodesRemaining: number;
  webauthnCredentials: number;
}) => {
  server.use(
    http.get(url('/v1/auth/session'), () =>
      HttpResponse.json({
        accessToken: 'tok',
        tokenType: 'Bearer',
        expiresIn: 900,
        user: sessionUser,
        authenticated: true,
        success: true
      })
    ),
    mySessionsHandler({
      ...emptySessions,
      activeCount: 2,
      currentDevice: 'Firefox on Linux'
    } as typeof emptySessions),
    trustedDevicesHandler(emptyTrustedDevices),
    http.get(url('/v1/auth/operator/me/mfa'), () => HttpResponse.json(mfa)),
    http.get(url('/v1/tenants'), () =>
      HttpResponse.json({
        memberships: [
          {
            tenantId: 't-1',
            name: 'Acme Ops',
            slug: 'acme-ops',
            plan: 'enterprise',
            kind: 'internal',
            roles: ['administrator'],
            isOwner: true
          }
        ]
      })
    )
  );
};

describe('UserDashboard', () => {
  it('greets the operator and renders the live security digest', async () => {
    stubAll({
      status: 'required_pending_enrollment',
      requiresMfa: true,
      backupCodesRemaining: 0,
      webauthnCredentials: 0
    });
    renderWithProviders(<UserDashboard />);

    // Greeting uses the first name from the session payload.
    expect(await screen.findByText('Hi, Ada')).toBeInTheDocument();
    // Session count + current device from /me/sessions.
    expect(await screen.findByText('2')).toBeInTheDocument();
    expect(screen.getByText('Firefox on Linux')).toBeInTheDocument();
    // MFA required but not enrolled → attention ribbon + setup link.
    expect(await screen.findAllByText('Off')).not.toHaveLength(0);
    expect(screen.getByText('Set up')).toBeInTheDocument();
    const setupLink = screen.getByRole('link', { name: 'Set up now' });
    expect(setupLink).toHaveAttribute('href', '/user/security?tab=mfa');
    // Digest rows read from the session user.
    expect(screen.getByText('op@example.com')).toBeInTheDocument();
    expect(screen.getByText('Verified')).toBeInTheDocument();
    // Memberships card renders the shared /v1/tenants cache.
    expect(await screen.findByText('Acme Ops')).toBeInTheDocument();
  });

  it('shows the backup-codes row only once MFA is enrolled', async () => {
    stubAll({
      status: 'enrolled',
      requiresMfa: true,
      backupCodesRemaining: 3,
      webauthnCredentials: 0
    });
    renderWithProviders(<UserDashboard />);

    expect(await screen.findByText('Backup codes')).toBeInTheDocument();
    expect(screen.getByText('3 left')).toBeInTheDocument();
    // Enrolled → no setup CTA anywhere.
    expect(
      screen.queryByRole('link', { name: 'Set up now' })
    ).not.toBeInTheDocument();
  });
});
