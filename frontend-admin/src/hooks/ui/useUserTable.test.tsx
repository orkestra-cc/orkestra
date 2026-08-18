import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import UserTable from 'pages/admin/users/UserTable';
import type { User, UserListResponse } from 'store/api/userApi';

// Two-row fixture: an admin (self) and an unverified operator (the
// resend-verification target). Every test starts from this baseline and
// overrides as needed.
const adminSelf: User = {
  id: 'admin-1',
  email: 'admin@example.com',
  username: 'admin',
  fullName: 'Admin Self',
  role: 'administrator',
  providers: [],
  isActive: true,
  emailVerified: true,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

const operatorUnverified: User = {
  id: 'op-2',
  email: 'op@example.com',
  username: 'op',
  fullName: 'Op Two',
  role: 'operator',
  providers: [],
  isActive: true,
  emailVerified: false,
  createdAt: '2026-01-02T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z'
};

const listResponse = (
  users: User[] = [adminSelf, operatorUnverified]
): UserListResponse => ({
  users,
  total: users.length,
  page: 1,
  pageSize: 10,
  totalPages: 1
});

const listHandler = (body: UserListResponse = listResponse()) =>
  http.get(url('/v1/users'), () => HttpResponse.json(body));

// Auth slice preload — pretend the current user is the administrator
// above so the self-row guard fires on that row. Matches the real
// AuthState shape in authSlice.ts so the cast stays type-safe.
const preloadedAuthState = {
  auth: {
    user: {
      id: adminSelf.id,
      email: adminSelf.email,
      username: adminSelf.username,
      fullName: adminSelf.fullName,
      role: adminSelf.role,
      providers: [],
      isActive: true,
      emailVerified: true,
      createdAt: adminSelf.createdAt,
      updatedAt: adminSelf.updatedAt
    },
    isAuthenticated: true,
    isLoading: false,
    error: null,
    sessionExpiry: null,
    permissions: [] as string[],
    preferences: {
      theme: 'light' as const,
      language: 'en',
      notifications: true
    },
    _isLoggingOut: false,
    accessToken: 'test-token',
    tokenExpiry: null
  }
};

// Open the actions dropdown on a specific row by its visible name.
async function openActionsForRow(
  user: ReturnType<typeof userEvent.setup>,
  fullName: string
) {
  const row = screen.getByText(fullName).closest('tr');
  if (!row) throw new Error(`row for ${fullName} not found`);
  // The toggle is the only button inside the row's actions cell.
  const toggle = within(row as HTMLElement)
    .getAllByRole('button')
    .find(btn => btn.classList.contains('btn-reveal'));
  if (!toggle) throw new Error(`actions toggle for ${fullName} not found`);
  await user.click(toggle);
}

describe('useUserTable rendering', () => {
  beforeEach(() => {
    server.use(listHandler());
  });

  it('renders Service badge for rows with kind === "service"', async () => {
    const serviceUser: User = {
      id: 'service-1',
      email: 'bot@example.com',
      username: 'bot',
      fullName: 'Bot User',
      kind: 'service',
      role: 'developer',
      providers: [],
      isActive: true,
      emailVerified: true,
      createdAt: '2026-01-03T00:00:00Z',
      updatedAt: '2026-01-03T00:00:00Z'
    };

    server.use(listHandler(listResponse([serviceUser])));

    renderWithProviders(<UserTable />, {
      preloadedState: preloadedAuthState
    });

    await waitFor(() =>
      expect(screen.getByText('Bot User')).toBeInTheDocument()
    );
    // Should find the Service badge near the user name
    expect(screen.getByText('Service')).toBeInTheDocument();
  });

  it('does not render Service badge for human users (no kind)', async () => {
    renderWithProviders(<UserTable />, {
      preloadedState: preloadedAuthState
    });

    await waitFor(() =>
      expect(screen.getByText('Admin Self')).toBeInTheDocument()
    );

    // Admin user has no kind, so no Service badge should appear
    expect(screen.queryByText('Service')).not.toBeInTheDocument();
  });
});

describe('useUserTable row actions', () => {
  beforeEach(() => {
    server.use(listHandler());
  });

  it('calls resend-verification only for unverified users', async () => {
    const resendCalled = vi.fn();
    server.use(
      http.post(url('/v1/admin/users/op-2/resend-verification'), () => {
        resendCalled();
        return HttpResponse.json({ success: true, message: 'sent' });
      })
    );

    renderWithProviders(<UserTable />, { preloadedState: preloadedAuthState });
    await waitFor(() => expect(screen.getByText('Op Two')).toBeInTheDocument());

    const user = userEvent.setup();
    await openActionsForRow(user, 'Op Two');

    const resendItem = await screen.findByText('Resend Verification');
    await user.click(resendItem);

    await waitFor(() => expect(resendCalled).toHaveBeenCalledTimes(1));
  });

  it('hides resend-verification for already-verified users', async () => {
    renderWithProviders(<UserTable />, { preloadedState: preloadedAuthState });
    await waitFor(() =>
      expect(screen.getByText('Admin Self')).toBeInTheDocument()
    );

    const user = userEvent.setup();
    await openActionsForRow(user, 'Admin Self');

    // Verified user must not have the resend item in their dropdown.
    expect(screen.queryByText('Resend Verification')).not.toBeInTheDocument();
  });

  it('calls send-password-reset on the right endpoint', async () => {
    const resetCalled = vi.fn();
    server.use(
      http.post(url('/v1/admin/users/op-2/send-password-reset'), () => {
        resetCalled();
        return HttpResponse.json({ success: true, message: 'sent' });
      })
    );

    renderWithProviders(<UserTable />, { preloadedState: preloadedAuthState });
    await waitFor(() => expect(screen.getByText('Op Two')).toBeInTheDocument());

    const user = userEvent.setup();
    await openActionsForRow(user, 'Op Two');

    await user.click(await screen.findByText('Send Password Reset'));
    await waitFor(() => expect(resetCalled).toHaveBeenCalledTimes(1));
  });

  it('disables Delete + Deactivate on the current user row', async () => {
    renderWithProviders(<UserTable />, { preloadedState: preloadedAuthState });
    await waitFor(() =>
      expect(screen.getByText('Admin Self')).toBeInTheDocument()
    );

    const user = userEvent.setup();
    await openActionsForRow(user, 'Admin Self');

    // Both items render but as disabled — the bootstrap classname
    // `disabled` lives on the anchor itself, so we check that the role-
    // queried button has aria-disabled. React-Bootstrap's Dropdown.Item
    // sets aria-disabled when `disabled` is set on a button-like item.
    const deactivate = await screen.findByText('Deactivate');
    const del = await screen.findByText('Delete User');
    expect(deactivate.closest('.dropdown-item')).toHaveClass('disabled');
    expect(del.closest('.dropdown-item')).toHaveClass('disabled');
  });

  it('Delete on a non-self row opens the confirm modal', async () => {
    renderWithProviders(<UserTable />, { preloadedState: preloadedAuthState });
    await waitFor(() => expect(screen.getByText('Op Two')).toBeInTheDocument());

    const user = userEvent.setup();
    await openActionsForRow(user, 'Op Two');

    const del = await screen.findByText('Delete User');
    await user.click(del);

    // The modal mounts an input with the confirm-email placeholder. Find
    // it by placeholder so we don't ambiguously match the dropdown item
    // text we just clicked.
    const confirmInput = await screen.findByPlaceholderText(
      /type the email to confirm/i
    );
    expect(confirmInput).toBeInTheDocument();
    // Submit is disabled until the user types the email. Inside the modal
    // the dialog role scopes the lookup so we don't match the dropdown
    // item.
    const dialog = confirmInput.closest('.modal') as HTMLElement;
    const submit = within(dialog).getByRole('button', {
      name: /^delete user$/i
    });
    expect(submit).toBeDisabled();
  });
});
