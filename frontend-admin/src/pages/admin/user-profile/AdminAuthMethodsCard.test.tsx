import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import AdminAuthMethodsCard from './AdminAuthMethodsCard';
import type { User } from 'store/api/userApi';

// Honest User value — every required field of the interface
// (userApi.ts:11-26), no cast.
const targetUser: User = {
  id: 'u-1',
  email: 'target@example.com',
  username: 'target',
  fullName: 'Target User',
  role: 'operator',
  providers: [],
  isActive: true,
  emailVerified: true,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

// The card reads the CURRENT admin from the auth slice to compute isSelf
// (AdminAuthMethodsCard.tsx:50-52); seed a different id so isSelf is
// false. Same AuthState shape useUserTable.test.tsx:57-84 preloads.
const preloadedAuthState = {
  auth: {
    user: {
      id: 'admin-1',
      email: 'admin@example.com',
      username: 'admin',
      fullName: 'Admin One',
      role: 'administrator',
      providers: [],
      isActive: true,
      emailVerified: true,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z'
    },
    isAuthenticated: true,
    isLoading: false,
    error: null,
    sessionExpiry: null,
    permissions: [] as string[],
    preferences: {
      theme: 'light' as const,
      language: 'en',
      notifications: true
    },
    _isLoggingOut: false,
    accessToken: 'test-token',
    tokenExpiry: null
  }
};

const adminAuthMethods = (overrides: Record<string, unknown>) =>
  http.get('*/v1/admin/users/u-1/auth-methods', () =>
    HttpResponse.json({
      hasPasswordSet: true,
      passwordUsableForLogin: true,
      hasUsablePassword: true,
      passwordUpdatedAt: '2026-05-01T00:00:00Z',
      emailVerified: true,
      mfaRequired: false,
      mfaFactors: [],
      oauthProviders: [],
      ...overrides
    })
  );

describe('AdminAuthMethodsCard split password fields (PR 3 §4.8)', () => {
  // Button-name matchers are the exact EN copy:
  //   adminUserProfile.authMethods.passwordSendResetButton = "Send password-reset email"
  //   adminUserProfile.authMethods.oauthActionsAria       = "actions for {{provider}}"
  it('method off: reset button disabled; presence badge still reads hasPasswordSet', async () => {
    server.use(adminAuthMethods({ passwordUsableForLogin: false }));
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    const reset = await screen.findByRole('button', {
      name: /send password-reset email/i
    });
    expect(reset).toBeDisabled();
    // presence badge (adminUserProfile.authMethods.passwordBadgeSet = "Set")
    expect(screen.getByText(/^set$/i)).toBeInTheDocument();
  });

  it('method on: reset button enabled', async () => {
    server.use(adminAuthMethods({}));
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    expect(
      await screen.findByRole('button', { name: /send password-reset email/i })
    ).toBeEnabled();
  });

  // Fix round 1, item 1: the reset button is blocked only when the method
  // is KNOWN off (a hash exists and is unusable) — with no hash at all,
  // sending a reset is the designed remedy for an OAuth-only user and the
  // backend's method-policy gate would accept it.
  it('no password set: reset button stays enabled (the remedy is still offered)', async () => {
    server.use(
      adminAuthMethods({
        hasPasswordSet: false,
        passwordUsableForLogin: false,
        hasUsablePassword: false
      })
    );
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    expect(
      await screen.findByRole('button', { name: /send password-reset email/i })
    ).toBeEnabled();
  });

  it('provider actions block keys off usability even with a hash present', async () => {
    server.use(
      adminAuthMethods({
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
    renderWithProviders(<AdminAuthMethodsCard user={targetUser} />, {
      preloadedState: preloadedAuthState
    });
    // The blocked ProviderActions button carries the same aria-label as
    // the enabled dropdown toggle (Step 3.4).
    const actions = await screen.findByRole('button', {
      name: /actions for google/i
    });
    expect(actions).toBeDisabled();
    // Fix round 1, item 1(b): the block reason must be the PASSWORD-DISABLED
    // copy (hasPasswordSet && !passwordUsableForLogin), not the "send a
    // reset first" copy that no longer applies once a hash already exists.
    // React Bootstrap's Tooltip only renders its content on hover.
    await userEvent.hover(actions);
    expect(
      await screen.findByText(/re-enable password sign-in for this surface/i)
    ).toBeInTheDocument();
  });
});
