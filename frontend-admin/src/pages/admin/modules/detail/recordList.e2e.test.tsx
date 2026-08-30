import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Route, Routes } from 'react-router';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import type { ModuleConfig } from 'store/api/moduleApi';
import ModuleDetailPage from './index';

// Same reason as index.test.tsx: useBlocker needs a data router.
vi.mock('react-router', async importOriginal => {
  const actual = await importOriginal<typeof import('react-router')>();
  return { ...actual, useBlocker: () => ({ state: 'unblocked' as const }) };
});

const listModule = {
  moduleName: 'demo',
  displayName: 'Demo',
  description: '',
  category: 'toggleable',
  enabled: true,
  status: 'running',
  needsRestart: false,
  configValues: {},
  secretStatus: {},
  configSchema: [
    {
      key: 'email.profiles',
      label: 'Delivery profiles',
      description: '',
      type: 'recordList',
      required: false,
      default: '',
      envVar: '',
      items: [
        { key: 'host', label: 'Host', type: 'string', required: false },
        { key: 'password', label: 'Password', type: 'secret', required: false }
      ]
    }
  ],
  dependsOn: [],
  providedServices: [],
  requiredServices: [],
  optionalServices: [],
  activeEnvironment: 'production',
  availableEnvironments: ['production', 'sandbox'],
  createdAt: '',
  updatedAt: ''
} as unknown as ModuleConfig;

let lastPatch: Record<string, unknown> | null = null;

const stub = (
  configValues: Record<string, string>,
  secretStatus: Record<string, boolean> = {},
  revision = 3
) => {
  lastPatch = null;
  server.use(
    http.get('*/v1/admin/modules', () =>
      HttpResponse.json({ modules: [listModule] })
    ),
    http.get('*/v1/admin/modules/health', () =>
      HttpResponse.json({
        modules: [{ moduleName: 'demo', status: 'healthy' }]
      })
    ),
    http.get('*/v1/admin/modules/:name', () => HttpResponse.json(listModule)),
    http.get('*/v1/admin/modules/:name/environments/:env', () =>
      HttpResponse.json({
        environment: 'production',
        configValues,
        secretStatus,
        updatedAt: '',
        revision
      })
    ),
    http.patch(
      '*/v1/admin/modules/:name/environments/:env',
      async ({ request }) => {
        lastPatch = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          environment: 'production',
          configValues,
          secretStatus,
          updatedAt: '',
          revision: revision + 1
        });
      }
    )
  );
};

const render = () =>
  renderWithProviders(
    <Routes>
      <Route path="/admin/modules/:moduleName" element={<ModuleDetailPage />} />
    </Routes>,
    { routerEntries: ['/admin/modules/demo'] }
  );

describe('record lists on the module detail page', () => {
  beforeEach(() => stub({}));

  it('renders a card per stored element with its own fields', async () => {
    stub({
      'email.profiles.__items': 'primary,backup',
      'email.profiles.primary.__label': 'Primary',
      'email.profiles.primary.host': 'smtp.primary',
      'email.profiles.backup.__label': 'Backup'
    });
    render();
    expect(await screen.findByText('Primary')).toBeInTheDocument();
    expect(screen.getByText('Backup')).toBeInTheDocument();
    expect(screen.getByDisplayValue('smtp.primary')).toBeInTheDocument();
  });

  it('adds an element and sends it as explicit intent, with no revision', async () => {
    const user = userEvent.setup();
    render();

    await user.click(await screen.findByRole('button', { name: /add/i }));
    await user.type(screen.getByLabelText(/^name$/i), 'MailUp SMTP');
    await user.click(screen.getByRole('button', { name: /^confirm$/i }));

    // The new element's own fields are on screen immediately — the roster the
    // schema expands against includes unsaved additions.
    expect(await screen.findByText('mailup-smtp')).toBeInTheDocument();

    await user.click(await screen.findByRole('button', { name: /^save/i }));
    await waitFor(() => expect(lastPatch).not.toBeNull());
    expect(lastPatch?.recordLists).toEqual([
      { field: 'email.profiles', create: ['mailup-smtp'], remove: [] }
    ]);
    expect(lastPatch).not.toHaveProperty('revision');
    expect(lastPatch?.config).toMatchObject({
      'email.profiles.mailup-smtp.__label': 'MailUp SMTP'
    });
  });

  it('stages a removal, confirms it at save, and sends the fetched revision', async () => {
    const user = userEvent.setup();
    stub(
      {
        'email.profiles.__items': 'primary',
        'email.profiles.primary.__label': 'Primary'
      },
      { 'email.profiles.primary.password': true },
      7
    );
    render();

    await user.click(await screen.findByRole('button', { name: /remove/i }));
    // Staged, not gone: still on screen with an Undo.
    expect(screen.getByText('Primary')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /undo/i })).toBeInTheDocument();
    expect(lastPatch).toBeNull();

    await user.click(await screen.findByRole('button', { name: /^save/i }));
    // The destructive save asks first, and names what it will destroy.
    expect(await screen.findByText(/delete on save\?/i)).toBeInTheDocument();
    const dialog = screen.getByRole('dialog');
    // Named, not counted — "delete 1 entry" is not a question anyone can answer.
    expect(within(dialog).getByText(/Primary/)).toBeInTheDocument();
    expect(lastPatch).toBeNull();

    await user.click(within(dialog).getByRole('button', { name: /^remove$/i }));
    await waitFor(() => expect(lastPatch).not.toBeNull());
    expect(lastPatch?.recordLists).toEqual([
      { field: 'email.profiles', create: [], remove: ['primary'] }
    ]);
    expect(lastPatch?.revision).toBe(7);
  });

  it('undoing a staged removal disarms the save', async () => {
    const user = userEvent.setup();
    stub({
      'email.profiles.__items': 'primary',
      'email.profiles.primary.__label': 'Primary'
    });
    render();

    await user.click(await screen.findByRole('button', { name: /remove/i }));
    await user.click(await screen.findByRole('button', { name: /undo/i }));
    expect(
      screen.queryByRole('button', { name: /undo/i })
    ).not.toBeInTheDocument();
    expect(lastPatch).toBeNull();
  });

  it('Reload & review disarms a staged removal even when the profile revision is unchanged', async () => {
    const user = userEvent.setup();
    stub(
      {
        'email.profiles.__items': 'primary',
        'email.profiles.primary.__label': 'Primary'
      },
      {},
      7
    );
    // The save lost its compare-and-swap because configRevision moved (an
    // activation, or another profile's write); THIS profile is still at 7,
    // so the reload returns identical data and the re-seed effect never runs.
    server.use(
      http.patch('*/v1/admin/modules/:name/environments/:env', () =>
        HttpResponse.json(
          {
            status: 409,
            title: 'Conflict',
            detail: 'moved',
            code: 'module.config_revision_stale'
          },
          { status: 409 }
        )
      )
    );
    render();

    await user.click(await screen.findByRole('button', { name: /remove/i }));
    await user.click(await screen.findByRole('button', { name: /^save/i }));
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /^remove$/i }));

    await user.click(
      await screen.findByRole('button', { name: 'Reload & review' })
    );
    // The removal is no longer staged: no Undo, no "deleted on save" notice,
    // the element is back to an ordinary card.
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: /undo/i })
      ).not.toBeInTheDocument()
    );
    expect(screen.getByText('Primary')).toBeInTheDocument();
    expect(
      screen.queryByText(/will be deleted on save/i)
    ).not.toBeInTheDocument();
  });
});
