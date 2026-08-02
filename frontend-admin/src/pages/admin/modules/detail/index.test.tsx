import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
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
    field({ key: 'toggle', label: 'Enable Google', group: 'oauth' }),
    field({ key: 'clientId', label: 'Client ID', group: 'oauth.google' }),
    field({ key: 'minLen', label: 'Minimum length', group: 'password' })
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
            configValues: { minLen: '10' },
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
    expect(capturedBody).toEqual({ config: { minLen: '10' } });
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
});
