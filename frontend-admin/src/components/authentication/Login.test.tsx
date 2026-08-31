import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
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
    // Anchor on the SETTLED error state first (auth.social.loadError,
    // en.json:340) — asserting the alert's absence before the query
    // resolves would pass vacuously.
    await screen.findByText(/could not load the social sign-in options/i);
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });

  it('no alert while password is on, even with zero providers', async () => {
    server.use(operatorPolicyHandler({}), providersWith([]));
    renderWithProviders(<Login />);
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
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
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).not.toBeInTheDocument();
  });
});
