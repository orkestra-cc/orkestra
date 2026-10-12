import { lazy } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Provider } from 'react-redux';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { setupStore } from 'test/render';
import { server } from 'test/server';
import type { ModuleAdminSectionProps } from 'modules/types';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleDetailPage from './index';

const seen: ModuleAdminSectionProps[] = [];
const Probe = (props: ModuleAdminSectionProps) => {
  seen.push(props);
  return (
    <div>
      <span>
        probe env={props.environment} dirty={String(props.configDirty)}
      </span>
      <button type="button" onClick={() => props.onBusyChange(true)}>
        busy
      </button>
      <button type="button" onClick={() => props.onBusyChange(false)}>
        idle
      </button>
    </div>
  );
};

vi.mock('modules/index', () => ({
  moduleCatalog: {
    demo: {
      name: 'demo',
      routes: () => [],
      adminSections: [
        {
          key: 'extra',
          labelKey: 'demo.extra',
          component: lazy(async () => ({ default: Probe }))
        },
        {
          key: 'Bad Key',
          labelKey: 'demo.bad',
          component: lazy(async () => ({ default: Probe }))
        }
      ]
    }
  }
}));

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

const railModule = {
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
    field({ key: 'a.one', label: 'First', group: 'a' }),
    field({ key: 'b.two', label: 'Second', group: 'b' })
  ],
  configGroups: [
    { key: 'a', label: 'Group A', order: 1 },
    { key: 'b', label: 'Group B', order: 2 }
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

const stub = (mod: ModuleConfig) =>
  server.use(
    http.get('*/v1/admin/modules', () => HttpResponse.json({ modules: [mod] })),
    http.get('*/v1/admin/modules/health', () =>
      HttpResponse.json({
        modules: [{ moduleName: 'demo', status: 'healthy' }]
      })
    ),
    http.get('*/v1/admin/modules/:name', () => HttpResponse.json(mod)),
    http.get('*/v1/admin/modules/:name/environments/:env', ({ params }) =>
      HttpResponse.json({
        environment: params.env,
        configValues: {},
        secretStatus: {},
        updatedAt: ''
      })
    )
  );

const renderAt = (path: string) => {
  const router = createMemoryRouter(
    [
      { path: '/admin/modules/:moduleName', element: <ModuleDetailPage /> },
      { path: '/elsewhere', element: <div>elsewhere</div> }
    ],
    { initialEntries: [path] }
  );
  render(
    <Provider store={setupStore()}>
      <RouterProvider router={router} />
    </Provider>
  );
  return router;
};

describe('module detail admin sections', () => {
  const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
  afterEach(() => {
    warn.mockClear();
  });

  it('lists valid addon sections under Extensions and renders them with page props', async () => {
    stub(railModule);
    const user = userEvent.setup();
    const router = renderAt('/admin/modules/demo?env=sandbox');

    await user.click(await screen.findByRole('button', { name: 'demo.extra' }));
    expect(
      await screen.findByText('probe env=sandbox dirty=false')
    ).toBeInTheDocument();
    expect(router.state.location.search).toContain('section=__addon%3Aextra');
    expect(screen.getByText('Extensions')).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'demo.bad' })
    ).not.toBeInTheDocument();
    // The invalid 'Bad Key' entry is dropped with a development warning.
    expect(warn).toHaveBeenCalled();
  });

  it('locks the rail and blocks navigation while the section is busy', async () => {
    stub(railModule);
    const user = userEvent.setup();
    const router = renderAt('/admin/modules/demo?section=__addon:extra');

    await user.click(await screen.findByRole('button', { name: 'busy' }));
    const groupA = screen.getByRole('button', { name: 'Group A' });
    expect(groupA).toHaveAttribute('aria-disabled', 'true');
    await user.click(groupA);
    expect(router.state.location.search).toContain('section=__addon');

    await router.navigate('/elsewhere');
    expect(await screen.findByText('Saving in progress')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Stay' }));
    expect(screen.queryByText('elsewhere')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'idle' }));
    await router.navigate('/elsewhere');
    expect(await screen.findByText('elsewhere')).toBeInTheDocument();
  });

  it('renders addon sections as cards on the stacked page', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    renderAt('/admin/modules/demo');
    expect(
      await screen.findByText('probe env=production dirty=false')
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText('demo.extra')).toBeInTheDocument()
    );
  });
});
