import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { renderWithProviders } from 'test/render';
import type { AdminOrgListItem } from 'store/api/tenantApi';
import TenantDetailModal from './TenantDetailModal';

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

// react-bootstrap's Tabs mounts every pane on first render (no
// mountOnEnter), so Members and Invites fire their queries immediately
// regardless of which tab is active.
const stubTabQueries = () =>
  server.use(
    http.get('*/v1/admin/tenants/*/members', () =>
      HttpResponse.json({ members: [] })
    ),
    http.get('*/v1/admin/tenants/*/invites', () =>
      HttpResponse.json({ invites: [] })
    )
  );

describe('TenantDetailModal — default tenant', () => {
  it('shows the Default badge in the title and hides Delete/Purge behind an explanation', () => {
    stubTabQueries();
    renderWithProviders(
      <TenantDetailModal
        org={baseOrg({ isDefault: true })}
        show
        onHide={() => {}}
        onDelete={() => {}}
        onPurge={() => {}}
      />
    );

    expect(screen.getByTestId('tenant-default-badge')).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /delete tenant/i })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /purge/i })
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(
        'This is the platform default tenant. Reassign the default to another tenant before you can suspend, archive, purge, or delete it.'
      )
    ).toBeInTheDocument();
  });

  it('a non-default tenant keeps the Delete/Purge buttons and no badge', () => {
    stubTabQueries();
    renderWithProviders(
      <TenantDetailModal
        org={baseOrg({ isDefault: false })}
        show
        onHide={() => {}}
        onDelete={() => {}}
        onPurge={() => {}}
      />
    );

    expect(
      screen.queryByTestId('tenant-default-badge')
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /delete tenant/i })
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /purge/i })).toBeInTheDocument();
  });
});
