import { describe, it, expect } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { Routes, Route, useLocation } from 'react-router';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders, waitForQuerySettled } from 'test/render';
import { server } from 'test/server';
import { operatorPolicyHandler } from 'test/handlers';
import EmailPasswordForm from './EmailPasswordForm';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';

// Default policy handler — login + registration enabled, password sign-in
// persisted on. Individual tests that need a kill switch override this.
const policyOk = http.get('*/v1/auth/operator/policy', () =>
  HttpResponse.json({
    registrationEnabled: true,
    loginEnabled: true,
    passwordMinLength: 10,
    passwordLoginEnabled: true,
    passwordLoginBreakGlassEffective: false
  })
);

// Surfaces useLocation().state on whatever route mounts it, so tests can
// assert the payload react-router carried across navigation.
const LocationProbe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <div data-testid={label}>
      <span data-testid={`${label}-pathname`}>{location.pathname}</span>
      <span data-testid={`${label}-state`}>
        {JSON.stringify(location.state ?? null)}
      </span>
    </div>
  );
};

// `from` mirrors what ProtectedRoute stashes in location.state when it bounces
// an unauthenticated user off a deep link.
const renderForm = (from?: unknown) =>
  renderWithProviders(
    <Routes>
      <Route path="/login" element={<EmailPasswordForm />} />
      <Route
        path={DEFAULT_POST_LOGIN}
        element={<LocationProbe label="dashboard" />}
      />
      <Route
        path="/admin/modules"
        element={<LocationProbe label="deeplink" />}
      />
      <Route path="/mfa/verify" element={<LocationProbe label="mfa" />} />
    </Routes>,
    {
      routerEntries: [
        from === undefined ? '/login' : { pathname: '/login', state: { from } }
      ]
    }
  );

const fillCredentials = async (
  email = 'op@example.com',
  password = 'hunter22hunter22'
) => {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText(/email/i), email);
  await user.type(screen.getByLabelText(/password/i), password);
  return user;
};

describe('EmailPasswordForm', () => {
  it('signs the user in and navigates to the dashboard on success', async () => {
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({
          success: true,
          accessToken: 'access-token-xyz',
          tokenType: 'Bearer',
          expiresIn: 900,
          user: {
            id: 'u-1',
            email: 'op@example.com',
            fullName: 'Op User',
            isActive: true,
            roles: ['operator'],
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z'
          }
        })
      )
    );

    const { store } = renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('dashboard-pathname')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
    // Redux auth slice was seeded with the response body. Without this the
    // app would render the dashboard route but every protected query would
    // fire without an Authorization header and bounce the user back to login.
    await waitFor(() => {
      const auth = store.getState().auth;
      expect(auth.accessToken).toBe('access-token-xyz');
    });
  });

  it('routes to /mfa/verify carrying the challenge id when the account has MFA', async () => {
    // Bug class: an MFA-enrolled account hits /login and the form silently
    // drops the partial response, leaving the user stuck on the login page
    // — or worse, treats the partial response as a full login and lets the
    // user past the gate without completing the second factor.
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({
          success: true,
          requiresMfa: true,
          mfaToken: 'challenge-abc',
          webauthnAvailable: true
        })
      )
    );

    const { store } = renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('mfa-pathname')).toHaveTextContent(
      '/mfa/verify'
    );
    const state = JSON.parse(
      screen.getByTestId('mfa-state').textContent ?? 'null'
    );
    expect(state).toMatchObject({
      challengeId: 'challenge-abc',
      email: 'op@example.com',
      webauthnAvailable: true
    });
    // Auth state must NOT be seeded — the user has not completed MFA yet.
    expect(store.getState().auth.accessToken).toBeFalsy();
  });

  const okLogin = http.post('*/v1/auth/operator/login', () =>
    HttpResponse.json({
      success: true,
      accessToken: 'access-token-xyz',
      tokenType: 'Bearer',
      expiresIn: 900,
      user: {
        id: 'u-1',
        email: 'op@example.com',
        fullName: 'Op User',
        isActive: true,
        roles: ['operator'],
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z'
      }
    })
  );

  it('returns the user to the deep link captured in location.state.from', async () => {
    server.use(policyOk, okLogin);

    // ProtectedRoute stores a full router Location in state.from.
    renderForm({ pathname: '/admin/modules', search: '?tab=addons', hash: '' });
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('deeplink-pathname')).toHaveTextContent(
      '/admin/modules'
    );
    expect(screen.queryByTestId('dashboard-pathname')).toBeNull();
  });

  it('falls back to the dashboard when from is an off-site open-redirect target', async () => {
    server.use(policyOk, okLogin);

    renderForm({ pathname: '//evil.com', search: '', hash: '' });
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('dashboard-pathname')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
    expect(screen.queryByTestId('deeplink-pathname')).toBeNull();
  });

  it('forwards the deep link to /mfa/verify as returnTo when MFA is required', async () => {
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({
          success: true,
          requiresMfa: true,
          mfaToken: 'challenge-abc',
          webauthnAvailable: false
        })
      )
    );

    renderForm({ pathname: '/admin/modules', search: '', hash: '' });
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('mfa-pathname')).toHaveTextContent(
      '/mfa/verify'
    );
    const state = JSON.parse(
      screen.getByTestId('mfa-state').textContent ?? 'null'
    );
    expect(state).toMatchObject({
      challengeId: 'challenge-abc',
      returnTo: '/admin/modules'
    });
  });

  it('shows the invalid-credentials message on a 401 response', async () => {
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({ detail: 'invalid credentials' }, { status: 401 })
      )
    );

    renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(
      await screen.findByText(/invalid email or password/i)
    ).toBeInTheDocument();
  });

  it('shows the rate-limit message on a 429 response', async () => {
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({ detail: 'too many' }, { status: 429 })
      )
    );

    renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(
      await screen.findByText(/too many failed attempts/i)
    ).toBeInTheDocument();
  });

  it('disables submit and shows the maintenance banner when policy says login is off', async () => {
    server.use(
      http.get('*/v1/auth/operator/policy', () =>
        HttpResponse.json({
          registrationEnabled: false,
          loginEnabled: false,
          passwordMinLength: 10,
          passwordLoginEnabled: true,
          passwordLoginBreakGlassEffective: false
        })
      ),
      // If the form ever calls login while disabled, this handler will
      // delay long enough that the assertions below race and fail loud.
      http.post('*/v1/auth/operator/login', async () => {
        await delay(2000);
        return HttpResponse.json({ success: false });
      })
    );

    renderForm();
    expect(
      await screen.findByText(/login is temporarily disabled/i)
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in/i })).toBeDisabled();
    // Registration link is hidden by the same kill switch.
    expect(screen.queryByText(/create one/i)).toBeNull();
  });
});

describe('password-login policy gating (PR 3 §4.10)', () => {
  it('renders nothing when the persisted method is off', async () => {
    server.use(operatorPolicyHandler({ passwordLoginEnabled: false }));
    renderForm();
    await waitFor(() =>
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument()
    );
    expect(
      screen.queryByRole('button', { name: /sign in/i })
    ).not.toBeInTheDocument();
  });

  it('renders nothing on the emergency-null state without break-glass', async () => {
    server.use(operatorPolicyHandler({ passwordLoginEnabled: null }));
    renderForm();
    await waitFor(() =>
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument()
    );
  });

  it('break-glass renders a labelled emergency form without forgot/register CTAs', async () => {
    server.use(
      operatorPolicyHandler({
        passwordLoginEnabled: false,
        passwordLoginBreakGlassEffective: true
      })
    );
    renderForm();
    // auth.pages.passwordBreakGlassActive (added in Step 7)
    expect(
      await screen.findByText(/emergency access mode/i)
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    // existing copy: auth.forgotPassword = "Forgot password?" (en.json:215),
    // auth.createOne = "Create one" (en.json:219)
    expect(screen.queryByText(/forgot password\?/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/create one/i)).not.toBeInTheDocument();
  });

  it('policy transport failure keeps the form (display fail-open)', async () => {
    server.use(
      http.get('*/v1/auth/operator/policy', () => HttpResponse.error())
    );
    const { store } = renderForm();
    // The fail-open fallback paints a tree byte-identical to the pre-fetch
    // one, so finding the email field on its own proves nothing. Anchor on
    // the query having SETTLED — the queryFn swallows the transport error
    // and resolves with the fallback, so the entry reaches `fulfilled` —
    // then assert the form survived that answer.
    await waitForQuerySettled(store, 'getAuthPolicy');
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });
});
