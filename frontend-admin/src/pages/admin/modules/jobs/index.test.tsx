import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Provider } from 'react-redux';
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router';
import { toast } from 'react-toastify';
import { setupStore } from 'test/render';
import { url } from 'test/handlers';
import { server } from 'test/server';
import type { ModuleConfig } from 'store/api/moduleApi';
import type { Job, JobRun } from 'types/jobs';
import JobsModulePage from './index';

vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

let canManage = true;
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => ({
    hasPermission: (key: string) =>
      key === 'system.jobs.read' || (canManage && key === 'system.jobs.manage')
  })
}));

const jobsModule: ModuleConfig = {
  moduleName: 'jobs',
  displayName: 'Jobs',
  description: 'Background job scheduler',
  category: 'core',
  enabled: true,
  status: 'running',
  needsRestart: false,
  configValues: {},
  secretStatus: {},
  configSchema: [],
  dependsOn: [],
  providedServices: [],
  requiredServices: [],
  optionalServices: [],
  activeEnvironment: 'production',
  availableEnvironments: ['production'],
  createdAt: '2026-08-20T10:00:00Z',
  updatedAt: '2026-08-20T12:00:00Z'
};

const sweep: Job = {
  key: 'auth.token_sweep',
  module: 'auth',
  name: 'token_sweep',
  title: 'Token sweep',
  description: 'Removes expired tokens.',
  defaultSchedule: { kind: 'every', everySeconds: 3600 },
  schedule: { kind: 'every', everySeconds: 3600 },
  timezone: 'UTC',
  enabled: true,
  defaultEnabled: true,
  overridden: false,
  moduleStarted: true,
  running: false,
  nextRunAt: '2030-01-01T10:00:00Z',
  attempt: 0,
  lastRun: {
    runId: 'r1',
    status: 'succeeded',
    startedAt: '2030-01-01T09:00:00Z',
    finishedAt: '2030-01-01T09:00:02Z',
    durationMs: 2000
  },
  timeoutSeconds: 600,
  maxAttempts: 1,
  revision: 7
};

const cleanup: Job = {
  ...sweep,
  key: 'jobs.runs_cleanup',
  module: 'jobs',
  name: 'runs_cleanup',
  title: 'Run history cleanup',
  description: 'Deletes old runs.',
  defaultSchedule: { kind: 'cron', cron: '0 2 * * *' },
  schedule: { kind: 'cron', cron: '30 3 * * *' },
  timezone: 'Europe/Rome',
  overridden: true,
  lastRun: {
    runId: 'r9',
    status: 'failed',
    startedAt: '2030-01-01T02:00:00Z',
    finishedAt: '2030-01-01T02:00:01Z',
    durationMs: 1000,
    error: 'boom'
  },
  revision: 3
};

const run = (n: number, over: Partial<JobRun> = {}): JobRun => ({
  id: `run-${n}`,
  key: sweep.key,
  trigger: 'schedule',
  status: 'succeeded',
  attempt: 1,
  startedAt: '2030-01-01T09:00:00Z',
  finishedAt: '2030-01-01T09:00:02Z',
  durationMs: 2000,
  ...over
});

let currentSearch = '';
const LocationProbe = () => {
  currentSearch = useLocation().search;
  return null;
};

let listCalls = 0;
const stubWorkspace = (jobs: Job[] = [sweep, cleanup]) => {
  server.use(
    http.get(url('/v1/admin/modules'), () =>
      HttpResponse.json({ modules: [jobsModule] })
    ),
    http.get(url('/v1/admin/modules/health'), () =>
      HttpResponse.json({ modules: [], checkedAt: '2026-08-20T12:00:00Z' })
    ),
    http.get(url('/v1/admin/modules/jobs'), () =>
      HttpResponse.json(jobsModule)
    ),
    http.get(url('/v1/admin/jobs'), () => {
      listCalls += 1;
      return HttpResponse.json({ jobs });
    })
  );
};

const renderAt = (search = '') => {
  const store = setupStore();
  const router = createMemoryRouter(
    [
      {
        path: '/admin/modules/jobs',
        element: (
          <>
            <LocationProbe />
            <JobsModulePage />
          </>
        )
      }
    ],
    { initialEntries: [`/admin/modules/jobs${search}`] }
  );
  return render(
    <Provider store={store}>
      <RouterProvider router={router} />
    </Provider>
  );
};

describe('JobsModulePage', () => {
  beforeEach(() => {
    currentSearch = '';
    listCalls = 0;
    canManage = true;
    vi.clearAllMocks();
    stubWorkspace();
  });

  it('renders a row per job with schedule text and status', async () => {
    renderAt();
    const sweepRow = (await screen.findByText('Token sweep')).closest('tr')!;
    expect(within(sweepRow).getByText('auth.token_sweep')).toBeInTheDocument();
    expect(within(sweepRow).getByText('every 1 h')).toBeInTheDocument();
    expect(within(sweepRow).getByText('Last run OK')).toBeInTheDocument();

    const cleanupRow = screen.getByText('Run history cleanup').closest('tr')!;
    expect(
      within(cleanupRow).getByText('cron 30 3 * * * (Europe/Rome)')
    ).toBeInTheDocument();
    expect(within(cleanupRow).getByText('Last run failed')).toBeInTheDocument();
  });

  it('shows disabled and module-stopped before the last run', async () => {
    stubWorkspace([
      { ...sweep, enabled: false },
      { ...cleanup, moduleStarted: false }
    ]);
    renderAt();
    expect(await screen.findByText('Disabled')).toBeInTheDocument();
    expect(screen.getByText('Module stopped')).toBeInTheDocument();
  });

  it('disabling a job sends PATCH with the revision it read', async () => {
    let body: unknown;
    server.use(
      http.patch(
        url('/v1/admin/jobs/auth.token_sweep'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json({ ...sweep, enabled: false, revision: 8 });
        }
      )
    );
    renderAt();
    await userEvent.click(
      await screen.findByRole('switch', { name: 'Enable Token sweep' })
    );
    await waitFor(() => expect(body).toEqual({ revision: 7, enabled: false }));
  });

  it('a revision conflict toasts and refetches the list', async () => {
    server.use(
      http.patch(url('/v1/admin/jobs/auth.token_sweep'), () =>
        HttpResponse.json(
          { code: 'jobs.revision_conflict', detail: 'changed' },
          { status: 409 }
        )
      )
    );
    renderAt();
    await userEvent.click(
      await screen.findByRole('switch', { name: 'Enable Token sweep' })
    );
    await waitFor(() => expect(toast.warn).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(listCalls).toBeGreaterThanOrEqual(2));
  });

  it('shows no action controls to a read-only user', async () => {
    canManage = false;
    renderAt();
    await screen.findByText('Token sweep');
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /Run Token sweep now/ })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /Edit schedule/ })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /Reset .* to default/ })
    ).not.toBeInTheDocument();
  });

  it('offers reset only for an overridden job and sends the revision', async () => {
    let query: string | undefined;
    server.use(
      http.delete(
        url('/v1/admin/jobs/jobs.runs_cleanup/override'),
        ({ request }) => {
          query = new URL(request.url).search;
          return HttpResponse.json({ ...cleanup, overridden: false });
        }
      )
    );
    renderAt();
    await screen.findByText('Token sweep');
    expect(
      screen.queryByRole('button', { name: 'Reset Token sweep to default' })
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole('button', {
        name: 'Reset Run history cleanup to default'
      })
    );
    await waitFor(() => expect(query).toBe('?revision=3'));
  });

  it('run now posts the request', async () => {
    let called = false;
    server.use(
      http.post(url('/v1/admin/jobs/auth.token_sweep/run'), () => {
        called = true;
        return HttpResponse.json(sweep, { status: 202 });
      })
    );
    renderAt();
    await userEvent.click(
      await screen.findByRole('button', { name: 'Run Token sweep now' })
    );
    await waitFor(() => expect(called).toBe(true));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
  });

  it('previews five due times from the backend while editing a cron schedule', async () => {
    let previewBody: unknown;
    server.use(
      http.post(url('/v1/admin/jobs/schedule-preview'), async ({ request }) => {
        const body = (await request.json()) as { kind: string };
        previewBody = body;
        // The interval schedule the drawer opens with previews one time;
        // only the cron the operator types previews five.
        const count = body.kind === 'cron' ? 5 : 1;
        return HttpResponse.json({
          next: Array.from(
            { length: count },
            (_, i) => `2030-01-0${i + 1}T02:00:00Z`
          ),
          description: 'At 02:00 every day'
        });
      })
    );
    renderAt();
    await userEvent.click(
      await screen.findByRole('button', {
        name: 'Edit schedule of Token sweep'
      })
    );
    expect(currentSearch).toContain('job=auth.token_sweep');
    expect(currentSearch).toContain('tab=schedule');

    await userEvent.selectOptions(
      await screen.findByLabelText('Schedule type'),
      'cron'
    );
    const cron = screen.getByLabelText('Cron expression');
    await userEvent.clear(cron);
    await userEvent.type(cron, '0 2 * * *');

    // The list is replaced while the new preview loads: query it afresh.
    await waitFor(() =>
      expect(
        within(screen.getByRole('list', { name: 'Next runs' })).getAllByRole(
          'listitem'
        )
      ).toHaveLength(5)
    );
    expect(screen.getByText('At 02:00 every day')).toBeInTheDocument();
    expect(previewBody).toMatchObject({ kind: 'cron', cron: '0 2 * * *' });
  });

  it('saves an every schedule in seconds with the revision', async () => {
    let body: unknown;
    server.use(
      http.post(url('/v1/admin/jobs/schedule-preview'), () =>
        HttpResponse.json({ next: ['2030-01-01T02:00:00Z'], description: 'x' })
      ),
      http.patch(
        url('/v1/admin/jobs/auth.token_sweep'),
        async ({ request }) => {
          body = await request.json();
          return HttpResponse.json(sweep);
        }
      )
    );
    renderAt('?job=auth.token_sweep&tab=schedule');
    const minutes = await screen.findByLabelText('Every (minutes)');
    await userEvent.clear(minutes);
    await userEvent.type(minutes, '15');
    await userEvent.click(
      screen.getByRole('button', { name: 'Save schedule' })
    );
    await waitFor(() =>
      expect(body).toEqual({
        revision: 7,
        schedule: { kind: 'every', everySeconds: 900 }
      })
    );
  });

  it('shows an invalid-schedule detail inline', async () => {
    server.use(
      http.post(url('/v1/admin/jobs/schedule-preview'), () =>
        HttpResponse.json({ next: ['2030-01-01T02:00:00Z'], description: 'x' })
      ),
      http.patch(url('/v1/admin/jobs/auth.token_sweep'), () =>
        HttpResponse.json(
          {
            code: 'jobs.invalid_schedule',
            detail: 'cron minute: 61 is out of range'
          },
          { status: 422 }
        )
      )
    );
    renderAt('?job=auth.token_sweep&tab=schedule');
    const minutes = await screen.findByLabelText('Every (minutes)');
    await userEvent.clear(minutes);
    await userEvent.type(minutes, '15');
    await userEvent.click(
      screen.getByRole('button', { name: 'Save schedule' })
    );
    expect(
      await screen.findByText(/cron minute: 61 is out of range/)
    ).toBeInTheDocument();
  });

  it('loads run history by cursor and filters by status', async () => {
    const seen: string[] = [];
    server.use(
      http.get(url('/v1/admin/jobs/auth.token_sweep/runs'), ({ request }) => {
        const q = new URL(request.url).searchParams;
        seen.push(q.toString());
        if (q.get('status') === 'failed') {
          return HttpResponse.json({
            runs: [run(50, { status: 'failed', error: 'kaput' })]
          });
        }
        if (q.get('cursor') === 'c1') {
          return HttpResponse.json({ runs: [run(3)] });
        }
        return HttpResponse.json({
          runs: [run(1), run(2, { status: 'failed', error: 'kaput' })],
          nextCursor: 'c1'
        });
      })
    );
    renderAt('?job=auth.token_sweep&tab=runs');
    await waitFor(() =>
      expect(screen.getAllByTestId('job-run-row')).toHaveLength(2)
    );
    await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() =>
      expect(screen.getAllByTestId('job-run-row')).toHaveLength(3)
    );
    expect(
      screen.queryByRole('button', { name: 'Load more' })
    ).not.toBeInTheDocument();
    expect(seen.some(s => s.includes('cursor=c1'))).toBe(true);

    await userEvent.selectOptions(
      screen.getByLabelText('Filter by status'),
      'failed'
    );
    await waitFor(() =>
      expect(screen.getAllByTestId('job-run-row')).toHaveLength(1)
    );
    await userEvent.click(screen.getByRole('button', { name: 'Show details' }));
    expect(screen.getByText('kaput')).toBeInTheDocument();
  });

  it('keeps the selected job and tab in the URL', async () => {
    renderAt('?job=jobs.runs_cleanup&tab=overview');
    expect(
      await screen.findByRole('dialog', { name: /Run history cleanup/ })
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: 'Runs' }));
    expect(currentSearch).toContain('tab=runs');
    expect(currentSearch).toContain('job=jobs.runs_cleanup');
  });
});
