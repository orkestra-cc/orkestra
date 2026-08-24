import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { AdminOrgListItem } from 'store/api/tenantApi';
import DeleteTenantModal from './DeleteTenantModal';

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

describe('DeleteTenantModal — guarded lifecycle for the default tenant', () => {
  it('renders the reassignment explanation and a disabled confirm instead of the delete flow', () => {
    renderWithProviders(
      <DeleteTenantModal
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

    // The normal type-to-confirm flow must not render at all — there's
    // nothing to confirm your way past.
    expect(screen.queryByPlaceholderText('acme')).not.toBeInTheDocument();

    const confirmButton = screen.getByRole('button', {
      name: /delete tenant/i
    });
    expect(confirmButton).toBeDisabled();
  });

  it('typing the slug still cannot enable the confirm button for the default tenant', async () => {
    const { container } = renderWithProviders(
      <DeleteTenantModal
        org={baseOrg({ isDefault: true })}
        show
        onHide={() => {}}
      />
    );
    // No confirm input is rendered at all for the default tenant, so there
    // is nothing to type into — the guard removes the path entirely
    // rather than merely disabling a reachable field.
    expect(container.querySelector('input[type="text"]')).toBeNull();
  });

  it('a non-default tenant keeps the normal confirm flow', () => {
    renderWithProviders(
      <DeleteTenantModal
        org={baseOrg({ isDefault: false })}
        show
        onHide={() => {}}
      />
    );

    expect(
      screen.queryByText(/platform default tenant/i)
    ).not.toBeInTheDocument();

    const confirmButton = screen.getByRole('button', {
      name: /delete tenant/i
    });
    // Disabled until the slug is typed — but reachable, unlike the default
    // case above.
    expect(confirmButton).toBeDisabled();
    expect(screen.getByPlaceholderText('acme')).toBeInTheDocument();
  });
});
