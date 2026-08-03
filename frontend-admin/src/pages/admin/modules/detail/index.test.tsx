import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Route, Routes, useLocation } from 'react-router';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleDetailPage from './index';

// The shared form (and therefore the blocker) now lives at this page level.
// useBlocker requires a *data* router (createBrowserRouter/createMemoryRouter
// + RouterProvider) and throws under the declarative <MemoryRouter> that
// renderWithProviders uses — see ModuleConfigSection.test.tsx for the same
// workaround. Blocking behavior itself isn't what these tests exercise, so
// stub just that hook and keep spreading the original: useLocation, Routes
// and Route below all come from the same module.
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

const demoModule: ModuleConfig = {
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
    field({
      key: 'oauth.google.enabled',
      label: 'Enable Google',
      group: 'oauth'
    }),
    field({
      key: 'oauth.google.client_id',
      label: 'Client ID',
      group: 'oauth.google'
    }),
    field({
      key: 'password.min_length',
      label: 'Minimum length',
      group: 'password'
    })
  ],
  configGroups: [
    { key: 'oauth', label: 'OAuth Providers', order: 1 },
    { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
    { key: 'password', label: 'Password Policy', order: 3 }
  ],
  dependsOn: [],
  providedServices: [],
  requiredServices: [],
  optionalServices: [],
  activeEnvironment: 'production',
  availableEnvironments: ['production', 'sandbox'],
  createdAt: '',
  updatedAt: ''
} as ModuleConfig;

// All four endpoints the page touches. MSW runs with
// onUnhandledRequest: 'error', so a missing stub fails the suite with an
// error that looks unrelated to any assertion.
const stubAll = (mod: ModuleConfig = demoModule) => {
  server.use(
    http.get('*/v1/admin/modules', () => HttpResponse.json({ modules: [mod] })),
    http.get('*/v1/admin/modules/health', () =>
      HttpResponse.json({
        modules: [{ moduleName: 'demo', status: 'healthy' }]
      })
    ),
    http.get('*/v1/admin/modules/:name', () => HttpResponse.json(mod)),
    http.get('*/v1/admin/modules/:name/environments/:env', () =>
      HttpResponse.json({
        environment: 'production',
        configValues: {},
        secretStatus: {},
        updatedAt: ''
      })
    )
  );
};

// renderWithProviders wraps in a MemoryRouter, so the URL lives in memory and
// `window.location` never changes. This probe is how the tests read it.
let currentSearch = '';
const LocationProbe = () => {
  currentSearch = useLocation().search;
  return null;
};

const renderAt = (search: string) =>
  renderWithProviders(
    <>
      <LocationProbe />
      <Routes>
        <Route
          path="/admin/modules/:moduleName"
          element={<ModuleDetailPage />}
        />
      </Routes>
    </>,
    { routerEntries: [`/admin/modules/demo${search}`] }
  );

describe('ModuleDetailPage sections', () => {
  beforeEach(() => stubAll());

  it('opens the section named in ?section= on load', async () => {
    renderAt('?section=password');
    expect(await screen.findByText('Minimum length')).toBeInTheDocument();
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();
  });

  it('reaches a nested group from the rail and reflects it in the URL', async () => {
    const user = userEvent.setup();
    renderAt('');
    await user.click(await screen.findByRole('button', { name: 'Google' }));
    expect(screen.getByText('Client ID')).toBeInTheDocument();
    expect(currentSearch).toContain('section=oauth.google');
  });

  it('falls back to Overview when ?section= names an unknown key', async () => {
    // A stale bookmark or a renamed group must not render an empty page.
    renderAt('?section=this-group-was-renamed');
    expect(
      await screen.findByRole('button', { name: /Overview/ })
    ).toHaveAttribute('aria-current', 'true');
  });

  it('keeps an unsaved edit when moving between sections', async () => {
    // Same route, one shared form — switching section is not a navigation, so
    // the edit survives and useBlocker must stay quiet. This is the whole
    // point of one form per module.
    const user = userEvent.setup();
    renderAt('?section=oauth.google');
    await user.type(await screen.findByLabelText('Client ID'), 'abc123');

    await user.click(screen.getByRole('button', { name: 'Password Policy' }));
    expect(screen.getByText('Minimum length')).toBeInTheDocument();
    expect(screen.queryByText(/unsaved changes\?/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Google' }));
    expect(screen.getByLabelText('Client ID')).toHaveValue('abc123');
  });

  it('renders the stacked page unchanged when the module declares no groups', async () => {
    // The degradation path — every module served today.
    stubAll({ ...demoModule, configGroups: undefined } as ModuleConfig);
    renderAt('');
    expect(await screen.findByText('Enable Google')).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /Overview/ })
    ).not.toBeInTheDocument();
  });

  it('saves an edit in one rail group with a payload containing only that key', async () => {
    // The full-page rail's onSave is a second, page-level copy of the same
    // save model ModuleConfigSection.tsx owns for the degradation path
    // (scoped validation, hidden-field exclusion, the {config, secrets}
    // shape) — the duplicated copy this task's review flagged as untested
    // in its new home. This is that coverage, against the page rather than
    // the card: an edit in `password` must not leak `oauth`/`oauth.google`
    // keys, and the request must fire exactly once.
    const user = userEvent.setup();
    let capturedBody: unknown = null;
    let patchCount = 0;
    server.use(
      http.patch(
        url('/v1/admin/modules/:name/environments/:env'),
        async ({ request }) => {
          patchCount += 1;
          capturedBody = await request.json();
          return HttpResponse.json({
            environment: 'production',
            configValues: { 'password.min_length': '10' },
            secretStatus: {},
            updatedAt: ''
          });
        }
      )
    );

    renderAt('?section=password');
    await user.type(await screen.findByLabelText('Minimum length'), '10');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));

    await waitFor(() => expect(capturedBody).not.toBeNull());
    expect(capturedBody).toEqual({ config: { 'password.min_length': '10' } });
    expect(patchCount).toBe(1);
  });

  it('rewrites a stale ?section= to the resolved fallback instead of leaving it in the URL', async () => {
    // A copied link naming a renamed/removed group must not keep
    // propagating the dead value every time it's shared again.
    renderAt('?section=this-group-was-renamed');
    await screen.findByRole('button', { name: /Overview/ });
    await waitFor(() => expect(currentSearch).toContain('section=__overview'));
    expect(currentSearch).not.toContain('this-group-was-renamed');
  });

  it('hides the save bar on Overview when there is nothing to save', async () => {
    // A permanently-disabled Save button under a panel that isn't a form
    // reads as broken UI, not as "nothing to do here".
    renderAt('');
    await screen.findByRole('button', { name: /Overview/ });
    expect(
      screen.queryByRole('button', { name: 'Save Changes' })
    ).not.toBeInTheDocument();
  });

  it('reports a failed save made from a non-config section', async () => {
    // The bar follows the operator everywhere once anything is dirty, so the
    // alerts have to as well. Nested inside the config card, a save fired
    // from Overview returned 400 and said nothing at all — the operator was
    // left believing a config that never landed had been written.
    const user = userEvent.setup();
    server.use(
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () =>
        HttpResponse.json({ detail: 'sandbox is read-only' }, { status: 400 })
      )
    );

    renderAt('?section=password');
    await user.type(await screen.findByLabelText('Minimum length'), '10');

    // Away from the group the edit lives in, onto a panel that is not a form.
    await user.click(screen.getByRole('button', { name: /Overview/ }));
    expect(screen.queryByText('Minimum length')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Save Changes' }));
    expect(await screen.findByText('sandbox is read-only')).toBeInTheDocument();
  });

  it('confirms before an environment switch discards unsaved edits', async () => {
    // Switching environment swaps the query arg behind the whole form and
    // re-seeds it — useBlocker cannot see it, because nothing navigates. With
    // edits now accumulating across the entire module, that silent discard
    // has to be asked for.
    const user = userEvent.setup();
    renderAt('?section=password');
    await user.type(await screen.findByLabelText('Minimum length'), '10');

    await user.click(screen.getByRole('button', { name: /Environments/ }));
    await user.click(screen.getByRole('button', { name: /sandbox/i }));

    // Declining keeps both the environment and the edit.
    expect(
      await screen.findByText(/Switching to sandbox will discard them/)
    ).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Stay' }));
    await user.click(screen.getByRole('button', { name: 'Password Policy' }));
    expect(screen.getByLabelText('Minimum length')).toHaveValue('10');

    // Accepting is what actually drops it.
    await user.click(screen.getByRole('button', { name: /Environments/ }));
    await user.click(screen.getByRole('button', { name: /sandbox/i }));
    await user.click(
      await screen.findByRole('button', { name: 'Discard and switch' })
    );
    await user.click(screen.getByRole('button', { name: 'Password Policy' }));
    expect(screen.getByLabelText('Minimum length')).toHaveValue('');
  });

  it('renders a fieldless parent group as links to its children, with no save bar', async () => {
    // The shape phase 4 declares: `oauth` sits over `oauth.google` /
    // `oauth.apple` and owns no fields itself. Rendering just a heading under
    // a permanently-disabled Save is the exact "this panel is a form when it
    // isn't" the save-bar gate exists to avoid.
    const user = userEvent.setup();
    stubAll({
      ...demoModule,
      configSchema: [
        field({
          key: 'oauth.google.client_id',
          label: 'Client ID',
          group: 'oauth.google'
        }),
        field({ key: 'appleId', label: 'Apple ID', group: 'oauth.apple' }),
        field({
          key: 'password.min_length',
          label: 'Minimum length',
          group: 'password'
        })
      ],
      configGroups: [
        { key: 'oauth', label: 'OAuth Providers', order: 1 },
        { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
        { key: 'oauth.apple', label: 'Apple', parent: 'oauth', order: 3 },
        { key: 'password', label: 'Password Policy', order: 4 }
      ]
    } as ModuleConfig);

    renderAt('?section=oauth');
    const heading = await screen.findByRole('heading', {
      name: 'OAuth Providers'
    });
    expect(
      screen.queryByRole('button', { name: 'Save Changes' })
    ).not.toBeInTheDocument();

    // Scoped to the panel card, so these are the panel's own links and not
    // the rail entries of the same name sitting in the left column.
    const panel = heading.closest('.card') as HTMLElement;
    expect(
      within(panel).getByRole('button', { name: 'Apple' })
    ).toBeInTheDocument();
    await user.click(within(panel).getByRole('button', { name: 'Google' }));

    // Selecting a child moves the rail there — and that panel, having fields,
    // does get a save bar.
    expect(await screen.findByLabelText('Client ID')).toBeInTheDocument();
    expect(currentSearch).toContain('section=oauth.google');
    expect(
      screen.getByRole('button', { name: 'Save Changes' })
    ).toBeInTheDocument();
  });

  it('renders an honest empty state for a leaf whose only field is hidden by dependsOn, with no save bar — and shows both once the condition is met', async () => {
    // A leaf node (no children, so the fieldless-parent branch above cannot
    // fire) can still land in the same "heading over an empty body" state:
    // it owns a field, but that field is currently hidden by an unmet
    // dependsOn — phase 4's `oauth.google` before either Google enable
    // toggle is on. Silently rendering nothing under the heading is exactly
    // as broken as the fieldless-parent case; and a permanently-disabled
    // Save bar underneath it is the same "form when it isn't" bug the
    // fieldless-parent gate above already avoids.
    //
    // "Honest" is not enough on its own, though: a bare "these settings appear
    // once the options they depend on are enabled" never says *where* those
    // options are, which on a fresh install is a dead end on every gated leaf
    // at once. When the node has a parent, the empty state names it and offers
    // a button that moves the rail there.
    const user = userEvent.setup();
    stubAll({
      ...demoModule,
      configSchema: [
        field({
          key: 'oauth.google.enabled',
          label: 'Enable Google',
          type: 'bool',
          default: 'false',
          group: 'oauth'
        }),
        field({
          key: 'oauth.google.client_id',
          label: 'Client ID',
          group: 'oauth.google',
          dependsOn: [{ key: 'oauth.google.enabled', in: ['true'] }]
        }),
        field({
          key: 'password.min_length',
          label: 'Minimum length',
          group: 'password'
        })
      ]
    } as ModuleConfig);

    renderAt('?section=oauth.google');
    expect(
      await screen.findByText(
        'These settings appear once the options they depend on are enabled in OAuth Providers.'
      )
    ).toBeInTheDocument();
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Save Changes' })
    ).not.toBeInTheDocument();

    // The way out of the dead end: the empty state's own button moves the
    // rail to the parent group that owns the gating toggle.
    const panel = screen
      .getByRole('heading', { name: 'Google' })
      .closest('.card') as HTMLElement;
    await user.click(
      within(panel).getByRole('button', { name: 'Go to OAuth Providers' })
    );
    expect(currentSearch).toContain('section=oauth');

    // Flip the toggle from the parent group — the field appears and the
    // save bar follows it.
    await user.click(screen.getByLabelText('Enable Google'));
    await user.click(screen.getByRole('button', { name: 'Google' }));

    expect(screen.getByText('Client ID')).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Save Changes' })
    ).toBeInTheDocument();
  });

  it('falls back to the unqualified empty state for a top-level group with no parent to point at', async () => {
    // The other half of the branch above. A root group has no parent, so
    // there is no group to name and nowhere for a "Go to" button to lead —
    // naming one would be inventing a destination. It gets the plain
    // sentence and no button, and must still not raise a save bar.
    stubAll({
      ...demoModule,
      configSchema: [
        field({
          key: 'oauth.google.enabled',
          label: 'Enable Google',
          type: 'bool',
          default: 'false',
          group: 'oauth'
        }),
        field({
          key: 'password.min_length',
          label: 'Minimum length',
          group: 'password',
          dependsOn: [{ key: 'oauth.google.enabled', in: ['true'] }]
        })
      ]
    } as ModuleConfig);

    renderAt('?section=password');
    expect(
      await screen.findByText(
        'These settings appear once the options they depend on are enabled.'
      )
    ).toBeInTheDocument();
    const panel = screen
      .getByRole('heading', { name: 'Password Policy' })
      .closest('.card') as HTMLElement;
    expect(
      within(panel).queryByRole('button', { name: /^Go to / })
    ).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Minimum length')).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Save Changes' })
    ).not.toBeInTheDocument();
  });
});
