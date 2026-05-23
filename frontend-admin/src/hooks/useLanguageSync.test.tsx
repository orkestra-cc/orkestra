import { describe, it, expect, beforeEach } from 'vitest';
import { renderWithProviders } from 'test/render';
import i18n from '../i18n';
import { useLanguageSync } from './useLanguageSync';

// Mounting a tiny component that just runs the hook lets us assert
// against i18n.language without needing access to the hook's return
// value (there isn't one — the hook is side-effect-only).
const Probe = () => {
  useLanguageSync();
  return null;
};

// Ensures every test starts from English. The i18n singleton survives
// across tests so a leaked changeLanguage('it') from an earlier case
// would silently pass this one.
beforeEach(async () => {
  await i18n.changeLanguage('en');
});

describe('useLanguageSync', () => {
  it('changes i18n.language to the user.language on mount', async () => {
    renderWithProviders(<Probe />, {
      preloadedState: {
        auth: {
          user: {
            id: 'u1',
            email: 'a@b.test',
            username: 'a',
            fullName: 'A',
            role: 'administrator',
            isActive: true,
            emailVerified: true,
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            language: 'it'
          },
          isAuthenticated: true,
          isLoading: false,
          error: null,
          sessionExpiry: null,
          permissions: ['administrator'],
          preferences: { theme: 'light', language: 'en', notifications: true },
          _isLoggingOut: false,
          accessToken: null,
          tokenExpiry: null
        }
      }
    });

    // i18next.changeLanguage resolves asynchronously even when the
    // resources are bundled in — wait one microtask.
    await new Promise<void>(resolve => queueMicrotask(() => resolve()));
    expect(i18n.language).toBe('it');
  });

  it('leaves i18n alone when no user is signed in', async () => {
    renderWithProviders(<Probe />);
    await new Promise<void>(resolve => queueMicrotask(() => resolve()));
    expect(i18n.language).toBe('en');
  });

  it('leaves i18n alone when user has no language field', async () => {
    renderWithProviders(<Probe />, {
      preloadedState: {
        auth: {
          user: {
            id: 'u1',
            email: 'a@b.test',
            username: 'a',
            fullName: 'A',
            role: 'administrator',
            isActive: true,
            emailVerified: true,
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z'
            // no language
          },
          isAuthenticated: true,
          isLoading: false,
          error: null,
          sessionExpiry: null,
          permissions: ['administrator'],
          preferences: { theme: 'light', language: 'en', notifications: true },
          _isLoggingOut: false,
          accessToken: null,
          tokenExpiry: null
        }
      }
    });
    await new Promise<void>(resolve => queueMicrotask(() => resolve()));
    expect(i18n.language).toBe('en');
  });
});
