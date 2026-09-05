import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import MfaEnrollmentBanner from './MfaEnrollmentBanner';
import type { MfaStatusResponse } from 'types/mfa';

// A privileged user who has not enrolled: this is the only state that
// renders the banner at all.
const authedAdmin = {
  user: {
    id: 'u1',
    email: 'admin@example.test',
    username: 'admin',
    fullName: 'Ada Admin',
    role: 'administrator',
    isActive: true,
    emailVerified: true,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z'
  },
  isAuthenticated: true,
  isLoading: false,
  error: null,
  sessionExpiry: null,
  permissions: [],
  preferences: {
    theme: 'light' as const,
    language: 'en',
    notifications: true
  },
  _isLoggingOut: false,
  accessToken: 'token',
  tokenExpiry: null
};

const mfaStatus = (
  over: Partial<MfaStatusResponse> = {}
): MfaStatusResponse => ({
  status: 'required_pending_enrollment',
  backupCodesRemaining: 0,
  requiresMfa: true,
  graceExpiresAt: new Date(Date.now() + 6 * 24 * 3600 * 1000).toISOString(),
  webauthnCredentials: 0,
  ...over
});

const stubStatus = (over?: Partial<MfaStatusResponse>) =>
  server.use(
    http.get('*/v1/auth/operator/me/mfa', () =>
      HttpResponse.json(mfaStatus(over))
    )
  );

describe('MfaEnrollmentBanner', () => {
  // The banner is the only route into enrollment for a user who has not
  // enrolled, and enrollment is time-boxed: the gate wants an interactive
  // sign-in within five minutes. Sending them to the wrong page burns that
  // window and, for a user whose grace has lapsed, the trip back can cost
  // them the session entirely. /user/settings is a different page — the MFA
  // controls live on /user/security under the "mfa" tab, which that page
  // reads from the `tab` search param.
  it('points Set up at the security page MFA tab, not at profile settings', async () => {
    stubStatus();

    renderWithProviders(<MfaEnrollmentBanner />, {
      preloadedState: { auth: authedAdmin }
    });

    const setUp = await screen.findByRole('link', { name: /set up/i });
    expect(setUp).toHaveAttribute('href', '/user/security?tab=mfa');
  });

  it('still points there once the grace window has expired', async () => {
    stubStatus({
      graceExpiresAt: new Date(Date.now() - 3600 * 1000).toISOString()
    });

    renderWithProviders(<MfaEnrollmentBanner />, {
      preloadedState: { auth: authedAdmin }
    });

    const setUp = await screen.findByRole('link', { name: /set up/i });
    expect(setUp).toHaveAttribute('href', '/user/security?tab=mfa');
  });

  // Regression pin, not a TDD driver: the banner already stayed silent for
  // these. Kept so a future change to the render guard cannot start nagging
  // users who owe nothing.
  it('renders nothing for an enrolled user', () => {
    stubStatus({ status: 'enrolled' });

    const { container } = renderWithProviders(<MfaEnrollmentBanner />, {
      preloadedState: { auth: authedAdmin }
    });

    expect(container).toBeEmptyDOMElement();
  });
});
