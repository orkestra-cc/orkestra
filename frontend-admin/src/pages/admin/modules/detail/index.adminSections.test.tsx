import { lazy, useEffect } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { delay, http, HttpResponse } from 'msw';
import { Provider } from 'react-redux';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { setupStore } from 'test/render';
import { server } from 'test/server';
import type { ModuleAdminSectionProps } from 'modules/types';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleDetailPage from './index';

// When set, Probe reports a write from its mount effect — the shape of a
// section that starts loading-and-saving as soon as it is shown.
let probeBusyOnMount = false;

const seen: ModuleAdminSectionProps[] = [];
const Probe = (props: ModuleAdminSectionProps) => {
  seen.push(props);
  const { onBusyChange } = props;
  useEffect(() => {
    if (probeBusyOnMount) onBusyChange(true);
  }, [onBusyChange]);
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

const seenOther: ModuleAdminSectionProps[] = [];
const OtherProbe = (props: ModuleAdminSectionProps) => {
  seenOther.push(props);
  return <span>other module={props.moduleName}</span>;
};

const PairProbe = ({
  name,
  onBusyChange
}: ModuleAdminSectionProps & { name: string }) => (
  <div>
    <button type="button" onClick={() => onBusyChange(true)}>
      {name} busy
    </button>
    <button type="button" onClick={() => onBusyChange(false)}>
      {name} idle
    </button>
  </div>
);
const PairOne = (props: ModuleAdminSectionProps) => (
  <PairProbe {...props} name="one" />
);
const PairTwo = (props: ModuleAdminSectionProps) => (
  <PairProbe {...props} name="two" />
);

vi.mock('modules/index', () => ({
  moduleCatalog: {
    pair: {
      name: 'pair',
      routes: () => [],
      adminSections: [
        {
          key: 'one',
          labelKey: 'pair.one',
          component: lazy(async () => ({ default: PairOne }))
        },
        {
          key: 'two',
          labelKey: 'pair.two',
          component: lazy(async () => ({ default: PairTwo }))
        }
      ]
    },
    other: {
      name: 'other',
      routes: () => [],
      adminSections: [
        {
          key: 'extra',
          labelKey: 'other.extra',
          component: lazy(async () => ({ default: OtherProbe }))
        }
      ]
    },
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
    http.get('*/v1/admin/modules/:name', ({ params }) =>
      HttpResponse.json({ ...mod, moduleName: params.name })
    ),
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
    probeBusyOnMount = false;
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

  it('lets a navigation through once the write that held it settles', async () => {
    stub(railModule);
    const user = userEvent.setup();
    const router = renderAt('/admin/modules/demo?section=__addon:extra');

    await user.click(await screen.findByRole('button', { name: 'busy' }));
    await router.navigate('/elsewhere');
    expect(await screen.findByText('Saving in progress')).toBeInTheDocument();

    // The write settles with the modal still open: nothing is left to protect,
    // so the operator's navigation completes without clicking Leave.
    await user.click(screen.getByRole('button', { name: 'idle' }));
    expect(await screen.findByText('elsewhere')).toBeInTheDocument();
    expect(screen.queryByText('Saving in progress')).not.toBeInTheDocument();
  });

  it('makes the shared config form read-only on the stacked page while busy', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    const user = userEvent.setup();
    renderAt('/admin/modules/demo');

    const input = await screen.findByLabelText(/First/);
    expect(input).toBeEnabled();
    await user.click(await screen.findByRole('button', { name: 'busy' }));
    expect(input).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'idle' }));
    expect(input).toBeEnabled();
  });

  it('wraps the section in a card titled by its label on the rail layout too', async () => {
    stub(railModule);
    renderAt('/admin/modules/demo?section=__addon:extra');
    expect(
      await screen.findByText('probe env=production dirty=false')
    ).toBeInTheDocument();
    const probe = screen.getByText('probe env=production dirty=false');
    const card = probe.closest('.card');
    expect(card).not.toBeNull();
    expect(
      within(card as HTMLElement).getByText('demo.extra')
    ).toBeInTheDocument();
  });

  it('disables the environment switcher while a section is busy', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    const user = userEvent.setup();
    renderAt('/admin/modules/demo');

    const sandbox = await screen.findByRole('button', { name: 'sandbox' });
    expect(sandbox).toBeEnabled();
    await user.click(await screen.findByRole('button', { name: 'busy' }));
    expect(sandbox).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'idle' }));
    expect(sandbox).toBeEnabled();
  });

  it('tells the section when the shared form has unsaved edits', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    const user = userEvent.setup();
    renderAt('/admin/modules/demo');

    const input = await screen.findByLabelText(/First/);
    expect(
      await screen.findByText('probe env=production dirty=false')
    ).toBeInTheDocument();
    await user.type(input, 'x');
    expect(
      await screen.findByText('probe env=production dirty=true')
    ).toBeInTheDocument();
  });

  it('clears a stuck busy state when the module changes', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    const user = userEvent.setup();
    const router = renderAt('/admin/modules/demo');

    await user.click(await screen.findByRole('button', { name: 'busy' }));
    expect(screen.getByRole('button', { name: 'sandbox' })).toBeDisabled();

    // The operator forces the navigation past the "saving in progress" modal.
    await router.navigate('/admin/modules/other');
    await user.click(
      await screen.findByRole('button', { name: 'Leave anyway' })
    );
    // The section that reported busy is gone; the page must not stay locked.
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'sandbox' })).toBeEnabled()
    );
  });

  it('never hands a section another module name while the new module loads', async () => {
    seenOther.length = 0;
    server.use(
      http.get('*/v1/admin/modules', () =>
        HttpResponse.json({ modules: [railModule] })
      ),
      http.get('*/v1/admin/modules/health', () =>
        HttpResponse.json({ modules: [] })
      ),
      http.get('*/v1/admin/modules/:name', async ({ params }) => {
        if (params.name === 'other') await delay(100);
        return HttpResponse.json({
          ...railModule,
          moduleName: params.name,
          displayName: String(params.name)
        });
      }),
      http.get('*/v1/admin/modules/:name/environments/:env', ({ params }) =>
        HttpResponse.json({
          environment: params.env,
          configValues: {},
          secretStatus: {},
          updatedAt: ''
        })
      )
    );
    const router = renderAt('/admin/modules/demo');
    await screen.findByRole('button', { name: 'demo.extra' });

    await router.navigate('/admin/modules/other?section=__addon:extra');
    expect(await screen.findByText('other module=other')).toBeInTheDocument();

    expect(seenOther.length).toBeGreaterThan(0);
    expect(seenOther.every(p => p.moduleName === 'other')).toBe(true);
    // The requested section survived the switch.
    expect(router.state.location.search).toContain('section=__addon');
  });

  it('locks the rail save bar while a section is busy', async () => {
    stub(railModule);
    const user = userEvent.setup();
    renderAt('/admin/modules/demo?section=a');

    // An unsaved edit keeps the save bar on screen from every section.
    await user.type(await screen.findByLabelText(/First/), 'x');
    await user.click(screen.getByRole('button', { name: 'demo.extra' }));
    await user.click(await screen.findByRole('button', { name: 'busy' }));

    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Discard' })).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'idle' }));
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Discard' })).toBeEnabled();
  });

  it('keeps the lock a section reports as it mounts', async () => {
    stub(railModule);
    const user = userEvent.setup();
    renderAt('/admin/modules/demo?section=__addon:extra');

    // Load the section once, so the second visit mounts it in the same
    // commit that changes the active section.
    await screen.findByRole('button', { name: 'busy' });
    await user.click(screen.getByRole('button', { name: 'Group A' }));
    await screen.findByLabelText(/First/);

    probeBusyOnMount = true;
    await user.click(screen.getByRole('button', { name: 'demo.extra' }));
    await screen.findByRole('button', { name: 'busy' });

    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Group A' })).toHaveAttribute(
        'aria-disabled',
        'true'
      )
    );
  });

  it('stays locked until every busy section on the stacked page settles', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    const user = userEvent.setup();
    renderAt('/admin/modules/pair');

    const sandbox = await screen.findByRole('button', { name: 'sandbox' });
    await user.click(await screen.findByRole('button', { name: 'one busy' }));
    await user.click(await screen.findByRole('button', { name: 'two busy' }));
    await user.click(screen.getByRole('button', { name: 'one idle' }));
    // Section two is still writing.
    expect(sandbox).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'two idle' }));
    expect(sandbox).toBeEnabled();
  });

  it('ignores a report from a section that is no longer shown', async () => {
    stub({ ...railModule, configGroups: [] } as ModuleConfig);
    seen.length = 0;
    const router = renderAt('/admin/modules/demo');
    await screen.findByRole('button', { name: 'busy' });
    const departed = seen[seen.length - 1].onBusyChange;

    await router.navigate('/admin/modules/other');
    expect(await screen.findByText('other module=other')).toBeInTheDocument();
    const sandbox = screen.getByRole('button', { name: 'sandbox' });
    expect(sandbox).toBeEnabled();

    // The old section's write settles (or starts) after it was unmounted: it
    // must not lock the page it no longer belongs to.
    act(() => departed(true));
    expect(sandbox).toBeEnabled();
  });
});
