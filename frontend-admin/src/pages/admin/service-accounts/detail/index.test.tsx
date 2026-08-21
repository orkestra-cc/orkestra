import { describe, it, expect } from 'vitest';
import { delay, http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router';
import { ToastContainer } from 'react-toastify';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { ServiceAccountDetail } from 'types/serviceAccounts';
import ServiceAccountDetailPage from './index';

// ServiceAccountDetailPage mounts at /admin/service-accounts/:id — the
// production route (coreRoutes.tsx) supplies the param via react-router, so
// tests mount the same param'd Route rather than the bare page (mirrors
// pages/admin/modules/detail/index.test.tsx's renderAt pattern).

const detail: ServiceAccountDetail = {
  id: 'sa-1',
  name: 'hermes-agent',
  email: 'sa-1@service.local',
  isActive: true,
  activeCredentials: 1,
  createdAt: '2026-08-01T10:00:00Z',
  credentials: [
    {
      id: 'cred-1',
      clientId: 'client-abc123',
      label: 'ci-pipeline',
      createdAt: '2026-08-01T10:00:00Z',
      lastUsedAt: '2026-08-15T09:30:00Z'
    },
    {
      id: 'cred-2',
      clientId: 'client-def456',
      label: 'staging',
      createdAt: '2026-07-01T10:00:00Z',
      revokedAt: '2026-08-10T12:00:00Z'
      // no lastUsedAt — must render '—', not a Go zero-time sentinel.
    }
  ]
};

const stubDetail = (body: ServiceAccountDetail = detail) => {
  server.use(
    http.get(url('/v1/admin/service-accounts/sa-1'), () =>
      HttpResponse.json(body)
    )
  );
};

const renderPage = () =>
  renderWithProviders(
    <>
      <Routes>
        <Route
          path="/admin/service-accounts/:id"
          element={<ServiceAccountDetailPage />}
        />
      </Routes>
      <ToastContainer />
    </>,
    { routerEntries: ['/admin/service-accounts/sa-1'] }
  );

describe('ServiceAccountDetailPage', () => {
  it('renders account summary and the credentials table', async () => {
    stubDetail();
    renderPage();

    expect(
      await screen.findByRole('heading', { name: 'hermes-agent' })
    ).toBeInTheDocument();

    const metaRow = screen.getByText('sa-1@service.local').closest('div');
    expect(metaRow).not.toBeNull();
    expect(within(metaRow!).getByText('Active')).toBeInTheDocument();

    const row1 = screen.getByText('client-abc123').closest('tr');
    expect(row1).not.toBeNull();
    expect(row1).toHaveTextContent('ci-pipeline');
    expect(within(row1!).getByText('Active')).toBeInTheDocument();

    const row2 = screen.getByText('client-def456').closest('tr');
    expect(row2).not.toBeNull();
    expect(within(row2!).getByText('Revoked')).toBeInTheDocument();
    // lastUsedAt absent on the revoked credential — Go zero-time guard.
    expect(row2).toHaveTextContent('—');
  });

  it('links to the admin user profile for the account (AccountView.ID is the user UUID)', async () => {
    stubDetail();
    renderPage();

    const manageLink = await screen.findByRole('link', {
      name: /manage roles & membership/i
    });
    expect(manageLink).toHaveAttribute('href', '/admin/user/profile/sa-1');
  });

  it('toggles active state via the switch, PATCHes {active:false}, and toasts', async () => {
    stubDetail();
    let body: unknown = null;
    server.use(
      http.patch(
        url('/v1/admin/service-accounts/sa-1'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({ ...detail, isActive: false });
        }
      )
    );
    const user = userEvent.setup();
    renderPage();

    const toggle = await screen.findByRole('checkbox');
    await user.click(toggle);

    await waitFor(() => expect(body).toEqual({ active: false }));
    expect(
      await screen.findByText('"hermes-agent" disabled')
    ).toBeInTheDocument();
  });

  it('does not double-fire the PATCH on a rapid double-click of the switch', async () => {
    stubDetail();
    let patchCount = 0;
    server.use(
      http.patch(
        url('/v1/admin/service-accounts/sa-1'),
        async ({ request }) => {
          patchCount += 1;
          await request.json();
          // Keeps the mutation in flight across the second click so the
          // in-flight guard (disabled={isUpdating}) actually gets exercised
          // instead of the second click landing after the first resolved.
          await delay(50);
          return HttpResponse.json({ ...detail, isActive: false });
        }
      )
    );
    const user = userEvent.setup();
    renderPage();

    const toggle = await screen.findByRole('checkbox');
    await user.click(toggle);
    // The switch should already be disabled by the in-flight PATCH — this
    // second click must be a no-op, not a second request.
    expect(toggle).toBeDisabled();
    await user.click(toggle);

    // Let the in-flight request resolve, then confirm only one PATCH ever
    // reached the server.
    await waitFor(() => expect(toggle).not.toBeDisabled());
    expect(patchCount).toBe(1);
  });

  it('renames the account via the inline form, PATCHes {name}, and toasts', async () => {
    stubDetail();
    let body: unknown = null;
    server.use(
      http.patch(
        url('/v1/admin/service-accounts/sa-1'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({ ...detail, name: 'hermes-agent-2' });
        }
      )
    );
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole('heading', { name: 'hermes-agent' });
    await user.click(screen.getByRole('button', { name: /rename/i }));

    const nameInput = await screen.findByLabelText(/^name$/i);
    await user.clear(nameInput);
    await user.type(nameInput, 'hermes-agent-2');
    await user.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => expect(body).toEqual({ name: 'hermes-agent-2' }));
    expect(
      await screen.findByText('Renamed to "hermes-agent-2"')
    ).toBeInTheDocument();
  });

  it('does not double-submit the rename PATCH on a rapid double-click of Save', async () => {
    stubDetail();
    let patchCount = 0;
    server.use(
      http.patch(
        url('/v1/admin/service-accounts/sa-1'),
        async ({ request }) => {
          patchCount += 1;
          await request.json();
          // Mirrors the switch double-fire test: keep the mutation in
          // flight across the second click so the shared isUpdating guard
          // (disabled={isUpdating} on Save/Cancel) actually gets exercised.
          await delay(50);
          return HttpResponse.json({ ...detail, name: 'hermes-agent-2' });
        }
      )
    );
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole('heading', { name: 'hermes-agent' });
    await user.click(screen.getByRole('button', { name: /rename/i }));

    const nameInput = await screen.findByLabelText(/^name$/i);
    await user.clear(nameInput);
    await user.type(nameInput, 'hermes-agent-2');

    const saveButton = screen.getByRole('button', { name: /^save$/i });
    await user.click(saveButton);
    expect(saveButton).toBeDisabled();
    // Second click lands on an already-disabled Save — must be a no-op.
    await user.click(saveButton);

    // The inline rename form closes on success — wait for that, then
    // confirm only one PATCH ever reached the server.
    await waitFor(() =>
      expect(screen.queryByLabelText(/^name$/i)).not.toBeInTheDocument()
    );
    expect(patchCount).toBe(1);
  });

  it('issues a credential, shows the secret once, and gates Done on ack', async () => {
    stubDetail();
    let body: unknown = null;
    server.use(
      http.post(
        url('/v1/admin/service-accounts/sa-1/credentials'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({
            id: 'cred-3',
            clientId: 'client-new789',
            label: 'ci-two',
            createdAt: '2026-08-18T00:00:00Z',
            clientSecret: 'super-secret-once'
          });
        }
      )
    );
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole('heading', { name: 'hermes-agent' });
    await user.click(screen.getByRole('button', { name: /issue credential/i }));

    const labelInput = await screen.findByLabelText(/^label$/i);
    await user.type(labelInput, 'ci-two');
    await user.click(screen.getByRole('button', { name: /^issue$/i }));

    await waitFor(() => expect(body).toEqual({ label: 'ci-two' }));

    // Own secretTitle, not createModal's borrowed "Service account created".
    // Scoped to the dialog — the successToast text is the identical string
    // "Credential issued" and would otherwise collide with the title.
    expect(
      within(screen.getByRole('dialog')).getByText('Credential issued')
    ).toBeInTheDocument();
    expect(await screen.findByText('super-secret-once')).toBeInTheDocument();
    expect(screen.getByText('client-new789')).toBeInTheDocument();

    const doneButton = screen.getByRole('button', { name: /done/i });
    expect(doneButton).toBeDisabled();

    // The account's active switch is also a checkbox on the page behind the
    // modal, so scope to the dialog to reach the ack checkbox unambiguously.
    const dialog = screen.getByRole('dialog');
    await user.click(within(dialog).getByRole('checkbox'));
    expect(doneButton).toBeEnabled();
  });

  it('surfaces the credential-cap toast on a 409 from issue', async () => {
    stubDetail();
    server.use(
      http.post(url('/v1/admin/service-accounts/sa-1/credentials'), () =>
        HttpResponse.json({ detail: 'cap reached' }, { status: 409 })
      )
    );
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole('heading', { name: 'hermes-agent' });
    await user.click(screen.getByRole('button', { name: /issue credential/i }));
    await screen.findByLabelText(/^label$/i);
    await user.click(screen.getByRole('button', { name: /^issue$/i }));

    expect(
      await screen.findByText(/already has two active credentials/i)
    ).toBeInTheDocument();
  });

  it('revokes a credential after confirm, captures the DELETE, and toasts', async () => {
    stubDetail();
    let deleted = false;
    server.use(
      http.delete(
        url('/v1/admin/service-accounts/sa-1/credentials/cred-1'),
        () => {
          deleted = true;
          return new HttpResponse(null, { status: 204 });
        }
      )
    );
    const user = userEvent.setup();
    renderPage();

    const row1 = (await screen.findByText('client-abc123')).closest('tr');
    expect(row1).not.toBeNull();
    await user.click(
      within(row1!).getByRole('button', { name: 'Revoke credential' })
    );

    const confirmButton = await screen.findByRole('button', {
      name: 'Revoke'
    });
    await user.click(confirmButton);

    await waitFor(() => expect(deleted).toBe(true));
    expect(await screen.findByText('Credential revoked')).toBeInTheDocument();
  });

  it('disables revoke on an already-revoked credential', async () => {
    stubDetail();
    renderPage();

    const row2 = (await screen.findByText('client-def456')).closest('tr');
    expect(row2).not.toBeNull();
    expect(
      within(row2!).getByRole('button', { name: 'Revoke credential' })
    ).toBeDisabled();
  });
});
