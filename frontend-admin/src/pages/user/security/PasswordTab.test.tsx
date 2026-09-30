import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, waitForQuerySettled } from 'test/render';
import { server } from 'test/server';
import { setNavigateToLogin } from 'store/api/baseApi';
import { completeStepUp, subscribeStepUp } from 'store/stepUp';
import {
  currentOperator,
  currentOperatorHandler,
  emptySelfAuthMethods,
  operatorPolicyHandler,
  selfAuthMethodsHandler,
  url
} from 'test/handlers';
import PasswordTab from './PasswordTab';

const oauthOnly = {
  ...emptySelfAuthMethods,
  hasPasswordSet: false,
  passwordUsableForLogin: false,
  hasUsablePassword: false
};

const stubQueries = () =>
  server.use(
    operatorPolicyHandler(),
    selfAuthMethodsHandler(oauthOnly),
    currentOperatorHandler()
  );

const queryCases = [
  {
    path: '/v1/auth/operator/policy',
    endpoint: 'getPasswordEnrollmentPolicy',
    body: {
      registrationEnabled: true,
      loginEnabled: true,
      passwordMinLength: 10,
      passwordLoginEnabled: true,
      passwordLoginBreakGlassEffective: false
    }
  },
  {
    path: '/v1/auth/operator/me/auth-methods',
    endpoint: 'getSelfAuthMethods',
    body: oauthOnly
  },
  {
    path: '/v1/auth/operator/me',
    endpoint: 'getCurrentUser',
    body: currentOperator
  }
];

describe('PasswordTab authoritative modes', () => {
  it.each(queryCases)(
    'waits for $endpoint before showing a form',
    async item => {
      stubQueries();
      let release!: () => void;
      const pending = new Promise<void>(resolve => {
        release = resolve;
      });
      server.use(
        http.get(url(item.path), async () => {
          await pending;
          return HttpResponse.json(item.body);
        })
      );
      const { store } = renderWithProviders(<PasswordTab />);
      for (const other of queryCases.filter(
        q => q.endpoint !== item.endpoint
      )) {
        await waitForQuerySettled(store, other.endpoint);
      }
      expect(screen.getByRole('status')).toBeInTheDocument();
      expect(screen.queryByLabelText('New password')).not.toBeInTheDocument();
      release();
      expect(
        await screen.findByRole('button', { name: 'Add password' })
      ).toBeInTheDocument();
    }
  );

  it.each(queryCases)('fails closed when $endpoint fails', async item => {
    stubQueries();
    server.use(
      http.get(url(item.path), () =>
        HttpResponse.json({ title: 'Unavailable' }, { status: 503 })
      )
    );
    renderWithProviders(<PasswordTab />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      /couldn.t load/i
    );
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument();
  });

  it('does not offer a form when the current user is unavailable', async () => {
    stubQueries();
    server.use(
      http.get(url('/v1/auth/operator/me'), () => HttpResponse.json(null))
    );
    renderWithProviders(<PasswordTab />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      /couldn.t load/i
    );
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument();
  });

  it('offers OAuth-only enrollment with the existing read-only email', async () => {
    stubQueries();
    renderWithProviders(<PasswordTab />);
    expect(await screen.findByLabelText(/account email/i)).toHaveValue(
      'oauth@example.com'
    );
    expect(screen.getByLabelText(/account email/i)).toHaveAttribute('readonly');
    expect(screen.getByLabelText('New password')).toBeRequired();
    expect(screen.getByLabelText('Confirm new password')).toBeRequired();
    expect(
      screen.queryByLabelText(/current password/i)
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Add password' })
    ).toBeInTheDocument();
  });

  it.each([false, null])(
    'hides enrollment when password login policy is %s',
    async enabled => {
      stubQueries();
      server.use(operatorPolicyHandler({ passwordLoginEnabled: enabled }));
      renderWithProviders(<PasswordTab />);
      expect(await screen.findByRole('alert')).toHaveTextContent(
        /contact.*administrator/i
      );
      expect(screen.queryByLabelText('New password')).not.toBeInTheDocument();
      expect(
        screen.queryByRole('button', { name: 'Add password' })
      ).not.toBeInTheDocument();
    }
  );

  it.each([true, false])(
    'retains change-password mode when login policy is %s',
    async enabled => {
      stubQueries();
      server.use(
        operatorPolicyHandler({ passwordLoginEnabled: enabled }),
        selfAuthMethodsHandler({
          ...emptySelfAuthMethods,
          passwordUsableForLogin: enabled
        })
      );
      renderWithProviders(<PasswordTab />);
      expect(await screen.findByLabelText('Current password')).toBeRequired();
      expect(
        screen.getByRole('button', { name: 'Update password' })
      ).toBeInTheDocument();
      if (!enabled)
        expect(screen.getByRole('alert')).toHaveTextContent(
          /disabled on this surface/i
        );
      else expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    }
  );
});

describe('PasswordTab credential submissions', () => {
  it.each(['step_up_required', 'reauthentication_required'])(
    'leaves %s to the global proof handler without a local error',
    async code => {
      stubQueries();
      server.use(
        http.post(url('/v1/auth/operator/me/password'), () =>
          HttpResponse.json({ code, detail: 'Proof required' }, { status: 401 })
        )
      );
      const navigate = vi.fn();
      setNavigateToLogin(navigate);
      window.history.pushState({}, '', '/user/security?tab=password');
      let proofOpened = false;
      const unsubscribe = subscribeStepUp(open => {
        if (open) {
          proofOpened = true;
          queueMicrotask(() => completeStepUp(false));
        }
      });
      try {
        const user = userEvent.setup();
        renderWithProviders(<PasswordTab />);
        await user.type(
          await screen.findByLabelText('New password'),
          'Correct-Horse-42!'
        );
        await user.type(
          screen.getByLabelText('Confirm new password'),
          'Correct-Horse-42!'
        );
        await user.click(screen.getByRole('button', { name: 'Add password' }));
        await screen.findByRole('button', { name: 'Add password' });
        await waitFor(() =>
          expect(
            screen.getByRole('button', { name: 'Add password' })
          ).toBeEnabled()
        );
        if (code === 'step_up_required') expect(proofOpened).toBe(true);
        else
          expect(navigate).toHaveBeenCalledWith('/user/security?tab=password');
        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      } finally {
        unsubscribe();
        completeStepUp(false);
        setNavigateToLogin(() => {});
        window.history.pushState({}, '', '/');
      }
    }
  );

  it.each([200, 409])(
    'clears secrets and switches mode after enrollment returns %s',
    async status => {
      stubQueries();
      let hasPassword = false;
      let payload: unknown;
      server.use(
        http.get(url('/v1/auth/operator/me/auth-methods'), () =>
          HttpResponse.json(hasPassword ? emptySelfAuthMethods : oauthOnly)
        ),
        http.post(url('/v1/auth/operator/me/password'), async ({ request }) => {
          payload = await request.json();
          hasPassword = true;
          return HttpResponse.json(
            status === 200
              ? { message: 'Password added' }
              : {
                  title: 'Conflict',
                  detail: 'A password already exists',
                  code: 'auth.password_already_set'
                },
            { status }
          );
        })
      );
      const user = userEvent.setup();
      renderWithProviders(<PasswordTab />);
      await user.type(
        await screen.findByLabelText('New password'),
        'Correct-Horse-42!'
      );
      await user.type(
        screen.getByLabelText('Confirm new password'),
        'Correct-Horse-42!'
      );
      await user.click(screen.getByRole('button', { name: 'Add password' }));
      expect(await screen.findByLabelText('Current password')).toHaveValue('');
      expect(payload).toEqual({ newPassword: 'Correct-Horse-42!' });
      expect(screen.getByLabelText('New password')).toHaveValue('');
      expect(screen.getByLabelText('Confirm new password')).toHaveValue('');
      if (status === 409)
        expect(screen.getByRole('alert')).toHaveTextContent(
          /already.*password/i
        );
    }
  );

  it('requires the current password in change mode and sends the change payload', async () => {
    stubQueries();
    server.use(selfAuthMethodsHandler());
    let payload: unknown;
    server.use(
      http.post(
        url('/v1/auth/operator/change-password'),
        async ({ request }) => {
          payload = await request.json();
          return HttpResponse.json({ message: 'Updated' });
        }
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<PasswordTab />);
    await user.type(
      await screen.findByLabelText('New password'),
      'Correct-Horse-42!'
    );
    await user.type(
      screen.getByLabelText('Confirm new password'),
      'Correct-Horse-42!'
    );
    await user.click(screen.getByRole('button', { name: 'Update password' }));
    expect(
      await screen.findByText(/enter.*current password/i)
    ).toBeInTheDocument();
    expect(payload).toBeUndefined();
    await user.type(
      screen.getByLabelText('Current password'),
      'Old-Password-42!'
    );
    await user.click(screen.getByRole('button', { name: 'Update password' }));
    await waitFor(() =>
      expect(payload).toEqual({
        currentPassword: 'Old-Password-42!',
        newPassword: 'Correct-Horse-42!'
      })
    );
    await waitFor(() =>
      expect(screen.getByLabelText('New password')).toHaveValue('')
    );
  });
});
