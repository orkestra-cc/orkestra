import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'react-toastify';
import { renderWithProviders, type TestRootState } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import tenantReducer from 'store/slices/tenantSlice';
import { completeStepUp, subscribeStepUp } from 'store/stepUp';
import BudgetTab from './BudgetTab';

vi.mock('react-toastify', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({ useAuth: () => mockedUseAuth() }));
const authWith = (permissions: string[]) => ({
  hasPermission: (p: string) => permissions.includes(p)
});
const ALL = ['llm.admin.read', 'llm.budgets.admin'];

const budget = {
  monthlyTokenLimit: 500000,
  warnPct: 90,
  action: 'warn_only',
  notifyExtraUserUuids: ['u-2'],
  updatedBy: 'u-1',
  updatedAt: '2026-10-01T00:00:00Z'
};
const members = {
  members: [
    {
      id: 'ms-2',
      userUUID: 'u-2',
      tenantId: 't-1',
      roles: ['member'],
      isOwner: false,
      joinedAt: '2026-10-01T00:00:00Z',
      email: 'grace@example.com'
    }
  ]
};

const puts: Record<string, unknown>[] = [];

function stub(current: typeof budget | null, put?: () => Response) {
  server.use(
    http.get(url('/v1/admin/llm/budget'), () =>
      current
        ? HttpResponse.json(current)
        : new HttpResponse(null, { status: 204 })
    ),
    http.get(url('/v1/admin/tenants/t-1/members'), () =>
      HttpResponse.json(members)
    ),
    http.put(url('/v1/admin/llm/budget'), async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      puts.push(body);
      return put ? put() : HttpResponse.json({ ...budget, ...body });
    })
  );
}

const preloadedState = {
  tenant: { ...tenantReducer(undefined, { type: 'init' }), currentOrgId: 't-1' }
} as Partial<TestRootState>;
const renderTab = () => renderWithProviders(<BudgetTab />, { preloadedState });

const limitInput = () => screen.findByLabelText(/monthly token limit/i);

beforeEach(() => {
  puts.length = 0;
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  mockedUseAuth.mockReturnValue(authWith(ALL));
});

describe('BudgetTab', () => {
  it('starts an empty form with the defaults when the org has no budget, and saves it', async () => {
    stub(null);
    renderTab();
    expect(await screen.findByText(/has no budget/i)).toBeInTheDocument();
    expect(await limitInput()).toHaveValue(null);
    expect(screen.getByLabelText(/warning threshold/i)).toHaveValue(80);
    expect(screen.getByLabelText(/when a call does not fit/i)).toHaveValue(
      'block'
    );

    await userEvent.type(await limitInput(), '1000000');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toEqual({
      monthlyTokenLimit: 1000000,
      warnPct: 80,
      action: 'block',
      notifyExtraUserUuids: []
    });
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith('Budget saved')
    );
  });

  it('prefills the stored budget with its extra recipients', async () => {
    stub(budget);
    renderTab();
    await waitFor(async () => expect(await limitInput()).toHaveValue(500000));
    expect(screen.getByLabelText(/warning threshold/i)).toHaveValue(90);
    expect(screen.getByLabelText(/when a call does not fit/i)).toHaveValue(
      'warn_only'
    );
    expect(await screen.findByText('grace@example.com')).toBeInTheDocument();
    expect(screen.getByText(/last changed on/i)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toEqual({
      monthlyTokenLimit: 500000,
      warnPct: 90,
      action: 'warn_only',
      notifyExtraUserUuids: ['u-2']
    });
  });

  it.each([
    ['0', '80', /the minimum is 1/i],
    ['1000', '101', /the maximum is 100/i],
    ['1000', '-1', /the minimum is 0/i],
    ['', '80', /this field is required/i]
  ])('refuses limit %j with threshold %j', async (limit, warnPct, message) => {
    stub(null);
    renderTab();
    const input = await limitInput();
    if (limit) await userEvent.type(input, limit);
    const warn = screen.getByLabelText(/warning threshold/i);
    await userEvent.clear(warn);
    await userEvent.type(warn, warnPct);
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(puts).toHaveLength(0);
  });

  it('shows the written detail of a refused save', async () => {
    stub(budget, () =>
      HttpResponse.json(
        { code: 'llm.invalid_request', detail: 'The limit is too large.' },
        { status: 422 }
      )
    );
    renderTab();
    await waitFor(async () => expect(await limitInput()).toHaveValue(500000));
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    expect(
      await screen.findByText('The limit is too large.')
    ).toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it('relies on the global step-up and says nothing when it is dismissed', async () => {
    stub(budget, () =>
      HttpResponse.json({ code: 'step_up_required' }, { status: 401 })
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
      await waitFor(async () => expect(await limitInput()).toHaveValue(500000));
      await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
      await waitFor(() => expect(proofOpened).toBe(true));
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /^save$/i })).toBeEnabled()
      );
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      expect(toast.error).not.toHaveBeenCalled();
      expect(toast.success).not.toHaveBeenCalled();
    } finally {
      unsubscribe();
      completeStepUp(false);
    }
  });

  it('keeps the form read-only without llm.budgets.admin', async () => {
    mockedUseAuth.mockReturnValue(authWith(['llm.admin.read']));
    stub(budget);
    renderTab();
    await waitFor(async () => expect(await limitInput()).toBeDisabled());
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled();
    expect(
      screen.getByText(/permission to manage the llm budget/i)
    ).toBeInTheDocument();
  });

  it('shows a retryable error state when the budget cannot be read', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/admin/llm/budget'), () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ code: 'llm.internal' }, { status: 500 })
          : new HttpResponse(null, { status: 204 });
      }),
      http.get(url('/v1/admin/tenants/t-1/members'), () =>
        HttpResponse.json(members)
      )
    );
    renderTab();
    await userEvent.click(
      await screen.findByRole('button', { name: /retry/i })
    );
    expect(await screen.findByText(/has no budget/i)).toBeInTheDocument();
  });
});
