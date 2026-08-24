// SetupGate: the top-level guard that routes a fresh (or degraded) install.
// Three states matter here:
//  - a 503 setup.status_unavailable must render a neutral, retryable
//    "service unavailable" screen — never the wizard, never the console,
//    and never treated as a cached phase;
//  - phase !== 'complete' off /setup redirects to the wizard;
//  - phase === 'complete' renders children.
// The stale-tenant-state cleanup effect must key off the same phase
// condition as the redirect, not the legacy setupCompleted boolean.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { Routes, Route } from 'react-router';

import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import i18n from '../../i18n';
import SetupGate from './SetupGate';

// baseQueryWithRetry fires a generic "Server error" toast for any 5xx
// whose code isn't in its featureNotConfiguredCodes allowlist — which
// setup.status_unavailable is not, by design (it is not "an optional
// feature isn't configured", it's a transient outage). Keep toast calls
// silent and inert so a stray call doesn't blow up happy-dom, mirroring
// setupApi.test.ts.
vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const renderGate = (
  children: React.ReactNode,
  { initialPath = '/dashboard', currentOrgId = null as string | null } = {}
) =>
  renderWithProviders(
    <Routes>
      <Route path="/setup" element={<div>SETUP_WIZARD</div>} />
      <Route path="*" element={<SetupGate>{children}</SetupGate>} />
    </Routes>,
    {
      routerEntries: [initialPath],
      preloadedState: {
        tenant: {
          memberships: [],
          currentOrgId,
          permissions: [],
          features: [],
          systemRole: '',
          loading: false,
          error: null,
          impersonatedTenantId: null,
          impersonatedTenantName: null
        }
      }
    }
  );

const unavailableBody = { code: 'setup.status_unavailable', detail: 'x' };

afterEach(() => {
  vi.useRealTimers();
});

describe('SetupGate — 503 setup.status_unavailable', () => {
  it('renders the neutral unavailable state, not the wizard nor children', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json(unavailableBody, {
          status: 503,
          headers: { 'Retry-After': '5' }
        })
      )
    );

    renderGate(<div>PROTECTED_CONTENT</div>);

    expect(
      await screen.findByText(i18n.t('setup.gate.unavailableTitle'))
    ).toBeInTheDocument();
    expect(screen.queryByText('SETUP_WIZARD')).not.toBeInTheDocument();
    expect(screen.queryByText('PROTECTED_CONTENT')).not.toBeInTheDocument();
  });

  it('does not redirect to /setup while errored', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json(unavailableBody, {
          status: 503,
          headers: { 'Retry-After': '5' }
        })
      )
    );

    renderGate(<div>PROTECTED_CONTENT</div>);

    await screen.findByText(i18n.t('setup.gate.unavailableTitle'));
    // Give any stray navigation a chance to happen before asserting it didn't.
    await new Promise(resolve => setTimeout(resolve, 20));
    expect(screen.queryByText('SETUP_WIZARD')).not.toBeInTheDocument();
  });

  it('retry button refetches the status probe', async () => {
    let statusCalls = 0;
    server.use(
      http.get(url('/v1/setup/status'), () => {
        statusCalls += 1;
        return HttpResponse.json(unavailableBody, {
          status: 503,
          headers: { 'Retry-After': '5' }
        });
      })
    );

    renderGate(<div>PROTECTED_CONTENT</div>);
    await screen.findByText(i18n.t('setup.gate.unavailableTitle'));
    await waitFor(() => expect(statusCalls).toBe(1));

    fireEvent.click(
      screen.getByRole('button', { name: i18n.t('setup.gate.retry') })
    );

    await waitFor(() => expect(statusCalls).toBe(2));
  });

  it('honors Retry-After: an automatic refetch fires after the delay, not before', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let statusCalls = 0;
    server.use(
      http.get(url('/v1/setup/status'), () => {
        statusCalls += 1;
        return HttpResponse.json(unavailableBody, {
          status: 503,
          headers: { 'Retry-After': '5' }
        });
      })
    );

    renderGate(<div>PROTECTED_CONTENT</div>);
    await screen.findByText(i18n.t('setup.gate.unavailableTitle'));
    await waitFor(() => expect(statusCalls).toBe(1));

    // Not yet — the delay hasn't elapsed.
    await vi.advanceTimersByTimeAsync(4000);
    expect(statusCalls).toBe(1);

    // Now it has.
    await vi.advanceTimersByTimeAsync(1500);
    await waitFor(() => expect(statusCalls).toBe(2));
  });
});

describe('SetupGate — phase routing', () => {
  // setupCompleted is deliberately the OPPOSITE of what phase implies below.
  // The backend always keeps them in sync in real traffic (setupCompleted is
  // derived server-side from phase === 'complete'), so a test that sends
  // matching values would pass against the old setupCompleted-keyed gate
  // too. Mismatching them is the only way to pin that the component reads
  // `phase` and ignores the legacy boolean — a regression guard against
  // silently reintroducing the setupCompleted check.
  it('redirects to /setup when phase is tenant_required and off the setup path', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json({
          setupCompleted: true,
          phase: 'tenant_required',
          smtpConfigured: false
        })
      )
    );

    renderGate(<div>PROTECTED_CONTENT</div>, { initialPath: '/dashboard' });

    expect(await screen.findByText('SETUP_WIZARD')).toBeInTheDocument();
    expect(screen.queryByText('PROTECTED_CONTENT')).not.toBeInTheDocument();
  });

  it('renders children when phase is complete', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json({
          setupCompleted: false,
          phase: 'complete',
          smtpConfigured: true
        })
      )
    );

    renderGate(<div>PROTECTED_CONTENT</div>, { initialPath: '/dashboard' });

    expect(await screen.findByText('PROTECTED_CONTENT')).toBeInTheDocument();
    expect(screen.queryByText('SETUP_WIZARD')).not.toBeInTheDocument();
  });
});

describe('SetupGate — stale tenant-state cleanup', () => {
  // Same deliberate setupCompleted/phase mismatch as above, for the same
  // reason: pin that the cleanup effect keys off phase, not the legacy
  // boolean.
  it('resets tenant state when the phase is not complete', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json({
          setupCompleted: true,
          phase: 'admin_required',
          smtpConfigured: false
        })
      )
    );

    const { store } = renderGate(<div>PROTECTED_CONTENT</div>, {
      initialPath: '/dashboard',
      currentOrgId: 'stale-org'
    });

    await screen.findByText('SETUP_WIZARD');
    await waitFor(() =>
      expect(store.getState().tenant.currentOrgId).toBeNull()
    );
  });

  it('does not reset tenant state when the phase is complete', async () => {
    server.use(
      http.get(url('/v1/setup/status'), () =>
        HttpResponse.json({
          setupCompleted: false,
          phase: 'complete',
          smtpConfigured: true
        })
      )
    );

    const { store } = renderGate(<div>PROTECTED_CONTENT</div>, {
      initialPath: '/dashboard',
      currentOrgId: 'kept-org'
    });

    await screen.findByText('PROTECTED_CONTENT');
    // Give the effect a tick to (not) fire before asserting it didn't.
    await new Promise(resolve => setTimeout(resolve, 20));
    expect(store.getState().tenant.currentOrgId).toBe('kept-org');
  });
});
