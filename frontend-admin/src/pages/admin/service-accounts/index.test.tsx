import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ToastContainer } from 'react-toastify';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { formatDate } from 'helpers/dateFormat';
import type {
  ServiceAccount,
  ServiceAccountWithSecret
} from 'types/serviceAccounts';
import ServiceAccountsPage from './index';

// ServiceAccountsPage mounts the list query on render and the create modal
// (unmounted content until opened) on top of it — every test stubs the list
// read, and the create tests additionally stub the POST.

const accounts: ServiceAccount[] = [
  {
    id: 'sa-1',
    name: 'hermes-agent',
    email: 'sa-1@service.local',
    isActive: true,
    activeCredentials: 2,
    createdAt: '2026-08-01T10:00:00Z'
  },
  {
    id: 'sa-2',
    name: 'stale-bot',
    email: 'sa-2@service.local',
    isActive: false,
    activeCredentials: 0,
    createdAt: '2026-07-01T10:00:00Z'
  }
];

const stubList = (body: ServiceAccount[] = accounts) => {
  server.use(
    http.get(url('/v1/admin/service-accounts'), () => HttpResponse.json(body))
  );
};

// renderWithProviders doesn't mount a ToastContainer (App.tsx normally owns
// that) — react-toastify's toast() targets whatever container is anywhere in
// the tree, not necessarily a parent of the caller, so tests that assert
// toast text render one alongside the page, mirroring the real app shell.
const renderPage = () =>
  renderWithProviders(
    <>
      <ServiceAccountsPage />
      <ToastContainer />
    </>
  );

describe('ServiceAccountsPage', () => {
  it('renders the rows returned by the list endpoint', async () => {
    stubList();
    renderPage();

    const nameLink = await screen.findByRole('link', {
      name: 'hermes-agent'
    });
    expect(nameLink).toHaveAttribute('href', '/admin/service-accounts/sa-1');

    const row = nameLink.closest('tr');
    expect(row).not.toBeNull();
    expect(row).toHaveTextContent('sa-1@service.local');
    expect(row).toHaveTextContent('Active');
    expect(row).toHaveTextContent('2');
    expect(row).toHaveTextContent(formatDate(accounts[0].createdAt));

    const secondRow = (
      await screen.findByRole('link', { name: 'stale-bot' })
    ).closest('tr');
    expect(secondRow).toHaveTextContent('Inactive');
  });

  it('renders an empty state when the list is empty', async () => {
    stubList([]);
    renderPage();

    expect(await screen.findByText(/no service accounts/i)).toBeInTheDocument();
  });

  it('creates a service account, shows the secret once, and gates Done on ack', async () => {
    stubList();
    let requestBody: unknown = null;
    const secretResponse: ServiceAccountWithSecret = {
      id: 'sa-3',
      name: 'new-bot',
      email: 'sa-3@service.local',
      isActive: true,
      activeCredentials: 1,
      createdAt: '2026-08-18T00:00:00Z',
      clientId: 'client-xyz',
      clientSecret: 'super-secret-once'
    };
    server.use(
      http.post(url('/v1/admin/service-accounts'), async ({ request }) => {
        requestBody = await request.json();
        return HttpResponse.json(secretResponse);
      })
    );
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole('link', { name: 'hermes-agent' });
    await user.click(
      screen.getByRole('button', { name: /new service account/i })
    );

    const nameInput = await screen.findByLabelText(/^name$/i);
    await user.type(nameInput, 'new-bot');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => expect(requestBody).toEqual({ name: 'new-bot' }));

    expect(await screen.findByText('super-secret-once')).toBeInTheDocument();
    expect(screen.getByText('client-xyz')).toBeInTheDocument();

    const doneButton = screen.getByRole('button', { name: /done/i });
    expect(doneButton).toBeDisabled();

    await user.click(screen.getByRole('checkbox'));
    expect(doneButton).toBeEnabled();
  });

  it('surfaces a toast when the backend rejects a duplicate name', async () => {
    stubList();
    server.use(
      http.post(url('/v1/admin/service-accounts'), () =>
        HttpResponse.json({ detail: 'duplicate' }, { status: 409 })
      )
    );
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole('link', { name: 'hermes-agent' });
    await user.click(
      screen.getByRole('button', { name: /new service account/i })
    );

    const nameInput = await screen.findByLabelText(/^name$/i);
    await user.type(nameInput, 'hermes-agent');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    expect(await screen.findByText(/already exists/i)).toBeInTheDocument();
  });
});
