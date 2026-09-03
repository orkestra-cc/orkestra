import { describe, it, expect, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { Routes, Route, useLocation } from 'react-router';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import SocialAuthCallback from './SocialAuthCallback';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';
import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS
} from 'utils/socialAuthUtils';

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <>
      <div data-testid={`${label}-location`}>
        {location.pathname + location.search + location.hash}
      </div>
      <div data-testid={`${label}-state`}>{JSON.stringify(location.state)}</div>
    </>
  );
};

const sessionBody = {
  accessToken: 'at-1',
  tokenType: 'Bearer',
  expiresIn: 900,
  success: true,
  user: {
    id: 'u-1',
    email: 'op@example.com',
    fullName: 'Op User',
    isActive: true,
    roles: ['operator'],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z'
  }
};

// A session endpoint the test releases by hand, so "scrub before the first
// await" is observable: the URL must be clean while the request is pending.
const deferredSession = () => {
  let release!: () => void;
  const gate = new Promise<void>(resolve => {
    release = resolve;
  });
  let hits = 0;
  server.use(
    http.get(url('/v1/auth/session'), async () => {
      hits++;
      await gate;
      return HttpResponse.json(sessionBody);
    })
  );
  return { release, hits: () => hits };
};

const renderCallback = (search: string, hash = '') =>
  renderWithProviders(
    <Routes>
      <Route
        path="/auth/callback"
        element={
          <>
            <SocialAuthCallback />
            <Probe label="cb" />
          </>
        }
      />
      <Route path={DEFAULT_POST_LOGIN} element={<Probe label="dashboard" />} />
      <Route path="/admin/modules" element={<Probe label="deeplink" />} />
      <Route path="/login" element={<Probe label="login" />} />
    </Routes>,
    { routerEntries: [{ pathname: '/auth/callback', search, hash }] }
  );

describe('SocialAuthCallback', () => {
  beforeEach(() => sessionStorage.clear());

  it('scrubs the URL before the session request resolves, then lands on the default page', async () => {
    const session = deferredSession();
    renderCallback('?success=true&provider=google');

    await waitFor(() =>
      expect(screen.getByTestId('cb-location')).toHaveTextContent(
        /^\/auth\/callback$/
      )
    );
    // The request is issued only after the scrub, and nothing navigates
    // while it is pending.
    await waitFor(() => expect(session.hits()).toBe(1));
    expect(screen.getByTestId('cb-location')).toHaveTextContent(
      /^\/auth\/callback$/
    );
    expect(screen.queryByTestId('dashboard-location')).toBeNull();

    session.release();
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
  });

  it('honours a fresh stashed return target and deletes it', async () => {
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: '/admin/modules', createdAt: Date.now() })
    );
    const session = deferredSession();
    renderCallback('?success=true&provider=github');
    // Taken in the first effect — already gone once render returns.
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    session.release();
    expect(await screen.findByTestId('deeplink-location')).toHaveTextContent(
      '/admin/modules'
    );
  });

  it('ignores a stale stashed return target', async () => {
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({
        target: '/admin/modules',
        createdAt: Date.now() - OAUTH_RETURN_TO_TTL_MS - 1
      })
    );
    const session = deferredSession();
    renderCallback('?success=true&provider=github');
    session.release();
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
  });

  it('takes the return target on an error outcome too', async () => {
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: '/admin/modules', createdAt: Date.now() })
    );
    renderCallback('?success=false&error=oauth_access_denied');
    expect(
      await screen.findByText(/cancelled at the identity provider/i)
    ).toBeInTheDocument();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it('renders the MFA panel locally from the fragment, with no router state and a clean URL', async () => {
    server.use(
      http.post(
        url('/v1/auth/operator/mfa/login/verify'),
        async ({ request }) => {
          const body = (await request.json()) as { challengeId: string };
          if (body.challengeId !== 'ch-1') {
            return HttpResponse.json(
              { detail: 'wrong challenge' },
              { status: 401 }
            );
          }
          return HttpResponse.json({ success: true, user: sessionBody.user });
        }
      )
    );
    renderCallback(
      '',
      '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false'
    );

    expect(
      await screen.findByRole('heading', { name: /two-factor/i })
    ).toBeInTheDocument();
    expect(screen.getByTestId('cb-location')).toHaveTextContent(
      /^\/auth\/callback$/
    );
    // The challenge lives in component memory only: no router state.
    expect(screen.getByTestId('cb-state')).toHaveTextContent(/^null$/);

    const user = userEvent.setup();
    await user.type(screen.getByRole('textbox'), '123456');
    await user.click(
      screen.getByRole('button', { name: /verify and sign in/i })
    );
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
  });

  it('treats an ambiguous payload (MFA fragment + query outcome) as the generic failure', async () => {
    renderCallback(
      '?success=true&provider=google',
      '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false'
    );
    expect(
      await screen.findByText(/authentication failed\. please try again/i)
    ).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: /two-factor/i })).toBeNull();
    expect(screen.queryByTestId('dashboard-location')).toBeNull();
  });

  it('renders the mapped copy for an allowlisted code, never the raw code', async () => {
    renderCallback('?success=false&error=oauth_signup_disabled');
    expect(await screen.findByText(/invitation-only/i)).toBeInTheDocument();
    expect(screen.queryByText(/oauth_signup_disabled/)).toBeNull();
    expect(screen.getByTestId('cb-location')).toHaveTextContent(
      /^\/auth\/callback$/
    );
  });

  it('collapses an unknown code to the generic copy and never renders raw URL text', async () => {
    renderCallback('?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E');
    expect(
      await screen.findByText(/authentication failed\. please try again/i)
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('<script>');
    expect(document.body.textContent).not.toContain('alert(1)');
  });

  it('treats a signed-out session as a login error, never a protected route', async () => {
    server.use(
      http.get(url('/v1/auth/session'), () =>
        HttpResponse.json({ authenticated: false })
      )
    );
    renderCallback('?success=true&provider=google');
    expect(
      await screen.findByText(/no session could be established/i)
    ).toBeInTheDocument();
    expect(screen.queryByTestId('dashboard-location')).toBeNull();
  });

  it('keeps the bootstrap state and offers retry when the session endpoint is unavailable', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/auth/session'), () => {
        calls++;
        return calls === 1
          ? HttpResponse.json(
              { code: 'session_enforcement_unavailable' },
              { status: 503 }
            )
          : HttpResponse.json(sessionBody);
      })
    );
    renderCallback('?success=true&provider=google');
    const retry = await screen.findByRole('button', { name: /try again/i });
    expect(screen.queryByTestId('dashboard-location')).toBeNull();
    await userEvent.setup().click(retry);
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
    expect(calls).toBe(2);
  });
});
