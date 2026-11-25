import type { TFunction } from 'i18next';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { Job, JobSchedule } from 'types/jobs';

const MINUTE = 60;
const HOUR = 3600;
const DAY = 86400;

/** An interval in the coarsest unit that divides it exactly. */
export const splitInterval = (
  seconds: number
): { value: number; unit: 's' | 'min' | 'h' | 'd' } => {
  if (seconds % DAY === 0) return { value: seconds / DAY, unit: 'd' };
  if (seconds % HOUR === 0) return { value: seconds / HOUR, unit: 'h' };
  if (seconds % MINUTE === 0) return { value: seconds / MINUTE, unit: 'min' };
  return { value: seconds, unit: 's' };
};

/** "every 1 h", "cron 0 2 * * * (Europe/Rome)" or "manual". */
export const describeSchedule = (
  t: TFunction,
  schedule: JobSchedule,
  timezone: string
): string => {
  switch (schedule.kind) {
    case 'every': {
      const { value, unit } = splitInterval(schedule.everySeconds ?? 0);
      return t('adminJobs.schedule.every', {
        value,
        unit: t(`adminJobs.schedule.units.${unit}`)
      });
    }
    case 'cron':
      return t('adminJobs.schedule.cron', {
        expr: schedule.cron ?? '',
        zone: timezone
      });
    default:
      return t('adminJobs.schedule.manual');
  }
};

/** "850 ms", "12.3 s", "4 min 5 s". */
export const formatDuration = (ms: number): string => {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`;
  const totalSeconds = ms / 1000;
  if (totalSeconds < 60) return `${Math.round(totalSeconds * 10) / 10} s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = Math.round(totalSeconds - minutes * 60);
  return seconds > 0 ? `${minutes} min ${seconds} s` : `${minutes} min`;
};

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60],
  ['second', 1]
];

/** Relative time ("in 5 minutes", "2 hours ago") in the console language. */
export const formatRelative = (
  iso: string | undefined,
  lang: string,
  now: number = Date.now()
): string => {
  if (!iso) return '—';
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return '—';
  const diffSeconds = Math.round((at - now) / 1000);
  const abs = Math.abs(diffSeconds);
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' });
  for (const [unit, size] of UNITS) {
    if (abs >= size || unit === 'second') {
      return rtf.format(Math.trunc(diffSeconds / size), unit);
    }
  }
  return '—';
};

export type JobStatusKey =
  | 'disabled'
  | 'moduleStopped'
  | 'running'
  | 'retryPending'
  | 'lastOk'
  | 'lastFailed'
  | 'lastSkipped'
  | 'neverRun';

/** The one status a job row shows, most blocking first. */
export const jobStatus = (
  job: Job
): { key: JobStatusKey; color: BadgeColor } => {
  if (!job.enabled) return { key: 'disabled', color: 'secondary' };
  if (!job.moduleStarted) return { key: 'moduleStopped', color: 'warning' };
  if (job.running) return { key: 'running', color: 'info' };
  if (job.retryAt) return { key: 'retryPending', color: 'warning' };
  if (!job.lastRun) return { key: 'neverRun', color: 'light' };
  if (job.lastRun.status === 'succeeded') {
    return { key: 'lastOk', color: 'success' };
  }
  if (job.lastRun.status === 'skipped') {
    return { key: 'lastSkipped', color: 'secondary' };
  }
  return { key: 'lastFailed', color: 'danger' };
};
