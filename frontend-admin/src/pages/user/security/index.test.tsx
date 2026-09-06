import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  emptySelfAuthMethods,
  mySessionsHandler,
  operatorPolicyHandler,
  selfAuthMethodsHandler,
  trustedDevicesHandler,
  url
} from 'test/handlers';
import SecurityPage from './index';

// The KPI row derives from three queries the tabs already hold. A query that
// FAILED is not a zero, and on a security page that distinction is the whole
// point: `?? 0` alone renders a transient /auth-methods outage as an
// authoritative "Two-factor: Off", telling someone their account is
// unprotected when it is not.
describe('SecurityPage summary row', () => {
  it('reports real state when the queries land', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        mfaFactors: [{ type: 'totp', backupCodesRemaining: 10 }]
      }),
      mySessionsHandler(),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    expect(await screen.findByText(/^on$/i)).toBeInTheDocument();
    expect(screen.getByText(/authenticator app/i)).toBeInTheDocument();
  });

  it('says "Off" only when the account really has no second factor', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler(),
      mySessionsHandler(),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    expect(await screen.findByText(/^off$/i)).toBeInTheDocument();
    expect(screen.getByText(/no second factor enrolled/i)).toBeInTheDocument();
  });

  it('never claims "Off" when the auth-methods query failed', async () => {
    server.use(
      operatorPolicyHandler(),
      http.get(
        url('/v1/auth/operator/me/auth-methods'),
        () => new HttpResponse(null, { status: 500 })
      ),
      mySessionsHandler(),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    // The tile resolves to the unavailable copy, not to a verdict about the
    // account, and carries no zero-count sibling claim either.
    expect(await screen.findAllByText(/couldn’t load/i)).not.toHaveLength(0);
    expect(screen.queryByText(/^off$/i)).not.toBeInTheDocument();
    expect(
      screen.queryByText(/no second factor enrolled/i)
    ).not.toBeInTheDocument();
  });

  it('shows a dash, not 0, for a failed session count', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler(),
      http.get(
        url('/v1/auth/operator/me/sessions'),
        () => new HttpResponse(null, { status: 503 })
      ),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    await screen.findByText(/signed-in browsers and devices|couldn’t load/i);
    // "active sessions" also appears in the page subtitle — take the match
    // that actually sits inside a KPI tile.
    const sessionsTile = screen
      .getAllByText(/active sessions/i)
      .map(el => el.closest('.stat-card'))
      .find(Boolean)!;
    expect(sessionsTile).toHaveTextContent('—');
    expect(sessionsTile).not.toHaveTextContent(/\b0\b/);
  });
});
