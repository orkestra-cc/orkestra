import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import SocialLoginForm from './SocialLoginForm';

// Stub the `initiateSocialLogin` redirect so the test doesn't actually
// blow away window.location. The component's interaction with the
// helper is tested elsewhere; here we only care about which providers
// the buttons render.
vi.mock('utils/socialAuthUtils', async () => {
  const actual = await vi.importActual<typeof import('utils/socialAuthUtils')>(
    'utils/socialAuthUtils'
  );
  return {
    ...actual,
    initiateSocialLogin: vi.fn().mockResolvedValue(undefined)
  };
});

describe('SocialLoginForm', () => {
  it('renders only the providers the backend returned', async () => {
    // Admin has disabled Apple, GitHub, Discord on the OAuth Providers
    // tab — backend filters them out of /providers.
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ providers: ['google'] })
      )
    );

    renderWithProviders(<SocialLoginForm />);

    // Google button is present, Apple/GitHub/Discord are not.
    const googleBtn = await screen.findByRole('button', { name: /google/i });
    expect(googleBtn).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /apple/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /github/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /discord/i })).toBeNull();
  });

  it('renders all four providers when the backend returns them', async () => {
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({
          providers: ['google', 'apple', 'github', 'discord']
        })
      )
    );

    renderWithProviders(<SocialLoginForm />);

    expect(
      await screen.findByRole('button', { name: /google/i })
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /apple/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /github/i })).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /discord/i })
    ).toBeInTheDocument();
  });

  it('shows a loading state while the providers query is in flight', async () => {
    // Delay the response so the loading branch is observable.
    server.use(
      http.get('*/v1/auth/operator/providers', async () => {
        await delay(50);
        return HttpResponse.json({ providers: ['google'] });
      })
    );

    renderWithProviders(<SocialLoginForm />);

    // Loading copy is rendered immediately. The `aria-busy` attribute
    // sits on a parent <div>, so query against the container that holds
    // the i18n string instead.
    expect(screen.getByText(/loading sign-in options/i)).toBeInTheDocument();

    // ...and goes away once the response settles.
    await waitFor(() =>
      expect(
        screen.queryByText(/loading sign-in options/i)
      ).not.toBeInTheDocument()
    );
    expect(screen.getByRole('button', { name: /google/i })).toBeInTheDocument();
  });

  it('shows an empty-state message when the backend returns no providers', async () => {
    // Admin disabled every provider — backend returns an empty list.
    // The UI must surface that intentionally rather than render a
    // confusing empty form.
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ providers: [] })
      )
    );

    renderWithProviders(<SocialLoginForm />);

    expect(
      await screen.findByText(/no social sign-in providers/i)
    ).toBeInTheDocument();
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('shows an alert when the providers endpoint fails', async () => {
    // Network failure path — the hook is fail-closed (empty list) but
    // distinguishes "empty list" from "errored" so the UI can show a
    // retryable alert instead of the steady-state empty copy.
    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ message: 'boom' }, { status: 500 })
      )
    );

    renderWithProviders(<SocialLoginForm />);

    expect(
      await screen.findByText(/could not load the social sign-in options/i)
    ).toBeInTheDocument();
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('skips unknown provider strings with a console warning', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

    server.use(
      http.get('*/v1/auth/operator/providers', () =>
        HttpResponse.json({ providers: ['google', 'microsoft'] })
      )
    );

    renderWithProviders(<SocialLoginForm />);

    expect(
      await screen.findByRole('button', { name: /google/i })
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /microsoft/i })).toBeNull();
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('microsoft'));

    warnSpy.mockRestore();
  });
});
