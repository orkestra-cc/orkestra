import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { renderWithProviders } from 'test/render';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleConfigSection from './ModuleConfigSection';

// The `availableEnvironments` fixtures below are all empty, which skips
// useGetModuleEnvironmentQuery (see ModuleConfigSection.tsx) — no HTTP
// request fires for it, so no MSW handler is needed for that GET in most of
// these tests. The one test that actually clicks Save still needs a handler
// for the PATCH mutation, which fires regardless of `skip` — see that test.

// ModuleConfigSection calls useBlocker to guard unsaved-changes navigation.
// useBlocker requires a *data* router (createBrowserRouter/createMemoryRouter
// + RouterProvider) — it throws under the declarative <MemoryRouter> that
// `renderWithProviders` uses for every other test in the suite. Blocking
// behavior itself isn't what these tests exercise, so stub just that hook
// and leave the rest of the module (MemoryRouter, etc.) real.
vi.mock('react-router', async importOriginal => {
  const actual = await importOriginal<typeof import('react-router')>();
  return {
    ...actual,
    useBlocker: () => ({ state: 'unblocked' as const })
  };
});

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

const moduleWith = (
  schema: ConfigField[],
  extra: Partial<ModuleConfig> = {}
): ModuleConfig =>
  ({
    moduleName: 'demo',
    displayName: 'Demo',
    description: '',
    category: 'toggleable',
    enabled: true,
    status: 'running',
    needsRestart: false,
    configValues: {},
    secretStatus: {},
    configSchema: schema,
    dependsOn: [],
    providedServices: [],
    requiredServices: [],
    optionalServices: [],
    activeEnvironment: 'production',
    availableEnvironments: [],
    createdAt: '',
    updatedAt: '',
    ...extra
  }) as ModuleConfig;

describe('ModuleConfigSection', () => {
  it('renders legacy group labels as tabs, unchanged', () => {
    // Today's shape: no configGroups, `group` is a display label. This is the
    // regression guard for "no visual change".
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha', group: 'Google' }),
      field({ key: 'b', label: 'Beta', group: 'Apple' })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByRole('button', { name: 'Google' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Apple' })).toBeInTheDocument();
    expect(screen.getByText('Alpha')).toBeInTheDocument();
  });

  it('keeps every tab keyboard-reachable, including the inactive one', () => {
    // The headers are plain <Nav.Link>s (Anchor without href → role="button",
    // tabIndex 0). Declaring role="tablist" on the <Nav> would make
    // @restart/ui apply a roving tabIndex=-1 to every inactive one — correct
    // only when paired with the arrow-key handler Nav.js wires up inside a
    // <Tab.Container>, which this bare <Nav> is not. That combination left
    // the inactive tab unreachable by keyboard/screen-reader (no sequential
    // Tab, no arrow keys). Regression guard for that: every tab header must
    // stay at tabIndex 0, not just the active one.
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha', group: 'Google' }),
      field({ key: 'b', label: 'Beta', group: 'Apple' })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    const tabs = ['Google', 'Apple'].map(name =>
      screen.getByRole('button', { name })
    );
    expect(tabs).toHaveLength(2);
    for (const tab of tabs) {
      expect(tab.tabIndex).toBe(0);
    }
  });

  it('hides a field whose condition is unmet and shows it once met', () => {
    const schema = [
      field({ key: 'on', label: 'Enabled', type: 'bool', default: 'false' }),
      field({
        key: 'secretish',
        label: 'Client ID',
        dependsOn: [{ key: 'on', in: ['true'] }]
      })
    ];
    const { rerender } = renderWithProviders(
      <ModuleConfigSection
        module={moduleWith(schema)}
        selectedEnvironment="production"
      />
    );
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();

    rerender(
      <ModuleConfigSection
        module={moduleWith(schema, { configValues: { on: 'true' } })}
        selectedEnvironment="production"
      />
    );
    expect(screen.getByText('Client ID')).toBeInTheDocument();
  });

  it('renders a declared group tree', () => {
    const mod = moduleWith(
      [
        field({ key: 'x', label: 'Toggle', group: 'oauth' }),
        field({ key: 'y', label: 'Client ID', group: 'oauth.google' })
      ],
      {
        configGroups: [
          { key: 'oauth', label: 'OAuth Providers', order: 1 },
          { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(
      screen.getByRole('button', { name: 'OAuth Providers' })
    ).toBeInTheDocument();
  });

  it('renders a vertical rail with nested groups and switches panel on click', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'toggle', label: 'Enable Google', group: 'oauth' }),
        field({ key: 'clientId', label: 'Client ID', group: 'oauth.google' }),
        field({ key: 'minLen', label: 'Minimum length', group: 'password' })
      ],
      {
        configGroups: [
          { key: 'oauth', label: 'OAuth Providers', order: 1 },
          { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
          { key: 'password', label: 'Password Policy', order: 3 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    // Every group, including the nested child, is reachable from the rail.
    expect(
      screen.getByRole('button', { name: 'OAuth Providers' })
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Google' })).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Password Policy' })
    ).toBeInTheDocument();

    // First node selected by default.
    expect(screen.getByText('Enable Google')).toBeInTheDocument();
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Google' }));
    expect(screen.getByText('Client ID')).toBeInTheDocument();
    expect(screen.queryByText('Enable Google')).not.toBeInTheDocument();
  });

  it('collapses advanced fields behind a toggle', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'plain', label: 'Plain', group: 'g1' }),
        field({ key: 'rare', label: 'Rare', group: 'g1', advanced: true })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByText('Plain')).toBeInTheDocument();
    expect(screen.queryByText('Rare')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Advanced \(1\)/ }));
    expect(screen.getByText('Rare')).toBeInTheDocument();
  });

  it('keeps the true flat form when there is only one settings bucket', () => {
    // The actual degradation path — fewer than 2 top-level nodes. Every
    // module served today, and every un-migrated fork addon, has either no
    // `group` at all (synthesized into a single trailing "General" bucket,
    // exercised here) or fields that all share one `group` label, and both
    // must render exactly like the pre-rail flat form: fields directly, no
    // rail, no group button anywhere.
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha' }),
      field({ key: 'b', label: 'Beta' })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByText('Alpha')).toBeInTheDocument();
    expect(screen.getByText('Beta')).toBeInTheDocument();
    // "General" is the label buildGroupTree synthesizes for the lone bucket
    // — asserting it never renders as a button proves no rail was drawn at
    // all (as opposed to the Save/Discard buttons, which always render).
    expect(
      screen.queryByRole('button', { name: 'General' })
    ).not.toBeInTheDocument();
  });

  it('renders no Advanced toggle when the only advanced field is hidden, and shows it once visible', async () => {
    const user = userEvent.setup();
    const schema = [
      field({ key: 'on', label: 'Enabled', type: 'bool', default: 'false' }),
      field({
        key: 'rare',
        label: 'Rare',
        group: 'g1',
        advanced: true,
        dependsOn: [{ key: 'on', in: ['true'] }]
      }),
      field({ key: 'plain', label: 'Plain', group: 'g1' })
    ];
    const mod = moduleWith(schema, {
      configGroups: [
        { key: 'g1', label: 'Group One', order: 1 },
        { key: 'g2', label: 'Group Two', order: 2 }
      ]
    });
    const { rerender } = renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(
      screen.queryByRole('button', { name: /Advanced/ })
    ).not.toBeInTheDocument();

    rerender(
      <ModuleConfigSection
        module={moduleWith(schema, {
          configGroups: mod.configGroups,
          configValues: { on: 'true' }
        })}
        selectedEnvironment="production"
      />
    );
    expect(
      screen.getByRole('button', { name: /Advanced \(1\)/ })
    ).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /Advanced \(1\)/ }));
    expect(screen.getByText('Rare')).toBeInTheDocument();
  });

  it('accumulates unsaved changes across two different groups', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'a', label: 'Alpha', group: 'g1' }),
        field({ key: 'b', label: 'Beta', group: 'g2' })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.type(screen.getByLabelText('Alpha'), 'x');
    await user.click(screen.getByRole('button', { name: 'Group Two' }));
    await user.type(screen.getByLabelText('Beta'), 'y');

    // One bar, both groups counted — this is what the per-card form could not do.
    expect(await screen.findByText(/2 unsaved changes/)).toBeInTheDocument();
    expect(screen.getByText(/Group One \(1\)/)).toBeInTheDocument();
    expect(screen.getByText(/Group Two \(1\)/)).toBeInTheDocument();
  });

  it('surfaces an error from a section that is not on screen', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'n', label: 'Count', type: 'int', min: 8, group: 'g1' }),
        field({ key: 'b', label: 'Beta', group: 'g2' })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.clear(screen.getByLabelText('Count'));
    await user.type(screen.getByLabelText('Count'), '3');
    await user.click(screen.getByRole('button', { name: 'Group Two' }));

    // The bad value is in a section the operator is no longer looking at.
    // Without this, save fails with no indication of where.
    const link = await screen.findByRole('button', { name: /Go to Group One/ });
    await user.click(link);
    expect(screen.getByLabelText('Count')).toBeInTheDocument();
  });

  it('saves only the fields being edited, leaving an unrelated stored-empty required field alone', async () => {
    const user = userEvent.setup();
    // 'req' is required and already stored empty — the backend allows this
    // (UpdateConfig writes '' for a cleared field) and configCompleteness
    // exists to *report* that state, not to make the rest of the module
    // unsavable until someone fills it in.
    const mod = moduleWith(
      [
        field({ key: 'req', label: 'Required A', required: true, group: 'g1' }),
        field({ key: 'b', label: 'Beta', group: 'g2' })
      ],
      {
        configValues: { req: '', b: 'old' },
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );

    let capturedBody: unknown = null;
    server.use(
      http.patch(
        url('/v1/admin/modules/:name/environments/:env'),
        async ({ request }) => {
          capturedBody = await request.json();
          return HttpResponse.json({
            environment: 'production',
            configValues: { req: '', b: 'new' },
            secretStatus: {},
            updatedAt: ''
          });
        }
      )
    );

    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.click(screen.getByRole('button', { name: 'Group Two' }));
    await user.clear(screen.getByLabelText('Beta'));
    await user.type(screen.getByLabelText('Beta'), 'new');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));

    // Only the edited key travels — 'req' never got touched, so it's never
    // validated and never sent, even though it would fail its own
    // `required` rule if it were.
    await waitFor(() => expect(capturedBody).not.toBeNull());
    expect(capturedBody).toEqual({ config: { b: 'new' } });
  });

  it('clears a typed secret from the form immediately after a successful save', async () => {
    const user = userEvent.setup();
    const mod = moduleWith([
      field({ key: 's', label: 'API Key', type: 'secret' })
    ]);

    server.use(
      http.patch(url('/v1/admin/modules/:name/environments/:env'), async () =>
        HttpResponse.json({
          environment: 'production',
          configValues: {},
          secretStatus: { s: true },
          updatedAt: ''
        })
      )
    );

    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.type(screen.getByLabelText('API Key'), 'sekret');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));

    // Cleared synchronously off the mutation's own success — not left
    // showing plaintext in the DOM until the invalidated query refetches.
    await waitFor(() =>
      expect(screen.getByLabelText('API Key')).toHaveValue('')
    );
  });

  it('shows the aggregate error count but no dead "Go to" button or group chip on the flat degradation path', async () => {
    const user = userEvent.setup();
    // No configGroups and a single ungrouped field — the same shape as
    // "keeps the true flat form when there is only one settings bucket"
    // above, so `showRail` is false here too.
    const mod = moduleWith([
      field({ key: 'n', label: 'Count', type: 'int', min: 8 })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.clear(screen.getByLabelText('Count'));
    await user.type(screen.getByLabelText('Count'), '3');

    // The aggregate message is still useful...
    expect(
      await screen.findByText(/1 field needs attention/)
    ).toBeInTheDocument();
    // ...but with one implicit bucket, a per-group chip only restates it,
    // and a "Go to <group>" button has nowhere to navigate — the field is
    // already the only thing on screen.
    expect(
      screen.queryByRole('button', { name: /Go to/ })
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/General \(/)).not.toBeInTheDocument();
  });
});
