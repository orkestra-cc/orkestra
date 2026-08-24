// OrgStep — the mandatory organization form. Access-state rendering (sign-in
// prompt, locked screens, recovery, unavailable) lives in
// OrgStep.access.test.tsx; this file covers the form itself: empty initial
// state, no Skip control, the choice→payload mapping, the 202→200 retry
// loop, and the session-re-mint failure/retry path — always against a
// `{ canFinalize: true }` access probe so the form is what's on screen.
import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import i18n from '../../../i18n';
import OrgStep from './OrgStep';

const sessionBody = (accessToken: string) => ({
  accessToken,
  tokenType: 'Bearer',
  expiresIn: 900,
  user: { id: 'u1', email: 'admin@example.com', role: 'developer' },
  authenticated: true,
  success: true
});

const mockAccess = () =>
  server.use(
    http.get(url('/v1/setup/finalization-access'), () =>
      HttpResponse.json({
        canFinalize: true,
        canClaimRecovery: false,
        reason: ''
      })
    )
  );

const finalize200Body = {
  tenantId: 't1',
  tenantName: 'Acme',
  tenantSlug: 'acme',
  mode: 'single' as const,
  allowAdditionalInternalTenants: false
};

// `sessionResolver` — when a test needs the session response to change
// across calls (the re-mint retry tests) — is registered LAST, after
// mockAccess, so it is the winning handler for GET /v1/auth/session; MSW's
// server.use() gives priority to the most-recently-added match, so any
// earlier per-test server.use() for the same URL would otherwise be
// shadowed by this helper's default.
const renderOrgStep = ({
  onNext = vi.fn<(tenantName: string, allowAdditional: boolean) => void>(),
  sessionResolver
}: {
  onNext?: (tenantName: string, allowAdditional: boolean) => void;
  sessionResolver?: () => ReturnType<typeof HttpResponse.json>;
} = {}) => {
  mockAccess();
  server.use(
    http.get(
      url('/v1/auth/session'),
      sessionResolver ?? (() => HttpResponse.json(sessionBody('token-a')))
    )
  );
  const result = renderWithProviders(<OrgStep onNext={onNext} />);
  return { ...result, onNext };
};

const fillNameAndSlug = async (user: ReturnType<typeof userEvent.setup>) => {
  const nameInput = await screen.findByLabelText(i18n.t('setup.org.labelName'));
  await user.type(nameInput, 'Acme HQ');
  return nameInput;
};

afterEach(async () => {
  await new Promise(resolve => setTimeout(resolve, 0));
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('OrgStep — mandatory organization form', () => {
  it('renders empty with no preselected choice and no Skip control; submit enables only once name + slug + a choice are set', async () => {
    const user = userEvent.setup();
    renderOrgStep();

    const nameInput = await screen.findByLabelText(
      i18n.t('setup.org.labelName')
    );
    const slugInput = screen.getByLabelText(i18n.t('setup.org.labelSlug'));
    expect(nameInput).toHaveValue('');
    expect(slugInput).toHaveValue('');

    const allowRadio = screen.getByRole('radio', {
      name: i18n.t('setup.org.provisioning.allowLabel')
    });
    const singleRadio = screen.getByRole('radio', {
      name: i18n.t('setup.org.provisioning.singleLabel')
    });
    expect(allowRadio).not.toBeChecked();
    expect(singleRadio).not.toBeChecked();

    // No skip control anywhere — the key was removed once the step became
    // mandatory (see locales/en.json), so only the absence of any "skip"
    // affordance is worth asserting here.
    expect(
      screen.queryByRole('button', { name: /skip/i })
    ).not.toBeInTheDocument();

    const submit = screen.getByRole('button', {
      name: i18n.t('setup.org.submit')
    });
    expect(submit).toBeDisabled();

    await user.type(nameInput, 'Acme HQ');
    expect(slugInput).toHaveValue('acme-hq'); // still auto-derived
    expect(submit).toBeDisabled(); // no choice yet

    await user.click(allowRadio);
    expect(submit).toBeEnabled();
  });

  it('never renders the removed "{{firstName}}\'s Workspace" suggestion', async () => {
    renderOrgStep();
    await screen.findByLabelText(i18n.t('setup.org.labelName'));
    expect(screen.queryByText(/workspace/i)).not.toBeInTheDocument();
  });

  it('maps "allow additional" to true and "do not allow" to false in the submitted payload', async () => {
    const user = userEvent.setup();
    const bodies: unknown[] = [];
    server.use(
      http.post(url('/v1/setup/finalize'), async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(finalize200Body);
      }),
      http.get(url('/v1/auth/session'), () =>
        HttpResponse.json(sessionBody('token-b'))
      )
    );
    renderOrgStep();

    await fillNameAndSlug(user);
    await user.click(
      screen.getByRole('radio', {
        name: i18n.t('setup.org.provisioning.allowLabel')
      })
    );
    await user.click(
      screen.getByRole('button', { name: i18n.t('setup.org.submit') })
    );

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ allowAdditionalInternalTenants: true });
  });

  it('maps "do not allow" to false in the submitted payload', async () => {
    const user = userEvent.setup();
    const bodies: unknown[] = [];
    server.use(
      http.post(url('/v1/setup/finalize'), async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(finalize200Body);
      }),
      http.get(url('/v1/auth/session'), () =>
        HttpResponse.json(sessionBody('token-b'))
      )
    );
    renderOrgStep();

    await fillNameAndSlug(user);
    await user.click(
      screen.getByRole('radio', {
        name: i18n.t('setup.org.provisioning.singleLabel')
      })
    );
    await user.click(
      screen.getByRole('button', { name: i18n.t('setup.org.submit') })
    );

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ allowAdditionalInternalTenants: false });
  });

  it('202 keeps the form locked with in-progress copy, retries the IDENTICAL payload after Retry-After, then completes on 200', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup();

    const bodies: unknown[] = [];
    let statusCalls = 0;
    let finalizeCalls = 0;
    server.use(
      http.post(url('/v1/setup/finalize'), async ({ request }) => {
        bodies.push(await request.json());
        finalizeCalls += 1;
        if (finalizeCalls === 1) {
          return HttpResponse.json(
            { state: 'setup.finalization_in_progress' },
            { status: 202, headers: { 'Retry-After': '1' } }
          );
        }
        return HttpResponse.json(finalize200Body);
      }),
      http.get(url('/v1/setup/status'), () => {
        statusCalls += 1;
        return HttpResponse.json({
          setupCompleted: false,
          phase: 'tenant_required',
          smtpConfigured: false
        });
      })
    );

    const { onNext } = renderOrgStep({
      sessionResolver: () => HttpResponse.json(sessionBody('token-b'))
    });

    await fillNameAndSlug(user);
    await user.click(
      screen.getByRole('radio', {
        name: i18n.t('setup.org.provisioning.singleLabel')
      })
    );
    const submitButton = screen.getByRole('button', {
      name: i18n.t('setup.org.submit')
    });
    await user.click(submitButton);

    await screen.findByText(i18n.t('setup.org.inProgressBody'));
    expect(submitButton).toBeDisabled();
    expect(bodies).toHaveLength(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });

    await waitFor(() => expect(finalizeCalls).toBe(2));
    // Identical payload — the retry never mutates it.
    expect(bodies[1]).toEqual(bodies[0]);
    expect(statusCalls).toBeGreaterThanOrEqual(1);

    await waitFor(() => expect(onNext).toHaveBeenCalledWith('Acme', false));
  });

  it('after a 200 whose session re-mint fails, stays in a recoverable state with no navigation and no tenant-scoped request; retry hydrates memberships and advances', async () => {
    const user = userEvent.setup();
    let sessionCalls = 0;
    server.use(
      http.post(url('/v1/setup/finalize'), () =>
        HttpResponse.json(finalize200Body)
      )
    );

    const { onNext, store } = renderOrgStep({
      sessionResolver: () => {
        sessionCalls += 1;
        if (sessionCalls === 1) {
          // Auth-boundary check at mount.
          return HttpResponse.json(sessionBody('token-a'));
        }
        if (sessionCalls === 2) {
          // First re-mint attempt fails.
          return HttpResponse.json({ authenticated: false, success: true });
        }
        // Retry's re-mint succeeds.
        return HttpResponse.json(sessionBody('token-b'));
      }
    });

    await fillNameAndSlug(user);
    await user.click(
      screen.getByRole('radio', {
        name: i18n.t('setup.org.provisioning.singleLabel')
      })
    );
    await user.click(
      screen.getByRole('button', { name: i18n.t('setup.org.submit') })
    );

    await screen.findByText(i18n.t('setup.org.refreshingSessionError'));
    expect(onNext).not.toHaveBeenCalled();
    expect(store.getState().tenant.memberships).toHaveLength(0);
    expect(store.getState().auth.accessToken).toBe('token-a');

    await user.click(
      screen.getByRole('button', { name: i18n.t('setup.gate.retry') })
    );

    await waitFor(() => expect(onNext).toHaveBeenCalledWith('Acme', false));
    expect(store.getState().auth.accessToken).toBe('token-b');
    expect(store.getState().tenant.memberships[0]).toMatchObject({
      tenantId: 't1',
      roles: ['org_owner'],
      isOwner: true
    });
  });
});
