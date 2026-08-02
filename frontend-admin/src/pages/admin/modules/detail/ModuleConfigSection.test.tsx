import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleConfigSection from './ModuleConfigSection';

// The `availableEnvironments` fixtures below are all empty, which skips
// useGetModuleEnvironmentQuery (see ModuleConfigSection.tsx) — no HTTP
// request fires, so no MSW handler (and no `server`/`msw` import) is
// needed here. If a future fixture sets availableEnvironments, add:
//   import { http, HttpResponse } from 'msw';
//   import { server } from 'test/server';
//   server.use(http.get('*/v1/admin/modules/:name/environments/:env', () =>
//     HttpResponse.json({ environment: 'production', configValues: {}, secretStatus: {}, updatedAt: '' })
//   ));

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

  it('keeps the flat form when the module declares no groups', () => {
    // The degradation path — every module served today, and every un-migrated
    // fork addon, takes it.
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha', group: 'Google' }),
      field({ key: 'b', label: 'Beta', group: 'Apple' })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByRole('button', { name: 'Google' })).toBeInTheDocument();
    expect(screen.getByText('Alpha')).toBeInTheDocument();
  });
});
