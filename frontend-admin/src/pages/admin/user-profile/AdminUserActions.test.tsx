import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import AdminUserActions from './AdminUserActions';
import type { User } from 'store/api/userApi';

let perms: string[] = [];
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => ({ hasPermission: (p: string) => perms.includes(p) })
}));
beforeEach(() => {
  perms = [];
});

const user: User = {
  id: 'u-1',
  email: 'target@example.com',
  username: 'target',
  fullName: 'Target User',
  role: 'operator',
  providers: [],
  isActive: true,
  emailVerified: true,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

describe('AdminUserActions data subject link', () => {
  it('links to the subject tab when the session can read compliance audit', () => {
    perms = ['system.compliance.audit.read'];
    renderWithProviders(<AdminUserActions user={user} />);
    const link = screen.getByRole('link', { name: /data subject requests/i });
    expect(link).toHaveAttribute(
      'href',
      '/admin/compliance?tab=subject&userUuid=u-1'
    );
  });

  it('hides the link without the permission', () => {
    renderWithProviders(<AdminUserActions user={user} />);
    expect(
      screen.queryByRole('link', { name: /data subject requests/i })
    ).not.toBeInTheDocument();
  });
});
