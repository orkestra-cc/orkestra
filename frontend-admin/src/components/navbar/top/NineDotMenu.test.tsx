import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useLocation } from 'react-router';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { renderWithProviders } from 'test/render';
import NineDotMenu from './NineDotMenu';

// NineDotMenu consumes useAuth only for the hasPermission gate — mock the
// hook to test the component's own branching (same pattern as
// ProtectedRoute's tests, sanctioned by src/test conventions).
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => ({ hasPermission: () => true })
}));

const memberships = [
  {
    tenantId: 'org-a',
    name: 'Default',
    slug: 'default',
    plan: 'free',
    kind: 'internal',
    roles: ['org_owner'],
    isOwner: true
  },
  {
    tenantId: 'org-b',
    name: 'Second',
    slug: 'second',
    plan: 'free',
    kind: 'internal',
    roles: ['org_owner'],
    isOwner: false
  }
];

const tenantState = (impersonating: boolean) => ({
  memberships,
  currentOrgId: 'org-a',
  permissions: ['*'],
  features: [],
  systemRole: '',
  loading: false,
  error: null,
  impersonatedTenantId: impersonating ? 'org-x' : null,
  impersonatedTenantName: impersonating ? 'X Corp' : null
});

describe('NineDotMenu', () => {
  it('opens and closes without changing the current path, query or hash', async () => {
    server.use(
      http.get('*/v1/admin/tenants', () => HttpResponse.json({ tenants: [] }))
    );
    const Location = () => {
      const { pathname, search, hash } = useLocation();
      return <output data-testid="location">{pathname + search + hash}</output>;
    };
    const initialUrl = '/workspace?run=example&tab=history#section';
    const user = userEvent.setup();
    renderWithProviders(
      <>
        <NineDotMenu />
        <Location />
      </>,
      {
        preloadedState: { tenant: tenantState(false) },
        routerEntries: [initialUrl]
      }
    );
    const toggle = screen.getByTitle('Workspaces');
    await user.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByTestId('location')).toHaveTextContent(initialUrl);
    await user.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByTestId('location')).toHaveTextContent(initialUrl);
  });

  it('fills the dots with an existing theme token while impersonating', () => {
    const { container } = renderWithProviders(<NineDotMenu />, {
      preloadedState: { tenant: tenantState(true) }
    });

    const circle = container.querySelector('svg circle');
    expect(circle).not.toBeNull();
    // The theme compiles Bootstrap with prefix "orkestra-", so --bs-warning
    // does not exist at runtime — an SVG fill referencing it computes to
    // "none" and the icon becomes invisible. The fill must reference the
    // real token and carry a literal fallback (#f5803e per DESIGN.md).
    expect(circle!.getAttribute('fill')).toBe(
      'var(--orkestra-warning, #f5803e)'
    );
  });

  it('keeps the neutral fill with a literal fallback when not impersonating', () => {
    const { container } = renderWithProviders(<NineDotMenu />, {
      preloadedState: { tenant: tenantState(false) }
    });

    const circle = container.querySelector('svg circle');
    expect(circle).not.toBeNull();
    const fill = circle!.getAttribute('fill');
    // Whatever variable it references, a literal fallback must be present —
    // a bare var() to a token the theme doesn't emit renders fill:none.
    expect(fill).toMatch(/^var\(--[\w-]+, #[0-9a-fA-F]{6}\)$/);
  });
});
