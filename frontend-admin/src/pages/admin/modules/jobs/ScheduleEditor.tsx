import { useMemo } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { skipToken } from '@reduxjs/toolkit/query';
import { yupResolver } from '@hookform/resolvers/yup';
import { useForm, type Resolver } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import * as yup from 'yup';
import { formatDateTime } from 'helpers/dateFormat';
import { resolveErrorMessage } from 'helpers/errorMessage';
import useDebounce from 'hooks/useDebounce';
import {
  usePreviewJobScheduleQuery,
  useUpdateJobMutation
} from 'store/api/jobsApi';
import type {
  Job,
  JobPreviewInput,
  JobSchedule,
  JobScheduleInput,
  UpdateJobInput
} from 'types/jobs';
import { toast } from 'react-toastify';
import { splitInterval } from './format';
import { errorBody, errorStatus, type JobActions } from './useJobActions';

// 366 days, the backend's MaxEvery.
const MAX_MINUTES = 366 * 24 * 60;
const PREVIEW_DEBOUNCE_MS = 300;

interface ScheduleFormValues {
  kind: 'every' | 'cron';
  everyMinutes: number;
  cron: string;
  timezone: string;
}

const schema = yup.object({
  kind: yup.string().oneOf(['every', 'cron']).required(),
  everyMinutes: yup
    .number()
    .transform((v, original) => (original === '' ? NaN : v))
    .when('kind', {
      is: 'every',
      then: s =>
        s
          .typeError('adminJobs.editor.errors.minutes')
          .integer('adminJobs.editor.errors.minutes')
          .min(1, 'adminJobs.editor.errors.minutes')
          .max(MAX_MINUTES, 'adminJobs.editor.errors.minutes')
          .required('adminJobs.editor.errors.minutes'),
      otherwise: s => s.notRequired()
    }),
  cron: yup.string().when('kind', {
    is: 'cron',
    then: s =>
      s
        .trim()
        .required('adminJobs.editor.errors.cron')
        .max(120, 'adminJobs.editor.errors.cron'),
    otherwise: s => s.notRequired()
  }),
  timezone: yup.string().max(64, 'adminJobs.editor.errors.timezone').default('')
});

const defaultsFor = (job: Job): ScheduleFormValues => ({
  kind: job.schedule.kind === 'cron' ? 'cron' : 'every',
  everyMinutes:
    job.schedule.kind === 'every' && job.schedule.everySeconds
      ? Math.max(1, Math.round(job.schedule.everySeconds / 60))
      : 60,
  cron: job.schedule.kind === 'cron' ? (job.schedule.cron ?? '') : '',
  timezone: job.timezone
});

const sameSchedule = (a: JobSchedule, b: JobScheduleInput): boolean =>
  a.kind === b.kind &&
  (b.kind === 'every'
    ? a.everySeconds === b.everySeconds
    : (a.cron ?? '') === b.cron);

const scheduleOf = (v: ScheduleFormValues): JobScheduleInput | null => {
  if (v.kind === 'every') {
    const minutes = Number(v.everyMinutes);
    if (!Number.isInteger(minutes) || minutes < 1 || minutes > MAX_MINUTES) {
      return null;
    }
    return { kind: 'every', everySeconds: minutes * 60 };
  }
  const cron = (v.cron ?? '').trim();
  return cron ? { kind: 'cron', cron } : null;
};

/** What saving would change, relative to the job as loaded. */
const changesOf = (
  job: Job,
  v: ScheduleFormValues
): Pick<UpdateJobInput, 'schedule' | 'timezone'> => {
  const out: Pick<UpdateJobInput, 'schedule' | 'timezone'> = {};
  const schedule = scheduleOf(v);
  if (schedule && !sameSchedule(job.schedule, schedule)) {
    out.schedule = schedule;
  }
  const zone = (v.timezone ?? '').trim();
  if (v.kind === 'cron' && zone !== job.timezone) out.timezone = zone;
  return out;
};

interface ScheduleEditorProps {
  job: Job;
  actions: JobActions;
}

const ScheduleEditor = ({ job, actions }: ScheduleEditorProps) => {
  const { t } = useTranslation();
  const [updateJob, update] = useUpdateJobMutation();
  const form = useForm<ScheduleFormValues>({
    resolver: yupResolver(schema) as unknown as Resolver<ScheduleFormValues>,
    defaultValues: defaultsFor(job)
  });
  const {
    register,
    watch,
    handleSubmit,
    reset,
    setError,
    formState: { errors }
  } = form;

  const kind = watch('kind');
  const everyMinutes = watch('everyMinutes');
  const cron = watch('cron');
  const timezone = watch('timezone');
  const values = useMemo<ScheduleFormValues>(
    () => ({ kind, everyMinutes, cron, timezone }),
    [kind, everyMinutes, cron, timezone]
  );

  const previewArg = useMemo<JobPreviewInput | null>(() => {
    const schedule = scheduleOf(values);
    if (!schedule) return null;
    const zone = (values.timezone ?? '').trim();
    return {
      ...schedule,
      ...(values.kind === 'cron' && zone ? { timezone: zone } : {})
    };
  }, [values]);
  const debouncedArg = useDebounce(previewArg, PREVIEW_DEBOUNCE_MS);
  const preview = usePreviewJobScheduleQuery(debouncedArg ?? skipToken);
  const shown = preview.currentData;

  const changes = changesOf(job, values);
  const dirty =
    changes.schedule !== undefined || changes.timezone !== undefined;
  const previewZone =
    values.kind === 'cron' && (values.timezone ?? '').trim()
      ? values.timezone.trim()
      : values.kind === 'cron'
        ? job.timezone
        : undefined;

  const onSubmit = handleSubmit(async submitted => {
    const diff = changesOf(job, submitted);
    if (!diff.schedule && diff.timezone === undefined) return;
    try {
      const saved = await updateJob({
        key: job.key,
        revision: job.revision,
        ...diff
      }).unwrap();
      reset(defaultsFor(saved));
      toast.success(t('adminJobs.toasts.scheduleSaved', { title: job.title }));
    } catch (e) {
      if (errorStatus(e) === 422) {
        setError('root.server', {
          message: resolveErrorMessage(
            errorBody(e),
            t('adminJobs.errors.generic'),
            { includeDetail: true }
          )
        });
        return;
      }
      actions.fail(e);
    }
  });

  if (job.schedule.kind === 'manual') {
    return (
      <Alert variant="secondary" className="fs-10 mb-0">
        {t('adminJobs.editor.manualLocked')}
      </Alert>
    );
  }

  const defaultText = (() => {
    const d = job.defaultSchedule;
    if (d.kind === 'every') {
      const { value, unit } = splitInterval(d.everySeconds ?? 0);
      return t('adminJobs.schedule.every', {
        value,
        unit: t(`adminJobs.schedule.units.${unit}`)
      });
    }
    if (d.kind === 'cron') {
      return t('adminJobs.schedule.cronShort', { expr: d.cron ?? '' });
    }
    return t('adminJobs.schedule.manual');
  })();

  const fieldError = (message?: string) => (message ? t(message) : undefined);

  return (
    <Form onSubmit={onSubmit} noValidate>
      <p className="text-600 fs-10">
        {t('adminJobs.editor.declaredDefault', { schedule: defaultText })}
      </p>

      <Form.Group className="mb-3" controlId="job-schedule-kind">
        <Form.Label>{t('adminJobs.editor.kind')}</Form.Label>
        <Form.Select {...register('kind')}>
          <option value="every">{t('adminJobs.editor.kindEvery')}</option>
          <option value="cron">{t('adminJobs.editor.kindCron')}</option>
        </Form.Select>
      </Form.Group>

      {kind === 'every' ? (
        <Form.Group className="mb-3" controlId="job-schedule-minutes">
          <Form.Label>{t('adminJobs.editor.everyMinutes')}</Form.Label>
          <Form.Control
            type="number"
            min={1}
            max={MAX_MINUTES}
            step={1}
            isInvalid={!!errors.everyMinutes}
            {...register('everyMinutes')}
          />
          <Form.Control.Feedback type="invalid">
            {fieldError(errors.everyMinutes?.message)}
          </Form.Control.Feedback>
        </Form.Group>
      ) : (
        <>
          <Form.Group className="mb-3" controlId="job-schedule-cron">
            <Form.Label>{t('adminJobs.editor.cron')}</Form.Label>
            <Form.Control
              className="font-monospace"
              placeholder="0 2 * * *"
              autoComplete="off"
              isInvalid={!!errors.cron}
              {...register('cron')}
            />
            <Form.Text className="text-600">
              {t('adminJobs.editor.cronHelp')}
            </Form.Text>
            <Form.Control.Feedback type="invalid">
              {fieldError(errors.cron?.message)}
            </Form.Control.Feedback>
          </Form.Group>
          <Form.Group className="mb-3" controlId="job-schedule-timezone">
            <Form.Label>{t('adminJobs.editor.timezone')}</Form.Label>
            <Form.Control
              placeholder="Europe/Rome"
              autoComplete="off"
              isInvalid={!!errors.timezone}
              {...register('timezone')}
            />
            <Form.Text className="text-600">
              {t('adminJobs.editor.timezoneHelp')}
            </Form.Text>
            <Form.Control.Feedback type="invalid">
              {fieldError(errors.timezone?.message)}
            </Form.Control.Feedback>
          </Form.Group>
        </>
      )}

      <div className="mb-3" aria-live="polite">
        <div className="fw-semibold fs-10 mb-1">
          {t('adminJobs.editor.preview')}
          {preview.isFetching && (
            <Spinner animation="border" size="sm" className="ms-2" />
          )}
        </div>
        {preview.error ? (
          <Alert variant="warning" className="fs-10 py-2 mb-0">
            {resolveErrorMessage(
              errorBody(preview.error),
              t('adminJobs.errors.generic'),
              { includeDetail: true }
            )}
          </Alert>
        ) : shown ? (
          <>
            <div className="text-600 fs-10 mb-1">{shown.description}</div>
            <ol
              className="fs-10 mb-0 ps-3"
              aria-label={t('adminJobs.editor.nextRuns')}
            >
              {shown.next.map(at => (
                <li key={at}>{formatDateTime(at, previewZone)}</li>
              ))}
            </ol>
          </>
        ) : (
          <div className="text-600 fs-10">
            {previewArg
              ? t('adminJobs.editor.previewLoading')
              : t('adminJobs.editor.previewIdle')}
          </div>
        )}
      </div>

      {errors.root?.server?.message && (
        <Alert variant="danger" className="fs-10 py-2" role="alert">
          {errors.root.server.message}
        </Alert>
      )}

      {actions.canManage && (
        <Button
          type="submit"
          variant="orkestra-primary"
          size="sm"
          disabled={!dirty || update.isLoading}
        >
          {t('adminJobs.editor.save')}
        </Button>
      )}
    </Form>
  );
};

export default ScheduleEditor;
