import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { emptySelfAuthMethods, selfAuthMethodsHandler } from 'test/handlers';
import LinkedProvidersTab from './LinkedProvidersTab';

// The tab mounts ONE query (useGetSelfAuthMethodsQuery,
// LinkedProvidersTab.tsx:51); the link/unlink mutations fire only on click.
describe('LinkedProvidersTab only-credential (PR 3 §4.8)', () => {
  it('sole provider + unusable password blocks unlink', async () => {
    server.use(
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: true,
        passwordUsableForLogin: false,
        oauthProviders: [
          {
            provider: 'google',
            email: 'u@example.com',
            linkedAt: '2026-05-01T00:00:00Z',
            isPrimary: true
          }
        ]
      })
    );
    renderWithProviders(<LinkedProvidersTab />);
    await screen.findByText(/google/i);
    // userSecurity.linkedProvidersTab.rowUnlink = "Unlink" — a real text
    // button (LinkedProvidersTab.tsx:276-282, disabled={onlyCredential || isFetching}).
    expect(screen.getByRole('button', { name: /^unlink$/i })).toBeDisabled();
  });

  it('two providers keep unlink available', async () => {
    server.use(
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        passwordUsableForLogin: false,
        oauthProviders: [
          {
            provider: 'google',
            email: 'u@example.com',
            linkedAt: '2026-05-01T00:00:00Z',
            isPrimary: true
          },
          {
            provider: 'github',
            email: 'u@example.com',
            linkedAt: '2026-05-02T00:00:00Z',
            isPrimary: false
          }
        ]
      })
    );
    renderWithProviders(<LinkedProvidersTab />);
    await screen.findByText(/github/i);
    // onlyCredential is false with two rows, so BOTH unlink buttons stay enabled.
    for (const b of screen.getAllByRole('button', { name: /^unlink$/i })) {
      expect(b).toBeEnabled();
    }
  });
});
