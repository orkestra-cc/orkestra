// Jobs types — core jobs module admin API (/v1/admin/jobs).
// Mirrors backend/internal/core/jobs/services (JobsJobView, JobsRunView) and
// handlers (update/preview bodies).

export type JobScheduleKind = 'every' | 'cron' | 'manual';

export interface JobSchedule {
  kind: JobScheduleKind;
  everySeconds?: number;
  cron?: string;
}

export interface JobLastRun {
  runId: string;
  status: JobRunStatus;
  startedAt: string;
  finishedAt: string;
  durationMs: number;
  error?: string;
}

export interface Job {
  key: string;
  module: string;
  name: string;
  title: string;
  description?: string;
  defaultSchedule: JobSchedule;
  /** Effective schedule (after the operator override). */
  schedule: JobSchedule;
  /** Effective IANA zone for cron schedules. */
  timezone: string;
  enabled: boolean;
  defaultEnabled: boolean;
  overridden: boolean;
  moduleStarted: boolean;
  running: boolean;
  nextRunAt?: string;
  retryAt?: string;
  attempt: number;
  runRequestedAt?: string;
  lastRun?: JobLastRun;
  timeoutSeconds: number;
  maxAttempts: number;
  /** Optimistic-concurrency token every write must carry. */
  revision: number;
}

export const JOB_RUN_STATUSES = [
  'running',
  'succeeded',
  'failed',
  'timed_out',
  'abandoned',
  'interrupted',
  'skipped'
] as const;

export type JobRunStatus = (typeof JOB_RUN_STATUSES)[number];

export type JobRunTrigger = 'schedule' | 'manual' | 'retry' | 'catchup';

export interface JobRun {
  id: string;
  key: string;
  trigger: JobRunTrigger;
  status: JobRunStatus;
  error?: string;
  replica?: string;
  requestedBy?: string;
  attempt: number;
  scheduledFor?: string;
  startedAt: string;
  finishedAt?: string;
  durationMs: number;
  details?: Record<string, unknown>;
  droppedDetails?: number;
}

export interface JobRunsPage {
  runs: JobRun[];
  nextCursor?: string;
}

export interface JobRunsArgs {
  key: string;
  status?: JobRunStatus;
  cursor?: string;
  limit?: number;
}

export interface JobScheduleInput {
  kind: 'every' | 'cron';
  everySeconds?: number;
  cron?: string;
}

export interface UpdateJobInput {
  key: string;
  revision: number;
  enabled?: boolean;
  schedule?: JobScheduleInput;
  /** IANA zone; an empty string removes the timezone override. */
  timezone?: string;
}

export interface JobPreviewInput {
  kind: 'every' | 'cron';
  everySeconds?: number;
  cron?: string;
  timezone?: string;
}

export interface JobPreviewResult {
  next: string[];
  description: string;
}
