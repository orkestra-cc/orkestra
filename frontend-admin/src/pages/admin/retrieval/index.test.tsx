import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import RetrievalAdminPage from './index';

// Permission gating is the consumer branching under test, so useAuth is
// mocked (precedent: pages/admin/llm/index.test.tsx).
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({ useAuth: () => mockedUseAuth() }));
const authWith = (permissions: string[]) => ({
  hasPermission: (p: string) => permissions.includes(p)
});

const reads: string[] = [];

beforeEach(() => {
  reads.length = 0;
  server.use(
    http.get(url('/v1/admin/retrieval/corpora'), () => {
      reads.push('corpora');
      return HttpResponse.json({
        engine: { status: 'ok' },
        inference: [],
        corpora: [],
        erasures: { pending: 0, fencing: 0 }
      });
    }),
    http.get(url('/v1/admin/retrieval/tenant/indexing'), () => {
      reads.push('indexing');
      return HttpResponse.json({
        tenantId: 't-1',
        engine: { status: 'ok' },
        corpora: [],
        erasuresPending: 0
      });
    }),
    http.get(url('/v1/admin/retrieval/search-log'), () => {
      reads.push('search-log');
      return HttpResponse.json({ items: [], total: 0, limit: 50, offset: 0 });
    })
  );
});

const renderPage = (entry: string) =>
  renderWithProviders(<RetrievalAdminPage />, { routerEntries: [entry] });

const tabNames = () =>
  screen.getAllByRole('tab').map(tab => tab.textContent?.trim());

describe('RetrievalAdminPage', () => {
  it('shows only the tabs the caller may open', async () => {
    mockedUseAuth.mockReturnValue(authWith(['retrieval.tenant.read']));
    const { unmount } = renderPage('/admin/retrieval');
    expect(tabNames()).toEqual(['Indexing']);
    expect(await screen.findByText(/no corpus is registered/i)).toBeVisible();
    unmount();

    mockedUseAuth.mockReturnValue(
      authWith(['retrieval.corpus.read', 'retrieval.searchlog.admin'])
    );
    renderPage('/admin/retrieval');
    expect(tabNames()).toEqual(['Corpora', 'Search log']);
  });

  it('falls back to the first visible tab for a forbidden ?tab=', async () => {
    mockedUseAuth.mockReturnValue(authWith(['retrieval.corpus.read']));
    renderPage('/admin/retrieval?tab=search-log');
    expect(screen.getByRole('tab', { name: 'Corpora' })).toHaveAttribute(
      'aria-selected',
      'true'
    );
    expect(
      await screen.findByText(/no corpus is registered or stored/i)
    ).toBeVisible();
    expect(reads).toEqual(['corpora']);
  });

  it('opens a permitted tab named in the URL', async () => {
    mockedUseAuth.mockReturnValue(
      authWith(['retrieval.corpus.read', 'retrieval.searchlog.admin'])
    );
    renderPage('/admin/retrieval?tab=search-log');
    expect(screen.getByRole('tab', { name: 'Search log' })).toHaveAttribute(
      'aria-selected',
      'true'
    );
    expect(
      await screen.findByText(/no search matches these filters/i)
    ).toBeVisible();
    expect(reads).toEqual(['search-log']);
  });

  it('shows the no-access alert without any retrieval permission', () => {
    mockedUseAuth.mockReturnValue(authWith(['llm.admin.read']));
    renderPage('/admin/retrieval');
    expect(
      screen.getByText(/you do not have access to any retrieval view/i)
    ).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(reads).toEqual([]);
  });
});
