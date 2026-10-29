import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'react-toastify';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { completeStepUp, subscribeStepUp } from 'store/stepUp';
import CorporaTab from './CorporaTab';

vi.mock('react-toastify', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({ useAuth: () => mockedUseAuth() }));
const authWith = (permissions: string[]) => ({
  hasPermission: (p: string) => permissions.includes(p)
});
const READ = 'retrieval.corpus.read';
const ADMIN = 'retrieval.corpus.admin';

const corpus = (over: Record<string, unknown>) => ({
  name: 'crm',
  owner: 'crm-module',
  ownerRunning: true,
  registered: true,
  rerankEnabled: false,
  state: 'active',
  activeGeneration: 1,
  collection: 'orkestra_crm_g1',
  outbox: { pending: 2, oldestAt: '2026-10-28T10:00:00Z' },
  tenantsDone: 3,
  tenantsBuilding: 1,
  ...over
});

const crm = corpus({});
// An unregistered corpus whose state row still exists: the only kind the
// drop action is offered on.
const old = corpus({
  name: 'old',
  owner: 'legacy',
  ownerRunning: false,
  registered: false,
  state: 'error',
  reason: 'endpoint_unknown',
  activeGeneration: 2,
  collection: 'orkestra_old_g2',
  outbox: { pending: 0 },
  tenantsDone: 0,
  tenantsBuilding: 0
});
// A registered corpus in the error state still serves its active
// generation, so it is still diagnosable.
const docs = corpus({
  name: 'docs',
  owner: 'docs-module',
  state: 'error',
  reason: 'locality',
  outbox: { pending: 0 }
});

const report = (over: Record<string, unknown> = {}) => ({
  engine: { status: 'ok' },
  inference: [
    {
      endpoint: 'embed-local',
      kind: 'embed',
      status: 'ok',
      profile: 'ip_embed_a',
      boundary: 'local'
    }
  ],
  corpora: [crm, old],
  erasures: { pending: 1, fencing: 0 },
  ...over
});

let corporaReads = 0;
const stubReport = (body: Record<string, unknown> = report()) => {
  corporaReads = 0;
  server.use(
    http.get(url('/v1/admin/retrieval/corpora'), () => {
      corporaReads++;
      return HttpResponse.json(body);
    })
  );
};

const rowOf = async (text: string) => {
  const cell = await screen.findByText(text, { selector: 'td *, td' });
  const row = cell.closest('tr');
  if (!row) throw new Error(`no table row holds ${text}`);
  return row as HTMLElement;
};

const renderTab = () => renderWithProviders(<CorporaTab />);

beforeEach(() => {
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  mockedUseAuth.mockReturnValue(authWith([READ, ADMIN]));
});

describe('CorporaTab', () => {
  it('shows the engine, inference and erasure status row', async () => {
    stubReport();
    renderTab();
    await rowOf('crm');
    expect(screen.getByText('Reachable')).toBeInTheDocument();
    expect(screen.getByText('embed-local')).toBeInTheDocument();
    expect(screen.getByText('Erasures pending')).toBeInTheDocument();
  });

  it('offers Drop only on orphan rows and only with corpus.admin', async () => {
    stubReport();
    const { unmount } = renderTab();
    const crmRow = await rowOf('crm');
    const oldRow = await rowOf('old');
    const drops = screen.getAllByRole('button', { name: /^drop/i });
    expect(drops).toHaveLength(1);
    expect(oldRow).toContainElement(drops[0]);
    expect(
      within(crmRow).getByRole('button', { name: /diagnose/i })
    ).toBeInTheDocument();
    expect(
      within(oldRow).queryByRole('button', { name: /diagnose/i })
    ).not.toBeInTheDocument();
    unmount();

    mockedUseAuth.mockReturnValue(authWith([READ]));
    renderTab();
    await rowOf('old');
    expect(
      screen.queryByRole('button', { name: /^drop/i })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /diagnose/i })
    ).not.toBeInTheDocument();
  });

  it('shows an error corpus with a danger badge and its reason, still diagnosable', async () => {
    stubReport(report({ corpora: [docs] }));
    renderTab();
    const row = await rowOf('docs');
    const badge = within(row).getByText('Error');
    expect(badge.closest('.badge')).toHaveClass('badge-subtle-danger');
    expect(within(row).getByText('Locality violation')).toBeInTheDocument();
    expect(
      within(row).getByRole('button', { name: /diagnose/i })
    ).toBeInTheDocument();
  });

  it('drops an orphan after confirmation and refetches', async () => {
    stubReport();
    const deletes: string[] = [];
    server.use(
      http.delete(url('/v1/admin/retrieval/corpora/:corpus'), ({ request }) => {
        deletes.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      })
    );
    renderTab();
    const row = await rowOf('old');
    await userEvent.click(within(row).getByRole('button', { name: /^drop/i }));
    const dialog = await screen.findByRole('dialog');
    // The confirmation names the corpus and every generation it drops.
    expect(dialog).toHaveTextContent('old');
    expect(dialog).toHaveTextContent('1, 2');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Drop' }));

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));
    expect(deletes).toEqual(['/v1/admin/retrieval/corpora/old']);
    await waitFor(() => expect(corporaReads).toBe(2));
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    );
  });

  it('explains a drop refused because the corpus was registered meanwhile', async () => {
    stubReport();
    server.use(
      http.delete(url('/v1/admin/retrieval/corpora/old'), () =>
        HttpResponse.json(
          { code: 'retrieval.corpus_registered', detail: 'registered' },
          { status: 409 }
        )
      )
    );
    renderTab();
    const row = await rowOf('old');
    await userEvent.click(within(row).getByRole('button', { name: /^drop/i }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Drop' }));
    expect(
      await within(dialog).findByText(/the corpus is still registered/i)
    ).toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it('says nothing when the step-up for a drop is dismissed', async () => {
    stubReport();
    server.use(
      http.delete(url('/v1/admin/retrieval/corpora/old'), () =>
        HttpResponse.json({ code: 'step_up_required' }, { status: 401 })
      )
    );
    let proofOpened = false;
    const unsubscribe = subscribeStepUp(open => {
      if (open) {
        proofOpened = true;
        queueMicrotask(() => completeStepUp(false));
      }
    });
    try {
      renderTab();
      const row = await rowOf('old');
      await userEvent.click(
        within(row).getByRole('button', { name: /^drop/i })
      );
      const dialog = await screen.findByRole('dialog');
      const confirm = within(dialog).getByRole('button', { name: 'Drop' });
      await userEvent.click(confirm);
      await waitFor(() => expect(proofOpened).toBe(true));
      await waitFor(() => expect(confirm).toBeEnabled());
      expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument();
      expect(toast.success).not.toHaveBeenCalled();
      expect(toast.error).not.toHaveBeenCalled();
    } finally {
      unsubscribe();
      completeStepUp(false);
    }
  });

  it('shows the not-configured notice and still lists corpora', async () => {
    stubReport(report({ engine: { status: 'not_configured' } }));
    renderTab();
    expect(
      await screen.findByText(/vector engine is not configured/i)
    ).toBeInTheDocument();
    expect(await rowOf('crm')).toBeInTheDocument();
    expect(await rowOf('old')).toBeInTheDocument();
  });

  it('renders diagnostic arm scores', async () => {
    stubReport();
    let body: Record<string, unknown> | null = null;
    server.use(
      http.post(
        url('/v1/admin/retrieval/corpora/crm/diagnostics'),
        async ({ request }) => {
          body = (await request.json()) as Record<string, unknown>;
          return HttpResponse.json({
            corpus: 'crm',
            collection: 'orkestra_crm_g1',
            profile: 'ip_embed_a',
            principals: ['tenant:*', 'user:u-1'],
            degraded: false,
            hits: [
              {
                sourceId: 'contact-7',
                chunkId: 'contact-7#0',
                rank: 1,
                score: 0.91,
                fused: 0.0328,
                dense: 0.8,
                keyword: null,
                snippet: 'Ada Lovelace'
              }
            ]
          });
        }
      )
    );
    renderTab();
    const row = await rowOf('crm');
    await userEvent.click(
      within(row).getByRole('button', { name: /diagnose/i })
    );
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Query'), 'ada');
    await userEvent.click(within(dialog).getByRole('button', { name: /run/i }));

    const hit = (await within(dialog).findByText('contact-7')).closest('tr');
    if (!hit) throw new Error('no hit row');
    expect(within(hit).getByText('0.8')).toBeInTheDocument();
    expect(within(hit).getAllByText('—').length).toBeGreaterThanOrEqual(1);
    expect(within(dialog).getByText('user:u-1')).toBeInTheDocument();
    expect(body).toEqual({ query: 'ada', mode: 'hybrid', limit: 10 });
  });

  it('validates the diagnostic query before sending it', async () => {
    stubReport();
    let posts = 0;
    server.use(
      http.post(url('/v1/admin/retrieval/corpora/crm/diagnostics'), () => {
        posts++;
        return HttpResponse.json({});
      })
    );
    renderTab();
    const row = await rowOf('crm');
    await userEvent.click(
      within(row).getByRole('button', { name: /diagnose/i })
    );
    const dialog = await screen.findByRole('dialog');
    const limit = within(dialog).getByLabelText('Results');
    await userEvent.clear(limit);
    await userEvent.type(limit, '101');
    await userEvent.click(within(dialog).getByRole('button', { name: /run/i }));
    expect(await within(dialog).findByText('Enter a query.')).toBeVisible();
    expect(within(dialog).getByText('Between 1 and 100.')).toBeVisible();
    expect(posts).toBe(0);
  });

  it('shows nothing when the step-up for a diagnostic search is dismissed', async () => {
    stubReport();
    server.use(
      http.post(url('/v1/admin/retrieval/corpora/crm/diagnostics'), () =>
        HttpResponse.json({ code: 'step_up_required' }, { status: 401 })
      )
    );
    let proofOpened = false;
    const unsubscribe = subscribeStepUp(open => {
      if (open) {
        proofOpened = true;
        queueMicrotask(() => completeStepUp(false));
      }
    });
    try {
      renderTab();
      const row = await rowOf('crm');
      await userEvent.click(
        within(row).getByRole('button', { name: /diagnose/i })
      );
      const dialog = await screen.findByRole('dialog');
      await userEvent.type(within(dialog).getByLabelText('Query'), 'ada');
      const run = within(dialog).getByRole('button', { name: /run/i });
      await userEvent.click(run);
      await waitFor(() => expect(proofOpened).toBe(true));
      await waitFor(() =>
        expect(
          within(dialog).getByRole('button', { name: /run/i })
        ).toBeEnabled()
      );
      expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument();
      expect(within(dialog).queryByRole('table')).not.toBeInTheDocument();
    } finally {
      unsubscribe();
      completeStepUp(false);
    }
  });
});
