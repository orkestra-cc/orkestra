import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { AdminOrgListItem } from 'store/api/tenantApi';
import PurgeTenantModal from './PurgeTenantModal';

const baseOrg = (
  overrides: Partial<AdminOrgListItem> = {}
): AdminOrgListItem => ({
  id: 'tenant-1',
  name: 'Acme',
  slug: 'acme',
  ownerUserUUID: 'u-1',
  plan: 'free',
  features: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  status: 'active',
  kind: 'internal',
  memberCount: 3,
  ...overrides
});

describe('PurgeTenantModal — guarded lifecycle for the default tenant', () => {
  it('renders the reassignment explanation and a disabled confirm instead of the purge warning', () => {
    renderWithProviders(
      <PurgeTenantModal
        org={baseOrg({ isDefault: true })}
        show
        onHide={() => {}}
      />
    );

    expect(
      screen.getByText(
        'This is the platform default tenant. Reassign the default to another tenant before you can suspend, archive, purge, or delete it.'
      )
    ).toBeInTheDocument();

    // The normal irreversible-warning copy must not render at all for the
    // default tenant.
    expect(
      screen.queryByText(/cryptographically unrecoverable/i)
    ).not.toBeInTheDocument();

    const confirmButton = screen.getByRole('button', {
      name: /i understand/i
    });
    expect(confirmButton).toBeDisabled();
  });

  it('a non-default tenant keeps the normal double-confirm purge flow', () => {
    renderWithProviders(
      <PurgeTenantModal
        org={baseOrg({ isDefault: false })}
        show
        onHide={() => {}}
      />
    );

    expect(
      screen.queryByText(/platform default tenant/i)
    ).not.toBeInTheDocument();

    const confirmButton = screen.getByRole('button', {
      name: /i understand/i
    });
    expect(confirmButton).toBeEnabled();
  });
});
