import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { toast } from 'react-toastify';
import ErasureRequestsTab from './ErasureRequestsTab';

vi.mock('react-toastify', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => ({ currentUser: { id: 'op-me' } })
}));
beforeEach(() => vi.clearAllMocks());

const LIST = '/v1/admin/compliance/erasure-requests';
const row = (over: Record<string, unknown>) => ({
  uuid: 'er-1',
  userUuid: 'u-1',
  status: 'pending',
  requestedAt: '2026-06-01T10:00:00Z',
  ...over
});
const rowOf = (subject: string) =>
  within(screen.getByText(subject).closest('tr') as HTMLElement);

describe('ErasureRequestsTab', () => {
  it('shows approvals as k/N, defaulting N to 1', async () => {
    server.use(
      http.get(url(LIST), () =>
        HttpResponse.json({
          items: [
            row({
              uuid: 'a',
              userUuid: 'u-a',
              requiredApprovals: 3,
              approvals: [{ by: 'x', at: '2026-06-01T11:00:00Z', note: 'n' }]
            }),
            row({ uuid: 'b', userUuid: 'u-b' })
          ]
        })
      )
    );
    renderWithProviders(<ErasureRequestsTab />);
    await screen.findByText('u-a');
    expect(rowOf('u-a').getByText('1/3')).toBeInTheDocument();
    expect(rowOf('u-b').getByText('0/1')).toBeInTheDocument();
  });

  it('disables execute until approvals plus the operator reach N, and lets it run after', async () => {
    let execBody: unknown = 'unset';
    server.use(
      http.get(url(LIST), () =>
        HttpResponse.json({
          items: [
            row({
              uuid: 'a',
              userUuid: 'u-a',
              requiredApprovals: 3,
              approvals: [{ by: 'x', at: '2026-06-01T11:00:00Z' }]
            }),
            row({
              uuid: 'b',
              userUuid: 'u-b',
              requiredApprovals: 2,
              mode: 'anonymize',
              approvals: [{ by: 'x', at: '2026-06-01T11:00:00Z' }]
            })
          ]
        })
      ),
      http.post(url(`${LIST}/:id/execute`), async ({ request }) => {
        execBody = await request.json();
        return HttpResponse.json({ status: 'completed' });
      })
    );
    renderWithProviders(<ErasureRequestsTab />);
    await screen.findByText('u-a');
    expect(
      rowOf('u-a').getByRole('button', { name: /execute/i })
    ).toBeDisabled();
    const run = rowOf('u-b').getByRole('button', { name: /execute/i });
    expect(run).toBeEnabled();
    const user = userEvent.setup();
    await user.click(run);
    // Mode is locked by the request: shown read-only, none is sent.
    const dlg = within(await screen.findByRole('dialog'));
    expect(dlg.getByDisplayValue('Anonymize')).toHaveAttribute('readonly');
    await user.click(dlg.getByRole('button', { name: /^execute$/i }));
    await waitFor(() => expect(execBody).toEqual({}));
    expect(toast.success).toHaveBeenCalled();
  });

  it('round-trips the status filter through the URL and the query', async () => {
    const seen: (string | null)[] = [];
    server.use(
      http.get(url(LIST), ({ request }) => {
        const st = new URL(request.url).searchParams.get('status');
        seen.push(st);
        return HttpResponse.json({
          items:
            st === 'completed'
              ? [row({ uuid: 'c', userUuid: 'u-done', status: 'completed' })]
              : [row({})]
        });
      })
    );
    renderWithProviders(<ErasureRequestsTab />, {
      routerEntries: ['/?tab=requests&status=completed']
    });
    expect(await screen.findByText('u-done')).toBeInTheDocument();
    expect(screen.getByLabelText(/status filter/i)).toHaveValue('completed');
    expect(seen).toContain('completed');
    await userEvent
      .setup()
      .selectOptions(screen.getByLabelText(/status filter/i), 'open');
    expect(await screen.findByText('u-1')).toBeInTheDocument();
  });

  it('opens a request with the typed body', async () => {
    let body: unknown;
    server.use(
      http.get(url(LIST), () => HttpResponse.json({ items: [] })),
      http.post(url(LIST), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(row({ uuid: 'new' }), { status: 201 });
      })
    );
    renderWithProviders(<ErasureRequestsTab />);
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole('button', { name: /new request/i })
    );
    await user.type(screen.getByLabelText(/subject useruuid/i), 'u-77');
    await user.selectOptions(screen.getByLabelText(/^mode/i), 'anonymize');
    await user.type(screen.getByLabelText(/^reason/i), 'subject asked');
    await user.click(screen.getByRole('button', { name: /open request/i }));
    await waitFor(() =>
      expect(body).toEqual({
        userUuid: 'u-77',
        mode: 'anonymize',
        reason: 'subject asked'
      })
    );
  });

  it('approves with mode and note, and translates a self-approval error', async () => {
    let body: unknown;
    server.use(
      http.get(url(LIST), () =>
        HttpResponse.json({
          items: [row({ uuid: 'a', userUuid: 'u-a', requiredApprovals: 2 })]
        })
      ),
      http.post(url(`${LIST}/:id/approve`), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(
          { code: 'compliance.erasure_self_approval', status: 409 },
          { status: 409 }
        );
      })
    );
    renderWithProviders(<ErasureRequestsTab />);
    const user = userEvent.setup();
    await user.click(
      (await screen.findAllByRole('button', { name: /^approve$/i }))[0]
    );
    await user.type(screen.getByLabelText(/^note/i), 'checked');
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: /^approve$/i
      })
    );
    await waitFor(() =>
      expect(body).toEqual({ mode: 'hard_delete', note: 'checked' })
    );
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'You cannot open, approve or execute the erasure of your own account'
      )
    );
  });

  it('shows the receipt and downloads it as JSON', async () => {
    const receipt = {
      requestUuid: 'er-c',
      subjectUserUuid: 'u-done',
      status: 'completed',
      requestedAt: '2026-06-01T10:00:00Z',
      attempts: 1,
      approvals: [],
      producers: [{ key: 'user', outcome: 'done' }],
      abandoned: [],
      totalRowsDeleted: 4,
      totalRowsAnonymized: 1,
      generatedAt: '2026-06-02T10:00:00Z'
    };
    server.use(
      http.get(url(LIST), () =>
        HttpResponse.json({
          items: [
            row({ uuid: 'er-c', userUuid: 'u-done', status: 'completed' })
          ]
        })
      ),
      http.get(url(`${LIST}/:id/receipt`), () => HttpResponse.json(receipt))
    );
    const create = vi.fn(() => 'blob:x');
    vi.stubGlobal(
      'URL',
      Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() })
    );
    renderWithProviders(<ErasureRequestsTab />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /receipt/i }));
    expect(
      await screen.findByText('4 deleted, 1 anonymized')
    ).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /download json/i }));
    expect(create).toHaveBeenCalledTimes(1);
    const blob = (create.mock.calls[0] as unknown as [Blob])[0];
    expect(JSON.parse(await blob.text())).toEqual(receipt);
  });

  it('asks for a mode when the request has none and sends it', async () => {
    let body: unknown;
    server.use(
      http.get(url(LIST), () => HttpResponse.json({ items: [row({})] })),
      http.post(url(`${LIST}/:id/execute`), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ status: 'completed' });
      })
    );
    renderWithProviders(<ErasureRequestsTab />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /^execute$/i }));
    const dlg = within(await screen.findByRole('dialog'));
    expect(dlg.getByLabelText(/^mode/i)).toHaveValue('hard_delete');
    await user.selectOptions(dlg.getByLabelText(/^mode/i), 'anonymize');
    await user.click(dlg.getByRole('button', { name: /^execute$/i }));
    await waitFor(() => expect(body).toEqual({ mode: 'anonymize' }));
  });

  it('hides Approve once the operator approved, and keeps loaded pages across a refetch', async () => {
    let calls = 0;
    server.use(
      http.get(url(LIST), ({ request }) => {
        const after = new URL(request.url).searchParams.get('after');
        calls++;
        return HttpResponse.json(
          after
            ? { items: [row({ uuid: 'p2', userUuid: 'u-page2' })] }
            : {
                items: [
                  row({
                    uuid: 'p1',
                    userUuid: 'u-page1',
                    requiredApprovals: 2,
                    mode: 'anonymize',
                    approvals: [{ by: 'op-me', at: '2026-06-01T11:00:00Z' }]
                  })
                ],
                next: 'c1'
              }
        );
      })
    );
    renderWithProviders(<ErasureRequestsTab />);
    const user = userEvent.setup();
    await screen.findByText('u-page1');
    expect(
      rowOf('u-page1').queryByRole('button', { name: /^approve$/i })
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /load more/i }));
    expect(await screen.findByText('u-page2')).toBeInTheDocument();
    expect(calls).toBeGreaterThan(1);
  });

  it('shows retention reasons and the date for retention_minimum_not_met', async () => {
    server.use(
      http.get(url(LIST), () => HttpResponse.json({ items: [row({})] })),
      http.post(url(`${LIST}/:id/execute`), () =>
        HttpResponse.json(
          {
            code: 'compliance.retention_minimum_not_met',
            retainUntil: '2028-03-05T00:00:00Z',
            reasons: ['platform_minimum', 'billing.invoices']
          },
          { status: 409 }
        )
      )
    );
    renderWithProviders(<ErasureRequestsTab />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /^execute$/i }));
    await user.click(
      within(await screen.findByRole('dialog')).getByRole('button', {
        name: /^execute$/i
      })
    );
    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    const msg = String(
      (toast.error as ReturnType<typeof vi.fn>).mock.calls[0][0]
    );
    expect(msg).toContain('platform minimum retention');
    expect(msg).toContain('billing.invoices');
    expect(msg).toContain('2028');
  });
});
