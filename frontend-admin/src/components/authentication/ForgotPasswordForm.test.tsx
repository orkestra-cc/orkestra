import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { operatorPolicyHandler } from 'test/handlers';
import ForgotPasswordForm from './ForgotPasswordForm';

describe('ForgotPasswordForm password-method gate (PR 3 §4.10)', () => {
  it.each([
    ['persisted false', { passwordLoginEnabled: false }],
    ['emergency null', { passwordLoginEnabled: null }],
    [
      'break-glass does not reopen it',
      { passwordLoginEnabled: false, passwordLoginBreakGlassEffective: true }
    ]
  ])('renders only the disabled alert — %s', async (_name, overrides) => {
    server.use(operatorPolicyHandler(overrides));
    renderWithProviders(<ForgotPasswordForm />);
    expect(
      await screen.findByText(/email\/password sign-in is disabled/i)
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /send|submit|reset/i })
    ).not.toBeInTheDocument();
  });

  it('renders the working form when the method is on', async () => {
    server.use(operatorPolicyHandler({}));
    renderWithProviders(<ForgotPasswordForm />);
    expect(await screen.findByLabelText(/email/i)).toBeInTheDocument();
  });
});
