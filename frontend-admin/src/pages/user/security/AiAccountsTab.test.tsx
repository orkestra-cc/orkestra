import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useLocation } from 'react-router';
import { renderWithProviders, type TestRootState } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import tenantReducer, { setCurrentOrg } from 'store/slices/tenantSlice';
import AiAccountsTab from './AiAccountsTab';

// The plan-billed models and the month's usage are their own sections, each
// gated on its own self permission, so useAuth is mocked (precedent:
// pages/admin/llm/BudgetTab.test.tsx).
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({ useAuth: () => mockedUseAuth() }));
const authWith = (permissions: string[]) => ({
  hasPermission: (p: string) => permissions.includes(p)
});
const ALL = ['llm.accounts.self', 'llm.models.self', 'llm.usage.self'];

const AUTHORIZE = 'https://auth.openai.com/api/accounts/authorize?state=s';
const PASTED =
  'http://127.0.0.1:14555/v1/llm/accounts/openai/callback?code=c&state=s&client_id=oaiapp_1';

const empty = { mode: 'hosted_paste', items: [], selectedUuid: '' };
const account = (over: Record<string, unknown> = {}) => ({
  uuid: 'a-1',
  provider: 'openai',
  label: 'Work',
  email: 'u@example.test',
  status: 'active',
  planUsageGranted: true,
  selected: true,
  linkedAt: '2026-10-10T00:00:00Z',
  expiresAt: '2026-10-10T01:00:00Z',
  refreshExpiresAt: '2026-11-10T00:00:00Z',
  ...over
});

const myModels = {
  items: [
    {
      uuid: 'm-1',
      name: 'Plan GPT',
      provider: 'openai',
      modelId: 'gpt-5.6-plan',
      capabilities: {
        chat: true,
        streaming: true,
        tools: false,
        structuredOutput: false,
        embeddings: false
      },
      credentialKind: 'user_account',
      purposes: ['default']
    },
    {
      uuid: 'm-2',
      name: 'Org GPT',
      provider: 'openai',
      modelId: 'gpt-5.6-org',
      capabilities: {
        chat: true,
        streaming: true,
        tools: false,
        structuredOutput: false,
        embeddings: false
      },
      credentialKind: 'org',
      purposes: ['default']
    }
  ]
};
const myUsage = {
  period: '2026-10',
  requests: 4,
  orgInputTokens: 100,
  orgOutputTokens: 20,
  userAccountInputTokens: 3000,
  userAccountOutputTokens: 400
};

// Every query the tab fires is answered: msw is onUnhandledRequest 'error'.
function stub(list: Record<string, unknown> = empty) {
  server.use(
    http.get(url('/v1/llm/accounts/openai'), () => HttpResponse.json(list)),
    http.get(url('/v1/llm/me/models'), () => HttpResponse.json(myModels)),
    http.get(url('/v1/llm/me/usage'), () => HttpResponse.json(myUsage))
  );
}

const rowOf = async (text: string) => {
  const cell = await screen.findByText(text);
  const row = cell.closest('tr');
  if (!row) throw new Error(`no table row holds ${text}`);
  return row as HTMLElement;
};

// Renders the tab with a probe of the router's search string, so a test
// can assert what the callback drain left in the URL.
const renderWithLocation = (entry: string) => {
  let current = '';
  const LocationProbe = () => {
    current = useLocation().search;
    return null;
  };
  renderWithProviders(
    <>
      <LocationProbe />
      <AiAccountsTab />
    </>,
    { routerEntries: [entry] }
  );
  return () => current;
};

const originalLocation = window.location;
let openSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  mockedUseAuth.mockReturnValue(authWith(ALL));
  openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
});
afterEach(() => {
  openSpy.mockRestore();
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: originalLocation
  });
});

describe('AiAccountsTab', () => {
  it('explains that linking is unavailable when siwc is disabled', async () => {
    stub({ ...empty, mode: 'disabled' });
    renderWithProviders(<AiAccountsTab />);
    expect(
      await screen.findByText(/not enabled on this installation/i)
    ).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /link a chatgpt account/i })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /open openai/i })
    ).not.toBeInTheDocument();
  });

  it('paste mode: step 1 opens OpenAI in a new tab, step 2 posts the pasted URL and shows the linked account', async () => {
    let posted: unknown = null;
    let linkBody: unknown = null;
    stub();
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), async ({ request }) => {
        linkBody = await request.json();
        return HttpResponse.json({
          authorizeUrl: AUTHORIZE,
          mode: 'hosted_paste'
        });
      }),
      http.post(
        url('/v1/llm/accounts/openai/complete'),
        async ({ request }) => {
          posted = await request.json();
          return HttpResponse.json(account({ label: 'u@example.test' }));
        }
      )
    );
    renderWithProviders(<AiAccountsTab />);
    await userEvent.click(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    );
    await userEvent.click(
      await screen.findByRole('button', { name: /open openai/i })
    );
    expect(linkBody).toEqual({});
    expect(openSpy).toHaveBeenCalledWith(AUTHORIZE, '_blank', 'noopener');
    const field = await screen.findByLabelText(/paste the address/i);
    await userEvent.type(field, PASTED);
    await userEvent.click(screen.getByRole('button', { name: /complete/i }));
    await waitFor(() => expect(posted).toEqual({ callbackUrl: PASTED }));
    expect(await screen.findByText(/account linked/i)).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/paste the address/i)
    ).not.toBeInTheDocument();
  });

  it('rejects a pasted value that is not an address without calling the API', async () => {
    let completed = false;
    stub();
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), () =>
        HttpResponse.json({ authorizeUrl: AUTHORIZE, mode: 'hosted_paste' })
      ),
      http.post(url('/v1/llm/accounts/openai/complete'), () => {
        completed = true;
        return HttpResponse.json(account());
      })
    );
    renderWithProviders(<AiAccountsTab />);
    await userEvent.click(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    );
    await userEvent.click(
      await screen.findByRole('button', { name: /open openai/i })
    );
    await userEvent.type(
      await screen.findByLabelText(/paste the address/i),
      'not an address'
    );
    await userEvent.click(screen.getByRole('button', { name: /complete/i }));
    expect(await screen.findByText(/not a web address/i)).toBeInTheDocument();
    expect(completed).toBe(false);
  });

  it('translates a refused completion by its llm.* code and keeps the wizard open', async () => {
    stub();
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), () =>
        HttpResponse.json({ authorizeUrl: AUTHORIZE, mode: 'hosted_paste' })
      ),
      http.post(url('/v1/llm/accounts/openai/complete'), () =>
        HttpResponse.json(
          { code: 'llm.siwc_state_invalid', detail: 'provider text' },
          { status: 400 }
        )
      )
    );
    renderWithProviders(<AiAccountsTab />);
    await userEvent.click(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    );
    await userEvent.click(
      await screen.findByRole('button', { name: /open openai/i })
    );
    await userEvent.type(
      await screen.findByLabelText(/paste the address/i),
      PASTED
    );
    await userEvent.click(screen.getByRole('button', { name: /complete/i }));
    expect(
      await screen.findByText(/expired or was already used/i)
    ).toBeInTheDocument();
    expect(screen.queryByText(/provider text/)).not.toBeInTheDocument();
    expect(screen.getByLabelText(/paste the address/i)).toBeInTheDocument();
  });

  it('loopback mode hands the whole page to OpenAI', async () => {
    const assign = vi.fn();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...originalLocation, assign }
    });
    stub({ ...empty, mode: 'loopback_local' });
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), () =>
        HttpResponse.json({ authorizeUrl: AUTHORIZE, mode: 'loopback_local' })
      )
    );
    renderWithProviders(<AiAccountsTab />);
    await userEvent.click(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    );
    await waitFor(() => expect(assign).toHaveBeenCalledWith(AUTHORIZE));
    expect(openSpy).not.toHaveBeenCalled();
    // The paste fallback stays reachable under the button.
    await userEvent.click(
      screen.getByRole('button', { name: /paste a sign-in address/i })
    );
    expect(
      await screen.findByLabelText(/paste the address/i)
    ).toBeInTheDocument();
  });

  it('shows the outcome of the loopback return and drains its params', async () => {
    stub({ ...empty, mode: 'loopback_local' });
    const search = renderWithLocation(
      '/user/security?tab=ai&error=denied&keep=1'
    );
    expect(await screen.findByText(/declined at openai/i)).toBeInTheDocument();
    await waitFor(() => expect(search()).toBe('?tab=ai&keep=1'));
  });

  it('confirms a loopback return that linked the account and drains its params', async () => {
    stub({ ...empty, mode: 'loopback_local' });
    const search = renderWithLocation('/user/security?tab=ai&linked=ok');
    expect(await screen.findByText(/account linked/i)).toBeInTheDocument();
    await waitFor(() => expect(search()).toBe('?tab=ai'));
  });

  it.each([
    '__proto__',
    'constructor',
    'toString',
    '<script>alert(1)</script>'
  ])(
    'renders the documented fallback for a hostile ?error=%s',
    async hostile => {
      stub({ ...empty, mode: 'loopback_local' });
      const search = renderWithLocation(
        `/user/security?tab=ai&error=${encodeURIComponent(hostile)}`
      );
      expect(
        await screen.findByText(/openai did not complete the sign-in/i)
      ).toBeInTheDocument();
      expect(document.querySelector('script')).toBeNull();
      expect(document.body.textContent).not.toContain(hostile);
      await waitFor(() => expect(search()).toBe('?tab=ai'));
    }
  );

  it('shows plan_disabled and needs_reauth states with their remedies', async () => {
    stub({
      mode: 'hosted_paste',
      selectedUuid: 'a-1',
      items: [
        account({
          status: 'plan_disabled',
          planUsageGranted: false
        }),
        account({
          uuid: 'a-2',
          label: 'Personal',
          email: 'p@example.test',
          status: 'needs_reauth',
          selected: false,
          lastError: 'llm.user_account_reauth_required'
        })
      ]
    });
    renderWithProviders(<AiAccountsTab />);
    const work = await rowOf('Work');
    expect(
      within(work).getByText(/plan usage not granted/i)
    ).toBeInTheDocument();
    expect(within(work).getByText(/^selected$/i)).toBeInTheDocument();
    expect(
      within(work).getByRole('button', { name: /re-authorize/i })
    ).toBeInTheDocument();
    const personal = await rowOf('Personal');
    expect(
      within(personal).getByText(/sign in again to keep using/i)
    ).toBeInTheDocument();
    expect(
      within(personal).getByRole('button', { name: /re-authorize/i })
    ).toBeInTheDocument();
    // Only an active account can be selected.
    expect(
      within(personal).queryByRole('button', { name: /^select$/i })
    ).not.toBeInTheDocument();
  });

  it('re-authorizes an account through the link route with its uuid', async () => {
    let linkBody: unknown = null;
    stub({
      mode: 'hosted_paste',
      selectedUuid: '',
      items: [account({ status: 'needs_reauth', selected: false })]
    });
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), async ({ request }) => {
        linkBody = await request.json();
        return HttpResponse.json({
          authorizeUrl: AUTHORIZE,
          mode: 'hosted_paste'
        });
      })
    );
    renderWithProviders(<AiAccountsTab />);
    const row = await rowOf('Work');
    await userEvent.click(
      within(row).getByRole('button', { name: /re-authorize/i })
    );
    await userEvent.click(
      await screen.findByRole('button', { name: /open openai/i })
    );
    expect(linkBody).toEqual({ reauthAccountUuid: 'a-1' });
  });

  it('selects an active account and unlinks one after confirmation', async () => {
    const calls: string[] = [];
    stub({
      mode: 'hosted_paste',
      selectedUuid: 'a-1',
      items: [
        account(),
        account({ uuid: 'a-2', label: 'Personal', selected: false })
      ]
    });
    server.use(
      http.put(url('/v1/llm/accounts/openai/a-2/selected'), ({ request }) => {
        calls.push(`${request.method} a-2/selected`);
        return new HttpResponse(null, { status: 204 });
      }),
      http.delete(url('/v1/llm/accounts/openai/a-2'), ({ request }) => {
        calls.push(`${request.method} a-2`);
        return new HttpResponse(null, { status: 204 });
      })
    );
    renderWithProviders(<AiAccountsTab />);
    const personal = await rowOf('Personal');
    await userEvent.click(
      within(personal).getByRole('button', { name: /^select$/i })
    );
    await waitFor(() => expect(calls).toEqual(['PUT a-2/selected']));
    await userEvent.click(
      within(await rowOf('Personal')).getByRole('button', { name: /unlink/i })
    );
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/personal/i)).toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole('button', { name: /^unlink$/i })
    );
    await waitFor(() =>
      expect(calls).toEqual(['PUT a-2/selected', 'DELETE a-2'])
    );
  });

  it('lists only the plan-billed models and both usage totals', async () => {
    stub({ ...empty, items: [account()], selectedUuid: 'a-1' });
    renderWithProviders(<AiAccountsTab />);
    expect(await screen.findByText('Plan GPT')).toBeInTheDocument();
    expect(screen.queryByText('Org GPT')).not.toBeInTheDocument();
    expect(await screen.findByText('3,400')).toBeInTheDocument();
    expect(screen.getByText('120')).toBeInTheDocument();
  });

  it('skips the models and usage sections without their self permissions', async () => {
    mockedUseAuth.mockReturnValue(authWith(['llm.accounts.self']));
    server.use(
      http.get(url('/v1/llm/accounts/openai'), () => HttpResponse.json(empty))
    );
    renderWithProviders(<AiAccountsTab />);
    expect(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    ).toBeInTheDocument();
    expect(screen.queryByText(/billed to your plan/i)).not.toBeInTheDocument();
  });
  // An org switch (a super_admin keeps the tab mounted: every permission
  // still holds) must not carry an attempt minted in the previous org.
  const membership = (tenantId: string) => ({
    tenantId,
    name: tenantId,
    slug: tenantId,
    plan: 'free',
    roles: [],
    isOwner: true
  });
  const twoOrgs = {
    tenant: {
      ...tenantReducer(undefined, { type: 'init' }),
      memberships: [membership('org-a'), membership('org-b')],
      currentOrgId: 'org-a'
    }
  } as Partial<TestRootState>;

  it('closes the link wizard when the active org changes', async () => {
    stub();
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), () =>
        HttpResponse.json({ authorizeUrl: AUTHORIZE, mode: 'hosted_paste' })
      )
    );
    const { store } = renderWithProviders(<AiAccountsTab />, {
      preloadedState: twoOrgs
    });
    await userEvent.click(
      await screen.findByRole('button', { name: /link a chatgpt account/i })
    );
    expect(
      await screen.findByRole('button', { name: /open openai/i })
    ).toBeInTheDocument();
    act(() => {
      store.dispatch(setCurrentOrg('org-b'));
    });
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: /open openai/i })
      ).not.toBeInTheDocument()
    );
  });

  it('ignores a link response that arrives after the org changed', async () => {
    let answer: () => void = () => {};
    const answered = new Promise<void>(resolve => {
      answer = resolve;
    });
    stub();
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), async () => {
        await answered;
        return HttpResponse.json({
          authorizeUrl: AUTHORIZE,
          mode: 'hosted_paste'
        });
      })
    );
    const { store } = renderWithProviders(<AiAccountsTab />, {
      preloadedState: twoOrgs
    });
    const linkButton = await screen.findByRole('button', {
      name: /link a chatgpt account/i
    });
    await userEvent.click(linkButton);
    await waitFor(() => expect(linkButton).toBeDisabled());
    act(() => {
      store.dispatch(setCurrentOrg('org-b'));
    });
    answer();
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /link a chatgpt account/i })
      ).toBeEnabled()
    );
    expect(
      screen.queryByRole('button', { name: /open openai/i })
    ).not.toBeInTheDocument();
  });
});
