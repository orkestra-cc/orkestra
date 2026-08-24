import { describe, it, expect, vi } from 'vitest';
import { screen, within, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { renderWithProviders } from 'test/render';
import type { AdminOrgListItem } from 'store/api/tenantApi';
import TenantTable from './TenantTable';

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

const noop = () => {};

const renderTable = (
  orgs: AdminOrgListItem[],
  overrides: Partial<React.ComponentProps<typeof TenantTable>> = {}
) =>
  renderWithProviders(
    <TenantTable
      orgs={orgs}
      isLoading={false}
      error={false}
      includeDeleted={false}
      onIncludeDeletedChange={noop}
      searchTerm=""
      onSearchChange={noop}
      includeDeletedUsers={false}
      onIncludeDeletedUsersChange={noop}
      searchActive={false}
      onRowClick={noop}
      onCreateClick={noop}
      onDeleteClick={noop}
      {...overrides}
    />
  );

const rowFor = (name: string) => {
  const cell = screen.getByText(name);
  const row = cell.closest('tr');
  if (!row) throw new Error(`no <tr> ancestor for "${name}"`);
  return row as HTMLElement;
};

describe('TenantTable — platform default badge', () => {
  it('renders the Default badge only on the row whose isDefault is true', () => {
    renderTable([
      baseOrg({ id: 't-default', name: 'Default Tenant', isDefault: true }),
      baseOrg({ id: 't-other', name: 'Other Tenant', isDefault: false })
    ]);

    const defaultRow = rowFor('Default Tenant');
    const otherRow = rowFor('Other Tenant');

    expect(
      within(defaultRow).getByTestId('tenant-default-badge')
    ).toBeInTheDocument();
    expect(
      within(otherRow).queryByTestId('tenant-default-badge')
    ).not.toBeInTheDocument();
  });

  it('adds no extra column — the empty-state row still spans every column', () => {
    renderTable([]);
    // The empty-state <td> must keep colSpan=7 (7 <th> in the header) even
    // though the badge now lives inside the existing name cell rather than
    // a new one.
    const headerCells = screen.getAllByRole('columnheader');
    expect(headerCells).toHaveLength(7);
    const cell = screen.getByText(/No tenants match the current filters\./);
    expect(cell.getAttribute('colspan')).toBe('7');
  });
});

describe('TenantTable — Set as default eligibility', () => {
  it('offers "Set as default" only for internal + active + non-deleted + non-default rows', () => {
    renderTable([
      baseOrg({ id: 't-eligible', name: 'Eligible Tenant' }),
      baseOrg({ id: 't-external', name: 'External Tenant', kind: 'external' }),
      baseOrg({
        id: 't-suspended',
        name: 'Suspended Tenant',
        status: 'suspended'
      }),
      baseOrg({
        id: 't-deleted',
        name: 'Deleted Tenant',
        deletedAt: '2026-01-02T00:00:00Z'
      }),
      baseOrg({ id: 't-default', name: 'Already Default', isDefault: true })
    ]);

    expect(
      within(rowFor('Eligible Tenant')).getByTitle('Set as default')
    ).toBeInTheDocument();
    expect(
      within(rowFor('External Tenant')).queryByTitle('Set as default')
    ).not.toBeInTheDocument();
    expect(
      within(rowFor('Suspended Tenant')).queryByTitle('Set as default')
    ).not.toBeInTheDocument();
    expect(
      within(rowFor('Deleted Tenant')).queryByTitle('Set as default')
    ).not.toBeInTheDocument();
    expect(
      within(rowFor('Already Default')).queryByTitle('Set as default')
    ).not.toBeInTheDocument();
  });

  it('clicking "Set as default" fires PUT /v1/admin/tenants/default with {tenantId}', async () => {
    let capturedMethod = '';
    let capturedBody: unknown = null;
    server.use(
      http.put('*/v1/admin/tenants/default', async ({ request }) => {
        capturedMethod = request.method;
        capturedBody = await request.json();
        return new HttpResponse(null, { status: 204 });
      })
    );

    const user = userEvent.setup();
    renderTable([baseOrg({ id: 'tenant-42', name: 'Eligible Tenant' })]);

    await user.click(
      within(rowFor('Eligible Tenant')).getByTitle('Set as default')
    );

    await waitFor(() => expect(capturedMethod).toBe('PUT'));
    expect(capturedBody).toEqual({ tenantId: 'tenant-42' });
  });
});

describe('TenantTable — guarded lifecycle on the default row', () => {
  it('hides the archive/trash action for the default tenant and shows an explanation instead', () => {
    renderTable([
      baseOrg({ id: 't-default', name: 'Default Tenant', isDefault: true }),
      baseOrg({ id: 't-other', name: 'Other Tenant' })
    ]);

    const defaultRow = rowFor('Default Tenant');
    const otherRow = rowFor('Other Tenant');

    expect(
      within(defaultRow).queryByTitle('Archive (soft-delete)')
    ).not.toBeInTheDocument();
    expect(
      within(defaultRow).getByTitle(
        'This is the platform default tenant. Reassign the default to another tenant before you can suspend, archive, purge, or delete it.'
      )
    ).toBeInTheDocument();

    // Non-default row keeps the normal trash action.
    expect(
      within(otherRow).getByTitle('Archive (soft-delete)')
    ).toBeInTheDocument();
  });

  it('never fires onDeleteClick for the default row (trash is gone, not just styled)', async () => {
    const onDeleteClick = vi.fn();
    renderTable(
      [baseOrg({ id: 't-default', name: 'Default Tenant', isDefault: true })],
      { onDeleteClick }
    );

    expect(
      within(rowFor('Default Tenant')).queryByRole('button', {
        name: /archive/i
      })
    ).not.toBeInTheDocument();
    expect(onDeleteClick).not.toHaveBeenCalled();
  });
});
