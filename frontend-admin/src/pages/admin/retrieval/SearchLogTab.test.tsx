import { beforeEach, describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { completeStepUp, subscribeStepUp } from 'store/stepUp';
import SearchLogTab from './SearchLogTab';

const USER = '3f0c1f6e-1d2a-4b7e-9a51-2c4d5e6f7a80';
// An integer above 2^53: JSON.parse would round it to ...992.
const FILTER_TEXT = '{"field":"seniority","op":"eq","value":9007199254740993}';

const entry = (i: number) => ({
  id: `row-${i}`,
  createdAt: '2026-10-28T10:00:00Z',
  userUuid: USER,
  corpus: 'crm',
  query: `query ${i}`,
  filter: { field: 'seniority', op: 'gte', value: 5 },
  filterText: FILTER_TEXT,
  mode: 'hybrid',
  principals: ['tenant:*', 'user:u-1'],
  returnedSourceIds: ['contact-7', 'contact-9'],
  subjects: ['subject-a'],
  hitCount: 2,
  degraded: false,
  outcome: 'ok',
  durationMs: 42
});

const searches: URLSearchParams[] = [];
const stubLog = (total: number, pageSize = 50) => {
  server.use(
    http.get(url('/v1/admin/retrieval/search-log'), ({ request }) => {
      const params = new URL(request.url).searchParams;
      searches.push(params);
      const offset = Number(params.get('offset'));
      const count = Math.max(0, Math.min(pageSize, total - offset));
      return HttpResponse.json({
        items: Array.from({ length: count }, (_, k) => entry(offset + k)),
        total,
        limit: 50,
        offset
      });
    })
  );
};

beforeEach(() => {
  searches.length = 0;
});

describe('SearchLogTab', () => {
  it('states that every view is audited', async () => {
    stubLog(1);
    renderWithProviders(<SearchLogTab />);
    expect(
      await screen.findByText(/recorded in the audit trail/i)
    ).toBeInTheDocument();
  });

  it('applies the user and date filters to the request', async () => {
    stubLog(1);
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 0');
    expect(searches).toHaveLength(1);

    await userEvent.type(screen.getByLabelText('User UUID'), USER);
    await userEvent.type(screen.getByLabelText('From'), '2026-10-01');
    // Nothing is sent until the filters are applied.
    expect(searches).toHaveLength(1);
    await userEvent.click(screen.getByRole('button', { name: /apply/i }));

    await waitFor(() => expect(searches).toHaveLength(2));
    const sent = searches[1];
    expect(sent.get('userUuid')).toBe(USER);
    expect(sent.get('since')).toBe(new Date(2026, 9, 1).toISOString());
    expect(sent.has('until')).toBe(false);
    expect(sent.get('offset')).toBe('0');
    expect(sent.get('limit')).toBe('50');
  });

  it('sends the end of the chosen day as the upper bound', async () => {
    stubLog(1);
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 0');
    await userEvent.type(screen.getByLabelText('To'), '2026-10-02');
    await userEvent.click(screen.getByRole('button', { name: /apply/i }));
    await waitFor(() => expect(searches).toHaveLength(2));
    expect(searches[1].get('until')).toBe(
      new Date(2026, 9, 2, 23, 59, 59, 999).toISOString()
    );
  });

  it('refuses a malformed user UUID without sending it', async () => {
    stubLog(1);
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 0');
    await userEvent.type(screen.getByLabelText('User UUID'), 'not-a-uuid');
    await userEvent.click(screen.getByRole('button', { name: /apply/i }));
    expect(await screen.findByText('Enter a valid UUID.')).toBeVisible();
    expect(searches).toHaveLength(1);
  });

  it('pages by offset', async () => {
    stubLog(120);
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 0');
    expect(screen.getByText('1 to 50 of 120')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(await screen.findByText('query 50')).toBeInTheDocument();
    expect(searches.map(s => s.get('offset'))).toEqual(['0', '50']);
    expect(screen.getByText('51 to 100 of 120')).toBeInTheDocument();
  });

  it('returns to the first page when a later page comes back empty', async () => {
    let total = 120;
    server.use(
      http.get(url('/v1/admin/retrieval/search-log'), ({ request }) => {
        const offset = Number(new URL(request.url).searchParams.get('offset'));
        searches.push(new URL(request.url).searchParams);
        const count = Math.max(0, Math.min(50, total - offset));
        return HttpResponse.json({
          items: Array.from({ length: count }, (_, k) => entry(offset + k)),
          total,
          limit: 50,
          offset
        });
      })
    );
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 0');
    // Retention removes everything past the first page while the operator pages.
    total = 30;
    await userEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(await screen.findByText('query 29')).toBeInTheDocument();
    expect(screen.getByText('1 to 30 of 30')).toBeInTheDocument();
    expect(searches.map(s => s.get('offset'))).toEqual(['0', '50', '0']);
  });

  it('never shows the previous page while the next one loads', async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>(resolve => {
      release = resolve;
    });
    server.use(
      http.get(url('/v1/admin/retrieval/search-log'), async ({ request }) => {
        const offset = Number(new URL(request.url).searchParams.get('offset'));
        if (offset > 0) await gate;
        return HttpResponse.json({
          items: Array.from({ length: Math.min(50, 120 - offset) }, (_, k) =>
            entry(offset + k)
          ),
          total: 120,
          limit: 50,
          offset
        });
      })
    );
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 0');
    await userEvent.click(screen.getByRole('button', { name: 'Next' }));
    // Page 1's rows must not sit under the "51 to 100" range meanwhile.
    await waitFor(() =>
      expect(screen.queryByText('query 0')).not.toBeInTheDocument()
    );
    expect(screen.queryByText('51 to 100 of 120')).not.toBeInTheDocument();
    release();
    expect(await screen.findByText('query 50')).toBeInTheDocument();
    expect(screen.getByText('51 to 100 of 120')).toBeInTheDocument();
  });

  it("shows the server page and total as they are: hidden unchecked rows are the server's business", async () => {
    // The backend omits unchecked rows from both the page and the total; the
    // client neither asks for them nor second-guesses the count.
    stubLog(2);
    renderWithProviders(<SearchLogTab />);
    await screen.findByText('query 1');
    expect(screen.getByText('1 to 2 of 2')).toBeInTheDocument();
    expect([...searches[0].keys()].sort()).toEqual(['limit', 'offset']);
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
  });

  it('expands a row to its principals, returned ids, subjects and filter', async () => {
    stubLog(1);
    renderWithProviders(<SearchLogTab />);
    const row = (await screen.findByText('query 0')).closest('tr');
    if (!row) throw new Error('no row');
    const toggle = within(row).getByRole('button', { name: /show details/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(within(row).getByText('user:u-1')).toBeInTheDocument();
    expect(within(row).getByText(/contact-9/)).toBeInTheDocument();
    expect(within(row).getByText(/subject-a/)).toBeInTheDocument();
    // The stored text verbatim: the integer above 2^53 is not rounded.
    expect(within(row).getByText(FILTER_TEXT)).toBeInTheDocument();
    expect(within(row).queryByText(/9007199254740992/)).not.toBeInTheDocument();
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(within(row).queryByText('user:u-1')).not.toBeInTheDocument();
  });

  it('shows no filter for a row without filterText, never re-serializing filter', async () => {
    // Row 0 has a decoded filter but no text; row 1 has neither.
    const withFilterOnly: Record<string, unknown> = entry(0);
    delete withFilterOnly.filterText;
    const bare: Record<string, unknown> = { ...entry(1) };
    delete bare.filter;
    delete bare.filterText;
    server.use(
      http.get(url('/v1/admin/retrieval/search-log'), () =>
        HttpResponse.json({
          items: [withFilterOnly, bare],
          total: 2,
          limit: 50,
          offset: 0
        })
      )
    );
    renderWithProviders(<SearchLogTab />);
    for (const query of ['query 0', 'query 1']) {
      const row = (await screen.findByText(query)).closest('tr');
      if (!row) throw new Error('no row');
      await userEvent.click(
        within(row).getByRole('button', { name: /show details/i })
      );
      const filterLabel = within(row).getByText('Filter');
      const value = filterLabel.nextElementSibling;
      expect(value).toHaveTextContent('None');
      expect(within(row).queryByText(/seniority/)).not.toBeInTheDocument();
    }
  });

  it('offers to verify again when the step-up is dismissed', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/admin/retrieval/search-log'), () => {
        calls++;
        return calls === 1
          ? HttpResponse.json({ code: 'step_up_required' }, { status: 401 })
          : HttpResponse.json({
              items: [entry(0)],
              total: 1,
              limit: 50,
              offset: 0
            });
      })
    );
    let proofOpened = false;
    const unsubscribe = subscribeStepUp(open => {
      if (open) {
        proofOpened = true;
        queueMicrotask(() => completeStepUp(false));
      }
    });
    try {
      renderWithProviders(<SearchLogTab />);
      const verify = await screen.findByRole('button', {
        name: /verify and load/i
      });
      expect(proofOpened).toBe(true);
      expect(screen.queryByRole('table')).not.toBeInTheDocument();
      expect(
        screen.queryByText(/could not be loaded/i)
      ).not.toBeInTheDocument();
      await userEvent.click(verify);
      expect(await screen.findByText('query 0')).toBeInTheDocument();
    } finally {
      unsubscribe();
      completeStepUp(false);
    }
  });
});
