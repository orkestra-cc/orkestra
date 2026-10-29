import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import IndexingTab from './IndexingTab';

const corpus = (over: Record<string, unknown>) => ({
  corpus: 'crm',
  owner: 'crm-module',
  ownerRunning: true,
  state: 'active',
  initialBuild: 'done',
  points: 1234,
  outbox: { pending: 0 },
  ...over
});

const report = (over: Record<string, unknown> = {}) => ({
  tenantId: 't-1',
  engine: { status: 'ok' },
  corpora: [
    corpus({}),
    corpus({ corpus: 'docs', initialBuild: 'building', points: 7 }),
    corpus({
      corpus: 'notes',
      state: 'uninitialized',
      initialBuild: 'pending',
      points: undefined
    })
  ],
  erasuresPending: 0,
  ...over
});

const stub = (body: unknown) =>
  server.use(
    http.get(url('/v1/admin/retrieval/tenant/indexing'), () =>
      HttpResponse.json(body)
    )
  );

const rowOf = async (name: string) => {
  const row = (await screen.findByText(name)).closest('tr');
  if (!row) throw new Error(`no row for ${name}`);
  return row;
};

describe('IndexingTab', () => {
  it('shows each corpus with its initial-build badge', async () => {
    stub(report());
    renderWithProviders(<IndexingTab />);
    expect(within(await rowOf('crm')).getByText('Done')).toBeInTheDocument();
    expect(
      within(await rowOf('docs')).getByText('Building')
    ).toBeInTheDocument();
    expect(
      within(await rowOf('notes')).getByText('Pending')
    ).toBeInTheDocument();
  });

  it('shows a broken corpus as an error, not just a pending build', async () => {
    stub(
      report({
        corpora: [
          corpus({
            corpus: 'broken',
            state: 'error',
            initialBuild: 'pending',
            points: undefined
          })
        ]
      })
    );
    renderWithProviders(<IndexingTab />);
    const row = await rowOf('broken');
    expect(within(row).getByText('Error')).toBeInTheDocument();
    expect(within(row).getByText('Pending')).toBeInTheDocument();
  });

  it('shows points only when the engine reported them', async () => {
    stub(
      report({
        engine: { status: 'unavailable' },
        corpora: [corpus({ points: undefined })]
      })
    );
    renderWithProviders(<IndexingTab />);
    const row = await rowOf('crm');
    expect(within(row).getByText('—')).toBeInTheDocument();
    expect(within(row).queryByText(/1,234/)).not.toBeInTheDocument();
    expect(screen.getByText('Unavailable')).toBeInTheDocument();
  });

  it('shows the indexed points when the engine is reachable', async () => {
    stub(report());
    renderWithProviders(<IndexingTab />);
    expect(within(await rowOf('crm')).getByText('1,234')).toBeInTheDocument();
    expect(within(await rowOf('notes')).getByText('—')).toBeInTheDocument();
  });

  it('offers a retry when the report cannot be loaded', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/admin/retrieval/tenant/indexing'), () => {
        calls++;
        return calls === 1
          ? HttpResponse.json({ code: 'retrieval.internal' }, { status: 500 })
          : HttpResponse.json(report());
      })
    );
    renderWithProviders(<IndexingTab />);
    expect(
      await screen.findByText('The retrieval data could not be loaded.')
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await rowOf('crm')).toBeInTheDocument();
    expect(calls).toBe(2);
  });
});
