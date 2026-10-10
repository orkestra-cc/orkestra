import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import UsageTab from './UsageTab';

// The chart is ECharts on a canvas: stub the wrapper so the test stays
// DOM-only, and stub the theme context renderWithProviders does not wire.
vi.mock('components/common/ReactEchart', () => ({
  default: () => <div data-testid="echart-stub" />
}));
vi.mock('providers/AppProvider', () => ({
  useAppContext: () => ({ getThemeColor: () => 'currentColor', config: {} })
}));

const month = {
  period: '2026-10',
  requests: 7,
  orgInputTokens: 600,
  orgOutputTokens: 100,
  userAccountInputTokens: 200,
  userAccountOutputTokens: 100,
  reservedTokens: 50
};
const budget = {
  monthlyTokenLimit: 2000,
  warnPct: 80,
  action: 'block',
  notifyExtraUserUuids: [],
  updatedBy: 'u-1',
  updatedAt: '2026-10-01T00:00:00Z'
};
const rows = [
  { key: 'm-1', requests: 5, inputTokens: 700, outputTokens: 150 },
  { key: 'm-gone', requests: 2, inputTokens: 100, outputTokens: 50 }
];
const model = {
  uuid: 'm-1',
  name: 'Fast',
  provider: 'openai',
  modelId: 'gpt-5.6-terra',
  capabilities: {
    chat: true,
    streaming: true,
    tools: false,
    structuredOutput: false,
    embeddings: false
  },
  credentialRef: { kind: 'org', credentialUuid: 'c-1' },
  defaults: {},
  purposes: [{ purpose: 'default', priority: 10 }],
  access: 'granted',
  status: 'active',
  createdBy: 'u',
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
  grants: []
};

const queries: URLSearchParams[] = [];
const last = () => queries[queries.length - 1];

function stubUsage(report: Record<string, unknown>) {
  server.use(
    http.get(url('/v1/admin/llm/usage'), ({ request }) => {
      const params = new URL(request.url).searchParams;
      queries.push(params);
      return HttpResponse.json({
        from: '2026-09-10T00:00:00Z',
        to: '2026-10-10T00:00:00Z',
        groupBy: params.get('groupBy') ?? 'model',
        ...report
      });
    }),
    http.get(url('/v1/admin/llm/models'), () =>
      HttpResponse.json({ items: [model] })
    )
  );
}

const card = (title: RegExp) => {
  const el = screen.getByText(title).closest('.stat-card');
  if (!el) throw new Error(`no stat card titled ${title}`);
  return el as HTMLElement;
};

beforeEach(() => {
  queries.length = 0;
});

describe('UsageTab', () => {
  it('shows consumed and reserved tokens, the budget share and the grouped rows', async () => {
    stubUsage({ month, budget, rows });
    renderWithProviders(<UsageTab />);

    // 600 + 100 + 200 + 100: both credential kinds count.
    expect(await screen.findByText('1,000')).toBeInTheDocument();
    const budgetCard = card(/^budget used$/i);
    expect(within(budgetCard).getByText('50%')).toBeInTheDocument();
    // 50% is below the 80% threshold: the tile stays neutral.
    expect(budgetCard.className).not.toMatch(/stat-card-accent-/);
    // Reserved is declared next to consumed, never folded into it.
    expect(
      within(budgetCard).getByText(/1,000 consumed · 50 reserved · limit 2,000/)
    ).toBeInTheDocument();
    expect(
      within(card(/^tokens reserved$/i)).getByText('50')
    ).toBeInTheDocument();
    expect(
      screen.getByText(/the limit is an admission check/i)
    ).toBeInTheDocument();

    // Model keys resolve to their name; an unknown one stays visible.
    const table = screen.getByRole('table');
    expect(within(table).getByText('Fast')).toBeInTheDocument();
    expect(within(table).getByText(/m-gone/)).toBeInTheDocument();
    expect(within(table).getAllByRole('row')).toHaveLength(3);
    expect(screen.getByTestId('echart-stub')).toBeInTheDocument();
  });

  it('marks the budget tile once the warning threshold is reached and flags exhaustion', async () => {
    stubUsage({
      month: {
        ...month,
        orgInputTokens: 1600,
        exhaustedAt: '2026-10-09T00:00:00Z'
      },
      budget,
      rows
    });
    renderWithProviders(<UsageTab />);
    const value = await screen.findByText('100%');
    const budgetCard = value.closest('.stat-card') as HTMLElement;
    expect(budgetCard.className).toMatch(/stat-card-accent-danger/);
    expect(within(budgetCard).getByText(/exhausted/i)).toBeInTheDocument();
  });

  it('applies the warning accent at the threshold', async () => {
    stubUsage({ month: { ...month, orgInputTokens: 1200 }, budget, rows });
    renderWithProviders(<UsageTab />);
    const value = await screen.findByText('80%');
    expect((value.closest('.stat-card') as HTMLElement).className).toMatch(
      /stat-card-accent-warning/
    );
  });

  it('says there is no budget and keeps the tile neutral', async () => {
    stubUsage({ month, rows });
    renderWithProviders(<UsageTab />);
    const budgetCard = (await screen.findByText(/^no budget$/i)).closest(
      '.stat-card'
    ) as HTMLElement;
    expect(budgetCard.className).not.toMatch(/stat-card-accent-/);
  });

  it('sends the grouping and the period window to the report', async () => {
    stubUsage({ month, budget, rows });
    renderWithProviders(<UsageTab />);
    await screen.findByText('1,000');
    expect(last()?.get('groupBy')).toBe('model');
    const firstFrom = last()?.get('from');
    expect(firstFrom).toBeTruthy();

    await userEvent.selectOptions(screen.getByLabelText(/group by/i), 'caller');
    await waitFor(() => expect(last()?.get('groupBy')).toBe('caller'));

    await userEvent.selectOptions(screen.getByLabelText(/period/i), 'month');
    await waitFor(() => {
      const from = new Date(last()?.get('from') ?? '');
      expect(from.getUTCDate()).toBe(1);
      expect(from.getUTCHours()).toBe(0);
    });
  });

  it('shows a retryable error state', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/admin/llm/usage'), () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ code: 'llm.internal' }, { status: 500 })
          : HttpResponse.json({ groupBy: 'model', month, rows: [] });
      }),
      http.get(url('/v1/admin/llm/models'), () =>
        HttpResponse.json({ items: [] })
      )
    );
    renderWithProviders(<UsageTab />);
    await userEvent.click(
      await screen.findByRole('button', { name: /retry/i })
    );
    expect(
      await screen.findByText(/no usage in this period/i)
    ).toBeInTheDocument();
  });
});
