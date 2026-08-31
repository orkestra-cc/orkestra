import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import {
  emptySelfAuthMethods,
  operatorPolicyHandler,
  selfAuthMethodsHandler
} from 'test/handlers';
import PasswordTab from './PasswordTab';

// The tab fires TWO queries on mount (PasswordTab.tsx:18-19: the auth
// policy for passwordMinLength, and the self auth methods); MSW runs with
// onUnhandledRequest: 'error', so both are stubbed in every test.
describe('PasswordTab split password fields (PR 3 §4.8)', () => {
  it('set-but-unusable: form stays (credential management), notice shows', async () => {
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: true,
        passwordUsableForLogin: false
      })
    );
    renderWithProviders(<PasswordTab />);
    expect(
      await screen.findByText(/disabled on this surface/i)
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/current password/i)).toBeInTheDocument();
  });

  it('usable password: no notice', async () => {
    server.use(operatorPolicyHandler(), selfAuthMethodsHandler());
    renderWithProviders(<PasswordTab />);
    await screen.findByLabelText(/current password/i);
    expect(screen.queryByText(/disabled on this surface/i)).toBeNull();
  });

  it('no hash: current password stays rendered but stops being required', async () => {
    // The component never removes the field — it flips `required`
    // (PasswordTab.tsx:96, `required={hasPassword}`); the PR 3 change is
    // only WHICH view field feeds hasPassword (hasPasswordSet, not the
    // deprecated alias).
    server.use(
      operatorPolicyHandler(),
      selfAuthMethodsHandler({
        ...emptySelfAuthMethods,
        hasPasswordSet: false,
        passwordUsableForLogin: false,
        hasUsablePassword: false
      })
    );
    renderWithProviders(<PasswordTab />);
    const current = await screen.findByLabelText(/current password/i);
    expect(current).toBeInTheDocument();
    expect(current).not.toBeRequired();
  });
});
