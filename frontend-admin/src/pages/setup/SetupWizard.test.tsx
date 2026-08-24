// SetupWizard — resume behavior. A fresh install starts at Welcome; a
// browser that (re)loads while phase is already 'tenant_required' (the
// administrator exists) must resume directly at the organization step
// instead of replaying Welcome/Administrator. See OrgStep.test.tsx /
// OrgStep.access.test.tsx for the organization step's own behavior once
// mounted — this file only pins which step SetupWizard starts on.
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import i18n from '../../i18n';
import SetupWizard from './SetupWizard';

vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const tenantRequiredStatus = {
  setupCompleted: false,
  phase: 'tenant_required' as const,
  smtpConfigured: false
};

afterEach(async () => {
  await new Promise(resolve => setTimeout(resolve, 0));
  vi.restoreAllMocks();
});

describe('SetupWizard — resume', () => {
  it('phase=tenant_required + an authenticated session starts at the organization step, not Welcome', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json(tenantRequiredStatus)
      ),
      http.get(url('/v1/auth/session'), () =>
        HttpResponse.json({
          accessToken: 'token-a',
          tokenType: 'Bearer',
          expiresIn: 900,
          user: { id: 'u1', email: 'admin@example.com', role: 'developer' },
          authenticated: true,
          success: true
        })
      ),
      http.get(url('/v1/setup/finalization-access'), () =>
        HttpResponse.json({
          canFinalize: true,
          canClaimRecovery: false,
          reason: ''
        })
      )
    );

    renderWithProviders(<SetupWizard />, { routerEntries: ['/setup'] });

    expect(
      await screen.findByLabelText(i18n.t('setup.org.labelName'))
    ).toBeInTheDocument();
    expect(
      screen.queryByText(i18n.t('setup.welcome.title'))
    ).not.toBeInTheDocument();
  });

  it('phase=tenant_required + no restorable session shows a sign-in prompt linking to /login', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json(tenantRequiredStatus)
      ),
      http.get(url('/v1/auth/session'), () =>
        HttpResponse.json({ authenticated: false, success: true })
      )
      // No finalization-access handler — proves the probe is never reached
      // while unauthenticated (MSW's onUnhandledRequest: 'error').
    );

    renderWithProviders(<SetupWizard />, { routerEntries: ['/setup'] });

    expect(
      await screen.findByText(i18n.t('setup.org.access.signInTitle'))
    ).toBeInTheDocument();
    expect(
      screen.getByRole('link', { name: i18n.t('setup.org.access.signInCta') })
    ).toHaveAttribute('href', '/login');
    expect(
      screen.queryByText(i18n.t('setup.welcome.title'))
    ).not.toBeInTheDocument();
  });
});
