import { describe, it, expect } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { Routes, Route, useLocation } from 'react-router-dom';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import EmailPasswordForm from './EmailPasswordForm';

// Default policy handler — login + registration enabled. Individual tests
// that need the kill switch override this.
const policyOk = http.get('*/v1/auth/operator/policy', () =>
  HttpResponse.json({ registrationEnabled: true, loginEnabled: true, passwordMinLength: 10 }),
);

// Surfaces useLocation().state on whatever route mounts it, so tests can
// assert the payload react-router-dom carried across navigation.
const LocationProbe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <div data-testid={label}>
      <span data-testid={`${label}-pathname`}>{location.pathname}</span>
      <span data-testid={`${label}-state`}>{JSON.stringify(location.state ?? null)}</span>
    </div>
  );
};

const renderForm = () =>
  renderWithProviders(
    <Routes>
      <Route path="/login" element={<EmailPasswordForm />} />
      <Route path="/dashboard/analytics" element={<LocationProbe label="dashboard" />} />
      <Route path="/mfa/verify" element={<LocationProbe label="mfa" />} />
    </Routes>,
    { routerEntries: ['/login'] },
  );

const fillCredentials = async (email = 'op@example.com', password = 'hunter22hunter22') => {
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
            updatedAt: '2026-01-01T00:00:00Z',
          },
        }),
      ),
    );

    const { store } = renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('dashboard-pathname')).toHaveTextContent(
      '/dashboard/analytics',
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
          webauthnAvailable: true,
        }),
      ),
    );

    const { store } = renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByTestId('mfa-pathname')).toHaveTextContent('/mfa/verify');
    const state = JSON.parse(screen.getByTestId('mfa-state').textContent ?? 'null');
    expect(state).toMatchObject({
      challengeId: 'challenge-abc',
      email: 'op@example.com',
      webauthnAvailable: true,
    });
    // Auth state must NOT be seeded — the user has not completed MFA yet.
    expect(store.getState().auth.accessToken).toBeFalsy();
  });

  it('shows the invalid-credentials message on a 401 response', async () => {
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({ detail: 'invalid credentials' }, { status: 401 }),
      ),
    );

    renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByText(/invalid email or password/i)).toBeInTheDocument();
  });

  it('shows the rate-limit message on a 429 response', async () => {
    server.use(
      policyOk,
      http.post('*/v1/auth/operator/login', () =>
        HttpResponse.json({ detail: 'too many' }, { status: 429 }),
      ),
    );

    renderForm();
    const user = await fillCredentials();
    await user.click(screen.getByRole('button', { name: /sign in/i }));

    expect(await screen.findByText(/too many failed attempts/i)).toBeInTheDocument();
  });

  it('disables submit and shows the maintenance banner when policy says login is off', async () => {
    server.use(
      http.get('*/v1/auth/operator/policy', () =>
        HttpResponse.json({
          registrationEnabled: false,
          loginEnabled: false,
          passwordMinLength: 10,
        }),
      ),
      // If the form ever calls login while disabled, this handler will
      // delay long enough that the assertions below race and fail loud.
      http.post('*/v1/auth/operator/login', async () => {
        await delay(2000);
        return HttpResponse.json({ success: false });
      }),
    );

    renderForm();
    expect(
      await screen.findByText(/login is temporarily disabled/i),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in/i })).toBeDisabled();
    // Registration link is hidden by the same kill switch.
    expect(screen.queryByText(/create one/i)).toBeNull();
  });
});
