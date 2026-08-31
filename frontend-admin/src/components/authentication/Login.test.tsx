import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import { renderWithProviders, waitForQuerySettled } from 'test/render';
import { server } from 'test/server';
import { operatorPolicyHandler } from 'test/handlers';
import Login from './Login';

vi.mock('utils/socialAuthUtils', async () => {
  const actual = await vi.importActual<typeof import('utils/socialAuthUtils')>(
    'utils/socialAuthUtils'
  );
  return {
    ...actual,
    initiateSocialLogin: vi.fn().mockResolvedValue(undefined)
  };
});

const providersWith = (providers: string[]) =>
  http.get('*/v1/auth/operator/providers', () =>
    HttpResponse.json({ providers })
  );

// While /policy is in flight the page falls open and renders the password
// form, so the email field disappearing is proof the policy landed and said
// "off". Every absence assertion below is anchored on a settled state like
// this one — asserting against the first paint would pass vacuously.
const waitForPasswordFormGone = () =>
  waitFor(() =>
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument()
  );

describe('Login no-method alert (PR 3 §4.10)', () => {
  it('renders the alert when password is off and providers resolve empty', async () => {
    server.use(
      operatorPolicyHandler({ passwordLoginEnabled: false }),
      providersWith([])
    );
    renderWithProviders(<Login />);
    expect(await screen.findByText(/no sign-in method/i)).toBeInTheDocument();
  });

  it('no alert when a provider resolves', async () => {
    server.use(
      operatorPolicyHandler({ passwordLoginEnabled: false }),
      providersWith(['google'])
    );
    renderWithProviders(<Login />);
    await screen.findByRole('button', { name: /google/i });
    await waitForPasswordFormGone();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('a provider-query error shows the retryable error, never the alert', async () => {
    server.use(
      operatorPolicyHandler({ passwordLoginEnabled: false }),
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ detail: 'boom' }, { status: 503 })
      )
    );
    renderWithProviders(<Login />);
    // Anchor on BOTH settled states (auth.social.loadError, en.json:340,
    // and the policy-hidden password form) — asserting the alert's absence
    // before either query resolves would pass vacuously.
    await screen.findByText(/could not load the social sign-in options/i);
    await waitForPasswordFormGone();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('no alert while password is on, even with zero providers', async () => {
    server.use(operatorPolicyHandler({}), providersWith([]));
    const { store } = renderWithProviders(<Login />);
    // With the method ON the password form renders both before and after
    // /policy lands, so no DOM signal distinguishes them — anchor on the
    // cache entry. The providers query DOES have one: its loading copy
    // (auth.social.loading) clears when the empty list resolves.
    await waitForQuerySettled(store, 'getAuthPolicy');
    await waitFor(() =>
      expect(
        screen.queryByText(/loading sign-in options/i)
      ).not.toBeInTheDocument()
    );
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('no alert under break-glass — the emergency form is a method', async () => {
    server.use(
      operatorPolicyHandler({
        passwordLoginEnabled: false,
        passwordLoginBreakGlassEffective: true
      }),
      providersWith([])
    );
    renderWithProviders(<Login />);
    // auth.pages.passwordBreakGlassActive only renders once the policy has
    // landed, so it doubles as the settled-state anchor.
    await screen.findByText(/emergency access mode/i);
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });
});

describe('Login social divider (PR 3 §4.10)', () => {
  // auth.pages.loginContinueWith, en.json: "or continue with".
  it('hides the divider on an SSO-only console — nothing to divide from', async () => {
    server.use(
      operatorPolicyHandler({ passwordLoginEnabled: false }),
      providersWith(['google'])
    );
    renderWithProviders(<Login />);
    await screen.findByRole('button', { name: /google/i });
    await waitForPasswordFormGone();
    expect(screen.queryByText(/or continue with/i)).not.toBeInTheDocument();
  });

  it('keeps the divider when the password form renders above it', async () => {
    server.use(operatorPolicyHandler({}), providersWith(['google']));
    renderWithProviders(<Login />);
    expect(
      await screen.findByRole('button', { name: /google/i })
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByText(/or continue with/i)).toBeInTheDocument();
  });
});
