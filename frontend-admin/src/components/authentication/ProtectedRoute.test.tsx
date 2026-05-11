import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import ProtectedRoute from './ProtectedRoute';

// useAuth aggregates several RTK Query calls + tenant slice. ProtectedRoute
// only consumes the resolved shape, so mock the hook directly — we're
// testing the gate's branching, not the auth pipeline.
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => mockedUseAuth(),
}));

interface AuthState {
  isAuthenticated?: boolean;
  isLoading?: boolean;
  permissions?: string[];
  hasPermission?: (p: string) => boolean;
  hasAnyPermission?: (p: string[]) => boolean;
}

const setAuth = (state: AuthState) => {
  const permissions = state.permissions ?? [];
  mockedUseAuth.mockReturnValue({
    isAuthenticated: state.isAuthenticated ?? false,
    isLoading: state.isLoading ?? false,
    permissions,
    hasPermission:
      state.hasPermission ?? ((p: string) => permissions.includes('*') || permissions.includes(p)),
    hasAnyPermission:
      state.hasAnyPermission ??
      ((req: string[]) =>
        permissions.includes('*') || req.some((p) => permissions.includes(p))),
  });
};

const renderProtected = (
  ui: React.ReactNode,
  { initialEntries = ['/secret'] }: { initialEntries?: string[] } = {},
) =>
  render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route path="/secret" element={ui} />
        <Route path="/login" element={<div>LOGIN_PAGE</div>} />
        <Route path="/errors/401" element={<div>UNAUTHORIZED_PAGE</div>} />
      </Routes>
    </MemoryRouter>,
  );

describe('ProtectedRoute', () => {
  beforeEach(() => mockedUseAuth.mockReset());

  it('renders a loader while auth state is resolving', () => {
    setAuth({ isLoading: true });
    renderProtected(<ProtectedRoute>SECRET</ProtectedRoute>);
    expect(screen.queryByText('SECRET')).toBeNull();
    expect(screen.queryByText('LOGIN_PAGE')).toBeNull();
  });

  it('redirects unauthenticated users to /login (default fallback)', () => {
    setAuth({ isAuthenticated: false });
    renderProtected(<ProtectedRoute>SECRET</ProtectedRoute>);
    expect(screen.getByText('LOGIN_PAGE')).toBeInTheDocument();
  });

  it('honors a custom fallbackUrl', () => {
    setAuth({ isAuthenticated: false });
    render(
      <MemoryRouter initialEntries={['/secret']}>
        <Routes>
          <Route
            path="/secret"
            element={
              <ProtectedRoute fallbackUrl="/login?reason=custom">SECRET</ProtectedRoute>
            }
          />
          <Route path="/login" element={<div>LOGIN_PAGE</div>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText('LOGIN_PAGE')).toBeInTheDocument();
  });

  it('renders children when authenticated and no permissions are required', () => {
    setAuth({ isAuthenticated: true, permissions: [] });
    renderProtected(<ProtectedRoute>SECRET</ProtectedRoute>);
    expect(screen.getByText('SECRET')).toBeInTheDocument();
  });

  it('renders children when every required permission is held', () => {
    setAuth({ isAuthenticated: true, permissions: ['billing.read', 'billing.write'] });
    renderProtected(
      <ProtectedRoute requiredPermissions={['billing.read', 'billing.write']}>
        SECRET
      </ProtectedRoute>,
    );
    expect(screen.getByText('SECRET')).toBeInTheDocument();
  });

  it('redirects to /errors/401 when a required permission is missing', () => {
    // Bug class: a deny-by-default route silently allows access because the
    // gate evaluated permissions before they finished loading, or because
    // it was checking "any" semantics where it should check "all".
    setAuth({ isAuthenticated: true, permissions: ['billing.read'] });
    renderProtected(
      <ProtectedRoute requiredPermissions={['billing.read', 'billing.write']}>
        SECRET
      </ProtectedRoute>,
    );
    expect(screen.queryByText('SECRET')).toBeNull();
    expect(screen.getByText('UNAUTHORIZED_PAGE')).toBeInTheDocument();
  });

  it('grants access via the * wildcard permission', () => {
    setAuth({ isAuthenticated: true, permissions: ['*'] });
    renderProtected(
      <ProtectedRoute requiredPermissions={['billing.write', 'audit.admin']}>
        SECRET
      </ProtectedRoute>,
    );
    expect(screen.getByText('SECRET')).toBeInTheDocument();
  });

  // Nested array means "any of these" rather than "all". ProtectedRoute
  // detects it via Array.isArray(requiredPermissions[0]).
  it('treats a nested-array requirement as any-of', () => {
    setAuth({ isAuthenticated: true, permissions: ['billing.read'] });
    renderProtected(
      <ProtectedRoute
        requiredPermissions={[['billing.read', 'billing.write']] as unknown as string[]}
      >
        SECRET
      </ProtectedRoute>,
    );
    expect(screen.getByText('SECRET')).toBeInTheDocument();
  });

  // Race condition: useAuth returns isAuthenticated=true the moment the
  // session call resolves, but tenant permissions can land a tick later.
  // ProtectedRoute must show a loader instead of rendering "denied" prematurely.
  it('shows a loader when authenticated but permissions array has not arrived yet', () => {
    setAuth({ isAuthenticated: true, isLoading: false, permissions: [] });
    renderProtected(
      <ProtectedRoute requiredPermissions={['billing.read']}>SECRET</ProtectedRoute>,
    );
    expect(screen.queryByText('SECRET')).toBeNull();
    expect(screen.queryByText('UNAUTHORIZED_PAGE')).toBeNull();
  });

  it('passes children straight through when requireAuth=false', () => {
    setAuth({ isAuthenticated: false });
    renderProtected(
      <ProtectedRoute requireAuth={false}>SECRET</ProtectedRoute>,
    );
    expect(screen.getByText('SECRET')).toBeInTheDocument();
  });
});
