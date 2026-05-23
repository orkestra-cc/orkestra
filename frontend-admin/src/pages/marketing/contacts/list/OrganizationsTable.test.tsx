import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { Organization } from 'types/marketing';
import OrganizationsTable from './OrganizationsTable';

const orgs: Organization[] = [
  {
    uuid: 'o-1',
    tenantId: 't',
    legalName: 'Acme SpA',
    displayName: 'Acme',
    kind: 'company',
    vat: 'IT12345678901',
    emails: [{ address: 'info@acme.example', primary: true }],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-02-01T00:00:00Z'
  },
  {
    uuid: 'o-2',
    tenantId: 't',
    legalName: 'Beta Foundation',
    kind: 'foundation',
    emails: [{ address: 'hello@beta.example', primary: true }],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-02-02T00:00:00Z'
  }
];

const mountWith = (override?: Partial<{ items: Organization[] }>) => {
  server.use(
    http.get(url('/v1/marketing/organizations'), () =>
      HttpResponse.json({
        items: orgs,
        meta: { limit: 100, skip: 0, count: orgs.length },
        ...override
      })
    )
  );
  return renderWithProviders(<OrganizationsTable />);
};

describe('OrganizationsTable', () => {
  it('renders every organization with kind badge and VAT', async () => {
    mountWith();

    expect(await screen.findByText('Acme SpA')).toBeInTheDocument();
    expect(screen.getByText('Beta Foundation')).toBeInTheDocument();
    expect(screen.getByText('IT12345678901')).toBeInTheDocument();
    expect(screen.getByText('company')).toBeInTheDocument();
    expect(screen.getByText('foundation')).toBeInTheDocument();
  });

  it('global search narrows the rendered rows', async () => {
    mountWith();
    const user = userEvent.setup();

    await screen.findByText('Acme SpA');

    const search = screen.getByPlaceholderText(/search contacts/i);
    await user.type(search, 'beta');

    await waitFor(() => {
      expect(screen.queryByText('Acme SpA')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Beta Foundation')).toBeInTheDocument();
  });

  it('renders the empty state when no orgs are returned', async () => {
    mountWith({ items: [] });

    expect(
      await screen.findByText(/no organizations yet/i)
    ).toBeInTheDocument();
    expect(
      screen.queryByPlaceholderText(/search contacts/i)
    ).not.toBeInTheDocument();
  });
});
