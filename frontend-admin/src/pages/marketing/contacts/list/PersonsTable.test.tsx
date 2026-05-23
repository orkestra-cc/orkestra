import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { Person, Tag } from 'types/marketing';
import PersonsTable from './PersonsTable';

const tag: Tag = {
  uuid: 'tag-vip',
  tenantId: 't',
  name: 'VIP',
  slug: 'vip',
  path: '/vip',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z'
};

const persons: Person[] = [
  {
    uuid: 'p-1',
    tenantId: 't',
    firstName: 'Alice',
    lastName: 'Anderson',
    emails: [{ address: 'alice@example.com', primary: true }],
    tags: ['tag-vip'],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-02-01T00:00:00Z'
  },
  {
    uuid: 'p-2',
    tenantId: 't',
    firstName: 'Bob',
    lastName: 'Brown',
    emails: [{ address: 'bob@example.com', primary: true }],
    tags: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-02-02T00:00:00Z'
  }
];

const mountWith = (override?: Partial<{ items: Person[] }>) => {
  server.use(
    http.get(url('/v1/marketing/persons'), () =>
      HttpResponse.json({
        items: persons,
        meta: { limit: 100, skip: 0, count: persons.length },
        ...override
      })
    ),
    http.get(url('/v1/marketing/tags'), () =>
      HttpResponse.json({ items: [tag] })
    )
  );
  return renderWithProviders(<PersonsTable />);
};

describe('PersonsTable', () => {
  it('renders all persons with email and tag chips', async () => {
    mountWith();

    expect(
      await screen.findByRole('link', { name: 'Alice Anderson' })
    ).toBeInTheDocument();
    expect(screen.getByText('Bob Brown')).toBeInTheDocument();
    expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    // Tag chip rendered from resolved tag name (not raw UUID).
    expect(screen.getByText('VIP')).toBeInTheDocument();
  });

  it('global search narrows the rendered rows', async () => {
    mountWith();
    const user = userEvent.setup();

    await screen.findByText('Alice Anderson');

    const search = screen.getByPlaceholderText(/search contacts/i);
    await user.type(search, 'bob');

    await waitFor(() => {
      expect(screen.queryByText('Alice Anderson')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Bob Brown')).toBeInTheDocument();
  });

  it('search matches by tag name (not by UUID)', async () => {
    mountWith();
    const user = userEvent.setup();

    await screen.findByText('Alice Anderson');

    const search = screen.getByPlaceholderText(/search contacts/i);
    await user.type(search, 'vip');

    await waitFor(() => {
      expect(screen.queryByText('Bob Brown')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Alice Anderson')).toBeInTheDocument();
  });

  it('renders the empty state when no persons are returned', async () => {
    mountWith({ items: [] });

    expect(await screen.findByText(/no persons yet/i)).toBeInTheDocument();
    // Footer + search shouldn't render in the empty state.
    expect(
      screen.queryByPlaceholderText(/search contacts/i)
    ).not.toBeInTheDocument();
  });
});
