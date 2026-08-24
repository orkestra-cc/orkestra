// OrgStep — access-state rendering. The form itself (empty inputs, choice
// mapping, the submit loop) is covered by OrgStep.test.tsx, always against
// a { canFinalize: true } probe. This file covers the auth/access boundary
// that decides WHICH screen renders before the form ever appears: no
// restorable session, each finalization-access outcome
// (backend/internal/shared/setup/service.go's evaluateAccessDetailed), and
// a probe failure.
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import i18n from '../../../i18n';
import OrgStep from './OrgStep';

const sessionBody = (accessToken: string | null) =>
  accessToken
    ? {
        accessToken,
        tokenType: 'Bearer',
        expiresIn: 900,
        user: { id: 'u1', email: 'admin@example.com', role: 'developer' },
        authenticated: true,
        success: true
      }
    : { authenticated: false, success: true };

const mockSession = (accessToken: string | null = 'token-a') =>
  server.use(
    http.get(url('/v1/auth/session'), () =>
      HttpResponse.json(sessionBody(accessToken))
    )
  );

const mockAccess = (body: {
  canFinalize?: boolean;
  canClaimRecovery?: boolean;
  reason?: string;
}) =>
  server.use(
    http.get(url('/v1/setup/finalization-access'), () =>
      HttpResponse.json({
        canFinalize: false,
        canClaimRecovery: false,
        reason: '',
        ...body
      })
    )
  );

const renderRouted = (initialPath = '/setup') =>
  renderWithProviders(
    <Routes>
      <Route path="/login" element={<div>LOGIN_PAGE</div>} />
      <Route path="/setup" element={<OrgStep onNext={vi.fn()} />} />
    </Routes>,
    { routerEntries: [initialPath] }
  );

afterEach(async () => {
  await new Promise(resolve => setTimeout(resolve, 0));
  vi.restoreAllMocks();
});

describe('OrgStep — access boundary', () => {
  it('no restorable session renders a sign-in prompt linking to /login (and never probes finalization-access)', async () => {
    mockSession(null);
    // Deliberately no finalization-access handler — MSW's strict
    // onUnhandledRequest: 'error' would fail this test if the probe fired
    // while unauthenticated, proving the query is skipped.
    renderRouted();

    expect(
      await screen.findByText(i18n.t('setup.org.access.signInTitle'))
    ).toBeInTheDocument();
    const link = screen.getByRole('link', {
      name: i18n.t('setup.org.access.signInCta')
    });
    expect(link).toHaveAttribute('href', '/login');
  });

  it('{ canFinalize: true } renders the form', async () => {
    mockSession('token-a');
    mockAccess({ canFinalize: true });
    renderRouted();

    expect(
      await screen.findByLabelText(i18n.t('setup.org.labelName'))
    ).toBeInTheDocument();
  });

  it('{ reason: "bound_to_another_admin" } renders a locked screen with switch-account + logout, and leaks nothing about the bound admin', async () => {
    mockSession('token-a');
    mockAccess({ reason: 'bound_to_another_admin' });
    renderRouted();

    expect(
      await screen.findByText(
        i18n.t('setup.org.access.boundToAnotherAdminTitle')
      )
    ).toBeInTheDocument();
    expect(
      screen.getByText(i18n.t('setup.org.access.boundToAnotherAdminBody'))
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', {
        name: i18n.t('setup.org.access.switchAccount')
      })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: i18n.t('setup.org.access.logout') })
    ).toBeInTheDocument();

    // No form, no identity of the bound administrator anywhere on screen.
    expect(
      screen.queryByLabelText(i18n.t('setup.org.labelName'))
    ).not.toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/@/);
  });

  it('switch-account logs out and navigates to /login', async () => {
    const user = userEvent.setup();
    mockSession('token-a');
    mockAccess({ reason: 'bound_to_another_admin' });
    server.use(
      http.post(url('/v1/auth/operator/logout'), () =>
        HttpResponse.json({ success: true })
      )
    );
    renderRouted();

    await user.click(
      await screen.findByRole('button', {
        name: i18n.t('setup.org.access.switchAccount')
      })
    );

    expect(await screen.findByText('LOGIN_PAGE')).toBeInTheDocument();
  });

  it('{ canClaimRecovery: true } renders the recovery warning ABOVE the form (the claim itself happens only on submit)', async () => {
    mockSession('token-a');
    mockAccess({ canClaimRecovery: true });
    renderRouted();

    expect(
      await screen.findByText(i18n.t('setup.org.access.recoveryWarningTitle'))
    ).toBeInTheDocument();
    expect(
      screen.getByLabelText(i18n.t('setup.org.labelName'))
    ).toBeInTheDocument();
  });

  it('{ reason: "recovery_requires_super_admin" } renders a locked screen explaining an active super_admin must recover setup', async () => {
    mockSession('token-a');
    mockAccess({ reason: 'recovery_requires_super_admin' });
    renderRouted();

    expect(
      await screen.findByText(
        i18n.t('setup.org.access.recoveryRequiresSuperAdminTitle')
      )
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        i18n.t('setup.org.access.recoveryRequiresSuperAdminBody')
      )
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', {
        name: i18n.t('setup.org.access.switchAccount')
      })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: i18n.t('setup.org.access.logout') })
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText(i18n.t('setup.org.labelName'))
    ).not.toBeInTheDocument();
  });

  it('a probe 503 renders a retryable unavailable screen, never a form or a recovery offer', async () => {
    mockSession('token-a');
    let probeCalls = 0;
    server.use(
      http.get(url('/v1/setup/finalization-access'), () => {
        probeCalls += 1;
        return HttpResponse.json(
          { code: 'setup.finalizer_state_unavailable', detail: 'x' },
          { status: 503, headers: { 'Retry-After': '5' } }
        );
      })
    );
    const user = userEvent.setup();
    renderRouted();

    expect(
      await screen.findByText(i18n.t('setup.org.access.unavailableTitle'))
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText(i18n.t('setup.org.labelName'))
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(i18n.t('setup.org.access.recoveryWarningTitle'))
    ).not.toBeInTheDocument();

    await waitFor(() => expect(probeCalls).toBe(1));
    await user.click(
      screen.getByRole('button', { name: i18n.t('setup.gate.retry') })
    );
    await waitFor(() => expect(probeCalls).toBe(2));
  });
});
