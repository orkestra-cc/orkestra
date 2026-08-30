import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { renderWithProviders } from 'test/render';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import { useModuleConfigController } from '../useModuleConfigController';
import ModuleConfigSection from './ModuleConfigSection';

// The `availableEnvironments` fixtures below are all empty, which skips
// useGetModuleEnvironmentQuery (see useModuleConfigController.ts) — no HTTP
// request fires for it, so no MSW handler is needed for that GET in most of
// these tests. The one test that actually clicks Save still needs a handler
// for the PATCH mutation, which fires regardless of `skip` — see that test.

// ModuleConfigSection no longer owns the form or the blocker — both now live
// in useModuleConfigController, shared with (and instantiated exactly once
// by) detail/index.tsx, which is also the only place that registers
// useBlocker (a router only supports one at a time — see index.tsx). This
// component test therefore doesn't need to touch react-router at all; a
// thin host renders the hook's result straight into the component under
// test, mirroring how detail/index.tsx wires the two together.
const TestHost: React.FC<{ mod: ModuleConfig }> = ({ mod }) => {
  const controller = useModuleConfigController(mod, 'production');
  return <ModuleConfigSection module={mod} controller={controller} />;
};

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
  // The controller validates the seeded values once on mount (see the
  // re-seed effect in useModuleConfigController) — an async state update that
  // lands after a synchronous render returns. The `findBy*` in each otherwise
  // synchronous test below is what flushes it inside act(), not an assertion
  // about timing.
  it('renders legacy group labels as tabs, unchanged', async () => {
    // Today's shape: no configGroups, `group` is a display label. This is the
    // regression guard for "no visual change".
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha', group: 'Google' }),
      field({ key: 'b', label: 'Beta', group: 'Apple' })
    ]);
    renderWithProviders(<TestHost mod={mod} />);
    expect(
      await screen.findByRole('button', { name: 'Google' })
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Apple' })).toBeInTheDocument();
    expect(screen.getByText('Alpha')).toBeInTheDocument();
  });

  it('keeps every tab keyboard-reachable, including the inactive one', async () => {
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
    renderWithProviders(<TestHost mod={mod} />);
    await screen.findByRole('button', { name: 'Google' });
    const tabs = ['Google', 'Apple'].map(name =>
      screen.getByRole('button', { name })
    );
    expect(tabs).toHaveLength(2);
    for (const tab of tabs) {
      expect(tab.tabIndex).toBe(0);
    }
  });

  it('hides a field whose condition is unmet and shows it once met', async () => {
    const schema = [
      field({ key: 'on', label: 'Enabled', type: 'bool', default: 'false' }),
      field({
        key: 'secretish',
        label: 'Client ID',
        dependsOn: [{ key: 'on', in: ['true'] }]
      })
    ];
    const { rerender } = renderWithProviders(
      <TestHost mod={moduleWith(schema)} />
    );
    await screen.findByLabelText('Enabled');
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();

    rerender(
      <TestHost mod={moduleWith(schema, { configValues: { on: 'true' } })} />
    );
    expect(await screen.findByText('Client ID')).toBeInTheDocument();
  });

  it('renders a declared group tree', async () => {
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
    renderWithProviders(<TestHost mod={mod} />);
    expect(
      await screen.findByRole('button', { name: 'OAuth Providers' })
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
    renderWithProviders(<TestHost mod={mod} />);

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
    renderWithProviders(<TestHost mod={mod} />);
    expect(screen.getByText('Plain')).toBeInTheDocument();
    expect(screen.queryByText('Rare')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Advanced \(1\)/ }));
    expect(screen.getByText('Rare')).toBeInTheDocument();
  });

  it('keeps the true flat form when there is only one settings bucket', async () => {
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
    renderWithProviders(<TestHost mod={mod} />);
    expect(await screen.findByText('Alpha')).toBeInTheDocument();
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
    const { rerender } = renderWithProviders(<TestHost mod={mod} />);
    expect(
      screen.queryByRole('button', { name: /Advanced/ })
    ).not.toBeInTheDocument();

    rerender(
      <TestHost
        mod={moduleWith(schema, {
          configGroups: mod.configGroups,
          configValues: { on: 'true' }
        })}
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
    // `email.smtp.host` deliberately: the per-group breakdown intersects
    // `GroupNode.fieldKeys` (schema keys) with the dirty set, and the dirty
    // set is derived from react-hook-form's register names. A dotted key is
    // the only shape that catches those two drifting apart.
    const mod = moduleWith(
      [
        field({ key: 'email.smtp.host', label: 'Alpha', group: 'g1' }),
        field({ key: 'b', label: 'Beta', group: 'g2' })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(<TestHost mod={mod} />);

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
    renderWithProviders(<TestHost mod={mod} />);

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

    renderWithProviders(<TestHost mod={mod} />);

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

  it('dirty-tracks and saves an edit to a dotted config key', async () => {
    // react-hook-form reads "." in a field name as a path separator, so
    // registering `email.smtp.host` verbatim wrote the operator's edit to
    // {email:{smtp:{host}}} while every consumer here reads the flat key.
    // The edit was then invisible to `dirtyFields` (which reported the
    // synthesized "email" branch), so the save bar never appeared, Save never
    // enabled, and `collectDiff` saw no change even when forced. That made
    // /admin/modules/notification (11 of 11 keys dotted) and
    // /admin/modules/tenant (2 of 2) impossible to configure at all. Every
    // fixture in this repo used dot-free keys, which is why it shipped.
    const user = userEvent.setup();
    const mod = moduleWith(
      [field({ key: 'email.smtp.host', label: 'SMTP host' })],
      { configValues: { 'email.smtp.host': 'old.example.com' } }
    );

    let capturedBody: unknown = null;
    server.use(
      http.patch(
        url('/v1/admin/modules/:name/environments/:env'),
        async ({ request }) => {
          capturedBody = await request.json();
          return HttpResponse.json({
            environment: 'production',
            configValues: { 'email.smtp.host': 'new.example.com' },
            secretStatus: {},
            updatedAt: ''
          });
        }
      )
    );

    renderWithProviders(<TestHost mod={mod} />);

    await user.clear(await screen.findByLabelText('SMTP host'));
    await user.type(screen.getByLabelText('SMTP host'), 'new.example.com');

    // The reported symptom: the field shows the new value but the bar never
    // counts it, so Save stays disabled forever.
    expect(await screen.findByText(/1 unsaved change/)).toBeInTheDocument();
    const save = screen.getByRole('button', { name: 'Save Changes' });
    expect(save).toBeEnabled();

    await user.click(save);

    // The payload must still be keyed by the backend's real schema key —
    // the register-name mapping is a form-layer detail and must not leak
    // into the API contract.
    await waitFor(() => expect(capturedBody).not.toBeNull());
    expect(capturedBody).toEqual({
      config: { 'email.smtp.host': 'new.example.com' }
    });
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

    renderWithProviders(<TestHost mod={mod} />);

    await user.type(screen.getByLabelText('API Key'), 'sekret');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));

    // Cleared synchronously off the mutation's own success — not left
    // showing plaintext in the DOM until the invalidated query refetches.
    await waitFor(() =>
      expect(screen.getByLabelText('API Key')).toHaveValue('')
    );
  });

  it('flags stored values that violate their declared rules with no interaction at all', async () => {
    // react-hook-form's `mode: 'onChange'` writes `formState.errors[name]`
    // only for the field that fired a change, and never validates on mount —
    // so a value the backend already stores in violation of its own declared
    // `min`/`required` rendered perfectly clean until the operator happened
    // to type in that exact field. Before the form migration these checks ran
    // inline on every render and were red on arrival; the controller's
    // seed-time `form.trigger()` is what restores that. Nothing below touches
    // the form.
    const mod = moduleWith(
      [
        field({ key: 'n', label: 'Count', type: 'int', min: 8 }),
        field({ key: 'req', label: 'Required A', required: true })
      ],
      { configValues: { n: '3', req: '' } }
    );
    renderWithProviders(<TestHost mod={mod} />);

    expect(await screen.findByText('Minimum is 8')).toBeInTheDocument();
    expect(screen.getByText('This field is required.')).toBeInTheDocument();
    expect(screen.getByLabelText('Count')).toHaveClass('is-invalid');
    // Regex: a required field's label carries a trailing "*" marker, so its
    // accessible name is "Required A*", not "Required A".
    expect(screen.getByLabelText(/Required A/)).toHaveClass('is-invalid');
  });

  it('shows the aggregate error count but no dead "Go to" button or group chip on the flat degradation path', async () => {
    const user = userEvent.setup();
    // No configGroups and a single ungrouped field — the same shape as
    // "keeps the true flat form when there is only one settings bucket"
    // above, so `showRail` is false here too.
    const mod = moduleWith([
      field({ key: 'n', label: 'Count', type: 'int', min: 8 })
    ]);
    renderWithProviders(<TestHost mod={mod} />);

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

describe('ModuleConfigSection revision conflict', () => {
  const envGet = (hits: { n: number }, configValues: Record<string, string>) =>
    http.get(url('/v1/admin/modules/:name/environments/:env'), () => {
      hits.n += 1;
      return HttpResponse.json({
        environment: 'production',
        configValues,
        secretStatus: {},
        updatedAt: '',
        revision: hits.n
      });
    });

  it('latches on module.config_revision_stale, re-applies only the dirty draft, and reloads on demand without retrying', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'a', label: 'Alpha' }),
        field({ key: 'b', label: 'Beta' }),
        field({ key: 'c', label: 'Gamma' }),
        field({ key: 's', label: 'API Key', type: 'secret' })
      ],
      { availableEnvironments: ['production'] }
    );
    const hits = { n: 0 };
    let patches = 0;
    server.use(
      envGet(hits, { a: 'server-old', b: 'b-old', c: 'c-old' }),
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () => {
        patches += 1;
        return HttpResponse.json(
          {
            status: 409,
            title: 'Conflict',
            detail: 'moved',
            code: 'module.config_revision_stale'
          },
          { status: 409 }
        );
      })
    );

    renderWithProviders(<TestHost mod={mod} />);
    const alpha = await screen.findByLabelText('Alpha');
    await waitFor(() => expect(alpha).toHaveValue('server-old'));
    await user.clear(alpha);
    await user.type(alpha, 'mine');
    await user.clear(screen.getByLabelText('Gamma')); // an intentional clear to ''
    await user.type(screen.getByLabelText('API Key'), 'unsent-secret');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));

    // The conflict copy, not the record-list one; Save disabled; Reload offered.
    expect(
      await screen.findByText(/changed this module's configuration/)
    ).toBeInTheDocument();
    expect(screen.queryByText(/changed this list/)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled();
    expect(patches).toBe(1);

    // Meanwhile the other operator changed a, b (untouched here) and c
    // (cleared here). Reload adopts their baseline and re-applies ONLY the
    // fields this operator touched.
    server.use(envGet(hits, { a: 'server-new', b: 'b-new', c: 'c-new' }));
    await user.click(screen.getByRole('button', { name: 'Reload & review' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
    );
    expect(screen.getByLabelText('Alpha')).toHaveValue('mine'); // dirty: re-applied
    expect(screen.getByLabelText('Beta')).toHaveValue('b-new'); // untouched: theirs, NOT reverted to b-old
    expect(screen.getByLabelText('Gamma')).toHaveValue(''); // intentional clear survives
    expect(screen.getByLabelText('API Key')).toHaveValue('unsent-secret'); // unsent secret kept in memory
    expect(screen.getByText(/3 unsaved changes/)).toBeInTheDocument();
    // Nothing was auto-submitted.
    expect(patches).toBe(1);
  });

  it('keeps Save disabled and the conflict latched when the reload itself fails', async () => {
    const user = userEvent.setup();
    const mod = moduleWith([field({ key: 'a', label: 'Alpha' })], {
      availableEnvironments: ['production']
    });
    const hits = { n: 0 };
    server.use(
      envGet(hits, { a: 'x' }),
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () =>
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
    renderWithProviders(<TestHost mod={mod} />);
    const alpha = await screen.findByLabelText('Alpha');
    await waitFor(() => expect(alpha).toHaveValue('x'));
    await user.clear(alpha);
    await user.type(alpha, 'y');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));
    await screen.findByRole('button', { name: 'Reload & review' });

    server.use(
      http.get(url('/v1/admin/modules/:name/environments/:env'), () =>
        HttpResponse.json(
          { status: 503, title: 'Service Unavailable', detail: 'down' },
          { status: 503 }
        )
      )
    );
    await user.click(screen.getByRole('button', { name: 'Reload & review' }));
    expect(
      await screen.findByText(/Reloading the latest configuration failed/)
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled();
    expect(
      screen.getByRole('button', { name: 'Reload & review' })
    ).toBeInTheDocument();
    expect(alpha).toHaveValue('y'); // the draft is untouched
  });

  it('keeps the record-list wording for a codeless 409', async () => {
    const user = userEvent.setup();
    const mod = moduleWith([field({ key: 'a', label: 'Alpha' })], {
      availableEnvironments: ['production']
    });
    server.use(
      envGet({ n: 0 }, { a: 'x' }),
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () =>
        HttpResponse.json(
          { status: 409, title: 'Conflict', detail: 'slug exists' },
          { status: 409 }
        )
      )
    );
    renderWithProviders(<TestHost mod={mod} />);
    const alpha = await screen.findByLabelText('Alpha');
    await waitFor(() => expect(alpha).toHaveValue('x'));
    await user.clear(alpha);
    await user.type(alpha, 'y');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));
    expect(await screen.findByText(/changed this list/)).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Reload & review' })
    ).not.toBeInTheDocument();
  });
});
