import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { Routes, Route, useLocation } from 'react-router';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import LoginMfaVerify from './LoginMfaVerify';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return <div data-testid={`${label}-pathname`}>{location.pathname}</div>;
};

const okVerify = http.post('*/v1/auth/operator/mfa/login/verify', () =>
  HttpResponse.json({
    success: true,
    user: {
      id: 'u-1',
      email: 'op@example.com',
      fullName: 'Op User',
      isActive: true,
      roles: ['operator'],
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z'
    }
  })
);

const renderMfa = (state: Record<string, unknown>) =>
  renderWithProviders(
    <Routes>
      <Route path="/mfa/verify" element={<LoginMfaVerify />} />
      <Route path={DEFAULT_POST_LOGIN} element={<Probe label="dashboard" />} />
      <Route path="/admin/modules" element={<Probe label="deeplink" />} />
      <Route path="/login" element={<Probe label="login" />} />
    </Routes>,
    { routerEntries: [{ pathname: '/mfa/verify', state }] }
  );

const submitCode = async () => {
  const user = userEvent.setup();
  await user.type(screen.getByRole('textbox'), '123456');
  await user.click(screen.getByRole('button', { name: /verify and sign in/i }));
};

describe('LoginMfaVerify', () => {
  it('lands on the forwarded returnTo after a successful TOTP verify', async () => {
    server.use(okVerify);

    renderMfa({ challengeId: 'challenge-abc', returnTo: '/admin/modules' });
    await submitCode();

    expect(await screen.findByTestId('deeplink-pathname')).toHaveTextContent(
      '/admin/modules'
    );
  });

  it('falls back to the dashboard when no returnTo was forwarded', async () => {
    server.use(okVerify);

    renderMfa({ challengeId: 'challenge-abc' });
    await submitCode();

    expect(await screen.findByTestId('dashboard-pathname')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
  });

  it('ignores an off-site returnTo and uses the dashboard', async () => {
    server.use(okVerify);

    renderMfa({ challengeId: 'challenge-abc', returnTo: '//evil.com' });
    await submitCode();

    expect(await screen.findByTestId('dashboard-pathname')).toHaveTextContent(
      DEFAULT_POST_LOGIN
    );
  });
});
