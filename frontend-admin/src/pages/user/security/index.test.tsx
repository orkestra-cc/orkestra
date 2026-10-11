import { beforeEach, describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  emptySelfAuthMethods,
  mySessionsHandler,
  operatorPolicyHandler,
  selfAuthMethodsHandler,
  trustedDevicesHandler,
  url
} from 'test/handlers';
import SecurityPage from './index';

// The AI accounts tab is gated on llm.accounts.self, so useAuth is mocked
// (precedent: pages/admin/llm/BudgetTab.test.tsx). No other tab reads it.
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({ useAuth: () => mockedUseAuth() }));
const authWith = (permissions: string[]) => ({
  hasPermission: (p: string) => permissions.includes(p)
});
beforeEach(() => mockedUseAuth.mockReturnValue(authWith([])));

// The KPI row derives from three queries the tabs already hold. A query that
// FAILED is not a zero, and on a security page that distinction is the whole
// point: `?? 0` alone renders a transient /auth-methods outage as an
// authoritative "Two-factor: Off", telling someone their account is
// unprotected when it is not.
describe('SecurityPage summary row', () => {
  it('reports real state when the queries land', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        mfaFactors: [{ type: 'totp', backupCodesRemaining: 10 }]
      }),
      mySessionsHandler(),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    expect(await screen.findByText(/^on$/i)).toBeInTheDocument();
    expect(screen.getByText(/authenticator app/i)).toBeInTheDocument();
  });

  it('says "Off" only when the account really has no second factor', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler(),
      mySessionsHandler(),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    expect(await screen.findByText(/^off$/i)).toBeInTheDocument();
    expect(screen.getByText(/no second factor enrolled/i)).toBeInTheDocument();
  });

  it('never claims "Off" when the auth-methods query failed', async () => {
    server.use(
      operatorPolicyHandler(),
      http.get(
        url('/v1/auth/operator/me/auth-methods'),
        () => new HttpResponse(null, { status: 500 })
      ),
      mySessionsHandler(),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    // The tile resolves to the unavailable copy, not to a verdict about the
    // account, and carries no zero-count sibling claim either.
    expect(await screen.findAllByText(/couldn’t load/i)).not.toHaveLength(0);
    expect(screen.queryByText(/^off$/i)).not.toBeInTheDocument();
    expect(
      screen.queryByText(/no second factor enrolled/i)
    ).not.toBeInTheDocument();
  });

  it('shows a dash, not 0, for a failed session count', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler(),
      http.get(
        url('/v1/auth/operator/me/sessions'),
        () => new HttpResponse(null, { status: 503 })
      ),
      trustedDevicesHandler()
    );
    renderWithProviders(<SecurityPage />);

    // Anchor on the ERROR copy, not on an alternation that includes the
    // healthy subtitle: the tile renders that subtitle while the query is
    // still in flight, so the alternation resolves on first paint and the
    // assertions below would race a tile that is merely loading.
    await screen.findByText(/couldn’t load/i);
    // "active sessions" also appears in the page subtitle — take the match
    // that actually sits inside a KPI tile.
    const sessionsTile = screen
      .getAllByText(/active sessions/i)
      .map(el => el.closest('.stat-card'))
      .find(Boolean)!;
    expect(sessionsTile).toHaveTextContent('—');
    expect(sessionsTile).not.toHaveTextContent(/\b0\b/);
  });
});

describe('SecurityPage AI accounts tab', () => {
  const summary = () => [
    operatorPolicyHandler(),
    selfAuthMethodsHandler(),
    mySessionsHandler(),
    trustedDevicesHandler()
  ];

  it('is hidden without llm.accounts.self, and its URL degrades to the password tab', async () => {
    server.use(...summary());
    renderWithProviders(<SecurityPage />, {
      routerEntries: ['/user/security?tab=ai']
    });
    expect(
      await screen.findByRole('tab', { name: /password/i })
    ).toHaveAttribute('aria-selected', 'true');
    expect(
      screen.queryByRole('tab', { name: /ai accounts/i })
    ).not.toBeInTheDocument();
  });

  it('opens through the URL with llm.accounts.self', async () => {
    mockedUseAuth.mockReturnValue(authWith(['llm.accounts.self']));
    server.use(
      ...summary(),
      http.get(url('/v1/llm/accounts/openai'), () =>
        HttpResponse.json({ mode: 'hosted_paste', items: [] })
      )
    );
    renderWithProviders(<SecurityPage />, {
      routerEntries: ['/user/security?tab=ai']
    });
    expect(
      await screen.findByRole('tab', { name: /ai accounts/i })
    ).toHaveAttribute('aria-selected', 'true');
    expect(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    ).toBeInTheDocument();
  });
});
