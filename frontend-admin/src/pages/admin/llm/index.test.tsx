import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'react-toastify';
import {
  renderWithProviders,
  waitForQuerySettled,
  type TestRootState
} from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import tenantReducer from 'store/slices/tenantSlice';
import LlmAdminPage from './index';

vi.mock('react-toastify', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));
// Permission gating is the consumer branching under test, so useAuth is
// mocked (precedent: components/navbar/top/NineDotMenu.test.tsx).
const mockedUseAuth = vi.fn();
vi.mock('hooks/auth/useAuthRTK', () => ({ useAuth: () => mockedUseAuth() }));

const ALL = [
  'llm.admin.read',
  'llm.credentials.admin',
  'llm.models.admin',
  'llm.grants.admin'
];
const authWith = (permissions: string[]) => ({
  hasPermission: (p: string) => permissions.includes(p)
});

const credential = {
  uuid: 'c-1',
  name: 'OpenAI prod',
  provider: 'openai',
  status: 'active',
  hasSecret: true,
  secretLast4: 'cdef',
  createdBy: 'u',
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z'
};
const ollamaCredential = {
  ...credential,
  uuid: 'c-2',
  name: 'Local ollama',
  provider: 'ollama',
  baseUrl: 'http://ollama:11434',
  hasSecret: false,
  secretLast4: undefined
};
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
  grants: [
    {
      uuid: 'g-1',
      modelUuid: 'm-1',
      userUuid: 'u-1',
      grantedBy: 'u',
      createdAt: '2026-10-01T00:00:00Z'
    }
  ]
};
const members = {
  members: [
    {
      id: 'ms-1',
      userUUID: 'u-1',
      tenantId: 't-1',
      roles: ['member'],
      isOwner: false,
      joinedAt: '2026-10-01T00:00:00Z',
      email: 'ada@example.com'
    },
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

type Body = Record<string, unknown>;
const sent: { method: string; path: string; body: Body | null }[] = [];

function stubReads({
  credentials = [credential],
  models = [model],
  allowHosted = 'true'
}: {
  credentials?: unknown[];
  models?: unknown[];
  allowHosted?: string;
} = {}) {
  server.use(
    http.get(url('/v1/admin/llm/credentials'), () =>
      HttpResponse.json({ items: credentials })
    ),
    http.get(url('/v1/admin/llm/models'), () =>
      HttpResponse.json({ items: models })
    ),
    http.get(url('/v1/admin/modules/llm'), () =>
      HttpResponse.json({
        name: 'llm',
        configValues: { allow_hosted: allowHosted }
      })
    ),
    http.get(url('/v1/admin/tenants/t-1/members'), () =>
      HttpResponse.json(members)
    )
  );
}

// Records every mutation's method, path and JSON body, replying with reply.
function captureWrite(
  method: 'post' | 'patch' | 'put' | 'delete',
  path: string,
  reply: () => Response = () => HttpResponse.json({})
) {
  server.use(
    http[method](url(path), async ({ request }) => {
      const text = await request.text();
      sent.push({
        method: request.method,
        path: new URL(request.url).pathname,
        body: text ? (JSON.parse(text) as Body) : null
      });
      return reply();
    })
  );
}

const preloadedState = {
  tenant: { ...tenantReducer(undefined, { type: 'init' }), currentOrgId: 't-1' }
} as Partial<TestRootState>;

const renderPage = (entry: string) =>
  renderWithProviders(<LlmAdminPage />, {
    routerEntries: [entry],
    preloadedState
  });

const rowOf = async (text: string) => {
  const cell = await screen.findByText(text);
  const row = cell.closest('tr');
  if (!row) throw new Error(`no table row holds ${text}`);
  return row as HTMLElement;
};

beforeEach(() => {
  sent.length = 0;
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  vi.mocked(toast.warning).mockClear();
  mockedUseAuth.mockReturnValue(authWith(ALL));
});

describe('LlmAdminPage — tabs', () => {
  it('opens on the models tab and shows the configured model with its grant count', async () => {
    stubReads();
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    expect(within(row).getByText(/gpt-5\.6-terra/)).toBeInTheDocument();
    expect(within(row).getByTestId('llm-grant-count')).toHaveTextContent('1');
  });

  it('switches to the credentials tab through the URL and never renders a secret', async () => {
    stubReads();
    renderPage('/admin/llm?tab=credentials');
    const row = await rowOf('OpenAI prod');
    expect(within(row).getByText(/cdef/)).toBeInTheDocument();
    expect(screen.queryByText(/sk-live/)).not.toBeInTheDocument();
  });

  it('falls back to the models tab on an unknown tab value', async () => {
    stubReads();
    renderPage('/admin/llm?tab=nope');
    expect(await screen.findByText('Fast')).toBeInTheDocument();
  });

  it('shows no tabs to a caller without llm.admin.read', async () => {
    mockedUseAuth.mockReturnValue(authWith([]));
    renderPage('/admin/llm');
    expect(
      await screen.findByText(/you do not have access to the language models/i)
    ).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
  });

  it('shows a retryable error state when the list fails', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/admin/llm/credentials'), () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ code: 'llm.internal' }, { status: 500 })
          : HttpResponse.json({ items: [credential] });
      })
    );
    renderPage('/admin/llm?tab=credentials');
    await userEvent.click(
      await screen.findByRole('button', { name: /retry/i })
    );
    expect(await screen.findByText('OpenAI prod')).toBeInTheDocument();
  });
});

describe('LlmAdminPage — credentials', () => {
  it('opens the credential editor from the empty state CTA', async () => {
    stubReads({ credentials: [], models: [] });
    renderPage('/admin/llm?tab=credentials');
    await userEvent.click(
      await screen.findByRole('button', { name: /add credential/i })
    );
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
  });

  it.each([
    ['1', false],
    ['yes', false],
    ['false', true],
    ['', true]
  ])(
    'reads allow_hosted=%j like the backend (warning shown: %s)',
    async (allowHosted, warned) => {
      stubReads({ credentials: [], models: [], allowHosted });
      const { store } = renderPage('/admin/llm?tab=credentials');
      await userEvent.click(
        await screen.findByRole('button', { name: /add credential/i })
      );
      const dialog = await screen.findByRole('dialog');
      // Anchor on the config query, not the DOM: without the warning the
      // tree is identical before and after it lands.
      await waitForQuerySettled(store, 'getModule');
      expect(
        within(dialog).queryByText(/hosted providers are disabled/i) !== null
      ).toBe(warned);
    }
  );

  it('creates an ollama credential on a compose-internal endpoint', async () => {
    stubReads({ credentials: [], models: [] });
    captureWrite('post', '/v1/admin/llm/credentials', () =>
      HttpResponse.json(ollamaCredential, { status: 201 })
    );
    renderPage('/admin/llm?tab=credentials');
    await userEvent.click(
      await screen.findByRole('button', { name: /add credential/i })
    );
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Local ollama');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Provider'),
      'ollama'
    );
    await userEvent.type(
      within(dialog).getByLabelText('Base URL'),
      'http://ollama:11434'
    );
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({
      name: 'Local ollama',
      provider: 'ollama',
      baseUrl: 'http://ollama:11434'
    });
  });

  it('rejects a base URL that is not http or https', async () => {
    stubReads({ credentials: [], models: [] });
    renderPage('/admin/llm?tab=credentials');
    await userEvent.click(
      await screen.findByRole('button', { name: /add credential/i })
    );
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Local');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Provider'),
      'ollama'
    );
    await userEvent.type(
      within(dialog).getByLabelText('Base URL'),
      'ftp://ollama:11434'
    );
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(
      await within(dialog).findByText(/enter an http or https url/i)
    ).toBeInTheDocument();
    expect(sent).toHaveLength(0);
  });

  it('submits an edit with only the changed name, never the locked provider', async () => {
    stubReads();
    captureWrite('patch', '/v1/admin/llm/credentials/c-1', () =>
      HttpResponse.json({ ...credential, name: 'OpenAI main' })
    );
    renderPage('/admin/llm?tab=credentials');
    const row = await rowOf('OpenAI prod');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    const name = within(dialog).getByLabelText('Name');
    await userEvent.clear(name);
    await userEvent.type(name, 'OpenAI main');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({ name: 'OpenAI main' });
    expect(toast.success).toHaveBeenCalled();
  });

  it('disables a credential through the PATCH status', async () => {
    stubReads();
    captureWrite('patch', '/v1/admin/llm/credentials/c-1', () =>
      HttpResponse.json({ ...credential, status: 'disabled' })
    );
    renderPage('/admin/llm?tab=credentials');
    const row = await rowOf('OpenAI prod');
    await userEvent.click(within(row).getByRole('button', { name: 'Disable' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({ status: 'disabled' });
  });

  it('names the credential in the delete confirmation and returns focus on cancel', async () => {
    stubReads();
    renderPage('/admin/llm?tab=credentials');
    const row = await rowOf('OpenAI prod');
    const trigger = within(row).getByRole('button', { name: 'Delete' });
    await userEvent.click(trigger);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/OpenAI prod/)).toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole('button', { name: 'Cancel' })
    );
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    );
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(sent).toHaveLength(0);
  });

  it('explains a delete refused because a model still uses the credential', async () => {
    stubReads();
    captureWrite('delete', '/v1/admin/llm/credentials/c-1', () =>
      HttpResponse.json(
        { code: 'llm.credential_in_use', detail: 'in use' },
        { status: 409 }
      )
    );
    renderPage('/admin/llm?tab=credentials');
    const row = await rowOf('OpenAI prod');
    await userEvent.click(within(row).getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(
      within(dialog).getByRole('button', { name: 'Delete' })
    );
    expect(
      await within(dialog).findByText(
        /an active model still uses this credential/i
      )
    ).toBeInTheDocument();
  });

  it('disables every mutation without llm.credentials.admin', async () => {
    mockedUseAuth.mockReturnValue(authWith(['llm.admin.read']));
    stubReads();
    renderPage('/admin/llm?tab=credentials');
    const row = await rowOf('OpenAI prod');
    for (const name of ['Edit', 'Rotate key', 'Disable', 'Delete']) {
      expect(within(row).getByRole('button', { name })).toBeDisabled();
    }
    expect(
      screen.getByRole('button', { name: /add credential/i })
    ).toBeDisabled();
  });
});

describe('LlmAdminPage — models', () => {
  it('creates a user_account model without embeddings or sampling defaults', async () => {
    stubReads();
    captureWrite('post', '/v1/admin/llm/models', () =>
      HttpResponse.json({ ...model, uuid: 'm-2', grants: [] }, { status: 201 })
    );
    renderPage('/admin/llm');
    await screen.findByText('Fast');
    await userEvent.click(screen.getByRole('button', { name: /add model/i }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Plan');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Billed to'),
      'user_account'
    );
    await userEvent.type(within(dialog).getByLabelText('Model ID'), 'gpt-5.5');
    expect(within(dialog).getByLabelText('Embeddings')).toBeDisabled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    const body = sent[0].body as Body;
    expect(body.credentialRef).toEqual({ kind: 'user_account' });
    expect(body.capabilities).toMatchObject({ embeddings: false });
    expect(body.defaults).toEqual({ effort: '' });
    expect(toast.success).toHaveBeenCalled();
  });

  it('patches only the fields that changed', async () => {
    stubReads();
    captureWrite('patch', '/v1/admin/llm/models/m-1', () =>
      HttpResponse.json({ ...model, name: 'Faster' })
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    const name = within(dialog).getByLabelText('Name');
    await userEvent.clear(name);
    await userEvent.type(name, 'Faster');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({
      method: 'PATCH',
      body: { name: 'Faster' }
    });
  });

  it('labels grantees with their email and replaces the grant list', async () => {
    stubReads();
    captureWrite('put', '/v1/admin/llm/models/m-1/grants', () =>
      HttpResponse.json(model)
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(
      within(row).getByRole('button', { name: 'Who may use it' })
    );
    const dialog = await screen.findByRole('dialog');
    expect(
      await within(dialog).findByText('ada@example.com')
    ).toBeInTheDocument();
    expect(within(dialog).queryByText('u-1')).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({ access: 'granted', userUuids: ['u-1'] });
  });

  it('opens a model to everyone through the grants route, keeping its grants', async () => {
    stubReads();
    captureWrite('put', '/v1/admin/llm/models/m-1/grants', () =>
      HttpResponse.json({ ...model, access: 'everyone' })
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(
      within(row).getByRole('button', { name: 'Who may use it' })
    );
    const dialog = await screen.findByRole('dialog');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Access'),
      'everyone'
    );
    expect(
      within(dialog).queryByLabelText('Granted users')
    ).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({
      method: 'PUT',
      body: { access: 'everyone', userUuids: ['u-1'] }
    });
  });

  it('saves an access change from the model editor on the grants route, never in the model PATCH', async () => {
    stubReads();
    captureWrite('patch', '/v1/admin/llm/models/m-1');
    captureWrite('put', '/v1/admin/llm/models/m-1/grants', () =>
      HttpResponse.json({ ...model, access: 'everyone' })
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Access'),
      'everyone'
    );
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({
      method: 'PUT',
      path: '/v1/admin/llm/models/m-1/grants',
      body: { access: 'everyone', userUuids: ['u-1'] }
    });
    expect(toast.success).toHaveBeenCalled();
  });

  it('creates a model without access, then sets access on the grants route', async () => {
    stubReads();
    captureWrite('post', '/v1/admin/llm/models', () =>
      HttpResponse.json({ ...model, uuid: 'm-2', grants: [] }, { status: 201 })
    );
    captureWrite('put', '/v1/admin/llm/models/m-2/grants', () =>
      HttpResponse.json({
        ...model,
        uuid: 'm-2',
        access: 'everyone',
        grants: []
      })
    );
    renderPage('/admin/llm');
    await screen.findByText('Fast');
    await userEvent.click(screen.getByRole('button', { name: /add model/i }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Open');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Credential'),
      'c-1'
    );
    await userEvent.type(within(dialog).getByLabelText('Model ID'), 'gpt-5.5');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Access'),
      'everyone'
    );
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[0].method).toBe('POST');
    expect(sent[0].body).not.toHaveProperty('access');
    expect(sent[1]).toMatchObject({
      method: 'PUT',
      body: { access: 'everyone', userUuids: [] }
    });
  });

  it('shows the written detail of a rejected model instead of a generic line', async () => {
    stubReads();
    captureWrite('patch', '/v1/admin/llm/models/m-1', () =>
      HttpResponse.json(
        {
          code: 'llm.invalid_request',
          detail:
            'The credential belongs to a different provider than the model.'
        },
        { status: 422 }
      )
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    const name = within(dialog).getByLabelText('Name');
    await userEvent.clear(name);
    await userEvent.type(name, 'Faster');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(
      await within(dialog).findByText(
        'The credential belongs to a different provider than the model.'
      )
    ).toBeInTheDocument();
  });

  it('disables and enables a model through the PATCH status', async () => {
    stubReads({
      models: [
        model,
        { ...model, uuid: 'm-9', name: 'Old', status: 'disabled', grants: [] }
      ]
    });
    captureWrite('patch', '/v1/admin/llm/models/m-1', () =>
      HttpResponse.json({ ...model, status: 'disabled' })
    );
    captureWrite('patch', '/v1/admin/llm/models/m-9', () =>
      HttpResponse.json({ ...model, uuid: 'm-9', status: 'active' })
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Disable' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({
      method: 'PATCH',
      path: '/v1/admin/llm/models/m-1',
      body: { status: 'disabled' }
    });
    const old = await rowOf('Old');
    await userEvent.click(within(old).getByRole('button', { name: 'Enable' }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1].body).toEqual({ status: 'active' });
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2));
  });

  it('says access and grants were not saved when the model is created but the grant PUT fails', async () => {
    stubReads();
    captureWrite('post', '/v1/admin/llm/models', () =>
      HttpResponse.json({ ...model, uuid: 'm-2', grants: [] }, { status: 201 })
    );
    captureWrite('put', '/v1/admin/llm/models/m-2/grants', () =>
      HttpResponse.json(
        { code: 'llm.grant_not_member', detail: 'not a member' },
        { status: 422 }
      )
    );
    renderPage('/admin/llm');
    await screen.findByText('Fast');
    await userEvent.click(screen.getByRole('button', { name: /add model/i }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Second');
    await userEvent.selectOptions(
      within(dialog).getByLabelText('Credential'),
      'c-1'
    );
    await userEvent.type(within(dialog).getByLabelText('Model ID'), 'gpt-5.5');
    const picker = within(dialog).getByLabelText('Granted users');
    await userEvent.click(picker);
    await userEvent.click(await screen.findByText('grace@example.com'));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1].body).toEqual({ access: 'granted', userUuids: ['u-2'] });
    await waitFor(() => expect(toast.warning).toHaveBeenCalled());
    const warning = vi.mocked(toast.warning).mock.calls[0][0] as string;
    expect(warning).toMatch(/access and grants were not saved/i);
    // The reason travels with it (errors.llm.grant_not_member).
    expect(warning).toMatch(/must be a member of this organization/i);
  });

  it('never calls a grantee a former member when the member list was not loaded', async () => {
    mockedUseAuth.mockReturnValue(
      authWith(['llm.admin.read', 'llm.models.admin'])
    );
    stubReads();
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText('Granted users')).toBeDisabled();
    expect(within(dialog).getByText('u-1')).toBeInTheDocument();
    expect(
      within(dialog).queryByText(/no longer a member/i)
    ).not.toBeInTheDocument();
  });

  it('explains a refused member list and keeps current grantees visible', async () => {
    stubReads();
    server.use(
      http.get(url('/v1/admin/tenants/t-1/members'), () =>
        HttpResponse.json({ detail: 'forbidden' }, { status: 403 })
      )
    );
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    expect(
      await within(dialog).findByText(/needs tenant-admin access/i)
    ).toBeInTheDocument();
    expect(within(dialog).getByText('u-1')).toBeInTheDocument();
    expect(
      within(dialog).queryByText(/no longer a member/i)
    ).not.toBeInTheDocument();
  });

  it('hides model mutations behind their own permissions', async () => {
    mockedUseAuth.mockReturnValue(
      authWith(['llm.admin.read', 'llm.grants.admin'])
    );
    stubReads();
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    expect(within(row).getByRole('button', { name: 'Edit' })).toBeDisabled();
    expect(within(row).getByRole('button', { name: 'Delete' })).toBeDisabled();
    expect(within(row).getByRole('button', { name: 'Disable' })).toBeDisabled();
    expect(
      within(row).getByRole('button', { name: 'Who may use it' })
    ).toBeEnabled();
    expect(screen.getByRole('button', { name: /add model/i })).toBeDisabled();
  });

  it('keeps access read-only in the model editor without llm.grants.admin', async () => {
    mockedUseAuth.mockReturnValue(
      authWith(['llm.admin.read', 'llm.models.admin'])
    );
    stubReads();
    renderPage('/admin/llm');
    const row = await rowOf('Fast');
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText('Access')).toBeDisabled();
  });
});
