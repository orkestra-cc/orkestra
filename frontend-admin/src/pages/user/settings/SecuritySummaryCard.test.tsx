import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  emptySelfAuthMethods,
  emptySessions,
  mySessionsHandler,
  selfAuthMethodsHandler
} from 'test/handlers';
import SecuritySummaryCard from './SecuritySummaryCard';

// Complete required BackendUser body (authApi.ts:39-62: id, email,
// username, fullName, role, isActive, emailVerified, createdAt,
// updatedAt are the non-optional fields).
const meHandler = http.get('*/v1/auth/operator/me', () =>
  HttpResponse.json({
    id: 'u-1',
    email: 'op@example.com',
    username: 'op',
    fullName: 'Operator One',
    role: 'administrator',
    isActive: true,
    emailVerified: true,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z'
  })
);

describe('SecuritySummaryCard password row (PR 3 §4.8)', () => {
  it('set-but-unusable shows the kept note', async () => {
    server.use(
      meHandler,
      mySessionsHandler(emptySessions),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: true,
        passwordUsableForLogin: false
      })
    );
    renderWithProviders(<SecuritySummaryCard />);
    // settings.security.summary.passwordKeptNotice (added in Step 4)
    expect(
      await screen.findByText(/sign-in with it is disabled/i)
    ).toBeInTheDocument();
  });

  it('no hash hides the password row', async () => {
    server.use(
      meHandler,
      mySessionsHandler(emptySessions),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: false,
        passwordUsableForLogin: false,
        hasUsablePassword: false
      })
    );
    renderWithProviders(<SecuritySummaryCard />);
    // Anchor on a SETTLED auth-methods element first — with zero factors
    // the card renders settings.security.summary.mfaOff = "Two-factor off";
    // asserting an absence before the query resolves passes vacuously.
    await screen.findByText(/two-factor off/i);
    // settings.security.summary.passwordAgeUnknown = "Password update date
    // unknown" is the password row's copy when no updatedAt is present.
    expect(
      screen.queryByText(/password update date unknown/i)
    ).not.toBeInTheDocument();
  });
});
